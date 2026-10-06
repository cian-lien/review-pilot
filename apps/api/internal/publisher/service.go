package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/observability"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/repositories"
	"time"
)

type Service struct {
	DB      *gorm.DB
	Gateway *github.Gateway
	Config  config.Config
}

func New(db *gorm.DB, gateway *github.Gateway, cfg config.Config) *Service {
	return &Service{db, gateway, cfg}
}

type lease struct {
	Run      persistence.Run
	Token    string
	Epoch    int
	Attempt  int
	FullName string
}
type request struct {
	RunID          string
	State          string
	Attempts       int
	LeaseToken     *string
	LeaseExpiresAt *time.Time
	AvailableAt    time.Time
}

func (request) TableName() string { return "publication_requests" }
func (s *Service) claim(ctx context.Context, runID string) (*lease, error) {
	if s.DB == nil {
		return nil, fault.New(503, "DATABASE_UNAVAILABLE")
	}
	var acquired *lease
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runtime persistence.RuntimeControl
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&runtime, 1).Error; err != nil {
			return err
		}
		if runtime.Mode != "go" {
			return nil
		}
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("(state='pending' OR (state='running' AND lease_expires_at<=clock_timestamp())) AND available_at<=clock_timestamp()").Order("available_at,run_id")
		if runID != "" {
			query = query.Where("run_id=?", runID)
		}
		var rows []request
		if err := query.Limit(1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		req := rows[0]
		var run persistence.Run
		if err := tx.First(&run, "id=?", req.RunID).Error; err != nil {
			return err
		}
		if req.Attempts >= 5 {
			if err := tx.Exec("UPDATE publication_requests SET state='failed',lease_token=NULL,lease_expires_at=NULL,last_error_code='PUBLICATION_RETRIES_EXHAUSTED' WHERE run_id=?", run.ID).Error; err != nil {
				return err
			}
			return tx.Exec("UPDATE review_runs SET status='Failed',updated_at=clock_timestamp() WHERE id=? AND status='Publishing'", run.ID).Error
		}
		token := jobs.UUID()
		if err := tx.Exec("UPDATE publication_requests SET state='running',attempts=attempts+1,lease_token=?,lease_expires_at=clock_timestamp()+interval '90 seconds' WHERE run_id=?", token, run.ID).Error; err != nil {
			return err
		}
		var repo persistence.Repository
		if err := tx.First(&repo, "installation_id=? AND id=?", run.InstallationID, run.RepositoryID).Error; err != nil {
			return err
		}
		acquired = &lease{Run: run, Token: token, Epoch: runtime.Epoch, Attempt: req.Attempts + 1, FullName: repo.FullName}
		return nil
	})
	return acquired, err
}
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	l, err := s.claim(ctx, "")
	if err != nil || l == nil {
		return false, err
	}
	err = s.publish(ctx, *l)
	if err != nil {
		_ = s.retry(ctx, *l)
		return true, err
	}
	return true, nil
}
func (s *Service) PublishConnection(ctx context.Context, runID string) error {
	l, err := s.claim(ctx, runID)
	if err != nil {
		return err
	}
	if l == nil {
		return fault.New(409, "PUBLICATION_NOT_READY")
	}
	err = s.publish(ctx, *l)
	if err != nil {
		_ = s.retry(ctx, *l)
	}
	return err
}
func (s *Service) fence(ctx context.Context, l lease) error {
	var count int64
	err := s.DB.WithContext(ctx).Raw("SELECT count(*) FROM publication_requests p JOIN review_runs r ON r.id=p.run_id JOIN jobs j ON j.run_id=r.id JOIN repositories repo ON repo.installation_id=r.installation_id AND repo.id=r.repository_id JOIN installations i ON i.id=repo.installation_id CROSS JOIN runtime_control c WHERE p.run_id=? AND p.state='running' AND p.lease_token=? AND p.lease_expires_at>clock_timestamp() AND r.status='Publishing' AND j.status='result_ready' AND repo.public AND repo.enabled AND i.active AND c.id=1 AND c.mode='go' AND c.epoch=? AND EXISTS(SELECT 1 FROM job_results x WHERE x.task_id=j.id)", l.Run.ID, l.Token, l.Epoch).Scan(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return fault.New(409, "PUBLICATION_LEASE_LOST")
	}
	return nil
}
func (s *Service) remotePermission(ctx context.Context, l lease, token string) error {
	if err := s.fence(ctx, l); err != nil {
		return err
	}
	if err := s.Gateway.AppInstallation(ctx, int(l.Run.InstallationID)); err != nil {
		return err
	}
	repo, err := s.Gateway.UserRepository(ctx, token, l.FullName)
	if err != nil {
		return err
	}
	if repo.ID != int(l.Run.RepositoryID) || !repo.Public() {
		return fault.New(403, "REPOSITORY_ACCESS_DENIED")
	}
	return s.fence(ctx, l)
}

