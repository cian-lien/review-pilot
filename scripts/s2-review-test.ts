import { spawn, execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { randomBytes } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createServer } from 'node:net';

const scenarios: Record<string, { pkg: string; prefix: string }> = {
  context: { pkg: './internal/reviewcontext', prefix: 'Test(Context|ReviewContext)' },
  budget: { pkg: './internal/budgets', prefix: 'Test(Budget|Settled)' },
  stages: { pkg: './internal/reviewstages', prefix: 'TestReviewStages' },
  results: { pkg: './internal/reviews', prefix: 'TestReview(Result|Query)' },
  publication: { pkg: './internal/publisher', prefix: 'Test(Review|Foreign)' },
  pipeline: { pkg: './internal/reviews', prefix: 'TestReviewPipeline' },
};
async function execute(args: string[], env: NodeJS.ProcessEnv): Promise<void> {
  const child = spawn('pnpm', ['exec', 'turbo', 'run', ...args], {
    cwd: root,
    stdio: ['ignore', 'pipe', 'inherit'],
    env,
  });
  let output = '';
  child.stdout.on('data', (chunk: Buffer) => {
    output += chunk.toString();
    process.stdout.write(chunk);
  });
  const status = await new Promise<number>((resolve, reject) => {
    child.once('error', reject);
    child.once('exit', (code, signal) => resolve(signal ? 1 : (code ?? 1)));
  });
  if (status) throw new Error('REVIEW_SCENARIO_FAILED');
  if (/--- SKIP:|\[no tests to run\]/.test(output) || !output.includes('=== RUN'))
    throw new Error('REVIEW_SCENARIO_NOT_EXECUTED');
}
async function databaseScenario(name: string): Promise<Record<string, unknown>> {
  const scenario = scenarios[name];
  if (!scenario) throw new Error('REVIEW_SCENARIO_NOT_IMPLEMENTED');
  const { default: EmbeddedPostgres } = await import('embedded-postgres');
  const directory = await mkdtemp(join(tmpdir(), 'reviewpilot-review-' + name + '-'));
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('TEST_PORT_UNAVAILABLE');
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  const port = address.port;
  const postgres = new EmbeddedPostgres({
    databaseDir: join(directory, 'data'),
    port,
    user: 'reviewpilot_test',
    password: 'isolated-only',
    authMethod: 'scram-sha-256',
    persistent: true,
    createPostgresUser: false,
    postgresFlags: ['-h', '127.0.0.1', '-k', directory],
    onLog: () => {},
    onError: () => {},
  });
  const docker = promisify(execFile);
  let brokerID: string | undefined;
  let brokerURL = '';
  try {
    if (name === 'pipeline') {
      const socket = createServer();
      await new Promise<void>((resolve) => socket.listen(0, '127.0.0.1', resolve));
      const brokerAddress = socket.address();
      if (!brokerAddress || typeof brokerAddress === 'string')
        throw new Error('TEST_PORT_UNAVAILABLE');
      await new Promise<void>((resolve) => socket.close(() => resolve()));
      const password = randomBytes(24).toString('hex');
      const started = await docker(
        'docker',
        [
          'run',
          '-d',
          '--name',
          'reviewpilot-s2-' + randomBytes(10).toString('hex'),
          '--label',
          'reviewpilot.test-owned=true',
          '-p',
          '127.0.0.1:' + brokerAddress.port + ':5672',
          '-e',
          'RABBITMQ_DEFAULT_USER=reviewpilot_test',
          '-e',
          'RABBITMQ_DEFAULT_PASS=' + password,
          'rabbitmq:4.3.6-management-alpine',
        ],
        { timeout: 30000 },
      );
      brokerID = started.stdout.trim();
      if (!/^[a-f0-9]{64}$/.test(brokerID)) throw new Error('INVALID_OWNED_BROKER');
      let ready = false;
      for (let i = 0; i < 40; i++) {
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
            { timeout: 3000 },
          );
          ready = true;
          break;
        } catch {
          await delay(500);
        }
      }
      if (!ready) throw new Error('OWNED_BROKER_UNAVAILABLE');
      brokerURL = 'amqp://reviewpilot_test:' + password + '@127.0.0.1:' + brokerAddress.port;
    }
    await postgres.initialise();
    await postgres.start();
    await postgres.createDatabase('reviewpilot_migration_test');
    const verdictPath = join(directory, 'verdict.json');
    // Tests receive only owned infrastructure, never project environment files or business credentials.
    const env: NodeJS.ProcessEnv = {
      PATH: process.env.PATH,
      HOME: process.env.HOME,
      LANG: process.env.LANG,
      TMPDIR: process.env.TMPDIR,
      REVIEW_TEST_ISOLATED: '1',
      TEST_DATABASE_URL:
        'postgresql://reviewpilot_test:isolated-only@127.0.0.1:' +
        port +
        '/reviewpilot_migration_test',
      MIGRATION_DIRECTORY: join(root, 'packages/persistence/migrations'),
      SCENARIO_RESULT_PATH: verdictPath,
      TEST_RABBITMQ_URL: brokerURL,
    };
    await execute(
      [
        'test:unit',
        '--filter=@reviewpilot/api',
        '--',
        '-tags',
        'integration',
        scenario.pkg,
        '-run',
        '^' + scenario.prefix,
        '-count=1',
        '-v',
      ],
      env,
    );
    const verdict = JSON.parse(await readFile(verdictPath, 'utf8')) as Record<string, unknown>;
    if (verdict.simulated !== true || verdict.scenario !== name || verdict.passed !== true)
      throw new Error('INVALID_REVIEW_VERDICT');
    return verdict;
  } finally {
    if (brokerID && /^[a-f0-9]{64}$/.test(brokerID))
      await docker('docker', ['rm', '-f', '-v', brokerID], { timeout: 10000 }).catch(() => {});
    await postgres.stop().catch(() => {});
    await rm(directory, { recursive: true, force: true });
  }
}

