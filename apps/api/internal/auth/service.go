package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"gorm.io/gorm"
	"regexp"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/persistence"
)

type Service struct {
	Store   *persistence.Store
	Config  config.Config
	Gateway *github.Gateway
}
type Session struct {
	UserID    int64
	Login     string
	UserToken string `json:"-"`
	CSRFToken string
}

func New(store *persistence.Store, cfg config.Config, gateway *github.Gateway) *Service {
	return &Service{store, cfg, gateway}
}

var opaquePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{43}$`)

func Hash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
func RandomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fault.New(503, "ENTROPY_UNAVAILABLE")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *Service) RequireSession(ctx context.Context, opaque string) (Session, error) {
	if s.Store == nil || s.Config.SessionSecret == "" {
		return Session{}, fault.New(503, "NOT_CONFIGURED")
	}
	if !opaquePattern.MatchString(opaque) {
		return Session{}, fault.New(401, "SESSION_REQUIRED")
	}
	var rows []struct {
		UserID             int64
		Login              string
		EncryptedUserToken string
	}
	db := s.Store.DB().WithContext(ctx)
	err := db.Raw("SELECT s.user_id,u.login,s.encrypted_user_token FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>clock_timestamp()", Hash(opaque)).Scan(&rows).Error
	if err != nil {
		return Session{}, err
	}
	if len(rows) == 0 {
		return Session{}, fault.New(401, "SESSION_EXPIRED")
	}
	token, err := OpenToken(rows[0].EncryptedUserToken, s.Config.SessionSecret)
	if err != nil {
		return Session{}, fault.New(401, "SESSION_INVALID")
	}
	proposed, err := RandomToken()
	if err != nil {
		return Session{}, err
	}
	var csrf []struct {
		CSRFToken string `gorm:"column:csrf_token"`
	}
	err = db.Raw("UPDATE sessions SET csrf_token=COALESCE(csrf_token,?) WHERE token_hash=? AND expires_at>clock_timestamp() RETURNING csrf_token", proposed, Hash(opaque)).Scan(&csrf).Error
	if err != nil {
		return Session{}, err
	}
	if len(csrf) == 0 {
		return Session{}, fault.New(401, "SESSION_EXPIRED")
	}
	return Session{UserID: rows[0].UserID, Login: rows[0].Login, UserToken: token, CSRFToken: csrf[0].CSRFToken}, nil
}
func (s *Service) ValidateCSRF(session Session, origin, csrf string) error {
	if origin != s.Config.AppURL || !opaquePattern.MatchString(csrf) || subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(csrf)) != 1 {
		return fault.New(403, "CSRF_REQUIRED")
	}
	return nil
}
func (s *Service) SaveAttempt(ctx context.Context, state, verifier string) error {
	return s.Store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM oauth_attempts WHERE expires_at<clock_timestamp()").Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO oauth_attempts(state_hash,verifier,expires_at) VALUES(?,?,clock_timestamp()+interval '10 minutes')", Hash(state), verifier).Error
	})
}
func (s *Service) ConsumeAttempt(ctx context.Context, state string) (string, error) {
	var rows []struct{ Verifier string }
	err := s.Store.DB().WithContext(ctx).Raw("DELETE FROM oauth_attempts WHERE state_hash=? AND expires_at>clock_timestamp() RETURNING verifier", Hash(state)).Scan(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fault.New(400, "OAUTH_STATE_INVALID")
	}
	return rows[0].Verifier, nil
}
func (s *Service) CreateSession(ctx context.Context, user github.User, token string, lifetime int) (string, error) {
	if lifetime <= 0 {
		return "", fault.New(400, "INVALID_SESSION_LIFETIME")
	}
	if lifetime > 28800 {
		lifetime = 28800
	}
	opaque, err := RandomToken()
	if err != nil {
		return "", err
	}
	csrf, err := RandomToken()
	if err != nil {
		return "", err
	}
	encrypted, err := SealToken(token, s.Config.SessionSecret)
	if err != nil {
		return "", err
	}
	err = s.Store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO users(id,login) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET login=EXCLUDED.login", user.ID, user.Login).Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO sessions(token_hash,user_id,encrypted_user_token,csrf_token,expires_at) VALUES(?,?,?,?,clock_timestamp()+?*interval '1 second')", Hash(opaque), user.ID, encrypted, csrf, lifetime).Error
	})
	return opaque, err
}
