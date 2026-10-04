import 'server-only';
import { cookies } from 'next/headers';
import { GitHubClient, openToken, requireRepositoryAccess } from '@reviewpilot/github';
import {
  createPool,
  readSession,
  synchronizeRepository,
  transaction,
} from '@reviewpilot/persistence';
import type { Pool } from 'pg';
import type { TenantRef } from '@reviewpilot/domain';

const required = [
  'DATABASE_URL',
  'APP_URL',
  'GITHUB_APP_ID',
  'GITHUB_APP_SLUG',
  'GITHUB_CLIENT_ID',
  'GITHUB_CLIENT_SECRET',
  'GITHUB_PRIVATE_KEY_PATH',
  'GITHUB_WEBHOOK_SECRET',
  'SESSION_SECRET',
] as const;
export const SESSION_COOKIE = 'reviewpilot_session';
export const OAUTH_COOKIE = 'reviewpilot_oauth';
let pool: Pool | undefined;
export function configurationIssues(): string[] {
  const issues: string[] = required.filter((key) => !process.env[key]);
  if (
    process.env.GITHUB_APP_ID &&
    (!Number.isSafeInteger(Number(process.env.GITHUB_APP_ID)) ||
      Number(process.env.GITHUB_APP_ID) <= 0)
  )
    issues.push('GITHUB_APP_ID（格式）');
  if (process.env.GITHUB_APP_SLUG && !/^[a-zA-Z0-9-]+$/.test(process.env.GITHUB_APP_SLUG))
    issues.push('GITHUB_APP_SLUG（格式）');
  if (process.env.SESSION_SECRET) {
    const key = Buffer.from(process.env.SESSION_SECRET, 'base64');
    if (key.length !== 32 || key.toString('base64') !== process.env.SESSION_SECRET)
      issues.push('SESSION_SECRET（格式）');
  }
  if (process.env.APP_URL) {
    try {
      const url = new URL(process.env.APP_URL);
      if (
        url.username ||
        url.password ||
        url.search ||
        url.hash ||
        url.pathname !== '/' ||
        (url.protocol !== 'https:' &&
          !(url.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)))
      )
        issues.push('APP_URL（需 HTTPS 或本机 HTTP 根地址）');
    } catch {
      issues.push('APP_URL（格式）');
    }
  }
  return issues;
}
export function configuration() {
  if (configurationIssues().length) throw new Error('Application is not configured');
  return {
    appUrl: process.env.APP_URL!.replace(/\/$/, ''),
    appId: Number(process.env.GITHUB_APP_ID),
    appSlug: process.env.GITHUB_APP_SLUG!,
    clientId: process.env.GITHUB_CLIENT_ID!,
    clientSecret: process.env.GITHUB_CLIENT_SECRET!,
    privateKeyPath: process.env.GITHUB_PRIVATE_KEY_PATH!,
    sessionSecret: process.env.SESSION_SECRET!,
  };
}
export function database(): Pool {
  if (!process.env.DATABASE_URL) throw new Error('Database is not configured');
  return (pool ??= createPool(process.env.DATABASE_URL));
}
export function github(): GitHubClient {
  const config = configuration();
  return new GitHubClient({ appId: config.appId, privateKeyPath: config.privateKeyPath });
}
export function cookieOptions(maxAge: number) {
  return {
    httpOnly: true,
    secure: configuration().appUrl.startsWith('https:'),
    sameSite: 'lax' as const,
    path: '/',
    maxAge,
  };
}
export async function getSession() {
  if (configurationIssues().length) return null;
  const token = (await cookies()).get(SESSION_COOKIE)?.value;
  if (!token) return null;
  const session = await readSession(database(), token);
  return session
    ? {
        ...session,
        token,
        userToken: openToken(session.encryptedToken, configuration().sessionSecret),
      }
    : null;
}
export async function requireSession() {
  const session = await getSession();
  if (!session) throw new Error('Authentication required');
  return session;
}
export async function requireRepoAccess(tenant: TenantRef, action: 'view' | 'manage') {
  const session = await requireSession();
  return requireRepositoryAccess(
    github().userGateway(session.userToken),
    tenant,
    action,
    configuration().appId,
  );
}
export async function syncInstallation(
  session: Awaited<ReturnType<typeof requireSession>>,
  installationId: number,
): Promise<void> {
  const api = github(),
    config = configuration();
  const installation = (await api.userInstallations(session.userToken)).find(
    (item) => item.id === installationId && item.appId === config.appId && !item.suspended,
  );
  if (!installation) throw new Error('Installation access denied');
  const repositories = await api.userInstallationRepositories(session.userToken, installationId);
  await transaction(database(), async (client) => {
    await client.query('INSERT INTO installations(id) VALUES($1) ON CONFLICT DO NOTHING', [
      installationId,
    ]);
    await client.query(
      'INSERT INTO installation_memberships(user_id,installation_id) VALUES($1,$2) ON CONFLICT DO NOTHING',
      [session.userId, installationId],
    );
  });
  for (const repository of repositories)
    await synchronizeRepository(database(), {
      installationId,
      repositoryId: repository.id,
      fullName: repository.fullName,
      public: repository.public,
    });
  // Removal from the installation is handled by signed webhooks; a user may only see a subset of an organization's repositories.
}
