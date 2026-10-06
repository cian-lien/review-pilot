import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
try {
  process.loadEnvFile(new URL('../../../.env', import.meta.url));
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
const root = fileURLToPath(new URL('..', import.meta.url));
const [task, ...extra] = process.argv.slice(2);
const run = (command, args) => {
  const result = spawnSync(command, args, { cwd: root, stdio: 'inherit', env: process.env });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
};
switch (task) {
  case 'build':
    for (const entry of ['api', 'dispatcher', 'migrate']) {
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
  case 'dev':
    run('go', ['run', './cmd/api', ...extra]);
    break;
  case 'start':
    run(root + '/bin/api', extra);
    break;
  default:
    throw new Error('Unknown Go task: ' + task);
}
