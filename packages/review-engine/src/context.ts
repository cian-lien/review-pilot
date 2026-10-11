import type { ReviewContextPage, ReviewLease, components } from '@reviewpilot/contracts';
import { safeHash } from '@reviewpilot/model-gateway';
import type { ReviewInternalClient } from './types';
export type Stage = components['schemas']['ReviewStageRecord']['stage'];
function normalized(value: unknown): string {
  const sort = (v: unknown): unknown =>
    Array.isArray(v)
      ? v.map(sort)
      : v && typeof v === 'object'
        ? Object.fromEntries(
            Object.entries(v)
              .sort(([a], [b]) => a.localeCompare(b, 'en'))
              .map(([k, c]) => [k, sort(c)]),
          )
        : v;
  return JSON.stringify(sort(value));
}
export async function saveRecoverableStage(
  client: ReviewInternalClient,
  request: components['schemas']['SaveReviewStageRequest'],
): Promise<void> {
  try {
    await client.saveStage(request);
  } catch {
    try {
      const saved = await client.readStage({
        lease: request.lease,
        stage: request.stage,
        stageInputHash: request.stageInputHash,
      });
      if (
        saved.found &&
        saved.record?.callId === request.callId &&
        normalized(saved.record.payload) === normalized(request.payload)
      )
        return;
    } catch {
      /* Unconfirmed model artifacts remain blocked; never regenerate from an unknown save. */
    }
    throw new Error(request.callId ? 'MODEL_ARTIFACT_UNKNOWN' : 'CONTEXT_ARTIFACT_UNKNOWN');
  }
}
export function stageInputHash(lease: ReviewLease, contextHash: string, stage: Stage): string {
  const prompt =
    stage === 'context'
      ? 'none'
      : stage === 'candidates'
        ? lease.snapshot.generationPromptVersion
        : lease.snapshot.verificationPromptVersion;
  return safeHash(
    [lease.inputHash, contextHash, stage, prompt, lease.snapshot.modelConfigVersion].join('\n'),
  );
}
export async function loadContext(
  lease: ReviewLease,
  client: ReviewInternalClient,
  signal: AbortSignal,
): Promise<ReviewContextPage> {
  const pages: ReviewContextPage[] = [],
    seen = new Set<string>();
  let cursor: string | null = null;
  for (let count = 0; count <= lease.snapshot.policy.maxFiles; count++) {
    signal.throwIfAborted();
    const page = await client.context({ lease, cursor });
    if (page.cursor !== cursor || (pages.length && page.manifestId !== pages[0]!.manifestId))
      throw new Error('CONTEXT_PAGE_MISMATCH');
    pages.push(page);
    if (page.complete) {
      if (!page.contextHash || page.nextCursor !== null) throw new Error('CONTEXT_NOT_SEALED');
      const key = stageInputHash(lease, page.contextHash, 'context');
      const saved = await client.readStage({ lease, stage: 'context', stageInputHash: key });
      // The small sealed receipt is recoverable; paged source remains owned by Go's manifest.
      if (!saved.found)
        await saveRecoverableStage(client, {
          lease,
          stage: 'context',
          stageInputHash: key,
          callId: null,
          payload: { context: page, candidates: null, verification: null },
        });
      return {
        ...page,
        files: pages.flatMap((p) => p.files),
        evidence: pages.flatMap((p) => p.evidence),
      };
    }
    if (!page.nextCursor || seen.has(page.nextCursor)) throw new Error('CONTEXT_PAGE_MISMATCH');
    seen.add(page.nextCursor);
    cursor = page.nextCursor;
  }
  throw new Error('CONTEXT_PAGE_LIMIT');
}
