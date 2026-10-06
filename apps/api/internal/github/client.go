package github

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/fault"
	"strings"
	"time"
)

type Gateway struct {
	Config   config.Config
	APIURL   string
	OAuthURL string
	Client   *http.Client
}

func New(cfg config.Config) *Gateway {
	return &Gateway{Config: cfg, APIURL: "https://api.github.com", OAuthURL: "https://github.com", Client: &http.Client{
		Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}}
}
func (g *Gateway) Request(ctx context.Context, path, token, method string, input, output any) error {
	return g.requestURL(ctx, g.APIURL+path, token, method, input, output)
}
func (g *Gateway) requestURL(ctx context.Context, address, token, method string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return fault.New(400, "INVALID_INPUT")
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return fault.New(503, "GITHUB_UNAVAILABLE")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ReviewPilot")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	response, err := g.Client.Do(req)
	if err != nil {
		return fault.New(503, "GITHUB_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := response.StatusCode
		if status == 403 && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "") {
			status = 429
		}
		if status == 401 {
			return fault.New(401, "GITHUB_SESSION_EXPIRED")
		}
		if status == 403 || status == 404 {
			return fault.New(403, "GITHUB_ACCESS_DENIED")
		}
		return fault.New(503, "GITHUB_UNAVAILABLE")
	}
	if output != nil && response.StatusCode != 204 {
		if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(output); err != nil {
			return fault.New(503, "GITHUB_INVALID_RESPONSE")
		}
	}
	return nil
}
func RepositoryPath(name string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return "", fault.New(400, "INVALID_REPOSITORY")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fault.New(400, "INVALID_REPOSITORY")
		}
	}
	return url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]), nil
}
