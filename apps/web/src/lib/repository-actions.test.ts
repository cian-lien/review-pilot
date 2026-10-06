import { describe, expect, it } from 'vitest';
import { createAPIClient } from './api-client';
import { toggleRepository } from './repository-actions';

const input = {
  tenant: { installationId: 1, repositoryId: 2 },
  enabled: true,
  configVersion: 3,
  csrfToken: 'test-csrf',
};
describe('browser to Go repository API', () => {
  it('sends the version and CSRF directly, not in the JSON body', async () => {
    let request:
      | {
          url: string;
          headers: Record<string, string>;
          body: unknown;
          credentials?: RequestCredentials;
        }
      | undefined;
    const fetcher: typeof fetch = async (url, init) => {
      request = {
        url: String(url),
        headers: Object.fromEntries(new Headers(init?.headers)),
        body: JSON.parse(String(init?.body)),
        credentials: init?.credentials,
      };
      return new Response(null, { status: 204 });
    };
    await toggleRepository(createAPIClient(fetcher), input);
    expect(request?.url).toBe('/api/repositories/1/2');
    expect(request?.headers['x-csrf-token']).toBe('test-csrf');
    expect(request?.body).toEqual({ enabled: true, configVersion: 3 });
    expect(request?.credentials).toBe('include');
  });
  it.each([
    [409, 'VERSION_CONFLICT', '刷新'],
    [401, 'UNAUTHENTICATED', '登录'],
    [403, 'FORBIDDEN', '权限'],
  ])('rejects %i with a safe Chinese hint', async (status, code, hint) => {
    const fetcher: typeof fetch = async () =>
      Response.json(
        { code, message: 'remote detail should not be shown', requestId: 'test' },
        { status },
      );
    await expect(toggleRepository(createAPIClient(fetcher), input)).rejects.toMatchObject({
      code,
      message: expect.stringContaining(hint),
    });
  });
});
