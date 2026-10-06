package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/persistence"
)

type Service struct{ DB *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{DB: db} }
func UUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("RANDOM_UNAVAILABLE")
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func Hash(value []byte) string { h := sha256.Sum256(value); return hex.EncodeToString(h[:]) }
func ResultHash(taskID, runID, inputHash string) string {
	return Hash([]byte(taskID + "\n" + runID + "\n" + inputHash))
}
func conflict(code string) error { return &fault.Error{Status: 409, Code: code} }

var uuidPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var shaPattern = regexp.MustCompile(`^[a-f0-9]{40,64}$`)

func ValidEnvelope(e contracts.TaskEnvelope) bool {
	return e.SchemaVersion == 1 && uuidPattern.MatchString(e.MessageId) && uuidPattern.MatchString(e.TaskId) && uuidPattern.MatchString(e.RunId) && e.Tenant.InstallationId > 0 && e.Tenant.InstallationId <= 9007199254740991 && e.Tenant.RepositoryId > 0 && e.Tenant.RepositoryId <= 9007199254740991 && shaPattern.MatchString(e.HeadSha) && e.NotificationGeneration >= 0 && e.Kind.Valid()
}
func control(tx *gorm.DB) (persistence.RuntimeControl, error) {
	var c persistence.RuntimeControl
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&c, 1).Error
	if err != nil {
		return c, err
	}
	if c.Mode != "go" {
		return c, &fault.Error{Status: 503, Code: "RUNTIME_PAUSED"}
	}
	return c, nil
}

