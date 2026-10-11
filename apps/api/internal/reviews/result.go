package reviews

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/observability"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/reviewcontext"
	"reviewpilot.local/api/internal/reviewstages"
)

type BusinessResult struct {
	ResultID    string                           `json:"resultId"`
	RunID       string                           `json:"runId"`
	InputHash   string                           `json:"inputHash"`
	ContextHash string                           `json:"contextHash"`
	Candidates  []contracts.ReviewCandidate      `json:"candidates"`
	Decisions   []contracts.VerificationDecision `json:"decisions"`
	Coverage    contracts.ReviewCoverage         `json:"coverage"`
	CallIDs     []string                         `json:"callIds"`
}

func Business(result contracts.ReviewResult) BusinessResult {
	value := BusinessResult{result.ResultId, result.Lease.RunId, result.Lease.InputHash, result.ContextHash, result.Candidates, result.Decisions, result.Coverage, result.CallIds}
	body, _ := json.Marshal(value)
	_ = json.Unmarshal(contracts.RedactReviewJSON(body), &value)
	return value
}
func BusinessHash(result contracts.ReviewResult) string {
	body, _ := json.Marshal(Business(result))
	return jobs.Hash(body)
}
func (s *Service) Commit(ctx context.Context, req contracts.CommitReviewRequest) (contracts.ReviewCommitResponse, error) {
	_, span := observability.FromContext(ctx).Start(ctx, "review.result.commit", observability.Fields{RunID: req.Result.Lease.RunId, TaskID: req.Result.Lease.TaskId})
	defer span.End()
	result := req.Result
	l := result.Lease
	answer := contracts.ReviewCommitResponse{}
	raw, _ := json.Marshal(result)
	if contracts.DecodeReview("ReviewResult", raw, new(contracts.ReviewResult), 65536) != nil {
		return answer, fault.New(400, "INVALID_REVIEW_RESULT")
	}
	if result.ResultId != l.TaskId+":review" || len(raw) > l.Snapshot.Policy.MaxResultBytes {
		return answer, fault.New(409, "RESULT_IDENTITY_MISMATCH")
	}
	hash := BusinessHash(result)
	fatalReference := false
	err := s.transaction(ctx, func(tx *gorm.DB) error {
		// Confirmed immutable business facts are independent of a transport lease's expiry.
		var runtime persistence.RuntimeControl
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&runtime, 1).Error; err != nil {
			return err
		}
		j, r, authorized, err := jobs.LockReview(tx, l.TaskId)
		if err != nil {
			return err
		}
		if !authorized || r.PipelineVersion != "review-v1" || j.RunID != l.RunId || l.InputHash != jobs.Hash(r.Snapshot) || r.InstallationID != int64(l.Tenant.InstallationId) || r.RepositoryID != int64(l.Tenant.RepositoryId) || r.HeadSHA != l.HeadSha {
			return fault.New(409, "RESULT_IDENTITY_MISMATCH")
		}
		var existing []struct {
			ResultHash string
			ResultID   string
		}
		if err = tx.Raw("SELECT result_hash,result_id FROM job_results WHERE task_id=?", j.ID).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) > 0 {
			if existing[0].ResultHash != hash || existing[0].ResultID != result.ResultId {
				return fault.New(409, "RESULT_CONFLICT")
			}
			answer = contracts.ReviewCommitResponse{Outcome: "already-accepted", ResultId: result.ResultId, ResultHash: hash}
			return nil
		}
		if _, err = jobs.CheckReviewLease(tx, l); err != nil {
			return err
		}
		pages, coverage, err := sealedContext(tx, l, result.ContextHash)
		if err != nil {
			return err
		}
		if !l.Snapshot.Policy.RetainContent {
			var present bool
			_, pages, present = reviewcontext.EphemeralContext(l)
			if !present {
				return fault.New(409, "CONTEXT_CONTENT_UNAVAILABLE")
			}
		}
		if !validCoverage(coverage, result.Coverage) {
			return fault.New(409, "COVERAGE_MISMATCH")
		}
		if len(result.Candidates) != len(result.Decisions) {
			return fault.New(409, "VERIFICATION_INCOMPLETE")
		}
		callIDs := map[string]bool{}
		for _, id := range result.CallIds {
			if callIDs[id] {
				return fault.New(409, "RESULT_CALL_CONFLICT")
			}
			callIDs[id] = true
		}
		payloads := []struct {
			stage   string
			payload contracts.ReviewStagePayload
		}{{"candidates", contracts.ReviewStagePayload{Candidates: &contracts.ReviewCandidates{Candidates: result.Candidates}}}}
		if len(result.Candidates) > 0 {
			payloads = append(payloads, struct {
				stage   string
				payload contracts.ReviewStagePayload
			}{"verification", contracts.ReviewStagePayload{Verification: &contracts.ReviewVerification{Decisions: result.Decisions}}})
		}
		if len(callIDs) != len(payloads) {
			return fault.New(409, "RESULT_CALL_CONFLICT")
		}
		for _, p := range payloads {
			key, e := reviewstages.StageInputHash(tx, l, p.stage)
			if e != nil {
				return e
			}
			var stage reviewstages.Record
			if e = tx.Where("run_id=? AND stage=? AND stage_input_hash=? AND expires_at>clock_timestamp()", l.RunId, p.stage, key).First(&stage).Error; e != nil {
				return fault.New(409, "RESULT_STAGE_MISSING")
			}
			b, _ := json.Marshal(p.payload)
			var normalized contracts.ReviewStagePayload
			_ = json.Unmarshal(contracts.RedactReviewJSON(b), &normalized)
			b, _ = json.Marshal(normalized)
			if stage.OutputHash != jobs.Hash(b) || stage.CallID == nil || !callIDs[*stage.CallID] {
				return fault.New(409, "RESULT_STAGE_CONFLICT")
			}
			delete(callIDs, *stage.CallID)
		}
		var incomplete int64
		if err = tx.Raw("SELECT count(*) FROM model_calls WHERE run_id=? AND state NOT IN('settled','released')", l.RunId).Scan(&incomplete).Error; err != nil {
			return err
		}
		if incomplete != 0 {
			return fault.New(409, "MODEL_FACTS_INCOMPLETE")
		}
		seen := map[string]bool{}
		roots := map[string]bool{}
		for _, candidate := range result.Candidates {
			if seen[candidate.CandidateId] {
				return fault.New(409, "CANDIDATE_ID_CONFLICT")
			}
			seen[candidate.CandidateId] = true
			var decision *contracts.VerificationDecision
			for index := range result.Decisions {
				d := &result.Decisions[index]
				if d.CandidateId == candidate.CandidateId {
					if decision != nil {
						return fault.New(409, "VERIFICATION_INCOMPLETE")
					}
					decision = d
				}
			}
			if decision == nil {
				return fault.New(409, "VERIFICATION_INCOMPLETE")
			}
			facts, reason, cross, e := validateEvidence(tx, l, pages, candidate, *decision)
			if e != nil {
				return e
			}
			if cross {
				fatalReference = true
				if e = tx.Exec("UPDATE review_runs SET status='Failed',reason_code='CROSS_SCOPE_EVIDENCE',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; e != nil {
					return e
				}
				return tx.Exec("UPDATE jobs SET status='failed',lease_token=NULL,lease_expires_at=NULL WHERE id=?", j.ID).Error
			}
			if reason == "" && decision.Verdict != "verified" {
				reason = contracts.RedactReview(decision.ReasonCode)
				if reason == "" {
					reason = "INSUFFICIENT_EVIDENCE"
				}
			}
			fingerprint, rootKey := "", []byte("{}")
			if reason == "" {
				fingerprint, rootKey = RootFingerprint(candidate, facts)
				if roots[fingerprint] {
					reason = "DUPLICATE_ROOT"
				} else {
					roots[fingerprint] = true
				}
			}
			candidateBody, _ := json.Marshal(redactedCandidate(candidate))
			refs, _ := json.Marshal(candidate.Evidence)
			evidence, _ := json.Marshal(facts)
			var persistedCandidate, persistedEvidence any = string(candidateBody), string(evidence)
			if !l.Snapshot.Policy.RetainContent {
				persistedCandidate = nil
				persistedEvidence = nil
				rootKey = []byte("{}")
			}
			if reason != "" {
				var title any = contracts.RedactReview(candidate.Title)
				var storedRefs any = string(refs)
				if !l.Snapshot.Policy.RetainContent {
					title = nil
					storedRefs = nil
				}
				if e = tx.Exec("INSERT INTO review_filtered_candidates(run_id,candidate_id,title,reason_code,evidence,expires_at) VALUES(?,?,?,?,?::jsonb,?)", r.ID, candidate.CandidateId, title, reason, storedRefs, l.Snapshot.Provenance.ContentExpiresAt).Error; e != nil {
					return e
				}
			} else if e = tx.Exec("INSERT INTO review_findings(id,run_id,fingerprint,root_key,candidate,evidence,expires_at) VALUES(?,?,?,?::jsonb,?::jsonb,?::jsonb,?)", jobs.UUID(), r.ID, fingerprint, string(rootKey), persistedCandidate, persistedEvidence, l.Snapshot.Provenance.ContentExpiresAt).Error; e != nil {
				return e
			}
		}
		coverageBody, _ := json.Marshal(result.Coverage)
		if err = tx.Exec("INSERT INTO review_coverage(run_id,payload) VALUES(?,?::jsonb)", r.ID, string(coverageBody)).Error; err != nil {
			return err
		}
		// Acceptance retains only normalized business payload, never tokens or lease credentials.
		business, _ := json.Marshal(Business(result))
		if !l.Snapshot.Policy.RetainContent {
			business = []byte("{}")
		}
		if err = tx.Exec("INSERT INTO job_results(task_id,result_id,result_hash,input_hash,payload) VALUES(?,?,?,?,?::jsonb)", j.ID, result.ResultId, hash, l.InputHash, string(business)).Error; err != nil {
			return err
		}
		if err = tx.Exec("UPDATE jobs SET status='result_ready',lease_token=NULL,lease_expires_at=NULL WHERE id=?", j.ID).Error; err != nil {
			return err
		}
		if err = tx.Exec("UPDATE review_runs SET status='Publishing',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; err != nil {
			return err
		}
		if err = tx.Exec("INSERT INTO publication_requests(run_id) VALUES(?) ON CONFLICT DO NOTHING", r.ID).Error; err != nil {
			return err
		}
		answer = contracts.ReviewCommitResponse{Outcome: "accepted", ResultId: result.ResultId, ResultHash: hash}
		return nil
	})
	if err == nil && fatalReference {
		err = fault.New(409, "CROSS_SCOPE_EVIDENCE")
	}
	if err == nil && !l.Snapshot.Policy.RetainContent {
		reviewcontext.ForgetEphemeral(l.RunId)
	}
	return answer, err
}
func (s *Service) Confirm(ctx context.Context, taskID, runID, inputHash string) (contracts.ReviewCommitResponse, error) {
	answer := contracts.ReviewCommitResponse{}
	if s.DB == nil {
		return answer, fault.New(503, "DATABASE_UNAVAILABLE")
	}
	if !identifier.MatchString(taskID) || !identifier.MatchString(runID) || !hashPattern.MatchString(inputHash) {
		return answer, fault.New(400, "INVALID_RESULT_IDENTITY")
	}
	var rows []struct {
		ResultID   string
		ResultHash string
	}
	err := s.DB.WithContext(ctx).Raw("SELECT x.result_id,x.result_hash FROM job_results x JOIN jobs j ON j.id=x.task_id JOIN review_runs r ON r.id=j.run_id JOIN repositories repo ON repo.installation_id=r.installation_id AND repo.id=r.repository_id JOIN installations i ON i.id=r.installation_id WHERE j.id=? AND r.id=? AND x.input_hash=? AND r.pipeline_version='review-v1' AND repo.enabled AND repo.public AND i.active", taskID, runID, inputHash).Scan(&rows).Error
	if err != nil {
		return answer, err
	}
	if len(rows) != 1 {
		return answer, fault.New(404, "REVIEW_RESULT_NOT_FOUND")
	}
	return contracts.ReviewCommitResponse{Outcome: "already-accepted", ResultId: rows[0].ResultID, ResultHash: rows[0].ResultHash}, nil
}
func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func validCoverage(authoritative, provided contracts.ReviewCoverage) bool {
	if sameJSON(authoritative, provided) {
		return true
	}
	// A Worker may reduce claimed coverage, never increase server-established read facts.
	prefix := len(authoritative.Limitations)
	return !provided.Complete && provided.TotalFiles == authoritative.TotalFiles && provided.ReadFiles == authoritative.ReadFiles && provided.TotalChangedLines == authoritative.TotalChangedLines && provided.RawBytes == authoritative.RawBytes && provided.CoveredChangedLines <= authoritative.CoveredChangedLines && provided.CoveredChangedLines >= 0 && sameJSON(provided.Skipped, authoritative.Skipped) && len(provided.Limitations) > prefix && sameJSON(provided.Limitations[:prefix], authoritative.Limitations) && provided.Limitations[len(provided.Limitations)-1] == "MODEL_PROMPT_CONTEXT_LIMIT"
}
func redactedCandidate(c contracts.ReviewCandidate) contracts.ReviewCandidate {
	c.Title = contracts.RedactReview(c.Title)
	c.FailureScenario = contracts.RedactReview(c.FailureScenario)
	c.Impact = contracts.RedactReview(c.Impact)
	c.SuggestedVerification = contracts.RedactReview(c.SuggestedVerification)
	c.RootCauseStatement.Trigger = contracts.RedactReview(c.RootCauseStatement.Trigger)
	c.RootCauseStatement.Consequence = contracts.RedactReview(c.RootCauseStatement.Consequence)
	return c
}
func sealedContext(tx *gorm.DB, l contracts.ReviewLease, hash string) ([]contracts.ReviewContextPage, contracts.ReviewCoverage, error) {
	var rows []struct {
		Pages    json.RawMessage
		Coverage json.RawMessage
	}
	err := tx.Raw("SELECT pages,coverage FROM review_context_manifests WHERE run_id=? AND input_hash=? AND context_hash=? AND sealed AND expires_at>clock_timestamp()", l.RunId, l.InputHash, hash).Scan(&rows).Error
	var pages []contracts.ReviewContextPage
	var coverage contracts.ReviewCoverage
	if err != nil {
		return pages, coverage, err
	}
	if len(rows) != 1 || json.Unmarshal(rows[0].Pages, &pages) != nil || json.Unmarshal(rows[0].Coverage, &coverage) != nil || len(pages) == 0 {
		return pages, coverage, fault.New(409, "CONTEXT_NOT_SEALED")
	}
	return pages, coverage, nil
}
