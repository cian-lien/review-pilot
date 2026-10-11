package reviews

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"regexp"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
	"time"
)

type Service struct{ DB *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{db} }
func (s *Service) transaction(ctx context.Context, fn func(*gorm.DB) error) error {
	if s.DB == nil {
		return fault.New(503, "DATABASE_UNAVAILABLE")
	}
	return s.DB.WithContext(ctx).Transaction(fn)
}
func (s *Service) Claim(ctx context.Context, req contracts.ClaimReviewRequest) (contracts.ClaimReviewResponse, error) {
	answer := contracts.ClaimReviewResponse{}
	body, _ := json.Marshal(req)
	if contracts.DecodeReview("ClaimReviewRequest", body, new(contracts.ClaimReviewRequest), 65536) != nil {
		return answer, fault.New(400, "INVALID_TASK")
	}
	err := s.transaction(ctx, func(tx *gorm.DB) error {
		var runtime persistence.RuntimeControl
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&runtime, 1).Error; err != nil {
			return err
		}
		if runtime.Mode != "go" {
			return fault.New(503, "RUNTIME_PAUSED")
		}
		j, r, authorized, err := jobs.LockReview(tx, req.Envelope.TaskId)
		if err != nil {
			return err
		}
		e := req.Envelope
		if j.Kind != "review" || j.RunID != e.RunId || r.PipelineVersion != "review-v1" || r.HeadSHA != e.HeadSha || r.InstallationID != int64(e.Tenant.InstallationId) || r.RepositoryID != int64(e.Tenant.RepositoryId) {
			return fault.New(409, "TASK_IDENTITY_MISMATCH")
		}
		answer.Outcome = "terminal"
		if e.NotificationGeneration != j.NotificationGeneration {
			return nil
		}
		if !authorized || j.Status == "cancelled" || r.Status == "Stale" || r.Status == "Cancelled" {
			answer.Outcome = "cancelled"
			return nil
		}
		if j.Status == "completed" || j.Status == "result_ready" {
			answer.Outcome = "accepted"
			return nil
		}
		if r.NeedsReconciliation {
			answer.Outcome = "blocked"
			return nil
		}
		if j.Status == "failed" {
			answer.Outcome = "terminal"
			return nil
		}
		snapshot, err := jobs.ReviewSnapshot(r)
		if err != nil {
			return err
		}
		var now time.Time
		if err = tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
		if j.Status == "running" && j.LeaseExpiresAt != nil && j.LeaseExpiresAt.After(now) {
			answer.Outcome = "busy"
			return nil
		}
		var gaps int64
		if err = tx.Raw("SELECT count(*) FROM model_calls c WHERE run_id=? AND (state IN('dispatching','uncertain') OR(state='settled' AND outcome='succeeded' AND NOT EXISTS(SELECT 1 FROM review_stage_artifacts a WHERE a.call_id=c.call_id AND payload IS NOT NULL AND expires_at>clock_timestamp())))", r.ID).Scan(&gaps).Error; err != nil {
			return err
		}
		if gaps > 0 {
			answer.Outcome = "blocked"
			if err = tx.Exec("UPDATE model_calls SET state='uncertain',reason_code='MODEL_EXECUTION_LOST',revision=revision+1 WHERE run_id=? AND state='dispatching'", r.ID).Error; err != nil {
				return err
			}
			return tx.Exec("UPDATE review_runs SET needs_reconciliation=true,reason_code='MODEL_FACTS_INCOMPLETE',updated_at=clock_timestamp() WHERE id=?", r.ID).Error
		}
		if j.AvailableAt.After(now) {
			answer.Outcome = "busy"
			return nil
		}
		if j.Attempts >= j.MaxAttempts {
			answer.Outcome = "terminal"
			if err = tx.Exec("UPDATE jobs SET status='failed',lease_token=NULL,lease_expires_at=NULL WHERE id=?", j.ID).Error; err != nil {
				return err
			}
			return tx.Exec("UPDATE review_runs SET status='Failed',reason_code='REVIEW_ATTEMPTS_EXHAUSTED',updated_at=clock_timestamp() WHERE id=?", r.ID).Error
		}
		token := jobs.UUID()
		expiry := now.Add(90 * time.Second)
		if err = tx.Exec("UPDATE jobs SET status='running',attempts=attempts+1,lease_token=?,lease_expires_at=?,worker_id=?,execution_epoch=? WHERE id=?", token, expiry, req.WorkerId, runtime.Epoch, j.ID).Error; err != nil {
			return err
		}
		if err = tx.Exec("UPDATE review_runs SET status='Indexing',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; err != nil {
			return err
		}
		answer.Outcome = "claimed"
		answer.Lease = &contracts.ReviewLease{TaskId: j.ID, RunId: r.ID, Tenant: e.Tenant, HeadSha: r.HeadSHA, InputHash: jobs.Hash(r.Snapshot), LeaseToken: token, LeaseExpiresAt: expiry, ExecutionEpoch: runtime.Epoch, Attempt: j.Attempts + 1, Snapshot: snapshot}
		return nil
	})
	return answer, err
}
func (s *Service) Heartbeat(ctx context.Context, lease contracts.ReviewLease) error {
	return s.transaction(ctx, func(tx *gorm.DB) error {
		if _, err := jobs.CheckReviewLease(tx, lease); err != nil {
			return err
		}
		return tx.Exec("UPDATE jobs SET lease_expires_at=clock_timestamp()+interval '90 seconds' WHERE id=?", lease.TaskId).Error
	})
}
func (s *Service) Block(ctx context.Context, req contracts.ReviewTaskActionRequest) error {
	return s.action(ctx, req, true)
}
func (s *Service) Fail(ctx context.Context, req contracts.ReviewTaskActionRequest) error {
	return s.action(ctx, req, false)
}
func (s *Service) action(ctx context.Context, req contracts.ReviewTaskActionRequest, blocked bool) error {
	if !regexp.MustCompile(`^[A-Z_]{1,64}$`).MatchString(req.ReasonCode) {
		return fault.New(400, "INVALID_REASON_CODE")
	}
	return s.transaction(ctx, func(tx *gorm.DB) error {
		r, err := jobs.CheckReviewLeaseFacts(tx, req.Lease)
		if err != nil {
			return err
		}
		if blocked {
			if r.NeedsReconciliation {
				return nil
			}
			return tx.Exec("UPDATE review_runs SET needs_reconciliation=true,reason_code=?,updated_at=clock_timestamp() WHERE id=?", req.ReasonCode, r.ID).Error
		}
		if r.NeedsReconciliation {
			return fault.New(409, "REVIEW_BLOCKED")
		}
		if err = tx.Exec("UPDATE review_runs SET status='Failed',reason_code=?,updated_at=clock_timestamp() WHERE id=?", req.ReasonCode, r.ID).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE jobs SET status='failed',lease_token=NULL,lease_expires_at=NULL,last_error_code=? WHERE id=?", req.ReasonCode, req.Lease.TaskId).Error
	})
}
