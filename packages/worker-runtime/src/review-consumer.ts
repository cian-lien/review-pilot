import type {
  ReviewInternalClient,
  PipelineInput,
  ReviewPipelineOutcome,
} from '@reviewpilot/review-engine';
import { validateReviewTask, type ReviewLease } from '@reviewpilot/contracts';
import {
  ModelGateway,
  FakeProvider,
  OpenAICompatibleProvider,
  readModelConfiguration,
} from '@reviewpilot/model-gateway';
import { inTaskTrace } from '@reviewpilot/observability';
import { leaseHeartbeat } from './lease-heartbeat';
export function workerModelGateway(client: ReviewInternalClient): ModelGateway {
  const configuration = readModelConfiguration(process.env.REVIEW_MODEL_CONFIG_FILE ?? '');
  const provider =
    process.env.REVIEW_MODE === 'fake'
      ? FakeProvider.fromFile(configuration, process.env.REVIEW_FAKE_SCRIPT_FILE ?? '')
      : new OpenAICompatibleProvider(configuration, process.env.MODEL_API_KEY ?? '');
  return new ModelGateway(provider, client);
}
export async function processReviewDelivery(
  input: unknown,
  client: ReviewInternalClient,
  run: (input: Omit<PipelineInput, 'gateway'>) => Promise<ReviewPipelineOutcome>,
  signal: AbortSignal,
): Promise<'ack' | 'retry' | 'quarantine'> {
  let envelope;
  try {
    envelope = validateReviewTask(input);
  } catch {
    return 'quarantine';
  }
  return inTaskTrace(envelope, async () => {
    let lease: ReviewLease | undefined;
    let heartbeat: ReturnType<typeof leaseHeartbeat> | undefined;
    try {
      const claimed = await client.claim(envelope);
      if (claimed.outcome !== 'claimed') return 'ack';
      if (
        !claimed.lease ||
        claimed.lease.taskId !== envelope.taskId ||
        claimed.lease.runId !== envelope.runId ||
        claimed.lease.headSha !== envelope.headSha ||
        claimed.lease.tenant.installationId !== envelope.tenant.installationId ||
        claimed.lease.tenant.repositoryId !== envelope.tenant.repositoryId
      )
        return 'quarantine';
      lease = claimed.lease;
      heartbeat = leaseHeartbeat(client, lease, signal);
      await client.heartbeat(lease);
      // Tests supply the graph; production creates a provider only after a live claim.
      const outcome = await run({
        lease,
        client,
        signal: heartbeat.signal,
      });
      if (heartbeat.signal.aborted) return 'retry';
      if (outcome.kind === 'blocked') {
        await client.block({ lease, reasonCode: outcome.reasonCode });
        return 'ack';
      }
      if (outcome.kind === 'failed') {
        await client.fail({ lease, reasonCode: outcome.reasonCode });
        return 'ack';
      }
      try {
        await client.commit(outcome.result);
        return 'ack';
      } catch {
        const saved = await client.confirm(lease);
        return saved.outcome === 'already-accepted' && saved.resultId === outcome.result.resultId
          ? 'ack'
          : 'retry';
      }
    } catch {
      return 'retry';
    } finally {
      heartbeat?.stop();
    }
  });
}
