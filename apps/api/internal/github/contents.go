package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"strings"
	"time"
	"unicode/utf8"
)

func SafeCodePath(path string) bool {
	if path == "" || len(path) > 1024 || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\:\x00") {
		return false
	}
	for _, r := range path {
		if r < 32 {
			return false
		}
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (g *Gateway) readBounded(ctx context.Context, path, token, accept string, limit int) ([]byte, http.Header, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", g.APIURL+path, nil)
	if err != nil {
		return nil, nil, 0, fault.New(503, "GITHUB_UNAVAILABLE")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "ReviewPilot")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	response, err := g.Client.Do(req)
	if err != nil {
		return nil, nil, 0, fault.New(503, "GITHUB_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.Header, response.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil {
		return nil, nil, 0, fault.New(503, "GITHUB_UNAVAILABLE")
	}
	if len(body) > limit {
		return nil, response.Header, response.StatusCode, fault.New(413, "RESPONSE_TOO_LARGE")
	}
	return body, response.Header, response.StatusCode, nil
}

func (g *Gateway) FixedContent(ctx context.Context, token, name, path, sha string, limit int) (contracts.ReviewFileContent, error) {
	result := contracts.ReviewFileContent{Sha: sha, State: "missing"}
	if !SafeCodePath(path) || limit <= 0 || limit > 131072 {
		return result, fault.New(400, "INVALID_CODE_PATH")
	}
	repo, err := RepositoryPath(name)
	if err != nil {
		return result, err
	}
	regular, err := g.regularCodeFile(ctx, token, repo, path, sha)
	if err != nil {
		return result, err
	}
	if !regular {
		result.State = "denied"
		return result, nil
	}
	parts := strings.Split(path, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	body, _, status, err := g.readBounded(ctx, "/repos/"+repo+"/contents/"+strings.Join(parts, "/")+"?ref="+url.QueryEscape(sha), token, "application/vnd.github.raw+json", limit)
	if err != nil {
		if failure, ok := err.(*fault.Error); ok && failure.Code == "RESPONSE_TOO_LARGE" {
			result.State = "too-large"
			result.Bytes = limit
			return result, nil
		}
		return result, err
	}
	if status == 404 {
		return result, nil
	}
	if status == 403 || status == 401 {
		result.State = "denied"
		return result, nil
	}
	if status != 200 {
		return result, fault.New(503, "GITHUB_UNAVAILABLE")
	}
	result.Bytes = len(body)
	if !utf8.Valid(body) || strings.ContainsRune(string(body), 0) {
		result.State = "binary"
		return result, nil
	}
	text := string(body)
	result.Text = &text
	result.State = "available"
	return result, nil
}

func (g *Gateway) ReviewReadToken(ctx context.Context, tenant contracts.TenantRef) (string, error) {
	if err := g.AppInstallation(ctx, tenant.InstallationId); err != nil {
		return "", err
	}
	jwt, err := g.AppJWT()
	if err != nil {
		return "", err
	}
	var response struct {
		Token        string            `json:"token"`
		ExpiresAt    time.Time         `json:"expires_at"`
		Permissions  map[string]string `json:"permissions"`
		Repositories []struct {
			ID int `json:"id"`
		} `json:"repositories"`
	}
	input := map[string]any{"repository_ids": []int{tenant.RepositoryId}, "permissions": map[string]string{"contents": "read", "pull_requests": "read", "metadata": "read"}}
	if err = g.Request(ctx, fmt.Sprintf("/app/installations/%d/access_tokens", tenant.InstallationId), jwt, "POST", input, &response); err != nil {
		return "", err
	}
	if response.Token == "" || !response.ExpiresAt.After(time.Now().Add(5*time.Second)) || len(response.Repositories) != 1 || response.Repositories[0].ID != tenant.RepositoryId || response.Permissions["contents"] != "read" || response.Permissions["pull_requests"] != "read" {
		return "", fault.New(503, "GITHUB_INVALID_TOKEN_RESPONSE")
	}
	for _, permission := range response.Permissions {
		if permission == "write" || permission == "admin" {
			return "", fault.New(503, "GITHUB_TOKEN_OVERPRIVILEGED")
		}
	}
	return response.Token, nil
}

// Contents can dereference symlinks; verify Git object modes before requesting raw content.
func (g *Gateway) regularCodeFile(ctx context.Context, token, repo, path, commitSHA string) (bool, error) {
	body, _, status, err := g.readBounded(ctx, "/repos/"+repo+"/git/commits/"+commitSHA, token, "application/vnd.github+json", 65536)
	if err != nil {
		return false, err
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if status != 200 || json.Unmarshal(body, &commit) != nil || !regexp.MustCompile(`^[a-f0-9]{40,64}$`).MatchString(commit.Tree.SHA) {
		return false, fault.New(503, "GITHUB_INVALID_TREE")
	}
	parts := strings.Split(path, "/")
	if len(parts) > 32 {
		return false, nil
	}
	treeSHA := commit.Tree.SHA
	for index, part := range parts {
		body, _, status, err = g.readBounded(ctx, "/repos/"+repo+"/git/trees/"+treeSHA, token, "application/vnd.github+json", 262144)
		if err != nil {
			return false, err
		}
		var tree struct {
			Truncated bool `json:"truncated"`
			Entries   []struct {
				Path string `json:"path"`
				Mode string `json:"mode"`
				Type string `json:"type"`
				SHA  string `json:"sha"`
			} `json:"tree"`
		}
		if status != 200 || json.Unmarshal(body, &tree) != nil || tree.Truncated {
			return false, fault.New(503, "GITHUB_INVALID_TREE")
		}
		found := false
		for _, entry := range tree.Entries {
			if entry.Path != part {
				continue
			}
			found = true
			if index == len(parts)-1 {
				return entry.Type == "blob" && (entry.Mode == "100644" || entry.Mode == "100755"), nil
			}
			if entry.Type != "tree" || entry.Mode != "040000" || !regexp.MustCompile(`^[a-f0-9]{40,64}$`).MatchString(entry.SHA) {
				return false, nil
			}
			treeSHA = entry.SHA
			break
		}
		if !found {
			return false, nil
		}
	}
	return false, nil
}

func (g *Gateway) FixedPull(ctx context.Context, token, name string, number int) (base, head, state string, err error) {
	repo, err := RepositoryPath(name)
	if err != nil {
		return "", "", "", err
	}
	var response struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Base   struct {
			SHA string `json:"sha"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	body, _, status, err := g.readBounded(ctx, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), token, "application/vnd.github+json", 65536)
	if err != nil {
		return "", "", "", err
	}
	if status != 200 || json.Unmarshal(body, &response) != nil || response.Number != number {
		return "", "", "", fault.New(503, "GITHUB_INVALID_RESPONSE")
	}
	return response.Base.SHA, response.Head.SHA, response.State, nil
}
