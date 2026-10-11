import type { ReviewLease } from '@reviewpilot/contracts';
import type { ReviewInternalClient } from '@reviewpilot/review-engine';
export function leaseHeartbeat(
  client: ReviewInternalClient,
  lease: ReviewLease,
  parent: AbortSignal,
  intervalMs = 20000,
) {
  const controller = new AbortController();
  let pending = false;
  const timer = setInterval(() => {
    if (pending || controller.signal.aborted) return;
    pending = true;
    void client
      .heartbeat(lease)
      .catch(() => controller.abort())
      .finally(() => {
        pending = false;
      });
  }, intervalMs);
  return {
    signal: AbortSignal.any([parent, controller.signal]),
    stop() {
      clearInterval(timer);
      controller.abort();
    },
  };
}
