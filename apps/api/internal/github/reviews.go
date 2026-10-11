package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"strconv"
	"strings"
	"time"
)

const ReviewCheckName = "ReviewPilot · AI 审查"

type ReviewRateLimit struct{ After time.Duration }

func (e *ReviewRateLimit) Error() string { return "GITHUB_REVIEW_RATE_LIMITED" }

// A successful transport with an invalid body is still an unknown remote write.
func (g *Gateway) reviewJSON(ctx context.Context, path, token, method string, input, output any) error {
	var raw []byte
	var err error
	if input != nil {
		raw, err = json.Marshal(input)
		if err != nil || len(raw) > 32768 {
			return fault.New(400, "REVIEW_REQUEST_TOO_LARGE")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, g.APIURL+path, bytes.NewReader(raw))
	if err != nil {
		return fault.New(503, "GITHUB_UNAVAILABLE")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "ReviewPilot")
	response, err := g.Client.Do(req)
	if err != nil {
		return fault.New(503, "GITHUB_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode == 429 || response.StatusCode == 403 && (response.Header.Get("Retry-After") != "" || response.Header.Get("X-RateLimit-Remaining") == "0") {
		wait := time.Minute
		if seconds, e := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 32); e == nil && seconds > 0 {
			wait = time.Duration(seconds) * time.Second
		} else if date, e := http.ParseTime(response.Header.Get("Retry-After")); e == nil && date.After(time.Now()) {
			wait = time.Until(date)
		}
		if reset, e := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); e == nil && reset > time.Now().Unix() && time.Until(time.Unix(reset, 0)) > wait {
			wait = time.Until(time.Unix(reset, 0))
		}
		return &ReviewRateLimit{wait}
	}
	if response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 404 {
		return fault.New(403, "GITHUB_ACCESS_DENIED")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fault.New(503, "GITHUB_UNAVAILABLE")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 262145))
	if err != nil || len(body) > 262144 {
		return fault.New(503, "GITHUB_RESPONSE_TOO_LARGE")
	}
	if output != nil && json.Unmarshal(body, output) != nil {
		return fault.New(503, "GITHUB_INVALID_RESPONSE")
	}
	return nil
}

func (g *Gateway) ReviewWriteToken(ctx context.Context, tenant contracts.TenantRef) (string, error) {
	if err := g.AppInstallation(ctx, tenant.InstallationId); err != nil {
		return "", err
	}
	jwt, err := g.AppJWT()
	if err != nil {
		return "", err
	}
	var response struct {
		Token        string
		ExpiresAt    time.Time `json:"expires_at"`
		Permissions  map[string]string
		Repositories []struct{ ID int }
	}
	requested := map[string]string{"contents": "read", "pull_requests": "write", "checks": "write", "metadata": "read"}
	if err = g.reviewJSON(ctx, fmt.Sprintf("/app/installations/%d/access_tokens", tenant.InstallationId), jwt, "POST", map[string]any{"repository_ids": []int{tenant.RepositoryId}, "permissions": requested}, &response); err != nil {
		return "", err
	}
	if response.Token == "" || !response.ExpiresAt.After(time.Now().Add(5*time.Second)) || len(response.Repositories) != 1 || response.Repositories[0].ID != tenant.RepositoryId {
		return "", fault.New(503, "GITHUB_INVALID_TOKEN_RESPONSE")
	}
	for key, value := range requested {
		if response.Permissions[key] != value {
			return "", fault.New(503, "GITHUB_INVALID_TOKEN_RESPONSE")
		}
	}
	for key, value := range response.Permissions {
		if value == "admin" || value == "write" && requested[key] != "write" {
			return "", fault.New(503, "GITHUB_TOKEN_OVERPRIVILEGED")
		}
	}
	return response.Token, nil
}

type reviewAuthor struct {
	ID    int64
	Login string
	Type  string
}
type remoteReview struct {
	ID       int64
	Body     string
	CommitID string `json:"commit_id"`
	State    string
	User     reviewAuthor
}
type remoteReviewComment struct {
	ID       int64
	ReviewID int64  `json:"pull_request_review_id"`
	CommitID string `json:"commit_id"`
	Body     string
	Path     string
	Line     *int
	Side     string
	User     reviewAuthor
}

