import { afterEach, expect, it, vi } from 'vitest';
import { boundedModelFetch, OpenAICompatibleProvider } from '../src/openai-compatible';
import { configurationSchema } from '../src/configuration';
import { FakeProvider } from '../src/fake';
import { configuration } from './fixture';
const realConfig = {
  ...configuration,
  baseURL: 'https://fixture.invalid/v1',
  model: 'gpt-4o-mini',
  currency: 'USD',
  fixtureId: '',
};
const request = {
  purpose: 'generate' as const,
  system: 'Return JSON.',
  input: '{}',
  schema: { type: 'object' },
  outputTokenLimit: 32,
};
afterEach(() => vi.unstubAllEnvs());
it('bounds UTF8 bytes and prevents redirection or destination changes', async () => {
  let sends = 0;
  const transport: typeof fetch = async (_url, init) => {
    sends++;
    expect(init?.redirect).toBe('error');
    return new Response('汉'.repeat(100000));
  };
  const bounded = boundedModelFetch(realConfig, transport);
  await expect(
    bounded('https://different.invalid/v1/chat/completions', { method: 'POST' }),
  ).rejects.toThrow('MODEL_DESTINATION_MISMATCH');
  expect(sends).toBe(0);
  await expect(
    bounded('https://fixture.invalid/v1/chat/completions', { method: 'POST' }),
  ).rejects.toThrow('RESPONSE_TOO_LARGE');
  expect(sends).toBe(1);
});
it('forces Chat Completions, structured mode, output cap and zero SDK retries', async () => {
  vi.stubEnv('REVIEW_MODE', 'real');
  vi.stubEnv('REVIEW_REAL_ALLOWED', 'true');
  vi.stubEnv('REVIEW_FAKE_SCRIPT_FILE', '');
  let sends = 0;
  const transport: typeof fetch = async (url, init) => {
    sends++;
    expect(String(url)).toBe(realConfig.baseURL + '/chat/completions');
    const body = JSON.parse(String(init?.body));
    expect(body.stream).toBe(false);
    expect(body.max_tokens).toBe(32);
    expect(body.response_format).toEqual({ type: 'json_object' });
    expect(body.tools).toBeUndefined();
    return new Response(JSON.stringify({ error: { message: 'deliberately not logged' } }), {
      status: 500,
      headers: { 'content-type': 'application/json' },
    });
  };
  const provider = new OpenAICompatibleProvider(realConfig, 'fixture-only', transport);
  await expect(provider.invoke(request, new AbortController().signal)).rejects.toThrow(
    'MODEL_EXECUTION_UNKNOWN',
  );
  expect(sends).toBe(1);
});
it('rejects credentials in model configuration and refuses unowned fake scripts', () => {
  expect(configurationSchema.safeParse({ ...configuration, apiKey: 'secret' }).success).toBe(false);
  vi.stubEnv('REVIEW_TEST_ISOLATED', '1');
  vi.stubEnv('REVIEW_FIXTURE_ID', configuration.fixtureId);
  vi.stubEnv('TEST_MODEL_BASE_URL', configuration.baseURL);
  expect(() => new FakeProvider(configuration, { fixtureId: 'different', responses: [] })).toThrow(
    'FIXTURE_ID_MISMATCH',
  );
});
it('binds owned runtime evidence without evaluating fixture templates', async () => {
  vi.stubEnv('REVIEW_TEST_ISOLATED', '1');
  vi.stubEnv('REVIEW_FIXTURE_ID', configuration.fixtureId);
  vi.stubEnv('TEST_MODEL_BASE_URL', configuration.baseURL);
  const ref = { evidenceId: 'owned-evidence' };
  const provider = new FakeProvider(configuration, {
    fixtureId: configuration.fixtureId,
    responses: [
      {
        purpose: 'generate',
        evidenceFromInput: true,
        data: { candidates: [{ evidence: [] }] },
        usage: { inputTokens: 1, outputTokens: 1 },
      },
    ],
  });
  expect(
    (
      await provider.invoke(
        { ...request, input: JSON.stringify({ evidence: [{ ref }] }) },
        new AbortController().signal,
      )
    ).data,
  ).toEqual({ candidates: [{ evidence: [ref] }] });
});
