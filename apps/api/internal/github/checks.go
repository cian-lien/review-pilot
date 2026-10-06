package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"time"
)

const ConnectionCheckName = "ReviewPilot · 连接检查"

func (g *Gateway) AppJWT() (string, error) {
	if g.Config.GitHubAppID <= 0 || g.Config.GitHubPrivateKeyPath == "" {
		return "", fault.New(503, "GITHUB_APP_NOT_CONFIGURED")
	}
	raw, err := os.ReadFile(g.Config.GitHubPrivateKeyPath)
	if err != nil || len(raw) > 256<<10 {
		return "", fault.New(503, "GITHUB_KEY_UNAVAILABLE")
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", fault.New(503, "GITHUB_KEY_INVALID")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return "", fault.New(503, "GITHUB_KEY_INVALID")
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return "", fault.New(503, "GITHUB_KEY_INVALID")
		}
	}
	if key.N.BitLen() < 2048 {
		return "", fault.New(503, "GITHUB_KEY_INVALID")
	}
	now := time.Now().Unix()
	claims, _ := json.Marshal(map[string]any{"iat": now - 60, "exp": now + 540, "iss": fmt.Sprint(g.Config.GitHubAppID)})
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", fault.New(503, "GITHUB_KEY_INVALID")
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
func (g *Gateway) AppInstallation(ctx context.Context, id int) error {
	jwt, err := g.AppJWT()
	if err != nil {
		return err
	}
	var installation Installation
	if err = g.Request(ctx, fmt.Sprintf("/app/installations/%d", id), jwt, "GET", nil, &installation); err != nil {
		return err
	}
	if installation.ID != id || installation.AppID != g.Config.GitHubAppID || installation.SuspendedAt != nil {
		return fault.New(403, "INSTALLATION_ACCESS_DENIED")
	}
	return nil
}
func (g *Gateway) InstallationToken(ctx context.Context, tenant contracts.TenantRef) (string, error) {
	if tenant.InstallationId <= 0 || tenant.RepositoryId <= 0 {
		return "", fault.New(400, "INVALID_TENANT")
	}
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
	body := map[string]any{"repository_ids": []int{tenant.RepositoryId}, "permissions": map[string]string{"checks": "write", "pull_requests": "read", "metadata": "read"}}
	if err = g.Request(ctx, fmt.Sprintf("/app/installations/%d/access_tokens", tenant.InstallationId), jwt, "POST", body, &response); err != nil {
		return "", err
	}
	if response.Token == "" || !response.ExpiresAt.After(time.Now().Add(5*time.Second)) || response.Permissions["checks"] != "write" || response.Permissions["pull_requests"] != "read" || len(response.Repositories) != 1 || response.Repositories[0].ID != tenant.RepositoryId {
		return "", fault.New(503, "GITHUB_INVALID_TOKEN_RESPONSE")
	}
	if response.Permissions["contents"] == "write" || response.Permissions["pull_requests"] == "write" {
		return "", fault.New(503, "GITHUB_TOKEN_OVERPRIVILEGED")
	}
	return response.Token, nil
}

type PullRequest struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

func (g *Gateway) PullRequest(ctx context.Context, token, name string, number int) (PullRequest, error) {
	path, err := RepositoryPath(name)
	if err != nil {
		return PullRequest{}, err
	}
	var pr PullRequest
	err = g.Request(ctx, fmt.Sprintf("/repos/%s/pulls/%d", path, number), token, "GET", nil, &pr)
	if err == nil && (pr.Number != number || (pr.State != "open" && pr.State != "closed") || !regexp.MustCompile(`^[a-f0-9]{40,64}$`).MatchString(pr.Head.SHA)) {
		err = fault.New(503, "GITHUB_INVALID_RESPONSE")
	}
	return pr, err
}

type Check struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	ExternalID string  `json:"external_id"`
	HeadSHA    string  `json:"head_sha"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
	App        struct {
		ID int64 `json:"id"`
	} `json:"app"`
}

func (g *Gateway) CheckMatches(check Check, head, runID string) bool {
	return check.ID > 0 && check.ID <= 9007199254740991 && check.Name == ConnectionCheckName && check.ExternalID == "reviewpilot:"+runID && check.HeadSHA == head && check.App.ID == g.Config.GitHubAppID
}
func (g *Gateway) FindConnectionCheck(ctx context.Context, token, name, head, runID string) (*Check, error) {
	path, err := RepositoryPath(name)
	if err != nil {
		return nil, err
	}
	for page := 1; page <= 50; page++ {
		var response struct {
			Checks []Check `json:"check_runs"`
		}
		endpoint := fmt.Sprintf("/repos/%s/commits/%s/check-runs?filter=all&check_name=%s&per_page=100&page=%d", path, head, url.QueryEscape(ConnectionCheckName), page)
		if err = g.Request(ctx, endpoint, token, "GET", nil, &response); err != nil {
			return nil, err
		}
		for _, check := range response.Checks {
			if g.CheckMatches(check, head, runID) {
				return &check, nil
			}
		}
		if len(response.Checks) < 100 {
			return nil, nil
		}
	}
	return nil, fault.New(503, "GITHUB_PAGINATION_LIMIT")
}
func (g *Gateway) GetConnectionCheck(ctx context.Context, token, name, head, runID string, id int64) (*Check, error) {
	path, err := RepositoryPath(name)
	if err != nil {
		return nil, err
	}
	var check Check
	if err = g.Request(ctx, fmt.Sprintf("/repos/%s/check-runs/%d", path, id), token, "GET", nil, &check); err != nil {
		return nil, err
	}
	if !g.CheckMatches(check, head, runID) {
		return nil, fault.New(409, "CHECK_IDENTITY_MISMATCH")
	}
	return &check, nil
}
func (g *Gateway) WriteConnectionCheck(ctx context.Context, token, name, head, runID, appURL string, id *int64, conclusion string) (Check, error) {
	path, err := RepositoryPath(name)
	if err != nil {
		return Check{}, err
	}
	status, title := "in_progress", "正在验证 GitHub 接入"
	summary := "此 Check 仅验证事件、任务与 GitHub 写入链路。尚未分析代码，不能代表代码正确或安全。"
	body := map[string]any{"name": ConnectionCheckName, "external_id": "reviewpilot:" + runID, "details_url": appURL + "/repositories"}
	if conclusion != "" {
		status = "completed"
		title = "连接检查完成（未进行 AI 审查）"
		body["conclusion"] = conclusion
		body["completed_at"] = time.Now().UTC().Format(time.RFC3339)
		if conclusion == "cancelled" {
			title = "旧快照已停止"
			summary = "PR 已关闭或 Head 已变化；此检查未分析代码。"
		}
	}
	body["status"] = status
	body["output"] = map[string]string{"title": title, "summary": summary}
	method, endpoint := "POST", "/repos/"+path+"/check-runs"
	if id != nil {
		method = "PATCH"
		endpoint += fmt.Sprintf("/%d", *id)
	} else {
		body["head_sha"] = head
	}
	var check Check
	if err = g.Request(ctx, endpoint, token, method, body, &check); err != nil {
		return Check{}, err
	}
	if !g.CheckMatches(check, head, runID) || check.Status != status || (conclusion != "" && (check.Conclusion == nil || *check.Conclusion != conclusion)) {
		return Check{}, fault.New(503, "GITHUB_CHECK_INVALID_RESPONSE")
	}
	return check, nil
}
