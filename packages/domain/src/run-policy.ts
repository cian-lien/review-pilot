import type { RunStatus } from './types';
const transitions: Record<RunStatus, readonly RunStatus[]> = {
  // Direct completion is only used by the explicitly labelled S1 connectivity check.
  Queued: ['Indexing', 'Completed', 'Stale', 'Cancelled', 'Failed'],
  Indexing: ['Reviewing', 'Partial', 'Stale', 'Cancelled', 'Failed'],
  Reviewing: ['Verifying', 'Partial', 'Stale', 'Cancelled', 'Failed'],
  Verifying: ['Publishing', 'Completed', 'Partial', 'Stale', 'Cancelled', 'Failed'],
  Publishing: ['Completed', 'Partial', 'Stale', 'Cancelled', 'Failed'],
  Completed: [],
  Partial: [],
  Stale: [],
  Cancelled: [],
  Failed: [],
};
export function canTransitionRun(from: RunStatus, to: RunStatus): boolean {
  return transitions[from].includes(to);
}
