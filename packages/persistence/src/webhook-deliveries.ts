import { createHash } from 'node:crypto';
import { z } from 'zod';
import type { Pool } from 'pg';
import type { ReviewIdentity, RunSnapshot } from '@reviewpilot/domain';
import { transaction } from './database';
import { enqueueReviewInTransaction } from './jobs';
import {
  cancelRepositoryWork,
  revokeInstallationInTransaction,
  synchronizeRepositoryInTransaction,
} from './repositories';

const id = z.number().int().positive().safe();
const repositorySchema = z.object({
  id,
  full_name: z.string().regex(/^[^/]+\/[^/]+$/),
  private: z.boolean(),
});
const installationSchema = z.object({ id });
const prSchema = z.object({
  action: z.string(),
  installation: installationSchema,
  repository: repositorySchema,
  pull_request: z.object({
    number: id,
    updated_at: z.string().datetime(),
    base: z.object({ sha: z.string().regex(/^[a-f0-9]{40,64}$/) }),
    head: z.object({ sha: z.string().regex(/^[a-f0-9]{40,64}$/) }),
  }),
});

export async function acceptDelivery(
  pool: Pool,
  deliveryId: string,
  event: string,
  rawBody: Buffer,
  pipelineVersion = 'foundation-v1',
): Promise<{ accepted: boolean; runId?: string }> {
  if (!/^[a-zA-Z0-9-]{1,100}$/.test(deliveryId) || !/^[a-z_]{1,64}$/.test(event))
    throw new Error('Invalid delivery metadata');
  const payload: unknown = JSON.parse(rawBody.toString('utf8'));
  const payloadHash = createHash('sha256').update(rawBody).digest('hex');
  return transaction(pool, async (client) => {
    const delivery = await client.query(
      'INSERT INTO webhook_deliveries(delivery_id,event,payload_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING RETURNING delivery_id',
      [deliveryId, event, payloadHash],
    );
    if (!delivery.rowCount) {
      const existing = await client.query<{ payload_hash: string; event: string }>(
        'SELECT payload_hash,event FROM webhook_deliveries WHERE delivery_id=$1',
        [deliveryId],
      );
      if (existing.rows[0]?.payload_hash !== payloadHash || existing.rows[0]?.event !== event)
        throw new Error('Delivery identity conflict');
      return { accepted: false };
    }
    if (event === 'installation') {
      const input = z
        .object({
          action: z.string(),
          installation: installationSchema,
          repositories: z.array(repositorySchema).optional(),
        })
        .parse(payload);
      if (['deleted', 'suspend'].includes(input.action))
        await revokeInstallationInTransaction(client, input.installation.id);
      else if (input.action === 'unsuspend')
        await client.query(
          'UPDATE installations SET active=true,updated_at=clock_timestamp() WHERE id=$1',
          [input.installation.id],
        );
      else if (input.action === 'created')
        for (const repository of input.repositories ?? [])
          await synchronizeRepositoryInTransaction(client, {
            installationId: input.installation.id,
            repositoryId: repository.id,
            fullName: repository.full_name,
            public: !repository.private,
          });
      return { accepted: true };
    }
    if (event === 'installation_repositories') {
      const input = z
        .object({
          installation: installationSchema,
          repositories_added: z.array(repositorySchema).default([]),
          repositories_removed: z.array(z.object({ id })).default([]),
        })
        .parse(payload);
      for (const repository of input.repositories_added)
        await synchronizeRepositoryInTransaction(client, {
          installationId: input.installation.id,
          repositoryId: repository.id,
          fullName: repository.full_name,
          public: !repository.private,
        });
      await client.query('SELECT id FROM installations WHERE id=$1 FOR UPDATE', [
        input.installation.id,
      ]);
      for (const repository of input.repositories_removed) {
        await client.query(
          'UPDATE repositories SET enabled=false,auth_revision=auth_revision+1 WHERE installation_id=$1 AND id=$2',
          [input.installation.id, repository.id],
        );
        await cancelRepositoryWork(client, {
          installationId: input.installation.id,
          repositoryId: repository.id,
        });
      }
      return { accepted: true };
    }
    if (event === 'repository') {
      const input = z
        .object({
          action: z.string(),
          installation: installationSchema,
          repository: repositorySchema,
        })
        .parse(payload);
      if (['privatized', 'publicized', 'renamed'].includes(input.action))
        await synchronizeRepositoryInTransaction(client, {
          installationId: input.installation.id,
          repositoryId: input.repository.id,
          fullName: input.repository.full_name,
          public: !input.repository.private,
        });
      if (input.action === 'deleted') {
        await client.query('SELECT id FROM installations WHERE id=$1 FOR UPDATE', [
          input.installation.id,
        ]);
        await client.query(
          'UPDATE repositories SET enabled=false,auth_revision=auth_revision+1 WHERE installation_id=$1 AND id=$2',
          [input.installation.id, input.repository.id],
        );
        await cancelRepositoryWork(client, {
          installationId: input.installation.id,
          repositoryId: input.repository.id,
        });
      }
      return { accepted: true };
    }
    if (event !== 'pull_request') return { accepted: true };
    const input = prSchema.parse(payload),
      tenant = { installationId: input.installation.id, repositoryId: input.repository.id };
    if (input.repository.private) {
      await synchronizeRepositoryInTransaction(client, {
        ...tenant,
        fullName: input.repository.full_name,
        public: false,
      });
      return { accepted: true };
    }
    if (!['opened', 'synchronize', 'reopened', 'ready_for_review'].includes(input.action))
      return { accepted: true };
    // Lock installation before repository, as in all enable/revoke operations.
    const installation = await client.query(
      'SELECT id FROM installations WHERE id=$1 AND active FOR UPDATE',
      [tenant.installationId],
    );
    if (!installation.rowCount) return { accepted: true };
    const repository = await client.query<{ config_version: number }>(
      'SELECT config_version FROM repositories WHERE installation_id=$1 AND id=$2 AND enabled AND public FOR UPDATE',
      [tenant.installationId, tenant.repositoryId],
    );
    if (!repository.rows[0]) return { accepted: true };
    const pr = input.pull_request;
    const current = await client.query<{ remote_updated_at: Date }>(
      'SELECT remote_updated_at FROM pull_requests WHERE installation_id=$1 AND repository_id=$2 AND number=$3',
      [tenant.installationId, tenant.repositoryId, pr.number],
    );
    if (current.rows[0] && current.rows[0].remote_updated_at.getTime() > Date.parse(pr.updated_at))
      return { accepted: true };
    await client.query(
      `INSERT INTO pull_requests(installation_id,repository_id,number,base_sha,head_sha,remote_updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(installation_id,repository_id,number) DO UPDATE SET base_sha=EXCLUDED.base_sha,head_sha=EXCLUDED.head_sha,remote_updated_at=EXCLUDED.remote_updated_at WHERE pull_requests.remote_updated_at<EXCLUDED.remote_updated_at`,
      [
        tenant.installationId,
        tenant.repositoryId,
        pr.number,
        pr.base.sha,
        pr.head.sha,
        pr.updated_at,
      ],
    );
    // Equal-second deliveries cannot be reliably ordered: the worker re-fetches the current PR before publishing.
    const previous = await client.query<{ generation: number; status: string }>(
      'SELECT generation,status FROM review_runs WHERE installation_id=$1 AND repository_id=$2 AND pr_number=$3 AND head_sha=$4 AND pipeline_version=$5 ORDER BY generation DESC LIMIT 1',
      [tenant.installationId, tenant.repositoryId, pr.number, pr.head.sha, pipelineVersion],
    );
    const last = previous.rows[0],
      generation = last
        ? last.generation + (['Cancelled', 'Stale'].includes(last.status) ? 1 : 0)
        : 0;
    const identity: ReviewIdentity = {
      tenant,
      prNumber: pr.number,
      baseSha: pr.base.sha,
      headSha: pr.head.sha,
      pipelineVersion,
      generation,
    };
    // No model or trusted rules are loaded during S1. Empty SHA is explicit, not a fabricated default-branch rule snapshot.
    const snapshot: RunSnapshot = {
      identity,
      modelConfigVersion: 'none',
      repositoryConfigVersion: String(repository.rows[0].config_version),
      memorySetVersion: 'empty',
      trustedRulesSha: '',
    };
    const run = await enqueueReviewInTransaction(client, identity, snapshot);
    if (run.created)
      await client.query(
        "UPDATE jobs SET available_at=clock_timestamp()+interval '2 seconds' WHERE run_id=$1",
        [run.runId],
      );
    return { accepted: true, runId: run.runId };
  });
}
