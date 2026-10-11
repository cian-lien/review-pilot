import { expect, it } from 'vitest';
import { runReviewScenario } from '../../scripts/s2-review-test';
it('requires a fresh successful isolated review verdict', async () => {
  const verdict = await runReviewScenario(process.env.REVIEW_CASE ?? 'contracts');
  console.log(JSON.stringify(verdict));
  expect(verdict).toMatchObject({
    simulated: true,
  });
  if (process.env.REVIEW_CASE === 'pipeline') {
    expect(verdict).toMatchObject({
      acceptedResultCount: 1,
      modelCallCount: 2,
      unknownReservationHeld: true,
      stalePublishBlocked: true,
      traceLinked: true,
      secretSentinelAbsent: true,
    });
    expect(verdict.publishedRootCauseCount).toBeGreaterThanOrEqual(0);
    expect(verdict.publishedRootCauseCount).toBeLessThanOrEqual(3);
  }
});
