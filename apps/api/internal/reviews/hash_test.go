package reviews

import (
	"encoding/json"
	"os"
	"reviewpilot.local/api/internal/contracts"
	"strings"
	"testing"
)

func TestReviewBusinessHashFixture(t *testing.T) {
	data, err := os.ReadFile("../../../../tests/fixtures/review/golden/result-hash.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Business     BusinessResult `json:"business"`
		ExpectedHash string         `json:"expectedHash"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	b := fixture.Business
	r := contracts.ReviewResult{Kind: "ai-review", Lease: contracts.ReviewLease{RunId: b.RunID, InputHash: b.InputHash, LeaseToken: "first"}, ResultId: b.ResultID, ContextHash: b.ContextHash, Candidates: b.Candidates, Decisions: b.Decisions, Coverage: b.Coverage, CallIds: b.CallIDs}
	if hash := BusinessHash(r); hash != fixture.ExpectedHash {
		t.Fatalf("canonical unicode/escaping hash mismatch: %s", hash)
	}
	r.Lease.LeaseToken = "different"
	r.Lease.ExecutionEpoch = 99
	if BusinessHash(r) != fixture.ExpectedHash {
		t.Fatal("transport lease entered business hash")
	}
	body, _ := json.Marshal(Business(r))
	if strings.Contains(string(body), "leaseToken") || strings.Contains(string(body), "different") {
		t.Fatal("lease credential persisted")
	}
	r.Coverage.Complete = false
	if BusinessHash(r) == fixture.ExpectedHash {
		t.Fatal("changed business content retained hash")
	}
}
