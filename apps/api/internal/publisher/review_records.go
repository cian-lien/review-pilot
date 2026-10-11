package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
)

func (s *Service) BuildReviewPlan(ctx context.Context, runID string) (contracts.ReviewPublicationPlan, error) {
	plan := contracts.ReviewPublicationPlan{Findings: []contracts.ReviewPublicationFinding{}}
	if s.DB == nil {
		return plan, fault.New(503, "DATABASE_UNAVAILABLE")
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runtime persistence.RuntimeControl
		if e := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&runtime, 1).Error; e != nil {
			return e
		}
		if runtime.Mode != "go" {
			return fault.New(409, "RUNTIME_NOT_GO")
		}
		var task persistence.Job
		if e := tx.First(&task, "run_id=?", runID).Error; e != nil {
			return e
		}
		_, run, allowed, e := jobs.LockReview(tx, task.ID)
		if e != nil {
			return e
		}
		if !allowed || run.PipelineVersion != "review-v1" || run.Status != "Publishing" {
			return fault.New(409, "PUBLICATION_NOT_READY")
		}
		var request struct {
			ReviewPlan       []byte
			ReviewPlanErased bool
		}
		if e = tx.Raw("SELECT review_plan,review_plan_erased FROM publication_requests WHERE run_id=? FOR UPDATE", runID).Scan(&request).Error; e != nil {
			return e
		}
		if request.ReviewPlanErased {
			return fault.New(409, "PUBLICATION_CONTENT_EXPIRED")
		}
		if len(request.ReviewPlan) > 0 {
			return contracts.DecodeReview("ReviewPublicationPlan", request.ReviewPlan, &plan, 32768)
		}
		var snapshot contracts.ReviewSnapshot
		if e = json.Unmarshal(run.Snapshot, &snapshot); e != nil {
			return e
		}
		plan.RunId = run.ID
		plan.Tenant = snapshot.Identity.Tenant
		plan.PrNumber = run.PrNumber
		plan.HeadSha = run.HeadSHA
		plan.Mode = contracts.ReviewPublicationPlanMode(snapshot.Provenance.ModelMode)
		scope := fmt.Sprintf("review-slots:%d:%d:%d", run.InstallationID, run.RepositoryID, run.PrNumber)
		// All Runs for a PR share one transaction lock, including Runs for different SHAs.
		if e = tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", scope).Error; e != nil {
			return e
		}
		var count int
		if e = tx.Raw("SELECT count(*) FROM pr_review_slots WHERE installation_id=? AND repository_id=? AND pr_number=?", run.InstallationID, run.RepositoryID, run.PrNumber).Scan(&count).Error; e != nil {
			return e
		}
		var rows []struct {
			ID, Fingerprint     string
			Candidate, Evidence []byte
			Live                bool
		}
		if e = tx.Raw("SELECT id,fingerprint,candidate,evidence,expires_at>clock_timestamp() AS live FROM review_findings WHERE run_id=? ORDER BY CASE candidate->>'severity' WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END,id", run.ID).Scan(&rows).Error; e != nil {
			return e
		}
		for _, row := range rows {
			reason := ""
			var duplicate int
			if e = tx.Raw("SELECT count(*) FROM pr_review_slots WHERE installation_id=? AND repository_id=? AND pr_number=? AND fingerprint=?", run.InstallationID, run.RepositoryID, run.PrNumber, row.Fingerprint).Scan(&duplicate).Error; e != nil {
				return e
			}
			if duplicate > 0 {
				reason = "ROOT_ALREADY_OCCUPIED"
			} else if count >= 3 {
				reason = "PR_LIFETIME_LIMIT"
			} else if !row.Live || len(row.Candidate) == 0 {
				reason = "CONTENT_UNAVAILABLE"
			}
			if reason != "" {
				if e = tx.Exec("UPDATE review_findings SET publication_status='not-published',publication_reason=? WHERE id=?", reason, row.ID).Error; e != nil {
					return e
				}
				continue
			}
			var candidate contracts.ReviewCandidate
			var facts []contracts.CodeEvidence
			if json.Unmarshal(row.Candidate, &candidate) != nil || json.Unmarshal(row.Evidence, &facts) != nil {
				return fault.New(409, "FINDING_CONTENT_INVALID")
			}
			path := ""
			var line *int
			if len(facts) > 0 {
				path = facts[0].Ref.Path
				value := facts[0].Ref.EndLine
				line = &value
			}
			body := contracts.RedactReview(fmt.Sprintf("%s\n\n触发条件：%s\n影响：%s\n建议验证：%s", candidate.Title, candidate.FailureScenario, candidate.Impact, candidate.SuggestedVerification))
			if len([]rune(body)) > 8192 {
				return fault.New(409, "FINDING_CONTENT_TOO_LARGE")
			}
			plan.Findings = append(plan.Findings, contracts.ReviewPublicationFinding{FindingId: row.ID, Fingerprint: row.Fingerprint, Path: path, Line: line, Body: body})
			if e = tx.Exec("INSERT INTO pr_review_slots(installation_id,repository_id,pr_number,fingerprint,run_id,state) VALUES(?,?,?,?,?,'reserved')", run.InstallationID, run.RepositoryID, run.PrNumber, row.Fingerprint, run.ID).Error; e != nil {
				return e
			}
			count++
		}
		canonical, _ := json.Marshal(plan)
		plan.PlanHash = jobs.Hash(canonical)
		plan.Marker = github.ReviewMarker(plan.RunId, plan.PlanHash)
		body, _ := json.Marshal(plan)
		return tx.Exec("UPDATE publication_requests SET review_plan=?::jsonb WHERE run_id=?", string(body), run.ID).Error
	})
	return plan, err
}

