import {
  validateReview,
  type ReviewContextPage,
  type ReviewCandidate,
} from '@reviewpilot/contracts';
import type { PipelineInput } from './types';
import { stageInputHash, saveRecoverableStage } from './context';
import { modelSchema } from './schema';
import { parseDiff, validateDiffLocation } from '@reviewpilot/repository';
export class PipelineStop extends Error {
  constructor(
    readonly kind: 'blocked' | 'failed',
    readonly reasonCode: string,
  ) {
    super(reasonCode);
  }
}
export function contextInput(page: ReviewContextPage, candidates?: ReviewCandidate[]): string {
  // Source and candidates remain JSON data below the gateway's trusted system boundary.
  return JSON.stringify({
    files: page.files.map((f) => ({
      path: f.path,
      patch: f.patch,
      base: adjacentContext(f.base.text, f.patch, false),
      head: adjacentContext(f.head.text, f.patch, true),
    })),
    evidence: page.evidence,
    candidates,
    limitations: page.coverage.limitations,
  });
}
function adjacentContext(
  text: string | null,
  patch: string | null,
  head: boolean,
): { startLine: number; text: string }[] {
  if (text === null || patch === null) return [];
  const lines = text.replaceAll('\r\n', '\n').split('\n'),
    windows: { startLine: number; text: string }[] = [];
  for (const match of patch.matchAll(/@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/g)) {
    const start = Number(match[head ? 3 : 1]),
      count = Number(match[head ? 4 : 2] ?? 1),
      from = Math.max(0, start - 9),
      to = Math.min(lines.length, start + count + 7);
    windows.push({ startLine: from + 1, text: lines.slice(from, to).join('\n') });
  }
  return windows;
}
export function selectModelContext(page: ReviewContextPage): ReviewContextPage {
  const selected: ReviewContextPage = { ...page, files: [], evidence: [] };
  const diff = parseDiff(page.files);
  for (const file of page.files) {
    const evidence = page.evidence.filter(
      (e) => e.ref.path === file.path && validateDiffLocation(e.ref, diff),
    );
    const next = {
      ...selected,
      files: [...selected.files, file],
      evidence: [...selected.evidence, ...evidence],
    };
    // Reserve space for schema, system framing and bounded verification candidates.
    if (Buffer.byteLength(contextInput(next)) <= 5000) {
      selected.files = next.files;
      selected.evidence = next.evidence;
    }
  }
  if (selected.files.length !== page.files.length) {
    if (page.coverage.limitations.length >= 16)
      throw new PipelineStop('failed', 'COVERAGE_LIMITATIONS_FULL');
    selected.coverage = {
      ...page.coverage,
      complete: false,
      coveredChangedLines: selected.files.reduce((sum, f) => sum + f.additions + f.deletions, 0),
      limitations: [...page.coverage.limitations, 'MODEL_PROMPT_CONTEXT_LIMIT'],
    };
  }
  return selected;
}
export async function generate(input: PipelineInput, page: ReviewContextPage) {
  const key = stageInputHash(input.lease, page.contextHash!, 'candidates');
  const saved = await input.client.readStage({
    lease: input.lease,
    stage: 'candidates',
    stageInputHash: key,
  });
  if (saved.found && saved.record?.payload.candidates && saved.record.callId)
    return { candidates: saved.record.payload.candidates.candidates, callId: saved.record.callId };
  const output = await input.gateway.call({
    lease: input.lease,
    purpose: 'generate',
    ordinal: 1,
    schema: modelSchema('ReviewCandidates'),
    validate: (value) => validateReview('ReviewCandidates', value),
    input: contextInput(page),
    signal: input.signal,
  });
  if (output.kind !== 'succeeded')
    throw new PipelineStop(
      output.kind === 'needs-reconciliation' ? 'blocked' : 'failed',
      output.reasonCode,
    );
  await saveRecoverableStage(input.client, {
    lease: input.lease,
    stage: 'candidates',
    stageInputHash: key,
    callId: output.callId,
    payload: { context: null, candidates: output.value, verification: null },
  });
  return { candidates: output.value.candidates, callId: output.callId };
}
