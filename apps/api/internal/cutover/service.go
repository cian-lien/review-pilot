package cutover

import (
	"context"
	"database/sql"
	"encoding/json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
	"time"
)

type Report struct {
	Mode                    string `json:"mode"`
	Epoch                   int64  `json:"epoch"`
	ActiveLeaseCount        int64  `json:"activeLeaseCount"`
	UnsubmittedCount        int64  `json:"unsubmittedCount"`
	PublicationPendingCount int64  `json:"publicationPendingCount"`
	UnreconciledCheckCount  int64  `json:"unreconciledCheckCount"`
	CanSwitch               bool   `json:"canSwitch"`
}
type Service struct {
	DB *gorm.DB
	// An operator assertion, not process discovery: old binaries do not understand runtime_control.
	ConsumersStopped bool
	Inspect          func(context.Context, persistence.Run) (*github.Check, error)
}

func New(db *gorm.DB, gateway *github.Gateway) *Service {
	s := &Service{DB: db}
	if gateway != nil {
		s.Inspect = func(ctx context.Context, r persistence.Run) (*github.Check, error) {
			var repo persistence.Repository
			if err := db.WithContext(ctx).First(&repo, "installation_id=? AND id=?", r.InstallationID, r.RepositoryID).Error; err != nil {
				return nil, err
			}
			token, err := gateway.InstallationToken(ctx, contracts.TenantRef{InstallationId: int(r.InstallationID), RepositoryId: int(r.RepositoryID)})
			if err != nil {
				return nil, err
			}
			// NULL check_id cannot prove that an earlier POST had no remote effect.
			return gateway.FindConnectionCheck(ctx, token, repo.FullName, r.HeadSHA, r.ID)
		}
	}
	return s
}

type observation struct {
	Run   persistence.Run
	Check *github.Check
}