func (s *Service) beginReviewWrite(ctx context.Context, l lease, check bool) error {
	column := "review_write_started"
	if check {
		column = "check_write_started"
	}
	return s.locked(ctx, l, func(tx *gorm.DB) error {
		result := tx.Exec("UPDATE publication_requests SET "+column+"=true WHERE run_id=? AND NOT "+column, l.Run.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fault.New(409, "PUBLICATION_WRITE_ALREADY_STARTED")
		}
		return nil
	})
}
func (s *Service) uncertainReview(ctx context.Context, l lease, reason string) error {
	return s.locked(ctx, l, func(tx *gorm.DB) error {
		if e := tx.Exec("UPDATE publication_requests SET state='uncertain',lease_token=NULL,lease_expires_at=NULL,last_error_code=?,available_at=clock_timestamp()+interval '60 seconds' WHERE run_id=?", reason, l.Run.ID).Error; e != nil {
			return e
		}
		if e := tx.Exec("UPDATE pr_review_slots SET state='uncertain' WHERE run_id=? AND state='reserved'", l.Run.ID).Error; e != nil {
			return e
		}
		return tx.Exec("UPDATE review_runs SET needs_reconciliation=true,reason_code=?,updated_at=clock_timestamp() WHERE id=?", reason, l.Run.ID).Error
	})
}
func (s *Service) saveReviewReceipt(ctx context.Context, l lease, plan contracts.ReviewPublicationPlan, receipt contracts.ReviewPublicationReceipt) error {
	body, _ := json.Marshal(receipt)
	return s.locked(ctx, l, func(tx *gorm.DB) error {
		if e := tx.Exec("INSERT INTO review_publication_facts(run_id,plan_hash,receipt) VALUES(?,?,?::jsonb) ON CONFLICT(run_id) DO UPDATE SET receipt=EXCLUDED.receipt,updated_at=clock_timestamp() WHERE review_publication_facts.plan_hash=EXCLUDED.plan_hash", l.Run.ID, plan.PlanHash, string(body)).Error; e != nil {
			return e
		}
		if receipt.ReviewId != nil {
			if e := tx.Exec("UPDATE pr_review_slots SET state='published' WHERE run_id=?", l.Run.ID).Error; e != nil {
				return e
			}
			return tx.Exec("UPDATE review_findings SET publication_status='published',publication_reason=NULL WHERE id IN(SELECT (value->>'findingId')::uuid FROM publication_requests p,jsonb_array_elements(p.review_plan->'findings') WHERE p.run_id=?)", l.Run.ID).Error
		}
		return nil
	})
}
