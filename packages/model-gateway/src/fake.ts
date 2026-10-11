import { z } from 'zod';
import { readBoundedFile } from './configuration';
import { safeHash } from './gateway';
import {
  ProviderFault,
  type ModelConfiguration,
  type ModelProvider,
  type ProviderRequest,
  type ProviderResponse,
} from './types';
const usage = z.strictObject({
  inputTokens: z.number().int().nonnegative().max(12000),
  outputTokens: z.number().int().nonnegative().max(4096),
});
const scriptSchema = z.strictObject({
  fixtureId: z.string().min(1).max(128),
  responses: z
    .array(
      z.strictObject({
        purpose: z.enum(['generate', 'verify']),
        data: z.unknown().optional(),
        evidenceFromInput: z.boolean().optional(),
        usage: usage.optional(),
        truncated: z.boolean().optional(),
        error: z
          .enum([
            'MODEL_TIMEOUT',
            'MODEL_RATE_LIMITED',
            'MODEL_TRANSIENT_FAILURE',
            'MODEL_CREDENTIALS_INVALID',
          ])
          .optional(),
        delayMs: z.number().int().min(0).max(60000).optional(),
      }),
    )
    .max(16),
});
export class FakeProvider implements ModelProvider {
  private index = 0;
  private readonly script: z.infer<typeof scriptSchema>;
  constructor(
    readonly configuration: ModelConfiguration,
    script: unknown,
  ) {
    if (
      process.env.REVIEW_TEST_ISOLATED !== '1' ||
      process.env.REVIEW_FIXTURE_ID !== configuration.fixtureId ||
      process.env.TEST_MODEL_BASE_URL !== configuration.baseURL ||
      configuration.currency !== 'TEST'
    )
      throw new Error('FAKE_MODEL_NOT_ISOLATED');
    const url = new URL(configuration.baseURL);
    if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname))
      throw new Error('FAKE_MODEL_NOT_ISOLATED');
    this.script = scriptSchema.parse(script);
    if (this.script.fixtureId !== configuration.fixtureId) throw new Error('FIXTURE_ID_MISMATCH');
  }
  static fromFile(configuration: ModelConfiguration, path: string): FakeProvider {
    return new FakeProvider(configuration, JSON.parse(readBoundedFile(path, 131072)));
  }
  async invoke(request: ProviderRequest, signal: AbortSignal): Promise<ProviderResponse> {
    signal.throwIfAborted();
    const entry = this.script.responses[this.index++];
    if (!entry || entry.purpose !== request.purpose)
      throw new ProviderFault('FAKE_SCRIPT_EXHAUSTED');
    if (entry.delayMs)
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(resolve, entry.delayMs);
        signal.addEventListener(
          'abort',
          () => {
            clearTimeout(timer);
            reject(new ProviderFault('MODEL_TIMEOUT'));
          },
          { once: true },
        );
      });
    signal.throwIfAborted();
    if (entry.error) throw new ProviderFault(entry.error, entry.usage);
    const data = structuredClone(entry.data);
    // Only isolated fixtures can bind runtime evidence IDs; no templates or tools are evaluated.
    if (entry.evidenceFromInput) {
      const input = JSON.parse(request.input) as { evidence?: { ref: unknown }[] };
      const value = data as {
        candidates?: { evidence: unknown[] }[];
        decisions?: { evidence: unknown[] }[];
      };
      const ref = input.evidence?.[0]?.ref;
      if (!ref) throw new ProviderFault('FAKE_EVIDENCE_MISSING');
      for (const item of value.candidates ?? value.decisions ?? []) item.evidence = [ref];
    }
    return {
      data,
      usage: entry.usage,
      truncated: entry.truncated,
      safeResponseHash: safeHash(JSON.stringify(data) ?? 'null'),
    };
  }
}
