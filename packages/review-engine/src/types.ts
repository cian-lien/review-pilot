import type {
  components,
  ReviewLease,
  ReviewResult,
  ReviewTaskEnvelope,
} from '@reviewpilot/contracts';
import type { BudgetClient, ModelGateway } from '@reviewpilot/model-gateway';
type S = components['schemas'];
export interface ReviewInternalClient extends BudgetClient {
  claim(envelope: ReviewTaskEnvelope): Promise<S['ClaimReviewResponse']>;
  heartbeat(lease: ReviewLease): Promise<void>;
  context(request: S['ReadReviewContextRequest']): Promise<S['ReviewContextPage']>;
  readStage(request: S['ReadReviewStageRequest']): Promise<S['ReadReviewStageResponse']>;
  saveStage(request: S['SaveReviewStageRequest']): Promise<void>;
  commit(result: ReviewResult): Promise<S['ReviewCommitResponse']>;
  confirm(lease: ReviewLease): Promise<S['ReviewCommitResponse']>;
  block(request: S['ReviewTaskActionRequest']): Promise<void>;
  fail(request: S['ReviewTaskActionRequest']): Promise<void>;
}
export type ReviewPipelineOutcome =
  | { kind: 'completed'; result: ReviewResult }
  | { kind: 'blocked'; reasonCode: string }
  | { kind: 'failed'; reasonCode: string };
export interface PipelineInput {
  lease: ReviewLease;
  client: ReviewInternalClient;
  gateway: Pick<ModelGateway, 'call'>;
  signal: AbortSignal;
}
