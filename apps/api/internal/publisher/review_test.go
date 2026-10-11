//go:build integration

package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/testsupport"
	"strings"
	"sync"
	"testing"
)

func TestReviewSlotRace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer server.Close()
	store := testsupport.NewStore(t)
	first, gateway := testsupport.SeedReview(t, store, server.URL)
	second, _ := testsupport.SeedReview(t, store, server.URL)
	t.Cleanup(func() {
		store.DB().Exec("DELETE FROM pr_review_slots WHERE run_id IN (?,?)", first.RunId, second.RunId)
	})
	for index, l := range []contracts.ReviewLease{first, second} {
		body, _ := json.Marshal(contracts.ReviewCandidate{CandidateId: "F-1", Title: "Retry duplicates payment", Severity: "high", Category: "correctness", FailureScenario: "Response is lost", Impact: "Duplicate payment", RootCauseStatement: contracts.RootCauseStatement{Trigger: "Retry uses a new key", Consequence: "Duplicate payment"}, IntroducedByPR: true, Evidence: []contracts.EvidenceRef{}, SuggestedVerification: "Retry after timeout"})
		if err := store.DB().Exec("UPDATE jobs SET status='result_ready',lease_token=NULL,lease_expires_at=NULL WHERE id=?", l.TaskId).Error; err != nil {
			t.Fatal(err)
		}
		store.DB().Exec("UPDATE review_runs SET status='Publishing' WHERE id=?", l.RunId)
		store.DB().Exec("INSERT INTO publication_requests(run_id) VALUES(?)", l.RunId)
		if err := store.DB().Exec("INSERT INTO review_findings(id,run_id,fingerprint,root_key,candidate,evidence,expires_at) VALUES(?,?,?, '{}'::jsonb,?::jsonb,'[]',clock_timestamp()+interval '1 day')", jobs.UUID(), l.RunId, strings.Repeat(string(rune('c'+index)), 64), string(body)).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, fingerprint := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		if err := store.DB().Exec("INSERT INTO pr_review_slots(installation_id,repository_id,pr_number,fingerprint,run_id,state) VALUES(101,202,1,?,?,'published')", fingerprint, first.RunId).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := New(store.DB(), gateway, config.Config{})
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	errs := make(chan error, 2)
	for _, l := range []contracts.ReviewLease{first, second} {
		wg.Add(1)
		go func(l contracts.ReviewLease) {
			defer wg.Done()
			p, e := service.BuildReviewPlan(context.Background(), l.RunId)
			counts <- len(p.Findings)
			errs <- e
		}(l)
	}
	wg.Wait()
	close(counts)
	close(errs)
	total := 0
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for n := range counts {
		total += n
	}
	var occupied int
	store.DB().Raw("SELECT count(*) FROM pr_review_slots WHERE installation_id=101 AND repository_id=202 AND pr_number=1").Scan(&occupied)
	if occupied != 3 || total != 1 {
		t.Fatalf("slot race: occupied=%d new=%d", occupied, total)
	}
	again, e := service.BuildReviewPlan(context.Background(), first.RunId)
	if e != nil {
		t.Fatal(e)
	}
	_ = again
	testsupport.ReviewVerdict(t, "publication", map[string]any{"occupiedSlots": occupied, "newlyReserved": total})
}

