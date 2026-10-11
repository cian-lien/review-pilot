//go:build integration

package reviewstages

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/testsupport"
	"strings"
	"testing"
)

func TestReviewStagesFencingConflictAndExpiry(t *testing.T) {
	store := testsupport.NewStore(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	lease, _ := testsupport.SeedReview(t, store, server.URL)
	service := New(store.DB())
	contextHash := strings.Repeat("e", 64)
	manifestID := jobs.UUID()
	if err := store.DB().Exec("INSERT INTO review_context_manifests(id,run_id,input_hash,files,coverage,sealed,context_hash,expires_at) VALUES(?,?,?,'[]','{}',true,?,clock_timestamp()+interval '30 days')", manifestID, lease.RunId, lease.InputHash, contextHash).Error; err != nil {
		t.Fatal(err)
	}
	stageInput := jobs.Hash([]byte(lease.InputHash + "\n" + contextHash + "\ncontext\nnone\n" + lease.Snapshot.ModelConfigVersion))
	page := contracts.ReviewContextPage{ManifestId: manifestID, ContextHash: &contextHash, Complete: true, Files: []contracts.ReviewChangedFile{}, Evidence: []contracts.CodeEvidence{}, Coverage: contracts.ReviewCoverage{Complete: true, Skipped: []contracts.ReviewSkippedFile{}, Limitations: []string{}}}
	request := contracts.SaveReviewStageRequest{Lease: lease, Stage: "context", StageInputHash: stageInput, Payload: contracts.ReviewStagePayload{Context: &page}}
	if err := service.Save(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	read, err := service.Read(context.Background(), contracts.ReadReviewStageRequest{Lease: lease, Stage: "context", StageInputHash: stageInput})
	if err != nil || !read.Found || read.Record == nil || read.Record.OutputHash == "" {
		t.Fatalf("saved stage cannot be recovered: %+v %v", read, err)
	}
	page.Coverage.Complete = false
	request.Payload.Context = &page
	if err := service.Save(context.Background(), request); err == nil {
		t.Fatal("same stage key accepted conflicting content")
	}
	old := request
	old.Lease.LeaseToken = jobs.UUID()
	if err := service.Save(context.Background(), old); err == nil {
		t.Fatal("old lease wrote stage")
	}
	if err := store.DB().Exec("UPDATE review_stage_artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE run_id=?", lease.RunId).Error; err != nil {
		t.Fatal(err)
	}
	read, err = service.Read(context.Background(), contracts.ReadReviewStageRequest{Lease: lease, Stage: "context", StageInputHash: stageInput})
	if err != nil || read.Found {
		t.Fatal("expired source artifact still readable")
	}
	testsupport.ReviewVerdict(t, "stages", map[string]any{"immutable": true, "fenced": true, "expiryFiltered": true})
}
