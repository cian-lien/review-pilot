import {
  validateReview,
  type ReviewCandidate,
  type ReviewContextPage,
} from '@reviewpilot/contracts';
import type { PipelineInput } from './types';
import { stageInputHash, saveRecoverableStage } from './context';
import { modelSchema } from './schema';
import { validateDecisions } from './validate';
import { contextInput, PipelineStop } from './generate';
export async function verify(
  input: PipelineInput,
  page: ReviewContextPage,
  candidates: ReviewCandidate[],
) {
  const key = stageInputHash(input.lease, page.contextHash!, 'verification');
  const saved = await input.client.readStage({
    lease: input.lease,
    stage: 'verification',
    stageInputHash: key,
  });
  if (saved.found && saved.record?.payload.verification && saved.record.callId)
    return {
      decisions: validateDecisions(candidates, saved.record.payload.verification.decisions, page),
      callId: saved.record.callId,
    };
  const output = await input.gateway.call({
    lease: input.lease,
    purpose: 'verify',
    ordinal: 1,
    schema: modelSchema('ReviewVerification'),
    validate: (value) => validateReview('ReviewVerification', value),
    input: contextInput(page, candidates),
    signal: input.signal,
  });
  if (output.kind !== 'succeeded')
    throw new PipelineStop(
      output.kind === 'needs-reconciliation' ? 'blocked' : 'failed',
      output.reasonCode,
    );
  const decisions = validateDecisions(candidates, output.value.decisions, page);
  await saveRecoverableStage(input.client, {
    lease: input.lease,
    stage: 'verification',
    stageInputHash: key,
    callId: output.callId,
    payload: { context: null, candidates: null, verification: { decisions } },
  });
  return { decisions, callId: output.callId };
}
