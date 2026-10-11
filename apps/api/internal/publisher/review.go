package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"os"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/observability"
	"strconv"
	"time"
)

func (s *Service) reviewGate(snapshot contracts.ReviewSnapshot) error {
	cfg, e := config.LoadReview()
	if e != nil {
		return fault.New(503, "REVIEW_CONFIG_INVALID")
	}
	if snapshot.Provenance.ModelMode == "fake" {
		if cfg.Mode != "fake" || snapshot.Provenance.GithubMode != "fake" || snapshot.Provenance.FixtureId != cfg.Model.FixtureID || s.Gateway.APIURL != os.Getenv("TEST_GITHUB_BASE_URL") || os.Getenv("REVIEW_TEST_ISOLATED") != "1" {
			return fault.New(409, "FAKE_PUBLICATION_NOT_ISOLATED")
		}
	} else if cfg.Mode != "real" || !cfg.RealAllowed || !cfg.PublishEnabled || snapshot.Provenance.GithubMode != "real" || s.Gateway.APIURL != "https://api.github.com" {
		return fault.New(409, "REAL_PUBLICATION_DISABLED")
	}
	return nil
}
func (s *Service) publishReview(ctx context.Context, l lease) error {
	ctx, span := observability.FromContext(ctx).Start(ctx, "review.publish", observability.Fields{RunID: l.Run.ID})
	defer span.End()
	var snapshot contracts.ReviewSnapshot
	if e := json.Unmarshal(l.Run.Snapshot, &snapshot); e != nil {
		return e
	}
	if e := s.reviewGate(snapshot); e != nil {
		return s.uncertainReview(ctx, l, e.Error())
	}
	if e := s.fence(ctx, l); e != nil {
		return e
	}
	token, e := s.Gateway.ReviewWriteToken(ctx, snapshot.Identity.Tenant)
	if e != nil {
		return s.reviewBeforeError(ctx, l, e)
	}
	var started struct{ ReviewWriteStarted, CheckWriteStarted bool }
	if e = s.DB.WithContext(ctx).Raw("SELECT review_write_started,check_write_started FROM publication_requests WHERE run_id=?", l.Run.ID).Scan(&started).Error; e != nil {
		return e
	}
	if !started.ReviewWriteStarted && !started.CheckWriteStarted {
		if e = s.guard(ctx, l, token); e != nil {
			return s.reviewBeforeError(ctx, l, e)
		}
	}
	plan, e := s.BuildReviewPlan(ctx, l.Run.ID)
	if e != nil {
		if e.Error() == "PUBLICATION_CONTENT_EXPIRED" {
			return s.uncertainReview(ctx, l, "PUBLICATION_CONTENT_EXPIRED")
		}
		return e
	}
	receipt := contracts.ReviewPublicationReceipt{HeadSha: plan.HeadSha, AuthorAppId: int(s.Gateway.Config.GitHubAppID), Marker: plan.Marker, State: "confirmed", Mode: contracts.ReviewPublicationReceiptMode(plan.Mode), CommentIds: []string{}}
	if len(plan.Findings) > 0 {
		found, err := s.Gateway.FindReview(ctx, token, l.FullName, l.Run.PrNumber, plan)
		if err != nil {
			return s.uncertainReview(ctx, l, "REMOTE_REVIEW_FACTS_UNKNOWN")
		}
		if found != nil {
			receipt = *found
		} else {
			if started.ReviewWriteStarted {
				return s.uncertainReview(ctx, l, "REMOTE_REVIEW_FACTS_UNKNOWN")
			}
			if e = s.guard(ctx, l, token); e != nil {
				return s.reviewBeforeError(ctx, l, e)
			}
			if e = s.beginReviewWrite(ctx, l, false); e != nil {
				return e
			}
			receipt, e = s.Gateway.CreateReview(ctx, token, l.FullName, l.Run.PrNumber, plan)
			if e != nil {
				return s.reviewWriteError(ctx, l, e, false)
			}
		}
		if e = s.saveReviewReceipt(ctx, l, plan, receipt); e != nil {
			return e
		}
	}
	// Save observed remote facts before determining whether the Head changed after the write.
	if e = s.guard(ctx, l, token); e != nil {
		return s.reviewBeforeError(ctx, l, e)
	}
	var coverage struct{ Payload []byte }
	s.DB.WithContext(ctx).Raw("SELECT payload FROM review_coverage WHERE run_id=?", l.Run.ID).Scan(&coverage)
	var complete contracts.ReviewCoverage
	_ = json.Unmarshal(coverage.Payload, &complete)
	if !snapshot.Policy.RetainContent || !snapshot.Provenance.ContentExpiresAt.After(time.Now()) {
		complete.Complete = false
	}
	check, e := s.Gateway.FindReviewCheck(ctx, token, l.FullName, plan)
	if e != nil {
		return s.uncertainReview(ctx, l, "REMOTE_CHECK_FACTS_UNKNOWN")
	}
	if check == nil {
		if started.CheckWriteStarted {
			return s.uncertainReview(ctx, l, "REMOTE_CHECK_FACTS_UNKNOWN")
		}
		if e = s.guard(ctx, l, token); e != nil {
			return s.reviewBeforeError(ctx, l, e)
		}
		if e = s.beginReviewWrite(ctx, l, true); e != nil {
			return e
		}
		created, err := s.Gateway.WriteReviewCheck(ctx, token, l.FullName, plan, complete.Complete)
		if err != nil {
			return s.reviewWriteError(ctx, l, err, true)
		}
		check = &created
	}
	if check.Status != "completed" || check.Conclusion == nil || *check.Conclusion != "neutral" {
		return s.uncertainReview(ctx, l, "REMOTE_CHECK_FACTS_UNKNOWN")
	}
	id := githubID(check.ID)
	receipt.CheckId = &id
	if e = s.saveReviewReceipt(ctx, l, plan, receipt); e != nil {
		return e
	}
	if e = s.guard(ctx, l, token); e != nil {
		return s.reviewBeforeError(ctx, l, e)
	}
	if e = s.locked(ctx, l, func(tx *gorm.DB) error {
		return tx.Exec("UPDATE review_runs SET needs_reconciliation=false,reason_code=NULL WHERE id=?", l.Run.ID).Error
	}); e != nil {
		return e
	}
	status := "Partial"
	if complete.Complete {
		status = "Completed"
	}
	return s.finish(ctx, l, status, "published", "completed")
}
func githubID(id int) string { return strconv.Itoa(id) }
func (s *Service) reviewBeforeError(ctx context.Context, l lease, e error) error {
	if errors.Is(e, stale) {
		return s.finish(ctx, l, "Stale", "cancelled", "cancelled")
	}
	var f *fault.Error
	if errors.As(e, &f) && f.Status == 403 {
		return s.permissionError(ctx, l, e)
	}
	var limited *github.ReviewRateLimit
	if errors.As(e, &limited) {
		return s.locked(ctx, l, func(tx *gorm.DB) error {
			return tx.Exec("UPDATE publication_requests SET state='pending',lease_token=NULL,lease_expires_at=NULL,available_at=? WHERE run_id=?", time.Now().Add(limited.After), l.Run.ID).Error
		})
	}
	return s.uncertainReview(ctx, l, "REMOTE_READ_UNKNOWN")
}
func (s *Service) reviewWriteError(ctx context.Context, l lease, e error, check bool) error {
	var limited *github.ReviewRateLimit
	if errors.As(e, &limited) {
		return s.locked(ctx, l, func(tx *gorm.DB) error {
			column := "review_write_started"
			if check {
				column = "check_write_started"
			}
			return tx.Exec("UPDATE publication_requests SET state='pending',lease_token=NULL,lease_expires_at=NULL,"+column+"=false,last_error_code='GITHUB_REVIEW_RATE_LIMITED',available_at=? WHERE run_id=?", time.Now().Add(limited.After), l.Run.ID).Error
		})
	}
	return s.uncertainReview(ctx, l, "REMOTE_WRITE_UNKNOWN")
}
func (s *Service) ReconcileReview(ctx context.Context, runID string) (contracts.ReviewPublicationReceipt, error) {
	// Persistent write-start flags make this retry a read-only reconciliation of unknown writes.
	l, e := s.claim(ctx, runID)
	if e != nil {
		return contracts.ReviewPublicationReceipt{}, e
	}
	if l == nil {
		return contracts.ReviewPublicationReceipt{}, fault.New(409, "PUBLICATION_NOT_READY")
	}
	if l.Run.PipelineVersion != "review-v1" {
		return contracts.ReviewPublicationReceipt{}, fault.New(409, "PUBLICATION_PIPELINE_MISMATCH")
	}
	if e = s.publishReview(ctx, *l); e != nil {
		return contracts.ReviewPublicationReceipt{}, e
	}
	var row struct{ Receipt []byte }
	e = s.DB.WithContext(ctx).Raw("SELECT receipt FROM review_publication_facts WHERE run_id=?", runID).Scan(&row).Error
	var receipt contracts.ReviewPublicationReceipt
	if e == nil && len(row.Receipt) > 0 {
		e = json.Unmarshal(row.Receipt, &receipt)
	} else if e == nil {
		e = fault.New(409, "PUBLICATION_UNCONFIRMED")
	}
	return receipt, e
}
