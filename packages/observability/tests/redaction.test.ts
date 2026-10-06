import { expect, it, vi } from 'vitest';
import { safeLog, safeFields } from '../src/log';
it('only emits bounded correlation fields, never unknown bodies or credentials', () => {
  const output = vi.spyOn(console, 'info').mockImplementation(() => {});
  const input = {
    service: 'worker-review',
    code: 'RESULT_ACCEPTED',
    attempt: 1,
    token: 'TEST-CREDENTIAL-SENTINEL',
    source: 'private source',
    error: new Error('secret error'),
  };
  safeLog(input);
  expect(safeFields(input)).toEqual({
    service: 'worker-review',
    code: 'RESULT_ACCEPTED',
    attempt: 1,
  });
  expect(output).toHaveBeenCalledWith(
    JSON.stringify({ service: 'worker-review', code: 'RESULT_ACCEPTED', attempt: 1 }),
  );
  output.mockRestore();
});
it('does not allow a sensitive payload disguised as a correlation field', () => {
  expect(
    safeFields({
      service: 'worker-review',
      stage: 'Authorization: Bearer SECRET',
      runId: 'full source here',
      traceId: 'not a trace',
      durationMs: Infinity,
    }),
  ).toEqual({ service: 'worker-review' });
});
