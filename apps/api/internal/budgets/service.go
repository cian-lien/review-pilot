package budgets

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/big"
	"regexp"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/jobs"
	"strconv"
	"time"
)

type Service struct{ DB *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{db} }

type Call struct {
	CallID             string `gorm:"column:call_id;primaryKey"`
	ReservationID      string
	RunID              string
	Purpose            string
	Ordinal            int
	RequestHash        string
	State              string
	ModelConfigVersion string
	PriceVersion       string
	Currency           string
	InputPrice         int64
	OutputPrice        int64
	InputLimit         int
	OutputLimit        int
	ReservedCostMicros int64
	ActualCostMicros   *int64
	Usage              json.RawMessage `gorm:"type:jsonb"`
	ResponseHash       *string
	Outcome            *string
	ReasonCode         *string
	Revision           int
	RetryAuthorized    bool
}

func (Call) TableName() string { return "model_calls" }
func (c Call) DTO() contracts.ModelCall {
	var usage *contracts.ModelUsage
	if len(c.Usage) > 0 && string(c.Usage) != "null" {
		usage = new(contracts.ModelUsage)
		_ = json.Unmarshal(c.Usage, usage)
	}
	var actual *string
	if c.ActualCostMicros != nil {
		v := strconv.FormatInt(*c.ActualCostMicros, 10)
		actual = &v
	}
	return contracts.ModelCall{CallId: c.CallID, ReservationId: c.ReservationID, Purpose: contracts.ModelCallPurpose(c.Purpose), Ordinal: c.Ordinal, RequestHash: c.RequestHash,
		State: contracts.ModelCallState(c.State), PriceVersion: c.PriceVersion, Currency: c.Currency, ReservedCostMicros: strconv.FormatInt(c.ReservedCostMicros, 10), ActualCostMicros: actual, Usage: usage, Revision: c.Revision, ReasonCode: c.ReasonCode}
}

type scope struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Limit int64  `json:"limit"`
}

