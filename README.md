# ReviewPilot

面向公共 TypeScript/JavaScript 仓库的 AI 代码审查助手：Web 管理仓库与结果，未来以可追溯证据、低噪声发布、自进化规则记忆和人工纠错形成闭环。

## 当前状态

S1 本地代码已迁移为 Go/Gin/GORM API/dispatcher、RabbitMQ/事务 Outbox、TypeScript Workers 与 Next/React Web。浏览器通过 Caddy 同源直连 Go REST，没有 BFF。

目前只发布 neutral 连接 Check，Completed 不表示代码已经审查。没有 AI、源码执行或自动学习。本轮只执行 S1，结束后停止。真实 GitHub App、实际数据库切换、生产/TLS 仍待用户环境和授权；专用临时库/假 GitHub 不替代外部验收。

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

## 文档与所有权

- [当前技术栈设计](docs/superpowers/specs/2026-10-05-reviewpilot-tech-stack-design.md)
- [S1 迁移计划](2026-10-05-reviewpilot-s1-migration-plan.md)
- [运行、切换、回退与监控](docs/runbooks/s1-go-migration.md)
- [迁移交付记录](docs/reports/s1-go-migration.md)
- [产品与自进化设计](docs/superpowers/specs/2026-10-04-reviewpilot-design.md)
- [六阶段路线图](2026-10-04-reviewpilot-implementation-plan.md)

apps/api 独占业务数据库/GitHub；Web 只展示；Worker 只接 Broker/角色 REST。packages/contracts 是跨语言契约源，observability 提供受控遥测。旧 TS persistence/github 仅保留为开发兼容 Fixture，不是应用运行时。LangChain/LangGraph 在 S2，pgvector/规则记忆/人工纠错在 S4，尚未实现。