var stale = fault.New(409, "PR_SNAPSHOT_STALE")

func (s *Service) guard(ctx context.Context, l lease, token string) error {
	if err := s.remotePermission(ctx, l, token); err != nil {
		return err
	}
	pr, err := s.Gateway.PullRequest(ctx, token, l.FullName, l.Run.PrNumber)
	if err != nil {
		return err
	}
	if pr.State != "open" || pr.Head.SHA != l.Run.HeadSHA {
		return stale
	}
	return s.fence(ctx, l)
}
func (s *Service) publish(ctx context.Context, l lease) error {
	var correlation struct{ TaskID, Traceparent string }
	_ = s.DB.WithContext(ctx).Raw("SELECT j.id AS task_id,COALESCE((SELECT o.payload->>'traceparent' FROM outbox_events o WHERE o.task_id=j.id ORDER BY notification_generation DESC LIMIT 1),'') AS traceparent FROM jobs j WHERE j.run_id=? AND j.kind='review' LIMIT 1", l.Run.ID).Scan(&correlation).Error
	ctx, span := observability.FromContext(ctx).Start(observability.Extract(ctx, correlation.Traceparent), "connection.publish", observability.Fields{RunID: l.Run.ID, TaskID: correlation.TaskID, Attempt: l.Attempt})
	defer span.End()
	if err := s.fence(ctx, l); err != nil {
		return err
	}
	var snapshot contracts.RunSnapshot
	if err := json.Unmarshal(l.Run.Snapshot, &snapshot); err != nil || snapshot.ModelConfigVersion != "none" || snapshot.Identity.HeadSha != l.Run.HeadSHA || snapshot.Identity.Tenant.InstallationId != int(l.Run.InstallationID) || snapshot.Identity.Tenant.RepositoryId != int(l.Run.RepositoryID) {
		return fault.New(409, "PUBLICATION_SNAPSHOT_INVALID")
	}
	tenant := contracts.TenantRef{InstallationId: int(l.Run.InstallationID), RepositoryId: int(l.Run.RepositoryID)}
	token, err := s.Gateway.InstallationToken(ctx, tenant)
	if err != nil {
		return s.permissionError(ctx, l, err)
	}
	if err = s.guard(ctx, l, token); err != nil {
		if errors.Is(err, stale) {
			return s.cancelStale(ctx, l, token)
		}
		return s.permissionError(ctx, l, err)
	}
	var check *github.Check
	if l.Run.CheckID != nil {
		check, err = s.Gateway.GetConnectionCheck(ctx, token, l.FullName, l.Run.HeadSHA, l.Run.ID, *l.Run.CheckID)
	} else {
		check, err = s.Gateway.FindConnectionCheck(ctx, token, l.FullName, l.Run.HeadSHA, l.Run.ID)
	}
	if err != nil {
		return s.permissionError(ctx, l, err)
	}
	if check == nil {
		if err = s.guard(ctx, l, token); err != nil {
			if errors.Is(err, stale) {
				return s.cancelStale(ctx, l, token)
			}
			return s.permissionError(ctx, l, err)
		}
		created, e := s.Gateway.WriteConnectionCheck(ctx, token, l.FullName, l.Run.HeadSHA, l.Run.ID, s.Config.AppURL, nil, "")
		if e != nil {
			return s.permissionError(ctx, l, e)
		}
		check = &created
	}
	checkID := int64(check.ID)
	if err = s.saveCheckID(ctx, l, checkID); err != nil {
		return err
	}
	if check.Status != "completed" || check.Conclusion == nil || *check.Conclusion != "neutral" {
		if err = s.guard(ctx, l, token); err != nil {
			if errors.Is(err, stale) {
				return s.cancelStale(ctx, l, token)
			}
			return s.permissionError(ctx, l, err)
		}
		if _, err = s.Gateway.WriteConnectionCheck(ctx, token, l.FullName, l.Run.HeadSHA, l.Run.ID, s.Config.AppURL, &checkID, "neutral"); err != nil {
			return s.permissionError(ctx, l, err)
		}
	}
	if err = s.guard(ctx, l, token); err != nil {
		if errors.Is(err, stale) {
			return s.cancelStale(ctx, l, token)
		}
		return s.permissionError(ctx, l, err)
	}
	return s.finish(ctx, l, "Completed", "published", "completed")
}
func (s *Service) permissionError(ctx context.Context, l lease, err error) error {
	var failure *fault.Error
	if !errors.As(err, &failure) || failure.Status != 403 {
		return err
	}
	revokeErr := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT id FROM installations WHERE id=? FOR UPDATE", l.Run.InstallationID).Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE repositories SET enabled=false,auth_revision=auth_revision+1,config_version=config_version+1 WHERE installation_id=? AND id=?", l.Run.InstallationID, l.Run.RepositoryID).Error; err != nil {
			return err
		}
		return repositories.CancelWork(tx, contracts.TenantRef{InstallationId: int(l.Run.InstallationID), RepositoryId: int(l.Run.RepositoryID)})
	})
	if revokeErr != nil {
		return revokeErr
	}
	return err
}
func (s *Service) cancelStale(ctx context.Context, l lease, token string) error {
	// Never create a new check for an old head; only close a previously owned one.
	existing, err := s.Gateway.FindConnectionCheck(ctx, token, l.FullName, l.Run.HeadSHA, l.Run.ID)
	if err != nil {
		return s.permissionError(ctx, l, err)
	}
	if existing != nil {
		if err = s.remotePermission(ctx, l, token); err != nil {
			return s.permissionError(ctx, l, err)
		}
		id := int64(existing.ID)
		if _, err = s.Gateway.WriteConnectionCheck(ctx, token, l.FullName, l.Run.HeadSHA, l.Run.ID, s.Config.AppURL, &id, "cancelled"); err != nil {
			return s.permissionError(ctx, l, err)
		}
	}
	return s.finish(ctx, l, "Stale", "cancelled", "cancelled")
}
func (s *Service) retry(ctx context.Context, l lease) error {
	n := l.Attempt - 1
	if n < 0 {
		n = 0
	}
	if n > 3 {
		n = 3
	}
	seconds := 30 * (1 << n)
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runtime persistence.RuntimeControl
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&runtime, 1).Error; err != nil {
			return err
		}
		if runtime.Mode != "go" || runtime.Epoch != l.Epoch {
			return fault.New(409, "PUBLICATION_LEASE_LOST")
		}
		return tx.Exec("UPDATE publication_requests SET state='pending',lease_token=NULL,lease_expires_at=NULL,last_error_code='CONNECTION_PUBLICATION_FAILED',available_at=clock_timestamp()+?*interval '1 second' WHERE run_id=? AND state='running' AND lease_token=?", seconds, l.Run.ID, l.Token).Error
	})
}
