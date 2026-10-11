// This declarative source projects into the single OpenAPI document and its runtime schema.
const text = (maxLength = 2048) => ({ type: 'string', maxLength });
const integer = (maximum = 9007199254740991, minimum = 0) => ({
  type: 'integer',
  minimum,
  maximum,
});
const bool = { type: 'boolean' };
const ref = (name) => ({ $ref: '#/components/schemas/' + name });
const values = (...items) => ({ type: 'string', enum: items });
const array = (items, maxItems = 64) => ({ type: 'array', items, maxItems });
const object = (properties, optional = []) => ({
  type: 'object',
  additionalProperties: false,
  required: Object.keys(properties).filter((key) => !optional.includes(key)),
  properties,
});
const uuid = {
  type: 'string',
  pattern: '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
};
const hash = { type: 'string', pattern: '^[0-9a-f]{64}$' };
const sha = { type: 'string', pattern: '^[0-9a-f]{40,64}$' };
const money = { type: 'string', pattern: '^(0|[1-9][0-9]{0,18})$', maxLength: 19 };
const timestamp = { type: 'string', format: 'date-time' };
const nullable = (schema) =>
  schema.$ref ? { allOf: [schema], nullable: true } : { ...schema, nullable: true };
const stage = values('context', 'candidates', 'verification');
const purpose = values('generate', 'verify');
const lease = ref('ReviewLease');
const category = values(
  'concurrency',
  'validation',
  'authorization',
  'lifecycle',
  'error-handling',
  'data-integrity',
  'other',
);
const mode = values('fake', 'real');
const coverage = ref('ReviewCoverage');
const identity = object({
  tenant: ref('TenantRef'),
  prNumber: integer(2147483647, 1),
  baseSha: sha,
  headSha: sha,
  pipelineVersion: values('review-v1'),
  generation: integer(),
  repositoryFullName: text(256),
  prTitle: text(512),
});
export const reviewComponents = {
  RunState: values(
    'Queued',
    'Indexing',
    'Reviewing',
    'Verifying',
    'Publishing',
    'Completed',
    'Partial',
    'Stale',
    'Cancelled',
    'Failed',
  ),
  ReviewPolicy: object({
    maxChangedLines: integer(1500, 1),
    maxFiles: integer(64, 1),
    maxFileBytes: integer(131072, 1),
    maxRawBytes: integer(2097152, 1),
    maxPageBytes: integer(262144, 1),
    maxStageBytes: integer(262144, 1),
    maxResultBytes: integer(65536, 1),
    maxCandidates: integer(10, 1),
    maxReferences: integer(5, 1),
    maxSnippetBytes: integer(2048, 1),
    maxInputTokens: integer(12000, 1),
    maxOutputTokens: integer(4096, 1),
    timeoutSeconds: integer(60, 1),
    retainContent: bool,
  }),
  ReviewIdentity: identity,
  ReviewProvenance: object({
    modelMode: mode,
    githubMode: mode,
    fixtureId: text(128),
    contentExpiresAt: timestamp,
  }),
  ReviewSnapshot: object({
    identity: ref('ReviewIdentity'),
    modelConfigVersion: text(128),
    priceVersion: text(128),
    repositoryConfigVersion: integer(),
    generationPromptVersion: values('review-generation-v1'),
    verificationPromptVersion: values('review-verification-v1'),
    memorySetVersion: values('none'),
    trustedRulesSha: values('none'),
    policy: ref('ReviewPolicy'),
    provenance: ref('ReviewProvenance'),
  }),
  ReviewLease: object({
    taskId: uuid,
    runId: uuid,
    tenant: ref('TenantRef'),
    headSha: sha,
    inputHash: hash,
    leaseToken: uuid,
    leaseExpiresAt: timestamp,
    executionEpoch: integer(),
    attempt: integer(5, 1),
    snapshot: ref('ReviewSnapshot'),
  }),
  ReviewSkippedFile: object({ path: text(1024), reasonCode: text(64), changedLines: integer() }),
  ReviewCoverage: object({
    complete: bool,
    totalFiles: integer(),
    readFiles: integer(),
    totalChangedLines: integer(),
    coveredChangedLines: integer(),
    rawBytes: integer(),
    skipped: array(ref('ReviewSkippedFile')),
    limitations: array(text(256), 16),
  }),
  ReviewFileContent: object({
    state: values('available', 'deleted', 'binary', 'missing', 'denied', 'too-large'),
    sha,
    contentHash: nullable(hash),
    text: nullable(text(131072)),
    bytes: integer(131072),
  }),
  ReviewChangedFile: object({
    path: text(1024),
    previousPath: nullable(text(1024)),
    status: values('added', 'modified', 'removed', 'renamed'),
    additions: integer(),
    deletions: integer(),
    patch: nullable(text(131072)),
    base: ref('ReviewFileContent'),
    head: ref('ReviewFileContent'),
  }),
  EvidenceRef: object({
    evidenceId: uuid,
    path: text(1024),
    sha,
    startLine: integer(2147483647, 1),
    endLine: integer(2147483647, 1),
    contentHash: hash,
  }),
  CodeEvidence: object({
    ref: ref('EvidenceRef'),
    tenant: ref('TenantRef'),
    snippet: text(2048),
    readState: values('available'),
    redacted: bool,
  }),
  ReviewContextPage: object({
    manifestId: uuid,
    contextHash: nullable(hash),
    cursor: nullable(text(256)),
    nextCursor: nullable(text(256)),
    complete: bool,
    files: array(ref('ReviewChangedFile')),
    evidence: array(ref('CodeEvidence'), 1024),
    coverage,
  }),
  RootCauseStatement: object({ trigger: text(512), consequence: text(512) }),
  ReviewCandidate: object({
    candidateId: text(64),
    title: text(240),
    severity: values('high', 'medium', 'low'),
    category,
    failureScenario: text(2048),
    impact: text(2048),
    introducedByPR: bool,
    evidence: array(ref('EvidenceRef'), 5),
    suggestedVerification: text(2048),
    rootCauseStatement: ref('RootCauseStatement'),
  }),
  ReviewCandidates: object({ candidates: array(ref('ReviewCandidate'), 10) }),
  VerificationDecision: object({
    candidateId: text(64),
    verdict: values('verified', 'abstain'),
    reasonCode: text(64),
    evidence: array(ref('EvidenceRef'), 5),
  }),
  ReviewVerification: object({ decisions: array(ref('VerificationDecision'), 10) }),
  ModelUsage: object({ inputTokens: integer(1000000), outputTokens: integer(1000000) }),
  ModelCall: object({
    callId: uuid,
    reservationId: uuid,
    purpose,
    ordinal: integer(2, 1),
    requestHash: hash,
    state: values('reserved', 'dispatching', 'settled', 'released', 'uncertain'),
    priceVersion: text(128),
    currency: text(4),
    reservedCostMicros: money,
    actualCostMicros: nullable(money),
    usage: nullable(ref('ModelUsage')),
    revision: integer(),
    sendAuthorized: bool,
    reasonCode: nullable(text(64)),
  }),
  ReviewStagePayload: object({
    context: nullable(ref('ReviewContextPage')),
    candidates: nullable(ref('ReviewCandidates')),
    verification: nullable(ref('ReviewVerification')),
  }),
  ReviewStageRecord: object({
    stage,
    stageInputHash: hash,
    outputHash: hash,
    callId: nullable(uuid),
    payload: ref('ReviewStagePayload'),
    expiresAt: timestamp,
  }),
  ReviewResult: object({
    kind: values('ai-review'),
    lease,
    resultId: text(64),
    contextHash: hash,
    candidates: array(ref('ReviewCandidate'), 10),
    decisions: array(ref('VerificationDecision'), 10),
    coverage,
    callIds: array(uuid, 4),
  }),
  ReviewFinding: object({
    findingId: uuid,
    fingerprint: hash,
    candidate: ref('ReviewCandidate'),
    evidence: array(ref('CodeEvidence'), 5),
    publicationStatus: values('pending', 'published', 'not-published', 'uncertain'),
    reasonCode: nullable(text(64)),
    contentExpired: bool,
  }),
  ReviewFilteredCandidate: object({
    candidateId: text(64),
    title: text(240),
    reasonCode: text(64),
    evidence: array(ref('EvidenceRef'), 5),
  }),
  ReviewCostSummary: object({
    currency: text(4),
    priceVersion: text(128),
    calls: array(ref('ModelCall'), 4),
    knownCostMicros: money,
    heldCostMicros: money,
    needsReconciliation: bool,
    monthlyWarning: bool,
    dailyResetsAt: timestamp,
  }),
  ReviewPublicationFinding: object({
    findingId: uuid,
    fingerprint: hash,
    path: text(1024),
    line: nullable(integer(2147483647, 1)),
    body: text(8192),
  }),
  ReviewPublicationPlan: object({
    runId: uuid,
    tenant: ref('TenantRef'),
    prNumber: integer(2147483647, 1),
    headSha: sha,
    planHash: hash,
    marker: text(256),
    findings: array(ref('ReviewPublicationFinding'), 3),
    mode,
  }),
  ReviewPublicationReceipt: object({
    reviewId: nullable(text(64)),
    commentIds: array(text(64), 3),
    checkId: nullable(text(64)),
    headSha: sha,
    authorAppId: integer(),
    marker: text(256),
    state: values('confirmed', 'uncertain', 'not-written'),
    mode,
  }),
  ReviewPublicationSummary: object({
    state: values('pending', 'published', 'cancelled', 'failed', 'uncertain'),
    occupiedSlots: integer(3),
    receipt: nullable(ref('ReviewPublicationReceipt')),
  }),
  ReviewRunSummary: object({
    runId: uuid,
    headSha: sha,
    status: ref('RunState'),
    needsReconciliation: bool,
    reasonCode: nullable(text(64)),
    createdAt: timestamp,
    mode,
  }),
  ReviewRunDetail: object({
    runId: uuid,
    status: ref('RunState'),
    needsReconciliation: bool,
    reasonCode: nullable(text(64)),
    snapshot: ref('ReviewSnapshot'),
    findings: array(ref('ReviewFinding'), 10),
    filtered: array(ref('ReviewFilteredCandidate'), 10),
    coverage,
    costs: ref('ReviewCostSummary'),
    publication: ref('ReviewPublicationSummary'),
    contentExpired: bool,
  }),
  ReviewPullSummary: object({
    prNumber: integer(2147483647, 1),
    title: text(512),
    headSha: sha,
    latestRun: ref('ReviewRunSummary'),
  }),
  ReviewPullList: object({
    items: array(ref('ReviewPullSummary'), 20),
    nextCursor: nullable(text(512)),
  }),
  ReviewRunList: object({
    items: array(ref('ReviewRunSummary'), 20),
    nextCursor: nullable(text(512)),
  }),
  ClaimReviewRequest: object({ envelope: ref('ReviewTaskEnvelope'), workerId: text(128) }),
  ClaimReviewResponse: object({
    outcome: {
      ...values('claimed', 'busy', 'accepted', 'cancelled', 'terminal', 'blocked'),
      'x-enum-varnames': [
        'ReviewClaimed',
        'ReviewBusy',
        'ReviewFinished',
        'ReviewCancelled',
        'ReviewTerminal',
        'ReviewBlocked',
      ],
    },
    lease: nullable(lease),
  }),
  ReadReviewContextRequest: object({ lease, cursor: nullable(text(256)) }),
  ReserveModelCallRequest: object({
    modelConfigHash: hash,
    lease,
    purpose,
    ordinal: integer(2, 1),
    requestHash: hash,
    inputTokenLimit: integer(12000, 1),
    outputTokenLimit: integer(4096, 1),
  }),
  DispatchModelCallRequest: object({ lease, callId: uuid, expectedRevision: integer() }),
  SettleModelCallRequest: object({
    lease,
    callId: uuid,
    expectedRevision: integer(),
    outcome: values('succeeded', 'failed', 'not-sent', 'uncertain'),
    usage: nullable(ref('ModelUsage')),
    safeResponseHash: nullable(hash),
    reasonCode: nullable(text(64)),
  }),
  ReadReviewStageRequest: object({ lease, stage, stageInputHash: hash }),
  ReadReviewStageResponse: object({ found: bool, record: nullable(ref('ReviewStageRecord')) }),
  SaveReviewStageRequest: object({
    lease,
    stage,
    stageInputHash: hash,
    callId: nullable(uuid),
    payload: ref('ReviewStagePayload'),
  }),
  CommitReviewRequest: object({ result: ref('ReviewResult') }),
  ReviewCommitResponse: object({
    outcome: {
      ...values('accepted', 'already-accepted'),
      'x-enum-varnames': ['ReviewAccepted', 'ReviewAlreadyAccepted'],
    },
    resultId: text(64),
    resultHash: hash,
  }),
  ReviewTaskActionRequest: object({ lease, reasonCode: text(64) }),
};

