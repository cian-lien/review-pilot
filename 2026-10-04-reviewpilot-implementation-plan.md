# ReviewPilot 分阶段实现方案

> 面向执行者：本轮已由用户授权实施 S1。用户要求直接在 projects/review-pilot 开发、完成一个阶段后停下、不使用 subagent、不过量测试。S2–S6 不启动；部署及真实 GitHub 安装不自动操作。

**目标：** 在接近全职投入下，用 4–6 周交付受控 Beta，跑通“GitHub PR 审查 → 证据发布 → 人工纠错 → 自动学习 → Replay/Shadow → 激活或回滚”。

**架构：** 使用 TypeScript 模块化单体，共享领域模型与 PostgreSQL，Web/API、Review Worker、Evolution Worker 分进程运行。模型与 GitHub 通过适配器隔离，外部发布可对账，审查与 Memory 版本可恢复、可追溯。

**技术方向：** Next.js、React、TypeScript、PostgreSQL、Node.js、pnpm workspace + Turborepo；pnpm 只负责依赖安装，全部工程任务采用 turbo run。测试采用 Vitest，Playwright 在后续阶段引入；数据库先使用参数化 SQL 与迁移，队列先采用 PostgreSQL 租约任务。S1 已锁定的依赖见 ADR 0001；模型等后续选择不表示已安装。

**依据：** [ReviewPilot 产品与系统设计文档 v1.1](docs/superpowers/specs/2026-10-04-reviewpilot-design.md)。

**日期：** 2026-10-04。  
**状态：** S1 本地工程与接入代码已实现；真实 GitHub 安装、Webhook 与 Check 验收待用户配置凭证。S2–S6 尚未执行。详细记录见 [S1 交付记录](docs/reports/s1-implementation.md)。未实际执行的原验收项保持未勾选，不用模拟服务测试替代真实安装验收。

## 全局约束

- 首版服务 GitHub 公共 TypeScript / JavaScript 仓库。
- 默认每个 PR 0–3 条问题评论；新增 Run 不重发同一根因。摘要中的问题计入同一上限。
- 证据不足时 Abstain；无评论是正常 Completed；未覆盖内容必须明确说明。
- 首版只静态读取与解析，不执行 PR 的安装脚本、测试、服务或原生扩展。
- 人工锁定规则 > 可信默认分支规则 > 人工纠正 Memory > 自动仓库 Memory > 通用知识。
- 外部 PR 的规则修改不能作为本次审查的高优先级规范。
- 普通自动 Memory 经 Replay/Shadow 门槛验证后自主生效；不逐条等待人工批准。
- Balanced 初始门槛：3 个独立强支持观察或一次明确长期指令；内部评分 ≥ 0.85；至少 5 次 Shadow 命中；至少 10 个有标签的适用案例，其中至少 3 个反例；有效率 ≥ 80%；无已知高风险回归。
- 未回复保持未知，Thread resolved 不直接视为正确；同一 PR 的多个行为合并为一次观察。
- 人工单次纠错不静默扩大全局范围；人工锁定规则不被自动改写。
- 安全与高影响业务保护不能因负反馈自动取消；模型、Prompt、工具权限的全局变更单独发版。
- Run 固定 Base/Head、Pipeline、配置、模型和 Memory 集合，回放输入不含未来答案。
- 核心数据、检索、缓存和 Artifact 按 Installation 与 Repository 隔离。
- Token 不进入日志、模型上下文或前端；模型不直接执行 GitHub 写操作。
- 模型调用前原子预留预算，Review、重试、Replay 与 Evolution 都计入总预算；80% 预警、100% 停止新任务。
- 默认原始 Prompt/Response 与 Context 保留 30 天，可关闭；Evaluation Artifact 90 天；元数据与脱敏审计 12 个月。
- 在线数据清理目标 24 小时内完成；备份最长 30 天轮转，恢复时重放删除凭据。
- Beta 质量目标：Golden 发布精度 ≥ 75%、引用有效率 ≥ 95%、结构位置校验 100%、高风险已知回归 0、重复比例 ≤ 3%。
- 延迟与成本目标：支持范围内 P90 ≤ 3 分钟；平均 Review 模型费用 ≤ ¥3/PR；P95 ≤ ¥8/PR。均为验收目标，尚未测得。
- 默认自动支持不超过 1,500 行变更的 PR；超限采用明确的部分覆盖或手动触发。
- S1 的 Web、Worker、迁移、单元与集成测试命令已经存在；S2–S6 相关命令仍是未来实施约定。
- 所有根脚本只调用 Turbo；服务、Worker、数据库迁移、格式化和集成测试关闭缓存，不使用远程缓存。packages/tooling 统一负责全仓静态检查和跨包集成测试。

## 重点复核的五类输入

| 输入或失败条件 | 必须保证的行为 | 负责任务 |
| --- | --- | --- |
| GitHub 写入成功但响应丢失 | 对账远端后重试，不产生重复问题评论 | 08、19 |
| 安装被撤销或仓库变私有时，任务仍在执行 | 读取与发布前重验权限，停止未授权任务 | 04、08、19 |
| Memory 同时被人工修改与自动激活 | 使用预期版本并发控制，人工锁定不被覆盖 | 12、15 |
| 当前外部 PR 修改规则文件或携带越界路径 | 可信规则取已合并版本，文件读取限制在授权快照 | 10、13、19 |
| 冷启动规则有多次命中但无独立标签 | 保持 Shadow，不用模型自评替代真值 | 14、15、17 |

## 一、阶段总览

| 阶段 | 建议时间 | 任务 | 可验收交付物 |
| --- | --- | --- | --- |
| S1：GitHub 与任务基础 | 第 1 周 | 01–04 | 安装公共仓库、接收 PR、持久化任务、更新 Check |
| S2：最小 AI Review | 第 2 周 | 05–08 | 真实 PR 收到 0–3 条证据已校验的问题，Web 可查看 |
| S3：上下文与反证 | 第 3 周 | 09–11 | 固定快照、按需检索、可信规则、候选过滤与 Trace |
| S4：自进化与纠错 | 第 4 周 | 12–15 | Memory 版本、纠错、Replay/Shadow、自动激活与回滚 |
| S5：评测与运行可靠性 | 第 5 周 | 16–19 | 约 50 个 Golden Case、版本门禁、删除、预算与故障测试 |
| S6：受控 Beta | 第 6 周 | 20–22 | 基础治理页面、部署文档、Demo、受控试用入口 |

这些是阶段预算，不是固定日历承诺。任务有依赖时按顺序执行；例如 Memory 自动激活先依赖后台 Replay，再依赖完整评测门禁，界面可以稍后完善。

