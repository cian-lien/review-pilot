import {
  validateReview,
  type ReviewLease,
  type ReviewResult,
  type ReviewTaskEnvelope,
  type components,
  type ReviewSchemaName,
} from '@reviewpilot/contracts';
import type { ReviewInternalClient } from '@reviewpilot/review-engine';
import { readBoundedBytes } from '@reviewpilot/repository';
import { traceHeaders } from '@reviewpilot/observability';
type S = components['schemas'];
export class ReviewAPIError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(code);
  }
}
export class ReviewRestClient implements ReviewInternalClient {
  private readonly base: URL;
  constructor(
    address: string,
    private readonly token: string,
    private readonly workerId: string,
    private readonly signal: AbortSignal = new AbortController().signal,
  ) {
    this.base = new URL(address);
    if (
      !['http:', 'https:'].includes(this.base.protocol) ||
      this.base.username ||
      this.base.password ||
      this.base.search ||
      this.base.hash ||
      this.base.pathname !== '/' ||
      token.length < 32 ||
      !/^[a-zA-Z0-9_-]{1,100}$/.test(workerId)
    )
      throw new Error('INVALID_INTERNAL_CONFIG');
  }
  private path(taskId: string, suffix: string): string {
    if (!/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(taskId))
      throw new Error('INVALID_TASK_ID');
    return '/internal/review-tasks/' + taskId + '/' + suffix;
  }
  private async request<N extends ReviewSchemaName>(
    method: string,
    path: string,
    body: unknown | undefined,
    name: N | null,
    maxBytes = 65536,
  ): Promise<S[N] | undefined> {
    const encoded = body === undefined ? undefined : JSON.stringify(body);
    if (encoded && Buffer.byteLength(encoded) > maxBytes)
      throw new Error('REVIEW_REQUEST_TOO_LARGE');
    const signal = AbortSignal.any([this.signal, AbortSignal.timeout(12000)]);
    const response = await fetch(new URL(path, this.base), {
      method,
      headers: {
        ...traceHeaders(),
        Authorization: 'Bearer ' + this.token,
        ...(encoded ? { 'Content-Type': 'application/json' } : {}),
      },
      body: encoded,
      signal,
      redirect: 'error',
    });
    const bytes = await readBoundedBytes(response, response.ok ? maxBytes : 4096, signal);
    if (!response.ok) {
      let code = 'INTERNAL_API_FAILED';
      try {
        const value = JSON.parse(new TextDecoder().decode(bytes));
        if (typeof value.code === 'string' && /^[A-Z_]{1,64}$/.test(value.code)) code = value.code;
      } catch {
        /* Error bodies never leave the bounded safe-code mapping. */
      }
      throw new ReviewAPIError(response.status, code);
    }
    if (name === null) {
      if (response.status !== 204 && bytes.length) throw new Error('INVALID_INTERNAL_RESPONSE');
      return undefined;
    }
    return validateReview(
      name,
      JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)),
    );
  }
  async claim(envelope: ReviewTaskEnvelope) {
    const body = validateReview('ClaimReviewRequest', { envelope, workerId: this.workerId });
    return (await this.request(
      'POST',
      this.path(envelope.taskId, 'claim'),
      body,
      'ClaimReviewResponse',
    ))!;
  }
  async heartbeat(lease: ReviewLease) {
    await this.request(
      'POST',
      this.path(lease.taskId, 'heartbeat'),
      validateReview('ReviewLease', lease),
      null,
    );
  }
  async context(body: S['ReadReviewContextRequest']) {
    validateReview('ReadReviewContextRequest', body);
    return (await this.request(
      'POST',
      this.path(body.lease.taskId, 'context'),
      body,
      'ReviewContextPage',
      body.lease.snapshot.policy.maxPageBytes,
    ))!;
  }
  async reserve(body: S['ReserveModelCallRequest']) {
    validateReview('ReserveModelCallRequest', body);
    return (await this.request(
      'POST',
      this.path(body.lease.taskId, 'model-calls/reserve'),
      body,
      'ModelCall',
    ))!;
  }
  async dispatch(body: S['DispatchModelCallRequest']) {
    validateReview('DispatchModelCallRequest', body);
    return (await this.request(
      'POST',
      this.path(body.lease.taskId, 'model-calls/' + body.callId + '/dispatch'),
      body,
      'ModelCall',
    ))!;
  }
  async settle(body: S['SettleModelCallRequest']) {
    validateReview('SettleModelCallRequest', body);
    return (await this.request(
      'POST',
      this.path(body.lease.taskId, 'model-calls/' + body.callId + '/settle'),
      body,
      'ModelCall',
    ))!;
  }
  async readStage(body: S['ReadReviewStageRequest']) {
    validateReview('ReadReviewStageRequest', body);
    return (await this.request(
      'POST',
      this.path(body.lease.taskId, 'stages/read'),
      body,
      'ReadReviewStageResponse',
      body.lease.snapshot.policy.maxStageBytes + 65536,
    ))!;
  }
  async saveStage(body: S['SaveReviewStageRequest']) {
    validateReview('SaveReviewStageRequest', body);
    await this.request(
      'POST',
      this.path(body.lease.taskId, 'stages'),
      body,
      'CommitResponse',
      body.lease.snapshot.policy.maxStageBytes + 65536,
    );
  }
  async commit(result: ReviewResult) {
    const body = validateReview('CommitReviewRequest', { result });
    return (await this.request(
      'POST',
      this.path(result.lease.taskId, 'result'),
      body,
      'ReviewCommitResponse',
      result.lease.snapshot.policy.maxResultBytes,
    ))!;
  }
  async confirm(lease: ReviewLease) {
    const query = new URLSearchParams({ runId: lease.runId, inputHash: lease.inputHash });
    return (await this.request(
      'GET',
      this.path(lease.taskId, 'result?' + query),
      undefined,
      'ReviewCommitResponse',
    ))!;
  }
  async block(body: S['ReviewTaskActionRequest']) {
    validateReview('ReviewTaskActionRequest', body);
    await this.request('POST', this.path(body.lease.taskId, 'block'), body, null);
  }
  async fail(body: S['ReviewTaskActionRequest']) {
    validateReview('ReviewTaskActionRequest', body);
    await this.request('POST', this.path(body.lease.taskId, 'fail'), body, null);
  }
}
