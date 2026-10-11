//go:build integration

package reviewcontext

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/testsupport"
)

func TestContextRejectsSymlinkBeforeContentRead(t *testing.T) {
	contentReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/git/commits/") {
			json.NewEncoder(w).Encode(map[string]any{"tree": map[string]string{"sha": strings.Repeat("d", 40)}})
			return
		}
		if strings.Contains(r.URL.Path, "/contents/") {
			contentReads++
			w.Write([]byte("target();"))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"truncated": false, "tree": []map[string]string{{"path": "linked.ts", "mode": "120000", "type": "blob", "sha": strings.Repeat("d", 40)}}})
	}))
	defer server.Close()
	gateway := github.New(config.Config{})
	gateway.APIURL = server.URL
	content, err := gateway.FixedContent(context.Background(), "test", "acme/web", "linked.ts", strings.Repeat("b", 40), 128)
	if err != nil || content.State != "denied" || contentReads != 0 {
		t.Fatalf("symlink must not be dereferenced: %+v %v reads=%d", content, err, contentReads)
	}
}

func TestReviewContextTextualAnchors(t *testing.T) {
	for _, item := range []struct {
		patch, base, head    string
		additions, deletions int
		want                 bool
	}{
		{"@@ -1 +1 @@\n-old();\n+newCall();", "old();", "newCall();", 1, 1, true},
		{"@@ -1 +1 @@\n-old();\n+invented();", "old();", "newCall();", 1, 1, false},
		{"@@ -1,2 +1,2 @@\r\n same();\r\n-old();\r\n+newCall();\r\n\\ No newline at end of file", "same();\r\nold();", "same();\r\nnewCall();", 1, 1, true},
		{"@@ -0,0 +1 @@\n+newCall();", "", "newCall();", 1, 0, true},
		{"@@ -1 +0,0 @@\n-old();", "old();", "", 0, 1, true},
	} {
		if MatchPatch(item.patch, item.base, item.head, item.additions, item.deletions) != item.want {
			t.Fatalf("wrong textual attribution: %+v", item)
		}
	}
	for _, path := range []string{"../outside.ts", "/absolute.ts", "src/./a.ts", "src//a.ts", "https://other/a.ts", "src\\a.ts"} {
		if github.SafeCodePath(path) {
			t.Fatal("unsafe repository path accepted")
		}
	}
}

func TestContextMissingPaginationCannotBecomeComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://untrusted.example/next>; rel="next"`)
		w.Write([]byte("[]"))
	}))
	defer server.Close()
	gateway := github.New(config.Config{})
	gateway.APIURL = server.URL
	_, err := gateway.PullFiles(context.Background(), "test", "acme/web", 1)
	var failure *fault.Error
	if !errors.As(err, &failure) || failure.Code != "GITHUB_PAGINATION_INCOMPLETE" {
		t.Fatalf("missing file page was accepted: %v", err)
	}
}

func TestContextVersionChangesBetweenPages(t *testing.T) {
	changed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/git/commits/"):
			json.NewEncoder(w).Encode(map[string]any{"tree": map[string]string{"sha": strings.Repeat("d", 40)}})
		case strings.Contains(r.URL.Path, "/git/trees/"):
			json.NewEncoder(w).Encode(map[string]any{"truncated": false, "tree": []map[string]string{
				{"path": "src", "mode": "040000", "type": "tree", "sha": strings.Repeat("e", 40)},
				{"path": "a.ts", "mode": "100644", "type": "blob", "sha": strings.Repeat("f", 40)},
				{"path": "b.ts", "mode": "100644", "type": "blob", "sha": strings.Repeat("f", 40)},
				{"path": "old.ts", "mode": "100644", "type": "blob", "sha": strings.Repeat("f", 40)}}})
		case r.URL.Path == "/app/installations/101":
			json.NewEncoder(w).Encode(map[string]any{"id": 101, "app_id": 77, "suspended_at": nil})
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			json.NewEncoder(w).Encode(map[string]any{"token": "test-installation-token", "permissions": map[string]string{"contents": "read", "pull_requests": "read", "metadata": "read"}, "repositories": []map[string]int{{"id": 202}}, "expires_at": "2099-01-01T00:00:00Z"})
		case r.URL.Path == "/repos/acme/web":
			json.NewEncoder(w).Encode(map[string]any{"id": 202, "full_name": "acme/web", "private": false})
		case r.URL.Path == "/repos/acme/web/pulls/1":
			head := strings.Repeat("b", 40)
			if changed {
				head = strings.Repeat("c", 40)
			}
			json.NewEncoder(w).Encode(map[string]any{"number": 1, "state": "open", "head": map[string]string{"sha": head}, "base": map[string]string{"sha": strings.Repeat("a", 40)}})
		case strings.HasSuffix(r.URL.Path, "/files"):
			json.NewEncoder(w).Encode([]map[string]any{
				{"filename": "src/a.ts", "status": "modified", "additions": 1, "deletions": 1, "patch": "@@ -1 +1 @@\n-old();\n+newCall();"},
				{"filename": "src/b.ts", "status": "renamed", "previous_filename": "src/old.ts", "additions": 1, "deletions": 1, "patch": "@@ -1 +1 @@\n-old();\n+newCall();"}})
		case strings.Contains(r.URL.Path, "/contents/"):
			w.Header().Set("Content-Type", "text/plain")
			if r.URL.Query().Get("ref") == strings.Repeat("a", 40) {
				w.Write([]byte("old();"))
			} else {
				w.Write([]byte("newCall();"))
			}
		default:
			t.Errorf("unexpected fixture request: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	store := testsupport.NewStore(t)
	lease, gateway := testsupport.SeedReview(t, store, server.URL)
	service := New(store.DB(), gateway, config.DefaultReviewPolicy())
	page, err := service.Read(context.Background(), contracts.ReadReviewContextRequest{Lease: lease})
	if err != nil || page.NextCursor == nil || page.ContextHash != nil || page.Complete || len(page.Files) != 1 || len(page.Evidence) == 0 {
		t.Fatalf("first page must be bounded and unsealed: %+v %v", page, err)
	}
	changed = true
	_, err = service.Read(context.Background(), contracts.ReadReviewContextRequest{Lease: lease, Cursor: page.NextCursor})
	var failure *fault.Error
	if !errors.As(err, &failure) || failure.Code != "REVIEW_STALE" {
		t.Fatalf("mixed heads must refuse context: %v", err)
	}
	var status string
	store.DB().Raw("SELECT status FROM review_runs WHERE id=?", lease.RunId).Scan(&status)
	if status != "Stale" {
		t.Fatal("remote head change was not durably recorded")
	}
	changed = false
	// A separate run completes the same fixed fixture, including rename and base evidence.
	lease, gateway = testsupport.SeedReview(t, store, server.URL)
	service = New(store.DB(), gateway, config.DefaultReviewPolicy())
	page, err = service.Read(context.Background(), contracts.ReadReviewContextRequest{Lease: lease})
	if err != nil {
		t.Fatal(err)
	}
	page, err = service.Read(context.Background(), contracts.ReadReviewContextRequest{Lease: lease, Cursor: page.NextCursor})
	if err != nil || !page.Complete || page.ContextHash == nil || !page.Coverage.Complete || page.Files[0].PreviousPath == nil {
		t.Fatalf("complete fixture failed: %+v %v", page, err)
	}
	testsupport.ReviewVerdict(t, "context", map[string]any{"versionChangeBlocked": true, "sealedFixedContext": true})
}
