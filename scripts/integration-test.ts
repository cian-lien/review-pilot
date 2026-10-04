import { spawn } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { z } from 'zod';

// embedded-postgres/async-exit-hook can turn beforeExit into exit(0).
// Check the JSON test verdict outside that process so CI can never report a failed suite as green.
const directory = await mkdtemp(join(tmpdir(), 'rp-test-result-'));
try {
  const report = join(directory, 'result.json');
  const child = spawn(
    process.execPath,
    [
      fileURLToPath(new URL('../node_modules/vitest/vitest.mjs', import.meta.url)),
      'run',
      '--config',
      'vitest.integration.config.ts',
      '--reporter=default',
      '--reporter=json',
      `--outputFile.json=${report}`,
      ...process.argv.slice(2),
    ],
    { stdio: 'inherit' },
  );
  process.once('SIGINT', () => child.kill('SIGINT'));
  process.once('SIGTERM', () => child.kill('SIGTERM'));
  const code = await new Promise<number>((resolve, reject) => {
    child.once('error', reject);
    child.once('exit', (code, signal) => resolve(signal ? 1 : (code ?? 1)));
  });
  const result = z
    .object({
      success: z.boolean(),
      numFailedTests: z.number(),
      numFailedTestSuites: z.number(),
      numTotalTests: z.number(),
    })
    .parse(JSON.parse(await readFile(report, 'utf8')));
  process.exitCode =
    code === 0 &&
    result.success &&
    result.numFailedTests === 0 &&
    result.numFailedTestSuites === 0 &&
    result.numTotalTests > 0
      ? 0
      : 1;
} catch {
  console.error('Integration test result could not be confirmed.');
  process.exitCode = 1;
} finally {
  await rm(directory, { recursive: true, force: true });
}
