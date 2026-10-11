import { Annotation, StateGraph, START, END } from '@langchain/langgraph';
import {
  validateReview,
  type ReviewContextPage,
  type ReviewCandidate,
  type VerificationDecision,
} from '@reviewpilot/contracts';
import type { PipelineInput, ReviewPipelineOutcome } from './types';
import { loadContext } from './context';
import { generate, PipelineStop, selectModelContext } from './generate';
import { verify } from './verify';
const State = Annotation.Root({
  page: Annotation<ReviewContextPage | null>(),
  candidateItems: Annotation<ReviewCandidate[]>(),
  decisions: Annotation<VerificationDecision[]>(),
  callIds: Annotation<string[]>(),
  outcome: Annotation<ReviewPipelineOutcome | null>(),
});
export async function runReviewPipeline(input: PipelineInput): Promise<ReviewPipelineOutcome> {
  const graph = new StateGraph(State)
    .addNode('context', async () => ({
      page: selectModelContext(await loadContext(input.lease, input.client, input.signal)),
    }))
    .addNode('candidates', async (state) => {
      input.signal.throwIfAborted();
      const output = await generate(input, state.page!);
      return { candidateItems: output.candidates, callIds: [output.callId] };
    })
    .addNode('verification', async (state) => {
      input.signal.throwIfAborted();
      const output = await verify(input, state.page!, state.candidateItems);
      return { decisions: output.decisions, callIds: [...state.callIds, output.callId] };
    })
    .addNode('finalize', async (state) => ({
      outcome: {
        kind: 'completed' as const,
        result: validateReview('ReviewResult', {
          kind: 'ai-review',
          lease: input.lease,
          resultId: input.lease.taskId + ':review',
          contextHash: state.page!.contextHash,
          candidates: state.candidateItems,
          decisions: state.decisions,
          coverage: state.page!.coverage,
          callIds: state.callIds,
        }),
      },
    }))
    .addEdge(START, 'context')
    .addEdge('context', 'candidates')
    .addConditionalEdges('candidates', (state) =>
      state.candidateItems.length ? 'verification' : 'finalize',
    )
    .addEdge('verification', 'finalize')
    .addEdge('finalize', END)
    .compile();
  try {
    const state = await graph.invoke(
      { page: null, candidateItems: [], decisions: [], callIds: [], outcome: null },
      { signal: input.signal, recursionLimit: 8 },
    );
    return state.outcome!;
  } catch (error) {
    if (error instanceof PipelineStop) return { kind: error.kind, reasonCode: error.reasonCode };
    if (error instanceof Error && error.message === 'MODEL_ARTIFACT_UNKNOWN')
      return { kind: 'blocked', reasonCode: error.message };
    return {
      kind: 'failed',
      reasonCode:
        error instanceof Error && /^[A-Z_]{1,64}$/.test(error.message)
          ? error.message
          : 'REVIEW_PIPELINE_FAILED',
    };
  }
}
