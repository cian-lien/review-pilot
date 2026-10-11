import { metrics } from '@opentelemetry/api';
type Stage = 'context' | 'candidates' | 'verification' | 'finalize';
// Metric dimensions deliberately exclude identifiers, source and model-generated text.
export async function reviewStage<T>(stage: Stage, work: () => Promise<T>): Promise<T> {
  const started = performance.now();
  try {
    return await work();
  } finally {
    metrics
      .getMeter('reviewpilot')
      .createHistogram('review.stage.duration', { unit: 'ms' })
      .record(performance.now() - started, { stage });
  }
}
export function reviewOutcome(kind: 'failed' | 'blocked'): void {
  metrics
    .getMeter('reviewpilot')
    .createCounter('review.' + kind)
    .add(1);
}
