import { setTimeout as delay } from 'node:timers/promises';
import { createPool } from '@reviewpilot/persistence';

if (process.argv.includes('--check'))
  console.info(
    JSON.stringify({
      service: 'worker-evolution',
      phase: 'foundation',
      enabled: false,
      configured: Boolean(process.env.DATABASE_URL),
    }),
  );
else if (!process.env.DATABASE_URL) {
  console.error('Evolution Worker 需要 DATABASE_URL');
  process.exitCode = 1;
} else {
  try {
    const pool = createPool(process.env.DATABASE_URL),
      controller = new AbortController();
    process.once('SIGINT', () => controller.abort());
    process.once('SIGTERM', () => controller.abort());
    try {
      await pool.query('SELECT 1');
      console.info('Evolution Worker 基础进程已启动。自进化在 S4 实现，S1 不领取学习任务。');
      while (!controller.signal.aborted)
        await delay(30000, undefined, { signal: controller.signal }).catch(() => {});
    } finally {
      await pool.end();
    }
  } catch {
    console.error('EVOLUTION_WORKER_START_FAILED：请检查服务端数据库配置。');
    process.exitCode = 1;
  }
}