func Cost(input, output int, inputPrice, outputPrice int64) (int64, error) {
	if input < 0 || output < 0 || inputPrice <= 0 || outputPrice <= 0 {
		return 0, fault.New(400, "INVALID_MODEL_USAGE")
	}
	amount := new(big.Int).Mul(big.NewInt(int64(input)), big.NewInt(inputPrice))
	amount.Add(amount, new(big.Int).Mul(big.NewInt(int64(output)), big.NewInt(outputPrice)))
	amount.Add(amount, big.NewInt(999999))
	amount.Div(amount, big.NewInt(1000000))
	if !amount.IsInt64() {
		return 0, fault.New(400, "MODEL_COST_OVERFLOW")
	}
	return amount.Int64(), nil
}
func (s *Service) transaction(ctx context.Context, l contracts.ReviewLease, fn func(*gorm.DB) error) error {
	return s.runTransaction(ctx, l, false, fn)
}
func (s *Service) runTransaction(ctx context.Context, l contracts.ReviewLease, factsOnly bool, fn func(*gorm.DB) error) error {
	if s.DB == nil {
		return fault.New(503, "DATABASE_UNAVAILABLE")
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if factsOnly {
			_, err = jobs.CheckReviewLeaseFacts(tx, l)
		} else {
			_, err = jobs.CheckReviewLease(tx, l)
		}
		if err != nil {
			return err
		}
		// One database lock serializes budget mutations across all Runs and UTC scopes.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(782914631)").Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func block(tx *gorm.DB, runID, code string) error {
	return tx.Exec("UPDATE review_runs SET needs_reconciliation=true,reason_code=?,updated_at=clock_timestamp() WHERE id=?", code, runID).Error
}

func (s *Service) Reserve(ctx context.Context, req contracts.ReserveModelCallRequest) (contracts.ModelCall, error) {
	answer := contracts.ModelCall{}
	var blocked string
	if req.Ordinal < 1 || req.Ordinal > 2 || (req.Purpose != "generate" && req.Purpose != "verify") || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(req.RequestHash) ||
		req.InputTokenLimit <= 0 || req.InputTokenLimit > req.Lease.Snapshot.Policy.MaxInputTokens || req.OutputTokenLimit <= 0 || req.OutputTokenLimit > req.Lease.Snapshot.Policy.MaxOutputTokens {
		return answer, fault.New(400, "INVALID_MODEL_RESERVATION")
	}
	cfg, err := config.LoadReview()
	if err != nil || cfg.Mode != string(req.Lease.Snapshot.Provenance.ModelMode) || cfg.Model.ModelConfigVersion != req.Lease.Snapshot.ModelConfigVersion || cfg.Model.PriceVersion != req.Lease.Snapshot.PriceVersion {
		return answer, fault.New(409, "MODEL_CONFIG_MISMATCH")
	}
	m := cfg.Model
	modelBody, _ := json.Marshal(m)
	if req.ModelConfigHash != jobs.Hash(modelBody) {
		return answer, fault.New(409, "MODEL_CONFIG_MISMATCH")
	}
	inputPrice, _ := config.ParseMicros(m.InputMicrosPerMillion)
	outputPrice, _ := config.ParseMicros(m.OutputMicrosPerMillion)
	amount, err := Cost(req.InputTokenLimit, req.OutputTokenLimit, inputPrice, outputPrice)
	if err != nil || amount <= 0 {
		return answer, fault.New(400, "INVALID_MODEL_COST")
	}
	err = s.transaction(ctx, req.Lease, func(tx *gorm.DB) error {
		digest := jobs.Hash(modelBody)
		if err := tx.Exec("INSERT INTO review_model_configs(version,digest,payload) VALUES(?,?,?::jsonb) ON CONFLICT DO NOTHING", m.ModelConfigVersion, digest, string(modelBody)).Error; err != nil {
			return err
		}
		var storedDigest string
		if err := tx.Raw("SELECT digest FROM review_model_configs WHERE version=?", m.ModelConfigVersion).Scan(&storedDigest).Error; err != nil {
			return err
		}
		if storedDigest != digest {
			return fault.New(409, "MODEL_CONFIG_VERSION_CONFLICT")
		}
		var gap int64
		if err := tx.Raw("SELECT count(*) FROM model_calls c WHERE c.run_id=? AND (c.state IN('dispatching','uncertain') OR (c.state='settled' AND c.outcome='succeeded' AND NOT EXISTS(SELECT 1 FROM review_stage_artifacts a WHERE a.call_id=c.call_id)))", req.Lease.RunId).Scan(&gap).Error; err != nil {
			return err
		}
		if gap > 0 {
			blocked = "MODEL_FACTS_INCOMPLETE"
			return block(tx, req.Lease.RunId, blocked)
		}
		var existing Call
		if err := tx.First(&existing, "run_id=? AND purpose=? AND ordinal=?", req.Lease.RunId, string(req.Purpose), req.Ordinal).Error; err == nil {
			if existing.RequestHash != req.RequestHash || existing.InputLimit != req.InputTokenLimit || existing.OutputLimit != req.OutputTokenLimit {
				return fault.New(409, "MODEL_CALL_CONFLICT")
			}
			answer = existing.DTO()
			return nil
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		var count int64
		if err := tx.Model(&Call{}).Where("run_id=?", req.Lease.RunId).Count(&count).Error; err != nil {
			return err
		}
		if count >= 4 {
			return fault.New(409, "MODEL_CALL_LIMIT")
		}
		if req.Ordinal == 2 {
			var prior Call
			if err := tx.First(&prior, "run_id=? AND purpose=? AND ordinal=1", req.Lease.RunId, string(req.Purpose)).Error; err != nil {
				return fault.New(409, "MODEL_RETRY_NOT_ALLOWED")
			}
			retryable := prior.ReasonCode != nil && (*prior.ReasonCode == "MODEL_RATE_LIMITED" || *prior.ReasonCode == "MODEL_TRANSIENT_FAILURE")
			if !prior.RetryAuthorized && (!retryable || prior.Outcome == nil || *prior.Outcome != "failed" || (prior.State != "settled" && prior.State != "released")) {
				return fault.New(409, "MODEL_RETRY_NOT_ALLOWED")
			}
		}
		var now time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
		now = now.UTC()
		runLimit, _ := config.ParseMicros(m.RunLimitMicros)
		daily, _ := config.ParseMicros(m.RepositoryDailyLimitMicros)
		monthly, _ := config.ParseMicros(m.GlobalMonthlyLimitMicros)
		fixture := ""
		if cfg.Mode == "fake" {
			fixture = ":" + m.FixtureID
		}
		scopes := []scope{{"global", now.Format("2006-01") + fixture, monthly}, {"repository", fmt.Sprintf("%d:%d:%s%s", req.Lease.Tenant.InstallationId, req.Lease.Tenant.RepositoryId, now.Format("2006-01-02"), fixture), daily}, {"run", req.Lease.RunId, runLimit}}
		var otherCurrency int64
		if err := tx.Raw("SELECT count(*) FROM budget_limits WHERE scope_type='global' AND scope_key=? AND currency<>?", scopes[0].Key, m.Currency).Scan(&otherCurrency).Error; err != nil {
			return err
		}
		if otherCurrency != 0 {
			return fault.New(409, "BUDGET_CURRENCY_CONFLICT")
		}
		for _, key := range scopes {
			if err := tx.Exec("INSERT INTO budget_limits(scope_type,scope_key,currency,limit_micros) VALUES(?,?,?,?) ON CONFLICT DO NOTHING", key.Type, key.Key, m.Currency, key.Limit).Error; err != nil {
				return err
			}
			result := tx.Exec("UPDATE budget_limits SET held_micros=held_micros+? WHERE scope_type=? AND scope_key=? AND currency=? AND limit_micros-spent_micros-held_micros>=?", amount, key.Type, key.Key, m.Currency, amount)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fault.New(409, "REVIEW_BUDGET_EXHAUSTED")
			}
		}
		reservationID, callID := jobs.UUID(), jobs.UUID()
		scopeBody, _ := json.Marshal(scopes)
		if err := tx.Exec("INSERT INTO budget_reservations(id,run_id,installation_id,repository_id,currency,reserved_micros,scopes) VALUES(?,?,?,?,?,?,?::jsonb)", reservationID, req.Lease.RunId, req.Lease.Tenant.InstallationId, req.Lease.Tenant.RepositoryId, m.Currency, amount, string(scopeBody)).Error; err != nil {
			return err
		}
		call := Call{CallID: callID, ReservationID: reservationID, RunID: req.Lease.RunId, Purpose: string(req.Purpose), Ordinal: req.Ordinal, RequestHash: req.RequestHash, State: "reserved", ModelConfigVersion: m.ModelConfigVersion, PriceVersion: m.PriceVersion, Currency: m.Currency,
			InputPrice: inputPrice, OutputPrice: outputPrice, InputLimit: req.InputTokenLimit, OutputLimit: req.OutputTokenLimit, ReservedCostMicros: amount}
		if err := tx.Create(&call).Error; err != nil {
			return err
		}
		answer = call.DTO()
		return nil
	})
	if err == nil && blocked != "" {
		err = fault.New(409, blocked)
	}
	return answer, err
}

func (s *Service) Dispatch(ctx context.Context, req contracts.DispatchModelCallRequest) (contracts.ModelCall, error) {
	answer := contracts.ModelCall{}
	err := s.transaction(ctx, req.Lease, func(tx *gorm.DB) error {
		var call Call
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&call, "call_id=? AND run_id=?", req.CallId, req.Lease.RunId).Error; err != nil {
			return fault.New(409, "MODEL_CALL_UNKNOWN")
		}
		if call.State != "reserved" || call.Revision != req.ExpectedRevision {
			answer = call.DTO()
			return nil
		}
		call.State = "dispatching"
		call.Revision++
		if err := tx.Save(&call).Error; err != nil {
			return err
		}
		answer = call.DTO()
		answer.SendAuthorized = true
		return nil
	})
	return answer, err
}

