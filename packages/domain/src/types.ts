export interface TenantRef {
  installationId: number;
  repositoryId: number;
}
export interface RepoAccess {
  tenant: TenantRef;
  public: boolean;
  installationActive: boolean;
  maintainer: boolean;
}
export interface ReviewIdentity {
  tenant: TenantRef;
  prNumber: number;
  baseSha: string;
  headSha: string;
  pipelineVersion: string;
  generation: number;
}
export interface RunSnapshot {
  identity: ReviewIdentity;
  modelConfigVersion: string;
  repositoryConfigVersion: string;
  memorySetVersion: string;
  trustedRulesSha: string;
}
export type RunStatus =
  | 'Queued'
  | 'Indexing'
  | 'Reviewing'
  | 'Verifying'
  | 'Publishing'
  | 'Completed'
  | 'Partial'
  | 'Stale'
  | 'Cancelled'
  | 'Failed';
export interface CodeRef {
  tenant: TenantRef;
  path: string;
  commitSha: string;
  startLine: number;
  endLine: number;
  contentHash: string;
}
export type FileReadResult =
  | { kind: 'success'; content: string; contentHash: string }
  | { kind: 'missing' | 'denied' | 'too-large'; reason: string };
export interface RepoSnapshot {
  tenant: TenantRef;
  baseSha: string;
  headSha: string;
  files: Array<{ path: string; contentHash: string | null; result: FileReadResult }>;
}
export interface Coverage {
  reviewedFiles: string[];
  skippedFiles: Array<{ path: string; reason: string }>;
  partial: boolean;
}
export interface ContextBundle {
  tenant: TenantRef;
  snapshot: RunSnapshot;
  changeUnits: Array<{ path: string; summary: string }>;
  codeRefs: CodeRef[];
  memoryVersions: Array<{ memoryId: string; version: number }>;
  coverage: Coverage;
}
export type Severity = 'critical' | 'high' | 'medium' | 'low';
export interface Candidate {
  id: string;
  rootCauseFingerprint: string;
  location: CodeRef;
  failureScenario: string;
  impact: string;
  severity: Severity;
  evidence: CodeRef[];
}
export interface VerificationResult {
  candidateId: string;
  verdict: 'verified' | 'rejected' | 'abstain';
  reasonCodes: string[];
  verifiedEvidence: CodeRef[];
}
export interface PublicationPlan {
  runId: string;
  snapshot: RunSnapshot;
  findings: Candidate[];
  summary: string;
  coverage: Coverage;
  remainingCommentSlots: number;
}
export type ScopeIntent = 'finding' | 'file' | 'directory' | 'pattern' | 'repository';
export type FeedbackKind =
  | 'useful'
  | 'false-positive'
  | 'dismiss'
  | 'resolved'
  | 'adoption-inferred'
  | 'long-term-instruction';
export interface FeedbackInput {
  tenant: TenantRef;
  actorId: string;
  findingId: string;
  kind: FeedbackKind;
  reason: string;
  scopeIntent: ScopeIntent;
  idempotencyKey: string;
}
export interface MemoryVersion {
  tenant: TenantRef;
  memoryId: string;
  version: number;
  type: 'rule' | 'preference' | 'incident';
  statement: string;
  scope: { intent: ScopeIntent; value: string };
  riskLevel: Severity;
  origin: 'manual-locked' | 'trusted-rule' | 'human-correction' | 'learned';
  locked: boolean;
  status: 'proposed' | 'replay' | 'shadow' | 'active' | 'quarantined' | 'retired';
  evidenceIds: string[];
  createdAt: string;
  lastUsedAt: string | null;
}
export interface EvolutionMetrics {
  independentSupport: number;
  explicitInstruction: boolean;
  score: number;
  shadowHits: number;
  labeledCases: number;
  counterexamples: number;
  precision: number;
  criticalRegressions: number;
}
export interface ReplaySpec {
  tenant: TenantRef;
  datasetVersion: string;
  cutoffAt: string;
  baselineSnapshot: RunSnapshot;
  candidateSnapshot: RunSnapshot;
  publish: false;
}
export interface EvaluationReport {
  version: string;
  labeledFindingCount: number;
  TP: number;
  FP: number;
  FN: number;
  coverage: number;
  criticalMisses: number;
  cost: { micros: number; currency: string };
  latency: { p50Ms: number; p90Ms: number };
  regressions: string[];
}
export type ModelPurpose = 'generate' | 'verify' | 'propose' | 'replay' | 'judge' | 'embedding';
export interface BudgetReservation {
  tenant: TenantRef;
  reservationId: string;
  purpose: ModelPurpose;
  maxCostMicros: number;
  currency: string;
  expiresAt: string;
}
export interface StageCheckpoint {
  runId: string;
  stage: string;
  artifactRef: string | null;
  inputHash: string;
  status: 'pending' | 'completed' | 'failed';
}