四周压缩版适用于进展顺利的内部 Alpha：保留安全与质量检查，合并 S2/S3，推迟 Evaluation 图表、Patch、语义向量增强和部分概览。满足公开 Beta 门槛前不以四周期限强行上线。

~~~mermaid
flowchart LR
    S1["S1 集成与任务"] --> S2["S2 可用审查"]
    S2 --> S3["S3 上下文与反证"]
    S3 --> S4["S4 自进化与纠错"]
    S4 --> S5["S5 评测与可靠性"]
    S5 --> S6["S6 受控 Beta"]
~~~

## 二、拟创建的文件与职责

所有路径均相对于 review-pilot。下表覆盖已实现的 S1 与后续拟建模块；实际已创建范围以 S1 交付记录为准。

~~~text
apps/
  web/                         页面、认证与用户 API
  worker-review/               审查任务入口
  worker-evolution/            学习与评测任务入口
packages/
  tooling/                   全仓类型、lint、格式化与跨包集成测试的 Turbo 任务
  domain/src/                 types、run-policy、scope、memory-policy
  persistence/src/            数据库、任务租约、预算与领域持久化
  github/src/                 验签、授权、读取与发布对账
  repository/src/             固定 SHA 快照、受限读取
  analyzer/src/               JS/TS 符号与 Import 分析
  retrieval/src/              Context Bundle 与规则筛选
  model-gateway/src/          Provider、Schema、重试、费用
  review-engine/src/          候选、验证、去重与发布计划
  evolution/src/              信号归因、Memory 生命周期与纠错
  evaluation/src/             固定输入回放、Judge、指标与门禁
  artifacts/src/              按租户读写、保留与删除
  observability/src/          脱敏日志、Trace 与指标
tests/
  fixtures/                  受控 PR、模型输出、Memory 与删除数据
  integration/               数据库与 GitHub fake server
  e2e/                       本地与专用 GitHub 测试仓库
eval/
  cases/                     Golden Case JSON 与固定源码
  labels/                    只提供给评分器的答案
  manifests/                 数据集版本与拆分
docs/
  adr/                       技术、权限、隐私与指标决策
  runbooks/                  部署、恢复、预算与删除操作
  reports/                   评测与 Beta 复盘
~~~

不要让一个 orchestrator 文件承担分析、Prompt、发布和数据库的全部实现；每个任务只通过下方接口与相邻模块通信。

## 三、任务共用契约

任务 01 在 packages/domain/src/types.ts 建立类型，后续只能兼容扩展：

| 类型 | 最小字段 |
| --- | --- |
| TenantRef | installationId、repositoryId |
| RepoAccess | tenant、public、installationActive、maintainer |
| ReviewIdentity | tenant、prNumber、baseSha、headSha、pipelineVersion、generation |
| RunSnapshot | identity、modelConfigVersion、repositoryConfigVersion、memorySetVersion、trustedRulesSha |
| RepoSnapshot | tenant、baseSha、headSha、files：路径/内容哈希/读取结果 |
| CodeRef | tenant、path、commitSha、startLine、endLine、contentHash |
| ContextBundle | tenant、snapshot、changeUnits、codeRefs、memoryVersions、coverage |
| Candidate | id、rootCauseFingerprint、location、failureScenario、impact、severity、evidence |
| VerificationResult | candidateId、verdict：verified/rejected/abstain、reasonCodes、verifiedEvidence |
| PublicationPlan | runId、snapshot、findings、summary、coverage、remainingCommentSlots |
| FeedbackInput | tenant、actorId、findingId、kind、reason、scopeIntent、idempotencyKey |
| MemoryVersion | tenant、memoryId、version、type、statement、scope、riskLevel、origin、locked、status、evidenceIds、createdAt、lastUsedAt |
| EvolutionMetrics | independentSupport、explicitInstruction、score、shadowHits、labeledCases、counterexamples、precision、criticalRegressions |
| ReplaySpec | tenant、datasetVersion、cutoffAt、baselineSnapshot、candidateSnapshot、publish：false |
| EvaluationReport | version、labeledFindingCount、TP/FP/FN、coverage、criticalMisses、cost、latency、regressions |
| BudgetReservation | tenant、reservationId、purpose、maxCostMicros、expiresAt |
| StageCheckpoint | runId、stage、artifactRef、inputHash、status |

其余返回类型由所属任务创建并导出，避免执行时各自定义不同字段：

| 类型 | 定义任务与最小内容 |
| --- | --- |
| RunStatus | 01；Queued/Indexing/Reviewing/Verifying/Publishing/Completed/Partial/Stale/Cancelled/Failed |
| ChangedFile / ParsedDiff / FileReadResult | 05；变更前后路径、操作、有效行映射；读取结果区分 success/missing/denied/too-large |
| ModelRequest / ModelResult | 06；请求字段见任务接口；结果包含合法 data、usage、costMicros、modelVersion、traceId |
| ModelPurpose | 06；generate/verify/propose/replay/judge/embedding |
| FileAnalysis / ChangeUnit | 09；符号、Import、类型引用；变更单元包含路径、符号、Base/Head CodeRef |
| Artifact | 11；tenant、kind、inputHash、createdAt、expiresAt、payload |
| Resolution | 12；applicable 为 MemoryVersion 数组，conflicts 保存冲突 ID/版本/范围 |
| FeedbackEvent | 13；FeedbackInput 加 eventId、prNumber、rootCauseFingerprint、label、observedAt、actorVerified |
| ActivationDecision | 15；kind 为 activate/stay-shadow/reject，加 reasonCodes 与 evaluatedVersion |
| HealthDecision | 15；kind 为 keep/quarantine/restore/revalidate/notify，加 reasonCodes；restore 必须有目标版本 |
| EvaluationInput / GoldenLabels / CaseScore | 14 建立最小版，16–17 扩展；输入不含答案，标签保存根因与反例，分数保存 TP/FP/FN/unknown 与证据校验 |
| GateDecision | 17；pass/fail/incomplete，加 reasonCodes、baselineVersion、candidateVersion |
| DeletionReceipt | 18；deletionId、tenant、在线清理状态、存储清理状态、备份截止时间 |

金额统一使用整数微单位及币种，报告再换算人民币；模型版本和价格表版本一起记录。不存在或不可读的文件用显式读取失败值表示，不能伪造成空文件。

scopeIntent 只允许 finding、file、directory、pattern、repository，默认 finding；kind 区分明确有用、明确误报、dismiss、resolved、采用推断与长期规则指令。

