# ReviewPilot

面向 TypeScript / JavaScript 公共仓库的 AI 代码审查助手。采用托管式 GitHub App 与 Web 控制台，提供可追溯的审查证据、自进化规则记忆与人工纠错。

## 文档入口

- [中文产品与系统设计文档 v1.1](docs/superpowers/specs/2026-10-04-reviewpilot-design.md)
- [中文分阶段实现方案](2026-10-04-reviewpilot-implementation-plan.md)

设计文档整合了已讨论确认的八部分：产品闭环、Review 引擎、Web 控制台、系统架构、自进化 Memory、评测与测试、安全隐私与成本、MVP 路线图。

实现方案将设计拆成六个阶段、22 个可验收任务，明确文件职责、接口、依赖、测试与交付门槛，并列出可延后的增强功能。

## 当前状态

S1 的本地工程与 GitHub 接入代码已实现：中文仓库管理页、OAuth 登录、安装归属与维护权限校验、PostgreSQL 任务队列、Webhook 验签与去重、基础连接 Check，以及两个独立 Worker 入口。

**尚未进行真实 GitHub 安装验收**，需要你配置自己的 App 凭证与可访问的 Webhook 地址。此阶段不调用模型，不生成 AI 问题评论；Completed 仅表示连接检查结束，不表示代码已经审查。

按你的要求，本轮只交付 S1，停下后不自行进入 S2、不使用 subagent，只保留关键测试。自进化与人工纠错仍按 S4 计划实施；文档里的质量、性能与成本数值依然是目标，尚未测得。

## 快速开始

建议 Node 24 LTS、pnpm 12.8.1。Monorepo 采用 Turborepo 2.11.7：pnpm 只负责安装依赖和管理 workspace，所有工程任务统一使用 `turbo run`。下方用 `pnpm exec` 调用项目锁定的 Turbo，无需全局安装。详情见 [S1 本地运行与 GitHub 接入](docs/runbooks/s1-local-setup.md)。

```bash
pnpm install --frozen-lockfile
cp .env.example .env
docker compose -f compose.dev.yml up -d
pnpm exec turbo run db:migrate
pnpm exec turbo run dev
```

打开 http://localhost:3000/repositories。未配置 GitHub 时显示配置说明，不显示模拟数据。实际接入需在另一终端启动 `pnpm exec turbo run worker:review`。

核心验证：`pnpm exec turbo run test`、`pnpm exec turbo run typecheck lint`、`pnpm exec turbo run build`。集成测试使用专用临时 PostgreSQL，不操作你的现有数据库。

生产启动：`pnpm exec turbo run start`（先执行 build）。格式化：`pnpm exec turbo run format`。Evolution Worker：`pnpm exec turbo run worker:evolution`（S1 仍未启用学习）。根目录同名脚本全部只是 Turbo 别名，没有独立的旧执行路径。

`apps/*` 保存应用和 Worker，`packages/*` 保存共享模块；`packages/tooling` 承接全仓静态检查、格式化和跨包集成测试。`turbo.json` 定义任务依赖、缓存和环境变量边界。构建及无副作用检查使用本地缓存；开发/生产服务、Worker、迁移、格式化与集成测试始终真实执行。不启用远程缓存。

工程选择及本阶段取舍见 [ADR 0001](docs/adr/0001-runtime-and-dependencies.md)。

优先完成的产品闭环：

~~~text
安装 App → PR 审查 → 证据验证 → 低噪声发布
→ 维护者反馈 → 自动学习 → Replay / Shadow → 自动生效或回滚
~~~

自动学习首先作用于仓库记忆、检索与候选排序；人工规则具有优先权。模型、Prompt 与审查策略的变更通过独立版本和评测门禁管理。
