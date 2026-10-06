import { createHash } from 'node:crypto';
import type { Lease, Result } from './client';
export function connectionResult(lease: Lease): Result {
  return {
    taskId: lease.taskId,
    runId: lease.runId,
    resultId: lease.taskId + ':connection',
    kind: 'connection-check',
    leaseToken: lease.leaseToken,
    executionEpoch: lease.executionEpoch,
    inputHash: lease.inputHash,
    resultHash: createHash('sha256')
      .update(lease.taskId + '\n' + lease.runId + '\n' + lease.inputHash)
      .digest('hex'),
  };
}
