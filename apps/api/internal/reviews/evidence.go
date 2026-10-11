package reviews

import (
	"encoding/json"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
	"regexp"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/reviewcontext"
	"strconv"
	"strings"
)

var identifier = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var hunk = regexp.MustCompile(`^@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)

func normalize(value string) string {
	return strings.Join(strings.Fields(norm.NFKC.String(value)), " ")
}
func RootFingerprint(candidate contracts.ReviewCandidate, facts []contracts.CodeEvidence) (string, []byte) {
	// Stable local code and normalized root statements intentionally exclude titles, SHA and line numbers.
	anchor := normalize(facts[0].Snippet)
	key := struct {
		Version     string `json:"version"`
		Category    string `json:"category"`
		Path        string `json:"path"`
		AnchorHash  string `json:"anchorHash"`
		Trigger     string `json:"trigger"`
		Consequence string `json:"consequence"`
	}{"fingerprint-v1", string(candidate.Category), facts[0].Ref.Path, jobs.Hash([]byte(anchor)), normalize(contracts.RedactReview(candidate.RootCauseStatement.Trigger)), normalize(contracts.RedactReview(candidate.RootCauseStatement.Consequence))}
	body, _ := json.Marshal(key)
	return jobs.Hash(body), body
}
func addedLocation(file contracts.ReviewChangedFile, ref contracts.EvidenceRef) bool {
	if file.Path != ref.Path || file.Head.Sha != ref.Sha || file.Patch == nil || file.Head.Text == nil || (file.Status != "added" && file.Base.Text == nil) {
		return false
	}
	base := ""
	if file.Base.Text != nil {
		base = *file.Base.Text
	}
	if !reviewcontext.MatchPatch(*file.Patch, base, *file.Head.Text, file.Additions, file.Deletions) {
		return false
	}
	line := 0
	for _, part := range strings.Split(strings.ReplaceAll(*file.Patch, "\r\n", "\n"), "\n") {
		if match := hunk.FindStringSubmatch(part); match != nil {
			line, _ = strconv.Atoi(match[1])
			continue
		}
		if strings.HasPrefix(part, "+") {
			if line >= ref.StartLine && line <= ref.EndLine {
				return true
			}
			line++
		} else if strings.HasPrefix(part, " ") {
			line++
		}
	}
	return false
}
func validateEvidence(tx *gorm.DB, l contracts.ReviewLease, pages []contracts.ReviewContextPage, candidate contracts.ReviewCandidate, decision contracts.VerificationDecision) ([]contracts.CodeEvidence, string, bool, error) {
	facts := []contracts.CodeEvidence{}
	reason := ""
	if !candidate.IntroducedByPR {
		reason = "PREEXISTING_ISSUE"
	}
	if len(candidate.Evidence) == 0 || len(decision.Evidence) == 0 {
		reason = "INSUFFICIENT_CONTEXT"
	}
	for index, ref := range append(append([]contracts.EvidenceRef{}, candidate.Evidence...), decision.Evidence...) {
		var rows []struct {
			RunID          string
			InstallationID int64
			RepositoryID   int64
			Path           string
			Sha            string
			ContentHash    string
			StartLine      int
			EndLine        int
			Snippet        *string
			Redacted       bool
			Live           bool
		}
		if err := tx.Raw("SELECT run_id,installation_id,repository_id,path,sha,content_hash,start_line,end_line,snippet,redacted,expires_at>clock_timestamp() AS live FROM review_evidence WHERE id=?", ref.EvidenceId).Scan(&rows).Error; err != nil {
			return nil, "", false, err
		}
		if len(rows) == 0 {
			reason = "INVALID_EVIDENCE"
			continue
		}
		e := rows[0]
		if e.RunID != l.RunId || e.InstallationID != int64(l.Tenant.InstallationId) || e.RepositoryID != int64(l.Tenant.RepositoryId) {
			return nil, "", true, nil
		}
		if !l.Snapshot.Policy.RetainContent && e.Snippet == nil {
			for _, page := range pages {
				for _, cached := range page.Evidence {
					if cached.Ref.EvidenceId == ref.EvidenceId {
						value := cached.Snippet
						e.Snippet = &value
					}
				}
			}
		}
		if !e.Live || e.Snippet == nil || ref.Path != e.Path || ref.Sha != e.Sha || ref.ContentHash != e.ContentHash || ref.StartLine < e.StartLine || ref.EndLine > e.EndLine || ref.EndLine < ref.StartLine {
			reason = "INVALID_EVIDENCE"
			continue
		}
		if index < len(candidate.Evidence) {
			lines := strings.Split(strings.ReplaceAll(*e.Snippet, "\r\n", "\n"), "\n")
			start, end := ref.StartLine-e.StartLine, ref.EndLine-e.StartLine+1
			if start < 0 || end > len(lines) {
				reason = "INVALID_EVIDENCE"
				continue
			}
			facts = append(facts, contracts.CodeEvidence{Ref: ref, Tenant: l.Tenant, Snippet: contracts.RedactReview(strings.Join(lines[start:end], "\n")), ReadState: "available", Redacted: e.Redacted})
		}
	}
	changed := false
	old := false
	for _, ref := range candidate.Evidence {
		for _, page := range pages {
			for _, file := range page.Files {
				if addedLocation(file, ref) {
					changed = true
					if len(facts) > 0 && file.Base.Text != nil && normalize(facts[0].Snippet) != "" && strings.Contains(normalize(*file.Base.Text), normalize(facts[0].Snippet)) {
						old = true
					}
				}
			}
		}
	}
	if reason == "" && !changed {
		reason = "NOT_CHANGED_LOCATION"
	}
	if reason == "" && old {
		reason = "PREEXISTING_ISSUE"
	}
	if reason == "" && len(facts) == 0 {
		reason = "INSUFFICIENT_CONTEXT"
	}
	return facts, reason, false, nil
}
