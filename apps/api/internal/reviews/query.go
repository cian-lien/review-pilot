package reviews

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"gorm.io/gorm"
	"math/big"
	"reviewpilot.local/api/internal/auth"
	"reviewpilot.local/api/internal/budgets"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
	"strings"
	"time"
)

type QueryService struct {
	DB   *gorm.DB
	Auth *auth.Service
}

func NewQuery(db *gorm.DB, service *auth.Service) *QueryService { return &QueryService{db, service} }
func (s *QueryService) authorize(ctx context.Context, session auth.Session, tenant contracts.TenantRef) error {
	if s.DB == nil || s.Auth == nil || s.Auth.Gateway == nil {
		return fault.New(503, "DATABASE_UNAVAILABLE")
	}
	if session.UserToken == "" {
		return fault.New(401, "SESSION_REQUIRED")
	}
	if _, e := s.Auth.Gateway.Access(ctx, session.UserToken, tenant, true); e != nil {
		return e
	}
	var count int64
	if e := s.DB.WithContext(ctx).Raw("SELECT count(*) FROM repositories r JOIN installations i ON i.id=r.installation_id WHERE r.installation_id=? AND r.id=? AND r.public AND i.active", tenant.InstallationId, tenant.RepositoryId).Scan(&count).Error; e != nil {
		return e
	}
	if count != 1 {
		return fault.New(403, "REPOSITORY_ACCESS_DENIED")
	}
	return nil
}
func expiredCandidate() contracts.ReviewCandidate {
	return contracts.ReviewCandidate{CandidateId: "expired", Category: "correctness", Severity: "low", Title: "内容已过期或未保留", FailureScenario: "内容不可用", Impact: "内容不可用", RootCauseStatement: contracts.RootCauseStatement{Trigger: "内容不可用", Consequence: "内容不可用"}, SuggestedVerification: "内容不可用", Evidence: []contracts.EvidenceRef{}, IntroducedByPR: true}
}
func (s *QueryService) Detail(ctx context.Context, session auth.Session, tenant contracts.TenantRef, prNumber int, runID string) (contracts.ReviewRunDetail, error) {
	out := contracts.ReviewRunDetail{Findings: []contracts.ReviewFinding{}, Filtered: []contracts.ReviewFilteredCandidate{}, Coverage: contracts.ReviewCoverage{Skipped: []contracts.ReviewSkippedFile{}, Limitations: []string{"CONTEXT_NOT_READY"}}, Publication: contracts.ReviewPublicationSummary{State: "pending"}}
	if e := s.authorize(ctx, session, tenant); e != nil {
		return out, e
	}
	if prNumber < 1 || !identifier.MatchString(runID) {
		return out, fault.New(400, "INVALID_REVIEW_ID")
	}
	// One repeatable snapshot prevents result, budget and publication panels disagreeing mid-read.
	e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run persistence.Run
		if e := tx.First(&run, "id=? AND installation_id=? AND repository_id=? AND pr_number=? AND pipeline_version='review-v1'", runID, tenant.InstallationId, tenant.RepositoryId, prNumber).Error; e == gorm.ErrRecordNotFound {
			return fault.New(404, "REVIEW_NOT_FOUND")
		} else if e != nil {
			return e
		}
		snapshot, e := jobs.ReviewSnapshot(run)
		if e != nil {
			return e
		}
		out.RunId = run.ID
		out.Status = contracts.RunState(run.Status)
		out.Snapshot = snapshot
		out.ReasonCode = run.ReasonCode
		out.NeedsReconciliation = run.NeedsReconciliation
		out.ContentExpired = !snapshot.Policy.RetainContent || !snapshot.Provenance.ContentExpiresAt.After(time.Now())
		var coverage struct{ Payload []byte }
		if e = tx.Raw("SELECT payload FROM review_coverage WHERE run_id=?", run.ID).Scan(&coverage).Error; e != nil {
			return e
		}
		if len(coverage.Payload) > 0 {
			if e = json.Unmarshal(coverage.Payload, &out.Coverage); e != nil {
				return e
			}
		} else {
			if e = tx.Raw("SELECT coverage AS payload FROM review_context_manifests WHERE run_id=?", run.ID).Scan(&coverage).Error; e != nil {
				return e
			}
			if len(coverage.Payload) > 0 {
				if e = json.Unmarshal(coverage.Payload, &out.Coverage); e != nil {
					return e
				}
			}
		}
		var findings []struct {
			ID, Fingerprint, PublicationStatus string
			PublicationReason                  *string
			Candidate, Evidence                []byte
			Live                               bool
		}
		if e = tx.Raw("SELECT id,fingerprint,publication_status,publication_reason,candidate,evidence,expires_at>clock_timestamp() AS live FROM review_findings WHERE run_id=? ORDER BY created_at,id LIMIT 11", run.ID).Scan(&findings).Error; e != nil {
			return e
		}
		if len(findings) > 10 {
			return fault.New(503, "FINDING_LIMIT_INVALID")
		}
		for _, f := range findings {
			item := contracts.ReviewFinding{FindingId: f.ID, Fingerprint: f.Fingerprint, PublicationStatus: contracts.ReviewFindingPublicationStatus(f.PublicationStatus), ReasonCode: f.PublicationReason, ContentExpired: out.ContentExpired || !f.Live || len(f.Candidate) == 0, Candidate: expiredCandidate(), Evidence: []contracts.CodeEvidence{}}
			if !item.ContentExpired {
				if e = json.Unmarshal(contracts.RedactReviewJSON(f.Candidate), &item.Candidate); e != nil {
					return e
				}
				if e = json.Unmarshal(contracts.RedactReviewJSON(f.Evidence), &item.Evidence); e != nil {
					return e
				}
			}
			out.Findings = append(out.Findings, item)
		}
		var filtered []struct {
			CandidateID string
			Title       *string
			ReasonCode  string
			Evidence    []byte
			Live        bool
		}
		if e = tx.Raw("SELECT candidate_id,title,reason_code,evidence,expires_at>clock_timestamp() AS live FROM review_filtered_candidates WHERE run_id=? ORDER BY candidate_id LIMIT 11", run.ID).Scan(&filtered).Error; e != nil {
			return e
		}
		if len(filtered) > 10 {
			return fault.New(503, "CANDIDATE_LIMIT_INVALID")
		}
		for _, f := range filtered {
			item := contracts.ReviewFilteredCandidate{CandidateId: f.CandidateID, Title: "内容已过期或未保留", ReasonCode: f.ReasonCode, Evidence: []contracts.EvidenceRef{}}
			if f.Live && !out.ContentExpired && f.Title != nil {
				item.Title = contracts.RedactReview(*f.Title)
				if len(f.Evidence) > 0 {
					if e = json.Unmarshal(f.Evidence, &item.Evidence); e != nil {
						return e
					}
				}
			}
			out.Filtered = append(out.Filtered, item)
		}
		currency := "NONE"
		if snapshot.Provenance.ModelMode == "fake" {
			currency = "TEST"
		}
		out.Costs = contracts.ReviewCostSummary{Currency: currency, PriceVersion: snapshot.PriceVersion, Calls: []contracts.ModelCall{}, KnownCostMicros: "0", HeldCostMicros: "0", DailyResetsAt: time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)}
		var calls []budgets.Call
		if e = tx.Where("run_id=?", run.ID).Order("purpose,ordinal").Limit(5).Find(&calls).Error; e != nil {
			return e
		}
		if len(calls) > 4 {
			return fault.New(503, "MODEL_CALL_LIMIT_INVALID")
		}
		known, held := new(big.Int), new(big.Int)
		for _, call := range calls {
			out.Costs.Calls = append(out.Costs.Calls, call.DTO())
			if out.Costs.Currency != "NONE" && out.Costs.Currency != call.Currency {
				return fault.New(503, "COST_CURRENCY_INVALID")
			}
			out.Costs.Currency = call.Currency
			if call.ActualCostMicros != nil {
				known.Add(known, big.NewInt(*call.ActualCostMicros))
			}
			if call.State == "reserved" || call.State == "dispatching" || call.State == "uncertain" {
				held.Add(held, big.NewInt(call.ReservedCostMicros))
			}
			if call.State == "uncertain" {
				out.Costs.NeedsReconciliation = true
			}
		}
		if !known.IsInt64() || !held.IsInt64() {
			return fault.New(503, "COST_OVERFLOW")
		}
		out.Costs.KnownCostMicros = known.String()
		out.Costs.HeldCostMicros = held.String()
		if e = tx.Raw("SELECT EXISTS(SELECT 1 FROM budget_limits b JOIN budget_reservations r ON r.run_id=? CROSS JOIN jsonb_array_elements(r.scopes) x WHERE b.scope_type='global' AND x->>'type'='global' AND b.scope_key=x->>'key' AND b.currency=r.currency AND (b.held_micros::numeric+b.spent_micros)*5>=b.limit_micros::numeric*4)", run.ID).Scan(&out.Costs.MonthlyWarning).Error; e != nil {
			return e
		}
		var publication struct {
			State   string
			Receipt []byte
		}
		if e = tx.Raw("SELECT p.state,f.receipt FROM publication_requests p LEFT JOIN review_publication_facts f ON f.run_id=p.run_id WHERE p.run_id=?", run.ID).Scan(&publication).Error; e != nil {
			return e
		}
		if publication.State != "" {
			out.Publication.State = contracts.ReviewPublicationSummaryState(publication.State)
			if publication.State == "running" {
				out.Publication.State = "pending"
			}
		}
		if len(publication.Receipt) > 0 {
			out.Publication.Receipt = new(contracts.ReviewPublicationReceipt)
			if e = json.Unmarshal(publication.Receipt, out.Publication.Receipt); e != nil {
				return e
			}
		}
		return tx.Raw("SELECT count(*) FROM pr_review_slots WHERE installation_id=? AND repository_id=? AND pr_number=?", tenant.InstallationId, tenant.RepositoryId, prNumber).Scan(&out.Publication.OccupiedSlots).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return out, e
	}
	body, _ := json.Marshal(out)
	var checked contracts.ReviewRunDetail
	if e = contracts.DecodeReview("ReviewRunDetail", body, &checked, 262144); e != nil {
		return out, fault.New(503, "REVIEW_VIEW_INVALID")
	}
	return checked, nil
}

