import { expect, it } from 'vitest';
import { reviewView, reviewActive } from './review-view';
import type { ReviewRunDetail } from '@reviewpilot/contracts';
const detail: ReviewRunDetail = {
  runId: '00000000-0000-4000-8000-000000000001',
  status: 'Partial',
  needsReconciliation: false,
  reasonCode: null,
  contentExpired: false,
  findings: [],
  filtered: [],
  publication: { state: 'pending', occupiedSlots: 0, receipt: null },
  coverage: {
    complete: false,
    totalFiles: 0,
    readFiles: 0,
    totalChangedLines: 0,
    coveredChangedLines: 0,
    rawBytes: 0,
    skipped: [],
    limitations: ['MODEL_PROMPT_CONTEXT_LIMIT'],
  },
  costs: {
    currency: 'TEST',
    priceVersion: 'fake-v1',
    knownCostMicros: '0',
    heldCostMicros: '10',
    calls: [],
    needsReconciliation: true,
    monthlyWarning: false,
    dailyResetsAt: '2099-01-01T00:00:00Z',
  },
  snapshot: {
    identity: {
      tenant: { installationId: 101, repositoryId: 202 },
      prNumber: 1,
      baseSha: 'a'.repeat(40),
      headSha: 'b'.repeat(40),
      pipelineVersion: 'review-v1',
      generation: 0,
      repositoryFullName: 'acme/web',
      prTitle: 'Fixture',
    },
    modelConfigVersion: 'fake-v1',
    priceVersion: 'fake-v1',
    repositoryConfigVersion: 1,
    generationPromptVersion: 'review-generation-v1',
    verificationPromptVersion: 'review-verification-v1',
    memorySetVersion: 'none',
    trustedRulesSha: 'none',
    policy: {
      maxFiles: 64,
      maxChangedLines: 1500,
      maxFileBytes: 131072,
      maxRawBytes: 2097152,
      maxPageBytes: 262144,
      maxStageBytes: 262144,
      maxResultBytes: 65536,
      maxCandidates: 10,
      maxReferences: 5,
      maxSnippetBytes: 2048,
      maxInputTokens: 12000,
      maxOutputTokens: 4096,
      timeoutSeconds: 60,
      retainContent: true,
    },
    provenance: {
      modelMode: 'fake',
      githubMode: 'fake',
      fixtureId: 'fixture',
      contentExpiresAt: '2099-01-01T00:00:00Z',
    },
  },
};
it('distinguishes fake, partial and unknown cost without accuracy claims', () => {
  const view = reviewView(detail);
  expect(view.coverageLabel).toBe('部分覆盖');
  expect(view.costLabel).toBe('费用待对账');
  expect(view.modeLabel).toContain('模拟');
  expect(JSON.stringify(view)).not.toContain('%');
  expect(reviewActive(detail)).toBe(false);
});
it('uses bounded empty-result wording and hides expired content', () => {
  expect(
    reviewView({ ...detail, status: 'Completed', coverage: { ...detail.coverage, complete: true } })
      .emptyLabel,
  ).toBe('本次覆盖内未发现可发布问题');
  expect(reviewView({ ...detail, contentExpired: true }).emptyLabel).toContain('过期');
  expect(reviewActive({ ...detail, status: 'Reviewing', needsReconciliation: false })).toBe(true);
});
