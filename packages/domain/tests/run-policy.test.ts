import { describe, expect, it } from 'vitest';
import { canTransitionRun } from '../src/run-policy';
describe('审查状态', () => {
  it('排队任务可进入索引阶段', () => {
    expect(canTransitionRun('Queued', 'Indexing')).toBe(true);
  });
  it.each(['Stale', 'Cancelled', 'Failed', 'Completed'] as const)('%s 不再发布', (status) => {
    expect(canTransitionRun(status, 'Publishing')).toBe(false);
  });
  it('不能跳过验证直接发布', () => {
    expect(canTransitionRun('Reviewing', 'Publishing')).toBe(false);
  });
  it('基础设施检查可以完成但不能伪装成 AI 审查', () => {
    expect(canTransitionRun('Queued', 'Completed')).toBe(true);
  });
});
