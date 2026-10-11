import { describe, expect, it } from 'vitest';
import { validateReviewTask, validateTask } from '../src/validate';
import { validateReview } from '../src/review';

const task = {
  schemaVersion: 2,
  messageId: '16c13b9a-35cc-4b1d-897e-156cfad419b6',
  taskId: '179873d0-c155-45f7-a724-b6f0e8c80976',
  runId: '0f671ed0-38d5-447a-89db-2b0ce65712f6',
  tenant: { installationId: 1, repositoryId: 2 },
  headSha: 'a'.repeat(40),
  kind: 'ai-review',
  notificationGeneration: 0,
};
describe('review notification isolation', () => {
  it('rejects schema extras and money overflow in public cost facts', () => {
    const call = {
      callId: task.taskId,
      reservationId: task.runId,
      purpose: 'generate',
      ordinal: 1,
      requestHash: 'a'.repeat(64),
      state: 'reserved',
      priceVersion: 'test-v1',
      currency: 'TEST',
      reservedCostMicros: '6',
      actualCostMicros: null,
      usage: null,
      revision: 0,
      sendAuthorized: false,
      reasonCode: null,
    };
    expect(validateReview('ModelCall', call).reservedCostMicros).toBe('6');
    expect(() =>
      validateReview('ModelCall', { ...call, reservedCostMicros: '9223372036854775808' }),
    ).toThrow();
    expect(() => validateReview('ModelCall', { ...call, token: 'secret' })).toThrow();
  });
  it('accepts v2 without relaxing the v1 boundary', () => {
    expect(validateReviewTask(task).kind).toBe('ai-review');
    expect(() => validateTask(task)).toThrow();
    const legacy = { ...task, schemaVersion: 1, kind: 'connection-check' };
    expect(validateTask(legacy).kind).toBe('connection-check');
    expect(() => validateReviewTask(legacy)).toThrow();
  });
  it.each([
    { schemaVersion: 1 },
    { schemaVersion: 3 },
    { token: 'secret' },
    { headSha: 'not-a-sha' },
    { notificationGeneration: -1 },
  ])('rejects incompatible or credential-bearing notifications: %j', (change) => {
    expect(() => validateReviewTask({ ...task, ...change })).toThrow();
  });
});
