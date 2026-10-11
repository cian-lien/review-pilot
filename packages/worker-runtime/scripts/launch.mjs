import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { workerEnvironment } from './environment.mjs';
const role = process.argv[2];
if (!['review', 'evolution'].includes(role)) throw new Error('INVALID_WORKER_ROLE');
const env = workerEnvironment(role);
const entry = fileURLToPath(
  new URL('../../../apps/worker-' + role + '/src/main.ts', import.meta.url),
);
const child = spawn(
  process.execPath,
  [
    '--import',
    fileURLToPath(new URL('../../../node_modules/tsx/dist/loader.mjs', import.meta.url)),
    entry,
    ...process.argv.slice(3),
  ],
  { stdio: 'inherit', env },
);
for (const event of ['SIGINT', 'SIGTERM']) process.once(event, () => child.kill(event));
child.once('error', () => {
  console.error('WORKER_LAUNCH_FAILED');
  process.exitCode = 1;
});
child.once('exit', (code, signal) => {
  process.exitCode = signal ? 1 : (code ?? 1);
});
