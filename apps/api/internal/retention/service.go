package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/reviewcontext"
	"time"
)

type Service struct{ DB *gorm.DB }
type CleanupCounts struct{ Contexts, Stages, FindingContents, Metadata int64 }

func New(db *gorm.DB) *Service { return &Service{db} }
func BatchLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 100
	}
	return limit
}
func validTenant(t contracts.TenantRef) bool {
	return t.InstallationId > 0 && t.RepositoryId > 0 && t.InstallationId <= 9007199254740991 && t.RepositoryId <= 9007199254740991
}

func (s *Service) Cleanup(ctx context.Context, now time.Time, limit int) (CleanupCounts, error) {
	counts := CleanupCounts{}
	remaining := BatchLimit(limit)
	if s.DB == nil {
		return counts, fault.New(503, "DATABASE_UNAVAILABLE")
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, action := range []struct {
			query string
			count *int64
		}{
			{"WITH due AS(SELECT p.run_id FROM publication_requests p JOIN review_runs r ON r.id=p.run_id WHERE r.pipeline_version='review-v1' AND (r.snapshot->'provenance'->>'contentExpiresAt')::timestamptz<=? AND p.review_plan IS NOT NULL ORDER BY p.run_id LIMIT ? FOR UPDATE OF p SKIP LOCKED) UPDATE publication_requests p SET review_plan=NULL,review_plan_erased=true FROM due d WHERE p.run_id=d.run_id", &counts.FindingContents},
			{"WITH due AS(SELECT id FROM review_findings WHERE expires_at<=? AND (candidate IS NOT NULL OR evidence IS NOT NULL) ORDER BY expires_at LIMIT ? FOR UPDATE SKIP LOCKED) UPDATE review_findings a SET candidate=NULL,evidence=NULL,root_key='{}' FROM due d WHERE a.id=d.id", &counts.FindingContents},
			{"WITH due AS(SELECT run_id,candidate_id FROM review_filtered_candidates WHERE expires_at<=? AND (title IS NOT NULL OR evidence IS NOT NULL) ORDER BY expires_at LIMIT ? FOR UPDATE SKIP LOCKED) UPDATE review_filtered_candidates a SET title=NULL,evidence=NULL FROM due d WHERE a.run_id=d.run_id AND a.candidate_id=d.candidate_id", &counts.FindingContents},
			{"WITH due AS(SELECT x.task_id FROM job_results x JOIN jobs j ON j.id=x.task_id JOIN review_runs r ON r.id=j.run_id WHERE r.pipeline_version='review-v1' AND COALESCE((SELECT m.expires_at FROM review_context_manifests m WHERE m.run_id=r.id),x.accepted_at+interval '30 days')<=? AND x.payload<>'{}'::jsonb ORDER BY x.accepted_at LIMIT ? FOR UPDATE OF x SKIP LOCKED) UPDATE job_results x SET payload='{}' FROM due d WHERE x.task_id=d.task_id", &counts.FindingContents},
			{"WITH due AS(SELECT run_id,stage,stage_input_hash FROM review_stage_artifacts WHERE expires_at<=? AND payload IS NOT NULL ORDER BY expires_at LIMIT ? FOR UPDATE SKIP LOCKED) UPDATE review_stage_artifacts a SET payload=NULL FROM due d WHERE a.run_id=d.run_id AND a.stage=d.stage AND a.stage_input_hash=d.stage_input_hash", &counts.Stages},
			{"WITH due AS(SELECT id FROM review_context_manifests WHERE expires_at<=? AND (pages<>'[]'::jsonb OR files<>'[]'::jsonb) ORDER BY expires_at LIMIT ? FOR UPDATE SKIP LOCKED) UPDATE review_context_manifests a SET pages='[]',files='[]' FROM due d WHERE a.id=d.id", &counts.Contexts},
			{"WITH due AS(SELECT id FROM review_evidence WHERE expires_at<=? AND snippet IS NOT NULL ORDER BY expires_at LIMIT ? FOR UPDATE SKIP LOCKED) UPDATE review_evidence a SET snippet=NULL FROM due d WHERE a.id=d.id", &counts.Contexts},
		} {
			if remaining == 0 {
				break
			}
			result := tx.Exec(action.query, now, remaining)
			if result.Error != nil {
				return result.Error
			}
			*action.count += result.RowsAffected
			remaining -= int(result.RowsAffected)
		}
		// Unknown financial holds are deliberately not part of content or metadata TTL cleanup.
		for _, query := range []string{
			"DELETE FROM review_publication_facts WHERE run_id IN(SELECT run_id FROM review_publication_facts WHERE updated_at<? ORDER BY updated_at LIMIT ?)",
			"DELETE FROM review_filtered_candidates WHERE (run_id,candidate_id) IN(SELECT run_id,candidate_id FROM review_filtered_candidates WHERE created_at<? ORDER BY created_at LIMIT ?)",
			"DELETE FROM review_findings WHERE id IN(SELECT id FROM review_findings WHERE created_at<? ORDER BY created_at LIMIT ?)",
			"DELETE FROM review_coverage WHERE run_id IN(SELECT run_id FROM review_coverage WHERE created_at<? ORDER BY created_at LIMIT ?)",
			"DELETE FROM operator_audits WHERE id IN(SELECT id FROM operator_audits WHERE created_at<? ORDER BY created_at LIMIT ?)",
			"DELETE FROM review_stage_artifacts WHERE (run_id,stage,stage_input_hash) IN(SELECT run_id,stage,stage_input_hash FROM review_stage_artifacts WHERE created_at<? ORDER BY created_at LIMIT ?)",
			"DELETE FROM review_evidence WHERE id IN(SELECT id FROM review_evidence WHERE expires_at<? ORDER BY expires_at LIMIT ?)",
			"DELETE FROM review_context_manifests WHERE id IN(SELECT id FROM review_context_manifests WHERE expires_at<? AND NOT EXISTS(SELECT 1 FROM review_evidence e WHERE e.manifest_id=review_context_manifests.id) ORDER BY expires_at LIMIT ?)",
			"DELETE FROM model_calls WHERE call_id IN(SELECT call_id FROM model_calls c WHERE created_at<? AND state IN('settled','released') AND NOT EXISTS(SELECT 1 FROM review_stage_artifacts a WHERE a.call_id=c.call_id) ORDER BY created_at LIMIT ?)",
			"DELETE FROM budget_reservations WHERE id IN(SELECT id FROM budget_reservations b WHERE created_at<? AND state<>'held' AND NOT EXISTS(SELECT 1 FROM model_calls c WHERE c.reservation_id=b.id) ORDER BY created_at LIMIT ?)",
		} {
			if remaining == 0 {
				break
			}
			result := tx.Exec(query, now.AddDate(-1, 0, 0), remaining)
			if result.Error != nil {
				return result.Error
			}
			counts.Metadata += result.RowsAffected
			remaining -= int(result.RowsAffected)
		}
		return nil
	})
	return counts, err
}

