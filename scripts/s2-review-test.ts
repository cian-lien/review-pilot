import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));
export async function runReviewScenario(name: string): Promise<Record<string, unknown>> {
  if (name !== 'contracts') throw new Error('REVIEW_SCENARIO_NOT_IMPLEMENTED: ' + name);
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
  const verdict = await runReviewScenario(process.argv[option + 1] ?? '');
  console.log(JSON.stringify(verdict));
}