const response = (name) => ({
  description: 'Success',
  content: { 'application/json': { schema: ref(name) } },
});
const parameter = (name, schema, where = 'path') => ({
  name,
  in: where,
  required: where === 'path',
  schema,
});
export const reviewPaths = {};
for (const [suffix, request, result] of [
  ['claim', 'ClaimReviewRequest', 'ClaimReviewResponse'],
  ['heartbeat', 'ReviewLease', null],
  ['context', 'ReadReviewContextRequest', 'ReviewContextPage'],
  ['model-calls/reserve', 'ReserveModelCallRequest', 'ModelCall'],
  ['model-calls/{callId}/dispatch', 'DispatchModelCallRequest', 'ModelCall'],
  ['model-calls/{callId}/settle', 'SettleModelCallRequest', 'ModelCall'],
  ['stages/read', 'ReadReviewStageRequest', 'ReadReviewStageResponse'],
  ['stages', 'SaveReviewStageRequest', 'CommitResponse'],
  ['result', 'CommitReviewRequest', 'ReviewCommitResponse'],
  ['block', 'ReviewTaskActionRequest', null],
  ['fail', 'ReviewTaskActionRequest', null],
]) {
  reviewPaths['/internal/review-tasks/{taskId}/' + suffix] = {
    post: {
      operationId: 'review' + request + suffix.replace(/[^a-zA-Z]/g, ''),
      security: [{ WorkerBearer: [] }],
      parameters: [
        parameter('taskId', uuid),
        ...(suffix.includes('{callId}') ? [parameter('callId', uuid)] : []),
      ],
      requestBody: { required: true, content: { 'application/json': { schema: ref(request) } } },
      responses: {
        [result ? '200' : '204']: result ? response(result) : { description: 'Success' },
        409: response('APIError'),
        403: response('APIError'),
      },
    },
  };
}
reviewPaths['/internal/review-tasks/{taskId}/result'].get = {
  operationId: 'confirmReviewResult',
  security: [{ WorkerBearer: [] }],
  parameters: [
    parameter('taskId', uuid),
    parameter('runId', uuid, 'query'),
    parameter('inputHash', hash, 'query'),
  ],
  responses: { 200: response('ReviewCommitResponse'), 404: response('APIError') },
};
for (const [suffix, result] of [
  ['pulls', 'ReviewPullList'],
  ['pulls/{number}/runs', 'ReviewRunList'],
  ['pulls/{number}/runs/{runId}', 'ReviewRunDetail'],
]) {
  reviewPaths['/api/repositories/{installationId}/{repositoryId}/' + suffix] = {
    get: {
      operationId: 'get' + result,
      parameters: [
        parameter('installationId', integer(9007199254740991, 1)),
        parameter('repositoryId', integer(9007199254740991, 1)),
        ...(suffix.includes('{number}') ? [parameter('number', integer(2147483647, 1))] : []),
        ...(suffix.includes('{runId}')
          ? [parameter('runId', uuid)]
          : [parameter('cursor', text(512), 'query')]),
      ],
      responses: { 200: response(result), 403: response('APIError') },
    },
  };
}
