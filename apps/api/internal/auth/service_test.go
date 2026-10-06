//go:build integration

package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reviewpilot.local/api/internal/auth"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/repositories"
)

func TestLegacyAuth(t *testing.T) {
	raw, err := os.ReadFile("testdata/legacy-session.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Secret        string
		Sealed        string
		ExpectedToken string
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	token, err := auth.OpenToken(fixture.Sealed, fixture.Secret)
	if err != nil || token != "test-token" {
		t.Fatalf("legacy credential incompatible: %v", err)
	}
	store, err := persistence.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := persistence.ApplyMigrations(context.Background(), store, os.Getenv("MIGRATION_DIRECTORY")); err != nil {
		t.Fatal(err)
	}
	opaque := strings.Repeat("A", 43)
	digest := sha256.Sum256([]byte(opaque))
	db := store.DB()
	for _, sql := range []string{
		"INSERT INTO installations(id) VALUES(101)",
		"INSERT INTO repositories(installation_id,id,full_name,public) VALUES(101,202,'fixture/repo',true)",
		"INSERT INTO users(id,login) VALUES(42,'fixture-user')",
		"INSERT INTO installation_memberships(user_id,installation_id) VALUES(42,101)",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO sessions(token_hash,user_id,encrypted_user_token,expires_at) VALUES(?,42,?,clock_timestamp()+interval '1 hour')", hex.EncodeToString(digest[:]), fixture.Sealed).Error; err != nil {
		t.Fatal(err)
	}
	maintainer := false
	private := false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/login/oauth/access_token" && r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing expected GitHub credential")
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/user/installations":
			fmt.Fprint(w, `{"installations":[{"id":101,"app_id":1,"suspended_at":null}]}`)
		case "/user/installations/101/repositories":
			fmt.Fprintf(w, `{"repositories":[{"id":202,"full_name":"fixture/repo","private":%t}]}`, private)
		case "/repos/fixture/repo":
			fmt.Fprintf(w, `{"id":202,"full_name":"fixture/repo","private":%t,"permissions":{"pull":true,"maintain":%t,"admin":false}}`, private, maintainer)
		case "/login/oauth/access_token":
			var payload map[string]string
			if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["code_verifier"] != strings.Repeat("C", 43) || payload["redirect_uri"] != "http://localhost:8080/auth/github/callback" || payload["code"] != "test-code" || payload["client_secret"] != "fixture-secret" {
				t.Error("OAuth exchange did not preserve stored verifier and redirect")
			}
			fmt.Fprint(w, `{"access_token":"test-token","expires_in":28800}`)
		case "/user":
			fmt.Fprint(w, `{"id":42,"login":"fixture-user"}`)
		default:
			t.Errorf("unexpected GitHub path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	cfg := config.Config{AppURL: "http://localhost:8080", GitHubAppID: 1, GitHubClientID: "fixture-client", GitHubClientSecret: "fixture-secret", SessionSecret: fixture.Secret}
	gateway := github.New(cfg)
	gateway.APIURL = fake.URL
	gateway.OAuthURL = fake.URL
	service := auth.New(store, cfg, gateway)
	router := httpapi.NewRouter()
	auth.Register(router, service)
	repositories.Register(router, repositories.New(store, service))
	call := func(method, path, body, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", cfg.AppURL)
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		req.AddCookie(&http.Cookie{Name: "reviewpilot_session", Value: opaque})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	session := call("GET", "/api/session", "", "")
	if session.Code != 200 {
		t.Fatalf("legacy session status=%d", session.Code)
	}
	if strings.Contains(session.Body.String(), "test-token") || strings.Contains(session.Body.String(), fixture.Secret) {
		t.Fatal("credential in public response")
	}
	var sessionBody struct{ CSRFToken string }
	if err := json.Unmarshal(session.Body.Bytes(), &sessionBody); err != nil || len(sessionBody.CSRFToken) != 43 {
		t.Fatal("session CSRF missing")
	}
	again := call("GET", "/api/session", "", "")
	var second struct{ CSRFToken string }
	json.Unmarshal(again.Body.Bytes(), &second)
	if second.CSRFToken != sessionBody.CSRFToken {
		t.Fatal("polling rotated CSRF")
	}
	for _, sql := range []string{
		"INSERT INTO installations(id) VALUES(999)",
		"INSERT INTO repositories(installation_id,id,full_name,public) VALUES(999,888,'foreign/repo',true)",
		"INSERT INTO review_runs(id,installation_id,repository_id,pr_number,base_sha,head_sha,pipeline_version,generation,snapshot) VALUES('11111111-1111-4111-8111-111111111111',101,202,7,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','foundation-v1',0,'{}')",
		"INSERT INTO review_runs(id,installation_id,repository_id,pr_number,base_sha,head_sha,pipeline_version,generation,snapshot) VALUES('22222222-2222-4222-8222-222222222222',999,888,8,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','foundation-v1',0,'{}')",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	repoResponse := call("GET", "/api/repositories", "", "")
	var repoList contracts.RepositoryListResponse
	if repoResponse.Code != 200 || json.Unmarshal(repoResponse.Body.Bytes(), &repoList) != nil || len(repoList.Repositories) != 1 || repoList.Repositories[0].Tenant.RepositoryId != 202 || repoList.Repositories[0].CanManage {
		t.Fatalf("repository list permission boundary: status=%d body=%s", repoResponse.Code, repoResponse.Body)
	}
	runResponse := call("GET", "/api/runs?limit=20", "", "")
	var runList contracts.RunListResponse
	if runResponse.Code != 200 || json.Unmarshal(runResponse.Body.Bytes(), &runList) != nil || len(runList.Runs) != 1 || runList.Runs[0].Id != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("Run list permission boundary: status=%d body=%s", runResponse.Code, runResponse.Body)
	}
	denied := call("POST", "/api/repositories/101/202", `{"enabled":true,"configVersion":1}`, sessionBody.CSRFToken)
	if denied.Code != 403 {
		t.Fatalf("non maintainer status=%d", denied.Code)
	}
	foreign := call("GET", "/installations/callback?installation_id=999", "", "")
	if foreign.Code != 403 {
		t.Fatalf("foreign installation status=%d", foreign.Code)
	}
	maintainer = true
	noCSRF := call("POST", "/api/repositories/101/202", `{"enabled":true,"configVersion":1}`, "")
	if noCSRF.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	enabled := call("POST", "/api/repositories/101/202", `{"enabled":true,"configVersion":1}`, sessionBody.CSRFToken)
	if enabled.Code != 204 {
		t.Fatalf("maintainer update status=%d body=%s", enabled.Code, enabled.Body)
	}
	conflict := call("POST", "/api/repositories/101/202", `{"enabled":false,"configVersion":1}`, sessionBody.CSRFToken)
	if conflict.Code != 409 {
		t.Fatal("stale config overwrote current version")
	}
	private = true
	if synced := call("POST", "/api/repositories/sync", "", sessionBody.CSRFToken); synced.Code != 204 {
		t.Fatalf("private metadata synchronization status=%d", synced.Code)
	}
	var synchronized struct {
		Public  bool
		Enabled bool
	}
	if err := db.Raw("SELECT public,enabled FROM repositories WHERE installation_id=101 AND id=202").Scan(&synchronized).Error; err != nil {
		t.Fatal(err)
	}
	if synchronized.Public || synchronized.Enabled {
		t.Fatal("synchronization left private repository enabled")
	}
	private = false
	if synced := call("POST", "/api/repositories/sync", "", sessionBody.CSRFToken); synced.Code != 204 {
		t.Fatalf("public metadata synchronization status=%d", synced.Code)
	}
	if err := db.Raw("SELECT public,enabled FROM repositories WHERE installation_id=101 AND id=202").Scan(&synchronized).Error; err != nil {
		t.Fatal(err)
	}
	if !synchronized.Public || synchronized.Enabled {
		t.Fatal("public restoration automatically enabled repository")
	}
	state := strings.Repeat("B", 43)
	stateHash := sha256.Sum256([]byte(state))
	if err := db.Exec("INSERT INTO oauth_attempts(state_hash,verifier,expires_at) VALUES(?,?,clock_timestamp()+interval '10 minutes')", hex.EncodeToString(stateHash[:]), strings.Repeat("C", 43)).Error; err != nil {
		t.Fatal(err)
	}
	callback := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/auth/github/callback?state="+state+"&code=test-code", nil)
		req.AddCookie(&http.Cookie{Name: "reviewpilot_oauth", Value: state})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if first := callback(); first.Code != 302 {
		t.Fatalf("OAuth callback status=%d body=%s", first.Code, first.Body)
	}
	replay := callback()
	if replay.Code != 400 {
		t.Fatalf("replayed state status=%d", replay.Code)
	}
	if err := db.Exec("UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second'").Error; err != nil {
		t.Fatal(err)
	}
	expired := call("GET", "/api/session", "", "")
	if expired.Code != 401 {
		t.Fatalf("expired session status=%d", expired.Code)
	}
	result := map[string]any{"decryptedToken": token, "legacySessionStatus": session.Code, "replayedStateStatus": replay.Code, "nonMaintainerStatus": denied.Code, "foreignInstallationStatus": foreign.Code, "expiredSessionStatus": expired.Code}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(os.Getenv("SCENARIO_RESULT_PATH"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
