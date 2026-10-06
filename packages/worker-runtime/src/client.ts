import type { components, TaskEnvelope } from '@reviewpilot/contracts';
import { z } from 'zod';
export type Lease = components['schemas']['TaskLease'];
export type Result = components['schemas']['ConnectionResult'];
export interface InternalClient {
  claim(envelope: TaskEnvelope): Promise<components['schemas']['ClaimResponse']>;
  heartbeat(lease: Lease): Promise<void>;
  commit(result: Result): Promise<components['schemas']['CommitResponse']>;
  read(result: Result): Promise<components['schemas']['CommitResponse']>;
  fail(lease: Lease): Promise<void>;
}
const identity = z.object({
  tenant: z.object({
    installationId: z.number().int().positive().max(Number.MAX_SAFE_INTEGER),
    repositoryId: z.number().int().positive().max(Number.MAX_SAFE_INTEGER),
  }),
  baseSha: z.string().regex(/^[a-f0-9]{40,64}$/),
  headSha: z.string().regex(/^[a-f0-9]{40,64}$/),
  prNumber: z.number().int().positive(),
  pipelineVersion: z.string(),
  generation: z.number().int().nonnegative(),
});
const leaseSchema = z.object({
  taskId: z.uuid(),
  runId: z.uuid(),
  leaseToken: z.uuid(),
  attempt: z.number().int().positive(),
  executionEpoch: z.number().int().nonnegative(),
  expiresAt: z.iso.datetime({ offset: true }),
  inputHash: z.string().regex(/^[a-f0-9]{64}$/),
  snapshot: z.object({
    identity,
    modelConfigVersion: z.string(),
    repositoryConfigVersion: z.string(),
    memorySetVersion: z.string(),
    trustedRulesSha: z.string(),
  }),
});
const claimSchema = z.object({
  outcome: z.enum([
    'claimed',
    'busy',
    'finished',
    'cancelled',
    'superseded',
    'not-ready',
    'exhausted',
  ]),
  lease: leaseSchema.optional(),
});
const commitSchema = z.object({ outcome: z.enum(['accepted', 'already-accepted']) });
export class InternalError extends Error {
  constructor(readonly status: number) {
    super('INTERNAL_API_FAILED');
  }
}
export class RestClient implements InternalClient {
  private readonly base: URL;
  constructor(
    address: string,
    private readonly token: string,
    private readonly workerId: string,
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
  private async request(method: string, path: string, body?: unknown): Promise<unknown> {
    const response = await fetch(new URL(path, this.base), {
      method,
      headers: {
        Authorization: 'Bearer ' + this.token,
        ...(body ? { 'Content-Type': 'application/json' } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(8000),
      redirect: 'error',
    });
    if (!response.ok) throw new InternalError(response.status);
    if (response.status === 204) return undefined;
    // Bound even error-free internal responses; snapshots never carry source or credentials.
    const text = await response.text();
    if (text.length > 65536) throw new Error('INVALID_INTERNAL_RESPONSE');
    return JSON.parse(text);
  }
  async claim(envelope: TaskEnvelope) {
    return claimSchema.parse(
      await this.request('POST', '/internal/tasks/' + envelope.taskId + '/claim', {
        envelope,
        workerId: this.workerId,
      }),
    );
  }
  async heartbeat(lease: Lease) {
    await this.request('POST', '/internal/tasks/' + lease.taskId + '/heartbeat', lease);
  }
  async commit(result: Result) {
    return commitSchema.parse(
      await this.request('POST', '/internal/tasks/' + result.taskId + '/connection-result', result),
    );
  }
  async read(result: Result) {
    const query = new URLSearchParams({
      runId: result.runId,
      inputHash: result.inputHash,
      resultHash: result.resultHash,
    });
    return commitSchema.parse(
      await this.request('GET', '/internal/tasks/' + result.taskId + '/result?' + query),
    );
  }
  async fail(lease: Lease) {
    await this.request('POST', '/internal/tasks/' + lease.taskId + '/fail', lease);
  }
}
