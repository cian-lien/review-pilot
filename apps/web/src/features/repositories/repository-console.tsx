'use client';
import { useState } from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { components } from '@reviewpilot/contracts';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button, buttonVariants } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty';
import { Skeleton } from '@/components/ui/skeleton';
import { Spinner } from '@/components/ui/spinner';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  APIRequestError,
  createAPIClient,
  errorHint,
  type SetEnabledInput,
} from '@/lib/api-client';
import { toggleRepository } from '@/lib/repository-actions';

type RunStatus = components['schemas']['RunSummary']['status'];
const statusLabels: Record<RunStatus, string> = {
  Queued: '等待处理',
  Indexing: '准备连接',
  Reviewing: '验证连接',
  Verifying: '核验连接',
  Publishing: '发布 Check',
  Completed: '连接完成 · neutral',
  Partial: '部分完成',
  Stale: '旧版本 · 已失效',
  Cancelled: '已取消',
  Failed: '连接失败',
};
function Notice({ title, message }: { title: string; message: string }) {
  return (
    <Alert>
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  );
}
function NoData({ title, description }: { title: string; description: string }) {
  return (
    <Empty>
      <EmptyHeader>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}
const isUnauthorized = (error: unknown) => error instanceof APIRequestError && error.status === 401;

export function RepositoryConsole() {
  const [api] = useState(() => createAPIClient(fetch));
  const cache = useQueryClient();
  const bootstrap = useQuery({
    queryKey: ['bootstrap'],
    queryFn: api.getBootstrap,
    refetchInterval: false,
  });
  const session = useQuery({ queryKey: ['session'], queryFn: api.getSession });
  const authenticated = !!session.data && !session.error;
  const repositories = useQuery({
    queryKey: ['repositories', session.data?.user.id],
    queryFn: api.listRepositories,
    enabled: authenticated,
  });
  const runs = useQuery({
    queryKey: ['runs', session.data?.user.id],
    queryFn: api.listRuns,
    enabled: authenticated,
  });
  const expired =
    isUnauthorized(session.error) ||
    isUnauthorized(repositories.error) ||
    isUnauthorized(runs.error);
  const refresh = async () => {
    await Promise.all([
      cache.invalidateQueries({ queryKey: ['repositories'] }),
      cache.invalidateQueries({ queryKey: ['runs'] }),
      cache.invalidateQueries({ queryKey: ['session'] }),
    ]);
  };
  const toggle = useMutation({
    mutationFn: (input: SetEnabledInput) => toggleRepository(api, input),
    onSuccess: refresh,
    onError: refresh,
  });
  const sync = useMutation({
    mutationFn: api.syncRepositories,
    onSuccess: refresh,
    onError: refresh,
  });
  const logout = useMutation({
    mutationFn: api.logout,
    onSuccess: async () => {
      await cache.cancelQueries();
      cache.clear();
      window.location.assign('/repositories');
    },
  });
  const actionError = toggle.error ?? sync.error ?? logout.error;
  const busy = toggle.isPending || sync.isPending || logout.isPending;
  const missing = bootstrap.data?.missingConfiguration ?? [];
  const dataError = repositories.error ?? runs.error;
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold tracking-tight">仓库与连接任务</h1>
          <p className="text-muted-foreground">
            S1 仅验证 GitHub 接入，不进行 AI 代码审查、规则学习或执行 PR 代码。
          </p>
        </div>
        {authenticated && !expired ? (
          <div className="flex items-center gap-3">
            <span className="text-sm text-muted-foreground">{session.data?.user.login}</span>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => logout.mutate(session.data!.csrfToken)}
            >
              退出登录
            </Button>
          </div>
        ) : null}
      </div>
      {bootstrap.isPending ? (
        <Skeleton className="h-24 w-full" aria-label="正在获取服务配置" />
      ) : bootstrap.error ? (
        <Notice title="无法读取 Go 服务配置" message={errorHint(bootstrap.error)} />
      ) : null}
      {missing.length ? (
        <Notice
          title="后端配置尚未完成"
          message={`请按运行手册配置 Go 服务并重启。缺少：${missing.join('、')}。页面不接收后端密钥。`}
        />
      ) : null}
      {session.isPending ? (
        <Skeleton className="h-36 w-full" aria-label="正在确认登录状态" />
      ) : !authenticated || expired ? (
        <Card>
          <CardHeader>
            <CardTitle>连接 GitHub</CardTitle>
            <CardDescription>从公共仓库开始，Go 服务独立验证登录身份和维护权限。</CardDescription>
          </CardHeader>
          <CardContent>
            {session.error && !expired ? (
              <Notice title="登录服务暂时不可用" message={errorHint(session.error)} />
            ) : (
              <p>请登录 GitHub 后查看仓库。App 安装和登录权限分开校验。</p>
            )}
          </CardContent>
          <CardFooter>
            <a href="/auth/github" className={buttonVariants()}>
              使用 GitHub 登录
            </a>
          </CardFooter>
        </Card>
      ) : (
        <>
          {actionError ? <Notice title="操作未完成" message={errorHint(actionError)} /> : null}
          {dataError ? (
            <Notice title="数据暂时不可用，已隐藏缓存内容" message={errorHint(dataError)} />
          ) : null}
          <Card>
            <CardHeader>
              <CardTitle>已授权仓库</CardTitle>
              <CardDescription>
                只有当前具备 maintain/admin 权限的用户可以开启或停用连接检查。
              </CardDescription>
            </CardHeader>
            <CardContent>
              {repositories.isPending ? (
                <Skeleton className="h-32 w-full" aria-label="正在读取仓库" />
              ) : repositories.error ? null : !repositories.data?.repositories.length ? (
                <NoData
                  title="尚无可访问的公共仓库"
                  description="安装 App 后同步仓库；仓库默认停用，由维护者显式开启。"
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>仓库</TableHead>
                      <TableHead>连接检查</TableHead>
                      <TableHead>配置版本</TableHead>
                      <TableHead>管理</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {repositories.data.repositories.map((repo) => (
                      <TableRow key={`${repo.tenant.installationId}:${repo.tenant.repositoryId}`}>
                        <TableCell>
                          <a
                            href={`https://github.com/${repo.fullName}`}
                            target="_blank"
                            rel="noreferrer"
                          >
                            {repo.fullName}
                          </a>
                          <p>
                            <Link
                              href={`/repositories/${repo.tenant.installationId}/${repo.tenant.repositoryId}/pulls`}
                            >
                              查看代码审查 PR →
                            </Link>
                          </p>
                        </TableCell>
                        <TableCell>
                          <Badge variant={repo.enabled ? 'secondary' : 'outline'}>
                            {repo.enabled ? '已启用' : '已停用'}
                          </Badge>
                        </TableCell>
                        <TableCell>v{repo.configVersion}</TableCell>
                        <TableCell>
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={!repo.canManage || !repo.public || busy}
                            aria-label={`${repo.enabled ? '停用' : '启用'} ${repo.fullName}`}
                            onClick={() =>
                              toggle.mutate({
                                tenant: repo.tenant,
                                enabled: !repo.enabled,
                                configVersion: repo.configVersion,
                                csrfToken: session.data!.csrfToken,
                              })
                            }
                          >
                            {toggle.isPending &&
                            toggle.variables?.tenant.repositoryId === repo.tenant.repositoryId ? (
                              <Spinner data-icon="inline-start" aria-label="正在保存" />
                            ) : null}
                            {repo.canManage ? (repo.enabled ? '停用' : '启用') : '无维护权限'}
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
            <CardFooter className="flex flex-wrap gap-3">
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => sync.mutate(session.data!.csrfToken)}
              >
                {sync.isPending ? <Spinner data-icon="inline-start" aria-label="正在同步" /> : null}
                同步仓库
              </Button>
              {bootstrap.data?.installUrl ? (
                <a
                  className={buttonVariants({ variant: 'outline' })}
                  href={bootstrap.data.installUrl}
                >
                  安装 / 调整 GitHub App
                </a>
              ) : null}
            </CardFooter>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>最近 20 个连接任务</CardTitle>
              <CardDescription>
                约每 5 秒刷新；切到后台暂停轮询。neutral 只表示基础连接完成，不代表代码质量通过。
              </CardDescription>
            </CardHeader>
            <CardContent>
              {runs.isPending ? (
                <Skeleton className="h-32 w-full" aria-label="正在读取任务" />
              ) : runs.error ? null : !runs.data?.runs.length ? (
                <NoData
                  title="还没有连接任务"
                  description="开启仓库后，新 PR 或推送更新会创建任务。这里不展示模拟数据。"
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>仓库 / PR</TableHead>
                      <TableHead>Head</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead>创建时间</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {runs.data.runs.slice(0, 20).map((run) => (
                      <TableRow key={run.id}>
                        <TableCell>
                          <a
                            href={`https://github.com/${run.fullName}/pull/${run.prNumber}`}
                            target="_blank"
                            rel="noreferrer"
                          >
                            {run.fullName} #{run.prNumber}
                          </a>
                        </TableCell>
                        <TableCell>
                          <code>{run.headSha.slice(0, 8)}</code>
                        </TableCell>
                        <TableCell>
                          <Badge variant={run.status === 'Failed' ? 'destructive' : 'outline'}>
                            {statusLabels[run.status]}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <time dateTime={run.createdAt}>
                            {new Date(run.createdAt).toLocaleString('zh-CN')}
                          </time>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
            <CardFooter>
              <p className="text-sm text-muted-foreground">
                只有 Go 确认 Check 发布成功后，任务才会显示“连接完成”。
              </p>
            </CardFooter>
          </Card>
        </>
      )}
    </div>
  );
}
