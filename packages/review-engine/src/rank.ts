import type { ReviewCandidate } from '@reviewpilot/contracts';
export function rankCandidates(candidates: ReviewCandidate[]): ReviewCandidate[] {
  const rank = { high: 0, medium: 1, low: 2 };
  return [...candidates].sort(
    (a, b) =>
      rank[a.severity] - rank[b.severity] || a.candidateId.localeCompare(b.candidateId, 'en'),
  );
}
