import type { ReviewRunDetail } from '@reviewpilot/contracts';
type ReviewFinding=ReviewRunDetail['findings'][number];
type ReviewFilteredCandidate=ReviewRunDetail['filtered'][number];
export type ReviewViewModel = {
  coverageLabel: string;
  costLabel: string;
  modeLabel: string;
  statusLabel: string;
  emptyLabel: string;
  findings: ReviewFinding[];
  filtered: ReviewFilteredCandidate[];
};
const statusLabels: Record<ReviewRunDetail['status'], string> = {
  Queued: '等待审查',
  Indexing: '读取固定版本',
  Reviewing: '生成候选',
  Verifying: '核验证据',
  Publishing: '发布与对账',
  Completed: '审查完成',
  Partial: '部分完成',
  Stale: 'PR 版本已变化',
  Cancelled: '已取消',
  Failed: '审查失败',
};
export function reviewActive(
  detail: Pick<ReviewRunDetail, 'status' | 'needsReconciliation'>,
): boolean {
  return (
    !detail.needsReconciliation &&
    !['Completed', 'Partial', 'Stale', 'Cancelled', 'Failed'].includes(detail.status)
  );
}
export function reviewView(detail: ReviewRunDetail): ReviewViewModel {
  return {
    coverageLabel:
      detail.coverage.complete && detail.status !== 'Partial' ? '本次范围完整' : '部分覆盖',
    costLabel: detail.costs.needsReconciliation
      ? '费用待对账'
      : detail.costs.calls.length
        ? '已结算费用与预留'
        : '尚未调用模型',
    modeLabel:
      detail.snapshot.provenance.modelMode === 'fake' ? '模拟审查 · 非真实账单' : '真实模型审查',
    statusLabel: detail.needsReconciliation ? '已阻断 · 等待对账' : statusLabels[detail.status],
    emptyLabel: detail.contentExpired
      ? '内容已过期或未保留，问题数量仍为历史记录'
      : ['Completed', 'Partial'].includes(detail.status)
        ? '本次覆盖内未发现可发布问题'
        : '本次审查尚无可展示问题',
    findings: detail.findings,
    filtered: detail.filtered,
  };
}
export function microAmount(value: string): string {
  const amount = BigInt(value);
  const fraction = (amount % 1000000n).toString().padStart(6, '0').replace(/0+$/, '');
  return (amount / 1000000n).toString() + (fraction ? '.' + fraction : '');
}
export function reviewGithubURL(detail: ReviewRunDetail, remoteId?: string): string | null {
  const name = detail.snapshot.identity.repositoryFullName;
  if (
    !/^[a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+$/.test(name) ||
    name.split('/').some((p) => p === '.' || p === '..')
  )
    return null;
  const number = detail.snapshot.identity.prNumber;
  if (!Number.isSafeInteger(number) || number < 1) return null;
  if (
    remoteId !== undefined &&
    (!/^[1-9][0-9]{0,15}$/.test(remoteId) || BigInt(remoteId) > 9007199254740991n)
  )
    return null;
  return (
    `https://github.com/${name}/pull/${number}` + (remoteId ? `#pullrequestreview-${remoteId}` : '')
  );
}