const root = fileURLToPath(new URL('../', import.meta.url));
export async function runReviewScenario(name: string): Promise<Record<string, unknown>> {
  if (name !== 'contracts') return databaseScenario(name);
  const directory = await mkdtemp(join(tmpdir(), 'reviewpilot-review-contracts-'));
  const report = join(directory, 'verdict.json');
  try {
    const child = spawn(
      'pnpm',
      [
        'exec',
        'turbo',
        'run',
        'test:unit',
        '--filter=@reviewpilot/contracts',
        '--',
        '--reporter=json',
        '--outputFile=' + report,
      ],
      {
        cwd: root,
        stdio: 'inherit',
        env: { ...process.env, REVIEW_TEST_ISOLATED: '1' },
      },
    );
    const status = await new Promise<number>((resolve, reject) => {
      child.once('error', reject);
      child.once('exit', (code, signal) => resolve(signal ? 1 : (code ?? 1)));
    });
    if (status !== 0) throw new Error('REVIEW_SCENARIO_FAILED');
    const verdict = JSON.parse(await readFile(report, 'utf8')) as {
      success: boolean;
      numPassedTests: number;
      numFailedTests: number;
      numPendingTests: number;
    };
    if (
      !verdict.success ||
      verdict.numPassedTests < 12 ||
      verdict.numFailedTests ||
      verdict.numPendingTests
    )
      throw new Error('REVIEW_CONTRACT_VERDICT_INVALID');
    return { contractsValid: true, passedTests: verdict.numPassedTests, simulated: true };
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const option = process.argv.indexOf('--case');
  if (option < 0) throw new Error('REVIEW_CASE_REQUIRED');
  // The outer process must not import EmbeddedPostgres: its exit hook can hide failures.
  const directory = await mkdtemp(join(tmpdir(), 'reviewpilot-s2-verdict-'));
  try {
    const report = join(directory, 'tests.json');
    const child = spawn(
      'pnpm',
      [
        'exec',
        'vitest',
        'run',
        '--config',
        'vitest.review.config.ts',
        '--reporter=default',
        '--reporter=json',
        '--outputFile.json=' + report,
      ],
      {
        cwd: root,
        stdio: 'inherit',
        env: {
          PATH: process.env.PATH,
          HOME: process.env.HOME,
          LANG: process.env.LANG,
          TMPDIR: process.env.TMPDIR,
          REVIEW_CASE: process.argv[option + 1],
          REVIEW_TEST_ISOLATED: '1',
        },
      },
    );
    const status = await new Promise<number>((resolve, reject) => {
      child.once('error', reject);
      child.once('exit', (code, signal) => resolve(signal ? 1 : (code ?? 1)));
    });
    const verdict = JSON.parse(await readFile(report, 'utf8'));
    if (
      status ||
      !verdict.success ||
      verdict.numPassedTests !== 1 ||
      verdict.numFailedTests ||
      verdict.numPendingTests
    )
      throw new Error('REVIEW_VERDICT_FAILED');
  } catch {
    console.error('REVIEW_VERIFICATION_FAILED');
    process.exitCode = 1;
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}
