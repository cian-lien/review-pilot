import { describe, expect, it } from 'vitest';
import { validateTask } from '../src/validate';

const task = {
  schemaVersion: 1,
  messageId: '16c13b9a-35cc-4b1d-897e-156cfad419b6',
  taskId: '179873d0-c155-45f7-a724-b6f0e8c80976',
  runId: '0f671ed0-38d5-447a-89db-2b0ce65712f6',
  tenant: { installationId: 1, repositoryId: 2 },
  headSha: 'a'.repeat(40),
  kind: 'connection-check',
  notificationGeneration: 0,
};
describe('task notification boundary', () => {
  it('preserves legacy SHA-256 identifiers', () =>
    expect(validateTask({ ...task, headSha: 'a'.repeat(64) }).headSha).toHaveLength(64));
  it('accepts a minimal notification', () => expect(validateTask(task)).toEqual(task));
  it('rejects an unknown contract version', () =>
    expect(() => validateTask({ ...task, schemaVersion: 2 })).toThrow());
  it('rejects a negative tenant identifier', () =>
    expect(() =>
      validateTask({ ...task, tenant: { installationId: -1, repositoryId: 2 } }),
    ).toThrow());
  it('rejects credentials smuggled as extra fields', () =>
    expect(() => validateTask({ ...task, token: 'secret' })).toThrow());
});
