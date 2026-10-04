# ADR 0001：S1 工程与运行时

日期：2026-10-04。状态：采用；真实 GitHub 安装尚待配置与验收。

## 决策

使用 pnpm 工作区和 TypeScript 模块化单体，Web、Review Worker、Evolution Worker 分进程。共享 PostgreSQL，但每次查询与任务身份都带 Installation/Repository。S1 不调用模型、不执行 PR 代码、不生成问题评论。

| 项目 | 锁定版本或约定 |
| --- | --- |
| 目标 Node | Node 24 LTS，记录在 .node-version；本机实际验证为 Node 26.10.0，未运行额外版本矩阵 |
| pnpm | 12.8.1 |
| Next.js / React | 16.3.8 / 19.3.0 |
| TypeScript | 6.0.3；未采用最新 7.0.2，因为 typescript-eslint 8.71.0 的 peer 范围小于 6.1 |
| PostgreSQL | 18.4；开发 Compose 使用 postgres:18.4 |
| SQL / Schema | pg 8.23.1、zod 4.6.5 |
| 单元与集成测试 | Vitest 5.0.3；测试专用 embedded-postgres 18.4.0-beta.17 启动真实 PostgreSQL，不属于生产依赖 |
| 静态与格式检查 | ESLint 10.12.0、typescript-eslint 8.71.0、Prettier 3.9.9 |
| Playwright / 模型 Provider | 本阶段不安装；分别在界面验收与模型网关阶段引入 |

lockfile 固定完整依赖树。工作区包直接导出 TypeScript；Web 通过 transpilePackages 构建，Worker 用 tsx 运行。相对导入采用无扩展名形式，兼容 Next/Turbopack 与 tsx。

迁移通过 `@reviewpilot/persistence/migrate` 单独导出，不进入 Web 的数据库运行时入口。迁移在事务与 advisory lock 内运行，并校验已执行文件的内容哈希。

## GitHub 与任务边界

- OAuth 使用 state、S256 PKCE 和一次性数据库消费；浏览器只有 HttpOnly 不透明会话 ID。
- 用户令牌仅在服务端解密；数据库存 AES-256-GCM 密文，不向日志、页面或模型输出。会话最多 8 小时，不实现 refresh token 保存；过期后重新登录。
- App 的安装归属与用户的 maintain/admin 权限分开校验；安装仓库默认不启用。安装回调参数不能作为授权依据。
- S1 安装令牌限一个仓库，Contents/PR 只读、Checks 可写。S2 发布 PR 评论前需要升级 App 的 Pull requests 权限。
- GitHub REST API 固定为官方当前示例使用的 2026-03-10 版本，不依赖未固定的默认版本。
- Webhook 按原始字节验签；delivery、Run、job 在同一事务提交，重复 ID 携带不同内容时拒绝。
- Run 固定身份与配置快照。S1 的 model 为 none、memory 为空、trustedRulesSha 为空：没有载入可信规则，不伪造默认分支 SHA。
- 队列采用 PostgreSQL SKIP LOCKED 与 leaseToken；数据库时钟决定有效期。重试指数退避，最多 5 次领取。
- 事件默认有 2 秒稳定窗口；严格旧时间戳不覆盖新快照。相同秒的事件无法可靠排序，Worker 在每次写入前重新读取远端 PR Head。
- 基础 Check 名称为“ReviewPilot · 连接检查”，完成结论为 neutral，并说明没有分析代码。远端按 external_id 对账，以覆盖写入成功但响应丢失。
- 外部 REST 写入与数据库无法组成原子事务；本阶段实现租约防护和远端对账，不宣称外部 exactly-once。

## 验证范围与取舍

按用户要求仅实施 S1、完成该阶段本地交付后停止、不使用 subagent、保留少量关键测试。未实施 S2–S6。

本机 Docker daemon 未启动，因此核心集成测试使用项目专用临时 PostgreSQL，且显式禁止创建系统用户。没有操作用户现有数据库。embedded-postgres 的退出钩子可能将失败进程变为 exit(0)，测试命令用外层 JSON 结果确认，避免 CI 假绿。

当前没有 GitHub App 凭证：真实 OAuth、安装、Webhook 与 Check 尚未线上验证。Compose 路径及 Node 24 部署未在本机额外演练，不能从本地测试推出生产可用性。

## 官方依据

- [Node 发布计划](https://github.com/nodejs/Release/blob/main/README.md)
- [Next.js 安装与系统要求](https://nextjs.org/docs/app/getting-started/installation)
- [GitHub App 用户令牌与登录](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app)
- [GitHub App 安装与仓库访问 API](https://docs.github.com/en/rest/apps/installations)
- [GitHub Checks API](https://docs.github.com/en/rest/checks/runs)
- [embedded-postgres 源码与生命周期](https://github.com/leinelissen/embedded-postgres)

Next、TypeScript、Vitest 等具体依赖版本通过官方 npm registry 核实，兼容性以本地构建与 peer 检查为准。
