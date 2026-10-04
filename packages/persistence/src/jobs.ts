import { randomUUID } from 'node:crypto';
import { isDeepStrictEqual } from 'node:util';
import type { Pool, PoolClient } from 'pg';
import {
  canTransitionRun,
  type ReviewIdentity,
  type RunSnapshot,
  type RunStatus,
  type StageCheckpoint,
} from '@reviewpilot/domain';
import { transaction } from './database';
import { assertEnabledRepository } from './repositories';
export interface JobLease {
  jobId: string;
  runId: string;
  leaseToken: string;
  expiresAt: Date;
  attempts: number;
}
export async function enqueueReview(
  pool: Pool,
  identity: ReviewIdentity,
  snapshot: RunSnapshot,
): Promise<{ runId: string; created: boolean }> {
  return transaction(pool, (client) => enqueueReviewInTransaction(client, identity, snapshot));
}
export async function enqueueReviewInTransaction(
  client: PoolClient,
  identity: ReviewIdentity,
  snapshot: RunSnapshot,
): Promise<{ runId: string; created: boolean }> {
  if (!isDeepStrictEqual(identity, snapshot.identity))
    throw new Error('Snapshot identity mismatch');
  if (
    !Number.isSafeInteger(identity.prNumber) ||
    identity.prNumber <= 0 ||
    !Number.isSafeInteger(identity.generation) ||
    identity.generation < 0 ||
    !/^[a-f0-9]{40,64}$/.test(identity.headSha) ||
    !/^[a-f0-9]{40,64}$/.test(identity.baseSha)
  )
    throw new Error('Invalid review identity');
  await assertEnabledRepository(client, identity.tenant);
  const inserted = await client.query<{ id: string }>(
    `INSERT INTO review_runs(id,installation_id,repository_id,pr_number,base_sha,head_sha,pipeline_version,generation,snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(installation_id,repository_id,pr_number,head_sha,pipeline_version,generation) DO NOTHING RETURNING id`,
    [
      randomUUID(),
      identity.tenant.installationId,
      identity.tenant.repositoryId,
      identity.prNumber,
      identity.baseSha,
      identity.headSha,
      identity.pipelineVersion,
      identity.generation,
      JSON.stringify(snapshot),
    ],
  );
  const id = inserted.rows[0]?.id;
  if (id) {
    await client.query('INSERT INTO jobs(id,run_id) VALUES($1,$2)', [randomUUID(), id]);
    return { runId: id, created: true };
  }
  const existing = await client.query<{ id: string }>(
    'SELECT id FROM review_runs WHERE installation_id=$1 AND repository_id=$2 AND pr_number=$3 AND head_sha=$4 AND pipeline_version=$5 AND generation=$6',
    [
      identity.tenant.installationId,
      identity.tenant.repositoryId,
      identity.prNumber,
      identity.headSha,
      identity.pipelineVersion,
      identity.generation,
    ],
  );
  if (!existing.rows[0]) throw new Error('Duplicate run could not be reconciled');
  return { runId: existing.rows[0].id, created: false };
}
// PostgreSQL's clock, not a worker clock, is authoritative for leases.
export async function claimJob(
  pool: Pool,
  workerId: string,
  _now: Date,
  leaseSeconds = 60,
  kind: 'review' | 'evolution' = 'review',
): Promise<JobLease | null> {
  if (!workerId || leaseSeconds <= 0 || leaseSeconds > 600) throw new Error('Invalid lease');
  return transaction(pool, async (client) => {
    const exhausted = await client.query<{ run_id: string }>(
      "UPDATE jobs SET status='failed',lease_token=NULL,lease_expires_at=NULL,last_error_code='LEASE_EXHAUSTED' WHERE kind=$1 AND attempts>=max_attempts AND(status='queued' OR(status='running' AND lease_expires_at<=clock_timestamp())) RETURNING run_id",
      [kind],
    );
    for (const row of exhausted.rows)
      await client.query(
        "UPDATE review_runs SET status='Failed',updated_at=clock_timestamp() WHERE id=$1 AND status NOT IN('Completed','Partial','Stale','Cancelled','Failed')",
        [row.run_id],
      );
    const result = await client.query<{
      id: string;
      run_id: string;
      lease_token: string;
      lease_expires_at: Date;
      attempts: number;
    }>(
      `WITH candidate AS(SELECT j.id FROM jobs j JOIN review_runs r ON r.id=j.run_id JOIN repositories repo ON repo.installation_id=r.installation_id AND repo.id=r.repository_id JOIN installations i ON i.id=repo.installation_id WHERE j.kind=$1 AND j.attempts<j.max_attempts AND repo.public AND repo.enabled AND i.active AND((j.status='queued' AND j.available_at<=clock_timestamp()) OR(j.status='running' AND j.lease_expires_at<=clock_timestamp())) ORDER BY j.available_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1) UPDATE jobs j SET status='running',worker_id=$2,lease_token=$3,lease_expires_at=clock_timestamp()+$4*interval '1 second',attempts=j.attempts+1 FROM candidate c WHERE j.id=c.id RETURNING j.id,j.run_id,j.lease_token,j.lease_expires_at,j.attempts`,
      [kind, workerId, randomUUID(), leaseSeconds],
    );
    const job = result.rows[0];
    return job
      ? {
          jobId: job.id,
          runId: job.run_id,
          leaseToken: job.lease_token,
          expiresAt: job.lease_expires_at,
          attempts: job.attempts,
        }
      : null;
  });
}
export async function heartbeatJob(pool: Pool, lease: JobLease, leaseSeconds = 60): Promise<void> {
  if (leaseSeconds <= 0 || leaseSeconds > 600) throw new Error('Invalid lease');
  const result = await pool.query(
    "UPDATE jobs SET lease_expires_at=clock_timestamp()+$4*interval '1 second' WHERE id=$1 AND run_id=$2 AND lease_token=$3 AND status='running' AND lease_expires_at>clock_timestamp() RETURNING id",
    [lease.jobId, lease.runId, lease.leaseToken, leaseSeconds],
  );
  if (!result.rowCount) throw new Error('Lease lost');
}
export async function completeJob(
  pool: Pool,
  lease: JobLease,
  target: 'Completed' | 'Stale' | 'Partial' = 'Completed',
): Promise<void> {
  await transaction(pool, async (client) => {
    const result = await client.query(
      "UPDATE jobs SET status='completed',lease_token=NULL,lease_expires_at=NULL WHERE id=$1 AND run_id=$2 AND lease_token=$3 AND status='running' AND lease_expires_at>clock_timestamp() RETURNING run_id",
      [lease.jobId, lease.runId, lease.leaseToken],
    );
    if (!result.rowCount) throw new Error('Lease lost');
    const run = await client.query<{ status: RunStatus }>(
      'SELECT status FROM review_runs WHERE id=$1 FOR UPDATE',
      [lease.runId],
    );
    if (!run.rows[0] || !canTransitionRun(run.rows[0].status, target))
      throw new Error('Invalid run transition');
    await client.query(
      'UPDATE review_runs SET status=$2,updated_at=clock_timestamp() WHERE id=$1',
      [lease.runId, target],
    );
  });
}
export async function failJob(
  pool: Pool,
  lease: JobLease,
  reasonCode: string,
  retrySeconds = 30,
): Promise<void> {
  if (!/^[A-Z][A-Z0-9_]{0,63}$/.test(reasonCode) || retrySeconds < 0 || retrySeconds > 3600)
    throw new Error('Invalid retry');
  await transaction(pool, async (client) => {
    const result = await client.query<{ status: string }>(
      `UPDATE jobs SET status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'queued' END,available_at=clock_timestamp()+$4*interval '1 second',lease_token=NULL,lease_expires_at=NULL,last_error_code=$3 WHERE id=$1 AND lease_token=$2 AND run_id=$5 AND status='running' AND lease_expires_at>clock_timestamp() RETURNING status`,
      [lease.jobId, lease.leaseToken, reasonCode, retrySeconds, lease.runId],
    );
    if (!result.rowCount) throw new Error('Lease lost');
    if (result.rows[0]?.status === 'failed')
      await client.query(
        "UPDATE review_runs SET status='Failed',updated_at=clock_timestamp() WHERE id=$1",
        [lease.runId],
      );
  });
}
export async function saveCheckpoint(
  pool: Pool,
  lease: JobLease,
  checkpoint: StageCheckpoint,
): Promise<void> {
  if (checkpoint.runId !== lease.runId) throw new Error('Checkpoint run mismatch');
  await transaction(pool, async (client) => {
    const result = await client.query(
      "SELECT id FROM jobs WHERE id=$1 AND run_id=$2 AND lease_token=$3 AND status='running' AND lease_expires_at>clock_timestamp() FOR UPDATE",
      [lease.jobId, lease.runId, lease.leaseToken],
    );
    if (!result.rowCount) throw new Error('Lease lost');
    await client.query(
      'INSERT INTO stage_checkpoints(run_id,stage,artifact_ref,input_hash,status) VALUES($1,$2,$3,$4,$5) ON CONFLICT(run_id,stage) DO UPDATE SET artifact_ref=EXCLUDED.artifact_ref,input_hash=EXCLUDED.input_hash,status=EXCLUDED.status',
      [
        checkpoint.runId,
        checkpoint.stage,
        checkpoint.artifactRef,
        checkpoint.inputHash,
        checkpoint.status,
      ],
    );
  });
}
