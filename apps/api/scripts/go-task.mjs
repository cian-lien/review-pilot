import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';
const [task, ...extra] = process.argv.slice(2);
const isolatedTest = task === 'test' && process.env.REVIEW_TEST_ISOLATED === '1';
try {
  if (!isolatedTest) process.loadEnvFile(new URL('../../../.env', import.meta.url));
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
const root = fileURLToPath(new URL('..', import.meta.url));
for (const name of [
  'DATABASE_URL',
  'RABBITMQ_URL',
  'SESSION_SECRET',
  'GITHUB_CLIENT_SECRET',
  'GITHUB_WEBHOOK_SECRET',
  'REVIEW_WORKER_SERVICE_TOKEN',
  'EVOLUTION_WORKER_SERVICE_TOKEN',
]) {
  if (isolatedTest || !process.env[name + '_FILE']) continue;
  const value = readFileSync(process.env[name + '_FILE'], 'utf8').trim();
  if (!value || value.length > 65536) throw new Error('INVALID_SECRET_FILE');
  process.env[name] = value;
}
const run = (command, args, cwd = root) => {
  const result = spawnSync(command, args, { cwd, stdio: 'inherit', env: process.env });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
};
switch (task) {
  case 'dispatcher':
    if (process.env.NODE_ENV === 'production') run(root + '/bin/dispatcher', extra);
    else run('go', ['run', './cmd/dispatcher', ...extra]);
    break;
  case 'migrate':
    if (process.env.NODE_ENV === 'production') run(root + '/bin/migrate', extra);
    else run('go', ['run', './cmd/migrate', ...extra]);
    break;
  case 'review-admin':
    if (process.env.NODE_ENV === 'production') run(root + '/bin/review-admin', extra);
    else run('go', ['run', './cmd/review-admin', ...extra]);
    break;
  case 'build':
    for (const entry of ['api', 'dispatcher', 'migrate', 'review-admin']) {
      if (existsSync(root + '/cmd/' + entry))
        run('go', ['build', '-trimpath', '-o', 'bin/' + entry, './cmd/' + entry]);
    }
    break;
  case 'typecheck':
    run('go', ['test', './...', '-run', '^$']);
    break;
  case 'lint':
    run('go', ['vet', './...']);
    break;
  case 'format':
    run('gofmt', ['-w', 'cmd', 'internal']);
    run('prettier', ['--write', 'package.json', 'scripts', '--log-level', 'warn']);
    break;
  case 'test':
    run('go', ['test', ...(extra.length ? extra : ['./...', '-count=1'])]);
    break;
  case 'deps':
    run('go', ['mod', 'tidy']);
    break;
  case 'deps-info':
    run('go', ['list', '-m', '-json', ...extra], tmpdir());
    break;
  case 'dev':
    run('go', ['run', './cmd/api', ...extra]);
    break;
  case 'start':
    run(root + '/bin/api', extra);
    break;
  default:
    throw new Error('Unknown Go task: ' + task);
}
