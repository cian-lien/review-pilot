import { setTimeout as delay } from 'node:timers/promises';
if (process.argv.includes('--check'))
  console.info(
    JSON.stringify({ service: 'worker-evolution', phase: 'foundation', enabled: false }),
  );
else {
  const controller = new AbortController();
  process.once('SIGINT', () => controller.abort());
  process.once('SIGTERM', () => controller.abort());
  console.info('Evolution Worker 基础进程已启动。S1 不连接业务数据库、不领取学习任务。');
  while (!controller.signal.aborted)
    await delay(30000, undefined, { signal: controller.signal }).catch(() => {});
}
