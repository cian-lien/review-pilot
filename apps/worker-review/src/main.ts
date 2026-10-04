import { randomUUID } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';
import { GitHubClient, GitHubRequestError } from '@reviewpilot/github';
import { claimJob, createPool, failJob, setRepositoryEnabled } from '@reviewpilot/persistence';
import type { RunSnapshot } from '@reviewpilot/domain';
import { processConnectionJob } from './process-job';

const required = ['DATABASE_URL', 'APP_URL', 'GITHUB_APP_ID', 'GITHUB_PRIVATE_KEY_PATH'] as const;
const missing = required.filter((key) => !process.env[key]);
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
  console.error(`Worker 配置缺少：${missing.join(', ')}`);
  process.exitCode = 1;
} else {
  try {
    const appId = Number(process.env.GITHUB_APP_ID);
    if (!Number.isSafeInteger(appId) || appId <= 0) throw new Error('Invalid GITHUB_APP_ID');
    const pool = createPool(process.env.DATABASE_URL!);
    const api = new GitHubClient({ appId, privateKeyPath: process.env.GITHUB_PRIVATE_KEY_PATH! });
    const workerId = `review-${randomUUID()}`,
      controller = new AbortController();
    process.once('SIGINT', () => controller.abort());
    process.once('SIGTERM', () => controller.abort());
    console.info('Review Worker 已启动：S1 仅进行 GitHub 连接检查，不调用模型。');
    try {
      while (!controller.signal.aborted) {
        const lease = await claimJob(pool, workerId, new Date(), 90);
        if (!lease) {
          await delay(1000, undefined, { signal: controller.signal }).catch(() => {});
          continue;
        }
        try {
          await processConnectionJob(pool, api, lease, process.env.APP_URL!.replace(/\/$/, ''));
        } catch (error) {
          if (error instanceof GitHubRequestError && [403, 404].includes(error.status)) {
            const run = await pool.query<{ snapshot: RunSnapshot }>(
              'SELECT snapshot FROM review_runs WHERE id=$1',
              [lease.runId],
            );
            if (run.rows[0])
              await setRepositoryEnabled(pool, run.rows[0].snapshot.identity.tenant, false);
          }
          // Never log remote response bodies, user tokens, keys or connection strings.
          console.warn(
            JSON.stringify({
              runId: lease.runId,
              code:
                error instanceof GitHubRequestError
                  ? `GITHUB_${error.status}`
                  : 'CONNECTION_JOB_FAILED',
            }),
          );
          await failJob(
            pool,
            lease,
            'CONNECTION_JOB_FAILED',
            Math.min(300, 30 * 2 ** Math.min(lease.attempts - 1, 3)),
          ).catch(() => {});
        }
      }
    } finally {
      await pool.end();
    }
  } catch {
    console.error('REVIEW_WORKER_START_FAILED：请检查服务端配置与数据库。');
    process.exitCode = 1;
  }
}