// All authority changes and execution use installation -> repository -> job -> run.
func locked(tx *gorm.DB, taskID string) (persistence.Job, persistence.Run, bool, error) {
	var initial persistence.Job
	var j persistence.Job
	var r persistence.Run
	if err := tx.First(&initial, "id = ?", taskID).Error; err != nil {
		return j, r, false, conflict("TASK_UNKNOWN")
	}
	if err := tx.First(&r, "id = ?", initial.RunID).Error; err != nil {
		return j, r, false, err
	}
	var installation persistence.Installation
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&installation, "id = ?", r.InstallationID).Error; err != nil {
		return j, r, false, err
	}
	var repository persistence.Repository
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&repository, "installation_id = ? AND id = ?", r.InstallationID, r.RepositoryID).Error; err != nil {
		return j, r, false, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&j, "id = ?", taskID).Error; err != nil {
		return j, r, false, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, "id = ?", j.RunID).Error; err != nil {
		return j, r, false, err
	}
	return j, r, installation.Active && repository.Public && repository.Enabled, nil
}
func snapshot(r persistence.Run) (contracts.RunSnapshot, error) {
	var s contracts.RunSnapshot
	if err := json.Unmarshal(r.Snapshot, &s); err != nil {
		return s, conflict("SNAPSHOT_INVALID")
	}
	i := s.Identity
	if i.Tenant.InstallationId != int(r.InstallationID) || i.Tenant.RepositoryId != int(r.RepositoryID) || i.HeadSha != r.HeadSHA || i.BaseSha != r.BaseSHA || i.PrNumber != r.PrNumber || i.Generation != r.Generation || i.PipelineVersion != r.PipelineVersion || s.ModelConfigVersion != "none" {
		return s, conflict("SNAPSHOT_INVALID")
	}
	return s, nil
}
func dbNow(tx *gorm.DB) (time.Time, error) {
	var now time.Time
	err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now, err
}
func (s *Service) transaction(ctx context.Context, fn func(*gorm.DB) error) error {
	if s.DB == nil {
		return &fault.Error{Status: 503, Code: "DATABASE_UNAVAILABLE"}
	}
	return s.DB.WithContext(ctx).Transaction(fn)
}
func (s *Service) Claim(ctx context.Context, e contracts.TaskEnvelope, workerID string) (contracts.ClaimResponse, error) {
	answer := contracts.ClaimResponse{}
	if !ValidEnvelope(e) || e.Kind != contracts.TaskEnvelopeKindConnectionCheck || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`).MatchString(workerID) {
		return answer, &fault.Error{Status: 400, Code: "INVALID_TASK"}
	}
	err := s.transaction(ctx, func(tx *gorm.DB) error {
		runtime, err := control(tx)
		if err != nil {
			return err
		}
		j, r, authorized, err := locked(tx, e.TaskId)
		if err != nil {
			return err
		}
		if j.Kind != "review" || j.RunID != e.RunId || r.HeadSHA != e.HeadSha || r.InstallationID != int64(e.Tenant.InstallationId) || r.RepositoryID != int64(e.Tenant.RepositoryId) {
			return conflict("TASK_IDENTITY_MISMATCH")
		}
		answer.Outcome = contracts.ClaimResponseOutcomeSuperseded
		if j.NotificationGeneration != e.NotificationGeneration {
			return nil
		}
		if j.Status == "completed" || j.Status == "result_ready" {
			answer.Outcome = contracts.ClaimResponseOutcomeFinished
			return nil
		}
		if j.Status == "cancelled" || !authorized {
			answer.Outcome = contracts.ClaimResponseOutcomeCancelled
			return nil
		}
		if j.Status == "failed" {
			answer.Outcome = contracts.ClaimResponseOutcomeExhausted
			return nil
		}
		snap, err := snapshot(r)
		if err != nil {
			return err
		}
		var current struct{ HeadSHA string }
		if err = tx.Raw("SELECT head_sha FROM pull_requests WHERE installation_id=? AND repository_id=? AND number=?", r.InstallationID, r.RepositoryID, r.PrNumber).Scan(&current).Error; err != nil {
			return err
		}
		if current.HeadSHA != r.HeadSHA {
			if err = tx.Exec("UPDATE jobs SET status='cancelled',lease_token=NULL,lease_expires_at=NULL WHERE id=?", j.ID).Error; err != nil {
				return err
			}
			if err = tx.Exec("UPDATE review_runs SET status='Stale',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; err != nil {
				return err
			}
			return nil
		}
		now, err := dbNow(tx)
		if err != nil {
			return err
		}
		if j.Status == "running" && j.LeaseExpiresAt != nil && j.LeaseExpiresAt.After(now) {
			answer.Outcome = contracts.ClaimResponseOutcomeBusy
			return nil
		}
		if j.AvailableAt.After(now) {
			answer.Outcome = contracts.ClaimResponseOutcomeNotReady
			return Schedule(tx, j, r, j.AvailableAt)
		}
		if j.Attempts >= j.MaxAttempts {
			answer.Outcome = contracts.ClaimResponseOutcomeExhausted
			if err = tx.Exec("UPDATE jobs SET status='failed',lease_token=NULL,lease_expires_at=NULL WHERE id=?", j.ID).Error; err != nil {
				return err
			}
			return tx.Exec("UPDATE review_runs SET status='Failed',updated_at=clock_timestamp() WHERE id=?", r.ID).Error
		}
		token := UUID()
		expires := now.Add(90 * time.Second)
		if err = tx.Exec("UPDATE jobs SET status='running',attempts=attempts+1,lease_token=?,lease_expires_at=?,worker_id=?,execution_epoch=?,last_error_code=NULL WHERE id=?", token, expires, workerID, runtime.Epoch, j.ID).Error; err != nil {
			return err
		}
		if err = tx.Exec("UPDATE review_runs SET status='Reviewing',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; err != nil {
			return err
		}
		answer.Outcome = contracts.ClaimResponseOutcomeClaimed
		answer.Lease = &contracts.TaskLease{TaskId: j.ID, RunId: r.ID, LeaseToken: token, ExecutionEpoch: runtime.Epoch, Attempt: j.Attempts + 1, ExpiresAt: expires, Snapshot: snap, InputHash: Hash(r.Snapshot)}
		return nil
	})
	return answer, err
}
func leaseValid(j persistence.Job, r persistence.Run, l contracts.TaskLease, epoch int, now time.Time) bool {
	return j.Kind == "review" && j.Status == "running" && j.RunID == l.RunId && j.ID == l.TaskId && j.LeaseToken != nil && *j.LeaseToken == l.LeaseToken && j.LeaseExpiresAt != nil && j.LeaseExpiresAt.After(now) && j.ExecutionEpoch == epoch && l.ExecutionEpoch == epoch && Hash(r.Snapshot) == l.InputHash
}
func (s *Service) Heartbeat(ctx context.Context, l contracts.TaskLease) error {
	return s.transaction(ctx, func(tx *gorm.DB) error {
		runtime, err := control(tx)
		if err != nil {
			return err
		}
		j, r, authorized, err := locked(tx, l.TaskId)
		if err != nil {
			return err
		}
		now, err := dbNow(tx)
		if err != nil {
			return err
		}
		if !authorized || !leaseValid(j, r, l, runtime.Epoch, now) {
			return conflict("LEASE_LOST")
		}
		return tx.Exec("UPDATE jobs SET lease_expires_at=clock_timestamp()+interval '90 seconds' WHERE id=?", j.ID).Error
	})
}
func (s *Service) CommitConnection(ctx context.Context, result contracts.ConnectionResult) (contracts.CommitResponse, error) {
	answer := contracts.CommitResponse{}
	err := s.transaction(ctx, func(tx *gorm.DB) error {
		// Acquire the epoch fence first, even when confirming an already accepted result.
		var runtime persistence.RuntimeControl
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&runtime, 1).Error; err != nil {
			return err
		}
		// An identical persisted result may be confirmed after lease expiry; it never writes.
		j, r, authorized, err := locked(tx, result.TaskId)
		if err != nil {
			return err
		}
		if j.Kind != "review" || r.ID != result.RunId || result.Kind != contracts.ConnectionResultKindConnectionCheck || result.ResultId != j.ID+":connection" || result.InputHash != Hash(r.Snapshot) || result.ResultHash != ResultHash(j.ID, r.ID, result.InputHash) {
			return conflict("RESULT_IDENTITY_MISMATCH")
		}
		var existing []struct{ ResultHash string }
		if err = tx.Raw("SELECT result_hash FROM job_results WHERE task_id=?", j.ID).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) > 0 {
			if existing[0].ResultHash != result.ResultHash {
				return conflict("RESULT_CONFLICT")
			}
			answer.Outcome = contracts.AlreadyAccepted
			return nil
		}
		if runtime.Mode != "go" {
			return conflict("LEASE_LOST")
		}
		now, err := dbNow(tx)
		if err != nil {
			return err
		}
		l := contracts.TaskLease{TaskId: result.TaskId, RunId: result.RunId, LeaseToken: result.LeaseToken, ExecutionEpoch: result.ExecutionEpoch, InputHash: result.InputHash}
		if !authorized || !leaseValid(j, r, l, runtime.Epoch, now) {
			return conflict("LEASE_LOST")
		}
		if _, err = snapshot(r); err != nil {
			return err
		}
		body, _ := json.Marshal(result)
		if err = tx.Exec("INSERT INTO job_results(task_id,result_id,result_hash,input_hash,payload) VALUES(?,?,?,?,?::jsonb)", j.ID, result.ResultId, result.ResultHash, result.InputHash, string(body)).Error; err != nil {
			return err
		}
		if err = tx.Exec("UPDATE jobs SET status='result_ready',lease_token=NULL,lease_expires_at=NULL WHERE id=?", j.ID).Error; err != nil {
			return err
		}
		if err = tx.Exec("UPDATE review_runs SET status='Publishing',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; err != nil {
			return err
		}
		if err = tx.Exec("INSERT INTO publication_requests(run_id) VALUES(?) ON CONFLICT DO NOTHING", r.ID).Error; err != nil {
			return err
		}
		answer.Outcome = contracts.Accepted
		return nil
	})
	return answer, err
}
func (s *Service) ReadResult(ctx context.Context, taskID string) (contracts.CommitResponse, error) {
	answer := contracts.CommitResponse{}
	if s.DB == nil {
		return answer, &fault.Error{Status: 503, Code: "DATABASE_UNAVAILABLE"}
	}
	var count int64
	if err := s.DB.WithContext(ctx).Raw("SELECT count(*) FROM job_results x JOIN jobs j ON j.id=x.task_id WHERE j.id=? AND j.kind='review'", taskID).Scan(&count).Error; err != nil {
		return answer, err
	}
	if count == 0 {
		return answer, &fault.Error{Status: 404, Code: "RESULT_NOT_FOUND"}
	}
	answer.Outcome = contracts.AlreadyAccepted
	return answer, nil
}
func (s *Service) ConfirmResult(ctx context.Context, taskID, runID, inputHash, resultHash string) (contracts.CommitResponse, error) {
	answer, err := s.ReadResult(ctx, taskID)
	if err != nil {
		return answer, err
	}
	var count int64
	err = s.DB.WithContext(ctx).Raw("SELECT count(*) FROM job_results x JOIN jobs j ON j.id=x.task_id WHERE x.task_id=? AND j.run_id=? AND x.input_hash=? AND x.result_hash=?", taskID, runID, inputHash, resultHash).Scan(&count).Error
	if err != nil {
		return contracts.CommitResponse{}, err
	}
	if count != 1 {
		return contracts.CommitResponse{}, conflict("RESULT_CONFLICT")
	}
	return answer, nil
}
func (s *Service) Fail(ctx context.Context, l contracts.TaskLease, code string) error {
	if code != "CONNECTION_JOB_FAILED" {
		return &fault.Error{Status: 400, Code: "INVALID_FAILURE_CODE"}
	}
	return s.transaction(ctx, func(tx *gorm.DB) error {
		runtime, err := control(tx)
		if err != nil {
			return err
		}
		j, r, authorized, err := locked(tx, l.TaskId)
		if err != nil {
			return err
		}
		now, err := dbNow(tx)
		if err != nil {
			return err
		}
		if !authorized || !leaseValid(j, r, l, runtime.Epoch, now) {
			return conflict("LEASE_LOST")
		}
		if j.Attempts >= j.MaxAttempts {
			if err = tx.Exec("UPDATE jobs SET status='failed',lease_token=NULL,lease_expires_at=NULL,last_error_code=? WHERE id=?", code, j.ID).Error; err != nil {
				return err
			}
			return tx.Exec("UPDATE review_runs SET status='Failed',updated_at=clock_timestamp() WHERE id=?", r.ID).Error
		}
		n := j.Attempts - 1
		if n < 0 {
			n = 0
		}
		if n > 3 {
			n = 3
		}
		available := now.Add(time.Duration(30*(1<<n)) * time.Second)
		if err = tx.Exec("UPDATE jobs SET status='queued',lease_token=NULL,lease_expires_at=NULL,last_error_code=?,available_at=? WHERE id=?", code, available, j.ID).Error; err != nil {
			return err
		}
		if err = tx.Exec("UPDATE review_runs SET status='Queued',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; err != nil {
			return err
		}
		return Schedule(tx, j, r, available)
	})
}
