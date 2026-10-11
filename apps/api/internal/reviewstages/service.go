package reviewstages

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/jobs"
	"time"
)

type Service struct{ DB *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{db} }

type Record struct {
	RunID          string
	Stage          string
	StageInputHash string
	OutputHash     string
	CallID         *string
	Payload        json.RawMessage
	ExpiresAt      time.Time
}

func (Record) TableName() string { return "review_stage_artifacts" }

func StageInputHash(tx *gorm.DB, l contracts.ReviewLease, stage string) (string, error) {
	var rows []struct{ ContextHash string }
	if err := tx.Raw("SELECT context_hash FROM review_context_manifests WHERE run_id=? AND input_hash=? AND sealed AND expires_at>clock_timestamp()", l.RunId, l.InputHash).Scan(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", fault.New(409, "CONTEXT_NOT_SEALED")
	}
	prompt := "none"
	if stage == "candidates" {
		prompt = string(l.Snapshot.GenerationPromptVersion)
	} else if stage == "verification" {
		prompt = string(l.Snapshot.VerificationPromptVersion)
	} else if stage != "context" {
		return "", fault.New(400, "INVALID_REVIEW_STAGE")
	}
	return jobs.Hash([]byte(l.InputHash + "\n" + rows[0].ContextHash + "\n" + stage + "\n" + prompt + "\n" + l.Snapshot.ModelConfigVersion)), nil
}
func (s *Service) transaction(ctx context.Context, l contracts.ReviewLease, fn func(*gorm.DB) error) error {
	if s.DB == nil {
		return fault.New(503, "DATABASE_UNAVAILABLE")
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := jobs.CheckReviewLease(tx, l); err != nil {
			return err
		}
		return fn(tx)
	})
}
func (s *Service) Read(ctx context.Context, req contracts.ReadReviewStageRequest) (contracts.ReadReviewStageResponse, error) {
	answer := contracts.ReadReviewStageResponse{}
	err := s.transaction(ctx, req.Lease, func(tx *gorm.DB) error {
		inputHash, err := StageInputHash(tx, req.Lease, string(req.Stage))
		if err != nil {
			return err
		}
		if inputHash != req.StageInputHash {
			return fault.New(409, "STAGE_INPUT_MISMATCH")
		}
		var records []Record
		if err = tx.Where("run_id=? AND stage=? AND stage_input_hash=? AND expires_at>clock_timestamp() AND payload IS NOT NULL", req.Lease.RunId, string(req.Stage), inputHash).Find(&records).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		record := records[0]
		var payload contracts.ReviewStagePayload
		if contracts.DecodeReview("ReviewStagePayload", record.Payload, &payload, req.Lease.Snapshot.Policy.MaxStageBytes) != nil {
			return fault.New(503, "STAGE_CONTENT_INVALID")
		}
		answer.Found = true
		answer.Record = &contracts.ReviewStageRecord{Stage: contracts.ReviewStageRecordStage(record.Stage), StageInputHash: inputHash, OutputHash: record.OutputHash, CallId: record.CallID, Payload: payload, ExpiresAt: record.ExpiresAt}
		return nil
	})
	return answer, err
}
func (s *Service) Save(ctx context.Context, req contracts.SaveReviewStageRequest) error {
	body, err := json.Marshal(req.Payload)
	if err != nil || len(body) > req.Lease.Snapshot.Policy.MaxStageBytes {
		return fault.New(400, "STAGE_TOO_LARGE")
	}
	var payload contracts.ReviewStagePayload
	if contracts.DecodeReview("ReviewStagePayload", contracts.RedactReviewJSON(body), &payload, req.Lease.Snapshot.Policy.MaxStageBytes) != nil {
		return fault.New(400, "STAGE_CONTENT_INVALID")
	}
	body, _ = json.Marshal(payload)
	if (req.Stage == "context" && (payload.Context == nil || payload.Candidates != nil || payload.Verification != nil || req.CallId != nil)) ||
		(req.Stage == "candidates" && (payload.Candidates == nil || payload.Context != nil || payload.Verification != nil || req.CallId == nil)) ||
		(req.Stage == "verification" && (payload.Verification == nil || payload.Context != nil || payload.Candidates != nil || req.CallId == nil)) {
		return fault.New(400, "STAGE_KIND_MISMATCH")
	}
	return s.transaction(ctx, req.Lease, func(tx *gorm.DB) error {
		inputHash, err := StageInputHash(tx, req.Lease, string(req.Stage))
		if err != nil {
			return err
		}
		if inputHash != req.StageInputHash {
			return fault.New(409, "STAGE_INPUT_MISMATCH")
		}
		outputHash := jobs.Hash(body)
		var existing []Record
		if err = tx.Where("run_id=? AND stage=? AND stage_input_hash=?", req.Lease.RunId, string(req.Stage), inputHash).Find(&existing).Error; err != nil {
			return err
		}
		if len(existing) > 0 {
			if existing[0].OutputHash != outputHash || !sameCall(existing[0].CallID, req.CallId) {
				return fault.New(409, "STAGE_CONTENT_CONFLICT")
			}
			return nil
		}
		if req.CallId != nil {
			purpose := "generate"
			if req.Stage == "verification" {
				purpose = "verify"
			}
			var count int64
			if err = tx.Raw("SELECT count(*) FROM model_calls WHERE call_id=? AND run_id=? AND purpose=? AND state='settled' AND outcome='succeeded'", *req.CallId, req.Lease.RunId, purpose).Scan(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return fault.New(409, "STAGE_CALL_NOT_SETTLED")
			}
		} else if payload.Context != nil {
			var contextHash string
			if err = tx.Raw("SELECT context_hash FROM review_context_manifests WHERE run_id=?", req.Lease.RunId).Scan(&contextHash).Error; err != nil {
				return err
			}
			if !payload.Context.Complete || payload.Context.ContextHash == nil || *payload.Context.ContextHash != contextHash {
				return fault.New(409, "CONTEXT_NOT_SEALED")
			}
		}
		// Retention-disabled Runs preserve recovery metadata but cannot recover content.
		var content any = string(body)
		if !req.Lease.Snapshot.Policy.RetainContent {
			content = nil
		}
		return tx.Exec("INSERT INTO review_stage_artifacts(run_id,stage,stage_input_hash,output_hash,call_id,payload,expires_at) VALUES(?,?,?,?,?,?::jsonb,?)", req.Lease.RunId, string(req.Stage), inputHash, outputHash, req.CallId, content, req.Lease.Snapshot.Provenance.ContentExpiresAt).Error
	})
}
func sameCall(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