任务 01 同时定义 Fixture 工厂：reviewIdentity、runSnapshot、repoSnapshot、candidate、feedbackInput、memoryVersion、evolutionMetrics、evaluationReport。每个工厂支持 Partial 参数，默认属于同一测试租户与固定 SHA，测试使用显式覆盖制造不同租户、版本和边界条件。

测试脚本统一约定：

~~~text
pnpm exec turbo run test:unit
pnpm exec turbo run test:integration
pnpm exec turbo run test:e2e
pnpm exec turbo run typecheck
pnpm exec turbo run lint
pnpm exec turbo run build
pnpm exec turbo run eval:run -- --dataset <version> --pipeline <version>
pnpm exec turbo run eval:compare -- --baseline <report> --candidate <report>
~~~

所有任务用项目本地 Turbo 执行，pnpm exec 仅用于找到该二进制。各 package 的单元测试使用 Vitest run，单任务命令为 pnpm exec turbo run test:unit --filter=@reviewpilot/<package> -- <仓库根目录相对测试路径>；跨包集成测试用 pnpm exec turbo run test:integration --filter=@reviewpilot/tooling -- <测试路径>。test:e2e 与 eval:* 仍是后续阶段约定，尚未创建；预计输出指对应任务完成后的结果。

## 四、逐阶段实施任务

每个任务采用测试先行：写下行为 → 确认失败 → 实现接口 → 验证通过 → 提交。Git 初始化纳入任务 01；本次文档交付不初始化或提交应用仓库。

任务中的 src/、tests/、migrations/ 简写路径属于紧邻其前的 package；跨包文件均写完整路径。每个任务消费第三节的共用契约与此前任务的输出，不能另起重复接口。任务 05–08 可先由 Diff 构建最小 ContextBundle；任务 09–10 在同一契约内增强检索。

关键依赖：02→03→04；05→06→07→08；09→10→11；12→13→14→15；11→16→17；18/19 依赖此前持久化与网关；20→21→22。任务 01 给所有后续任务提供工程与类型；依赖箭头表示推进顺序，不授权现在开始执行。

### S1：GitHub 与任务基础

#### 任务 01：可运行工程、领域契约与选型锁定

**文件：** 新建 package.json、pnpm-workspace.yaml、turbo.json、packages/tooling/package.json、tsconfig.base.json、apps/web/package.json、apps/web/src/app/api/health/route.ts、packages/domain/src/types.ts、packages/domain/tests/run-policy.test.ts、tests/fixtures/index.ts、docs/adr/0001-runtime-and-dependencies.md。

**接口：** canTransitionRun(from: RunStatus, to: RunStatus): boolean；GET /api/health 返回应用存活状态，不包含凭证。输出第三节的共用类型与 Fixture 工厂。

- [ ] 写 canTransitionRun 的失败测试：Queued 可以进入 Indexing；Stale/Cancelled 不能进入 Publishing。Web 健康接口返回 200。
- [ ] 运行 pnpm exec turbo run test:unit --filter=@reviewpilot/domain -- packages/domain/tests/run-policy.test.ts，确认尚无实现导致失败。
- [ ] 核实并锁定 Node 当前受支持 LTS、pnpm、Turborepo、Next.js/React、PostgreSQL、Vitest/Playwright、参数化 SQL 驱动、Schema 库与模型适配方式；记录官方依据、版本与选择理由，创建最小 Web 与两个 Worker 入口及统一 Turbo 任务。
- [ ] 实现契约和状态约束；运行 pnpm exec turbo run test:unit、pnpm exec turbo run typecheck、pnpm exec turbo run build，期望通过；本地健康接口可访问。
- [ ] 初始化项目 Git，按明确文件列表提交工程与 ADR；不提交 .env、缓存和凭证。

**完成条件：** 同一运行时可启动 Web 与空 Worker，领域约束可测试，lockfile 固定依赖。不开通付费服务。

#### 任务 02：PostgreSQL 持久化与可恢复任务

**文件：** 新建 packages/persistence/src/jobs.ts、src/review-runs.ts、migrations/0001-review-core.sql、tests/jobs.integration.test.ts、apps/worker-review/src/main.ts、compose.dev.yml。

**接口：** enqueueReview(identity: ReviewIdentity, snapshot: RunSnapshot): Promise<{ runId: string; created: boolean }>；claimJob(workerId: string, now: Date): Promise<JobLease | null>；completeJob(lease: JobLease): Promise<void>。JobLease 包含 jobId、leaseToken、expiresAt。

- [ ] 写事务与租约测试：相同自动键只创建一个 Run；两个 Worker 不同时获得同一有效租约；过期任务可重领，旧 leaseToken 不能完成任务。
- [ ] 运行 pnpm exec turbo run test:integration --filter=@reviewpilot/tooling -- tests/integration/s1.test.ts，确认失败；S1 的队列测试实际统一在该跨包文件中。
- [ ] 创建安装、仓库、PR、Run、stage、delivery、job 基础表；事务内落 Run 与任务；加入重试、心跳、检查点和唯一约束。
- [ ] 同时注入事务中途失败，期望没有孤立任务；数据库重启后可恢复未完成 Run。运行对应集成测试。
- [ ] 仅提交迁移、持久化与 Worker 相关文件。

**完成条件：** 队列按至少一次交付工作，重领不会重复领域结果，开发数据库可创建与重建。

#### 任务 03：GitHub 登录、安装与仓库管理

**文件：** 新建 packages/github/src/auth.ts、src/installation-access.ts、tests/access.test.ts；apps/web/src/app/auth/github/route.ts、auth/github/callback/route.ts、installations/callback/route.ts、repositories/page.tsx；packages/persistence/migrations/0002-installations.sql。

**接口：** requireRepoAccess(userId: string, tenant: TenantRef, action: "view" | "manage"): Promise<RepoAccess>；getInstallationToken(tenant: TenantRef): Promise<短期服务端令牌>。令牌类型仅在 github 模块内部使用。

- [ ] 写失败测试：伪造安装回调、无效 OAuth state、非维护者修改仓库、非该安装仓库访问均拒绝；公共仓库可启用，私有仓库不可启用。
- [ ] 运行 pnpm exec turbo run test:unit --filter=@reviewpilot/github -- packages/github/tests/access.test.ts，确认失败。
- [ ] 实现服务端会话、CSRF 防护、安装归属校验、短期令牌与仓库启用；禁止令牌进入浏览器或持久化日志。
- [ ] 使用 GitHub fake server 验证授权变化；在用户配置的专用测试安装完成一次登录与仓库启用。
- [ ] 提交认证、权限和仓库页面；只提交 .env.example 的变量名，不提交实际密钥。

