import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
const moduleURL = new URL('../scripts/environment.mjs', import.meta.url).href;
function probe(role: string) {
  const script = `import {workerEnvironment} from ${JSON.stringify(moduleURL)};process.stdout.write(JSON.stringify(workerEnvironment(${JSON.stringify(role)})));`;
  const result = spawnSync(process.execPath, ['--input-type=module', '-e', script], {
    cwd: fileURLToPath(new URL('../../../', import.meta.url)),
    encoding: 'utf8',
    env: {
      PATH: process.env.PATH,
      REVIEW_TEST_ISOLATED: '1',
      REVIEW_MODE: 'fake',
      REVIEW_FIXTURE_ID: 'launch-test',
      MODEL_API_KEY: 'model-sentinel',
      REVIEW_WORKER_SERVICE_TOKEN: 'review-sentinel',
      EVOLUTION_WORKER_SERVICE_TOKEN: 'evolution-sentinel',
      DATABASE_URL: 'database-sentinel',
      GITHUB_CLIENT_SECRET: 'github-sentinel',
      REVIEW_FAKE_SCRIPT_FILE: '/owned/fake.json',
    },
  });
  expect(result.status).toBe(0);
  return JSON.parse(result.stdout) as Record<string, string>;
}
describe('worker child credentials', () => {
  it('review receives only its role and model credentials', () => {
    const env = probe('review');
    expect(env.MODEL_API_KEY).toBe('model-sentinel');
    expect(env.REVIEW_WORKER_SERVICE_TOKEN).toBe('review-sentinel');
    expect(env.REVIEW_FAKE_SCRIPT_FILE).toBe('/owned/fake.json');
    expect(env).not.toHaveProperty('DATABASE_URL');
    expect(env).not.toHaveProperty('GITHUB_CLIENT_SECRET');
    expect(env).not.toHaveProperty('EVOLUTION_WORKER_SERVICE_TOKEN');
  });
  it('evolution never receives model credentials or fake scripts', () => {
    const env = probe('evolution');
    expect(env.EVOLUTION_WORKER_SERVICE_TOKEN).toBe('evolution-sentinel');
    expect(env).not.toHaveProperty('MODEL_API_KEY');
    expect(env).not.toHaveProperty('REVIEW_FAKE_SCRIPT_FILE');
    expect(env).not.toHaveProperty('REVIEW_WORKER_SERVICE_TOKEN');
    expect(env).not.toHaveProperty('DATABASE_URL');
  });
});
