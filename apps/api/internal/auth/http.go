package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/url"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"strings"
)

func (s *Service) Session(c *gin.Context) (Session, error) {
	token, _ := c.Cookie("reviewpilot_session")
	return s.RequireSession(c.Request.Context(), token)
}
func (s *Service) CSRF(c *gin.Context, session Session) error {
	token := c.GetHeader("X-CSRF-Token")
	if token == "" {
		token = c.PostForm("csrfToken")
	}
	return s.ValidateCSRF(session, c.GetHeader("Origin"), token)
}
func setCookie(c *gin.Context, s *Service, name, value string, lifetime int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, value, lifetime, "/", "", strings.HasPrefix(s.Config.AppURL, "https://"), true)
}
func Register(r *gin.Engine, s *Service) {
	r.GET("/api/session", func(c *gin.Context) {
		session, err := s.Session(c)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		var response contracts.SessionResponse
		response.User.Id = int(session.UserID)
		response.User.Login = session.Login
		response.CsrfToken = session.CSRFToken
		c.JSON(200, response)
	})
	r.GET("/auth/github", func(c *gin.Context) {
		if s.Store == nil || s.Config.GitHubClientID == "" || s.Config.GitHubClientSecret == "" || s.Config.SessionSecret == "" {
			httpapi.Error(c, 503, "NOT_CONFIGURED", "登录尚未配置")
			return
		}
		state, err := RandomToken()
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		verifier, err := RandomToken()
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		if err := s.SaveAttempt(c.Request.Context(), state, verifier); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		digest := sha256.Sum256([]byte(verifier))
		params := url.Values{"client_id": {s.Config.GitHubClientID}, "redirect_uri": {s.Config.AppURL + "/auth/github/callback"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}}
		setCookie(c, s, "reviewpilot_oauth", state, 600)
		c.Redirect(302, s.Gateway.OAuthURL+"/login/oauth/authorize?"+params.Encode())
	})
	r.GET("/auth/github/callback", func(c *gin.Context) {
		if s.Store == nil || s.Config.SessionSecret == "" {
			httpapi.Error(c, 503, "NOT_CONFIGURED", "登录尚未配置")
			return
		}
		state, code := c.Query("state"), c.Query("code")
		expected, _ := c.Cookie("reviewpilot_oauth")
		if !opaquePattern.MatchString(state) || code == "" || len(code) > 1024 || subtle.ConstantTimeCompare([]byte(expected), []byte(state)) != 1 {
			httpapi.Error(c, 400, "OAUTH_STATE_INVALID", "请重新登录")
			return
		}
		verifier, err := s.ConsumeAttempt(c.Request.Context(), state)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		token, lifetime, err := s.Gateway.Exchange(c.Request.Context(), code, verifier)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		user, err := s.Gateway.User(c.Request.Context(), token)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		opaque, err := s.CreateSession(c.Request.Context(), user, token, lifetime)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		setCookie(c, s, "reviewpilot_session", opaque, lifetime)
		setCookie(c, s, "reviewpilot_oauth", "", -1)
		c.Redirect(302, s.Config.AppURL+"/repositories")
	})
	r.POST("/auth/logout", func(c *gin.Context) {
		session, err := s.Session(c)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		if err := s.CSRF(c, session); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		token, _ := c.Cookie("reviewpilot_session")
		if err := s.Store.DB().WithContext(c.Request.Context()).Exec("DELETE FROM sessions WHERE token_hash=?", Hash(token)).Error; err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		setCookie(c, s, "reviewpilot_session", "", -1)
		c.Status(204)
	})
}