func (s *Service) DeleteRepository(ctx context.Context, tenant contracts.TenantRef) error {
	if !validTenant(tenant) {
		return fault.New(400, "EXPLICIT_DELETE_SCOPE_REQUIRED")
	}
	if s.DB == nil {
		return fault.New(503, "DATABASE_UNAVAILABLE")
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var installation persistence.Installation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, "id=?", tenant.InstallationId).Error; err != nil {
			return fault.New(404, "INSTALLATION_UNKNOWN")
		}
		var repository persistence.Repository
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&repository, "installation_id=? AND id=?", tenant.InstallationId, tenant.RepositoryId).Error; err != nil {
			return fault.New(404, "REPOSITORY_UNKNOWN")
		}
		if err := tx.Exec("UPDATE repositories SET enabled=false,auth_revision=auth_revision+1,config_version=config_version+1 WHERE installation_id=? AND id=?", tenant.InstallationId, tenant.RepositoryId).Error; err != nil {
			return err
		}
		var runs []persistence.Run
		if err := tx.Where("installation_id=? AND repository_id=? AND pipeline_version='review-v1'", tenant.InstallationId, tenant.RepositoryId).Find(&runs).Error; err != nil {
			return err
		}
		for _, run := range runs {
			reviewcontext.ForgetEphemeral(run.ID)
			if err := tx.Exec("UPDATE jobs SET status='cancelled',lease_token=NULL,lease_expires_at=NULL WHERE run_id=?", run.ID).Error; err != nil {
				return err
			}
			if err := tx.Exec("UPDATE publication_requests SET state=CASE WHEN review_write_started OR check_write_started THEN 'uncertain' ELSE 'cancelled' END,review_plan=NULL,lease_token=NULL,lease_expires_at=NULL WHERE run_id=?", run.ID).Error; err != nil {
				return err
			}
			// Keep only non-content occupancy so deletion cannot authorize duplicate external writes.
			if err := tx.Exec("UPDATE pr_review_slots SET run_id=NULL,state=CASE WHEN state='reserved' THEN 'uncertain' ELSE state END WHERE run_id=?", run.ID).Error; err != nil {
				return err
			}
			if err := tx.Exec("DELETE FROM review_publication_facts WHERE run_id=?", run.ID).Error; err != nil {
				return err
			}
			for _, query := range []string{"DELETE FROM review_findings WHERE run_id=?", "DELETE FROM review_filtered_candidates WHERE run_id=?", "DELETE FROM review_coverage WHERE run_id=?", "DELETE FROM review_evidence WHERE run_id=?", "DELETE FROM review_stage_artifacts WHERE run_id=?", "DELETE FROM review_context_manifests WHERE run_id=?", "UPDATE job_results SET payload='{}' WHERE task_id IN(SELECT id FROM jobs WHERE run_id=?)"} {
				if err := tx.Exec(query, run.ID).Error; err != nil {
					return err
				}
			}
			// Keep only anonymous global cost facts; a deletion is not a provider refund.
			var reservations []struct {
				ID     string
				Scopes json.RawMessage
			}
			if err := tx.Raw("SELECT id,scopes FROM budget_reservations WHERE run_id=?", run.ID).Scan(&reservations).Error; err != nil {
				return err
			}
			for _, reservation := range reservations {
				var scopes []map[string]any
				if json.Unmarshal(reservation.Scopes, &scopes) != nil {
					return fault.New(503, "BUDGET_FACTS_INVALID")
				}
				global := []map[string]any{}
				for _, scope := range scopes {
					if scope["type"] == "global" {
						global = append(global, scope)
					}
				}
				body, _ := json.Marshal(global)
				if err := tx.Exec("UPDATE budget_reservations SET run_id=NULL,installation_id=NULL,repository_id=NULL,scopes=?::jsonb WHERE id=?", string(body), reservation.ID).Error; err != nil {
					return err
				}
			}
			if err := tx.Exec("UPDATE model_calls SET run_id=NULL,request_hash=repeat('0',64),usage=NULL,response_hash=NULL WHERE run_id=?", run.ID).Error; err != nil {
				return err
			}
			if err := tx.Exec("DELETE FROM budget_limits WHERE scope_type='run' AND scope_key=?", run.ID).Error; err != nil {
				return err
			}
			if err := tx.Exec("UPDATE review_runs SET status='Cancelled',snapshot='{}',provenance=NULL,needs_reconciliation=false,reason_code='REPOSITORY_CONTENT_DELETED',updated_at=clock_timestamp() WHERE id=?", run.ID).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("DELETE FROM budget_limits WHERE scope_type='repository' AND scope_key LIKE ?", fmt.Sprintf("%d:%d:%%", tenant.InstallationId, tenant.RepositoryId)).Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO operator_audits(id,action,installation_id,repository_id) VALUES(?,'delete-repository',?,?)", jobs.UUID(), tenant.InstallationId, tenant.RepositoryId).Error
	})
}
