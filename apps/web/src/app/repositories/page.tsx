import Link from 'next/link';
import { configuration, configurationIssues, database, getSession, github } from '../../lib/server';
export const dynamic = 'force-dynamic';
interface RepoRow {
  installation_id: string;
  id: string;
  full_name: string;
  enabled: boolean;
}
interface RunRow {
  id: string;
  installation_id: string;
  repository_id: string;
  full_name: string;
  pr_number: number;
  status: string;
  head_sha: string;
  created_at: Date;
}
export default async function Repositories() {
  const issues = configurationIssues();
  if (issues.length)
    return (
      <>
        <h1>先连接你的 GitHub 仓库</h1>
        <p className="lead">
          完成接入后，PR 事件会生成持久化任务，并在 GitHub 上发布基础连接 Check。
        </p>
        <section className="notice">
          <h2>服务端配置尚未完成</h2>
          <p>请按 README 和 docs/runbooks/s1-local-setup.md 配置根目录 .env，然后重新启动 Web。</p>
          <ul>
            {issues.map((issue) => (
              <li key={issue}>
                <code>{issue}</code>
              </li>
            ))}
          </ul>
        </section>
        <p className="muted">不会展示模拟仓库、审查结果或准确率。</p>
      </>
    );
  let session: Awaited<ReturnType<typeof getSession>> = null,
    repositories: RepoRow[] = [],
    runs: RunRow[] = [],
    unavailable = false;
  try {
    session = await getSession();
    if (session) {
      const api = github(),
        installations = (await api.userInstallations(session.userToken)).filter(
          (item) => item.appId === configuration().appId && !item.suspended,
        );
      const visible = new Set<string>();
      for (const installation of installations)
        for (const repository of await api.userInstallationRepositories(
          session.userToken,
          installation.id,
        ))
          if (repository.public) visible.add(`${installation.id}:${repository.id}`);
      const records = await database().query<RepoRow>(
        'SELECT r.installation_id,r.id,r.full_name,r.enabled FROM repositories r JOIN installation_memberships m ON m.installation_id=r.installation_id WHERE m.user_id=$1 AND r.public ORDER BY r.full_name',
        [session.userId],
      );
      repositories = records.rows.filter((row) => visible.has(`${row.installation_id}:${row.id}`));
      const recent = await database().query<RunRow>(
        'SELECT r.id,r.installation_id,r.repository_id,repo.full_name,r.pr_number,r.status,r.head_sha,r.created_at FROM review_runs r JOIN repositories repo ON repo.installation_id=r.installation_id AND repo.id=r.repository_id JOIN installation_memberships m ON m.installation_id=r.installation_id WHERE m.user_id=$1 ORDER BY r.created_at DESC LIMIT 100',
        [session.userId],
      );
      runs = recent.rows
        .filter((row) => visible.has(`${row.installation_id}:${row.repository_id}`))
        .slice(0, 20);
    }
  } catch {
    unavailable = true;
  }
  if (!session && !unavailable)
    return (
      <>
        <h1>把审查接入开发流程</h1>
        <p className="lead">
          从一个公共 TypeScript / JavaScript 仓库开始。先连接
          GitHub，再由具有维护权限的用户开启仓库。
        </p>
        <section className="onboarding">
          <h2>连接 GitHub</h2>
          <p>登录身份与仓库管理权限独立验证。仅访问你授权的 App 安装与仓库。</p>
          <Link href="/auth/github" className="button primary">
            使用 GitHub 登录
          </Link>
        </section>
        <p className="muted">S1 不生成 AI 问题评论；自进化与人工纠错会在后续阶段实现。</p>
      </>
    );
  const config = configuration();
  return (
    <>
      <div className="heading">
        <div>
          <h1>仓库与任务</h1>
          <p className="lead">管理接入范围，查看真实任务状态。</p>
        </div>
        <div className="actions">
          <span className="muted">{session?.login ?? '会话待验证'}</span>
          <form action="/auth/logout" method="post">
            <button>退出登录</button>
          </form>
        </div>
      </div>
      {unavailable ? (
        <section className="notice">
          <h2>连接暂不可用</h2>
          <p>请检查数据库迁移与 GitHub 会话；令牌过期时需重新登录。</p>
          <Link href="/auth/github" className="button">
            重新登录
          </Link>
        </section>
      ) : null}
      <section>
        <div className="section-heading">
          <div>
            <h2>公共仓库</h2>
            <p className="muted">安装不会自动开启仓库。调整时会再次核实 maintain/admin 权限。</p>
          </div>
          <div className="actions">
            <a
              className="button"
              href={`https://github.com/apps/${encodeURIComponent(config.appSlug)}/installations/new`}
            >
              安装 GitHub App
            </a>
            <form action="/api/repositories/sync" method="post">
              <button>同步安装</button>
            </form>
          </div>
        </div>
        {repositories.length ? (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>仓库</th>
                  <th>接入状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {repositories.slice(0, 100).map((repository) => (
                  <tr key={`${repository.installation_id}:${repository.id}`}>
                    <td>
                      <a
                        href={`https://github.com/${repository.full_name}`}
                        target="_blank"
                        rel="noreferrer"
                      >
                        {repository.full_name}
                      </a>
                      <small>安装 #{repository.installation_id}</small>
                    </td>
                    <td>
                      <span className={repository.enabled ? 'status enabled' : 'status'}>
                        {repository.enabled ? '已启用' : '未启用'}
                      </span>
                    </td>
                    <td>
                      <form
                        action={`/api/repositories/${repository.installation_id}/${repository.id}`}
                        method="post"
                      >
                        <input type="hidden" name="enabled" value={String(!repository.enabled)} />
                        <button>{repository.enabled ? '停用' : '启用连接检查'}</button>
                      </form>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {repositories.length > 100 ? (
              <p className="muted">此基础页面仅展示前 100 个仓库。</p>
            ) : null}
          </div>
        ) : (
          <div className="empty">
            <strong>还没有已同步的公共仓库</strong>
            <p>安装 App 后点击“同步安装”。私有仓库不会出现在这里。</p>
          </div>
        )}
      </section>
      <section>
        <div className="section-heading">
          <div>
            <h2>最近连接任务</h2>
            <p className="muted">
              最多展示 20 个可访问任务。Completed 只代表连接检查结束，不代表代码已审查。
            </p>
          </div>
        </div>
        {runs.length ? (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Pull request</th>
                  <th>状态</th>
                  <th>快照</th>
                  <th>创建时间</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((run) => (
                  <tr key={run.id}>
                    <td>
                      <a
                        href={`https://github.com/${run.full_name}/pull/${run.pr_number}`}
                        target="_blank"
                        rel="noreferrer"
                      >
                        {run.full_name} #{run.pr_number}
                      </a>
                    </td>
                    <td>
                      <span className="status">{run.status}</span>
                    </td>
                    <td>
                      <code>{run.head_sha.slice(0, 8)}</code>
                    </td>
                    <td>{run.created_at.toISOString().replace('T', ' ').slice(0, 19)} UTC</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="empty">
            <strong>尚无连接任务</strong>
            <p>启用仓库后，打开或更新一个 PR。确保 Webhook 与 Review Worker 正在运行。</p>
          </div>
        )}
      </section>
    </>
  );
}
