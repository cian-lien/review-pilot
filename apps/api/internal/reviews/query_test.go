//go:build integration

package reviews

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reviewpilot.local/api/internal/auth"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/testsupport"
	"strings"
	"testing"
)

func TestReviewQueryAccess(t *testing.T) {
	maintain := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/user/installations/101/"):
			json.NewEncoder(w).Encode(map[string]any{"repositories": []map[string]any{{"id": 202, "full_name": "acme/web", "private": false}}})
		case r.URL.Path == "/user/installations":
			json.NewEncoder(w).Encode(map[string]any{"installations": []map[string]any{{"id": 101, "app_id": 77}}})
		case r.URL.Path == "/repos/acme/web":
			json.NewEncoder(w).Encode(map[string]any{"id": 202, "full_name": "acme/web", "private": false, "permissions": map[string]bool{"maintain": maintain, "pull": true}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	store := testsupport.NewStore(t)
	l, g := testsupport.SeedReview(t, store, server.URL)
	service := NewQuery(store.DB(), auth.New(store, config.Config{SessionSecret: strings.Repeat("s", 32)}, g))
	session := auth.Session{UserID: 1, UserToken: "user-fixture"}
	if _, e := service.Detail(context.Background(), session, l.Tenant, 1, l.RunId); e != nil {
		t.Fatal(e)
	}
	if _, e := service.Detail(context.Background(), session, l.Tenant, 2, l.RunId); e == nil {
		t.Fatal("PR mismatch leaked a Run")
	}
	if _, e := service.Detail(context.Background(), session, contracts.TenantRef{InstallationId: 999, RepositoryId: 202}, 1, l.RunId); e == nil {
		t.Fatal("installation mismatch leaked a Run")
	}
	maintain = false
	if _, e := service.Detail(context.Background(), session, l.Tenant, 1, l.RunId); e == nil {
		t.Fatal("revoked maintain permission cached")
	}
	maintain = true
	cursor, e := service.signCursor(queryCursor{Tenant: l.Tenant, PR: 1, Kind: "runs", ID: l.RunId})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.readCursor(cursor, l.Tenant, 2, "runs"); e == nil {
		t.Fatal("cursor crossed PR scope")
	}
	if _, e = service.readCursor(cursor, contracts.TenantRef{InstallationId: 101, RepositoryId: 303}, 1, "runs"); e == nil {
		t.Fatal("cursor crossed repository scope")
	}
	result := preparedResult(t, store, server.URL, "valid")
	if _, e = New(store.DB()).Commit(context.Background(), contracts.CommitReviewRequest{Result: result}); e != nil {
		t.Fatal(e)
	}
	l = result.Lease
	store.DB().Exec("UPDATE review_runs SET snapshot=jsonb_set(snapshot,'{provenance,contentExpiresAt}',to_jsonb('2000-01-01T00:00:00Z'::text)) WHERE id=?", l.RunId)
	detail, e := service.Detail(context.Background(), session, l.Tenant, 1, l.RunId)
	if e != nil || !detail.ContentExpired {
		t.Fatalf("expired content not distinguished: %+v %v", detail, e)
	}
	if len(detail.Findings) != 1 || !detail.Findings[0].ContentExpired || len(detail.Findings[0].Evidence) != 0 {
		t.Fatal("expiry erased finding count or exposed source")
	}
	testsupport.ReviewVerdict(t, "results", map[string]any{"currentMaintainRequired": true, "expiredContentHidden": true, "scopedCursor": true})
}
