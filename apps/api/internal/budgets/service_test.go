//go:build integration

package budgets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/retention"
	"reviewpilot.local/api/internal/testsupport"
	"strings"
	"sync"
	"testing"
	"time"
)

func modelHash(t *testing.T) string {
	t.Helper()
	cfg, err := config.LoadReview()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(cfg.Model)
	return jobs.Hash(body)
}
func TestBudgetReducedWindowLimitCannotReuseOldCeiling(t *testing.T) {
	store := testsupport.NewStore(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	first, _ := testsupport.SeedReview(t, store, server.URL)
	second, _ := testsupport.SeedReview(t, store, server.URL)
	service := New(store.DB())
	request := contracts.ReserveModelCallRequest{ModelConfigHash: modelHash(t), Lease: first, Purpose: "generate", Ordinal: 1, RequestHash: strings.Repeat("a", 64), InputTokenLimit: 5, OutputTokenLimit: 1}
	if _, e := service.Reserve(context.Background(), request); e != nil {
		t.Fatal(e)
	}
	cfg, e := config.LoadReview()
	if e != nil {
		t.Fatal(e)
	}
	cfg.Model.ModelConfigVersion += "-reduced"
	cfg.Model.GlobalMonthlyLimitMicros = "6"
	body, _ := json.Marshal(cfg.Model)
	if e = os.WriteFile(os.Getenv("REVIEW_MODEL_CONFIG_FILE"), body, 0600); e != nil {
		t.Fatal(e)
	}
	second.Snapshot.ModelConfigVersion = cfg.Model.ModelConfigVersion
	body, _ = json.Marshal(second.Snapshot)
	if e = store.DB().Exec("UPDATE review_runs SET snapshot=?::jsonb WHERE id=?", string(body), second.RunId).Error; e != nil {
		t.Fatal(e)
	}
	var stored struct{ Snapshot []byte }
	if e = store.DB().Raw("SELECT snapshot FROM review_runs WHERE id=?", second.RunId).Scan(&stored).Error; e != nil {
		t.Fatal(e)
	}
	second.InputHash = jobs.Hash(stored.Snapshot)
	request.Lease = second
	request.ModelConfigHash = modelHash(t)
	if _, e = service.Reserve(context.Background(), request); e == nil || e.Error() != "REVIEW_BUDGET_EXHAUSTED" {
		t.Fatal("new six-micro ceiling ignored the existing six-micro hold", e)
	}
}
func TestBudgetRejectsConfigurationDriftBeforeReservation(t *testing.T) {
	store := testsupport.NewStore(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	lease, _ := testsupport.SeedReview(t, store, server.URL)
	req := contracts.ReserveModelCallRequest{ModelConfigHash: strings.Repeat("0", 64), Lease: lease, Purpose: "generate", Ordinal: 1, RequestHash: strings.Repeat("a", 64), InputTokenLimit: 5, OutputTokenLimit: 1}
	if _, err := New(store.DB()).Reserve(context.Background(), req); err == nil {
		t.Fatal("configuration drift must fail before reservation")
	}
	var count int64
	if err := store.DB().Raw("SELECT count(*) FROM model_calls WHERE run_id=?", lease.RunId).Scan(&count).Error; err != nil || count != 0 {
		t.Fatal("mismatch created model facts")
	}
}
func TestBudgetRaceAndUnknownUsage(t *testing.T) {
	store := testsupport.NewStore(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	first, _ := testsupport.SeedReview(t, store, server.URL)
	second, _ := testsupport.SeedReview(t, store, server.URL)
	body, err := os.ReadFile(os.Getenv("REVIEW_MODEL_CONFIG_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	var model config.ReviewModelConfig
	if err = json.Unmarshal(body, &model); err != nil {
		t.Fatal(err)
	}
	model.GlobalMonthlyLimitMicros = "10"
	body, _ = json.Marshal(model)
	if err = os.WriteFile(os.Getenv("REVIEW_MODEL_CONFIG_FILE"), body, 0600); err != nil {
		t.Fatal(err)
	}
	service := New(store.DB())
	mutex := sync.Mutex{}
	successful := []struct {
		call  contracts.ModelCall
		lease contracts.ReviewLease
	}{}
	var wait sync.WaitGroup
	for _, lease := range []contracts.ReviewLease{first, second} {
		wait.Add(1)
		go func(l contracts.ReviewLease) {
			defer wait.Done()
			call, err := service.Reserve(context.Background(), contracts.ReserveModelCallRequest{ModelConfigHash: modelHash(t), Lease: l, Purpose: "generate", Ordinal: 1, RequestHash: strings.Repeat("a", 64), InputTokenLimit: 5, OutputTokenLimit: 1})
			if err == nil {
				mutex.Lock()
				successful = append(successful, struct {
					call  contracts.ModelCall
					lease contracts.ReviewLease
				}{call, l})
				mutex.Unlock()
			}
		}(lease)
	}
	wait.Wait()
	if len(successful) != 1 || successful[0].call.ReservedCostMicros != "6" {
		t.Fatalf("10-micro budget must admit only one six-micro reservation: %+v", successful)
	}
	call, lease := successful[0].call, successful[0].lease
	dispatched, err := service.Dispatch(context.Background(), contracts.DispatchModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision})
	if err != nil || !dispatched.SendAuthorized {
		t.Fatalf("first dispatch must authorize once: %+v %v", dispatched, err)
	}
	again, err := service.Dispatch(context.Background(), contracts.DispatchModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision})
	if err != nil || again.SendAuthorized {
		t.Fatal("repeated dispatch grants another send")
	}
	unknown, err := service.Settle(context.Background(), contracts.SettleModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: dispatched.Revision, Outcome: "uncertain"})
	if err != nil || unknown.State != "uncertain" {
		t.Fatalf("unknown usage did not retain reservation: %+v %v", unknown, err)
	}
	var held int64
	if err = store.DB().Raw("SELECT held_micros FROM budget_limits WHERE scope_type='global'").Scan(&held).Error; err != nil || held != 6 {
		t.Fatalf("unknown charge was refunded: %d %v", held, err)
	}
	if _, err = service.Reserve(context.Background(), contracts.ReserveModelCallRequest{ModelConfigHash: modelHash(t), Lease: lease, Purpose: "generate", Ordinal: 2, RequestHash: strings.Repeat("b", 64), InputTokenLimit: 5, OutputTokenLimit: 1}); err == nil {
		t.Fatal("unknown call automatically retried")
	}
	if _, err = config.ParseMicros("9223372036854775808"); err == nil {
		t.Fatal("money overflow accepted")
	}
	if _, err = retention.New(store.DB()).Cleanup(context.Background(), time.Now().Add(31*24*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	store.DB().Raw("SELECT held_micros FROM budget_limits WHERE scope_type='global'").Scan(&held)
	if held != 6 {
		t.Fatal("content TTL released an unknown provider charge")
	}
	if err = store.DB().Exec("UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=?", lease.TaskId).Error; err != nil {
		t.Fatal(err)
	}
	if err = jobs.New(store.DB()).Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	var notifications int64
	store.DB().Raw("SELECT count(*) FROM outbox_events WHERE task_id=?", lease.TaskId).Scan(&notifications)
	if notifications != 0 {
		t.Fatal("blocked Run was automatically rescheduled")
	}
	testsupport.ReviewVerdict(t, "budget", map[string]any{"successfulReservations": len(successful), "unknownReservationHeld": held == 6})
}

func TestSettledCallWithoutArtifactBlocksRecovery(t *testing.T) {
	store := testsupport.NewStore(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	lease, _ := testsupport.SeedReview(t, store, server.URL)
	service := New(store.DB())
	call, err := service.Reserve(context.Background(), contracts.ReserveModelCallRequest{ModelConfigHash: modelHash(t), Lease: lease, Purpose: "generate", Ordinal: 1, RequestHash: strings.Repeat("c", 64), InputTokenLimit: 5, OutputTokenLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	call, err = service.Dispatch(context.Background(), contracts.DispatchModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision})
	if err != nil {
		t.Fatal(err)
	}
	call, err = service.Settle(context.Background(), contracts.SettleModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision, Outcome: "succeeded", Usage: &contracts.ModelUsage{InputTokens: 2, OutputTokens: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Reserve(context.Background(), contracts.ReserveModelCallRequest{ModelConfigHash: modelHash(t), Lease: lease, Purpose: "generate", Ordinal: 2, RequestHash: strings.Repeat("d", 64), InputTokenLimit: 5, OutputTokenLimit: 1})
	if err == nil {
		t.Fatal("charged call without an artifact generated again")
	}
	var blocked bool
	store.DB().Raw("SELECT needs_reconciliation FROM review_runs WHERE id=?", lease.RunId).Scan(&blocked)
	if !blocked {
		t.Fatal("recovery gap did not create a durable scheduler block")
	}
}

func TestBudgetSettlementIsIdempotent(t *testing.T) {
	store := testsupport.NewStore(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	service := New(store.DB())
	for _, outcome := range []contracts.SettleModelCallRequestOutcome{"succeeded", "uncertain"} {
		lease, _ := testsupport.SeedReview(t, store, server.URL)
		call, err := service.Reserve(context.Background(), contracts.ReserveModelCallRequest{ModelConfigHash: modelHash(t), Lease: lease, Purpose: "generate", Ordinal: 1, RequestHash: strings.Repeat("e", 64), InputTokenLimit: 5, OutputTokenLimit: 1})
		if err != nil {
			t.Fatal(err)
		}
		call, err = service.Dispatch(context.Background(), contracts.DispatchModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision})
		if err != nil {
			t.Fatal(err)
		}
		request := contracts.SettleModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision, Outcome: outcome}
		if outcome == "succeeded" {
			request.Usage = &contracts.ModelUsage{InputTokens: 2, OutputTokens: 1}
		}
		settled, err := service.Settle(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		repeated, err := service.Settle(context.Background(), request)
		if err != nil || repeated.Revision != settled.Revision || repeated.State != settled.State {
			t.Fatalf("identical %s settlement conflicted: %+v %v", outcome, repeated, err)
		}
	}
}

func TestBudgetOperatorReconciliationAndScopedDeletion(t *testing.T) {
	store := testsupport.NewStore(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	lease, _ := testsupport.SeedReview(t, store, server.URL)
	service := New(store.DB())
	call, err := service.Reserve(context.Background(), contracts.ReserveModelCallRequest{ModelConfigHash: modelHash(t), Lease: lease, Purpose: "generate", Ordinal: 1, RequestHash: strings.Repeat("f", 64), InputTokenLimit: 5, OutputTokenLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	call, err = service.Dispatch(context.Background(), contracts.DispatchModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision})
	if err != nil {
		t.Fatal(err)
	}
	call, err = service.Settle(context.Background(), contracts.SettleModelCallRequest{Lease: lease, CallId: call.CallId, ExpectedRevision: call.Revision, Outcome: "uncertain"})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Reconcile(context.Background(), ReconcileRequest{CallID: call.CallId, ExpectedRevision: call.Revision, Outcome: "charged", ReceiptRef: "receipt-fixture-1", ActualCostMicros: "3", Currency: "TEST"}); err != nil {
		t.Fatal(err)
	}
	var blocked bool
	store.DB().Raw("SELECT needs_reconciliation FROM review_runs WHERE id=?", lease.RunId).Scan(&blocked)
	if !blocked {
		t.Fatal("a receipt without retry approval restarted generation")
	}
	if err = service.Reconcile(context.Background(), ReconcileRequest{CallID: call.CallId, ExpectedRevision: call.Revision + 1, Outcome: "charged", ReceiptRef: "receipt-fixture-2", ActualCostMicros: "3", Currency: "TEST", RetryStage: true}); err != nil {
		t.Fatal(err)
	}
	store.DB().Raw("SELECT needs_reconciliation FROM review_runs WHERE id=?", lease.RunId).Scan(&blocked)
	if blocked {
		t.Fatal("explicit bounded retry approval did not unblock the task")
	}
	// Keep a separate unknown call to verify that repository deletion cannot refund it.
	unknownLease, _ := testsupport.SeedReview(t, store, server.URL)
	unknown, err := service.Reserve(context.Background(), contracts.ReserveModelCallRequest{ModelConfigHash: modelHash(t), Lease: unknownLease, Purpose: "generate", Ordinal: 1, RequestHash: strings.Repeat("a", 64), InputTokenLimit: 5, OutputTokenLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err = service.Dispatch(context.Background(), contracts.DispatchModelCallRequest{Lease: unknownLease, CallId: unknown.CallId, ExpectedRevision: unknown.Revision})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err = service.Settle(context.Background(), contracts.SettleModelCallRequest{Lease: unknownLease, CallId: unknown.CallId, ExpectedRevision: unknown.Revision, Outcome: "uncertain"})
	if err != nil {
		t.Fatal(err)
	}
	otherRun := jobs.UUID()
	otherManifest := jobs.UUID()
	for _, statement := range []string{"INSERT INTO installations(id) VALUES(303)", "INSERT INTO repositories(installation_id,id,full_name,public,enabled) VALUES(303,202,'other/web',true,true)"} {
		if err = store.DB().Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = store.DB().Exec("INSERT INTO review_runs(id,installation_id,repository_id,pr_number,base_sha,head_sha,pipeline_version,generation,snapshot) VALUES(?,303,202,1,repeat('a',40),repeat('b',40),'review-v1',0,'{}')", otherRun).Error; err != nil {
		t.Fatal(err)
	}
	if err = store.DB().Exec("INSERT INTO review_context_manifests(id,run_id,input_hash,files,pages,coverage,expires_at) VALUES(?,?,repeat('a',64),'[]','[{\"snippet\":\"other-tenant-content\"}]','{}',clock_timestamp()+interval '30 days')", otherManifest, otherRun).Error; err != nil {
		t.Fatal(err)
	}
	if err = retention.New(store.DB()).DeleteRepository(context.Background(), lease.Tenant); err != nil {
		t.Fatal(err)
	}
	var held int64
	store.DB().Raw("SELECT held_micros FROM budget_limits WHERE scope_type='global' AND scope_key LIKE ?", "%"+lease.Snapshot.Provenance.FixtureId).Scan(&held)
	if held != 6 {
		t.Fatalf("deletion refunded an unknown charge: %d", held)
	}
	var anonymous int64
	store.DB().Raw("SELECT count(*) FROM model_calls WHERE call_id=? AND run_id IS NULL AND state='uncertain'", unknown.CallId).Scan(&anonymous)
	var otherContent string
	store.DB().Raw("SELECT pages::text FROM review_context_manifests WHERE id=?", otherManifest).Scan(&otherContent)
	if anonymous != 1 || !strings.Contains(otherContent, "other-tenant-content") {
		t.Fatal("deletion lost anonymous cost or crossed installation boundary")
	}
}
