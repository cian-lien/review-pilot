package budgets

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"regexp"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/jobs"
)

type ReconcileRequest struct {
	CallID           string
	ExpectedRevision int
	Outcome          string
	ReceiptRef       string
	ActualCostMicros string
	Currency         string
	RetryStage       bool
}

// Reconcile is an operator-only database capability, never registered on the public API.
func (s *Service) Reconcile(ctx context.Context, req ReconcileRequest) error {
	if !regexp.MustCompile(`^[a-f0-9-]{36}$`).MatchString(req.CallID) || req.ExpectedRevision < 0 ||
		(req.Outcome != "charged" && req.Outcome != "not-sent") || !regexp.MustCompile(`^[A-Za-z0-9:/._-]{1,256}$`).MatchString(req.ReceiptRef) || contracts.RedactReview(req.ReceiptRef) != req.ReceiptRef {
		return fault.New(400, "INVALID_RECONCILIATION_RECEIPT")
	}
	amount := int64(0)
	if req.Outcome == "charged" {
		var err error
		amount, err = config.ParseMicros(req.ActualCostMicros)
		if err != nil || req.Currency == "" {
			return fault.New(400, "INVALID_RECONCILIATION_COST")
		}
	}
	if s.DB == nil {
		return fault.New(503, "DATABASE_UNAVAILABLE")
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var initial Call
		if err := tx.First(&initial, "call_id=?", req.CallID).Error; err != nil {
			return fault.New(404, "MODEL_CALL_UNKNOWN")
		}
		if initial.RunID != "" {
			var taskID string
			if err := tx.Raw("SELECT id FROM jobs WHERE run_id=?", initial.RunID).Scan(&taskID).Error; err != nil {
				return err
			}
			if _, _, _, err := jobs.LockReview(tx, taskID); err != nil {
				return err
			}
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(782914631)").Error; err != nil {
			return err
		}
		var call Call
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&call, "call_id=?", req.CallID).Error; err != nil {
			return err
		}
		if call.Revision != req.ExpectedRevision || (call.State != "dispatching" && call.State != "uncertain" && call.State != "settled") {
			return fault.New(409, "MODEL_CALL_REVISION_CONFLICT")
		}
		if req.RetryStage && (call.Ordinal >= 2 || call.RunID == "") {
			return fault.New(409, "MODEL_RETRY_LIMIT")
		}
		if req.Outcome == "charged" && req.Currency != call.Currency {
			return fault.New(409, "MODEL_CURRENCY_MISMATCH")
		}
		if call.State == "settled" {
			if req.Outcome != "charged" || call.ActualCostMicros == nil || *call.ActualCostMicros != amount {
				return fault.New(409, "MODEL_COST_ALREADY_SETTLED")
			}
		} else if err := Account(tx, call, amount, req.Outcome == "not-sent"); err != nil {
			return err
		}
		state := "settled"
		if req.Outcome == "not-sent" {
			state = "released"
		}
		reason := "OPERATOR_RECONCILED_STAGE_MISSING"
		if err := tx.Exec("UPDATE model_calls SET state=?,actual_cost_micros=?,outcome='failed',reason_code=?,retry_authorized=?,revision=revision+1 WHERE call_id=?", state, amount, reason, req.RetryStage, call.CallID).Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO operator_audits(id,action,call_id,expected_revision,outcome,receipt_ref,retry_stage) VALUES(?,'reconcile-model',?,?,?,?,?)", jobs.UUID(), call.CallID, req.ExpectedRevision, req.Outcome, req.ReceiptRef, req.RetryStage).Error; err != nil {
			return err
		}
		if call.RunID != "" {
			if !req.RetryStage {
				return block(tx, call.RunID, reason)
			}
			var remaining int64
			if err := tx.Raw("SELECT count(*) FROM model_calls WHERE run_id=? AND state IN('dispatching','uncertain')", call.RunID).Scan(&remaining).Error; err != nil {
				return err
			}
			if remaining > 0 {
				return block(tx, call.RunID, "MODEL_FACTS_INCOMPLETE")
			}
			if err := tx.Exec("UPDATE review_runs SET needs_reconciliation=false,reason_code=NULL,updated_at=clock_timestamp() WHERE id=? AND status NOT IN('Completed','Partial','Stale','Cancelled','Failed')", call.RunID).Error; err != nil {
				return err
			}
			return tx.Exec("UPDATE jobs SET status='queued',lease_token=NULL,lease_expires_at=NULL,available_at=clock_timestamp() WHERE run_id=? AND status='running'", call.RunID).Error
		}
		return nil
	})
}
