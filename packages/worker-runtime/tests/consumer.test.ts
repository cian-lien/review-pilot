import { expect, it, vi } from 'vitest';
import type { InternalClient, Lease } from '../src/client';
import { processDelivery } from '../src/consumer';
const envelope = {
  schemaVersion: 1 as const,
  messageId: '00000000-0000-4000-8000-000000000003',
  taskId: '00000000-0000-4000-8000-000000000002',
  runId: '00000000-0000-4000-8000-000000000001',
  tenant: { installationId: 101, repositoryId: 202 },
  headSha: 'b'.repeat(40),
  kind: 'connection-check' as const,
  notificationGeneration: 1,
};
const lease: Lease = {
  taskId: envelope.taskId,
  runId: envelope.runId,
  leaseToken: '00000000-0000-4000-8000-000000000004',
  executionEpoch: 1,
  inputHash: 'a'.repeat(64),
  attempt: 1,
  expiresAt: new Date(Date.now() + 90000).toISOString(),
  snapshot: {
    identity: {
      tenant: envelope.tenant,
      baseSha: 'a'.repeat(40),
      headSha: envelope.headSha,
      prNumber: 1,
      pipelineVersion: 'foundation-v1',
      generation: 0,
    },
    modelConfigVersion: 'none',
    repositoryConfigVersion: '1',
    memorySetVersion: 'none',
    trustedRulesSha: 'none',
  },
};
function client(): InternalClient {
  return {
    claim: vi.fn<InternalClient['claim']>(async () => ({ outcome: 'claimed', lease })),
    heartbeat: vi.fn(async () => {}),
    commit: vi.fn<InternalClient['commit']>(async () => ({ outcome: 'accepted' })),
    read: vi.fn<InternalClient['read']>(async () => ({ outcome: 'already-accepted' })),
    fail: vi.fn(async () => {}),
  };
}
it('does not acknowledge until durable acceptance resolves', async () => {
  const api = client();
  let release: () => void = () => {};
  let acknowledged = false;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  api.commit = vi.fn<InternalClient['commit']>(async () => {
    await gate;
    return { outcome: 'accepted' };
  });
  const result = processDelivery(envelope, api).then((value) => {
    acknowledged = value === 'ack';
    return value;
  });
  await vi.waitFor(() => expect(api.commit).toHaveBeenCalledOnce());
  expect(acknowledged).toBe(false);
  release();
  expect(await result).toBe('ack');
});
it('recovers a lost commit response by reading matching persisted result', async () => {
  const api = client();
  api.commit = vi.fn(async () => {
    throw new Error('response lost');
  });
  expect(await processDelivery(envelope, api)).toBe('ack');
  expect(api.read).toHaveBeenCalledOnce();
});
it('never trusts malformed messages or invokes the API for evolution', async () => {
  const api = client();
  expect(await processDelivery({ ...envelope, secret: 'do not store' }, api)).toBe('quarantine');
  expect(await processDelivery({ ...envelope, kind: 'evolution' }, api)).toBe('quarantine');
  expect(api.claim).not.toHaveBeenCalled();
});
