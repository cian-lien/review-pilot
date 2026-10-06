//go:build integration

package webhooks_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/publisher"
	"reviewpilot.local/api/internal/testsupport"
	"reviewpilot.local/api/internal/webhooks"
	"strings"
	"sync"
	"testing"
	"time"
)

const secret = "test-webhook-secret"

func TestWebhookCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	store := testsupport.NewStore(t)
	if err := persistence.ApplyMigrations(ctx, store, os.Getenv("MIGRATION_DIRECTORY")); err != nil {
		t.Fatal(err)
	}
	db := store.DB()
	for _, statement := range []string{"UPDATE runtime_control SET mode='go',epoch=1 WHERE id=1", "INSERT INTO installations(id) VALUES(101)", "INSERT INTO repositories(installation_id,id,full_name,public,enabled) VALUES(101,202,'acme/web',true,true)"} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	ingestion := webhooks.New(db, "foundation-v1")
	cfg := config.Config{GitHubWebhookSecret: secret, GitHubAppID: 1, AppURL: "http://localhost:8080"}
	router := httpapi.NewRouter()
	webhooks.Register(router, ingestion, cfg)
	body := eventBody("b", "2026-10-06T04:00:00Z")
	invalid := send(router, "delivery-1", "pull_request", append(append([]byte{}, body...), 32), signature(body))
	if invalid != 401 {
		t.Fatal("changed raw bytes must invalidate signature", invalid)
	}
	oversized := send(router, "oversized", "pull_request", bytes.Repeat([]byte("x"), webhooks.MaxBody+1), "invalid")
	if oversized != 413 {
		t.Fatal("oversized body must be rejected before signature", oversized)
	}
	if code := send(router, "delivery-1", "pull_request", body, signature(body)); code != 202 {
		t.Fatal("valid event", code)
	}
	if code := send(router, "delivery-1", "pull_request", body, signature(body)); code != 202 {
		t.Fatal("identical delivery", code)
	}
	if code := send(router, "delivery-2", "pull_request", body, signature(body)); code != 202 {
		t.Fatal("semantic duplicate", code)
	}
	changed := bytes.Replace(body, []byte("opened"), []byte("reopened"), 1)
	conflict := send(router, "delivery-1", "pull_request", changed, signature(changed))
	if conflict != 409 {
		t.Fatal("delivery conflict", conflict)
	}
	var runCount, outboxCount int64
	db.Raw("SELECT count(*) FROM review_runs").Scan(&runCount)
	db.Raw("SELECT count(*) FROM outbox_events").Scan(&outboxCount)
	if runCount != 1 || outboxCount != 1 {
		t.Fatal("Delivery Run job Outbox must be atomic and idempotent")
	}
	var remaining float64
	db.Raw("SELECT EXTRACT(EPOCH FROM available_at-clock_timestamp()) FROM jobs LIMIT 1").Scan(&remaining)
	if remaining < 1.5 || remaining > 2 {
		t.Fatal("two-second stability window missing", remaining)
	}
	old := eventBody("a", "2026-10-06T03:59:59Z")
	if code := send(router, "older", "pull_request", old, signature(old)); code != 202 {
		t.Fatal(code)
	}
	var logicalCount int64
	db.Raw("SELECT count(*) FROM review_runs").Scan(&logicalCount)
	if logicalCount != 1 {
		t.Fatal("older event enqueued")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cfg.GitHubPrivateKeyPath = filepath.Join(t.TempDir(), "test-app.pem")
	if err = os.WriteFile(cfg.GitHubPrivateKeyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeGitHub{t: t, key: &key.PublicKey, head: strings.Repeat("b", 40), checks: map[int]github.Check{}, loseCreate: true}
	remote := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer remote.Close()
	gateway := github.New(cfg)
	gateway.APIURL = remote.URL
	publications := publisher.New(db, gateway, cfg)
	ready := prepare(t, ctx, db, "b")
	if err = publications.PublishConnection(ctx, ready.ID); err == nil {
		t.Fatal("lost create response should require recovery")
	}
	db.Exec("UPDATE publication_requests SET available_at=clock_timestamp() WHERE run_id=?", ready.ID)
	if err = publications.PublishConnection(ctx, ready.ID); err != nil {
		t.Fatal("publish reconciliation", err)
	}
	fake.mu.Lock()
	createAfterLoss := fake.creates
	conclusion := fake.lastConclusion
	fake.mu.Unlock()
	if createAfterLoss != 1 || conclusion != "neutral" {
		t.Fatal("duplicate POST or non-neutral Check")
	}
	var completed string
	db.Raw("SELECT status FROM review_runs WHERE id=?", ready.ID).Scan(&completed)
	if completed != "Completed" {
		t.Fatal("run marked completed before full publication")
	}
	// Equal-second head C cannot be ordered by the local head B. The remote PR says C.
	equal := eventBody("c", "2026-10-06T04:00:00Z")
	if code := send(router, "equal-second", "pull_request", equal, signature(equal)); code != 202 {
		t.Fatal(code)
	}
	fake.mu.Lock()
	fake.head = strings.Repeat("c", 40)
	fake.mu.Unlock()
	current := prepare(t, ctx, db, "c")
	if err = publications.PublishConnection(ctx, current.ID); err != nil {
		t.Fatal("equal-second current head suppressed", err)
	}
	// A stale local snapshot must not cause a new Check for its old head.
	oldHead := eventBody("d", "2026-10-06T04:00:01Z")
	send(router, "stale-head", "pull_request", oldHead, signature(oldHead))
	oldRun := prepare(t, ctx, db, "d")
	fake.mu.Lock()
	fake.head = strings.Repeat("e", 40)
	beforeStale := fake.writes
	fake.mu.Unlock()
	if err = publications.PublishConnection(ctx, oldRun.ID); err != nil {
		t.Fatal("stale cancellation", err)
	}
	fake.mu.Lock()
	stalePublished := fake.writes != beforeStale
	fake.mu.Unlock()
	if stalePublished {
		t.Fatal("stale head was written")
	}
	// Revocation between remote permission fetch and mutation is fenced by the DB.
	revoke := eventBody("f", "2026-10-06T04:00:02Z")
	send(router, "revoke-run", "pull_request", revoke, signature(revoke))
	revokeRun := prepare(t, ctx, db, "f")
	privateBody := []byte(`{"action":"privatized","installation":{"id":101},"repository":{"id":202,"full_name":"acme/web","private":true}}`)
	fake.mu.Lock()
	fake.head = strings.Repeat("f", 40)
	beforeRevoked := fake.writes
	fake.onRepository = func() {
		_, err := ingestion.Accept(ctx, "private-in-flight", "repository", privateBody)
		if err != nil {
			t.Error(err)
		}
	}
	fake.mu.Unlock()
	if err = publications.PublishConnection(ctx, revokeRun.ID); err == nil {
		t.Fatal("in-flight revoked publication accepted")
	}
	fake.mu.Lock()
	revokedPublished := fake.writes != beforeRevoked
	unsafeWrites := fake.unsafeWrites
	fake.mu.Unlock()
	var repo persistence.Repository
	db.First(&repo, "installation_id=101 AND id=202")
	if revokedPublished || repo.Enabled || repo.Public {
		t.Fatal("revoked repository not fenced")
	}
	publicBody := bytes.Replace(privateBody, []byte("privatized"), []byte("publicized"), 1)
	publicBody = bytes.Replace(publicBody, []byte("true"), []byte("false"), 1)
	if _, err = ingestion.Accept(ctx, "public-again", "repository", publicBody); err != nil {
		t.Fatal(err)
	}
	db.First(&repo, "installation_id=101 AND id=202")
	if repo.Enabled {
		t.Fatal("publicizing must not enable")
	}
	if _, err = ingestion.Accept(ctx, "installation-revoked", "installation", []byte(`{"action":"deleted","installation":{"id":101}}`)); err != nil {
		t.Fatal(err)
	}
	var installation persistence.Installation
	db.First(&installation, 101)
	if installation.Active {
		t.Fatal("installation revocation missing")
	}
	if unsafeWrites {
		t.Fatal("PR comments or Contents writes attempted")
	}
	observed := map[string]any{"invalidSignatureStatus": invalid, "oversizedStatus": oversized, "deliveryConflictStatus": conflict, "logicalRunCount": runCount, "logicalOutboxCount": outboxCount, "checkCreateCountAfterLostResponse": createAfterLoss, "staleHeadPublished": stalePublished, "revokedPublished": revokedPublished, "conclusion": conclusion}
	encoded, _ := json.Marshal(observed)
	if err = os.WriteFile(os.Getenv("SCENARIO_RESULT_PATH"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
}
func eventBody(head, date string) []byte {
	return []byte(fmt.Sprintf(`{"action":"opened","installation":{"id":101},"repository":{"id":202,"full_name":"acme/web","private":false},"pull_request":{"number":1,"updated_at":"%s","base":{"sha":"%s"},"head":{"sha":"%s"}}}`, date, strings.Repeat("a", 40), strings.Repeat(head, 40)))
}
func signature(raw []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
func send(router *gin.Engine, id, event string, body []byte, sig string) int {
	request := httptest.NewRequest("POST", "/api/github/webhook", bytes.NewReader(body))
	request.Header.Set("X-Hub-Signature-256", sig)
	request.Header.Set("X-GitHub-Delivery", id)
	request.Header.Set("X-GitHub-Event", event)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response.Code
}
func prepare(t *testing.T, ctx context.Context, db *gorm.DB, head string) persistence.Run {
	t.Helper()
	var run persistence.Run
	if err := db.First(&run, "head_sha=?", strings.Repeat(head, 40)).Error; err != nil {
		t.Fatal(err)
	}
	var notification struct{ Payload json.RawMessage }
	if err := db.Raw("SELECT payload FROM outbox_events WHERE task_id=(SELECT id FROM jobs WHERE run_id=?) ORDER BY notification_generation DESC LIMIT 1", run.ID).Scan(&notification).Error; err != nil {
		t.Fatal(err)
	}
	var envelope contracts.TaskEnvelope
	if err := json.Unmarshal(notification.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE jobs SET available_at=clock_timestamp() WHERE run_id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	service := jobs.New(db)
	claim, err := service.Claim(ctx, envelope, "test-worker")
	if err != nil || claim.Lease == nil {
		t.Fatalf("current run must be claimable, outcome=%s error=%v", claim.Outcome, err)
	}
	lease := claim.Lease
	result := contracts.ConnectionResult{TaskId: lease.TaskId, RunId: lease.RunId, LeaseToken: lease.LeaseToken, InputHash: lease.InputHash, ResultHash: jobs.ResultHash(lease.TaskId, lease.RunId, lease.InputHash), ResultId: lease.TaskId + ":connection", Kind: contracts.ConnectionResultKindConnectionCheck, ExecutionEpoch: lease.ExecutionEpoch}
	if _, err = service.CommitConnection(ctx, result); err != nil {
		t.Fatal(err)
	}
	return run
}

type fakeGitHub struct {
	t              *testing.T
	key            *rsa.PublicKey
	mu             sync.Mutex
	head           string
	checks         map[int]github.Check
	creates        int
	writes         int
	lastConclusion string
	loseCreate     bool
	unsafeWrites   bool
	onRepository   func()
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	respond := func(value any) { json.NewEncoder(w).Encode(value) }
	path := r.URL.Path
	if strings.HasPrefix(path, "/app/installations/") {
		if !f.jwt(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
			w.WriteHeader(401)
			return
		}
		if r.Method == "GET" {
			respond(map[string]any{"id": 101, "app_id": 1, "suspended_at": nil})
			return
		}
		if r.Method == "POST" && strings.HasSuffix(path, "/access_tokens") {
			var body struct {
				IDs         []int             `json:"repository_ids"`
				Permissions map[string]string `json:"permissions"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.IDs) != 1 || body.IDs[0] != 202 || body.Permissions["checks"] != "write" || body.Permissions["pull_requests"] != "read" || body.Permissions["contents"] == "write" {
				f.t.Error("token scope incorrect")
				w.WriteHeader(400)
				return
			}
			respond(map[string]any{"token": "test-installation-token", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "permissions": body.Permissions, "repositories": []any{map[string]any{"id": 202}}})
			return
		}
	}
	if r.Header.Get("Authorization") != "Bearer test-installation-token" {
		w.WriteHeader(401)
		return
	}
	switch {
	case r.Method == "GET" && path == "/repos/acme/web":
		if f.onRepository != nil {
			hook := f.onRepository
			f.onRepository = nil
			hook()
		}
		respond(map[string]any{"id": 202, "full_name": "acme/web", "private": false})
		return
	case r.Method == "GET" && path == "/repos/acme/web/pulls/1":
		respond(map[string]any{"number": 1, "state": "open", "head": map[string]string{"sha": f.head}})
		return
	case r.Method == "GET" && strings.Contains(path, "/commits/") && strings.HasSuffix(path, "/check-runs"):
		checks := []github.Check{}
		for _, check := range f.checks {
			if strings.Contains(path, "/commits/"+check.HeadSHA+"/") {
				checks = append(checks, check)
			}
		}
		respond(map[string]any{"check_runs": checks})
		return
	case strings.HasPrefix(path, "/repos/acme/web/check-runs"):
		if r.Method == "GET" {
			var id int
			fmt.Sscanf(path, "/repos/acme/web/check-runs/%d", &id)
			respond(f.checks[id])
			return
		}
		if r.Method == "POST" || r.Method == "PATCH" {
			var input struct {
				Name       string  `json:"name"`
				ExternalID string  `json:"external_id"`
				HeadSHA    string  `json:"head_sha"`
				Status     string  `json:"status"`
				Conclusion *string `json:"conclusion"`
				Output     struct {
					Title string `json:"title"`
				} `json:"output"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				f.t.Error(err)
			}
			var check github.Check
			if r.Method == "POST" {
				f.creates++
				check.ID = 100 + f.creates
				check.HeadSHA = input.HeadSHA
			} else {
				var id int
				fmt.Sscanf(path, "/repos/acme/web/check-runs/%d", &id)
				check = f.checks[id]
			}
			check.Name = input.Name
			check.ExternalID = input.ExternalID
			check.Status = input.Status
			check.Conclusion = input.Conclusion
			check.App.ID = 1
			f.checks[check.ID] = check
			f.writes++
			if input.Conclusion != nil {
				f.lastConclusion = *input.Conclusion
				if *input.Conclusion == "neutral" && input.Output.Title != "连接检查完成（未进行 AI 审查）" {
					f.t.Error("AI disclaimer missing")
				}
			}
			if f.loseCreate && r.Method == "POST" {
				f.loseCreate = false
				w.WriteHeader(503)
				return
			}
			respond(check)
			return
		}
	}
	if r.Method != "GET" {
		f.unsafeWrites = true
	}
	f.t.Error("unexpected fake GitHub endpoint", r.Method, path)
	w.WriteHeader(404)
}
func (f *fakeGitHub) jwt(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(f.key, crypto.SHA256, hash[:], signature) != nil {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claim struct {
		Issuer  string `json:"iss"`
		Expires int64  `json:"exp"`
	}
	if json.Unmarshal(payload, &claim) != nil {
		return false
	}
	return claim.Issuer == "1" && claim.Expires > time.Now().Unix() && claim.Expires <= time.Now().Unix()+600
}
