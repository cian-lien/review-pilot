import type { Pool, PoolClient } from 'pg';
import type { TenantRef } from '@reviewpilot/domain';
import { transaction, type Database } from './database';
export async function assertEnabledRepository(db: Database, tenant: TenantRef): Promise<void> {
  // Every repository mutation locks installation first, then repository.
  const installation = await db.query(
    'SELECT id FROM installations WHERE id=$1 AND active FOR UPDATE',
    [tenant.installationId],
  );
  if (!installation.rowCount) throw new Error('Installation not authorized');
  const repository = await db.query(
    'SELECT id FROM repositories WHERE installation_id=$1 AND id=$2 AND public AND enabled FOR UPDATE',
    [tenant.installationId, tenant.repositoryId],
  );
  if (!repository.rowCount) throw new Error('Repository not enabled or authorized');
}
export async function cancelRepositoryWork(client: PoolClient, tenant: TenantRef): Promise<void> {
  await client.query(
    "UPDATE jobs j SET status='cancelled',lease_token=NULL,lease_expires_at=NULL FROM review_runs r WHERE j.run_id=r.id AND r.installation_id=$1 AND r.repository_id=$2 AND j.status IN('queued','running')",
    [tenant.installationId, tenant.repositoryId],
  );
  await client.query(
    "UPDATE review_runs SET status='Cancelled',updated_at=clock_timestamp() WHERE installation_id=$1 AND repository_id=$2 AND status NOT IN('Completed','Partial','Stale','Cancelled','Failed')",
    [tenant.installationId, tenant.repositoryId],
  );
}
export async function synchronizeRepositoryInTransaction(
  client: PoolClient,
  repository: TenantRef & { fullName: string; public: boolean },
): Promise<void> {
  await client.query('INSERT INTO installations(id) VALUES($1) ON CONFLICT DO NOTHING', [
    repository.installationId,
  ]);
  await client.query('SELECT id FROM installations WHERE id=$1 FOR UPDATE', [
    repository.installationId,
  ]);
  await client.query(
    `INSERT INTO repositories(installation_id,id,full_name,public) VALUES($1,$2,$3,$4) ON CONFLICT(installation_id,id) DO UPDATE SET full_name=EXCLUDED.full_name,public=EXCLUDED.public,enabled=CASE WHEN EXCLUDED.public THEN repositories.enabled ELSE false END,auth_revision=repositories.auth_revision+1`,
    [repository.installationId, repository.repositoryId, repository.fullName, repository.public],
  );
  if (!repository.public) await cancelRepositoryWork(client, repository);
}
export async function synchronizeRepository(
  pool: Pool,
  repository: TenantRef & { fullName: string; public: boolean },
): Promise<void> {
  await transaction(pool, (client) => synchronizeRepositoryInTransaction(client, repository));
}
export async function setRepositoryEnabled(
  pool: Pool,
  tenant: TenantRef,
  enabled: boolean,
): Promise<void> {
  await transaction(pool, async (client) => {
    await client.query('SELECT id FROM installations WHERE id=$1 FOR UPDATE', [
      tenant.installationId,
    ]);
    const result = await client.query(
      'UPDATE repositories r SET enabled=$3,auth_revision=auth_revision+1 FROM installations i WHERE i.id=r.installation_id AND r.installation_id=$1 AND r.id=$2 AND(NOT $3 OR(r.public AND i.active)) RETURNING r.id',
      [tenant.installationId, tenant.repositoryId, enabled],
    );
    if (!result.rowCount) throw new Error('Repository cannot be enabled');
    if (!enabled) await cancelRepositoryWork(client, tenant);
  });
}
export async function revokeInstallationInTransaction(
  client: PoolClient,
  installationId: number,
): Promise<void> {
  await client.query(
    'UPDATE installations SET active=false,updated_at=clock_timestamp() WHERE id=$1',
    [installationId],
  );
  const repositories = await client.query<{ id: string }>(
    'UPDATE repositories SET enabled=false,auth_revision=auth_revision+1 WHERE installation_id=$1 RETURNING id',
    [installationId],
  );
  for (const row of repositories.rows)
    await cancelRepositoryWork(client, { installationId, repositoryId: Number(row.id) });
}
export async function revokeInstallation(pool: Pool, installationId: number): Promise<void> {
  await transaction(pool, (client) => revokeInstallationInTransaction(client, installationId));
}
