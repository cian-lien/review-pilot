import type {
  EvidenceRef,
  ReviewCandidate,
  ReviewContextPage,
  VerificationDecision,
} from '@reviewpilot/contracts';
import { parseDiff, validateDiffLocation } from '@reviewpilot/repository';
const normalize = (value: string) => value.normalize('NFKC').replace(/\s+/gu, ' ').trim();
export function validateDecisions(
  candidates: ReviewCandidate[],
  decisions: VerificationDecision[],
  context: ReviewContextPage,
): VerificationDecision[] {
  if (
    new Set(candidates.map((c) => c.candidateId)).size !== candidates.length ||
    decisions.length !== candidates.length ||
    new Set(decisions.map((d) => d.candidateId)).size !== decisions.length ||
    decisions.some((d) => !candidates.some((c) => c.candidateId === d.candidateId))
  )
    throw new Error('MODEL_DECISION_INVALID');
  const diff = parseDiff(context.files),
    roots = new Set<string>();
  function valid(ref: EvidenceRef): boolean {
    return context.evidence.some(
      (e) =>
        e.ref.evidenceId === ref.evidenceId &&
        e.ref.path === ref.path &&
        e.ref.sha === ref.sha &&
        e.ref.contentHash === ref.contentHash &&
        e.ref.startLine <= ref.startLine &&
        e.ref.endLine >= ref.endLine &&
        ref.endLine >= ref.startLine,
    );
  }
  return candidates.map((candidate) => {
    const decision = decisions.find((d) => d.candidateId === candidate.candidateId)!;
    let reason: string | undefined;
    if (!candidate.introducedByPR) reason = 'PREEXISTING_ISSUE';
    else if (!candidate.evidence.length || !decision.evidence.length)
      reason = 'INSUFFICIENT_CONTEXT';
    else if ([...candidate.evidence, ...decision.evidence].some((ref) => !valid(ref)))
      reason = 'INVALID_EVIDENCE';
    else if (!candidate.evidence.some((ref) => validateDiffLocation(ref, diff)))
      reason = 'NOT_CHANGED_LOCATION';
    const root = JSON.stringify([
      candidate.category,
      candidate.evidence[0]?.path,
      normalize(candidate.rootCauseStatement.trigger),
      normalize(candidate.rootCauseStatement.consequence),
    ]);
    if (!reason && decision.verdict === 'verified') {
      if (roots.has(root)) reason = 'DUPLICATE_ROOT';
      else roots.add(root);
    }
    return reason ? { ...decision, verdict: 'abstain', reasonCode: reason } : decision;
  });
}
