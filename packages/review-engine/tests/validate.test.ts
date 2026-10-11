import { expect, it } from 'vitest';
import type { ReviewCandidate, VerificationDecision } from '@reviewpilot/contracts';
import { validateDecisions } from '../src/validate';
import { candidate, contextPage, ref } from './fixture';

it('keeps distinct code anchors but deduplicates the same local root', () => {
  const page = structuredClone(contextPage);
  page.files[0]!.status = 'added';
  page.files[0]!.additions = 2;
  page.files[0]!.deletions = 0;
  page.files[0]!.patch =
    '@@ -0,0 +1,2 @@\n+capture(orderA, crypto.randomUUID());\n+capture(orderB, crypto.randomUUID());';
  page.files[0]!.base.text = '';
  page.files[0]!.head.text =
    'capture(orderA, crypto.randomUUID());\ncapture(orderB, crypto.randomUUID());';
  const fullRef = { ...ref, startLine: 1, endLine: 2 };
  page.evidence = [{ ...page.evidence[0]!, ref: fullRef, snippet: page.files[0]!.head.text }];
  const candidates: ReviewCandidate[] = [1, 2, 1].map((line, index) => ({
    ...structuredClone(candidate),
    candidateId: `candidate-${index + 1}`,
    evidence: [{ ...fullRef, startLine: line, endLine: line }],
  }));
  const decisions: VerificationDecision[] = candidates.map((value) => ({
    candidateId: value.candidateId,
    verdict: 'verified',
    reasonCode: 'EVIDENCE_SUPPORTED',
    evidence: value.evidence,
  }));
  const result = validateDecisions(candidates, decisions, page);
  expect(result.map((value) => value.verdict)).toEqual(['verified', 'verified', 'abstain']);
  expect(result[2]!.reasonCode).toBe('DUPLICATE_ROOT');
});
