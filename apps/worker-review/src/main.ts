import { runReviewWorker } from '@reviewpilot/worker-runtime';
import { setupObservability, safeLog, workerHeartbeat } from '@reviewpilot/observability';
const required = ['RABBITMQ_URL', 'INTERNAL_API_URL', 'REVIEW_WORKER_SERVICE_TOKEN'] as const;
const missing = required.filter((name) => !process.env[name]);
if (process.argv.includes('--check'))
  console.info(
    JSON.stringify({
      service: 'worker-review',
      phase: 'foundation',
      configured: missing.length === 0,
      missing,
    }),
  );
else if (missing.length) {
  console.error('Worker 配置缺少：' + missing.join(', '));
  process.exitCode = 1;
} else {
  const shutdown = await setupObservability('worker-review').catch(() => {
    safeLog({ service: 'worker-review', code: 'TELEMETRY_UNAVAILABLE' });
    return async () => {};
  });
  const controller = new AbortController();
  process.once('SIGINT', () => controller.abort());
  process.once('SIGTERM', () => controller.abort());
  workerHeartbeat('worker-review');
  const heartbeat = setInterval(() => workerHeartbeat('worker-review'), 15000);
  runReviewWorker(controller.signal)
    .catch(() => {
      safeLog({ service: 'worker-review', code: 'REVIEW_WORKER_START_FAILED' });
      process.exitCode = 1;
    })
    .finally(async () => {
      clearInterval(heartbeat);
      await shutdown();
    });
}
