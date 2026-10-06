//go:build integration

package webhooks_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/messaging"
	"reviewpilot.local/api/internal/observability"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/publisher"
	"reviewpilot.local/api/internal/testsupport"
	"reviewpilot.local/api/internal/webhooks"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestObservability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	store := testsupport.NewStore(t)
	if err := persistence.ApplyMigrations(ctx, store, os.Getenv("MIGRATION_DIRECTORY")); err != nil {
		t.Fatal(err)
	}
	db := store.DB()
	for _, sql := range []string{"UPDATE runtime_control SET mode='go',epoch=1 WHERE id=1", "INSERT INTO installations(id) VALUES(101)", "INSERT INTO repositories(installation_id,id,full_name,public,enabled) VALUES(101,202,'acme/web',true,true)"} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A real, isolated Collector writes OTLP JSON to its own temporary fixture volume.
	directory := t.TempDir()
	collectorConfig := "receivers:\n  otlp:\n    protocols:\n      http:\n        endpoint: 0.0.0.0:4318\nexporters:\n  file/traces:\n    path: /observe/traces.json\n  file/logs:\n    path: /observe/logs.json\n  file/metrics:\n    path: /observe/metrics.json\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [file/traces]\n    logs:\n      receivers: [otlp]\n      exporters: [file/logs]\n    metrics:\n      receivers: [otlp]\n      exporters: [file/metrics]\n"
	if err := os.WriteFile(filepath.Join(directory, "collector.yaml"), []byte(collectorConfig), 0644); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	docker := func(args ...string) []byte {
		t.Helper()
		command, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		out, err := exec.CommandContext(command, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatal("owned Collector command failed", string(out))
		}
		return out
	}
	id := strings.TrimSpace(string(docker("run", "-d", "--label", "reviewpilot.test-owned=true", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "-p", fmt.Sprintf("127.0.0.1:%d:4318", port), "-v", directory+":/observe", "otel/opentelemetry-collector-contrib:0.161.0", "--config=/observe/collector.yaml")))
	if len(id) != 64 {
		t.Fatal("invalid owned Collector ID")
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = exec.CommandContext(cleanup, "docker", "rm", "-f", "-v", id).Run()
	})
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	apiObs, err := observability.NewRuntime(ctx, "api", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer apiObs.Close(context.Background())
	dispatcherObs, err := observability.NewRuntime(ctx, "dispatcher", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer dispatcherObs.Close(context.Background())
	const sensitive = "TEST-CREDENTIAL-SENTINEL"
	cfg := config.Config{GitHubWebhookSecret: secret, GitHubAppID: 1, AppURL: "http://localhost:8080", ReviewWorkerServiceToken: strings.Repeat("r", 40) + sensitive, EvolutionWorkerServiceToken: strings.Repeat("e", 40)}
	apiObs.Logger.InfoContext(ctx, "TEST_EVENT", "token", sensitive, "source", sensitive, "code", "TEST_EVENT")
	public := httpapi.NewRouter(apiObs)
	webhooks.Register(public, webhooks.New(db, "foundation-v1"), cfg)
	internal := httpapi.NewRouter(apiObs)
	jobs.Register(internal, jobs.New(db), cfg)
	internalServer := httptest.NewServer(internal)
	defer internalServer.Close()
	publicServer := httptest.NewServer(public)
	defer publicServer.Close()
	response, err := http.Get(publicServer.URL + "/internal/tasks/not-public/claim")
	if err != nil {
		t.Fatal(err)
	}
	publicStatus := response.StatusCode
	response.Body.Close()
	response, err = http.Get(internalServer.URL + "/internal/tasks/00000000-0000-4000-8000-000000000000/result")
	if err != nil {
		t.Fatal(err)
	}
	deniedStatus := response.StatusCode
	response.Body.Close()
	if deniedStatus != 401 {
		t.Fatal("internal unauthenticated access", deniedStatus)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cfg.GitHubPrivateKeyPath = filepath.Join(t.TempDir(), "fake-app.pem")
	if err = os.WriteFile(cfg.GitHubPrivateKeyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeGitHub{t: t, key: &key.PublicKey, head: strings.Repeat("b", 40), checks: map[int]github.Check{}}
	remote := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer remote.Close()
	gateway := github.New(cfg)
	gateway.APIURL = remote.URL
	publications := publisher.New(db, gateway, cfg)
	rabbit := messaging.NewRabbit(os.Getenv("TEST_RABBITMQ_URL"))
	defer rabbit.Close()
	relay := &messaging.Relay{DB: db, Publisher: rabbit, Observer: dispatcherObs}
	root := filepath.Clean(filepath.Join(os.Getenv("MIGRATION_DIRECTORY"), "../../.."))
	worker := exec.Command("pnpm", "exec", "turbo", "run", "worker:review", "--filter=@reviewpilot/worker-review")
	worker.Dir = root
	worker.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	worker.Env = append(os.Environ(), "DATABASE_URL=", "RABBITMQ_URL="+os.Getenv("TEST_RABBITMQ_URL"), "INTERNAL_API_URL="+internalServer.URL, "REVIEW_WORKER_SERVICE_TOKEN="+cfg.ReviewWorkerServiceToken, "OTEL_EXPORTER_OTLP_ENDPOINT="+endpoint)
	var workerOutput bytes.Buffer
	worker.Stdout = &workerOutput
	worker.Stderr = &workerOutput
	if err = worker.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-worker.Process.Pid, syscall.SIGTERM); _ = worker.Wait() })
	complete := func(letter string) (string, string) {
		t.Helper()
		fake.mu.Lock()
		fake.head = strings.Repeat(letter, 40)
		fake.mu.Unlock()
		body := eventBody(letter, "2026-10-06T04:00:0"+map[string]string{"b": "0", "c": "1"}[letter]+"Z")
		request, _ := http.NewRequest("POST", publicServer.URL+"/api/github/webhook?code="+sensitive, bytes.NewReader(body))
		request.Header.Set("X-GitHub-Delivery", "observe-"+letter)
		request.Header.Set("X-GitHub-Event", "pull_request")
		request.Header.Set("X-Hub-Signature-256", signature(body))
		request.Header.Set("User-Agent", sensitive)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 202 {
			t.Fatal(response.StatusCode)
		}
		var run persistence.Run
		if err = db.Where("head_sha=?", strings.Repeat(letter, 40)).First(&run).Error; err != nil {
			t.Fatal(err)
		}
		var task string
		if err = db.Raw("SELECT id FROM jobs WHERE run_id=?", run.ID).Scan(&task).Error; err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if err = relay.Flush(ctx, 10); err != nil {
				t.Fatal("relay", err)
			}
			var status string
			db.Raw("SELECT status FROM jobs WHERE id=?", task).Scan(&status)
			if status == "result_ready" {
				if err = publications.PublishConnection(ctx, run.ID); err != nil {
					t.Fatal("fake Check", err)
				}
				return run.ID, task
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("real Worker failed to commit its result")
		return "", ""
	}
	runID, taskID := complete("b")
	// Publication exhaustion retains the accepted job result; it needs a separate failure metric.
	if err = db.Exec("UPDATE publication_requests SET state='failed' WHERE run_id=?", runID).Error; err != nil {
		t.Fatal(err)
	}
	dispatcherObs.RecordState(ctx, db)
	if err = apiObs.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err = dispatcherObs.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	failedPublicationVisible := false
	metricDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(metricDeadline) {
		metrics, _ := os.ReadFile(filepath.Join(directory, "metrics.json"))
		for _, line := range bytes.Split(metrics, []byte("\n")) {
			var batch struct {
				ResourceMetrics []struct {
					ScopeMetrics []struct {
						Metrics []struct {
							Name  string
							Gauge struct{ DataPoints []struct{ AsDouble float64 } }
						}
					}
				}
			}
			if json.Unmarshal(line, &batch) != nil {
				continue
			}
			for _, resource := range batch.ResourceMetrics {
				for _, scope := range resource.ScopeMetrics {
					for _, measurement := range scope.Metrics {
						if measurement.Name == "reviewpilot.publications.failed" {
							for _, point := range measurement.Gauge.DataPoints {
								failedPublicationVisible = failedPublicationVisible || point.AsDouble == 1
							}
						}
					}
				}
			}
		}
		if failedPublicationVisible {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !failedPublicationVisible {
		t.Fatal("failed publication missing from real OTLP metrics")
	}
	if err = db.Exec("UPDATE publication_requests SET state='published' WHERE run_id=?", runID).Error; err != nil {
		t.Fatal(err)
	}
	linked := map[string]string{}
	var captured []byte
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		captured, _ = os.ReadFile(filepath.Join(directory, "traces.json"))
		linked = linkedTraces(captured, runID, taskID)
		if len(linked) == 3 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(linked) != 3 || linked["api"] == "" || linked["api"] != linked["dispatcher"] || linked["api"] != linked["worker-review"] {
		t.Fatal("cross-service trace missing or disconnected", linked)
	}
	logs, _ := os.ReadFile(filepath.Join(directory, "logs.json"))
	secretsCaptured := bytes.Contains(append(captured, logs...), []byte(sensitive))
	if secretsCaptured || !bytes.Contains(logs, []byte("TEST_EVENT")) {
		t.Fatal("unsafe or absent real OTLP log bridge")
	}
	docker("stop", "--time", "1", id)
	secondID, _ := complete("c")
	var finalStatus string
	db.Raw("SELECT status FROM review_runs WHERE id=?", secondID).Scan(&finalStatus)
	report := map[string]any{"linkedServices": []string{"api", "dispatcher", "worker-review"}, "secretsCaptured": secretsCaptured, "completedWithCollectorDown": finalStatus == "Completed", "publicInternalRouteStatus": publicStatus, "unauthenticatedInternalStatus": deniedStatus}
	raw, _ := json.Marshal(report)
	if err = os.WriteFile(os.Getenv("SCENARIO_RESULT_PATH"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func linkedTraces(raw []byte, runID, taskID string) map[string]string {
	result := map[string]string{}
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
		for _, resource := range batch.ResourceSpans {
			service := ""
			for _, a := range resource.Resource.Attributes {
				if a.Key == "service.name" {
					service = a.Value.StringValue
				}
			}
			for _, scope := range resource.ScopeSpans {
				for _, span := range scope.Spans {
					matched := false
					for _, a := range span.Attributes {
						if (a.Key == "runId" && a.Value.StringValue == runID) || (a.Key == "taskId" && a.Value.StringValue == taskID) {
							matched = true
						}
					}
					if matched {
						result[service] = span.TraceID
					}
				}
			}
		}
	}
	return result
}