**完成条件：** 用户可安装并启用一个公共仓库，身份与仓库管理权限独立验证。

#### 任务 04：Webhook、状态同步与基础 Check

**文件：** 新建 packages/github/src/webhook.ts、src/checks.ts、tests/webhook.test.ts；apps/web/src/app/api/github/webhook/route.ts；packages/persistence/src/webhook-deliveries.ts、tests/webhook.integration.test.ts。

**接口：** verifyWebhook(rawBody: Uint8Array, signature: string, secret: string): boolean；acceptDelivery(deliveryId: string, event: string, payload: unknown): Promise<{ accepted: boolean; jobId?: string }>；updateCheck(runId: string, status: RunStatus): Promise<void>。

- [ ] 写失败测试：原始 Body 改一字节验签失败；同 Delivery 不重入队；无关事件不审查；卸载与私有化事件取消受影响任务。
- [ ] 运行对应 webhook 单元与集成测试，确认失败。
- [ ] 实现原始体验签、事件筛选、事务持久化、短稳定窗口和 Check 状态；安装变更原子更新可访问状态。
- [ ] 专用 PR 从 Queued 到 Completed；重复 delivery 不重复执行；乱序事件不得把旧 SHA 恢复成当前 SHA。
- [ ] 提交 Webhook、Check 与事件处理。

**阶段验收：** 真 GitHub 事件到达后，任务与 Check 工作；此阶段不需要模型产生问题。

### S2：最小 AI Review

#### 任务 05：Diff、代码位置与受限读取

**文件：** 新建 packages/github/src/pull-requests.ts；packages/repository/src/read-file.ts、src/diff.ts、tests/diff.test.ts、tests/read-file.test.ts；tests/fixtures/pr-diffs/。

**接口：** loadPullRequest(identity: ReviewIdentity): Promise<{ identity: ReviewIdentity; files: ChangedFile[] }>；readAtSha(tenant: TenantRef, sha: string, path: string): Promise<FileReadResult>；validateDiffLocation(ref: CodeRef, diff: ParsedDiff): boolean。

- [ ] 写失败测试：新增/删除/重命名、多行 CRLF、文件末尾无换行、缺失 patch 与分页；伪造行号拒绝；越界路径和仓库外链接不可读取。
- [ ] 运行 repository 的 diff.test.ts 与 read-file.test.ts，确认失败。
- [ ] 实现固定 SHA 读取、Diff 行映射、分页与部分覆盖记录；解析器有大小、时间和文件数上限。
- [ ] 以已知 Fixture 验证每个可发布位置；超限 PR 明确 Partial，不伪造完整覆盖。
- [ ] 提交读取和 Diff 实现及 Fixtures。

#### 任务 06：模型网关、Schema、脱敏与费用预留

**文件：** 新建 packages/model-gateway/src/gateway.ts、src/provider.ts、src/redact.ts、tests/gateway.test.ts；packages/persistence/src/budgets.ts、migrations/0003-model-budgets.sql、tests/budgets.integration.test.ts。

**接口：** callModel<T>(request: ModelRequest<T>): Promise<ModelResult<T>>；ModelRequest 包含 tenant、purpose、schema、redactedContext、modelConfigVersion、maxCostMicros。reserveBudget(tenant: TenantRef, purpose: ModelPurpose, maxCostMicros: number, priceVersion: string): Promise<BudgetReservation>；settleBudget(reservationId: string, actualCostMicros: number): Promise<void>。

- [ ] 写失败测试：无效 JSON、Schema 越界、超时、有界重试、Secret 脱敏；两个并发调用不能都拿走同一余额；费用不得漏算失败重试。
- [ ] 运行模型单元与预算集成测试，确认失败。
- [ ] 实现一个经过选型验证的 Provider 及 fake Provider；所有调用先预留费用，记录价格版本、用途、耗时和请求版本。
- [ ] 模型失败只有有限重试；没有合格备用模型时明确失败；日志不包含凭证或原始 Secret。
- [ ] 提交模型网关、预算表和脱敏实现。

**完成条件：** 后续代码只调用模型网关，Review 与进化共享总预算。

#### 任务 07：候选、基础验证与可查看结果

**文件：** 新建 packages/review-engine/src/generate.ts、src/verify.ts、src/rank.ts、src/pipeline.ts、tests/pipeline.test.ts；packages/persistence/src/findings.ts、migrations/0004-findings.sql；apps/web/src/app/repositories/[repoId]/pulls/[number]/page.tsx。

**接口：** generateCandidates(context: ContextBundle): Promise<Candidate[]>；verifyCandidate(candidate: Candidate, context: ContextBundle): Promise<VerificationResult>；buildPublicationPlan(runId: string, candidates: Candidate[], verified: VerificationResult[], snapshot: RunSnapshot, remainingCommentSlots: number): PublicationPlan。

- [ ] 写失败测试：模型提出不存在的函数、错误 SHA、重复根因、旧问题与空失败场景均不发布；零候选正常 Completed。
- [ ] 运行 pnpm exec turbo run test:unit --filter=@reviewpilot/review-engine -- packages/review-engine/tests/pipeline.test.ts，确认失败。
- [ ] 实现候选与独立验证阶段，确定性核验位置/引用/租户并持久化过滤原因；只保留 verified，默认上限 3。
- [ ] PR 页面能显示已验证结果、失败原因、覆盖范围和模型费用；分数显示可信等级，不冒充准确概率。
- [ ] 提交引擎、Finding 存储与页面。

#### 任务 08：GitHub 发布与远端对账

**文件：** 新建 packages/github/src/publication.ts、tests/publication.test.ts；packages/persistence/src/publication-records.ts、migrations/0005-publications.sql；tests/integration/publication.integration.test.ts。

**接口：** publishReview(plan: PublicationPlan): Promise<PublicationReceipt>；reconcilePublication(runId: string): Promise<PublicationReceipt | null>。Receipt 保存远端 Review/Comment/Check ID 与发布状态。

- [ ] 写失败测试：远端成功后丢响应，再跑任务只有一份结果；PR 新 SHA、安装撤销和仓库私有化阻止发布；同 PR 新 Run 不重复根因。
- [ ] 运行 github publication.test.ts 与 publication.integration.test.ts，确认失败。
- [ ] 发布前重验 SHA/权限，使用根因指纹与发布记录对账；一批结果共享三条问题上限，摘要与行级评论不重复列出同一问题。
- [ ] 验证无有效锚点但有充分证据时转摘要并占用额度；发布后发现 SHA 变化则标 Stale，不能声称消除了所有外部竞态。
- [ ] 提交发布、对账与测试。

