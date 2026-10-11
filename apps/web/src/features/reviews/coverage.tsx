import type { ReviewRunDetail } from '@reviewpilot/contracts';
import type { ReviewViewModel } from '@/lib/review-view';
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card';
export function Coverage({ detail, view }: { detail: ReviewRunDetail; view: ReviewViewModel }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{view.coverageLabel}</CardTitle>
        <CardDescription>固定 SHA 上的文本证据核验，不执行 PR 代码。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        <p>
          读取 {detail.coverage.readFiles}/{detail.coverage.totalFiles} 个文件 · 覆盖{' '}
          {detail.coverage.coveredChangedLines}/{detail.coverage.totalChangedLines} 行变更
        </p>
        <p>限制：{detail.coverage.limitations.join('、') || '无已记录限制'}</p>
        {detail.coverage.skipped.map((f) => (
          <p key={f.path} className="text-sm break-all">
            {f.path} · {f.reasonCode} · {f.changedLines} 行
          </p>
        ))}
      </CardContent>
    </Card>
  );
}
