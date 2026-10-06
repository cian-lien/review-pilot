package publisher

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/persistence"
)

func (s *Service) locked(ctx context.Context, l lease, update func(*gorm.DB) error) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runtime persistence.RuntimeControl
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&runtime, 1).Error; err != nil {
			return err
		}
		if runtime.Mode != "go" || runtime.Epoch != l.Epoch {
			return fault.New(409, "PUBLICATION_LEASE_LOST")
		}
		var installation persistence.Installation
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&installation, "id=?", l.Run.InstallationID).Error; err != nil {
			return err
		}
		var repo persistence.Repository
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&repo, "installation_id=? AND id=?", l.Run.InstallationID, l.Run.RepositoryID).Error; err != nil {
			return err
		}
		if !installation.Active || !repo.Public || !repo.Enabled {
			return fault.New(409, "PUBLICATION_LEASE_LOST")
		}
		var job persistence.Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "run_id=?", l.Run.ID).Error; err != nil {
			return err
		}
		if job.Status != "result_ready" {
			return fault.New(409, "PUBLICATION_LEASE_LOST")
		}
		var rows []request
		if err := tx.Raw("SELECT * FROM publication_requests WHERE run_id=? AND state='running' AND lease_token=? AND lease_expires_at>clock_timestamp() FOR UPDATE", l.Run.ID, l.Token).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return fault.New(409, "PUBLICATION_LEASE_LOST")
		}
		return update(tx)
	})
}
func (s *Service) saveCheckID(ctx context.Context, l lease, id int64) error {
	return s.locked(ctx, l, func(tx *gorm.DB) error {
		return tx.Exec("UPDATE review_runs SET check_id=?,updated_at=clock_timestamp() WHERE id=? AND status='Publishing'", id, l.Run.ID).Error
	})
}
func (s *Service) finish(ctx context.Context, l lease, runStatus, pubState, jobStatus string) error {
	return s.locked(ctx, l, func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE jobs SET status=?,lease_token=NULL,lease_expires_at=NULL WHERE run_id=?", jobStatus, l.Run.ID).Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE publication_requests SET state=?,lease_token=NULL,lease_expires_at=NULL,last_error_code=NULL WHERE run_id=?", pubState, l.Run.ID).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE review_runs SET status=?,updated_at=clock_timestamp() WHERE id=?", runStatus, l.Run.ID).Error
	})
}