**阶段验收：** 一个真 PR 完成模型审查、Web 结果和 0–3 条发布；费用、引用、SHA、过滤结果可查。

### S3：上下文与反证

#### 任务 09：增量 Snapshot 与 JS/TS 分析

**文件：** 新建 packages/repository/src/snapshot.ts、tests/snapshot.test.ts；packages/analyzer/src/typescript.ts、src/imports.ts、tests/analyzer.test.ts；packages/persistence/migrations/0006-snapshots.sql。

**接口：** buildSnapshot(identity: ReviewIdentity): Promise<RepoSnapshot>；analyzeFile(snapshot: RepoSnapshot, path: string): Promise<FileAnalysis>。Analysis 输出符号、Import、类型引用与明确的近似关系。

- [ ] 写失败测试：同路径不同 SHA 不复用旧内容；未变文件可复用内容哈希；TSX、路径别名、重命名与语法错误有明确结果。
- [ ] 运行 snapshot 与 analyzer 测试，确认失败。
- [ ] 用 TypeScript 静态能力索引直接关系，按需读取配置；不加载插件、原生扩展、项目脚本或执行代码。
- [ ] 解析失败和资源超限记录局部覆盖，整个仓库不因一个坏文件失去状态；近似调用不能标成运行时证明。
- [ ] 提交 Snapshot、分析器和迁移。

#### 任务 10：最小 Context、可信规则与反证

**文件：** 新建 packages/retrieval/src/context.ts、src/trusted-rules.ts、tests/context.test.ts、tests/trusted-rules.test.ts；修改 packages/review-engine/src/verify.ts；新建 tests/context-verification.test.ts。

**接口：** retrieveContext(snapshot: RepoSnapshot, changes: ChangeUnit[], memorySetVersion: string): Promise<ContextBundle>；loadTrustedRules(tenant: TenantRef, trustedRulesSha: string): Promise<MemoryVersion[]>。

- [ ] 写失败测试：PR 规则文件自我豁免不生效；跨仓库引用导致 Run 失败；有上游权限校验的 Trap 被反证拒绝；未来修复不进入历史 Context。
- [ ] 运行 retrieval 两项测试与 context-verification.test.ts，确认失败。
- [ ] 先按符号、路径、Import、测试和 Scope 检索，限制 Token；规则只读可信已合并版本；同级冲突暂停相应规则。
- [ ] 扩展验证检查上游保护、Base/Head 归因、业务前提与缺失上下文；断言 Evidence 来自真实读取结果。
- [ ] 提交上下文与规则实现。

**完成条件：** 语义向量检索可作为后续增强，首版不因缺少向量服务停工。

#### 任务 11：阶段检查点、Trace 与 20 个开发案例

**文件：** 新建 packages/artifacts/src/store.ts、tests/store.test.ts；packages/observability/src/trace.ts；修改 review-engine/src/pipeline.ts；新建 tests/checkpoint.test.ts、eval/manifests/dev-v0.json 与相关 Case；扩展 PR 页面。

**接口：** saveCheckpoint(checkpoint: StageCheckpoint): Promise<void>；resumeRun(runId: string): Promise<StageCheckpoint[]>；readArtifact(tenant: TenantRef, artifactRef: string): Promise<Artifact>。

- [ ] 写失败测试：验证阶段崩溃后只恢复未完成阶段；输入哈希或 Pipeline 变化不能复用旧 Artifact；其他租户不能读取 Trace。
- [ ] 运行 artifact 与 checkpoint 测试，确认失败。
- [ ] 保存分阶段输入/输出与脱敏决策记录，PR 页面加入 Filtered/Trace/Memory used；不展示隐藏思维链。
- [ ] 建立至少 20 个开发案例，包含跨文件、已有校验 Trap 和错误规则；固定预算比较 Diff-only 与 Context 结果，不要求预设上下文一定胜出。
- [ ] 提交阶段恢复、界面与案例。

**阶段验收：** 上下文检索可追溯，旧问题与反例过滤可演示，崩溃恢复不重复发布。

### S4：自进化与人工纠错

#### 任务 12：Memory 不可变版本、Scope 与优先级

**文件：** 新建 packages/domain/src/scope.ts、src/memory-policy.ts、tests/memory-policy.test.ts；packages/persistence/src/memories.ts、migrations/0007-memories.sql、tests/memories.integration.test.ts。

**接口：** matchesScope(memory: MemoryVersion, path: string): boolean；resolveMemories(memories: MemoryVersion[], path: string): Resolution；appendMemoryVersion(input: MemoryVersion, expectedCurrentVersion: number): Promise<MemoryVersion>。Resolution 包含 applicable、conflicts。

- [ ] 写失败测试：migration 例外覆盖同级宽范围规则；人工锁定优先；两个同级相反规则产生 conflict；并发版本修改只能一个成功。
- [ ] 运行领域与 Memory 集成测试，确认失败。
- [ ] 实现五类 Memory、版本指针、范围、来源、反例与可信规则同步；人工并发修改返回版本冲突，不丢更新。
- [ ] 对暂停/锁定/退役保持历史；文件规则 Web 只读，平台策略不能被 Memory 覆盖。
- [ ] 提交 Memory 领域与持久化。

#### 任务 13：反馈、人工修订与撤销

**文件：** 新建 packages/evolution/src/feedback.ts、src/correction.ts、tests/feedback.test.ts、tests/correction.test.ts；packages/persistence/migrations/0008-feedback.sql；apps/web/src/app/api/findings/[findingId]/feedback/route.ts 与反馈控件。

**接口：** recordFeedback(input: FeedbackInput): Promise<FeedbackEvent>；applyCorrection(input: CorrectionInput, expectedVersion: number): Promise<CorrectionResult>；undoCorrection(eventId: string, actorId: string): Promise<CorrectionResult>。

- [ ] 写失败测试：非维护者不能修订；重复反馈幂等；未回复未知；resolved 只是弱信号；默认只改 Finding；显式目录修订不影响其他目录。
- [ ] 运行 evolution 的 feedback.test.ts 与 correction.test.ts，确认失败。
- [ ] 存储明确标签与行为推断，按 PR/根因合并观察；创建新 Memory 版本或例外；撤销生成反向事件，不删除审计历史。
- [ ] 将设计文档 5.4 的信号权重固化为可版本化配置；同一 PR 去重后用于候选排序，不把 resolved 或高权重本身转换为正确标签。
- [ ] Web 展示范围与影响预览、异步 Replay 状态；低风险人工长期指令可直接生效，但锁定优先。
- [ ] 提交反馈、纠错、撤销与界面。

