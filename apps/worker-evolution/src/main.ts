import { setTimeout as delay } from 'node:timers/promises';
import { setupObservability, safeLog, workerHeartbeat } from '@reviewpilot/observability';
if (process.argv.includes('--check'))
  console.info(
    JSON.stringify({ service: 'worker-evolution', phase: 'foundation', enabled: false }),
  );
else {
  const shutdown = await setupObservability('worker-evolution').catch(() => async () => {});
  const controller = new AbortController();
  process.once('SIGINT', () => controller.abort());
  process.once('SIGTERM', () => controller.abort());
  safeLog({ service: 'worker-evolution', code: 'EVOLUTION_DISABLED' });
  while (!controller.signal.aborted) {
    workerHeartbeat('worker-evolution');
    await delay(30000, undefined, { signal: controller.signal }).catch(() => {});
  }
  await shutdown();
}