type queryCursor struct {
	Tenant   contracts.TenantRef
	PR       int
	Kind, ID string
	At       time.Time
	Number   int
}

func (s *QueryService) signCursor(c queryCursor) (string, error) {
	if len(s.Auth.Config.SessionSecret) < 32 {
		return "", fault.New(503, "CURSOR_SIGNER_UNAVAILABLE")
	}
	raw, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, []byte(s.Auth.Config.SessionSecret))
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *QueryService) readCursor(value string, tenant contracts.TenantRef, pr int, kind string) (queryCursor, error) {
	var c queryCursor
	if value == "" {
		return c, nil
	}
	parts := strings.Split(value, ".")
	if len(value) > 1024 || len(parts) != 2 {
		return c, fault.New(400, "INVALID_CURSOR")
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	mac := hmac.New(sha256.New, []byte(s.Auth.Config.SessionSecret))
	mac.Write(raw)
	if e != nil || err != nil || !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(raw, &c) != nil || c.Tenant != tenant || c.PR != pr || c.Kind != kind {
		return c, fault.New(400, "INVALID_CURSOR")
	}
	return c, nil
}
func summary(run persistence.Run) (contracts.ReviewRunSummary, error) {
	snapshot, e := jobs.ReviewSnapshot(run)
	return contracts.ReviewRunSummary{RunId: run.ID, HeadSha: run.HeadSHA, Status: contracts.RunState(run.Status), NeedsReconciliation: run.NeedsReconciliation, ReasonCode: run.ReasonCode, CreatedAt: run.CreatedAt, Mode: contracts.ReviewRunSummaryMode(snapshot.Provenance.ModelMode)}, e
}
func (s *QueryService) ListRuns(ctx context.Context, session auth.Session, tenant contracts.TenantRef, pr int, cursor string) (contracts.ReviewRunList, error) {
	out := contracts.ReviewRunList{Items: []contracts.ReviewRunSummary{}}
	if e := s.authorize(ctx, session, tenant); e != nil {
		return out, e
	}
	if pr < 1 {
		return out, fault.New(400, "INVALID_PR")
	}
	c, e := s.readCursor(cursor, tenant, pr, "runs")
	if e != nil {
		return out, e
	}
	query := s.DB.WithContext(ctx).Where("installation_id=? AND repository_id=? AND pr_number=? AND pipeline_version='review-v1'", tenant.InstallationId, tenant.RepositoryId, pr)
	if cursor != "" {
		query = query.Where("(created_at,id)<(?,?::uuid)", c.At, c.ID)
	}
	var rows []persistence.Run
	if e = query.Order("created_at DESC,id DESC").Limit(21).Find(&rows).Error; e != nil {
		return out, e
	}
	if len(rows) > 20 {
		last := rows[19]
		next, e := s.signCursor(queryCursor{Tenant: tenant, PR: pr, Kind: "runs", ID: last.ID, At: last.CreatedAt})
		if e != nil {
			return out, e
		}
		out.NextCursor = &next
		rows = rows[:20]
	}
	for _, run := range rows {
		item, e := summary(run)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
func (s *QueryService) ListPulls(ctx context.Context, session auth.Session, tenant contracts.TenantRef, cursor string) (contracts.ReviewPullList, error) {
	out := contracts.ReviewPullList{Items: []contracts.ReviewPullSummary{}}
	if e := s.authorize(ctx, session, tenant); e != nil {
		return out, e
	}
	c, e := s.readCursor(cursor, tenant, 0, "pulls")
	if e != nil {
		return out, e
	}
	max := 2147483647
	if cursor != "" {
		max = c.Number
	}
	var rows []persistence.Run
	e = s.DB.WithContext(ctx).Raw("SELECT * FROM(SELECT DISTINCT ON(pr_number) * FROM review_runs WHERE installation_id=? AND repository_id=? AND pipeline_version='review-v1' AND pr_number<? ORDER BY pr_number,created_at DESC,id DESC) latest ORDER BY pr_number DESC LIMIT 21", tenant.InstallationId, tenant.RepositoryId, max).Scan(&rows).Error
	if e != nil {
		return out, e
	}
	if len(rows) > 20 {
		next, e := s.signCursor(queryCursor{Tenant: tenant, Kind: "pulls", Number: rows[19].PrNumber})
		if e != nil {
			return out, e
		}
		out.NextCursor = &next
		rows = rows[:20]
	}
	for _, run := range rows {
		latest, e := summary(run)
		if e != nil {
			return out, e
		}
		snapshot, _ := jobs.ReviewSnapshot(run)
		out.Items = append(out.Items, contracts.ReviewPullSummary{PrNumber: run.PrNumber, Title: snapshot.Identity.PrTitle, HeadSha: run.HeadSHA, LatestRun: latest})
	}
	return out, nil
}