**补充契约：** CorrectionInput 为 FeedbackInput 的明确长期修订，加 memoryId、statement、include/exclude、lock；CorrectionResult 返回 eventId、newVersion、state、replayJobId。

#### 任务 14：自动候选归纳与最小 A/B Replay

**文件：** 新建 packages/evolution/src/propose.ts、tests/propose.test.ts；packages/evaluation/src/replay.ts、tests/replay.test.ts；apps/worker-evolution/src/main.ts；packages/persistence/migrations/0009-evaluation.sql。

**接口：** proposeMemories(tenant: TenantRef, observations: FeedbackEvent[]): Promise<MemoryVersion[]>；runReplay(spec: ReplaySpec): Promise<{ baseline: EvaluationReport; candidate: EvaluationReport }>。

最小 Replay 先读取任务 11 的 Development manifest 与独立人工标签，使用 EvaluationInput/GoldenLabels/CaseScore 的初版契约。任务 16 扩展为隔离的数据集加载器，任务 17 增加 Judge 与正式门禁，不重新实现第二套 Replay。

- [ ] 写失败测试：同 PR 多个信号只算一次；支持代码惯例不能直接 Active；生成候选用过的案例不能再算独立验证；Replay 无论结果如何都不发布 GitHub。
- [ ] 运行候选与 Replay 测试，确认失败。
- [ ] 通过网关生成范围明确、带支持/反证与验证计划的候选；以固定时点与版本比较有/无 Memory，先使用少量人工标签。
- [ ] 标签不足输出 insufficient-data，保留 Shadow；普通候选自主运行验证，不等待逐条审批。
- [ ] 提交候选、Replay 与 Evolution Worker。

#### 任务 15：Shadow、自动激活、隔离与回滚

**文件：** 新建 packages/evolution/src/lifecycle.ts、src/monitor.ts、tests/lifecycle.test.ts；packages/persistence/src/memory-set.ts、tests/activation.integration.test.ts。

**接口：** decideActivation(memory: MemoryVersion, metrics: EvolutionMetrics): ActivationDecision；activateVersion(memoryId: string, version: number, expectedVersion: number): Promise<void>；evaluateLiveHealth(memory: MemoryVersion, observations: FeedbackEvent[], now: Date): HealthDecision。

- [ ] 写边界测试：9 个标签、2 个反例、4 次 Shadow、0.84 分均不激活；3 强支持、0.85 分、5 Shadow、10 标签/3 反例、80% 有效率且无高风险回归可激活。
- [ ] 运行 lifecycle 与 activation 测试，确认失败。
- [ ] 实现候选到 Shadow/Active；发布 Run 只使用冻结 Active 集合；自动激活若版本被人工锁定/更改则失败并重新评估。
- [ ] 初期三个 Evolution 模式共用全局最低证据门槛；其差别只体现在低风险规则额外观察时间：Conservative 7 天、Balanced 24 小时、Autonomous 无额外等待。时间可配置，但不能替代标签、反例与 Shadow 次数。
- [ ] 测试前 5 个明确标签中 2 个误报触发隔离、有效率 <50% 回退、无合格前版本保持暂停、90 天未命中待复核；禁止自动取消安全保护。
- [ ] 提交生命周期、集合冻结和监控。

**阶段验收：** 人工标记为合成的演示数据跑通纠错、学习、Replay、Shadow、自动生效与撤销；真实低活跃仓库仍遵守样本门槛。

### S5：评测与运行可靠性

#### 任务 16：Golden 数据集与标签隔离

**文件：** 新建 packages/evaluation/src/dataset.ts、tests/dataset.test.ts；eval/cases/、eval/labels/、eval/manifests/golden-v1.json；docs/adr/0002-evaluation-data.md。

**接口：** loadEvaluationInput(caseId: string, datasetVersion: string): Promise<EvaluationInput>；loadGoldenLabels(caseId: string): Promise<GoldenLabels>。前者只供引擎，后者只供评分器。

- [ ] 写失败测试：Review 输入不含 labels/未来修复；同来源高度重复案例不能跨拆分泄露；无来源或许可说明的真实 Case 不纳入公开数据。
- [ ] 运行 evaluation 的 dataset.test.ts，确认失败。
- [ ] 扩展到约 50 个 Case：20 真实问题、10 跨文件、15 Trap、5 中性；按 60/20/20 分配并固定 manifest。
- [ ] 每个 Case 保存固定 SHA/配置、根因、场景、可接受位置和禁止声明；少量 Holdout 只作回归，不宣称统计代表性。
- [ ] 提交数据集、许可记录和输入隔离代码。

#### 任务 17：评分、指标与发版门禁

**文件：** 新建 packages/evaluation/src/judge.ts、src/metrics.ts、src/gate.ts、tests/metrics.test.ts、tests/gate.test.ts；scripts/eval-run.ts、scripts/eval-compare.ts；docs/reports/ 首份结果。

**接口：** scoreReview(input: EvaluationInput, labels: GoldenLabels, findings: Candidate[]): Promise<CaseScore>；computeMetrics(scores: CaseScore[]): EvaluationReport；evaluateRelease(baseline: EvaluationReport, candidate: EvaluationReport): GateDecision。

- [ ] 写失败测试：零评论精度为 null，不是 100%；未知不进精度分母但降低覆盖率；重复根因计噪声；新增未标注问题进入人工复核；缺失关键缺陷阻止发布。
- [ ] 运行 metrics/gate 测试，确认失败。
- [ ] 确定性核验引用与位置，语义 Judge 辅助根因匹配；高风险与不确定样本人工复核；输出样本量、区间、已知召回、噪声与完整调用成本。
- [ ] 固定关键回归 Case，不允许候选新增遗漏；执行 75% 精度、95% 引用、3% 重复和费用/延迟目标；没有足够标签时门禁“未完成”，不能自动通过。
- [ ] 运行 eval:run 与 eval:compare，生成可读报告并提交；把报告门禁接入自动 Memory 验证与全局版本发布。

**完成条件：** 固定数据和版本可重放，门禁支持失败原因。模型精度属于实测结果，不用测试 mock 伪造。

#### 任务 18：Artifact 保留、仓库删除与恢复防复活

**文件：** 新建 packages/artifacts/src/retention.ts、src/deletion.ts、tests/deletion.test.ts；packages/persistence/migrations/0010-deletion-ledger.sql；apps/web/src/app/api/repositories/[repoId]/delete/route.ts；docs/runbooks/data-deletion.md。

