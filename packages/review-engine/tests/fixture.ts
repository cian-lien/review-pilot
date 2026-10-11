import type {
  ReviewCandidate,
  ReviewContextPage,
  ReviewTaskEnvelope,
} from '@reviewpilot/contracts';
import type { ReviewInternalClient } from '../src/types';
export { lease } from '../../model-gateway/tests/fixture';
import { lease, call } from '../../model-gateway/tests/fixture';
export const ref = {
  evidenceId: lease.runId,
  path: 'src/pay.ts',
  sha: lease.headSha,
  startLine: 2,
  endLine: 2,
  contentHash: 'c'.repeat(64),
};
export const candidate: ReviewCandidate = {
  candidateId: 'candidate-1',
  title: 'Retry can charge twice',
  severity: 'high',
  category: 'data-integrity',
  failureScenario: 'Lost response retries with a new key.',
  impact: 'Duplicate charge.',
  introducedByPR: true,
  evidence: [ref],
  suggestedVerification: 'Retry after a lost response.',
  rootCauseStatement: {
    trigger: 'retry regenerates the payment key',
    consequence: 'provider captures the same order twice',
  },
};
export const contextPage: ReviewContextPage = {
  manifestId: lease.leaseToken,
  contextHash: 'd'.repeat(64),
  cursor: null,
  nextCursor: null,
  complete: true,
  files: [
    {
      path: ref.path,
      previousPath: null,
      status: 'modified',
      additions: 1,
      deletions: 1,
      patch:
        '@@ -1,3 +1,3 @@\n export function charge(key: string) {\n-  return capture(key);\n+  return capture(crypto.randomUUID());\n }',
      base: {
        state: 'available',
        sha: lease.snapshot.identity.baseSha,
        contentHash: 'e'.repeat(64),
        text: 'export function charge(key: string) {\n  return capture(key);\n}\n',
        bytes: 73,
      },
      head: {
        state: 'available',
        sha: lease.headSha,
        contentHash: ref.contentHash,
        text: 'export function charge(key: string) {\n  return capture(crypto.randomUUID());\n}\n',
        bytes: 93,
      },
    },
  ],
  evidence: [
    {
      ref,
      tenant: lease.tenant,
      snippet: '  return capture(crypto.randomUUID());',
      readState: 'available',
      redacted: false,
    },
  ],
  coverage: {
    complete: true,
    totalFiles: 1,
    readFiles: 1,
    totalChangedLines: 2,
    coveredChangedLines: 2,
    rawBytes: 166,
    skipped: [],
    limitations: [],
  },
};
export const envelope: ReviewTaskEnvelope = {
  schemaVersion: 2,
  messageId: lease.leaseToken,
  taskId: lease.taskId,
  runId: lease.runId,
  tenant: lease.tenant,
  headSha: lease.headSha,
  kind: 'ai-review',
  notificationGeneration: 1,
};
export function fakeClient(page: ReviewContextPage = contextPage): ReviewInternalClient {
  return {
    async claim() {
      return { outcome: 'claimed', lease };
    },
    async heartbeat() {},
    async context() {
      return structuredClone(page);
    },
    async readStage() {
      return { found: false, record: null };
    },
    async saveStage() {},
    async commit() {
      return {
        outcome: 'accepted',
        resultId: lease.taskId + ':review',
        resultHash: 'f'.repeat(64),
      };
    },
    async confirm() {
      return {
        outcome: 'already-accepted',
        resultId: lease.taskId + ':review',
        resultHash: 'f'.repeat(64),
      };
    },
    async block() {},
    async fail() {},
    async reserve() {
      return call;
    },
    async dispatch() {
      return { ...call, sendAuthorized: true, state: 'dispatching' };
    },
    async settle() {
      return { ...call, state: 'settled' };
    },
  };
}
