package repositories

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"reviewpilot.local/api/internal/auth"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/persistence"
)

type Service struct {
	Store *persistence.Store
	Auth  *auth.Service
}

func New(store *persistence.Store, s *auth.Service) *Service { return &Service{store, s} }
func CancelWork(tx *gorm.DB, tenant contracts.TenantRef) error {
	for _, query := range []string{
		"UPDATE jobs j SET status='cancelled',lease_token=NULL,lease_expires_at=NULL FROM review_runs r WHERE j.run_id=r.id AND r.installation_id=? AND r.repository_id=? AND j.status IN('queued','running','result_ready')",
		"UPDATE publication_requests p SET state='cancelled',lease_token=NULL,lease_expires_at=NULL FROM review_runs r WHERE p.run_id=r.id AND r.installation_id=? AND r.repository_id=? AND p.state IN('pending','running')",
		"UPDATE review_runs SET status='Cancelled',updated_at=clock_timestamp() WHERE installation_id=? AND repository_id=? AND status NOT IN('Completed','Partial','Stale','Cancelled','Failed')",
	} {
		if err := tx.Exec(query, tenant.InstallationId, tenant.RepositoryId).Error; err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) SetEnabled(ctx context.Context, session auth.Session, tenant contracts.TenantRef, enabled bool, expectedVersion int64) error {
	current, err := s.Auth.Gateway.Access(ctx, session.UserToken, tenant, true)
	if err != nil {
		return err
	}
	return s.Store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var installation persistence.Installation
		if err := tx.Raw("SELECT * FROM installations WHERE id=? FOR UPDATE", tenant.InstallationId).Scan(&installation).Error; err != nil {
			return err
		}
		if !installation.Active {
			return fault.New(403, "INSTALLATION_INACTIVE")
		}
		var repo persistence.Repository
		if err := tx.Raw("SELECT * FROM repositories WHERE installation_id=? AND id=? FOR UPDATE", tenant.InstallationId, tenant.RepositoryId).Scan(&repo).Error; err != nil {
			return err
		}
		if repo.ID == 0 || !repo.Public {
			return fault.New(403, "REPOSITORY_ACCESS_DENIED")
		}
		if int64(repo.ConfigVersion) != expectedVersion {
			return fault.New(409, "VERSION_CONFLICT")
		}
		if err := tx.Exec("UPDATE repositories SET enabled=?,full_name=?,config_version=config_version+1,auth_revision=auth_revision+1 WHERE installation_id=? AND id=?", enabled, current.FullName, tenant.InstallationId, tenant.RepositoryId).Error; err != nil {
			return err
		}
		if !enabled {
			return CancelWork(tx, tenant)
		}
		return nil
	})
}
func (s *Service) SyncInstallation(ctx context.Context, session auth.Session, id int) error {
	if id <= 0 || id > 9007199254740991 {
		return fault.New(400, "INVALID_INSTALLATION")
	}
	if err := s.Auth.Gateway.RequireInstallation(ctx, session.UserToken, id); err != nil {
		return err
	}
	repos, err := s.Auth.Gateway.InstallationRepositories(ctx, session.UserToken, id)
	if err != nil {
		return err
	}
	// Permissions are resolved before opening the transaction; no network calls while holding database locks.
	allowed := make(map[int]bool)
	for _, repo := range repos {
		if !repo.Public() {
			continue
		}
		current, err := s.Auth.Gateway.UserRepository(ctx, session.UserToken, repo.FullName)
		if err != nil {
			return err
		}
		allowed[repo.ID] = current.ID == repo.ID && current.Public() && current.Permissions.Pull && (current.Permissions.Maintain || current.Permissions.Admin)
	}
	return s.Store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO installations(id) VALUES(?) ON CONFLICT DO NOTHING", id).Error; err != nil {
			return err
		}
		var installation persistence.Installation
		if err := tx.Raw("SELECT * FROM installations WHERE id=? FOR UPDATE", id).Scan(&installation).Error; err != nil {
			return err
		}
		if !installation.Active {
			return fault.New(403, "INSTALLATION_INACTIVE")
		}
		if err := tx.Exec("INSERT INTO installation_memberships(user_id,installation_id) VALUES(?,?) ON CONFLICT DO NOTHING", session.UserID, id).Error; err != nil {
			return err
		}
		for _, repo := range repos {
			if !repo.Public() {
				if err := tx.Exec("UPDATE repositories SET public=false,enabled=false,auth_revision=auth_revision+1,config_version=config_version+1 WHERE installation_id=? AND id=?", id, repo.ID).Error; err != nil {
					return err
				}
				if err := CancelWork(tx, contracts.TenantRef{InstallationId: id, RepositoryId: repo.ID}); err != nil {
					return err
				}
				continue
			}
			if !allowed[repo.ID] {
				continue
			}
			if err := tx.Exec("INSERT INTO repositories(installation_id,id,full_name,public) VALUES(?,?,?,true) ON CONFLICT(installation_id,id) DO UPDATE SET full_name=EXCLUDED.full_name,public=true,auth_revision=repositories.auth_revision+1,config_version=CASE WHEN repositories.public THEN repositories.config_version ELSE repositories.config_version+1 END", id, repo.ID, repo.FullName).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Service) List(ctx context.Context, session auth.Session) ([]contracts.RepositorySummary, error) {
	rows := []persistence.Repository{}
	err := s.Store.DB().WithContext(ctx).Raw("SELECT r.* FROM repositories r JOIN installations i ON i.id=r.installation_id JOIN installation_memberships m ON m.installation_id=r.installation_id WHERE m.user_id=? AND r.public AND i.active ORDER BY r.full_name LIMIT 100", session.UserID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := []contracts.RepositorySummary{}
	for _, row := range rows {
		tenant := contracts.TenantRef{InstallationId: int(row.InstallationID), RepositoryId: int(row.ID)}
		repo, err := s.Auth.Gateway.Access(ctx, session.UserToken, tenant, false)
		if err != nil {
			var failure *fault.Error
			if errors.As(err, &failure) && failure.Status == 403 {
				continue
			}
			return nil, err
		}
		result = append(result, contracts.RepositorySummary{Tenant: tenant, FullName: repo.FullName, Public: true, Enabled: row.Enabled, ConfigVersion: row.ConfigVersion, CanManage: repo.Permissions.Admin || repo.Permissions.Maintain})
	}
	return result, nil
}
func (s *Service) Runs(ctx context.Context, session auth.Session, limit int) ([]contracts.RunSummary, error) {
	visible, err := s.List(ctx, session)
	if err != nil {
		return nil, err
	}
	result := []contracts.RunSummary{}
	if len(visible) == 0 {
		return result, nil
	}
	clauses := []string{}
	args := []any{}
	for _, repo := range visible {
		clauses = append(clauses, "(r.installation_id=? AND r.repository_id=?)")
		args = append(args, repo.Tenant.InstallationId, repo.Tenant.RepositoryId)
	}
	if limit < 1 || limit > 20 {
		limit = 20
	}
	args = append(args, session.UserID, limit)
	var rows []struct {
		persistence.Run
		FullName string
	}
	query := "SELECT r.*,repo.full_name FROM review_runs r JOIN repositories repo ON repo.installation_id=r.installation_id AND repo.id=r.repository_id JOIN installations i ON i.id=r.installation_id JOIN installation_memberships m ON m.installation_id=r.installation_id WHERE (" + strings.Join(clauses, " OR ") + ") AND m.user_id=? AND i.active AND repo.public ORDER BY r.created_at DESC LIMIT ?"
	if err := s.Store.DB().WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		item := contracts.RunSummary{Id: row.ID, Tenant: contracts.TenantRef{InstallationId: int(row.InstallationID), RepositoryId: int(row.RepositoryID)}, FullName: row.FullName, PrNumber: row.PrNumber, HeadSha: row.HeadSHA, Status: contracts.RunSummaryStatus(row.Status), CreatedAt: row.CreatedAt}
		if row.CheckID != nil {
			value := int(*row.CheckID)
			item.CheckId = &value
		}
		result = append(result, item)
	}
	return result, nil
}