**接口：** requestRepositoryDeletion(tenant: TenantRef, actorId: string): Promise<{ deletionId: string }>；executeDeletion(deletionId: string): Promise<DeletionReceipt>；applyDeletionLedgerAfterRestore(): Promise<void>。

- [ ] 写失败测试：删除立即阻止新任务和 Artifact 访问；在线记录、向量、缓存和对象全部清理；恢复含旧数据备份后仍不可访问已删内容。
- [ ] 运行 artifacts deletion.test.ts 与数据库集成测试，确认失败。
- [ ] 实现 30/90 天与 12 个月保留任务、24 小时在线清理目标、备份最长 30 天策略和无源码删除凭据；关闭保存时说明 Replay 降级。
- [ ] 通过测试恢复演练验证删除凭据重放；GitHub 既有评论处理与平台清理分开说明。
- [ ] 提交删除、保留与运行手册。

#### 任务 19：并发、故障与安全集成门禁

**文件：** 新建 tests/integration/failure-matrix.test.ts、tests/integration/tenant-isolation.test.ts、tests/integration/budget-race.test.ts；docs/runbooks/recovery.md、docs/adr/0003-threat-boundaries.md。

**接口：** 本任务不增加另一套业务接口，使用 02/04/06/08/10/15/18 的现有契约；新增可注入 Clock、GitHub fake server 与故障点。

- [ ] 为重点复核五类输入写跨模块测试，并覆盖 429、模型超时/无效 Schema、租约竞争、Worker 崩溃、PR 乱序更新、预算并发及未授权纠错。
- [ ] 运行 pnpm exec turbo run test:integration，确认真实集成缺陷对应失败，而不是只验证 mock 被调用。
- [ ] 修复所有发现的问题；分析器资源上限、Prompt Injection 与 Secret 输出验证需有确定性断言。
- [ ] 报告 P90 延迟、平均/P95 成本，标出队列与后台学习开销；未达目标调整额度和覆盖，不跳过验证。
- [ ] 提交补丁、故障矩阵和恢复手册。

**阶段验收：** 评测报告、租户隔离、预算与恢复通过；不满足门禁时停在 Alpha 并记录阻塞。

### S6：治理界面与受控 Beta

#### 任务 20：基础治理页面与设置

**文件：** 新建 apps/web/src/app/repositories/[repoId]/memory/page.tsx、evaluation/page.tsx、activity/page.tsx、settings/page.tsx、apps/web/src/app/page.tsx；packages/persistence/src/queries.ts；tests/e2e/console.spec.ts。

**接口：** 在 /api/repositories/[repoId] 下提供 overview、memory、memory/[memoryId]、evaluation、activity、settings 的 GET 路由；settings 使用 PATCH。Memory 锁定/暂停/退役使用 POST memory/[memoryId]/actions，纠错沿用任务 13；所有写操作复用 requireRepoAccess 和领域命令，不在页面构造 Prompt。

- [ ] 写 E2E 失败场景：无标签时显示“数据不足”；非维护者不能改规则；纠错范围预览与撤销；锁定后自动版本不覆盖；Partial/Stale 与无 Finding 明确区分。
- [ ] 运行 pnpm exec turbo run test:e2e -- tests/e2e/console.spec.ts，确认失败。
- [ ] 实现必要表格、版本 Diff、来源链接、任务状态与费用；Conservative/Balanced/Autonomous 共用不可放宽的权限约束。
- [ ] 设置可限制 Draft/Bot、目录、评论上限、预算、保存和触发；默认不自动批准/Request Changes，不展示尚未实现的开关。
- [ ] 提交界面与 E2E；验证键盘操作、基本移动端可读性和明确的删除确认。

**完成条件：** Memory 人工治理与自动变化可见。复杂趋势图、向量搜索和批量操作延后。

#### 任务 21：部署、监控与完整演练

**文件：** 新建 Dockerfile.web、Dockerfile.worker、.github/workflows/ci.yml、docs/runbooks/deployment.md、docs/runbooks/budget-alerts.md、tests/e2e/github-smoke.spec.ts。

**接口：** Web readiness 检查数据库可用；Worker 报心跳、队列年龄与失败计数；审查/进化费用分别计量。

- [ ] 写失败测试：缺必需配置时拒绝启动且日志不含值；数据库不可用 readiness 不健康；预算 80% 预警，100% 不创建新调用。
- [ ] 运行本地容器健康检查与对应测试，确认失败。
- [ ] 配置 Web、两个 Worker、数据库及 Artifact 存储部署说明；CI 运行 lint/typecheck/build、单元/集成与固定回归，不在任意外部 PR 中暴露部署凭证。
- [ ] 在用户提供并授权的专用环境部署，再跑安装 → PR → 评论 → 纠错 → Replay → 生效/回滚；备份恢复和数据删除演练。
- [ ] 提交部署文件、CI 与运行手册，记录实际环境与回滚版本。

**边界：** 本计划不创建云账户、不购买服务、不发送邀请。缺少凭证时本地验证可完成，线上验收保持待执行。

#### 任务 22：开源发布材料与试用验收

**文件：** 修改 README.md；新建 CONTRIBUTING.md、SECURITY.md、CODE_OF_CONDUCT.md、LICENSE、docs/roadmap.md、docs/reports/beta-readiness.md、examples/rules.yml。

**接口：** 不新增业务 API；交付版本化安装说明、贡献流程、配额、数据政策和反馈入口。

- [ ] 按用户实际安装流程逐项检查 README、权限、配置、限额与删除入口；文档中的每个命令必须在干净开发环境执行。
- [ ] 核查开源许可与依赖兼容性；把 Demo 中合成样本和真实标签分开。
- [ ] 汇总 Golden 报告、故障测试、未支持生态、成本/延迟与恢复结果；所有 P0 完成后标记可试用。
- [ ] 先使用自身仓库，再由维护者主动安装到约 5 个友好仓库；达到稳定条件后扩大到 10–20 个，邀请与公开发布需用户授权。
- [ ] 提交文档并准备 v0.1 发布说明；只记录实测数字，不把目标写成成果。

**阶段验收：** 交付可安装的受控 Beta，界面与后台功能匹配，尚未满足的质量与线上观察条件公开说明。

## 五、P1 增强任务

以下任务不阻挡首个受控 Beta：

