import { generateKeyPairSync, randomBytes, verify, type KeyObject } from 'node:crypto';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createServer } from 'node:net';
import { Pool } from 'pg';
import EmbeddedPostgres from 'embedded-postgres';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { GitHubClient, CONNECTION_CHECK_NAME } from '@reviewpilot/github';
import {
  acceptDelivery,
  claimJob,
  completeJob,
  consumeOAuthAttempt,
  createSession,
  enqueueReview,
  failJob,
  heartbeatJob,
  readSession,
  revokeInstallation,
  saveOAuthAttempt,
  setRepositoryEnabled,
  synchronizeRepository,
} from '@reviewpilot/persistence';
import { migrate } from '@reviewpilot/persistence/migrate';
import { processConnectionJob } from '../fixtures/legacy-process-job';
import { reviewIdentity, runSnapshot } from '../fixtures/index';

let directory: string,
  postgres: EmbeddedPostgres,
  pool: Pool,
  other: Pool,
  databaseUrl: string,
  keyPath: string,
  publicKey: KeyObject;
const identity = reviewIdentity(),
  snapshot = runSnapshot(),
  tenant = identity.tenant;
async function unusedPort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('No test port');
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  return address.port;
}
beforeAll(async () => {
  if (process.getuid?.() === 0)
    throw new Error('Run as a non-root user; no OS users will be created.');
  directory = await mkdtemp(join(tmpdir(), 'rp-pg-'));
  const password = randomBytes(24).toString('hex'),
    port = await unusedPort();
  postgres = new EmbeddedPostgres({
    databaseDir: join(directory, 'data'),
    port,
    user: 'reviewpilot_test',
    password,
    authMethod: 'scram-sha-256',
    persistent: true,
    createPostgresUser: false,
    postgresFlags: ['-h', '127.0.0.1', '-k', directory],
    onLog: () => {},
    onError: () => {},
  });
  await postgres.initialise();
  await postgres.start();
  await postgres.createDatabase('reviewpilot_test');
  databaseUrl = `postgresql://reviewpilot_test:${password}@127.0.0.1:${port}/reviewpilot_test`;
  pool = new Pool({ connectionString: databaseUrl });
  other = new Pool({ connectionString: databaseUrl });
  await migrate(pool);
  await migrate(pool);
  console.info(
    `Integration database: ${(await pool.query<{ version: string }>('SELECT version()')).rows[0]?.version.split(' on ')[0]}`,
  );
  const keys = generateKeyPairSync('rsa', { modulusLength: 2048 });
  publicKey = keys.publicKey;
  keyPath = join(directory, 'test-only-key.pem');
  await writeFile(keyPath, keys.privateKey.export({ type: 'pkcs8', format: 'pem' }), {
    mode: 0o600,
  });
});
beforeEach(async () => {
  await pool.query('TRUNCATE installations,users,oauth_attempts CASCADE');
  await synchronizeRepository(pool, { ...tenant, fullName: 'fixture/repo', public: true });
  await setRepositoryEnabled(pool, tenant, true);
});
afterAll(async () => {
  await Promise.all([pool?.end(), other?.end()]);
  await postgres?.stop();
  if (directory) await rm(directory, { recursive: true, force: true });
});
function prBody(headSha = identity.headSha, updatedAt = '2026-10-04T00:00:00Z') {
  return Buffer.from(
    JSON.stringify({
      action: 'synchronize',
      installation: { id: tenant.installationId },
      repository: { id: tenant.repositoryId, full_name: 'fixture/repo', private: false },
      pull_request: {
        number: identity.prNumber,
        updated_at: updatedAt,
        base: { sha: identity.baseSha },
        head: { sha: headSha },
      },
    }),
  );
}