func (s *Service) read(ctx context.Context, tx *gorm.DB) (Report, []observation, error) {
	report := Report{}
	var control persistence.RuntimeControl
	if err := tx.First(&control, 1).Error; err != nil {
		return report, nil, err
	}
	report.Mode = control.Mode
	report.Epoch = int64(control.Epoch)
	queries := []struct {
		query string
		dest  *int64
	}{
		{"SELECT (SELECT count(*) FROM jobs WHERE status='running' AND lease_expires_at>clock_timestamp())+(SELECT count(*) FROM publication_requests WHERE state='running' AND lease_expires_at>clock_timestamp())+(SELECT count(*) FROM outbox_events WHERE sent_at IS NULL AND relay_expires_at>clock_timestamp())", &report.ActiveLeaseCount},
		{"SELECT count(*) FROM jobs WHERE status IN ('queued','running')", &report.UnsubmittedCount},
		{"SELECT count(*) FROM publication_requests WHERE state IN ('pending','running','failed')", &report.PublicationPendingCount},
	}
	for _, q := range queries {
		if err := tx.Raw(q.query).Scan(q.dest).Error; err != nil {
			return report, nil, err
		}
	}
	var runs []persistence.Run
	// Failed/unknown publications are included, even when the run exhausted retries.
	if err := tx.Where("status NOT IN ('Completed','Stale','Cancelled') OR EXISTS(SELECT 1 FROM publication_requests p WHERE p.run_id=review_runs.id AND p.state IN ('pending','running','failed'))").Order("id").Limit(1001).Find(&runs).Error; err != nil {
		return report, nil, err
	}
	if len(runs) > 1000 {
		return report, nil, fault.New(409, "CUTOVER_RECONCILIATION_LIMIT")
	}
	observations := make([]observation, 0, len(runs))
	for _, r := range runs {
		// Legacy consumers cannot represent S2 model or publication uncertainty.
		if r.PipelineVersion == "review-v1" {
			report.UnreconciledCheckCount++
			continue
		}
		if s.Inspect == nil {
			report.UnreconciledCheckCount++
			continue
		}
		check, err := s.Inspect(ctx, r)
		if err != nil {
			report.UnreconciledCheckCount++
			continue
		}
		if check == nil {
			if r.CheckID != nil {
				report.UnreconciledCheckCount++
				continue
			}
		} else if check.ID <= 0 || check.Name != github.ConnectionCheckName || check.ExternalID != "reviewpilot:"+r.ID || check.HeadSHA != r.HeadSHA || check.Status != "completed" || check.Conclusion == nil || (*check.Conclusion != "neutral" && *check.Conclusion != "cancelled") {
			report.UnreconciledCheckCount++
			continue
		}
		observations = append(observations, observation{r, check})
	}
	report.CanSwitch = s.ConsumersStopped && report.ActiveLeaseCount == 0 && report.UnreconciledCheckCount == 0
	return report, observations, nil
}
func (s *Service) Preflight(ctx context.Context) (Report, error) {
	report := Report{}
	if s.DB == nil {
		return report, fault.New(503, "DATABASE_UNAVAILABLE")
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		report, _, err = s.read(ctx, tx)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return report, err
}
func (s *Service) Switch(ctx context.Context, expectedEpoch int64, target string) (Report, error) {
	report := Report{}
	if target != "go" && target != "legacy" {
		return report, fault.New(400, "INVALID_RUNTIME_TARGET")
	}
	if !s.ConsumersStopped {
		return report, fault.New(409, "CONSUMERS_STOP_CONFIRMATION_REQUIRED")
	}
	if s.DB == nil {
		return report, fault.New(503, "DATABASE_UNAVAILABLE")
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout='2s'").Error; err != nil {
			return err
		}
		var control persistence.RuntimeControl
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&control, 1).Error; err != nil {
			return err
		}
		if int64(control.Epoch) != expectedEpoch || control.Mode == target {
			return fault.New(409, "RUNTIME_EPOCH_CONFLICT")
		}
		// Freeze writers while reading external facts. No GitHub Check mutations are made here.
		if err := tx.Exec("LOCK TABLE installations,repositories,pull_requests,review_runs,jobs,outbox_events,publication_requests IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		var observed []observation
		var err error
		report, observed, err = s.read(ctx, tx)
		if err != nil {
			return err
		}
		if !report.CanSwitch {
			return fault.New(409, "CUTOVER_NOT_SAFE")
		}
		for _, o := range observed {
			if o.Check != nil {
				runStatus, jobStatus, pubState := "Completed", "completed", "published"
				if *o.Check.Conclusion == "cancelled" {
					runStatus = "Cancelled"
					jobStatus = "cancelled"
					pubState = "cancelled"
				}
				if err = tx.Exec("UPDATE review_runs SET status=?,check_id=?,updated_at=clock_timestamp() WHERE id=?", runStatus, o.Check.ID, o.Run.ID).Error; err != nil {
					return err
				}
				if err = tx.Exec("UPDATE jobs SET status=?,lease_token=NULL,lease_expires_at=NULL WHERE run_id=?", jobStatus, o.Run.ID).Error; err != nil {
					return err
				}
				if err = tx.Exec("UPDATE publication_requests SET state=?,lease_token=NULL,lease_expires_at=NULL WHERE run_id=?", pubState, o.Run.ID).Error; err != nil {
					return err
				}
			} else if target == "legacy" {
				// Confirmed no remote Check; retained job_results remain historical accepted facts.
				if err = tx.Exec("UPDATE review_runs SET status='Queued',updated_at=clock_timestamp() WHERE id=? AND EXISTS(SELECT 1 FROM jobs j WHERE j.run_id=review_runs.id AND j.status='result_ready')", o.Run.ID).Error; err != nil {
					return err
				}
				if err = tx.Exec("UPDATE jobs SET status='queued' WHERE run_id=? AND status='result_ready'", o.Run.ID).Error; err != nil {
					return err
				}
				if err = tx.Exec("UPDATE publication_requests SET state='cancelled',lease_token=NULL,lease_expires_at=NULL WHERE run_id=? AND state IN ('pending','running','failed')", o.Run.ID).Error; err != nil {
					return err
				}
			} else {
				// Rollback may have queued a job whose immutable result was already accepted.
				// Re-enter publication directly; a Worker confirmation cannot recreate its intent.
				if o.Run.Status == "Cancelled" || o.Run.Status == "Stale" {
					if err = tx.Exec("UPDATE publication_requests SET state='cancelled',lease_token=NULL,lease_expires_at=NULL WHERE run_id=?", o.Run.ID).Error; err != nil {
						return err
					}
					continue
				}
				var accepted []struct{ TaskID, ResultID, InputHash, ResultHash string }
				if err = tx.Raw("SELECT j.id AS task_id,x.result_id,x.input_hash,x.result_hash FROM jobs j JOIN job_results x ON x.task_id=j.id WHERE j.run_id=? AND j.kind='review' AND j.status IN ('queued','running','result_ready')", o.Run.ID).Scan(&accepted).Error; err != nil {
					return err
				}
				if len(accepted) > 0 {
					if len(accepted) != 1 || accepted[0].ResultID != accepted[0].TaskID+":connection" || accepted[0].InputHash != jobs.Hash(o.Run.Snapshot) || accepted[0].ResultHash != jobs.ResultHash(accepted[0].TaskID, o.Run.ID, accepted[0].InputHash) {
						return fault.New(409, "CUTOVER_RESULT_INVALID")
					}
					var authorized int64
					if err = tx.Raw("SELECT count(*) FROM repositories r JOIN installations i ON i.id=r.installation_id WHERE r.installation_id=? AND r.id=? AND r.public AND r.enabled AND i.active", o.Run.InstallationID, o.Run.RepositoryID).Scan(&authorized).Error; err != nil {
						return err
					}
					if authorized != 1 {
						if err = tx.Exec("UPDATE jobs SET status='cancelled',lease_token=NULL,lease_expires_at=NULL WHERE id=?", accepted[0].TaskID).Error; err != nil {
							return err
						}
						if err = tx.Exec("UPDATE publication_requests SET state='cancelled',lease_token=NULL,lease_expires_at=NULL WHERE run_id=?", o.Run.ID).Error; err != nil {
							return err
						}
						if err = tx.Exec("UPDATE review_runs SET status='Cancelled',updated_at=clock_timestamp() WHERE id=?", o.Run.ID).Error; err != nil {
							return err
						}
						continue
					}
					if err = tx.Exec("UPDATE jobs SET status='result_ready',lease_token=NULL,lease_expires_at=NULL WHERE id=?", accepted[0].TaskID).Error; err != nil {
						return err
					}
					if err = tx.Exec("UPDATE review_runs SET status='Publishing',updated_at=clock_timestamp() WHERE id=?", o.Run.ID).Error; err != nil {
						return err
					}
					if err = tx.Exec("INSERT INTO publication_requests(run_id) VALUES(?) ON CONFLICT(run_id) DO UPDATE SET state='pending',attempts=0,lease_token=NULL,lease_expires_at=NULL,last_error_code=NULL,available_at=clock_timestamp()", o.Run.ID).Error; err != nil {
						return err
					}
					continue
				}
				if err = tx.Exec("UPDATE publication_requests SET state='pending',attempts=0,lease_token=NULL,lease_expires_at=NULL,available_at=clock_timestamp() WHERE run_id=? AND state IN ('running','failed')", o.Run.ID).Error; err != nil {
					return err
				}
			}
		}
		if err = tx.Exec("UPDATE jobs SET status='queued' WHERE status='running'").Error; err != nil {
			return err
		}
		if err = tx.Exec("UPDATE jobs SET lease_token=NULL,lease_expires_at=NULL,execution_epoch=? WHERE status IN ('queued','result_ready')", expectedEpoch+1).Error; err != nil {
			return err
		}
		if target == "go" {
			var rows []persistence.Job
			if err = tx.Where("kind='review' AND status='queued'").Find(&rows).Error; err != nil {
				return err
			}
			for _, j := range rows {
				var r persistence.Run
				if err = tx.First(&r, "id=?", j.RunID).Error; err != nil {
					return err
				}
				if r.PipelineVersion == "review-v1" {
					continue
				}
				if r.Status == "Completed" || r.Status == "Cancelled" || r.Status == "Stale" || r.Status == "Failed" {
					continue
				}
				var authorized int64
				if err = tx.Raw("SELECT count(*) FROM repositories r JOIN installations i ON i.id=r.installation_id WHERE r.installation_id=? AND r.id=? AND r.public AND r.enabled AND i.active", r.InstallationID, r.RepositoryID).Scan(&authorized).Error; err != nil {
					return err
				}
				if authorized != 1 {
					if err = tx.Exec("UPDATE jobs SET status='cancelled' WHERE id=?", j.ID).Error; err != nil {
						return err
					}
					continue
				}
				var snapshot contracts.RunSnapshot
				if json.Unmarshal(r.Snapshot, &snapshot) != nil || snapshot.Identity.HeadSha != r.HeadSHA || snapshot.Identity.Tenant.InstallationId != int(r.InstallationID) || snapshot.Identity.Tenant.RepositoryId != int(r.RepositoryID) || snapshot.ModelConfigVersion != "none" {
					return fault.New(409, "CUTOVER_SNAPSHOT_INVALID")
				}
				if err = tx.Exec("UPDATE review_runs SET status='Queued',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; err != nil {
					return err
				}
				if err = jobs.Schedule(tx, j, r, time.Now()); err != nil {
					return err
				}
			}
		}
		change := tx.Exec("UPDATE runtime_control SET mode=?,epoch=epoch+1 WHERE id=1 AND epoch=?", target, expectedEpoch)
		if change.Error != nil {
			return change.Error
		}
		if change.RowsAffected != 1 {
			return fault.New(409, "RUNTIME_EPOCH_CONFLICT")
		}
		report.Mode = target
		report.Epoch = expectedEpoch + 1
		report.CanSwitch = false
		return nil
	})
	return report, err
}
