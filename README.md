# ReviewPilot

面向公共 TypeScript/JavaScript 仓库的 AI 代码审查助手：Web 管理仓库与结果，未来以可追溯证据、低噪声发布、自进化规则记忆和人工纠错形成闭环。

## 当前状态

S2 在 Go/Gin/GORM API/dispatcher、RabbitMQ/事务 Outbox、TypeScript Workers 与 Next/React Web 上加入最小 AI 审查。浏览器通过 Caddy 同源直连 Go REST，没有 BFF。

新增固定 Base/Head 源码读取、LangGraph 串行候选与验证、模型预算与未知用量阻断、证据过滤、PR 累计三根因限额、可对账发布，以及中文结果/覆盖/费用页面。模型与发布默认关闭；关闭时仍是 S1 neutral 连接 Check，不表示 AI 审查。没有源码执行、自动学习或人工纠错，本轮止于 S2。

当前验收范围是本地模拟；真实 GitHub App、模型兼容与质量、实际账单、业务数据库切换、完整应用镜像、生产/TLS 均待外部验收。模拟结果不能证明准确率或实际费用。

## 运行与验证

需要 Node 24+、pnpm 12.8.1、Docker；宿主开发需要 Go 1.27.1。pnpm 安装依赖，工程任务由 Turborepo 2.11.7 编排，无远程缓存。

```bash
pnpm install --frozen-lockfile
cp .env.example .env
pnpm exec turbo run infra:up --filter=@reviewpilot/tooling
pnpm exec turbo run infra:migrate --filter=@reviewpilot/tooling
pnpm exec turbo run infra:cutover --filter=@reviewpilot/tooling
```

打开 http://localhost:8080/repositories。新库初始 mode=legacy；最后一条是只读预检，不自动切换。启动 Go 消费前按手册实际停止旧进程、排空租约、显式 CAS 切换。私钥放仓库外，不发给 AI、不提交 Git。

```bash
pnpm exec turbo run verify:migration --filter=@reviewpilot/tooling
```

一次汇总关键单元、六个隔离集成场景、契约、静态/构建和配置，不连接业务库或写真实 GitHub。Next 的 Node 环境加载修复保留，不使用被复制到 NODE_OPTIONS 的 env-file CLI 参数。

S2 推荐使用有界验收入口，不重复完整 S1 矩阵：

```bash
pnpm exec turbo run test:review --filter=@reviewpilot/tooling -- --case pipeline
pnpm exec turbo run verify:review --filter=@reviewpilot/tooling
```

它创建本次专用的临时 PostgreSQL、RabbitMQ 和 Collector，模拟 GitHub 与模型；经过实际内部 REST 与 Worker，断言重复消息/提交确认丢失、未知用量与过期 Head 的行为，退出后只清理本次资源。不读取 `.env` 里的真实密钥。页面入口为 `/repositories/{installationId}/{repositoryId}/pulls`，仅展示当前有维护权的 S2 历史记录。

## 文档与所有权

- [当前技术栈设计](docs/superpowers/specs/2026-10-05-reviewpilot-tech-stack-design.md)
- [S1 迁移计划](2026-10-05-reviewpilot-s1-migration-plan.md)
- [运行、切换、回退与监控](docs/runbooks/s1-go-migration.md)
- [迁移交付记录](docs/reports/s1-go-migration.md)
- [S2 本地运行、预算对账与清理](docs/runbooks/s2-local-review.md)
- [S2 实施与验收记录](docs/reports/s2-ai-review.md)
- [产品与自进化设计](docs/superpowers/specs/2026-10-04-reviewpilot-design.md)
- [六阶段路线图](2026-10-04-reviewpilot-implementation-plan.md)

apps/api 独占业务数据库/GitHub；Web 只展示；Worker 只接 Broker/角色 REST，模型凭证仅属于 Review Worker。packages/contracts 是跨语言契约源，observability 提供受控遥测。旧 TS persistence/github 仅保留为开发兼容 Fixture，不是应用运行时。LangChain/LangGraph 已用于 S2；pgvector、规则记忆、自进化及人工纠错仍未实现。
