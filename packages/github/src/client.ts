import { readFile } from 'node:fs/promises';
import { sign } from 'node:crypto';
import { z } from 'zod';
import type { TenantRef } from '@reviewpilot/domain';
import type {
  AccessGateway,
  Installation,
  Repository,
  UserRepository,
} from './installation-access';
const numericId = z.number().int().positive().safe();
const repositorySchema = z.object({
  id: numericId,
  full_name: z.string().regex(/^[^/]+\/[^/]+$/),
  private: z.boolean(),
});
const installationSchema = z.object({
  id: numericId,
  app_id: numericId,
  suspended_at: z.string().nullable(),
});
const prSchema = z.object({
  number: z.number().int().positive(),
  state: z.enum(['open', 'closed']),
  base: z.object({ sha: z.string().regex(/^[a-f0-9]{40,64}$/) }),
  head: z.object({ sha: z.string().regex(/^[a-f0-9]{40,64}$/) }),
});
export interface GitHubConfiguration {
  appId: number;
  privateKeyPath: string;
}
export class GitHubRequestError extends Error {
  constructor(public readonly status: number) {
    super(`GitHub request failed (${status})`);
  }
}
export class GitHubClient {
  constructor(
    private readonly configuration: GitHubConfiguration,
    private readonly fetcher: typeof fetch = fetch,
    private readonly apiUrl = 'https://api.github.com',
  ) {}
  async request(path: string, token: string, method = 'GET', body?: unknown): Promise<unknown> {
    const response = await this.fetcher(`${this.apiUrl}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2026-03-10',
        'User-Agent': 'ReviewPilot',
        ...(body ? { 'Content-Type': 'application/json' } : {}),
      },
      ...(body ? { body: JSON.stringify(body) } : {}),
      signal: AbortSignal.timeout(15000),
      redirect: 'error',
    });
    if (!response.ok)
      throw new GitHubRequestError(
        response.status === 403 &&
          (response.headers.get('x-ratelimit-remaining') === '0' ||
            response.headers.has('retry-after'))
          ? 429
          : response.status,
      );
    return response.status === 204 ? null : response.json();
  }
  private async pages<T>(
    path: string,
    token: string,
    key: string,
    schema: z.ZodType<T>,
  ): Promise<T[]> {
    const result: T[] = [];
    for (let page = 1; page <= 50; page++) {
      const payload = z
        .record(z.string(), z.unknown())
        .parse(await this.request(`${path}?per_page=100&page=${page}`, token));
      const items = z.array(schema).parse(payload[key]);
      result.push(...items);
      if (items.length < 100) return result;
    }
    throw new Error('GitHub pagination limit exceeded');
  }
  async userInstallations(token: string): Promise<Installation[]> {
    return (
      await this.pages('/user/installations', token, 'installations', installationSchema)
    ).map((item) => ({ id: item.id, appId: item.app_id, suspended: item.suspended_at !== null }));
  }
  async userInstallationRepositories(token: string, id: number): Promise<Repository[]> {
    return (
      await this.pages(
        `/user/installations/${id}/repositories`,
        token,
        'repositories',
        repositorySchema,
      )
    ).map((item) => ({ id: item.id, fullName: item.full_name, public: !item.private }));
  }
  userGateway(token: string): AccessGateway {
    return {
      userInstallations: () => this.userInstallations(token),
      installationRepositories: (id) => this.userInstallationRepositories(token, id),
      userRepository: (name) => this.userRepository(token, name),
    };
  }
  async userRepository(token: string, name: string): Promise<UserRepository> {
    const schema = repositorySchema.extend({
      permissions: z.object({
        pull: z.boolean(),
        maintain: z.boolean().default(false),
        admin: z.boolean().default(false),
      }),
    });
    const item = schema.parse(await this.request(`/repos/${repositoryPath(name)}`, token));
    return {
      id: item.id,
      fullName: item.full_name,
      public: !item.private,
      permissions: item.permissions,
    };
  }
  async installationToken(tenant: TenantRef): Promise<string> {
    const key = await readFile(this.configuration.privateKeyPath, 'utf8'),
      now = Math.floor(Date.now() / 1000);
    const encode = (value: unknown) => Buffer.from(JSON.stringify(value)).toString('base64url');
    const input = `${encode({ alg: 'RS256', typ: 'JWT' })}.${encode({ iat: now - 60, exp: now + 540, iss: this.configuration.appId })}`;
    const jwt = `${input}.${sign('RSA-SHA256', Buffer.from(input), key).toString('base64url')}`;
    const installation = z
      .object({ id: numericId, suspended_at: z.string().nullable() })
      .parse(await this.request(`/app/installations/${tenant.installationId}`, jwt));
    if (installation.id !== tenant.installationId || installation.suspended_at !== null)
      throw new GitHubRequestError(403);
    return z
      .object({ token: z.string().min(1), expires_at: z.string() })
      .parse(
        await this.request(
          `/app/installations/${tenant.installationId}/access_tokens`,
          jwt,
          'POST',
          {
            repository_ids: [tenant.repositoryId],
            permissions: { contents: 'read', pull_requests: 'read', checks: 'write' },
          },
        ),
      ).token;
  }
  async repository(token: string, name: string): Promise<Repository> {
    const item = repositorySchema.parse(
      await this.request(`/repos/${repositoryPath(name)}`, token),
    );
    return { id: item.id, fullName: item.full_name, public: !item.private };
  }
  async pullRequest(
    token: string,
    name: string,
    number: number,
  ): Promise<z.infer<typeof prSchema>> {
    return prSchema.parse(
      await this.request(`/repos/${repositoryPath(name)}/pulls/${number}`, token),
    );
  }
  async user(token: string): Promise<{ id: number; login: string }> {
    return z
      .object({ id: numericId, login: z.string().min(1) })
      .parse(await this.request('/user', token));
  }
}
export function repositoryPath(name: string): string {
  const parts = name.split('/');
  if (parts.length !== 2 || parts.some((part) => !part || part === '.' || part === '..'))
    throw new Error('Invalid repository name');
  return parts.map(encodeURIComponent).join('/');
}
export async function exchangeOAuthCode(
  configuration: { clientId: string; clientSecret: string; appUrl: string },
  code: string,
  verifier: string,
): Promise<{ accessToken: string; expiresIn: number }> {
  const response = await fetch('https://github.com/login/oauth/access_token', {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client_id: configuration.clientId,
      client_secret: configuration.clientSecret,
      code,
      code_verifier: verifier,
      redirect_uri: `${configuration.appUrl}/auth/github/callback`,
    }),
    signal: AbortSignal.timeout(15000),
    redirect: 'error',
  });
  if (!response.ok) throw new GitHubRequestError(response.status);
  const payload = z
    .object({
      access_token: z.string().min(1),
      expires_in: z.number().int().positive().default(28800),
    })
    .parse(await response.json());
  return { accessToken: payload.access_token, expiresIn: Math.min(payload.expires_in, 28800) };
}
