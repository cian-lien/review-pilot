import { ChatOpenAI } from '@langchain/openai';
import { readBoundedBytes } from '@reviewpilot/repository';
import {
  ProviderFault,
  type ModelConfiguration,
  type ModelProvider,
  type ProviderRequest,
  type ProviderResponse,
} from './types';
import { safeHash } from './gateway';
export function boundedModelFetch(
  configuration: ModelConfiguration,
  transport: typeof fetch = fetch,
): typeof fetch {
  const endpoint = configuration.baseURL.replace(/\/$/, '') + '/chat/completions';
  return async (input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    if (url !== endpoint || init?.method !== 'POST')
      throw new ProviderFault('MODEL_DESTINATION_MISMATCH');
    const signal = init.signal ?? AbortSignal.timeout(60000);
    const response = await transport(input, { ...init, signal, redirect: 'error' });
    const body = await readBoundedBytes(response, 262144, signal);
    const headers = new Headers(response.headers);
    headers.delete('content-encoding');
    headers.set('content-length', String(body.length));
    return new Response(new Uint8Array(body), {
      status: response.status,
      statusText: response.statusText,
      headers,
    });
  };
}
export class OpenAICompatibleProvider implements ModelProvider {
  constructor(
    readonly configuration: ModelConfiguration,
    private readonly apiKey: string,
    private readonly transport: typeof fetch = fetch,
  ) {
    if (
      process.env.REVIEW_MODE !== 'real' ||
      process.env.REVIEW_REAL_ALLOWED !== 'true' ||
      process.env.REVIEW_FAKE_SCRIPT_FILE ||
      configuration.fixtureId ||
      configuration.currency === 'TEST' ||
      new URL(configuration.baseURL).protocol !== 'https:' ||
      !/^gpt-4o(?:-mini)?(?:-\d{4}-\d{2}-\d{2})?$/.test(configuration.model) ||
      !apiKey
    )
      throw new Error('REAL_MODEL_NOT_ALLOWED');
  }
  private model(outputTokenLimit: number): ChatOpenAI {
    return new ChatOpenAI({
      model: this.configuration.model,
      apiKey: this.apiKey,
      maxTokens: outputTokenLimit,
      streaming: false,
      streamUsage: false,
      useResponsesApi: false,
      maxRetries: 0,
      timeout: 60000,
      configuration: {
        baseURL: this.configuration.baseURL,
        maxRetries: 0,
        fetch: boundedModelFetch(this.configuration, this.transport),
      },
    });
  }
  async invoke(request: ProviderRequest, signal: AbortSignal): Promise<ProviderResponse> {
    try {
      const response = await this.model(request.outputTokenLimit).invoke(
        [
          { role: 'system', content: request.system },
          { role: 'user', content: request.input },
        ],
        {
          signal,
          response_format:
            this.configuration.structuredOutputMode === 'json-schema'
              ? {
                  type: 'json_schema',
                  json_schema: { name: 'review_response', strict: true, schema: request.schema },
                }
              : { type: 'json_object' },
        },
      );
      if (typeof response.content !== 'string' || response.tool_calls?.length)
        throw new ProviderFault('MODEL_RESPONSE_INVALID');
      const usage = response.usage_metadata
        ? {
            inputTokens: response.usage_metadata.input_tokens,
            outputTokens: response.usage_metadata.output_tokens,
          }
        : undefined;
      let data: unknown;
      try {
        data = JSON.parse(response.content);
      } catch {
        throw new ProviderFault('MODEL_SCHEMA_INVALID', usage);
      }
      return {
        data,
        usage,
        safeResponseHash: safeHash(JSON.stringify(data)),
        truncated: response.response_metadata.finish_reason !== 'stop',
      };
    } catch (error) {
      if (error instanceof ProviderFault) throw error;
      const status =
        error && typeof error === 'object' && 'status' in error ? error.status : undefined;
      // A rate-limit rejection is the only generic compatible error classified as uncharged.
      if (status === 429)
        throw new ProviderFault('MODEL_RATE_LIMITED', { inputTokens: 0, outputTokens: 0 });
      throw new ProviderFault(signal.aborted ? 'MODEL_TIMEOUT' : 'MODEL_EXECUTION_UNKNOWN');
    }
  }
}
