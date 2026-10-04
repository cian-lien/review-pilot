import type {
  Candidate,
  EvaluationReport,
  EvolutionMetrics,
  FeedbackInput,
  MemoryVersion,
  RepoSnapshot,
  ReviewIdentity,
  RunSnapshot,
} from '@reviewpilot/domain';
const tenant = () => ({ installationId: 101, repositoryId: 202 });
const baseSha = 'a'.repeat(40),
  headSha = 'b'.repeat(40);
export function reviewIdentity(overrides: Partial<ReviewIdentity> = {}): ReviewIdentity {
  return {
    tenant: tenant(),
    prNumber: 7,
    baseSha,
    headSha,
    pipelineVersion: 'foundation-v1',
    generation: 0,
    ...overrides,
  };
}
export function runSnapshot(overrides: Partial<RunSnapshot> = {}): RunSnapshot {
  return {
    identity: reviewIdentity(),
    modelConfigVersion: 'none',
    repositoryConfigVersion: '1',
    memorySetVersion: 'empty',
    trustedRulesSha: '',
    ...overrides,
  };
}
export function repoSnapshot(overrides: Partial<RepoSnapshot> = {}): RepoSnapshot {
  return { tenant: tenant(), baseSha, headSha, files: [], ...overrides };
}
export function candidate(overrides: Partial<Candidate> = {}): Candidate {
  const ref = {
    tenant: tenant(),
    path: 'src/example.ts',
    commitSha: headSha,
    startLine: 1,
    endLine: 2,
    contentHash: 'c'.repeat(64),
  };
  return {
    id: 'fixture-finding',
    rootCauseFingerprint: 'fixture-root-cause',
    location: ref,
    failureScenario: 'Retry uses an invalid token.',
    impact: 'The request fails.',
    severity: 'medium',
    evidence: [ref],
    ...overrides,
  };
}
export function feedbackInput(overrides: Partial<FeedbackInput> = {}): FeedbackInput {
  return {
    tenant: tenant(),
    actorId: 'fixture-actor',
    findingId: 'fixture-finding',
    kind: 'useful',
    reason: 'Confirmed by a regression test.',
    scopeIntent: 'finding',
    idempotencyKey: 'fixture-feedback',
    ...overrides,
  };
}
export function memoryVersion(overrides: Partial<MemoryVersion> = {}): MemoryVersion {
  return {
    tenant: tenant(),
    memoryId: 'fixture-memory',
    version: 1,
    type: 'rule',
    statement: 'Preserve the idempotency key on retries.',
    scope: { intent: 'directory', value: 'src/payments/' },
    riskLevel: 'high',
    origin: 'learned',
    locked: false,
    status: 'shadow',
    evidenceIds: [],
    createdAt: '2026-10-04T00:00:00Z',
    lastUsedAt: null,
    ...overrides,
  };
}
export function evolutionMetrics(overrides: Partial<EvolutionMetrics> = {}): EvolutionMetrics {
  return {
    independentSupport: 0,
    explicitInstruction: false,
    score: 0,
    shadowHits: 0,
    labeledCases: 0,
    counterexamples: 0,
    precision: 0,
    criticalRegressions: 0,
    ...overrides,
  };
}
export function evaluationReport(overrides: Partial<EvaluationReport> = {}): EvaluationReport {
  return {
    version: 'fixture-report',
    labeledFindingCount: 0,
    TP: 0,
    FP: 0,
    FN: 0,
    coverage: 0,
    criticalMisses: 0,
    cost: { micros: 0, currency: 'USD' },
    latency: { p50Ms: 0, p90Ms: 0 },
    regressions: [],
    ...overrides,
  };
}