func TestReviewLostResponseAndGuards(t *testing.T) {
	var mu sync.Mutex
	writes := 0
	lost := true
	staleHead := false
	revoked := false
	var remote map[string]any
	comments := []map[string]any{}
	checks := []map[string]any{}
	bot := map[string]any{"id": 900, "login": "reviewpilot-fixture[bot]", "type": "Bot"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/app":
			json.NewEncoder(w).Encode(map[string]any{"id": 77, "slug": "reviewpilot-fixture"})
		case strings.HasPrefix(r.URL.Path, "/users/"):
			json.NewEncoder(w).Encode(bot)
		case r.URL.Path == "/app/installations/101":
			json.NewEncoder(w).Encode(map[string]any{"id": 101, "app_id": 77})
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			json.NewEncoder(w).Encode(map[string]any{"token": "test", "expires_at": "2099-01-01T00:00:00Z", "permissions": map[string]string{"contents": "read", "pull_requests": "write", "checks": "write", "metadata": "read"}, "repositories": []map[string]int{{"id": 202}}})
		case r.URL.Path == "/repos/acme/web":
			if revoked {
				w.WriteHeader(403)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": 202, "full_name": "acme/web", "private": false})
		case r.URL.Path == "/repos/acme/web/pulls/1":
			head := strings.Repeat("b", 40)
			if staleHead {
				head = strings.Repeat("c", 40)
			}
			json.NewEncoder(w).Encode(map[string]any{"number": 1, "state": "open", "head": map[string]string{"sha": head}})
		case strings.HasSuffix(r.URL.Path, "/reviews"):
			if r.Method == "POST" {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				writes++
				remote = map[string]any{"id": 80, "body": body["body"], "commit_id": body["commit_id"], "state": "COMMENTED", "user": bot}
				comments = []map[string]any{}
				for index, value := range body["comments"].([]any) {
					comment := value.(map[string]any)
					comment["id"] = 100 + index
					comment["pull_request_review_id"] = 80
					comment["commit_id"] = body["commit_id"]
					comment["user"] = bot
					comments = append(comments, comment)
				}
				if lost {
					lost = false
					w.Write([]byte("{"))
					return
				}
				json.NewEncoder(w).Encode(remote)
			} else {
				rows := []map[string]any{}
				if remote != nil {
					rows = append(rows, remote)
				}
				json.NewEncoder(w).Encode(rows)
			}
		case strings.HasSuffix(r.URL.Path, "/comments"):
			json.NewEncoder(w).Encode(comments)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			if r.Method == "POST" {
				var check map[string]any
				json.NewDecoder(r.Body).Decode(&check)
				check["id"] = len(checks) + 200
				check["app"] = map[string]int{"id": 77}
				checks = append(checks, check)
				json.NewEncoder(w).Encode(check)
			} else {
				json.NewEncoder(w).Encode(map[string]any{"check_runs": checks})
			}
		default:
			t.Errorf("unexpected fixture path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	store := testsupport.NewStore(t)
	seed := func() (contracts.ReviewLease, *Service) {
		l, g := testsupport.SeedReview(t, store, server.URL)
		service := New(store.DB(), g, config.Config{})
		c := contracts.ReviewCandidate{CandidateId: "F-1", Title: "Retry duplicates payment", Severity: "high", Category: "correctness", FailureScenario: "Response is lost", Impact: "Double charge", RootCauseStatement: contracts.RootCauseStatement{Trigger: "New key", Consequence: "Double charge"}, Evidence: []contracts.EvidenceRef{}, SuggestedVerification: "Retry after timeout", IntroducedByPR: true}
		ref := contracts.EvidenceRef{EvidenceId: jobs.UUID(), Path: "src/pay.ts", Sha: l.HeadSha, StartLine: 2, EndLine: 2, ContentHash: strings.Repeat("d", 64)}
		candidate, _ := json.Marshal(c)
		evidence, _ := json.Marshal([]contracts.CodeEvidence{{Ref: ref, Tenant: l.Tenant, Snippet: "capture(randomUUID());", ReadState: "available"}})
		for _, statement := range []string{fmt.Sprintf("UPDATE jobs SET status='result_ready',lease_token=NULL,lease_expires_at=NULL WHERE id='%s'", l.TaskId), fmt.Sprintf("UPDATE review_runs SET status='Publishing' WHERE id='%s'", l.RunId)} {
			if e := store.DB().Exec(statement).Error; e != nil {
				t.Fatal(e)
			}
		}
		if e := store.DB().Exec("INSERT INTO job_results(task_id,result_id,result_hash,input_hash,payload) VALUES(?,?,?,?, '{}')", l.TaskId, l.TaskId+":review", strings.Repeat("f", 64), l.InputHash).Error; e != nil {
			t.Fatal(e)
		}
		store.DB().Exec("INSERT INTO publication_requests(run_id) VALUES(?)", l.RunId)
		if e := store.DB().Exec("INSERT INTO review_findings(id,run_id,fingerprint,root_key,candidate,evidence,expires_at) VALUES(?,?,?,'{}',?::jsonb,?::jsonb,clock_timestamp()+interval '1 day')", jobs.UUID(), l.RunId, strings.Repeat("e", 64), string(candidate), string(evidence)).Error; e != nil {
			t.Fatal(e)
		}
		store.DB().Exec("INSERT INTO review_coverage(run_id,payload) VALUES(?,'{\"complete\":true}')", l.RunId)
		return l, service
	}
	l, service := seed()
	_, _ = service.ReconcileReview(context.Background(), l.RunId)
	var state string
	store.DB().Raw("SELECT state FROM publication_requests WHERE run_id=?", l.RunId).Scan(&state)
	if state != "uncertain" {
		t.Fatal("lost response did not hold publication")
	}
	store.DB().Exec("UPDATE publication_requests SET available_at=clock_timestamp() WHERE run_id=?", l.RunId)
	receipt, e := service.ReconcileReview(context.Background(), l.RunId)
	if e != nil || receipt.ReviewId == nil || writes != 1 {
		t.Fatalf("reconciliation duplicated or lost write: %+v %v writes=%d", receipt, e, writes)
	}
	duplicate, service := seed()
	if _, e = service.ReconcileReview(context.Background(), duplicate.RunId); e != nil || writes != 1 {
		t.Fatalf("new Run resent the same root: %v writes=%d", e, writes)
	}
	staleRun, service := seed()
	staleHead = true
	if _, e = service.ReconcileReview(context.Background(), staleRun.RunId); e == nil {
		t.Fatal("stale Run produced a receipt")
	}
	var status string
	store.DB().Raw("SELECT status FROM review_runs WHERE id=?", staleRun.RunId).Scan(&status)
	if status != "Stale" || writes != 1 {
		t.Fatal("stale Head wrote review")
	}
	staleHead = false
	revokedRun, service := seed()
	revoked = true
	if _, e = service.ReconcileReview(context.Background(), revokedRun.RunId); e == nil || writes != 1 {
		t.Fatal("revoked permission wrote review")
	}
	testsupport.ReviewVerdict(t, "publication", map[string]any{"writesAfterLostResponse": writes, "repeatRootSkipped": true, "staleBlocked": true, "revokeBlocked": true})
}

func TestForeignMarkerIgnored(t *testing.T) {
	plan := contracts.ReviewPublicationPlan{RunId: jobs.UUID(), HeadSha: strings.Repeat("b", 40), Marker: "<!-- reviewpilot:owned -->", Findings: []contracts.ReviewPublicationFinding{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app":
			json.NewEncoder(w).Encode(map[string]any{"id": 77, "slug": "reviewpilot-fixture"})
		case strings.HasPrefix(r.URL.Path, "/users/"):
			json.NewEncoder(w).Encode(map[string]any{"id": 900, "login": "reviewpilot-fixture[bot]", "type": "Bot"})
		default:
			json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "commit_id": plan.HeadSha, "body": plan.Marker, "state": "COMMENTED", "user": map[string]any{"id": 901, "login": "intruder", "type": "User"}}})
		}
	}))
	defer server.Close()
	store := testsupport.NewStore(t)
	_, gateway := testsupport.SeedReview(t, store, server.URL)
	receipt, e := gateway.FindReview(context.Background(), "test", "acme/web", 1, plan)
	if e != nil || receipt != nil {
		t.Fatalf("foreign marker accepted: %+v %v", receipt, e)
	}
	testsupport.ReviewVerdict(t, "publication", map[string]any{"foreignMarkerIgnored": true})
}
