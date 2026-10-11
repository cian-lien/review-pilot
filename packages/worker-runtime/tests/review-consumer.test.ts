import { expect, it } from 'vitest';
import { processReviewDelivery } from '../src/review-consumer';
import { envelope, fakeClient, lease } from '../../review-engine/tests/fixture';
it('does not acknowledge a blocked call until durable block succeeds', async () => {
  const client = fakeClient();
  let confirmed = false;
  client.block = async () => {
    confirmed = true;
  };
  const result = await processReviewDelivery(
    envelope,
    client,
    async () => ({ kind: 'blocked', reasonCode: 'MODEL_USAGE_UNKNOWN' }),
    new AbortController().signal,
  );
  expect(confirmed).toBe(true);
  expect(result).toBe('ack');
  client.block = async () => {
    throw new Error('lost block');
  };
  expect(
    await processReviewDelivery(
      envelope,
      client,
      async () => ({ kind: 'blocked', reasonCode: 'MODEL_USAGE_UNKNOWN' }),
      new AbortController().signal,
    ),
  ).toBe('retry');
});
it('accepted redelivery skips the graph and model', async () => {
  const client = fakeClient();
  client.claim = async () => ({ outcome: 'accepted', lease: null });
  let runs = 0;
  expect(
    await processReviewDelivery(
      envelope,
      client,
      async () => {
        runs++;
        return { kind: 'failed', reasonCode: 'REVIEW_PIPELINE_FAILED' };
      },
      new AbortController().signal,
    ),
  ).toBe('ack');
  expect(runs).toBe(0);
});
it('confirms a lost result response before acknowledgment', async () => {
  const client = fakeClient();
  client.commit = async () => {
    throw new Error('lost response');
  };
  let confirmed = false;
  client.confirm = async () => {
    confirmed = true;
    return {
      outcome: 'already-accepted',
      resultId: lease.taskId + ':review',
      resultHash: 'a'.repeat(64),
    };
  };
  const result = {
    kind: 'ai-review' as const,
    lease,
    resultId: lease.taskId + ':review',
    contextHash: 'a'.repeat(64),
    candidates: [],
    decisions: [],
    coverage: {
      complete: true,
      totalFiles: 0,
      readFiles: 0,
      totalChangedLines: 0,
      coveredChangedLines: 0,
      rawBytes: 0,
      skipped: [],
      limitations: [],
    },
    callIds: [],
  };
  expect(
    await processReviewDelivery(
      envelope,
      client,
      async () => ({ kind: 'completed', result }),
      new AbortController().signal,
    ),
  ).toBe('ack');
  expect(confirmed).toBe(true);
});
