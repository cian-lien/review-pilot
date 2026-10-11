package github

import (
	"context"
	"encoding/json"
	"fmt"
	"reviewpilot.local/api/internal/fault"
	"strings"
)

type PullFile struct {
	Filename         string  `json:"filename"`
	PreviousFilename *string `json:"previous_filename"`
	Status           string  `json:"status"`
	Additions        int     `json:"additions"`
	Deletions        int     `json:"deletions"`
	Patch            *string `json:"patch"`
}

// PullFiles follows only our own page sequence; never follow an untrusted Link URL.
func (g *Gateway) PullFiles(ctx context.Context, token, name string, number int) ([]PullFile, error) {
	repo, err := RepositoryPath(name)
	if err != nil {
		return nil, err
	}
	files := []PullFile{}
	seen := map[string]bool{}
	remainingBytes := 2097152
	for page := 1; page <= 30; page++ {
		if remainingBytes <= 0 {
			return nil, fault.New(413, "RESPONSE_TOO_LARGE")
		}
		body, headers, status, err := g.readBounded(ctx, fmt.Sprintf("/repos/%s/pulls/%d/files?per_page=100&page=%d", repo, number, page), token, "application/vnd.github+json", remainingBytes)
		if err != nil {
			return nil, err
		}
		remainingBytes -= len(body)
		var items []PullFile
		if status != 200 || json.Unmarshal(body, &items) != nil || len(items) > 100 {
			return nil, fault.New(503, "GITHUB_INVALID_RESPONSE")
		}
		for _, item := range items {
			if !SafeCodePath(item.Filename) || (item.PreviousFilename != nil && !SafeCodePath(*item.PreviousFilename)) || seen[item.Filename] || item.Additions < 0 || item.Deletions < 0 {
				return nil, fault.New(503, "GITHUB_INVALID_DIFF")
			}
			seen[item.Filename] = true
			files = append(files, item)
		}
		next := strings.Contains(headers.Get("Link"), `rel="next"`)
		if next && len(items) == 0 {
			return nil, fault.New(503, "GITHUB_PAGINATION_INCOMPLETE")
		}
		if !next && len(items) < 100 {
			return files, nil
		}
		if !next && len(items) == 100 {
			// A full page without pagination evidence cannot prove completeness.
			return nil, fault.New(503, "GITHUB_PAGINATION_INCOMPLETE")
		}
	}
	return nil, fault.New(503, "GITHUB_PAGINATION_LIMIT")
}
