import { runReviewWorker } from '@reviewpilot/worker-runtime';
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
  const controller = new AbortController();
  process.once('SIGINT', () => controller.abort());
  process.once('SIGTERM', () => controller.abort());
  runReviewWorker(controller.signal).catch(() => {
    console.error('REVIEW_WORKER_START_FAILED');
    process.exitCode = 1;
  });
}
