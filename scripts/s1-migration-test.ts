import { spawn, execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { setTimeout as delay } from 'node:timers/promises';
import { createServer } from 'node:net';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { randomBytes } from 'node:crypto';
import { z } from 'zod';

const root = fileURLToPath(new URL('../', import.meta.url));
const cases: Record<string, { pkg: string; test: string }> = {
  'webhook-check': { pkg: './internal/webhooks', test: 'TestWebhookCheck' },
  'message-recovery': { pkg: './internal/jobs', test: 'TestMessageRecovery' },
  'legacy-auth': { pkg: './internal/auth', test: 'TestLegacyAuth' },
  'legacy-schema': { pkg: './internal/persistence', test: 'TestLegacySchema' },
};
async function execute(command: string, args: string[], env: NodeJS.ProcessEnv): Promise<void> {
  const child = spawn(command, args, { cwd: root, stdio: 'inherit', env });
  const code = await new Promise<number>((resolve, reject) => {
    child.once('error', reject);
    child.once('exit', (code, signal) => resolve(signal ? 1 : (code ?? 1)));
  });
  if (code !== 0) throw new Error('Migration subprocess failed');
}
async function unusedPort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('No private test port');
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  return address.port;
}
export async function runScenario(name: string): Promise<Record<string, unknown>> {
  // Import only inside Vitest's scenario process. This module installs an exit hook;
  // importing it in the outer verdict process can incorrectly force exit(0).
  const { default: EmbeddedPostgres } = await import('embedded-postgres');
  const scenario = cases[name];
  if (!scenario) throw new Error('Unknown migration scenario');
  if (process.getuid?.() === 0) throw new Error('Non-root test process required');
  const directory = await mkdtemp(join(tmpdir(), 'reviewpilot-migration-'));
  const port = await unusedPort();
  const brokerPort = name === 'message-recovery' ? await unusedPort() : undefined;
  const brokerName = 'reviewpilot-migration-' + randomBytes(10).toString('hex');
  let brokerID: string | undefined;
  const brokerPassword = randomBytes(24).toString('hex');
  const docker = promisify(execFile);
  const password = randomBytes(24).toString('hex');
  const postgres = new EmbeddedPostgres({
    databaseDir: join(directory, 'data'),
    port,
    user: 'reviewpilot_test',
    password,
    authMethod: 'scram-sha-256',
    persistent: true,
    createPostgresUser: false,
    postgresFlags: ['-h', '127.0.0.1', '-k', directory],
    onLog: () => {},
    onError: () => {},
  });
  try {
    if (brokerPort) {
      const started = await docker('docker', [
        'run',
        '-d',
        '--name',
        brokerName,
        '--label',
        'reviewpilot.test-owned=true',
        '-p',
        '127.0.0.1:' + brokerPort + ':5672',
        '-e',
        'RABBITMQ_DEFAULT_USER=reviewpilot_test',
        '-e',
        'RABBITMQ_DEFAULT_PASS=' + brokerPassword,
        'rabbitmq:4.3.6-management-alpine',
      ]);
      brokerID = started.stdout.trim();
      if (!/^[a-f0-9]{64}$/.test(brokerID)) throw new Error('Invalid test container identity');
      let ready = false;
      for (let attempt = 0; attempt < 40; attempt++) {
        try {
          await docker(
            'docker',
            [
              'exec',
              '--user',
              'rabbitmq',
              brokerID,
              'rabbitmq-diagnostics',
              '-q',
              'check_port_connectivity',
            ],
            { timeout: 5000 },
          );
          ready = true;
          break;
        } catch {
          await delay(500);
        }
      }
      if (!ready) {
        const logs = await docker('docker', ['logs', '--tail', '40', brokerID]).catch(() => ({
          stdout: 'container unavailable',
          stderr: '',
        }));
        console.error((logs.stdout + logs.stderr).replaceAll(brokerPassword, '[test-secret]'));
        throw new Error('Dedicated RabbitMQ unavailable');
      }
    }
    await postgres.initialise();
    await postgres.start();
    await postgres.createDatabase('reviewpilot_migration_test');
    const resultPath = join(directory, 'scenario.json');
    // Never inherit the user's business database; test helper requires this exact database name.
    const env = {
      ...process.env,
      DATABASE_URL: '',
      TEST_DATABASE_URL:
        'postgresql://reviewpilot_test:' +
        password +
        '@127.0.0.1:' +
        port +
        '/reviewpilot_migration_test',
      MIGRATION_DIRECTORY: join(root, 'packages/persistence/migrations'),
      SCENARIO_RESULT_PATH: resultPath,
      TEST_RABBITMQ_URL: brokerPort
        ? 'amqp://reviewpilot_test:' + brokerPassword + '@127.0.0.1:' + brokerPort
        : '',
    };
    await execute(
      'pnpm',
      [
        'exec',
        'turbo',
        'run',
        'test:unit',
        '--filter=@reviewpilot/api',
        '--',
        '-tags',
        'integration',
        scenario.pkg,
        '-run',
        '^' + scenario.test + '$',
        '-count=1',
      ],
      env,
    );
    return z.record(z.string(), z.unknown()).parse(JSON.parse(await readFile(resultPath, 'utf8')));
  } finally {
    if (brokerID && /^[a-f0-9]{64}$/.test(brokerID)) {
      await docker('docker', ['stop', '--time', '1', brokerID]).catch(() => {});
      await docker('docker', ['rm', '-v', brokerID]).catch(() => {});
    }
    await postgres.stop().catch(() => {});
    await rm(directory, { recursive: true, force: true });
  }
}
async function main(): Promise<void> {
  const option = process.argv.indexOf('--case');
  const selected = option < 0 ? 'all' : process.argv[option + 1];
  if (!selected || (selected !== 'all' && !cases[selected])) throw new Error('Unknown --case');
  const directory = await mkdtemp(join(tmpdir(), 'reviewpilot-migration-verdict-'));
  try {
    const report = join(directory, 'result.json');
    await execute(
      process.execPath,
      [
        fileURLToPath(new URL('../node_modules/vitest/vitest.mjs', import.meta.url)),
        'run',
        '--config',
        'vitest.migration.config.ts',
        '--reporter=default',
        '--reporter=json',
        '--outputFile.json=' + report,
      ],
      { ...process.env, MIGRATION_CASE: selected, DATABASE_URL: '' },
    );
    const verdict = z
      .object({
        success: z.boolean(),
        numFailedTests: z.number(),
        numFailedTestSuites: z.number(),
        numTotalTests: z.number(),
      })
      .parse(JSON.parse(await readFile(report, 'utf8')));
    if (
      !verdict.success ||
      verdict.numFailedTests !== 0 ||
      verdict.numFailedTestSuites !== 0 ||
      verdict.numTotalTests <= 0
    )
      throw new Error('Migration verdict is not green');
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}
if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch(() => {
    console.error('Migration verification failed; inspect scenario output.');
    process.exitCode = 1;
  });
}
