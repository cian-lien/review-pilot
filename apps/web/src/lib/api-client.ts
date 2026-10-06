import type { components } from '@reviewpilot/contracts';
type DTO = components['schemas'];
export type SetEnabledInput = {
  tenant: DTO['TenantRef'];
  enabled: boolean;
  configVersion: number;
  csrfToken: string;
};
export class APIRequestError extends Error {
  constructor(
    public readonly code: string,
    public readonly status: number,
    message: string,
  ) {
    super(message);
  }
}
export function errorHint(error: unknown): string {
  return error instanceof APIRequestError ? error.message : '服务暂时不可用，请稍后重试。';
}
export function createAPIClient(fetcher: typeof fetch) {
  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    let response: Response;
    try {
      response = await fetcher(path, {
        ...init,
        credentials: 'include',
        redirect: 'error',
        cache: 'no-store',
        signal: AbortSignal.timeout(15000),
      });
    } catch {
      throw new APIRequestError('UNAVAILABLE', 0, '服务暂时不可用，请稍后重试。');
    }
    if (!response.ok) {
      const hints: Record<number, string> = {
        401: '登录已失效，请重新登录 GitHub。',
        403: '当前账号没有维护此仓库的权限，请确认授权。',
        409: '仓库配置已变化，请刷新后重试。',
      };
      let code = 'REQUEST_FAILED';
      try {
        const body: unknown = await response.json();
        if (
          body &&
          typeof body === 'object' &&
          'code' in body &&
          typeof body.code === 'string' &&
          /^[A-Z_]{1,64}$/.test(body.code)
        )
          code = body.code;
      } catch {
        /* Proxy errors need not contain JSON. */
      }
      throw new APIRequestError(
        code,
        response.status,
        hints[response.status] ?? '服务暂时不可用，请稍后重试。',
      );
    }
    return response.status === 204 ? (undefined as T) : (response.json() as Promise<T>);
  }
  const post = (csrfToken: string, body?: unknown): RequestInit => ({
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  return {
    getBootstrap: () => request<DTO['BootstrapResponse']>('/api/bootstrap'),
    getSession: () => request<DTO['SessionResponse']>('/api/session'),
    listRepositories: () => request<DTO['RepositoryListResponse']>('/api/repositories'),
    listRuns: () => request<DTO['RunListResponse']>('/api/runs'),
    syncRepositories: (csrfToken: string) =>
      request<void>('/api/repositories/sync', post(csrfToken)),
    setEnabled: (input: SetEnabledInput) => {
      const body: DTO['SetRepositoryEnabledRequest'] = {
        enabled: input.enabled,
        configVersion: input.configVersion,
      };
      return request<void>(
        `/api/repositories/${input.tenant.installationId}/${input.tenant.repositoryId}`,
        post(input.csrfToken, body),
      );
    },
    logout: (csrfToken: string) => request<void>('/auth/logout', post(csrfToken)),
  };
}
export type APIClient = ReturnType<typeof createAPIClient>;
