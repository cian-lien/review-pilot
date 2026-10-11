//go:build integration

package reviews

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reviewpilot.local/api/internal/budgets"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/retention"
	"reviewpilot.local/api/internal/reviewstages"
	"reviewpilot.local/api/internal/testsupport"
	"strings"
	"testing"
	"time"
)

func preparedResult(t *testing.T, store *persistence.Store, address, variant string) contracts.ReviewResult {
	t.Helper()
	lease, _ := testsupport.SeedReview(t, store, address)
	manifestID, evidenceID := jobs.UUID(), jobs.UUID()
	contextHash := strings.Repeat("c", 64)
	codeHash := strings.Repeat("d", 64)
	base, head, patch := "export const key = order.key;\n", "export const key = crypto.randomUUID();\n", "@@ -1 +1 @@\n-export const key = order.key;\n+export const key = crypto.randomUUID();"
	coverage := contracts.ReviewCoverage{Complete: true, TotalFiles: 1, ReadFiles: 1, TotalChangedLines: 2, CoveredChangedLines: 2, RawBytes: 80, Skipped: []contracts.ReviewSkippedFile{}, Limitations: []string{"静态文本证据"}}
	file := contracts.ReviewChangedFile{Path: "src/pay.ts", Status: "modified", Additions: 1, Deletions: 1, Patch: &patch, Base: contracts.ReviewFileContent{State: "available", Sha: lease.Snapshot.Identity.BaseSha, ContentHash: &codeHash, Text: &base, Bytes: 28}, Head: contracts.ReviewFileContent{State: "available", Sha: lease.HeadSha, ContentHash: &codeHash, Text: &head, Bytes: 42}}
	page := contracts.ReviewContextPage{ManifestId: manifestID, ContextHash: &contextHash, Complete: true, Files: []contracts.ReviewChangedFile{file}, Evidence: []contracts.CodeEvidence{}, Coverage: coverage}
	pages, _ := json.Marshal([]contracts.ReviewContextPage{page})
	cbody, _ := json.Marshal(coverage)
	if err := store.DB().Exec("INSERT INTO review_context_manifests(id,run_id,input_hash,files,pages,coverage,sealed,context_hash,expires_at) VALUES(?,?,?,'[]',?::jsonb,?::jsonb,true,?,?)", manifestID, lease.RunId, lease.InputHash, string(pages), string(cbody), contextHash, lease.Snapshot.Provenance.ContentExpiresAt).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec("INSERT INTO review_evidence(id,run_id,manifest_id,installation_id,repository_id,path,sha,content_hash,start_line,end_line,original_hash,snippet,expires_at) VALUES(?,?,?,101,202,'src/pay.ts',?,?,1,1,?,?,?)", evidenceID, lease.RunId, manifestID, lease.HeadSha, codeHash, codeHash, strings.TrimSuffix(head, "\n"), lease.Snapshot.Provenance.ContentExpiresAt).Error; err != nil {
		t.Fatal(err)
	}
	ref := contracts.EvidenceRef{EvidenceId: evidenceID, Path: file.Path, Sha: lease.HeadSha, StartLine: 1, EndLine: 1, ContentHash: codeHash}
	if variant == "bad-line" {
		ref.EndLine = 99
	}
	if variant == "cross-run" {
		other, _ := testsupport.SeedReview(t, store, address)
		if err := store.DB().Exec("INSERT INTO repositories(installation_id,id,full_name,public,enabled) VALUES(101,303,'acme/other',true,true) ON CONFLICT DO NOTHING").Error; err != nil {
			t.Fatal(err)
		}
		if err := store.DB().Exec("UPDATE review_evidence SET run_id=?,repository_id=303 WHERE id=?", other.RunId, evidenceID).Error; err != nil {
			t.Fatal(err)
		}
	}
	candidate := contracts.ReviewCandidate{CandidateId: "c1", Title: "Retry may charge twice", Severity: "high", Category: "data-integrity", FailureScenario: "Lost response regenerates key", Impact: "Duplicate capture", IntroducedByPR: true, Evidence: []contracts.EvidenceRef{ref}, SuggestedVerification: "Retry lost response", RootCauseStatement: contracts.RootCauseStatement{Trigger: "retry creates a new key", Consequence: "provider charges twice"}}
	decision := contracts.VerificationDecision{CandidateId: "c1", Verdict: "verified", ReasonCode: "EVIDENCE_SUPPORTED", Evidence: []contracts.EvidenceRef{ref}}
	result := contracts.ReviewResult{Kind: "ai-review", Lease: lease, ResultId: lease.TaskId + ":review", ContextHash: contextHash, Candidates: []contracts.ReviewCandidate{candidate}, Decisions: []contracts.VerificationDecision{decision}, Coverage: coverage, CallIds: []string{}}
	cfg, err := config.LoadReview()
	if err != nil {
		t.Fatal(err)
	}
	cfgbody, _ := json.Marshal(cfg.Model)
	budget := budgets.New(store.DB())
	stages := reviewstages.New(store.DB())
	for _, stage := range []string{"candidates", "verification"} {
		purpose := contracts.ReserveModelCallRequestPurpose("generate")
		payload := contracts.ReviewStagePayload{Candidates: &contracts.ReviewCandidates{Candidates: result.Candidates}}
		if stage == "verification" {
			purpose = "verify"
			payload = contracts.ReviewStagePayload{Verification: &contracts.ReviewVerification{Decisions: result.Decisions}}
		}
		call, err := budget.Reserve(context.Background(), contracts.ReserveModelCallRequest{ModelConfigHash: jobs.Hash(cfgbody), Lease: lease, Purpose: purpose, Ordinal: 1, RequestHash: strings.Repeat("e", 64), InputTokenLimit: 5, OutputTokenLimit: 1})
		if err != nil {
			t.Fatal(err)
		}
		call, err = budget.Dispatch(context.Background(), contracts.DispatchModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = budget.Settle(context.Background(), contracts.SettleModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision, Outcome: "succeeded", Usage: &contracts.ModelUsage{InputTokens: 5, OutputTokens: 1}}); err != nil {
			t.Fatal(err)
		}
		prompt := "review-generation-v1"
		if stage == "verification" {
			prompt = "review-verification-v1"
		}
		key := jobs.Hash([]byte(lease.InputHash + "\n" + contextHash + "\n" + stage + "\n" + prompt + "\n" + lease.Snapshot.ModelConfigVersion))
		if err = stages.Save(context.Background(), contracts.SaveReviewStageRequest{Lease: lease, Stage: contracts.SaveReviewStageRequestStage(stage), StageInputHash: key, CallId: &call.CallId, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		result.CallIds = append(result.CallIds, call.CallId)
	}
	return result
}
func TestReviewResultFenced(t *testing.T) {
	store := testsupport.NewStore(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	service := New(store.DB())
	result := preparedResult(t, store, server.URL, "valid")
	old := result
	old.Lease.LeaseToken = jobs.UUID()
	if _, err := service.Commit(context.Background(), contracts.CommitReviewRequest{Result: old}); err == nil {
		t.Fatal("old lease accepted new result")
	}
	accepted, err := service.Commit(context.Background(), contracts.CommitReviewRequest{Result: result})
	if err != nil || accepted.Outcome != "accepted" {
		t.Fatalf("valid result rejected: %+v %v", accepted, err)
	}
	replay, err := service.Commit(context.Background(), contracts.CommitReviewRequest{Result: result})
	if err != nil || replay.Outcome != "already-accepted" || replay.ResultHash != accepted.ResultHash {
		t.Fatal("result replay changed business facts")
	}
	different := result
	different.Candidates = append([]contracts.ReviewCandidate{}, result.Candidates...)
	different.Candidates[0].Title = "different"
	if _, err = service.Commit(context.Background(), contracts.CommitReviewRequest{Result: different}); err == nil {
		t.Fatal("same resultId changed content")
	}
	invalid := preparedResult(t, store, server.URL, "bad-line")
	if _, err = service.Commit(context.Background(), contracts.CommitReviewRequest{Result: invalid}); err != nil {
		t.Fatal("ordinary wrong line should be filtered, not fail", err)
	}
	var findings, filtered int64
	store.DB().Raw("SELECT count(*) FROM review_findings WHERE run_id=?", invalid.Lease.RunId).Scan(&findings)
	store.DB().Raw("SELECT count(*) FROM review_filtered_candidates WHERE run_id=?", invalid.Lease.RunId).Scan(&filtered)
	if findings != 0 || filtered != 1 {
		t.Fatal("invalid line became publishable")
	}
	cross := preparedResult(t, store, server.URL, "cross-run")
	if _, err = service.Commit(context.Background(), contracts.CommitReviewRequest{Result: cross}); err == nil {
		t.Fatal("cross-run evidence accepted")
	}
	var status string
	store.DB().Raw("SELECT status FROM review_runs WHERE id=?", cross.Lease.RunId).Scan(&status)
	if status != "Failed" {
		t.Fatal("cross scope reference did not terminate Run")
	}
	// Expired findings lose their content, while the accepted identity stays confirmable.
	store.DB().Exec("UPDATE review_findings SET expires_at=clock_timestamp()-interval '1 second' WHERE run_id=?", result.Lease.RunId)
	if _, err = retention.New(store.DB()).Cleanup(context.Background(), time.Now(), 100); err != nil {
		t.Fatal(err)
	}
	var visible int64
	store.DB().Raw("SELECT count(*) FROM review_findings WHERE run_id=? AND candidate IS NOT NULL", result.Lease.RunId).Scan(&visible)
	if visible != 0 {
		t.Fatal("expired finding content retained")
	}
	testsupport.ReviewVerdict(t, "results", map[string]any{"acceptedOnce": true, "crossScopeBlocked": true, "invalidLineFiltered": true})
}

func TestReviewResultClaimKeepsV2Separate(t *testing.T) {
	store := testsupport.NewStore(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	lease, _ := testsupport.SeedReview(t, store, server.URL)
	if err := store.DB().Exec("UPDATE jobs SET status='queued',lease_token=NULL,lease_expires_at=NULL WHERE id=?", lease.TaskId).Error; err != nil {
		t.Fatal(err)
	}
	req := contracts.ClaimReviewRequest{Envelope: contracts.ReviewTaskEnvelope{SchemaVersion: 2, MessageId: lease.LeaseToken, TaskId: lease.TaskId, RunId: lease.RunId, Tenant: lease.Tenant, HeadSha: lease.HeadSha, Kind: "ai-review", NotificationGeneration: 0}, WorkerId: "isolated-review"}
	answer, err := New(store.DB()).Claim(context.Background(), req)
	if err != nil || answer.Outcome != "claimed" || answer.Lease == nil || answer.Lease.Snapshot.ModelConfigVersion == "none" {
		t.Fatalf("v2 review claim missing: %+v %v", answer, err)
	}
	testsupport.ReviewVerdict(t, "results", map[string]any{"v2Claimed": true})
}