// Account replaces held cost only after an authoritative settlement; unknown facts never enter here.
func Account(tx *gorm.DB, call Call, amount int64, released bool) error {
	var body []struct {
		Scopes json.RawMessage
		State  string
	}
	if err := tx.Raw("SELECT scopes,state FROM budget_reservations WHERE id=? FOR UPDATE", call.ReservationID).Scan(&body).Error; err != nil {
		return err
	}
	if len(body) != 1 || body[0].State != "held" {
		return fault.New(409, "RESERVATION_STATE_CONFLICT")
	}
	var scopes []scope
	if json.Unmarshal(body[0].Scopes, &scopes) != nil {
		return fault.New(503, "BUDGET_FACTS_INVALID")
	}
	for _, key := range scopes {
		if err := tx.Exec("UPDATE budget_limits SET held_micros=held_micros-?,spent_micros=spent_micros+? WHERE scope_type=? AND scope_key=? AND currency=?", call.ReservedCostMicros, amount, key.Type, key.Key, call.Currency).Error; err != nil {
			return err
		}
	}
	state := "settled"
	if released {
		state = "released"
	}
	return tx.Exec("UPDATE budget_reservations SET state=?,actual_micros=? WHERE id=?", state, amount, call.ReservationID).Error
}

func (s *Service) Settle(ctx context.Context, req contracts.SettleModelCallRequest) (contracts.ModelCall, error) {
	answer := contracts.ModelCall{}
	err := s.runTransaction(ctx, req.Lease, true, func(tx *gorm.DB) error {
		var call Call
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&call, "call_id=? AND run_id=?", req.CallId, req.Lease.RunId).Error; err != nil {
			return fault.New(409, "MODEL_CALL_UNKNOWN")
		}
		if call.State == "settled" || call.State == "released" || call.State == "uncertain" {
			facts := call.DTO()
			if call.Outcome == nil || *call.Outcome != string(req.Outcome) || !sameUsage(facts.Usage, req.Usage) || !sameString(call.ResponseHash, req.SafeResponseHash) || !sameString(call.ReasonCode, req.ReasonCode) {
				return fault.New(409, "MODEL_SETTLEMENT_CONFLICT")
			}
			answer = facts
			return nil
		}
		var blocked bool
		if err := tx.Raw("SELECT needs_reconciliation FROM review_runs WHERE id=?", req.Lease.RunId).Scan(&blocked).Error; err != nil {
			return err
		}
		if blocked {
			return fault.New(409, "REVIEW_BLOCKED")
		}
		if req.ExpectedRevision != call.Revision {
			return fault.New(409, "MODEL_CALL_REVISION_CONFLICT")
		}
		unknown := req.Outcome == "uncertain" || (req.Outcome != "not-sent" && req.Usage == nil)
		if req.Outcome == "not-sent" && call.State != "reserved" {
			return fault.New(409, "MODEL_SEND_FACT_UNKNOWN")
		}
		if req.Outcome != "not-sent" && call.State != "dispatching" {
			return fault.New(409, "MODEL_CALL_NOT_DISPATCHED")
		}
		outcome := string(req.Outcome)
		call.Outcome = &outcome
		call.Revision++
		call.ResponseHash = req.SafeResponseHash
		call.ReasonCode = req.ReasonCode
		call.Usage, _ = json.Marshal(req.Usage)
		if unknown {
			call.State = "uncertain"
			if err := block(tx, call.RunID, "MODEL_USAGE_UNKNOWN"); err != nil {
				return err
			}
		} else {
			amount := int64(0)
			if req.Usage != nil {
				var err error
				amount, err = Cost(req.Usage.InputTokens, req.Usage.OutputTokens, call.InputPrice, call.OutputPrice)
				if err != nil {
					return err
				}
			}
			call.ActualCostMicros = &amount
			call.State = "settled"
			if req.Outcome == "not-sent" {
				call.State = "released"
			}
			if err := Account(tx, call, amount, req.Outcome == "not-sent"); err != nil {
				return err
			}
			if amount > call.ReservedCostMicros || (req.Usage != nil && (req.Usage.InputTokens > call.InputLimit || req.Usage.OutputTokens > call.OutputLimit)) {
				if err := block(tx, call.RunID, "MODEL_BUDGET_OVERRUN"); err != nil {
					return err
				}
			}
		}
		if err := tx.Save(&call).Error; err != nil {
			return err
		}
		answer = call.DTO()
		return nil
	})
	return answer, err
}

func sameUsage(a, b *contracts.ModelUsage) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
func sameString(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
