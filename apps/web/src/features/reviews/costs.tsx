import type { ReviewRunDetail } from '@reviewpilot/contracts';
import { microAmount, type ReviewViewModel } from '@/lib/review-view';
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card';
export function Costs({ detail, view }: { detail: ReviewRunDetail; view: ReviewViewModel }) {
  const cost = detail.costs;
  return (
    <Card>
      <CardHeader>
        <CardTitle>{view.costLabel}</CardTitle>
        <CardDescription>
          {cost.currency === 'TEST'
            ? 'TEST 仅是模拟预算单位，不是真实价格。'
            : `币种 ${cost.currency} · 价格版本 ${cost.priceVersion}`}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        <p>
          已知：{microAmount(cost.knownCostMicros)} · 未结算预留：{microAmount(cost.heldCostMicros)}
        </p>
        {cost.calls.map((c) => (
          <p key={c.callId} className="text-sm">
            {c.purpose === 'generate' ? '生成' : '验证'} · 尝试 {c.ordinal} · {c.state} · 用量{' '}
            {c.usage ? `${c.usage.inputTokens} 输入 / ${c.usage.outputTokens} 输出` : '未知'} ·
            已知费用 {c.actualCostMicros === null ? '待对账' : microAmount(c.actualCostMicros)}
          </p>
        ))}
        {cost.monthlyWarning ? <p>全局月预算接近上限。</p> : null}
        <p className="text-xs text-muted-foreground">仓库日预算按 UTC 重置：{cost.dailyResetsAt}</p>
      </CardContent>
    </Card>
  );
}
