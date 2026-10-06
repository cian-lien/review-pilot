import { spawn } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { parseEnv } from 'node:util';
import { fileURLToPath } from 'node:url';
const role = process.argv[2];
if (!['review', 'evolution'].includes(role)) throw new Error('INVALID_WORKER_ROLE');
const allowed = [
  'PATH',
  'NODE_ENV',
  'LANG',
  'TMPDIR',
  'TMP',
  'TEMP',
  'RABBITMQ_URL',
  'INTERNAL_API_URL',
  'OTEL_EXPORTER_OTLP_ENDPOINT',
  'OTEL_SERVICE_NAME',
  'HEARTBEAT_FILE',
  role === 'review' ? 'REVIEW_WORKER_SERVICE_TOKEN' : 'EVOLUTION_WORKER_SERVICE_TOKEN',
];
let local = {};
try {
  local = parseEnv(readFileSync(new URL('../../../.env', import.meta.url), 'utf8'));
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
const env = {};
for (const name of allowed) {
  if (process.env[name] !== undefined) env[name] = process.env[name];
  else if (local[name] !== undefined) env[name] = local[name];
  const file = process.env[name + '_FILE'] ?? local[name + '_FILE'];
  if (
    [
      'RABBITMQ_URL',
      role === 'review' ? 'REVIEW_WORKER_SERVICE_TOKEN' : 'EVOLUTION_WORKER_SERVICE_TOKEN',
    ].includes(name) &&
    file
  ) {
    const value = readFileSync(file, 'utf8').trim();
    if (!value || value.length > 65536) throw new Error('INVALID_SECRET_FILE');
    env[name] = value;
  }
}
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
