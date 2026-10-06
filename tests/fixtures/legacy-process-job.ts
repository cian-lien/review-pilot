import { createHash } from 'node:crypto';
import type { Pool } from 'pg';
import type { RunSnapshot } from '@reviewpilot/domain';
import {
  GitHubClient,
  updateConnectionCheck,
  findConnectionCheck,
  GitHubRequestError,
  repositoryPath,
} from '@reviewpilot/github';
import {
  completeJob,
  heartbeatJob,
  saveCheckpoint,
  setRepositoryEnabled,
  type JobLease,
} from '@reviewpilot/persistence';

class StaleRun extends Error {}
export async function processConnectionJob(
  pool: Pool,
  api: GitHubClient,
  lease: JobLease,
  appUrl: string,
): Promise<void> {
  const result = await pool.query<{
    snapshot: RunSnapshot;
    full_name: string;
    check_id: string | null;
  }>(
    'SELECT r.snapshot,r.check_id,repo.full_name FROM review_runs r JOIN repositories repo ON repo.installation_id=r.installation_id AND repo.id=r.repository_id WHERE r.id=$1',
    [lease.runId],
  );
  const run = result.rows[0];
  if (!run || run.snapshot.modelConfigVersion !== 'none') throw new Error('Unsupported pipeline');
  const identity = run.snapshot.identity;
  await heartbeatJob(pool, lease, 90);
  const token = await api.installationToken(identity.tenant);
  const guardPermission = async () => {
    const state = await pool.query(
      `SELECT j.id FROM jobs j JOIN review_runs r ON r.id=j.run_id JOIN repositories repo ON repo.installation_id=r.installation_id AND repo.id=r.repository_id JOIN installations i ON i.id=repo.installation_id WHERE j.id=$1 AND j.run_id=$2 AND j.lease_token=$3 AND j.status='running' AND j.lease_expires_at>clock_timestamp() AND repo.enabled AND repo.public AND i.active`,
      [lease.jobId, lease.runId, lease.leaseToken],
    );
    if (!state.rowCount) throw new Error('Lease lost');
    const repository = await api.repository(token, run.full_name);
    if (repository.id !== identity.tenant.repositoryId || !repository.public) {
      await setRepositoryEnabled(pool, identity.tenant, false);
      throw new GitHubRequestError(403);
    }
  };
  const guardCurrent = async () => {
    await guardPermission();
    const pr = await api.pullRequest(token, run.full_name, identity.prNumber);
    if (pr.state !== 'open' || pr.head.sha !== identity.headSha)
      throw new StaleRun('PR snapshot no longer current');
    // Remote checks may take time; fence again immediately before the mutation.
    const leaseState = await pool.query(
      "SELECT id FROM jobs WHERE id=$1 AND run_id=$2 AND lease_token=$3 AND status='running' AND lease_expires_at>clock_timestamp()",
      [lease.jobId, lease.runId, lease.leaseToken],
    );
    if (!leaseState.rowCount) throw new Error('Lease lost');
  };
  let leaseLost = false;
  const timer = setInterval(() => {
    void heartbeatJob(pool, lease, 90).catch(() => {
      leaseLost = true;
    });
  }, 20000);
  const guard = async () => {
    if (leaseLost) throw new Error('Lease lost');
    await guardCurrent();
  };
  try {
    const checkId = await updateConnectionCheck(
      api,
      token,
      {
        repository: run.full_name,
        headSha: identity.headSha,
        runId: lease.runId,
        checkId: run.check_id ? Number(run.check_id) : null,
        appUrl,
        completed: false,
      },
      guard,
    );
    const saved = await pool.query(
      "UPDATE review_runs SET check_id=$2 WHERE id=$1 AND EXISTS(SELECT 1 FROM jobs WHERE id=$3 AND run_id=$1 AND lease_token=$4 AND status='running' AND lease_expires_at>clock_timestamp()) RETURNING id",
      [lease.runId, checkId, lease.jobId, lease.leaseToken],
    );
    if (!saved.rowCount) throw new Error('Lease lost');
    await updateConnectionCheck(
      api,
      token,
      {
        repository: run.full_name,
        headSha: identity.headSha,
        runId: lease.runId,
        checkId,
        appUrl,
        completed: true,
      },
      guard,
    );
    await saveCheckpoint(pool, lease, {
      runId: lease.runId,
      stage: 'connection',
      artifactRef: null,
      inputHash: createHash('sha256').update(JSON.stringify(run.snapshot)).digest('hex'),
      status: 'completed',
    });
    await completeJob(pool, lease);
  } catch (error) {
    if (!(error instanceof StaleRun)) throw error;
    await guardPermission();
    const existing = run.check_id
      ? Number(run.check_id)
      : await findConnectionCheck(api, token, run.full_name, identity.headSha, lease.runId);
    if (existing) {
      await guardPermission();
      await api.request(
        `/repos/${repositoryPath(run.full_name)}/check-runs/${existing}`,
        token,
        'PATCH',
        {
          status: 'completed',
          conclusion: 'cancelled',
          completed_at: new Date().toISOString(),
          output: { title: '旧快照已停止', summary: 'PR 已关闭或 Head 已变化；此检查未分析代码。' },
        },
      );
    }
    await completeJob(pool, lease, 'Stale');
  } finally {
    clearInterval(timer);
  }
}
