import type { ReviewViewModel } from '@/lib/review-view';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from '@/components/ui/card';
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty';
export function Findings({ view }: { view: ReviewViewModel }) {
  return (
    <section aria-label="审查问题" className="flex flex-col gap-4">
      {view.findings.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{view.emptyLabel}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : (
        view.findings.map((f) => (
          <Card key={f.findingId}>
            <CardHeader>
              <CardTitle>{f.candidate.title}</CardTitle>
              <CardDescription>
                <Badge variant="outline">{f.candidate.severity}</Badge> · {f.publicationStatus}{' '}
                {f.reasonCode}
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {f.contentExpired ? (
                <p>正文已过期或未保留；此条历史问题没有被当作零问题。</p>
              ) : (
                <>
                  <p>触发条件：{f.candidate.failureScenario}</p>
                  <p>影响：{f.candidate.impact}</p>
                  {f.evidence.map((e) => (
                    <div key={e.ref.evidenceId}>
                      <p className="text-sm text-muted-foreground break-all">
                        {e.ref.path} · {e.ref.startLine}–{e.ref.endLine} · {e.ref.sha.slice(0, 8)}
                      </p>
                      <pre className="overflow-x-auto whitespace-pre-wrap break-all bg-muted p-3 text-sm">
                        {e.snippet}
                      </pre>
                      <p className="text-xs text-muted-foreground break-all">
                        证据哈希：{e.ref.contentHash}
                      </p>
                    </div>
                  ))}
                </>
              )}
            </CardContent>
            <CardFooter>
              {f.contentExpired ? '仅保留元数据' : `建议验证：${f.candidate.suggestedVerification}`}
            </CardFooter>
          </Card>
        ))
      )}
      {view.filtered.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>已过滤的候选 · {view.filtered.length}</CardTitle>
            <CardDescription>过滤原因不等于代码安全证明。</CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="flex flex-col gap-2">
              {view.filtered.map((f) => (
                <li key={f.candidateId}>
                  {f.title} · {f.reasonCode}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ) : null}
    </section>
  );
}
