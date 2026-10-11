'use client';
import { useState } from 'react';
import Link from 'next/link';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import type { components } from '@reviewpilot/contracts';
import { createAPIClient, errorHint, APIRequestError } from '@/lib/api-client';
import { reviewView, reviewActive, reviewGithubURL } from '@/lib/review-view';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { Empty, EmptyHeader, EmptyTitle, EmptyDescription } from '@/components/ui/empty';
import { Skeleton } from '@/components/ui/skeleton';
import { Findings } from './findings';
import { Coverage } from './coverage';
import { Costs } from './costs';
type Tenant = components['schemas']['TenantRef'];
const forbidden = (e: unknown) =>
  e instanceof APIRequestError && (e.status === 401 || e.status === 403);
export function ReviewConsole({ tenant, number }: { tenant: Tenant; number: number }) {
  const [api] = useState(() => createAPIClient(fetch));
  const [selected, setSelected] = useState<string | null>(null);
  const session = useQuery({
    queryKey: ['session'],
    queryFn: api.getSession,
    refetchInterval: false,
  });
  const history = useInfiniteQuery({
    queryKey: ['review-runs', session.data?.user.id, tenant, number],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => api.listReviewRuns(tenant, number, pageParam),
    getNextPageParam: (p) => p.nextCursor ?? undefined,
    enabled: !!session.data && !session.error,
    refetchInterval: (q) =>
      q.state.error ? false : q.state.data?.pages[0]?.items.some(reviewActive) ? 5000 : false,
  });
  const runs = history.data?.pages.flatMap((p) => p.items) ?? [];
  const runId = selected ?? runs[0]?.runId;
  const detail = useQuery({
    queryKey: ['review-detail', session.data?.user.id, tenant, number, runId],
    queryFn: () => api.getReviewRun(tenant, number, runId!),
    enabled: !!runId && !history.error && !session.error,
    refetchInterval: (q) =>
      q.state.error ? false : q.state.data && reviewActive(q.state.data) ? 5000 : false,
  });
  const error = session.error ?? history.error ?? detail.error;
  if (error)
    return (
      <Alert>
        <AlertTitle>{forbidden(error) ? '授权已失效' : '无法读取审查'}</AlertTitle>
        <AlertDescription>
          {errorHint(error)} {forbidden(error) ? <a href="/auth/github">重新登录 GitHub</a> : null}
        </AlertDescription>
      </Alert>
    );
  if (session.isPending || history.isPending) return <Skeleton className="h-40 w-full" />;
  if (!runId)
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>尚未审查</EmptyTitle>
          <EmptyDescription>S1 的连接检查不属于代码审查结果。</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  const d = detail.data;
  const view = d ? reviewView(d) : null;
  const github = d ? reviewGithubURL(d, d.publication.receipt?.reviewId ?? undefined) : null;
  return (
    <div className="flex flex-col gap-5">
      <Link href={`/repositories/${tenant.installationId}/${tenant.repositoryId}/pulls`}>
        ← 仓库 PR
      </Link>
      <h1>
        PR #{number} · {d?.snapshot.identity.prTitle ?? '审查结果'}
      </h1>
      <nav aria-label="审查历史" className="flex flex-wrap gap-2">
        {runs.map((r) => (
          <Button
            key={r.runId}
            variant="outline"
            aria-pressed={runId === r.runId}
            onClick={() => setSelected(r.runId)}
          >
            {r.headSha.slice(0, 8)} · {r.status} · {new Date(r.createdAt).toLocaleString('zh-CN')}
          </Button>
        ))}
        {history.hasNextPage ? (
          <Button
            variant="outline"
            disabled={history.isFetchingNextPage}
            onClick={() => void history.fetchNextPage()}
          >
            更多历史
          </Button>
        ) : null}
      </nav>
      {d && view ? (
        <>
          <div className="flex flex-wrap gap-2">
            <Badge variant="secondary">{view.modeLabel}</Badge>
            <Badge variant="outline">{view.statusLabel}</Badge>
            {github ? (
              <a href={github} target="_blank" rel="noreferrer">
                GitHub PR ↗
              </a>
            ) : null}
          </div>
          {d.reasonCode ? (
            <Alert>
              <AlertTitle>处理状态说明</AlertTitle>
              <AlertDescription>{d.reasonCode} · 未知事实不会自动重试。</AlertDescription>
            </Alert>
          ) : null}
          <div className="grid gap-4 md:grid-cols-2">
            <Coverage detail={d} view={view} />
            <Costs detail={d} view={view} />
          </div>
          <Findings view={view} />
          <section aria-label="配置与发布" className="text-sm text-muted-foreground break-all">
            <p>
              模型 {d.snapshot.modelConfigVersion} · 生成 {d.snapshot.generationPromptVersion} ·
              验证 {d.snapshot.verificationPromptVersion}
            </p>
            <p>
              仓库配置 v{d.snapshot.repositoryConfigVersion} · 记忆 {d.snapshot.memorySetVersion} ·
              Head {d.snapshot.identity.headSha}
            </p>
            <p>
              发布：{d.publication.state} · PR 累计占用 {d.publication.occupiedSlots}/3 个根因额度
            </p>
          </section>
        </>
      ) : (
        <Skeleton className="h-40 w-full" />
      )}
    </div>
  );
}
export function PullConsole({ tenant }: { tenant: Tenant }) {
  const [api] = useState(() => createAPIClient(fetch));
  const session = useQuery({
    queryKey: ['session'],
    queryFn: api.getSession,
    refetchInterval: false,
  });
  const pulls = useInfiniteQuery({
    queryKey: ['review-pulls', session.data?.user.id, tenant],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => api.listPulls(tenant, pageParam),
    getNextPageParam: (p) => p.nextCursor ?? undefined,
    enabled: !!session.data && !session.error,
    refetchInterval: (q) =>
      q.state.error
        ? false
        : q.state.data?.pages.some((p) => p.items.some((i) => reviewActive(i.latestRun)))
          ? 5000
          : false,
  });
  const error = session.error ?? pulls.error;
  if (error)
    return (
      <Alert>
        <AlertTitle>无法读取 PR</AlertTitle>
        <AlertDescription>{errorHint(error)}</AlertDescription>
      </Alert>
    );
  if (pulls.isPending) return <Skeleton className="h-40 w-full" />;
  const items = pulls.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <div className="flex flex-col gap-4">
      <Link href="/repositories">← 仓库管理</Link>
      <h1>代码审查 PR</h1>
      {items.length ? (
        items.map((p) => (
          <Link
            key={p.prNumber}
            href={`/repositories/${tenant.installationId}/${tenant.repositoryId}/pulls/${p.prNumber}`}
          >
            #{p.prNumber} · {p.title} · {p.latestRun.status}
          </Link>
        ))
      ) : (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>尚未审查</EmptyTitle>
            <EmptyDescription>默认仅运行 S1 连接检查，AI 审查需显式启用。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {pulls.hasNextPage ? (
        <Button
          variant="outline"
          disabled={pulls.isFetchingNextPage}
          onClick={() => void pulls.fetchNextPage()}
        >
          更多 PR
        </Button>
      ) : null}
    </div>
  );
}
