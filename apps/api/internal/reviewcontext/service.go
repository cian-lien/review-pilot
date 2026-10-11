package reviewcontext

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"os"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/jobs"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	DB      *gorm.DB
	Gateway *github.Gateway
	Policy  contracts.ReviewPolicy
}

func New(db *gorm.DB, gateway *github.Gateway, policy contracts.ReviewPolicy) *Service {
	return &Service{db, gateway, policy}
}

type manifest struct {
	ID          string
	RunID       string
	InputHash   string
	Files       json.RawMessage
	Pages       json.RawMessage
	Coverage    json.RawMessage
	NextIndex   int
	Sealed      bool
	ContextHash *string
	ExpiresAt   time.Time
}

func (manifest) TableName() string { return "review_context_manifests" }

func (s *Service) transaction(ctx context.Context, l contracts.ReviewLease, fn func(*gorm.DB) error) error {
	if s.DB == nil {
		return fault.New(503, "DATABASE_UNAVAILABLE")
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := jobs.CheckReviewLease(tx, l); err != nil {
			return err
		}
		return fn(tx)
	})
}
func (s *Service) remote(ctx context.Context, l contracts.ReviewLease, token string) error {
	i := l.Snapshot.Identity
	repository, err := s.Gateway.UserRepository(ctx, token, i.RepositoryFullName)
	if err != nil || !repository.Public() || repository.ID != l.Tenant.RepositoryId {
		return s.stop(ctx, l, "Cancelled", "REVIEW_ACCESS_REVOKED")
	}
	base, head, state, err := s.Gateway.FixedPull(ctx, token, i.RepositoryFullName, i.PrNumber)
	if err != nil {
		return err
	}
	if state != "open" {
		return s.stop(ctx, l, "Cancelled", "REVIEW_PR_CLOSED")
	}
	if base != i.BaseSha || head != i.HeadSha {
		return s.stop(ctx, l, "Stale", "REVIEW_STALE")
	}
	return nil
}
func (s *Service) stop(ctx context.Context, l contracts.ReviewLease, status, code string) error {
	err := s.transaction(ctx, l, func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE review_runs SET status=?,reason_code=?,updated_at=clock_timestamp() WHERE id=?", status, code, l.RunId).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE jobs SET status='cancelled',lease_token=NULL,lease_expires_at=NULL,last_error_code=? WHERE id=?", code, l.TaskId).Error
	})
	if err != nil {
		return err
	}
	return fault.New(409, code)
}
func (s *Service) Read(ctx context.Context, req contracts.ReadReviewContextRequest) (contracts.ReviewContextPage, error) {
	l := req.Lease
	answer := contracts.ReviewContextPage{}
	if err := s.transaction(ctx, l, func(tx *gorm.DB) error { return nil }); err != nil {
		return answer, err
	}
	cfg, err := config.LoadReview()
	if err != nil {
		return answer, fault.New(503, "REVIEW_CONFIG_INVALID")
	}
	if cfg.Mode != string(l.Snapshot.Provenance.ModelMode) || cfg.Model.ModelConfigVersion != l.Snapshot.ModelConfigVersion || cfg.Model.PriceVersion != l.Snapshot.PriceVersion {
		return answer, fault.New(409, "MODEL_CONFIG_MISMATCH")
	}
	if cfg.Mode == "fake" && (s.Gateway.APIURL != os.Getenv("TEST_GITHUB_BASE_URL") || l.Snapshot.Provenance.FixtureId != cfg.Model.FixtureID) {
		return answer, fault.New(403, "FAKE_HOST_MISMATCH")
	}
	if cfg.Mode == "real" && s.Gateway.APIURL != "https://api.github.com" {
		return answer, fault.New(403, "GITHUB_HOST_MISMATCH")
	}
	token, err := s.Gateway.ReviewReadToken(ctx, l.Tenant)
	if err != nil {
		return answer, err
	}
	if err = s.remote(ctx, l, token); err != nil {
		return answer, err
	}
	var m manifest
	err = s.transaction(ctx, l, func(tx *gorm.DB) error { return tx.First(&m, "run_id=?", l.RunId).Error })
	if err == gorm.ErrRecordNotFound {
		files, e := s.Gateway.PullFiles(ctx, token, l.Snapshot.Identity.RepositoryFullName, l.Snapshot.Identity.PrNumber)
		if e != nil {
			return answer, e
		}
		if e = s.remote(ctx, l, token); e != nil {
			return answer, e
		}
		sort.Slice(files, func(a, b int) bool { return files[a].Filename < files[b].Filename })
		coverage := contracts.ReviewCoverage{Complete: true, TotalFiles: len(files), Skipped: []contracts.ReviewSkippedFile{}, Limitations: []string{"静态文本证据，不含跨文件调用图或运行时验证"}}
		for _, file := range files {
			coverage.TotalChangedLines += file.Additions + file.Deletions
		}
		selected := []github.PullFile{}
		changed := 0
		for _, file := range files {
			reason := ""
			size := file.Additions + file.Deletions
			if len(selected) >= s.Policy.MaxFiles {
				reason = "FILE_LIMIT"
			} else if changed+size > s.Policy.MaxChangedLines {
				reason = "CHANGED_LINE_LIMIT"
			} else if !(strings.HasSuffix(file.Filename, ".ts") || strings.HasSuffix(file.Filename, ".tsx") || strings.HasSuffix(file.Filename, ".js") || strings.HasSuffix(file.Filename, ".jsx")) {
				reason = "UNSUPPORTED_FILE"
			}
			if reason != "" {
				coverage.Complete = false
				coverage.Skipped = append(coverage.Skipped, contracts.ReviewSkippedFile{Path: file.Filename, ReasonCode: reason, ChangedLines: size})
				continue
			}
			selected = append(selected, file)
			changed += size
		}
		fileBody, _ := json.Marshal(selected)
		coverageBody, _ := json.Marshal(coverage)
		m = manifest{ID: jobs.UUID(), RunID: l.RunId, InputHash: l.InputHash, Files: fileBody, Pages: json.RawMessage("[]"), Coverage: coverageBody, ExpiresAt: l.Snapshot.Provenance.ContentExpiresAt}
		err = s.transaction(ctx, l, func(tx *gorm.DB) error {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "run_id"}}, DoNothing: true}).Create(&m).Error; err != nil {
				return err
			}
			return tx.First(&m, "run_id=?", l.RunId).Error
		})
	}
	if err != nil {
		return answer, err
	}
	if !m.ExpiresAt.After(time.Now()) {
		return answer, fault.New(409, "CONTEXT_EXPIRED")
	}
	index := 0
	if req.Cursor != nil {
		prefix := m.ID + ":"
		if !strings.HasPrefix(*req.Cursor, prefix) {
			return answer, fault.New(400, "INVALID_CONTEXT_CURSOR")
		}
		index, err = strconv.Atoi(strings.TrimPrefix(*req.Cursor, prefix))
		if err != nil || index < 0 {
			return answer, fault.New(400, "INVALID_CONTEXT_CURSOR")
		}
	}
	var pages []contracts.ReviewContextPage
	var files []github.PullFile
	var coverage contracts.ReviewCoverage
	if json.Unmarshal(m.Pages, &pages) != nil || json.Unmarshal(m.Files, &files) != nil || json.Unmarshal(m.Coverage, &coverage) != nil {
		return answer, fault.New(503, "CONTEXT_INVALID")
	}
	if index < len(pages) {
		return pages[index], nil
	}
	if index != m.NextIndex || index > len(files) || m.Sealed {
		return answer, fault.New(409, "CONTEXT_PAGE_MISSING")
	}
	answer = contracts.ReviewContextPage{ManifestId: m.ID, Cursor: req.Cursor, Files: []contracts.ReviewChangedFile{}, Evidence: []contracts.CodeEvidence{}, Coverage: coverage}
	if index < len(files) {
		file := files[index]
		changedFile := contracts.ReviewChangedFile{Path: file.Filename, PreviousPath: file.PreviousFilename, Status: contracts.ReviewChangedFileStatus(file.Status), Additions: file.Additions, Deletions: file.Deletions, Patch: file.Patch}
		basePath := file.Filename
		if file.PreviousFilename != nil {
			basePath = *file.PreviousFilename
		}
		if coverage.RawBytes+2*(s.Policy.MaxFileBytes+1) > s.Policy.MaxRawBytes {
			answer.Coverage.Complete = false
			answer.Coverage.Skipped = append(answer.Coverage.Skipped, contracts.ReviewSkippedFile{Path: file.Filename, ReasonCode: "RAW_BYTE_LIMIT", ChangedLines: file.Additions + file.Deletions})
		} else {
			changedFile.Base = contracts.ReviewFileContent{Sha: l.Snapshot.Identity.BaseSha, State: "deleted"}
			changedFile.Head = contracts.ReviewFileContent{Sha: l.HeadSha, State: "deleted"}
			if file.Status != "added" {
				changedFile.Base, err = s.Gateway.FixedContent(ctx, token, l.Snapshot.Identity.RepositoryFullName, basePath, l.Snapshot.Identity.BaseSha, s.Policy.MaxFileBytes)
				if err != nil {
					return answer, err
				}
			}
			if file.Status != "removed" {
				changedFile.Head, err = s.Gateway.FixedContent(ctx, token, l.Snapshot.Identity.RepositoryFullName, file.Filename, l.HeadSha, s.Policy.MaxFileBytes)
				if err != nil {
					return answer, err
				}
			}
			answer.Coverage.RawBytes += changedFile.Base.Bytes + changedFile.Head.Bytes
			if changedFile.Base.State == "too-large" {
				answer.Coverage.RawBytes++
			}
			if changedFile.Head.State == "too-large" {
				answer.Coverage.RawBytes++
			}
			reason := ""
			base, head := "", ""
			if changedFile.Base.Text != nil {
				base = *changedFile.Base.Text
			}
			if changedFile.Head.Text != nil {
				head = *changedFile.Head.Text
			}
			if file.Patch == nil {
				reason = "MISSING_PATCH"
			} else if changedFile.Head.State != "available" || (file.Status != "added" && changedFile.Base.State != "available") {
				reason = "INSUFFICIENT_CONTEXT"
			} else if !MatchPatch(*file.Patch, base, head, file.Additions, file.Deletions) {
				reason = "PATCH_MISMATCH"
			}
			for _, content := range []*contracts.ReviewFileContent{&changedFile.Base, &changedFile.Head} {
				if content.Text != nil {
					hash := jobs.Hash([]byte(*content.Text))
					content.ContentHash = &hash
					text := contracts.RedactReview(*content.Text)
					content.Text = &text
				}
			}
			if changedFile.Patch != nil {
				redacted := contracts.RedactReview(*changedFile.Patch)
				changedFile.Patch = &redacted
			}
			if reason != "" {
				answer.Coverage.Complete = false
				answer.Coverage.Skipped = append(answer.Coverage.Skipped, contracts.ReviewSkippedFile{Path: file.Filename, ReasonCode: reason, ChangedLines: file.Additions + file.Deletions})
			} else {
				answer.Coverage.ReadFiles++
				answer.Coverage.CoveredChangedLines += file.Additions + file.Deletions
				answer.Evidence = append(answer.Evidence, s.evidence(l, file.Filename, changedFile.Head)...)
				answer.Evidence = append(answer.Evidence, s.evidence(l, basePath, changedFile.Base)...)
			}
			answer.Files = append(answer.Files, changedFile)
		}
	}
	if index+1 < len(files) {
		cursor := fmt.Sprintf("%s:%d", m.ID, index+1)
		answer.NextCursor = &cursor
	} else {
		answer.Complete = true
	}
	body, _ := json.Marshal(answer)
	if len(body) > s.Policy.MaxPageBytes {
		answer.Files = []contracts.ReviewChangedFile{}
		answer.Evidence = []contracts.CodeEvidence{}
		answer.Coverage.Complete = false
		answer.Coverage.ReadFiles = coverage.ReadFiles
		answer.Coverage.CoveredChangedLines = coverage.CoveredChangedLines
		path := ""
		if index < len(files) {
			path = files[index].Filename
		}
		answer.Coverage.Skipped = append(answer.Coverage.Skipped, contracts.ReviewSkippedFile{Path: path, ReasonCode: "PAGE_BYTE_LIMIT"})
	}
	if err = s.remote(ctx, l, token); err != nil {
		return contracts.ReviewContextPage{}, err
	}
	err = s.transaction(ctx, l, func(tx *gorm.DB) error {
		var current manifest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id=?", m.ID).Error; err != nil {
			return err
		}
		if current.NextIndex != index {
			return fault.New(409, "CONTEXT_PAGE_CONFLICT")
		}
		pages = append(pages, answer)
		if answer.Complete {
			canonical, _ := json.Marshal(pages)
			hash := jobs.Hash(canonical)
			answer.ContextHash = &hash
			pages[len(pages)-1] = answer
		}
		for _, evidence := range answer.Evidence {
			r := evidence.Ref
			if err := tx.Exec("INSERT INTO review_evidence(id,run_id,manifest_id,installation_id,repository_id,path,sha,content_hash,start_line,end_line,original_hash,snippet,redacted,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)", r.EvidenceId, l.RunId, m.ID, l.Tenant.InstallationId, l.Tenant.RepositoryId, r.Path, r.Sha, r.ContentHash, r.StartLine, r.EndLine, r.ContentHash, evidence.Snippet, evidence.Redacted, m.ExpiresAt).Error; err != nil {
				return err
			}
		}
		pageBody, _ := json.Marshal(pages)
		coverageBody, _ := json.Marshal(answer.Coverage)
		return tx.Exec("UPDATE review_context_manifests SET pages=?::jsonb,coverage=?::jsonb,next_index=next_index+1,sealed=?,context_hash=? WHERE id=?", string(pageBody), string(coverageBody), answer.Complete, answer.ContextHash, m.ID).Error
	})
	return answer, err
}

func (s *Service) evidence(l contracts.ReviewLease, path string, content contracts.ReviewFileContent) []contracts.CodeEvidence {
	result := []contracts.CodeEvidence{}
	if content.Text == nil || content.ContentHash == nil {
		return result
	}
	lines := codeLines(*content.Text)
	for start := 0; start < len(lines); {
		end := start
		bytes := 0
		for end < len(lines) && end-start < 32 && bytes+len(lines[end])+1 <= s.Policy.MaxSnippetBytes {
			bytes += len(lines[end]) + 1
			end++
		}
		if end == start {
			start++
			continue
		}
		snippet := strings.Join(lines[start:end], "\n")
		result = append(result, contracts.CodeEvidence{Tenant: l.Tenant, ReadState: "available", Redacted: strings.Contains(snippet, "[REDACTED]"), Snippet: snippet,
			Ref: contracts.EvidenceRef{EvidenceId: jobs.UUID(), Path: path, Sha: content.Sha, StartLine: start + 1, EndLine: end, ContentHash: *content.ContentHash}})
		start = end
	}
	return result
}
