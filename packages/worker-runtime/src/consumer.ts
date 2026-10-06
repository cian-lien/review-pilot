import { validateTask } from '@reviewpilot/contracts';
import type { InternalClient, Lease } from './client';
import { InternalError } from './client';
import { connectionResult } from './result';
import { inTaskTrace } from '@reviewpilot/observability';
export async function processDelivery(
  input: unknown,
  client: InternalClient,
): Promise<'ack' | 'retry' | 'quarantine'> {
  let envelope;
  try {
    envelope = validateTask(input);
  } catch {
    return 'quarantine';
  }
  if (envelope.kind !== 'connection-check') return 'quarantine';
  return inTaskTrace(envelope, async () => {
    let lease: Lease | undefined;
    try {
      const claim = await client.claim(envelope);
      if (claim.outcome !== 'claimed') return 'ack';
      lease = claim.lease;
      if (
        !lease ||
        lease.taskId !== envelope.taskId ||
        lease.runId !== envelope.runId ||
        lease.snapshot.identity.headSha !== envelope.headSha ||
        lease.snapshot.identity.tenant.installationId !== envelope.tenant.installationId ||
        lease.snapshot.identity.tenant.repositoryId !== envelope.tenant.repositoryId ||
        lease.snapshot.modelConfigVersion !== 'none'
      )
        return 'quarantine';
      await client.heartbeat(lease);
      // S1 produces a deterministic connection result, no model, PR execution or GitHub write.
      const result = connectionResult(lease);
      try {
        const committed = await client.commit(result);
        return committed.outcome === 'accepted' || committed.outcome === 'already-accepted'
          ? 'ack'
          : 'retry';
      } catch {
        try {
          const persisted = await client.read(result);
          if (persisted.outcome === 'already-accepted') return 'ack';
        } catch {
          /* Unknown acceptance stays unacknowledged. */
        }
        return 'retry';
      }
    } catch (error) {
      if (error instanceof InternalError && error.status === 409)
        return lease ? 'ack' : 'quarantine';
      if (error instanceof InternalError && error.status === 400) return 'quarantine';
      return 'retry';
    }
  });
}
