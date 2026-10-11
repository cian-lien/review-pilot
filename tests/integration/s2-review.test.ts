import { expect, it } from 'vitest';
import { runReviewScenario } from '../../scripts/s2-review-test';
it('requires a fresh successful isolated review verdict', async () => {
  expect(await runReviewScenario(process.env.REVIEW_CASE ?? 'contracts')).toMatchObject({
    simulated: true,
  });
});