func (g *Gateway) reviewBot(ctx context.Context, token string) (reviewAuthor, error) {
	// Reviews do not expose an App ID; bind the bot's numeric identity to the authenticated App slug.
	jwt, err := g.AppJWT()
	if err != nil {
		return reviewAuthor{}, err
	}
	var app struct {
		ID   int64
		Slug string
	}
	if err = g.reviewJSON(ctx, "/app", jwt, "GET", nil, &app); err != nil {
		return reviewAuthor{}, err
	}
	if app.ID != g.Config.GitHubAppID || !regexp.MustCompile(`^[a-zA-Z0-9-]{1,100}$`).MatchString(app.Slug) {
		return reviewAuthor{}, fault.New(503, "GITHUB_APP_IDENTITY_INVALID")
	}
	var bot reviewAuthor
	err = g.reviewJSON(ctx, "/users/"+url.PathEscape(app.Slug+"[bot]"), token, "GET", nil, &bot)
	if err == nil && (bot.ID <= 0 || bot.ID > 9007199254740991 || bot.Type != "Bot" || bot.Login != app.Slug+"[bot]") {
		err = fault.New(503, "GITHUB_APP_IDENTITY_INVALID")
	}
	return bot, err
}
func sameBot(a, b reviewAuthor) bool { return a.ID == b.ID && a.Login == b.Login && a.Type == "Bot" }
func reviewBody(plan contracts.ReviewPublicationPlan) string {
	body := plan.Marker
	for _, finding := range plan.Findings {
		if finding.Line == nil {
			body += "\n\n" + finding.Body
		}
	}
	return body
}
func (g *Gateway) FindReview(ctx context.Context, token, name string, number int, plan contracts.ReviewPublicationPlan) (*contracts.ReviewPublicationReceipt, error) {
	path, err := RepositoryPath(name)
	if err != nil {
		return nil, err
	}
	bot, err := g.reviewBot(ctx, token)
	if err != nil {
		return nil, err
	}
	var found *remoteReview
	for page := 1; page <= 50; page++ {
		var rows []remoteReview
		if err = g.reviewJSON(ctx, fmt.Sprintf("/repos/%s/pulls/%d/reviews?per_page=100&page=%d", path, number, page), token, "GET", nil, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.ID > 0 && row.ID <= 9007199254740991 && row.Body == reviewBody(plan) && row.CommitID == plan.HeadSha && row.State == "COMMENTED" && sameBot(row.User, bot) {
				if found != nil {
					return nil, fault.New(409, "DUPLICATE_REMOTE_REVIEW")
				}
				copy := row
				found = &copy
			}
		}
		if len(rows) < 100 {
			break
		}
		if page == 50 {
			return nil, fault.New(503, "GITHUB_PAGINATION_LIMIT")
		}
	}
	if found == nil {
		return nil, nil
	}
	matches := map[string]string{}
	for page := 1; page <= 50; page++ {
		var rows []remoteReviewComment
		if err = g.reviewJSON(ctx, fmt.Sprintf("/repos/%s/pulls/%d/reviews/%d/comments?per_page=100&page=%d", path, number, found.ID, page), token, "GET", nil, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.ID <= 0 || row.ID > 9007199254740991 || row.ReviewID != found.ID || row.CommitID != plan.HeadSha || !sameBot(row.User, bot) {
				continue
			}
			for _, finding := range plan.Findings {
				if row.Path == finding.Path && row.Body == finding.Body+"\n\n"+plan.Marker && row.Side == "RIGHT" && row.Line != nil && finding.Line != nil && *row.Line == *finding.Line {
					if _, exists := matches[finding.FindingId]; exists {
						return nil, fault.New(409, "DUPLICATE_REMOTE_COMMENT")
					}
					matches[finding.FindingId] = fmt.Sprint(row.ID)
				}
			}
		}
		if len(rows) < 100 {
			break
		}
		if page == 50 {
			return nil, fault.New(503, "GITHUB_PAGINATION_LIMIT")
		}
	}
	ids := []string{}
	for _, finding := range plan.Findings {
		if finding.Line == nil {
			continue
		}
		id, ok := matches[finding.FindingId]
		if !ok {
			return nil, fault.New(409, "REVIEW_COMMENT_FACTS_INCOMPLETE")
		}
		ids = append(ids, id)
	}
	id := fmt.Sprint(found.ID)
	return &contracts.ReviewPublicationReceipt{ReviewId: &id, CommentIds: ids, HeadSha: plan.HeadSha, AuthorAppId: int(g.Config.GitHubAppID), Marker: plan.Marker, State: "confirmed", Mode: contracts.ReviewPublicationReceiptMode(plan.Mode)}, nil
}
func (g *Gateway) CreateReview(ctx context.Context, token, name string, number int, plan contracts.ReviewPublicationPlan) (contracts.ReviewPublicationReceipt, error) {
	path, err := RepositoryPath(name)
	if err != nil {
		return contracts.ReviewPublicationReceipt{}, err
	}
	comments := []map[string]any{}
	for _, finding := range plan.Findings {
		if finding.Line == nil {
			continue
		}
		comments = append(comments, map[string]any{"path": finding.Path, "line": *finding.Line, "side": "RIGHT", "body": contracts.RedactReview(finding.Body) + "\n\n" + plan.Marker})
	}
	if len(plan.Findings) == 0 {
		return contracts.ReviewPublicationReceipt{}, fault.New(409, "EMPTY_REVIEW")
	}
	var remote remoteReview
	if err = g.reviewJSON(ctx, fmt.Sprintf("/repos/%s/pulls/%d/reviews", path, number), token, "POST", map[string]any{"commit_id": plan.HeadSha, "event": "COMMENT", "body": reviewBody(plan), "comments": comments}, &remote); err != nil {
		return contracts.ReviewPublicationReceipt{}, err
	}
	// Verify all remote comments independently instead of trusting the POST response.
	receipt, err := g.FindReview(ctx, token, name, number, plan)
	if err != nil {
		return contracts.ReviewPublicationReceipt{}, err
	}
	if receipt == nil {
		return contracts.ReviewPublicationReceipt{}, fault.New(503, "REVIEW_RECEIPT_MISSING")
	}
	return *receipt, nil
}
func (g *Gateway) FindReviewCheck(ctx context.Context, token, name string, plan contracts.ReviewPublicationPlan) (*Check, error) {
	path, err := RepositoryPath(name)
	if err != nil {
		return nil, err
	}
	var found *Check
	for page := 1; page <= 50; page++ {
		var response struct {
			Checks []Check `json:"check_runs"`
		}
		if err = g.reviewJSON(ctx, fmt.Sprintf("/repos/%s/commits/%s/check-runs?filter=all&check_name=%s&per_page=100&page=%d", path, plan.HeadSha, url.QueryEscape(ReviewCheckName), page), token, "GET", nil, &response); err != nil {
			return nil, err
		}
		for _, check := range response.Checks {
			if check.ID > 0 && check.Name == ReviewCheckName && check.HeadSHA == plan.HeadSha && check.ExternalID == "reviewpilot:"+plan.RunId+":"+plan.PlanHash && check.App.ID == g.Config.GitHubAppID {
				if found != nil {
					return nil, fault.New(409, "DUPLICATE_REVIEW_CHECK")
				}
				copy := check
				found = &copy
			}
		}
		if len(response.Checks) < 100 {
			return found, nil
		}
	}
	return nil, fault.New(503, "GITHUB_PAGINATION_LIMIT")
}
func (g *Gateway) WriteReviewCheck(ctx context.Context, token, name string, plan contracts.ReviewPublicationPlan, complete bool) (Check, error) {
	path, err := RepositoryPath(name)
	if err != nil {
		return Check{}, err
	}
	label := "部分覆盖"
	if complete {
		label = "本次覆盖完成"
	}
	summary := fmt.Sprintf("%s；本次发布 %d 个根因。AI 审查不代表代码正确或安全。", label, len(plan.Findings))
	if len(plan.Findings) == 0 {
		summary = label + "；本次覆盖内未发现可发布问题。"
	}
	var check Check
	err = g.reviewJSON(ctx, "/repos/"+path+"/check-runs", token, "POST", map[string]any{"name": ReviewCheckName, "external_id": "reviewpilot:" + plan.RunId + ":" + plan.PlanHash, "head_sha": plan.HeadSha, "status": "completed", "conclusion": "neutral", "output": map[string]string{"title": label, "summary": summary}}, &check)
	if err == nil && (check.ID <= 0 || check.Name != ReviewCheckName || check.HeadSHA != plan.HeadSha || check.ExternalID != "reviewpilot:"+plan.RunId+":"+plan.PlanHash || check.App.ID != g.Config.GitHubAppID || check.Status != "completed" || check.Conclusion == nil || *check.Conclusion != "neutral") {
		err = fault.New(503, "GITHUB_CHECK_INVALID_RESPONSE")
	}
	return check, err
}

func ReviewMarker(runID, hash string) string {
	return "<!-- reviewpilot:" + strings.ToLower(runID) + ":" + hash + " -->"
}