describe('S1 核心集成', () => {
  it('同一自动键只入队一次，事务中途失败不留下孤立 Run', async () => {
    const results = await Promise.all([
      enqueueReview(pool, identity, snapshot),
      enqueueReview(other, identity, snapshot),
    ]);
    expect(results[0]?.runId).toBe(results[1]?.runId);
    expect(results.filter((result) => result.created)).toHaveLength(1);
    await pool.query(
      "CREATE FUNCTION reject_test_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END; $$",
    );
    await pool.query(
      'CREATE TRIGGER reject_test_job BEFORE INSERT ON jobs FOR EACH ROW EXECUTE FUNCTION reject_test_job()',
    );
    try {
      const changed = reviewIdentity({ generation: 1 });
      await expect(
        enqueueReview(pool, changed, runSnapshot({ identity: changed })),
      ).rejects.toThrow('injected failure');
      expect((await pool.query('SELECT * FROM review_runs')).rowCount).toBe(1);
    } finally {
      await pool.query('DROP TRIGGER reject_test_job ON jobs');
      await pool.query('DROP FUNCTION reject_test_job()');
    }
  });
  it('并发 Worker 不共享有效租约，过期令牌和错误 Run 不能完成或重试', async () => {
    await enqueueReview(pool, identity, snapshot);
    const leases = await Promise.all([
      claimJob(pool, 'one', new Date()),
      claimJob(other, 'two', new Date()),
    ]);
    expect(leases.filter(Boolean)).toHaveLength(1);
    const first = leases.find((lease) => lease !== null)!;
    await pool.query("UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second'");
    const replacement = (await claimJob(other, 'three', new Date()))!;
    expect(replacement.leaseToken).not.toBe(first.leaseToken);
    await expect(completeJob(pool, first)).rejects.toThrow('Lease lost');
    await expect(heartbeatJob(pool, first)).rejects.toThrow('Lease lost');
    await expect(
      failJob(
        pool,
        { ...replacement, runId: '00000000-0000-0000-0000-000000000000' },
        'TEST_FAILURE',
      ),
    ).rejects.toThrow('Lease lost');
    await completeJob(other, replacement);
  });
  it('数据库真的停止、重启后，仍能重新领取持久化任务', async () => {
    const pending = await enqueueReview(pool, identity, snapshot);
    await Promise.all([pool.end(), other.end()]);
    await postgres.stop();
    await postgres.start();
    pool = new Pool({ connectionString: databaseUrl });
    other = new Pool({ connectionString: databaseUrl });
    expect((await claimJob(pool, 'restarted', new Date()))?.runId).toBe(pending.runId);
  });
  it('撤销安装与私有化会取消任务，恢复公开也不会自动启用', async () => {
    await enqueueReview(pool, identity, snapshot);
    const lease = (await claimJob(pool, 'worker', new Date()))!;
    await acceptDelivery(
      pool,
      'private-change',
      'repository',
      Buffer.from(
        JSON.stringify({
          action: 'privatized',
          installation: { id: tenant.installationId },
          repository: { id: tenant.repositoryId, full_name: 'fixture/repo', private: true },
        }),
      ),
    );
    await expect(completeJob(pool, lease)).rejects.toThrow('Lease lost');
    expect((await pool.query('SELECT enabled FROM repositories')).rows[0]?.enabled).toBe(false);
    await synchronizeRepository(pool, { ...tenant, fullName: 'fixture/repo', public: true });
    expect((await pool.query('SELECT enabled FROM repositories')).rows[0]?.enabled).toBe(false);
    await setRepositoryEnabled(pool, tenant, true);
    await revokeInstallation(pool, tenant.installationId);
    await expect(enqueueReview(pool, identity, snapshot)).rejects.toThrow();
  });
  it('OAuth state 只能消费一次，会话过期后失效', async () => {
    await saveOAuthAttempt(pool, 'test-state', 'test-verifier');
    expect(await consumeOAuthAttempt(pool, 'test-state')).toBe('test-verifier');
    await expect(consumeOAuthAttempt(pool, 'test-state')).rejects.toThrow();
    const token = await createSession(
      pool,
      { id: 42, login: 'fixture-user' },
      'encrypted-test-token',
      60,
    );
    expect(await readSession(pool, token)).toMatchObject({ userId: 42 });
    expect((await pool.query('SELECT token_hash FROM sessions')).rows[0]?.token_hash).not.toBe(
      token,
    );
    await pool.query("UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second'");
    expect(await readSession(pool, token)).toBeNull();
  });
  it('重复 delivery 不重入队，乱序事件不会恢复旧 SHA，无关事件不审查', async () => {
    const current = await acceptDelivery(
      pool,
      'delivery-new',
      'pull_request',
      prBody('c'.repeat(40), '2026-10-04T00:00:02Z'),
    );
    expect(current.runId).toBeTruthy();
    expect(
      (
        await acceptDelivery(
          pool,
          'delivery-new',
          'pull_request',
          prBody('c'.repeat(40), '2026-10-04T00:00:02Z'),
        )
      ).accepted,
    ).toBe(false);
    await acceptDelivery(
      pool,
      'delivery-old',
      'pull_request',
      prBody(identity.headSha, '2026-10-04T00:00:01Z'),
    );
    await acceptDelivery(pool, 'delivery-push', 'push', Buffer.from('{}'));
    expect((await pool.query('SELECT * FROM jobs')).rowCount).toBe(1);
    expect((await pool.query('SELECT head_sha FROM pull_requests')).rows[0]?.head_sha).toBe(
      'c'.repeat(40),
    );
  });
  it.each([
    [false, true],
    [true, true],
    [false, false],
  ])('基础 Check 会对账；旧快照不发布（已有 Check=%s，当前 SHA=%s）', async (existing, current) => {
    const run = await enqueueReview(pool, identity, snapshot),
      lease = (await claimJob(pool, 'worker', new Date()))!;
    const writes: Array<{ method: string; body: Record<string, unknown> }> = [];
    let check: Record<string, unknown> | null = existing
      ? { id: 99, external_id: `reviewpilot:${run.runId}`, name: CONNECTION_CHECK_NAME }
      : null;
    const fakeFetch: typeof fetch = async (input, init) => {
      const url = new URL(String(input)),
        path = url.pathname;
      if (path === `/app/installations/${tenant.installationId}`) {
        const jwt = new Headers(init?.headers).get('authorization')!.slice(7),
          [header, payload, signature] = jwt.split('.');
        expect(
          verify(
            'RSA-SHA256',
            Buffer.from(`${header}.${payload}`),
            publicKey,
            Buffer.from(signature!, 'base64url'),
          ),
        ).toBe(true);
        return Response.json({ id: tenant.installationId, suspended_at: null });
      }
      if (path.endsWith('/access_tokens')) {
        expect(JSON.parse(String(init?.body))).toMatchObject({
          repository_ids: [tenant.repositoryId],
          permissions: { contents: 'read', pull_requests: 'read', checks: 'write' },
        });
        return Response.json({
          token: 'test-only-installation-token',
          expires_at: '2026-10-04T01:00:00Z',
        });
      }
      if (path === '/repos/fixture/repo')
        return Response.json({
          id: tenant.repositoryId,
          full_name: 'fixture/repo',
          private: false,
        });
      if (path.endsWith(`/pulls/${identity.prNumber}`))
        return Response.json({
          number: identity.prNumber,
          state: 'open',
          base: { sha: identity.baseSha },
          head: { sha: current ? identity.headSha : 'c'.repeat(40) },
        });
      if (path.includes('/commits/')) return Response.json({ check_runs: check ? [check] : [] });
      if (path.includes('/check-runs') && ['POST', 'PATCH'].includes(init?.method ?? '')) {
        const body: Record<string, unknown> = JSON.parse(String(init?.body));
        writes.push({ method: init!.method!, body });
        check = { ...check, ...body, id: 99 };
        return Response.json(check);
      }
      throw new Error(`Unexpected fake endpoint: ${path}`);
    };
    await processConnectionJob(
      pool,
      new GitHubClient({ appId: 99, privateKeyPath: keyPath }, fakeFetch),
      lease,
      'http://localhost:3000',
    );
    expect(writes.filter((write) => write.method === 'POST')).toHaveLength(
      current && !existing ? 1 : 0,
    );
    if (current)
      expect(writes.at(-1)?.body).toMatchObject({ status: 'completed', conclusion: 'neutral' });
    else expect(writes).toHaveLength(0);
    expect((await pool.query('SELECT status FROM review_runs')).rows[0]?.status).toBe(
      current ? 'Completed' : 'Stale',
    );
    expect((await pool.query('SELECT * FROM stage_checkpoints')).rowCount).toBe(current ? 1 : 0);
  });
});