| 增强 | 前置任务 | 拟文件 | 独立验收 |
| --- | --- | --- | --- |
| Patch 生成与预览 | 07、10、19 | review-engine/src/suggest-patch.ts；Web Patch 侧栏 | 固定 SHA、可应用格式、标注未经运行验证、不自动写仓库 |
| 多模型版本对比 | 06、16、17 | evaluation/src/model-comparison.ts；Evaluation 比较页 | 相同输入/预算/标签与价格版本，报告不混变量 |
| 语义向量检索 | 09、10、17 | retrieval/src/semantic.ts；向量迁移 | 与路径检索 A/B，按租户隔离且删除同步 |
| 规则导入向导 | 12、13、20 | retrieval/src/import-rules.ts；规则预览页 | 可信已合并来源、结构化预览、维护者确认导入范围 |
| 完整图表与批量治理 | 17、20 | Overview/Evaluation 图表与批量 API | 小样本提示、无标签显示未知、批量操作审计与权限 |
| 自动 Request Changes | 08、17、19 | github/src/review-decision.ts | 明确仓库开关、严重度校准、可撤销策略；默认关闭 |

多 Agent、跨语言、MCP、IDE/CLI、自动修复 PR、Kubernetes 和跨仓库 Knowledge 在 Beta 用户价值得到验证后另写子方案。

## 六、阶段退出门槛与进度调整

| 情况 | 调整 |
| --- | --- |
| GitHub 安装/权限没有通过 | 停在 S1，使用本地 fake server 继续契约工作，不声称线上闭环完成 |
| Diff-only 噪声高 | 优先 S3 上下文与反证，不扩展更多检查类别 |
| 自动规则标签不足 | 保持 Shadow，人工纠错可用，不强行自动激活 |
| 平均成本超目标 | 缩小支持 PR 范围与免费额度，分别统计后台成本 |
| 回归集出现严重遗漏 | 阻止新版本，保留原版本并新增失败案例 |
| 六周内 P0 未完成 | 延迟公开 Beta，压缩 P1；完整性优先于按时宣布 |
| 用户数据少、留存观察不足 | 延长试用观察，不将安装量和 Star 当作有效率替代 |

计划执行以阶段验收报告推进。每个阶段记录完成任务、测试结果、真实演示、费用、未解决项与下一阶段输入，不以“代码写完”代替验收。

## 七、需求到任务的追踪

| 设计部分 | 实施任务 |
| --- | --- |
| 产品安装、公共仓库与 Review 闭环 | 03–08、20–22 |
| 高精度、证据、反证与 Trace | 05–11、16–17 |
| Web 信息架构、状态与反馈 | 03、07、11、13、20 |
| 模块边界、任务幂等、冻结版本与外部对账 | 01–02、04、08–12、19 |
| 自进化、人工纠错、范围、优先级、锁定与回滚 | 12–15、17、20 |
| Golden、标签隔离、Judge 与发布门禁 | 11、14、16–17、19 |
| 身份、租户、恶意规则、静态分析安全 | 03–06、09–10、13、19 |
| 成本预留、配额、日志与监控 | 06、11、17、19–21 |
| 保存、数据删除与备份恢复 | 11、18、21 |
| 开源交付、受控 Beta 与真实价值验证 | 21–22 |
| Patch、多模型、向量增强与图表 | 第五节 P1，明确延后 |

## 八、执行记录与交接

- [ ] S1：安装、Webhook 与基础 Check 验收。
- [ ] S2：最小 Review 与外部发布对账验收。
- [ ] S3：上下文、反证、Trace 与恢复验收。
- [ ] S4：纠错、自动学习、Shadow 与回滚验收。
- [ ] S5：固定评测、预算、隔离与删除验收。
- [ ] S6：完整演练、开源材料与受控 Beta 验收。

每个任务提交前运行其指定检查；提交按该任务文件范围进行，不使用包含无关文件的批量暂存。已经通过的检查只在依赖修改或出现新失败时重跑。

推荐先逐阶段直接实施，理由是认证、Run、Evidence、Memory 与评测之间接口依赖较强，当前更适合先稳定契约。若用户希望按任务使用独立实现与审查代理，可在启动实施时明确选择；本次没有派发代理任务。

计划初稿自检记录：已覆盖设计中的 P0、明确 P1、固定共有接口和测试归属，给出了五类重点失败输入的测试任务。后续已完成 S1 本地交付与 Turborepo 全量接入，具体版本及实测命令见 ADR 和 S1 交付记录；真实 GitHub 验收与 S2–S6 仍未完成，原阶段验收复选框不提前勾选。

## 附录：关键行为的测试断言

以下断言在对应任务实现时加入其指定测试文件。引用第三节的 Fixture 工厂与任务导出函数；不在当前文档交付时执行。Fixture 默认是低风险、未锁定的普通仓库记忆，满足额外观察时长；每条测试只改其关注的变量。

任务 01，终态不再发布：

~~~ts
it("stale_run_cannot_publish", () => {
  expect(canTransitionRun("Stale", "Publishing")).toBe(false);
});
~~~

任务 07，没有验证通过的问题仍正常完成：

~~~ts
it("empty_verified_set_produces_no_findings", () => {
  const plan = buildPublicationPlan("run-1", [], [], runSnapshot(), 3);
  expect(plan.findings).toHaveLength(0);
});
~~~

任务 12，Scope 的排除优先于包含：

~~~ts
it("excluded_migration_does_not_match_rule", () => {
  const rule = memoryVersion({
    scope: { include: ["packages/**"], exclude: ["packages/migrations/**"] }
  });
  expect(matchesScope(rule, "packages/migrations/001.ts")).toBe(false);
});
~~~

任务 15，冷启动标签不足不得激活：

~~~ts
it("nine_labeled_cases_remain_in_shadow", () => {
  const decision = decideActivation(
    memoryVersion({ status: "shadow" }),
    evolutionMetrics({ labeledCases: 9 })
  );
  expect(decision.kind).toBe("stay-shadow");
});
~~~

任务 15，满足普通自动规则的证据门槛：

~~~ts
it("validated_low_risk_memory_can_activate_automatically", () => {
  const decision = decideActivation(
    memoryVersion({ status: "shadow", origin: "learned", locked: false }),
    evolutionMetrics({
      independentSupport: 3, explicitInstruction: false, score: 0.85,
      shadowHits: 5, labeledCases: 10, counterexamples: 3,
      precision: 0.80, criticalRegressions: 0
    })
  );
  expect(decision.kind).toBe("activate");
});
~~~

远端对账、并发预算与删除恢复必须使用任务 08/18/19 的真实持久化或可控 fake server，不能用只返回成功的 mock 替代端到端断言。
