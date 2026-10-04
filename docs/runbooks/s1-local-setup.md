# S1 本地运行与 GitHub 接入

本阶段是接入基础，不是可上线的完整 AI Reviewer。无需模型 API Key；不会调用付费模型、执行 PR 脚本或发布 AI 问题评论。

## 1. 本地准备

在 review-pilot 目录中运行。建议 Node 24 LTS 和 pnpm 12.8.1；本次本机验证使用 Node 26.10.0。

```bash
pnpm install --frozen-lockfile
cp .env.example .env
docker compose -f compose.dev.yml up -d
pnpm db:migrate
pnpm dev
```

Docker 需由你自行启动；也可以将 DATABASE_URL 指向你单独创建的 PostgreSQL 18 数据库，再执行迁移。迁移会创建表，不要连接不属于此项目的现有数据库。Compose 密码只适用于本地开发，生产必须替换。

打开 `http://localhost:3000/repositories`。没有 GitHub 配置时显示真实的配置缺项；`GET /api/health` 始终提供无凭证的存活检查，它不是数据库就绪检查。

## 2. 创建你自己的 GitHub App

在 GitHub Developer settings 创建 App；本工程不会替你创建、安装、部署或公开服务。

S1 Repository permissions：

- Metadata：只读。
- Contents：只读。
- Pull requests：只读。
- Checks：读写。

勾选 Pull request、Repository 事件。Installation/Installation repositories 生命周期由 GitHub App 接收，服务已处理创建、撤销、暂停、移除仓库和私有化。

填写对应地址：

| 配置 | 地址 |
| --- | --- |
| Homepage | APP_URL |
| User authorization callback | APP_URL/auth/github/callback |
| Setup URL | APP_URL/installations/callback |
| Webhook URL | 对外可达的 HTTPS 地址/api/github/webhook |

建议测试时使用你自己控制的 HTTPS 域名或开发隧道，把 APP_URL 设成这个根地址，并从该地址打开网页、登录。不要在 localhost 登录后让 OAuth 回调跳到另一个域名，否则 state cookie 不会跟随。隧道与部署均需你自行配置。

启用用户授权流程，在根目录 .env 填入 App ID、slug、Client ID、Client secret、Webhook secret。生成 GitHub App 私钥，将文件保存在仓库外，GITHUB_PRIVATE_KEY_PATH 填绝对路径。不要把凭证发送到聊天或提交 Git。

SESSION_SECRET 需要 32 字节随机值的 base64 编码。可在本地生成，再自行保存进 .env：

```bash
node -e 'console.log(require("node:crypto").randomBytes(32).toString("base64"))'
```

Web 与 Worker 必须使用相同的配置；Web dev/start 从根目录加载 .env。修改后重启。反向代理不要记录 OAuth callback 的查询参数，数据库连接字符串、令牌和私钥也不得进入日志。

## 3. 验证真实接入

1. 启动 Web，在配置的 APP_URL 上用 GitHub 登录。
2. 选择你有 maintain/admin 权限的公共测试仓库，安装 App。
3. 返回仓库页；如安装回调未同步，点击“同步安装”。
4. 点击“启用连接检查”。安装本身不等于启用。
5. 另开终端运行 `pnpm worker:review`。
6. 在专用测试仓库打开或更新 PR，确认 GitHub Webhook delivery 返回 202。
7. 等待仓库页出现真实任务，PR 出现“ReviewPilot · 连接检查”。完成后是 neutral，文字明确没有 AI 审查。
8. 重投同一 delivery，不应增加 Run。停用仓库后，不再领取该仓库任务。

卸载、暂停安装或仓库变私有会取消未完成任务；重新公开或取消暂停不会自行恢复启用，需要维护者确认并启用。用户会话过期后需重新登录。

S1 的 Evolution Worker 只是可启动的基础进程，连接数据库后等待退出，不领取学习任务：

```bash
pnpm worker:evolution
```

## 4. 少量必要验证

```bash
pnpm test
pnpm typecheck
pnpm lint
pnpm build
```

`pnpm test` 只包含 S1 的核心行为；集成测试启动独立临时 PostgreSQL，不依赖 Docker，不读取 .env 的数据库地址。测试结束清理自己的临时数据库与测试密钥。以 root 身份运行时会拒绝启动，而不是修改系统用户。

生产构建后可运行 `pnpm --filter @reviewpilot/web start`。当前启动脚本默认只监听 127.0.0.1；对外运行应使用 HTTPS 反向代理，并在后续部署阶段完善进程监管、限流、备份与就绪检查。

## 当前未覆盖

AI Review、问题评论、Diff/上下文、规则记忆、人工纠错、Replay/Shadow、自动激活回滚、评测、预算和生产治理均未实现。本轮不继续 S2。

持续远端故障或进程失联时，数据库会把耗尽重试的 Run 标记 Failed；远端 Check 可能仍保留进行中状态。S5 的周期终态巡检尚未实现，因此不要将 S1 连接检查设为正式合并门禁。
