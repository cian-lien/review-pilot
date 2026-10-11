//go:build integration

package reviews

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"reviewpilot.local/api/internal/auth"
	"reviewpilot.local/api/internal/budgets"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/messaging"
	"reviewpilot.local/api/internal/observability"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/publisher"
	"reviewpilot.local/api/internal/reviewcontext"
	"reviewpilot.local/api/internal/reviewstages"
	"reviewpilot.local/api/internal/testsupport"
	"reviewpilot.local/api/internal/webhooks"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type pipelineRemote struct {
	mu                        sync.Mutex
	head                      string
	reviews, comments, checks []map[string]any
	writes                    int
}

func (f *pipelineRemote) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	bot := map[string]any{"id": 900, "login": "reviewpilot-fixture[bot]", "type": "Bot"}
	encode := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.URL.Path == "/app":
		encode(map[string]any{"id": 77, "slug": "reviewpilot-fixture"})
	case strings.HasPrefix(r.URL.Path, "/users/"):
		encode(bot)
	case r.URL.Path == "/app/installations/101":
		encode(map[string]any{"id": 101, "app_id": 77})
	case strings.HasSuffix(r.URL.Path, "/access_tokens"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		encode(map[string]any{"token": "isolated-installation", "expires_at": "2099-01-01T00:00:00Z", "permissions": body["permissions"], "repositories": []map[string]int{{"id": 202}}})
	case r.URL.Path == "/user/installations":
		encode(map[string]any{"installations": []map[string]int{{"id": 101, "app_id": 77}}})
	case strings.HasPrefix(r.URL.Path, "/user/installations/101/"):
		encode(map[string]any{"repositories": []map[string]any{{"id": 202, "full_name": "acme/web", "private": false}}})
	case r.URL.Path == "/repos/acme/web":
		encode(map[string]any{"id": 202, "full_name": "acme/web", "private": false, "permissions": map[string]bool{"maintain": true, "pull": true}})
	case r.URL.Path == "/repos/acme/web/pulls/1":
		encode(map[string]any{"number": 1, "state": "open", "base": map[string]string{"sha": strings.Repeat("a", 40)}, "head": map[string]string{"sha": f.head}})
	case strings.HasSuffix(r.URL.Path, "/files"):
		encode([]map[string]any{{"filename": "pay.ts", "status": "modified", "additions": 1, "deletions": 1, "patch": "@@ -1 +1 @@\n-export const key = order.key;\n+export const key = crypto.randomUUID();"}})
	case strings.Contains(r.URL.Path, "/git/commits/"):
		encode(map[string]any{"tree": map[string]string{"sha": strings.Repeat("e", 40)}})
	case strings.Contains(r.URL.Path, "/git/trees/"):
		encode(map[string]any{"truncated": false, "tree": []map[string]string{{"path": "pay.ts", "mode": "100644", "type": "blob", "sha": strings.Repeat("f", 40)}}})
	case strings.Contains(r.URL.Path, "/contents/"):
		value := "export const key = crypto.randomUUID();\n"
		if r.URL.Query().Get("ref") == strings.Repeat("a", 40) {
			value = "export const key = order.key;\n"
		}
		_, _ = w.Write([]byte(value + "// api_key=\"sk-reviewpilot-source-SENTINEL123456789\"\n"))
	case strings.HasSuffix(r.URL.Path, "/reviews"):
		if r.Method == "POST" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.writes++
			id := 80 + len(f.reviews)
			review := map[string]any{"id": id, "body": body["body"], "commit_id": body["commit_id"], "state": "COMMENTED", "user": bot}
			f.reviews = append(f.reviews, review)
			for i, v := range body["comments"].([]any) {
				c := v.(map[string]any)
				c["id"] = 100 + i
				c["pull_request_review_id"] = id
				c["commit_id"] = body["commit_id"]
				c["user"] = bot
				f.comments = append(f.comments, c)
			}
			encode(review)
		} else {
			encode(f.reviews)
		}
	case strings.HasSuffix(r.URL.Path, "/comments"):
		encode(f.comments)
	case strings.HasSuffix(r.URL.Path, "/check-runs"):
		if r.Method == "POST" {
			var c map[string]any
			_ = json.NewDecoder(r.Body).Decode(&c)
			c["id"] = 200 + len(f.checks)
			c["app"] = map[string]int{"id": 77}
			f.checks = append(f.checks, c)
			encode(c)
		} else {
			encode(map[string]any{"check_runs": f.checks})
		}
	default:
		w.WriteHeader(404)
	}
}

type pipelineOutput struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *pipelineOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *pipelineOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }

func TestReviewPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	store := testsupport.NewStore(t)
	db := store.DB()
	remote := &pipelineRemote{head: strings.Repeat("b", 40), reviews: []map[string]any{}, comments: []map[string]any{}, checks: []map[string]any{}}
	githubServer := httptest.NewServer(remote)
	defer githubServer.Close()
	seeded, gateway := testsupport.SeedReview(t, store, githubServer.URL)
	// Keep only seeded authority/configuration; every measured Run must come from a signed webhook.
	if e := db.Exec("DELETE FROM jobs WHERE run_id=?", seeded.RunId).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("DELETE FROM review_runs WHERE id=?", seeded.RunId).Error; e != nil {
		t.Fatal(e)
	}
	reviewCfg, e := config.LoadReview()
	if e != nil {
		t.Fatal(e)
	}
	directory, endpoint := pipelineCollector(t)
	apiObs, e := observability.NewRuntime(ctx, "api", endpoint)
	if e != nil {
		t.Fatal(e)
	}
	defer apiObs.Close(context.Background())
	dispatcherObs, e := observability.NewRuntime(ctx, "dispatcher", endpoint)
	if e != nil {
		t.Fatal(e)
	}
	defer dispatcherObs.Close(context.Background())
	const sentinel = "TEST-CREDENTIAL-SENTINEL-S2"
	cfg := gateway.Config
	cfg.SessionSecret = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))
	cfg.GitHubWebhookSecret = "isolated-webhook"
	cfg.ReviewWorkerServiceToken = strings.Repeat("r", 40) + sentinel
	internal := httpapi.NewRouter(apiObs)
	reviewcontext.Register(internal, reviewcontext.New(db, gateway, reviewCfg.Policy), cfg)
	budgets.Register(internal, budgets.New(db), cfg)
	reviewstages.Register(internal, reviewstages.New(db), cfg)
	Register(internal, New(db), cfg)
	var lostCommit bool
	var acceptedClaims, confirmedResults, blockedActions int
	var proxyMu sync.Mutex
	internalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		internal.ServeHTTP(recorder, r)
		proxyMu.Lock()
		if recorder.Code == 200 && strings.HasSuffix(r.URL.Path, "/claim") && bytes.Contains(recorder.Body.Bytes(), []byte(`"accepted"`)) {
			acceptedClaims++
		}
		if recorder.Code == 200 && r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/result") {
			confirmedResults++
		}
		if recorder.Code == 204 && strings.HasSuffix(r.URL.Path, "/block") {
			blockedActions++
		}
		drop := !lostCommit && r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/result") && recorder.Code == 200
		if drop {
			lostCommit = true
		}
		proxyMu.Unlock()
		for k, v := range recorder.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(recorder.Code)
		if drop {
			_, _ = w.Write([]byte("{"))
		} else {
			_, _ = w.Write(recorder.Body.Bytes())
		}
	}))
	defer internalServer.Close()
	public := httpapi.NewRouter(apiObs)
	hook := webhooks.New(db, "foundation-v1")
	hook.Review = &reviewCfg
	webhooks.Register(public, hook, cfg)
	authService := auth.New(store, cfg, gateway)
	RegisterQueries(public, NewQuery(db, authService))
	publicServer := httptest.NewServer(public)
	defer publicServer.Close()
	sealed, e := auth.SealToken("isolated-user", cfg.SessionSecret)
	if e != nil {
		t.Fatal(e)
	}
	opaque := strings.Repeat("x", 43)
	for _, stmt := range []struct {
		q    string
		args []any
	}{{"INSERT INTO users(id,login) VALUES(42,'fixture')", nil}, {"INSERT INTO sessions(token_hash,user_id,encrypted_user_token,expires_at) VALUES(?,42,?,clock_timestamp()+interval '1 hour')", []any{auth.Hash(opaque), sealed}}} {
		if e = db.Exec(stmt.q, stmt.args...).Error; e != nil {
			t.Fatal(e)
		}
	}
	rabbit := messaging.NewRabbit(os.Getenv("TEST_RABBITMQ_URL"))
	defer rabbit.Close()
	relay := &messaging.Relay{DB: db, Publisher: rabbit, Observer: dispatcherObs}
	publications := publisher.New(db, gateway, cfg)
	var allOutput strings.Builder
	var activeOutput *pipelineOutput
	startWorker := func(unknown bool) func() {
		t.Helper()
		candidate := map[string]any{"candidateId": "c1", "title": "Retry may charge twice", "severity": "high", "category": "data-integrity", "failureScenario": "Lost response regenerates key", "impact": "Duplicate capture", "introducedByPR": true, "evidence": []any{}, "suggestedVerification": "Retry lost response", "rootCauseStatement": map[string]string{"trigger": "retry creates a new key", "consequence": "provider charges twice"}}
		generate := map[string]any{"purpose": "generate", "evidenceFromInput": true, "data": map[string]any{"candidates": []any{candidate}}}
		responses := []any{generate}
		if !unknown {
			generate["usage"] = map[string]int{"inputTokens": 10, "outputTokens": 10}
			responses = append(responses, map[string]any{"purpose": "verify", "evidenceFromInput": true, "usage": map[string]int{"inputTokens": 10, "outputTokens": 10}, "data": map[string]any{"decisions": []any{map[string]any{"candidateId": "c1", "verdict": "verified", "reasonCode": "EVIDENCE_SUPPORTED", "evidence": []any{}}}}})
		}
		raw, _ := json.Marshal(map[string]any{"fixtureId": reviewCfg.Model.FixtureID, "responses": responses})
		script := filepath.Join(t.TempDir(), "fake.json")
		if e = os.WriteFile(script, raw, 0600); e != nil {
			t.Fatal(e)
		}
		command := exec.Command("pnpm", "exec", "turbo", "run", "worker:review", "--filter=@reviewpilot/worker-review")
		command.Dir = filepath.Clean(filepath.Join(os.Getenv("MIGRATION_DIRECTORY"), "../../.."))
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "LANG=" + os.Getenv("LANG"), "TMPDIR=" + os.Getenv("TMPDIR"), "REVIEW_TEST_ISOLATED=1", "REVIEW_MODE=fake", "REVIEW_FIXTURE_ID=" + reviewCfg.Model.FixtureID, "REVIEW_MODEL_CONFIG_FILE=" + os.Getenv("REVIEW_MODEL_CONFIG_FILE"), "TEST_MODEL_BASE_URL=" + reviewCfg.Model.BaseURL, "REVIEW_FAKE_SCRIPT_FILE=" + script, "MODEL_API_KEY=" + sentinel, "RABBITMQ_URL=" + os.Getenv("TEST_RABBITMQ_URL"), "INTERNAL_API_URL=" + internalServer.URL, "REVIEW_WORKER_SERVICE_TOKEN=" + cfg.ReviewWorkerServiceToken, "OTEL_EXPORTER_OTLP_ENDPOINT=" + endpoint}
		output := &pipelineOutput{}
		activeOutput = output
		command.Stdout = output
		command.Stderr = output
		if e = command.Start(); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		var once sync.Once
		stop := func() {
			once.Do(func() {
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
				select {
				case <-done:
				case <-time.After(4 * time.Second):
					_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
					<-done
				}
				allOutput.WriteString(output.String())
			})
		}
		t.Cleanup(stop)
		return stop
	}
	enqueue := func(letter string, sequence int) (persistence.Run, string) {
		t.Helper()
		remote.mu.Lock()
		remote.head = strings.Repeat(letter, 40)
		remote.mu.Unlock()
		raw := []byte(fmt.Sprintf(`{"action":"opened","installation":{"id":101},"repository":{"id":202,"full_name":"acme/web","private":false},"pull_request":{"number":1,"title":"Prevent duplicate capture","updated_at":"2026-10-11T00:00:0%dZ","base":{"sha":"%s"},"head":{"sha":"%s"}}}`, sequence, strings.Repeat("a", 40), strings.Repeat(letter, 40)))
		req, _ := http.NewRequestWithContext(ctx, "POST", publicServer.URL+"/api/github/webhook", bytes.NewReader(raw))
		mac := hmac.New(sha256.New, []byte(cfg.GitHubWebhookSecret))
		mac.Write(raw)
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		req.Header.Set("X-GitHub-Event", "pull_request")
		req.Header.Set("X-GitHub-Delivery", "pipeline-"+letter)
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 202 {
			t.Fatal("webhook", res.StatusCode, string(body))
		}
		var run persistence.Run
		if e = db.Where("head_sha=?", strings.Repeat(letter, 40)).First(&run).Error; e != nil {
			t.Fatal(e)
		}
		var task string
		if e = db.Raw("SELECT id FROM jobs WHERE run_id=?", run.ID).Scan(&task).Error; e != nil {
			t.Fatal(e)
		}
		if e = relay.Flush(ctx, 10); e != nil {
			t.Fatal(e)
		}
		return run, task
	}
	wait := func(task string, blocked bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if e := relay.Flush(ctx, 10); e != nil {
				t.Fatal("relay", e)
			}
			var facts struct {
				Status              string
				NeedsReconciliation bool
			}
			if e = db.Raw("SELECT j.status,r.needs_reconciliation FROM jobs j JOIN review_runs r ON r.id=j.run_id WHERE j.id=?", task).Scan(&facts).Error; e != nil {
				t.Fatal(e)
			}
			if (!blocked && facts.Status == "result_ready") || (blocked && facts.NeedsReconciliation) {
				return
			}
			if facts.Status == "failed" {
				t.Fatal("worker failed", allOutput.String())
			}
			time.Sleep(100 * time.Millisecond)
		}
		var facts struct {
			Status, LastErrorCode, ReasonCode string
			NeedsReconciliation               bool
		}
		db.Raw("SELECT j.status,coalesce(j.last_error_code,'') AS last_error_code,coalesce(r.reason_code,'') AS reason_code,r.needs_reconciliation FROM jobs j JOIN review_runs r ON r.id=j.run_id WHERE j.id=?", task).Scan(&facts)
		t.Fatal("worker did not persist expected facts", facts, strings.ReplaceAll(activeOutput.String(), sentinel, "[test-secret]"))
	}
	stop := startWorker(false)
	normal, task := enqueue("b", 0)
	wait(task, false)
	if _, e = publications.ReconcileReview(ctx, normal.ID); e != nil {
		t.Fatal("publication", e)
	}
	// Replay the actual v2 Outbox notification, not a synthetic direct engine invocation.
	var message struct {
		ID, Kind string
		Payload  []byte
	}
	if e = db.Raw("SELECT id,kind,payload FROM outbox_events WHERE payload->>'runId'=? ORDER BY notification_generation LIMIT 1", normal.ID).Scan(&message).Error; e != nil {
		t.Fatal(e)
	}
	if e = rabbit.Publish(ctx, message.Kind, message.ID, message.Payload); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		proxyMu.Lock()
		observed := acceptedClaims > 0 && confirmedResults > 0
		proxyMu.Unlock()
		if observed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	proxyMu.Lock()
	replayed := acceptedClaims > 0 && confirmedResults > 0
	proxyMu.Unlock()
	if !replayed {
		t.Fatal("duplicate claim or lost-result confirmation was not observed")
	}
	if e = apiObs.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	if e = dispatcherObs.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(filepath.Join(directory, "traces.json"))
		if pipelineTraceLinked(raw, normal.ID, task) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()
	var accepted, calls, published int
	db.Raw("SELECT count(*) FROM job_results WHERE task_id=?", task).Scan(&accepted)
	db.Raw("SELECT count(*) FROM model_calls WHERE run_id=?", normal.ID).Scan(&calls)
	db.Raw("SELECT count(*) FROM pr_review_slots WHERE run_id=? AND state='published'", normal.ID).Scan(&published)
	if accepted != 1 || calls != 2 || published != 1 || !lostCommit {
		t.Fatalf("normal persisted facts: accepted=%d calls=%d published=%d lost=%v output=%s", accepted, calls, published, lostCommit, allOutput.String())
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", publicServer.URL+"/api/repositories/101/202/pulls/1/runs/"+normal.ID, nil)
	req.AddCookie(&http.Cookie{Name: "reviewpilot_session", Value: opaque})
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	detailRaw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var detail contracts.ReviewRunDetail
	if res.StatusCode != 200 || contracts.DecodeReview("ReviewRunDetail", detailRaw, &detail, 262144) != nil || len(detail.Findings) != 1 || detail.NeedsReconciliation {
		t.Fatal("public DTO did not expose accepted review", res.StatusCode, string(detailRaw))
	}
	stop = startWorker(true)
	unknown, unknownTask := enqueue("c", 1)
	wait(unknownTask, true)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		proxyMu.Lock()
		blocked := blockedActions > 0
		proxyMu.Unlock()
		if blocked {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	proxyMu.Lock()
	blocked := blockedActions > 0
	proxyMu.Unlock()
	if !blocked {
		t.Fatal("unknown usage was not durably acknowledged via block REST")
	}
	stop()
	var held bool
	db.Raw("SELECT EXISTS(SELECT 1 FROM model_calls c JOIN budget_reservations r ON r.id=c.reservation_id WHERE c.run_id=? AND c.state='uncertain' AND r.state='held' AND r.reserved_micros>0)", unknown.ID).Scan(&held)
	if !held {
		t.Fatal("unknown usage released its reservation")
	}
	stop = startWorker(false)
	staleRun, staleTask := enqueue("d", 2)
	wait(staleTask, false)
	stop()
	remote.mu.Lock()
	remote.head = strings.Repeat("e", 40)
	writes := remote.writes
	remote.mu.Unlock()
	_, _ = publications.ReconcileReview(ctx, staleRun.ID)
	var status string
	db.Raw("SELECT status FROM review_runs WHERE id=?", staleRun.ID).Scan(&status)
	remote.mu.Lock()
	unchanged := remote.writes == writes
	remote.mu.Unlock()
	if status != "Stale" || !unchanged {
		t.Fatal("changed Head published")
	}
	if e = apiObs.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	if e = dispatcherObs.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	traces, _ := os.ReadFile(filepath.Join(directory, "traces.json"))
	logs, _ := os.ReadFile(filepath.Join(directory, "logs.json"))
	metrics, _ := os.ReadFile(filepath.Join(directory, "metrics.json"))
	for _, name := range []string{"review.model.calls", "review.model.unknown", "review.cost.micros"} {
		if !bytes.Contains(metrics, []byte(name)) {
			t.Fatal("persisted accounting metric not exported", name)
		}
	}
	if !pipelineTraceLinked(traces, normal.ID, task) {
		t.Fatal("cross-service trace is disconnected")
	}
	// Check the exported boundary and persisted redacted artifacts, never the credential fixtures themselves.
	var artifacts string
	db.Raw("SELECT coalesce(string_agg(pages::text,' '),'') FROM review_context_manifests").Scan(&artifacts)
	captured := string(traces) + string(logs) + string(metrics) + allOutput.String() + artifacts + string(detailRaw)
	if strings.Contains(captured, sentinel) || strings.Contains(captured, "sk-reviewpilot-source-SENTINEL123456789") {
		t.Fatal("secret crossed safe boundary")
	}
	testsupport.ReviewVerdict(t, "pipeline", map[string]any{"acceptedResultCount": accepted, "modelCallCount": calls, "publishedRootCauseCount": published, "unknownReservationHeld": held, "stalePublishBlocked": true, "traceLinked": true, "secretSentinelAbsent": true})
}

func pipelineCollector(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	config := "receivers:\n  otlp:\n    protocols:\n      http:\n        endpoint: 0.0.0.0:4318\nexporters:\n  file/traces:\n    path: /observe/traces.json\n  file/logs:\n    path: /observe/logs.json\n  file/metrics:\n    path: /observe/metrics.json\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [file/traces]\n    logs:\n      receivers: [otlp]\n      exporters: [file/logs]\n    metrics:\n      receivers: [otlp]\n      exporters: [file/metrics]\n"
	if e := os.WriteFile(filepath.Join(directory, "collector.yaml"), []byte(config), 0644); e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, e := exec.CommandContext(ctx, "docker", "run", "-d", "--label", "reviewpilot.test-owned=true", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "-p", fmt.Sprintf("127.0.0.1:%d:4318", port), "-v", directory+":/observe", "otel/opentelemetry-collector-contrib:0.161.0", "--config=/observe/collector.yaml").CombinedOutput()
	if e != nil {
		t.Fatal("owned Collector unavailable", e)
	}
	id := strings.TrimSpace(string(out))
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(id) {
		t.Fatal("invalid owned Collector")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", "-v", id).Run()
	})
	deadline := time.Now().Add(10 * time.Second)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, e := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if e == nil {
			conn.Close()
			return directory, "http://" + address
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("owned Collector not ready")
	return "", ""
}
func pipelineTraceLinked(raw []byte, runID, taskID string) bool {
	services := map[string]map[string]bool{}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var batch struct {
			ResourceSpans []struct {
				Resource struct {
					Attributes []struct {
						Key   string
						Value struct{ StringValue string }
					}
				}
				ScopeSpans []struct {
					Spans []struct {
						TraceID    string `json:"traceId"`
						Attributes []struct {
							Key   string
							Value struct{ StringValue string }
						}
					}
				}
			}
		}
		if json.Unmarshal(line, &batch) != nil {
			continue
		}
		for _, r := range batch.ResourceSpans {
			service := ""
			for _, a := range r.Resource.Attributes {
				if a.Key == "service.name" {
					service = a.Value.StringValue
				}
			}
			for _, scope := range r.ScopeSpans {
				for _, span := range scope.Spans {
					for _, a := range span.Attributes {
						if (a.Key == "runId" && a.Value.StringValue == runID) || (a.Key == "taskId" && a.Value.StringValue == taskID) {
							if services[span.TraceID] == nil {
								services[span.TraceID] = map[string]bool{}
							}
							services[span.TraceID][service] = true
						}
					}
				}
			}
		}
	}
	for _, s := range services {
		if s["api"] && s["dispatcher"] && s["worker-review"] {
			return true
		}
	}
	return false
}
