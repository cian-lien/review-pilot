# ReviewPilot S2 最小 AI 审查实现计划

> 执行者必须使用 superpowers:executing-plans 逐项实施，并通过下方复选框记录进度。用户已指定由当前会话直接实施，不使用 subagent 或 worktree，整阶段自行审查；这项选择覆盖技能的代理审查默认流程。

**目标：** 在现有 Go 与 TypeScript 架构上完成固定版本读取、串行候选与验证、预算治理、结果展示和可对账发布，并通过本地模拟闭环。

**架构：** Go 独占业务数据库、GitHub 接入、预算、执行权、结果与发布；Review Worker 通过内部 REST 执行受控 LangGraph 流程，所有模型调用经过统一网关。Web 同源直连 Go，没有 BFF。模拟与真实模式分别记录，真实调用及发布默认关闭。

**技术栈：** Go、Gin、GORM、PostgreSQL、RabbitMQ、TypeScript、Node.js、LangChain、LangGraph、Next.js、TanStack Query、现有 UI 与 OTel、OpenAPI、JSON Schema、Turborepo。

**设计依据：** [已确认的 S2 设计](docs/superpowers/specs/2026-10-11-reviewpilot-s2-ai-review-design.md)。执行前同时阅读 [AGENTS.md](AGENTS.md) 和 [S1 交付记录](docs/reports/s1-go-migration.md)。

日期：2026-10-11。状态：用户已批准执行，实施中。真实模型调用、真实 GitHub 写入与业务数据库迁移仍未授权。

## 全局约束

- 只执行 S2，完成后停止。S3 上下文增强、S4 人工纠错与自进化不提前实现。
- 在现有 checkout 实施，保留用户已有变更；不合并、不推送，不创建 worktree 或 subagent。当前基线为 6284989 所在 feat/s1-go-migration，执行时重新确认实际 HEAD，计划提交不计为产品实现。
- 文档中文，代码注释简洁英文，重点解释权限、幂等、预算和并发约束。
- 工程命令全部通过 pnpm exec turbo run；依赖安装由 pnpm 管理，Go 包装器可透传定向 go test 参数。不启用远程缓存，测试、代码生成和状态变更任务关闭缓存。
- 仅公共 JS/TS 仓库；不执行 PR 安装脚本、插件、测试或服务。不引入 BFF、gRPC、Redis、多 Agent、pgvector 或对象存储。
- Worker 不持业务数据库、GitHub 或 Evolution 凭证；模型凭证不进入浏览器、Run 快照、日志或 Trace。
- S1 v1、connection-check、foundation-v1 和原 modelConfigVersion=none 条件不变；新增 ai-review、消息 v2 和 review-v1，Memory 与可信规则版本为 none。
- 保留 SQL 0001–0003 内容、哈希及迁移账本，不使用 AutoMigrate；仅在本次创建的隔离数据库执行新增迁移。
- 固定 Base/Head、输入哈希及配置版本。授权失效即 Cancelled，Head 变化即 Stale；旧 leaseToken 和 epoch 不能提交或发布。
- 每个 PR 累计最多 3 个根因，行级与摘要共用额度，跨 Run 去重；正常路径一次生成、一次独立验证，零候选跳过验证。
- 每个模型阶段至多一次显式重试；重试独立预留，SDK 隐式重试为零。未知发送或收费事实保留额度，持久化阻断后 ack，不自动重新调用。
- 全局月度预算 80% 提示、100% 阻断新自动调用；仓库每日窗口 UTC。真实价格、配额或兼容能力缺失则拒绝真实调用，不自动降级为 fake。
- 默认不存原始 Prompt/Response；带源码片段产物最多 30 天，元数据与最小审计 12 个月。关闭内容保存时明确恢复降级。
- 本阶段只有模拟模型与模拟 GitHub 验收；不购买服务，不修改真实 App 权限，不迁移业务库或自动部署。不用 fake 数据证明准确率或真实费用。

## 重点复核的五类输入

| 输入或故障 | 必须行为 | 固定断言所属任务 |
| --- | --- | --- |
| PR 版本在多页读取之间改变，或 patch 与固定内容不符 | 不拼接混合版本，不发布伪锚点 | 任务 2 TestContextVersionChangesBetweenPages |
| UTF8 内容超过字节限额但字符数未超 | 流式按字节截断并拒绝，不先读入无限响应 | 任务 2 readBoundedBytes 测试 |
| 模型费用已结算但阶段产物保存前崩溃 | 不重复生成，不把结果缺失解释为未收费 | 任务 3 TestSettledCallWithoutArtifactBlocksRecovery |
| 两个 Run 同时争抢 PR 剩余评论额度，或用户复制发布标记 | 合计不超 3，复制标记不能冒充 App | 任务 6 TestReviewSlotRace 与 TestForeignMarkerIgnored |
| Worker 启动配置与业务环境混合，或快照指定其他模型地址 | 不继承数据库及 GitHub 密钥，不接受任意模型目的地址 | 任务 4 worker-launch 与 gateway 测试 |

## 任务顺序与文件职责

八个任务共同交付一条最小审查链路，顺序实施。每项以必要失败断言、最小实现、定向验证和本地提交结束。下方 Create 是拟新增，Modify 是已存在；路径相对 review-pilot 根目录。

| 任务 | 独立交付 | 主要目录与职责 |
| --- | --- | --- |
| 1 | v2 契约与安全配置可生成、可验证 | packages/contracts、apps/api/internal/config |
| 2 | 固定版本读取与可信 Diff 映射 | apps/api/internal/reviewcontext、packages/repository |
| 3 | 预算、调用事实与阶段恢复可靠 | apps/api/internal/budgets、reviewstages、retention |
| 4 | 模型网关可模拟调用并遵守调用边界 | packages/model-gateway、Worker 启动配置 |
| 5 | 串行 Review 可保存最终结果 | packages/review-engine、apps/api/internal/reviews、worker-runtime |
| 6 | Webhook 到审查与发布对账贯通 | webhooks、jobs、messaging、publisher、github |
| 7 | 中文 Web 可查看 PR 结果与费用 | apps/web/src/features/reviews 与 Go 查询 API |
| 8 | 有证据的隔离验收、运维记录与停止 | scripts、tests/integration、infra、docs |

保持 Go 单模块 reviewpilot.local/api。读取、预算、产物、结果、发布分模块，现有大 service 文件仅按受影响职责拆出新文件，不进行无关重构。新增 TS workspace 为 @reviewpilot/repository、@reviewpilot/model-gateway、@reviewpilot/review-engine；不提前创建 analyzer、retrieval 或 evaluation 空包。

## 共用配置与数据约定

以下限额是本计划的可配置工程默认值，不是吞吐或质量承诺。增大限制不能绕过预算或结果 Schema。

| 配置 | 默认值及处理 |
| --- | --- |
| 变更覆盖 | 1,500 行 additions + deletions；按规范化路径排序，超限文件整体跳过并记录原因 |
| 文件与源码 | 最多 64 个文件、单文件 128 KiB、累计原始读取 2 MiB；二进制不进模型 |
| HTTP 与产物 | 新上下文响应每页和阶段写入 256 KiB；最终 Review 写入 64 KiB；流式计数 UTF8 字节 |
| 候选与引用 | 每轮最多 10 个候选、每候选最多 5 个引用、单段展示片段最多 2 KiB；超限拒绝而非截断 JSON |
| 模型调用 | 非流式，超时 60 秒；每 purpose 最多 2 次、每 Run 合计最多 4 次；计数跨执行尝试持久化 |
| 模型容量 | 最大输入 12,000 Token、输出 4,096 Token；完整请求经适配器保守计数，无法证明界限则阻止真实调用 |
| 上下文拟合 | 固定顺序保留完整 Diff 和必要相邻代码块；无法装入的块记录 INPUT_LIMIT，不静默漏掉反证 |
| 执行权 | 沿用租约 90 秒；持续心跳每 20 秒，续租失败立即中止本地工作；最多 5 次执行尝试不扩大模型调用次数 |
| Web | 活跃 Run 每 5 秒刷新，最终状态或 needsReconciliation 停止定时刷新，保留手动刷新 |
| 清理 | 每批最多 100 条到期内容，调度进程每小时执行；到期查询即时过滤 |

REVIEW_MODE=disabled 为默认，可显式设为 fake 或 real；REVIEW_REAL_ALLOWED=false、GITHUB_REVIEW_PUBLISH_ENABLED=false 默认不允许真实行为。REVIEW_MODEL_CONFIG_FILE 指向部署者维护的非敏感版本配置，包含 modelConfigVersion、baseURL、model、structuredOutputMode、价格版本、币种、计费能力与配额。Go 与 Worker 校验同一配置版本；只有 Worker 读取 MODEL_API_KEY 或 MODEL_API_KEY_FILE。只修改 .env.example 与示例配置，不自动写用户 .env。

测试子进程统一注入 REVIEW_TEST_ISOLATED=1，Go test 包装器和 Worker launcher 在此模式不读取项目 .env 或其中的 *_FILE，只接受 harness 明确注入的临时配置。REVIEW_FAKE_SCRIPT_FILE 仅在隔离 fake 模式启用，脚本限定本次 Fixture 身份和有限响应顺序，real 模式拒绝此配置。

fake 模式必须匹配本次测试创建的 loopback 模型/GitHub 地址与合成 Fixture 身份；不能只凭“是 localhost”接受用户已有服务。真实模式中禁止从网络失败退回 fake。价格与硬限额不设虚构生产默认值，测试使用明确标记的 TEST 币种或独立模拟价格账本。

契约继续使用已有 OpenAPI 3.0.3 文件 packages/contracts/openapi/s1.yaml，避免两份 OpenAPI 生成重复 Go 类型。新增消息源 schemas/task-v2.json 投影为 ReviewTaskEnvelope，已有 TaskEnvelope 仍只投影 v1；扩展生成器同时检查两份投影，生成文件沿用 s1.gen 名称。消息 v2 仍只有通知与引用，不含代码或凭证。

| 新增 DTO | 固定字段与约束 |
| --- | --- |
| ReviewTaskEnvelope | schemaVersion=2、kind=ai-review；其余身份、generation 与追踪字段沿用 v1 的限额 |
| ReviewPolicy | 上方固定限额；仓库可在有限范围内配置，预算硬上限不受模型控制 |
| ReviewSnapshot | identity、modelConfigVersion、priceVersion、repositoryConfigVersion、generationPromptVersion、verificationPromptVersion、memorySetVersion=none、trustedRulesSha=none、policy 与 provenance |
| ReviewLease | S1 租约身份字段及 ReviewSnapshot，不复用原 snapshot 校验来放松 S1 |
| ReviewContextPage | manifestId、contextHash 可空、cursor、nextCursor、complete、files、evidence、coverage；完成全部页面并封存 manifest 后提供 contextHash，缺页不能作为完整 Context |
| ReviewChangedFile | path、previousPath、status、additions、deletions、patch 可空、Base/Head 读取事实；ParsedReviewDiff 是 repository 包解析所得的受限锚点集合 |
| EvidenceRef | evidenceId、path、sha、startLine、endLine、contentHash；CodeEvidence 另含脱敏 snippet 与读取状态 |
| ReviewCandidate | candidateId、title、severity、category、failureScenario、impact、introducedByPR、evidence、suggestedVerification、rootCauseStatement（trigger/consequence）；不含可信根因哈希 |
| VerificationDecision | candidateId、verdict=verified 或 abstain、reasonCode、evidence；每个输入候选恰有一个决策 |
| ModelCall | callId、reservationId、purpose、ordinal、requestHash、state、priceVersion、currency、reservedCostMicros、actualCostMicros、usage、revision |
| ReviewStageRecord | stage=context 或 candidates 或 verification、stageInputHash、outputHash、callId、payload、expiresAt；键不可变 |
| ReviewResult | kind=ai-review、租约与输入身份、resultId、contextHash、candidates、decisions、coverage、callIds；提交不携 resultHash，Go 核验后生成 |
| ReviewRunDetail | status、needsReconciliation、reasonCode、snapshot、findings、filtered、coverage、costs、publication；内容过期时显示 contentExpired |

全部 DTO 从契约生成，TS 通过 components['schemas'] 导出具名别名。UUID、SHA、枚举、额外字段和大小都做运行时校验。金额为数据库 int64 微单位，JSON 用非负十进制字符串，Go 检查溢出，TS 使用 BigInt 计算，避免 JavaScript 浮点与安全整数问题。TEST 仅用于隔离模拟账本，不与真实货币混合。

severity 固定 high/medium/low，category 固定 concurrency/validation/authorization/lifecycle/error-handling/data-integrity/other，purpose 固定 generate/verify。ordinal 从 1 开始且最大 2，Go 不允许 Worker 任意重置。列表与详情 DTO 为 ReviewPullList、ReviewRunList 和 ReviewRunDetail，在任务 1 定义其分页与费用字段。RootCauseKey 的 anchorHash 使用 Go 核对后的局部代码片段规范化内容，不使用整文件哈希，以免无关改动导致重发。

ReviewResult.resultId 固定为 taskId + ':review'。resultHash 与阶段 outputHash 只由 Go 对去除租约等传输字段后的规范业务内容计算；Worker 提交稳定 resultId，响应丢失可重提同一内容由 Go 比对，或只读确认任务已接受事实。GET 确认不能将未知正文声称为自己的结果，不能套用 S1 仅含身份的 ResultHash。Go 使用固定字段 DTO、encoding/json 的确定键顺序和数组原顺序计算，不含自由数值或额外字段；哈希 Fixture 固定 Unicode、转义与键序行为，TS 不重新计算业务 hash。

stageInputHash 固定为 SHA256(inputHash + '\n' + contextHash + '\n' + stage + '\n' + promptVersion + '\n' + modelConfigVersion)，无末尾换行；context 阶段 promptVersion 为 none，其余取快照内对应 Prompt 版本。Go 按持久化事实重算，TS 只按这项明确定义构造，不能用任意 JSON 序列化代替。输出 hash 始终由 Go 计算。

## REST 能力与工程入口

新 Worker 内部路由使用 /internal/review-tasks/{taskId}，保持旧 /internal/tasks 的连接接口不变。所有内容读取通过 POST 携带 lease，不将 leaseToken 放进 URL。角色只能是 review-worker，URL、请求体、数据库事实三方身份一致。

| 方法与后缀 | 请求 DTO | 结果 |
| --- | --- | --- |
| POST /claim | ClaimReviewRequest，含 envelope、workerId | ClaimReviewResponse，claimed 时含 ReviewLease，其余 outcome 沿用 S1，新增 blocked |
| POST /heartbeat | ReviewLease | 204 |
| POST /context | ReadReviewContextRequest，含 lease、cursor | ReviewContextPage |
| POST /model-calls/reserve | ReserveModelCallRequest，含 lease、purpose、ordinal、requestHash、modelConfigHash、输入/输出界限 | ModelCall，重复键同内容返回已有调用；非敏感配置内容 hash 必须匹配 Go 冻结配置 |
| POST /model-calls/{callId}/dispatch | DispatchModelCallRequest，含 lease、expectedRevision | ModelCall；只有首次授权发送响应才允许 Worker 发请求 |
| POST /model-calls/{callId}/settle | SettleModelCallRequest，含 lease、expectedRevision、用量、结果类别 | ModelCall |
| POST /stages/read | ReadReviewStageRequest，含 lease、stage、stageInputHash | ReadReviewStageResponse，found 与可用 record |
| POST /stages | SaveReviewStageRequest，含 lease、stage、stageInputHash、callId 可空、payload，不含 outputHash | CommitResponse；Go 保存计算出的 ReviewStageRecord |
| POST /result | CommitReviewRequest，含 ReviewResult | ReviewCommitResponse，含 outcome、resultHash |
| GET /result | runId、inputHash 查询参数及服务身份 | 仅已接受身份、哈希与 outcome，无源码 |
| POST /block 与 /fail | ReviewTaskActionRequest，含 lease、稳定 reasonCode | 204；block 持久化 needsReconciliation |

公开查询为 GET /api/repositories/{installationId}/{repositoryId}/pulls?cursor=...、GET /api/repositories/{installationId}/{repositoryId}/pulls/{number}/runs?cursor=...、GET /api/repositories/{installationId}/{repositoryId}/pulls/{number}/runs/{runId}。页大小 20、cursor 为 Go 生成且校验归属的游标；每次查询验证当前维护权。S2 不新增反馈、手工重跑或凭证配置 Web 写接口。

新增脚本统一使用 test:review --case contracts、context、budget、stages、results、publication 或 pipeline；contracts 是独立 TS 契约场景，context/budget/stages/results/publication 创建临时 PostgreSQL，pipeline 额外创建唯一标识 RabbitMQ 与 Collector，复用 S1 的受控 OTLP 接收器检查 Trace 和敏感数据。只注入 TEST_DATABASE_URL 等测试配置，不能继承真实凭证。

场景映射固定为 context → ./internal/reviewcontext 的 TestContext/TestReviewContext；budget → ./internal/budgets 的 TestBudget/TestSettled；stages → ./internal/reviewstages 的 TestReviewStages；results → ./internal/reviews 的 TestReviewResult/TestReviewQuery；publication → ./internal/publisher 的 TestReview/TestForeign；pipeline → ./internal/reviews 的 TestReviewPipeline。Go -run 使用锚定的对应前缀组合，-count=1；缺少命中测试、skip 或缺少本次 verdict 都失败。retention 单元由独立 Turbo 定向命令覆盖，不塞进所有数据库场景。

下方命令是实施中要建立的任务，目前并非全部存在。任务 1 建立测试选择与配置骨架，后续任务实现各自场景，不提前填充成功 verdict。

- pnpm exec turbo run codegen contracts:check --filter=@reviewpilot/contracts
- pnpm exec turbo run test:review --filter=@reviewpilot/tooling -- --case <name>
- pnpm exec turbo run test:unit --filter=@reviewpilot/model-gateway
- pnpm exec turbo run test:unit --filter=@reviewpilot/review-engine
- pnpm exec turbo run review:reconcile --filter=@reviewpilot/api -- <operator arguments>
- pnpm exec turbo run review:cleanup --filter=@reviewpilot/api
- pnpm exec turbo run verify:review --filter=@reviewpilot/tooling

副作用任务禁用缓存；Turbo passthrough 与凭证白名单同步更新。新增 workspace 的 package.json、tsconfig.json、src/index.ts、test:unit/typecheck/lint/format 脚本随对应任务建立，不单列空 scaffold 任务。LangChain/LangGraph 在任务 4、5 核对官方稳定版、Node ≥24、TS 6 与现有 Zod 4 的 peer 兼容后锁定精确版本及 lockfile，不升级无关依赖或安装浮动分支。

## Task 1 v2 契约与安全配置

**文件：** Create packages/contracts/schemas/task-v2.json、packages/contracts/tests/review-schema.test.ts、apps/api/internal/config/review.go、apps/api/internal/config/review_test.go、scripts/s2-review-test.ts、vitest.review.config.ts、tests/integration/s2-review.test.ts、infra/review/model-config.example.json；Modify packages/contracts/{openapi/s1.yaml,scripts/generate.mjs,src/index.ts,src/validate.ts,package.json}、生成的 TS/Go 文件、apps/api/scripts/go-task.mjs、packages/tooling/package.json、turbo.json、.env.example。

**接口：** validateReviewTask(input: unknown): ReviewTaskEnvelope；config.LoadReview() (ReviewConfig,error)，包含上述模式、policy、非敏感模型版本与配额；runReviewScenario(name: string): Promise<Record<string, unknown>> 是测试调度入口，未实现 case 必须失败。

- [x] 写 review-schema.test.ts：合法 v2 通过；schemaVersion=1 携 ai-review、schemaVersion=3、额外 token、错误 SHA 均拒绝；同一合法 v1 仍只被旧 validateTask 接受。写 TestReviewDefaultsClosed，断言默认 disabled、真实发布 false、配置含 API Key 或无效配额拒绝。

```ts
// validV2 is the complete fixture for the v2 fields listed above.
expect(validateReviewTask(validV2).kind).toBe('ai-review');
expect(() => validateReviewTask({ ...validV2, schemaVersion: 3 })).toThrow();
expect(() => validateTask(validV2)).toThrow();
```

- [x] 用最小测试入口建立 contracts 场景：pnpm exec turbo run test:review --filter=@reviewpilot/tooling -- --case contracts。预期新增 v2 断言失败；保留实际失败输出，不把语法错误当行为失败。
- [x] 实现双消息投影、生成 DTO 与 config.LoadReview()；契约生成命令只写生成文件和已有 OpenAPI，不改旧消息源。脚本实现独立隔离 case 注册与 verdict 校验，不调用业务库；Go 包装器在 test + REVIEW_TEST_ISOLATED=1 下跳过项目环境和密钥文件加载，默认开发行为不变。
- [x] 执行 codegen contracts:check 命令以及 contracts 场景；运行 pnpm exec turbo run test:unit --filter=@reviewpilot/api -- ./internal/config -run TestReviewDefaultsClosed -count=1。预期通过且旧契约无漂移。
- [x] 仅提交本任务文件：docs 外已有用户 AGENTS.md 不自动加入。提交说明 feat: add S2 review contracts and closed configuration。

## Task 2 固定版本读取与 Diff 位置

**文件：** Create packages/persistence/migrations/0004-review-context.sql、apps/api/internal/{github/pull_files.go,github/contents.go,jobs/review_lease.go,reviewcontext/service.go,reviewcontext/http.go,reviewcontext/service_test.go}、packages/repository/{package.json,tsconfig.json,src/index.ts,src/diff.ts,src/read-bounded.ts,tests/diff.test.ts,tests/read-bounded.test.ts}、tests/fixtures/review/diffs/；Modify apps/api/cmd/api/main.go、apps/api/internal/github/client.go、apps/api/internal/persistence/models.go、scripts/s2-review-test.ts、packages/tooling/package.json。

**接口：** reviewcontext.New(db *gorm.DB,gateway *github.Gateway,policy contracts.ReviewPolicy) *Service；Service.Read(ctx context.Context,req contracts.ReadReviewContextRequest) (contracts.ReviewContextPage,error)；jobs.CheckReviewLease(tx *gorm.DB,lease contracts.ReviewLease) (persistence.Run,error) 供后续模块复用，核对安装、公开/启用、运行模式、epoch、租约、快照与输入 hash；parseDiff(files: ReviewChangedFile[]): ParsedReviewDiff；validateDiffLocation(ref: EvidenceRef,diff: ParsedReviewDiff): boolean；readBoundedBytes(response: Response,maxBytes: number,signal: AbortSignal): Promise<Uint8Array>。

- [x] 写 TestContextVersionChangesBetweenPages，断言中途新 Head 得到 Stale、不能返回 complete=true；用参数化 Fixture 断言分页缺失、错误内容锚点、越界路径、删除与重命名有明确状态。TS 断言 CRLF 与无末尾换行位置正确，超限 UTF8 响应停止 reader，不先执行 response.text()。

```ts
const oversized = new Response('汉'.repeat(100));
await expect(readBoundedBytes(oversized, 256, new AbortController().signal))
  .rejects.toThrow('RESPONSE_TOO_LARGE');
```

- [x] 执行 context 场景和 pnpm exec turbo run test:unit --filter=@reviewpilot/repository。预期新增读取或映射断言失败。
- [x] 0004 建立 review_context_manifests、review_evidence，扩展 review_runs 的 needs_reconciliation/reason_code/provenance 和不可变配置引用，允许 Outbox 的 ai-review 类型但暂不启用投递。实现 Go 固定 SHA 读取及证据持久化；页间 SHA 与 patch 内容核验，不能持有数据库锁等待 GitHub 网络。测试可 seed 合法 fenced lease，不依赖任务 5 的 Claim 实现。Go 不向 Worker 暴露 Installation Token；完整 manifest contextHash 由 Go 提供，最后一页以前不允许生成。
- [x] 实现配置中的字节与文件限制，完整读取状态为 available、deleted、binary、missing、denied 或 too-large。新正文流式读取，旧连接响应限额保持不变；超过范围写 coverage reason。运行同一组定向命令，预期通过。
- [x] 提交 feat: add fixed-SHA review context and diff mapping。

## Task 3 原子预算与可恢复阶段事实

**文件：** Create packages/persistence/migrations/0005-review-budgets-and-stages.sql、apps/api/internal/budgets/{service.go,http.go,reconcile.go,service_test.go}、apps/api/internal/reviewstages/{service.go,http.go,service_test.go}、apps/api/internal/retention/{service.go,service_test.go}、apps/api/cmd/review-admin/main.go；Modify apps/api/internal/{persistence/models.go,jobs/recovery.go}、apps/api/{cmd/api/main.go,cmd/dispatcher/main.go,scripts/go-task.mjs,package.json}、scripts/s2-review-test.ts、turbo.json。

**接口：** budgets.Service.Reserve(ctx context.Context,req contracts.ReserveModelCallRequest) (contracts.ModelCall,error)；Dispatch(ctx,req contracts.DispatchModelCallRequest) (contracts.ModelCall,error)；Settle(ctx,req contracts.SettleModelCallRequest) (contracts.ModelCall,error)；reviewstages.Service.Read(ctx,req contracts.ReadReviewStageRequest) (contracts.ReadReviewStageResponse,error)；Save(ctx,req contracts.SaveReviewStageRequest) error；retention.Service.Cleanup(ctx context.Context,now time.Time,limit int) (CleanupCounts,error)。所有 ctx 均为 context.Context，服务均经 New(db *gorm.DB) 构造。

CleanupCounts 在 retention/service.go 定义 Contexts、Stages、FindingContents、Metadata 四个非负计数；SQL 尚未创建的后续表不预先引用，任务 5/6 同步扩展清理和删除。模型费用和业务内容分表，清理正文不等于解除收费。

- [x] 写 TestBudgetRaceAndUnknownUsage：余额 10 微单位，两笔同时各申请 6，只能一笔成功；超时调用额度不释放，重试使用新调用且总数受限，19 位溢出金额拒绝。写 TestSettledCallWithoutArtifactBlocksRecovery：费用已记、artifact 不存在时阻断，不生成第二笔同阶段调用。TestReviewStages 断言旧 token、内容冲突和过期读取。

```go
// Values are queried from this test's isolated ledger after the concurrent requests.
if successfulReservations != 1 || reservedMicros != 6 {
    t.Fatal("atomic reservations must preserve the 10-micro limit")
}
if unknownCall.State != "uncertain" || heldAfterTimeout != 6 {
    t.Fatal("unknown usage must not release reserved cost")
}
```

- [x] 分别执行 budget 与 stages 场景，预期新预算和阶段行为失败。场景创建自己的数据库并执行迁移，不接收生产 URL。
- [x] 0005 建立 budget_limits、budget_reservations、model_calls、review_stage_artifacts 与 operator_audits。唯一约束覆盖 Run/purpose/ordinal、调用身份与阶段恢复键；复用任务 2 的 CheckReviewLease，调用记录与 fencing 同一事务检查。context 阶段无需模型 callId，candidates/verification 必须绑定相应已结算调用。
- [x] Dispatch 的 CAS 只在 reserved → dispatching 的首次响应授予发送权；重复或丢失响应不再次授予，恢复后进入 uncertain。结算与 artifact 写入采用幂等键，恢复扫描将过期执行的未完成 dispatching 标记 uncertain，排除 needs_reconciliation；也识别 settled 但无 stage 的不完整事实。
- [x] 建立部署者 CLI review-admin reconcile-model，必填 callId、expectedRevision、outcome=charged/not-sent、receiptRef；charged 必填 actualCostMicros/currency，审计后解除对应收费不确定性。结果缺失时只有显式 --retry-stage 且阶段次数未耗尽才可重新预留；无此参数不重调模型。review-admin cleanup 调用同一 Cleanup 服务；API 无公开管理员免鉴权入口。
- [x] 清理到期源码片段及产物正文，元数据超期按 12 个月政策清理；retention.Service.DeleteRepository(ctx context.Context,tenant contracts.TenantRef) error 使用显式范围事务清除 S2 内容，停止任务并保留不含内容的删除凭据。未知费用保留匿名化的全局余额占用，不因仓库删除伪造退款。测试到期 artifact 无法读取、删除不影响其他安装、未知费用不能被一般 TTL 清理释放；CLI cleanup --delete-installation/--delete-repository 需两者明确且匹配，默认仅清理到期内容。
- [x] 运行 budget/stages 场景及 pnpm exec turbo run test:unit --filter=@reviewpilot/api -- ./internal/retention -count=1，预期通过。提交 feat: add fenced review budgets and stage recovery。

## Task 4 模型网关与 Worker 凭证隔离

**文件：** Create packages/model-gateway/{package.json,tsconfig.json,src/index.ts,src/types.ts,src/gateway.ts,src/openai-compatible.ts,src/fake.ts,src/redact.ts,tests/gateway.test.ts,tests/redact.test.ts}、packages/worker-runtime/tests/launch.test.ts；Modify packages/worker-runtime/scripts/launch.mjs、packages/worker-runtime/package.json、pnpm-lock.yaml、turbo.json、apps/worker-review/package.json。

**接口：** ModelProvider.invoke(request: ProviderRequest,signal: AbortSignal): Promise<ProviderResponse>；ProviderResponse 为 data:unknown、usage 可选、safeResponseHash，不携隐藏推理；ModelGateway.call<T>(request: ModelRequest<T>): Promise<ModelOutcome<T>>。ModelRequest<T> 包含 ReviewLease、purpose、ordinal、Schema 验证器、脱敏 input、signal；ModelOutcome<T> 使用 kind=succeeded/failed/needs-reconciliation、callId，成功包含 value:T，其余包含 reasonCode。BudgetClient 的 reserve/dispatch/settle 方法分别消费任务 3 同名 Request DTO、返回 ModelCall。

- [x] gateway.test.ts 断言先 reserve 再 dispatch 再 invoke；dispatch 响应丢失时 invoke 次数为 0；超时或用量缺失返回 needs-reconciliation，不自动 retry；合法且明确可重试失败最多多一次调用，每次有新预留。断言 Schema 错误、截断响应和地址与冻结配置不匹配均拒绝。
- [x] launch.test.ts 子进程使用 sentinel 环境：review Worker 只得到 MODEL_API_KEY 与本角色服务 token，数据库、GitHub、Evolution token 不可见；evolution Worker 不获得模型密钥。redact.test.ts 断言生成和验证输入均去除 Secret，并保留行映射。

```ts
// trace is captured by the fake budget client and fake provider for one successful call.
expect(trace).toEqual(['reserve', 'dispatch', 'invoke', 'settle']);
expect(dispatchResponseLost.invokeCount).toBe(0);
expect(timeout.kind).toBe('needs-reconciliation');
expect(timeout.invokeCount).toBe(1);
```

- [x] 安装并锁定互相兼容的 @langchain/core 与 @langchain/openai，创建本包脚本。运行本包 test:unit 与 worker-runtime test:unit，预期上述断言在网关与白名单尚未实现时失败。
- [x] 实现 FakeProvider 的脚本化响应与 OpenAICompatibleProvider：非流式 Chat Completions、供应商和 LangChain 双层 maxRetries=0、请求 AbortSignal/60 秒上限、明确结构化输出模式和安全错误映射。配置有界 fetch，按 UTF8 字节限制模型响应最多 256 KiB 并禁止跨 host 重定向；SDK 不满足响应大小/重试控制时拒绝真实模式。
- [x] 模型目的地址和能力匹配冻结配置；完整模型消息计数含 system、Schema 与候选，未知 tokenizer 或计费项拒绝真实发送。脱敏由两个阶段共用，不把原始输入、错误对象、response 或 Prompt 输出到遥测。扩展 launcher 的 MODEL_API_KEY_FILE，仅 review 角色可读取；隔离模式不读项目 .env，REVIEW_FAKE_SCRIPT_FILE 仅 review + fake 角色获得。
- [x] 运行 pnpm exec turbo run test:unit --filter=@reviewpilot/model-gateway --filter=@reviewpilot/worker-runtime，预期通过；无付费请求。提交 feat: add budgeted model gateway and isolated credentials。

## Task 5 串行引擎与最终结果持久化

**文件：** Create packages/review-engine/{package.json,tsconfig.json,src/index.ts,src/context.ts,src/generate.ts,src/verify.ts,src/validate.ts,src/rank.ts,src/pipeline.ts,tests/pipeline.test.ts}、tests/fixtures/review/golden/、packages/persistence/migrations/0006-review-findings.sql、apps/api/internal/reviews/{tasks.go,result.go,http.go,result_test.go}、packages/worker-runtime/src/{review-client.ts,review-consumer.ts,lease-heartbeat.ts}、packages/worker-runtime/tests/review-consumer.test.ts；Modify packages/worker-runtime/src/{broker.ts,client.ts,index.ts}、apps/worker-review/src/main.ts、apps/api/cmd/api/main.go、pnpm-lock.yaml、scripts/s2-review-test.ts、apps/api/internal/retention/service.go 与测试。

**接口：** runReviewPipeline(input: {lease:ReviewLease;client:ReviewInternalClient;gateway:ModelGateway;signal:AbortSignal}): Promise<ReviewPipelineOutcome>；ReviewPipelineOutcome 是 completed（带 ReviewResult）、blocked（reasonCode）或 failed（reasonCode）。ReviewInternalClient 提供任务表全部方法，预算方法遵守 BudgetClient；reviews.Service.Claim(ctx,req contracts.ClaimReviewRequest) (contracts.ClaimReviewResponse,error)、Commit(ctx,req contracts.CommitReviewRequest) (contracts.ReviewCommitResponse,error)、Block(ctx,req contracts.ReviewTaskActionRequest) error，New(db *gorm.DB) 构造。

- [ ] 用六个 Golden Fixture 参数化断言：真实锚点可发布、零候选正常完成且仅一次模型调用、错误引用过滤、旧问题/已有保护过滤、重复根因合并、上下文不足 Abstain。再用恶意注释 Fixture 断言工具与预算不被修改。TestReviewResultFenced 断言跨仓库引用使 Run 失败，普通错行只过滤候选；不同租约或同 resultId 不同内容拒绝。

```ts
// empty is the completed outcome for the zero-candidate Golden fixture.
expect(empty.kind).toBe('completed');
if (empty.kind !== 'completed') throw new Error('EXPECTED_COMPLETED');
expect(empty.result.candidates).toEqual([]);
expect(emptyCalls.map(call => call.purpose)).toEqual(['generate']);
```

测试中的 emptyCalls 从 fake gateway 的捕获记录读取，不给生产 ReviewPipelineOutcome 增加仅供测试的字段。

- [ ] 安装并锁定 @langchain/langgraph，与任务 4 锁定版本满足 peer；运行 review-engine test:unit、worker-runtime test:unit 和 results 场景，预期新增结果/流程断言失败。
- [ ] 实现固定 graph：context → candidates → verification → finalize；零有效候选有限分支跳过 verification。generationPromptVersion=review-generation-v1、verificationPromptVersion=review-verification-v1。读取已有完整阶段，再决定调用；阶段输入与输出 hash 均在 Go 校验，不使用 LangGraph DB checkpoint。
- [ ] 实现租约持续心跳与中止清理，未知调用先 block 且确认后 ack，正常结果先 Commit/Confirm 再 ack。重投已接受任务不再次生成；旧 token 不能保存阶段。新 graph 不复用旧连接结果内容或旧 ModelConfig 条件。
- [ ] 0006 建立 review_findings、review_filtered_candidates、review_coverage；Go 按 evidence manifest 重新校验引用与结果 Schema，生成可信 Findings 和候选过滤记录，不直接信任 Worker 的 verified 标签。最终结果、job_results、任务 result_ready 与待发布事实同事务保存；零候选/全过滤不一定无需更新 Check，完成规则由任务 6 的 Publisher 按覆盖确定 Completed 或 Partial。新增表纳入任务 3 的清理与删除事务并验证。
- [ ] 结果正文的业务 hash 验证使用共享 Fixture；模型 key、leaseToken、原始响应不进入持久化内容 hash 或外部 DTO。结构化支持理由不称为证明，引用真实性与是否本次引入分别核验。
- [ ] 运行同一组定向命令，预期通过。提交 feat: add serial evidence-first review pipeline。

## Task 6 审查投递与 GitHub 发布对账

**文件：** Create packages/persistence/migrations/0007-review-publication-facts.sql、apps/api/internal/{publisher/review.go,publisher/review_records.go,publisher/review_test.go,github/reviews.go,jobs/review_schedule.go}；Modify apps/api/internal/{webhooks/service.go,webhooks/service_test.go,jobs/service.go,jobs/recovery.go,messaging/rabbitmq.go,publisher/service.go,publisher/recovery.go,cutover/service.go,repositories/service.go,retention/service.go}、apps/api/cmd/dispatcher/main.go、packages/worker-runtime/src/broker.ts、scripts/s2-review-test.ts。

**接口：** jobs.ScheduleReview(tx *gorm.DB,job persistence.Job,run persistence.Run,at time.Time) error；publisher.Service.ProcessNext(ctx context.Context) (bool,error) 沿用现有签名，按 Run Pipeline 选择 connection 或 review；BuildReviewPlan(ctx context.Context,runID string) (contracts.ReviewPublicationPlan,error)；ReconcileReview(ctx context.Context,runID string) (contracts.ReviewPublicationReceipt,error)；github.Gateway.CreateReview(ctx context.Context,token,fullName string,number int,plan contracts.ReviewPublicationPlan) (contracts.ReviewPublicationReceipt,error)。后两项 DTO 在任务 1 一并定义：Plan 含身份、planHash、marker、findings 与 mode，Receipt 含远端 ID、SHA、作者 App、marker 与确定状态。

- [ ] 写 TestReviewSlotRace：同 PR 已用 2 个额度，两个并发新根因最多一份被预留；TestForeignMarkerIgnored：相同文字标记、非本 App 作者不对账为成功。参数化断言已发布后新 Run 根因不重发、SHA/权限变化阻止发布、真实写入成功后丢响应只补记一份远端事实。

```go
// Counts come from the scoped slot table and the fake GitHub's recorded writes.
if occupiedSlots != 3 || newlyReserved != 1 { t.Fatal("PR slot race exceeded the limit") }
if writesAfterLostResponse != 1 { t.Fatal("reconciliation duplicated a remote review") }
```

- [ ] 执行 publication 场景，预期新增审查发布断言失败；S1 connection 用相同模拟端点保持原结果，不能通过删除旧断言实现通过。
- [ ] 启用 v2 消费时复用持久 exchange reviewpilot.tasks.v1，新 routing key=ai-review、新队列 reviewpilot.review.v2，旧队列仍只绑定 connection-check。Worker 一个共享串行处理器，所有 channel 总 prefetch 约束并以同一执行互斥防止双队列并行；先启用消费能力再开启审查投递。Outbox publisher confirms 与不可路由治理沿用 S1。
- [ ] Webhook 默认继续 S1 连接任务；仅显式 fake 受控测试或经批准 real 模式生成 ReviewSnapshot 和 ai-review Outbox。保留事件去重、乱序与 generation 规则；Recover 按 Pipeline 构造对应消息并排除阻断，切换回退不将 S2 accepted result 重新通知为连接检查。
- [ ] 0007 扩展现有 publication_requests 的待对账/内容类型与计划字段，建立 pr_review_slots 和 review_publication_facts；根因唯一键、三槽位占用与计划同事务创建。RootCauseKey 固定 category/path/anchorHash/trigger/consequence，Unicode 规范化及空白折叠，fingerprint-v1 SHA256，不包含行号、SHA 或标题；使用任务 1 的 rootCauseStatement 声明，仅用于 Go 校验后规范化，不信任模型给出的 hash。跨路径重命名暂不宣称语义等价。新表同步纳入清理/删除，已发布事实按保留策略保管。
- [ ] 发布 COMMENT、固定 commit_id，合法变更新侧优先行级；无行锚点但证据充分时摘要且共用 3 个额度。零问题不发讨论评论。内容再次脱敏；发送前后验证权限和 Head，记录真实事实再决定 Stale。
- [ ] 稳定 marker 包含 Run 与 planHash，并核对 App、Head、Review 和评论分页事实。写入不确定时保持槽位和 needsReconciliation，只对账不盲目重发；明确未写入才退避，429 遵守 Retry-After/重置时间。租约/epoch 失效不能补写业务状态；撤销或删除不释放未知外部事实并冒险再发。
- [ ] 运行 publication 场景，另执行一次 pnpm exec turbo run test:migration --filter=@reviewpilot/tooling -- --case webhook-check，预期旧 neutral Check 不回归。提交 feat: publish fenced reviews with root-cause reconciliation。

## Task 7 中文 PR 结果页

**文件：** Create apps/api/internal/reviews/{query.go,query_http.go,query_test.go}、apps/web/src/app/repositories/[installationId]/[repositoryId]/pulls/[number]/page.tsx、apps/web/src/features/reviews/{review-console.tsx,findings.tsx,coverage.tsx,costs.tsx}、apps/web/src/lib/{review-view.ts,review-view.test.ts}；Modify apps/api/cmd/api/main.go、apps/web/src/lib/api-client.ts、apps/web/src/features/repositories/repository-console.tsx、apps/web/package.json。

**接口：** reviews.QueryService.Detail(ctx context.Context,session auth.Session,tenant contracts.TenantRef,prNumber int,runID string) (contracts.ReviewRunDetail,error)，构造函数 NewQuery(db *gorm.DB,authService *auth.Service)；APIClient 新增 listPulls(tenant,cursor?)、listReviewRuns(tenant,prNumber,cursor?)、getReviewRun(tenant,prNumber,runId)，返回任务 1 生成 DTO；reviewView(detail: ReviewRunDetail): ReviewViewModel 只负责文案与安全展示。

- [ ] 写 TestReviewQueryAccess：安装与 PR 不匹配、维护权撤销、过期内容拒绝，cursor 跨仓库复用拒绝。review-view.test.ts 断言 fake 标签、未知费用不显示 0、Partial 不显示完整、零候选文案为“本次覆盖内未发现可发布问题”，排序分不显示准确率百分比。

```ts
// Fixture inputs contain Partial coverage or an uncertain model call respectively.
expect(reviewView(partialDetail).coverageLabel).toBe('部分覆盖');
expect(reviewView(unknownCostDetail).costLabel).toBe('费用待对账');
```

ReviewViewModel 在 review-view.ts 定义 coverageLabel、costLabel、modeLabel、statusLabel、findings 与 filtered；界面不另写一套判断逻辑。

- [ ] 运行 results 场景（加入查询断言）及 pnpm exec turbo run test:unit --filter=@reviewpilot/web，预期新增查询或展示断言失败。
- [ ] 实现三个公开查询端点与分页，复用 Auth.Session 及 Auth.Gateway.Access(ctx,session.UserToken,tenant,true) 的当前维护权检查；只返回脱敏必要片段。加入仓库的 PR/Run 入口和历史 Run 选择，默认当前版本；没有 Review 时显示尚未审查，不把 S1 连接记录映射为 Finding。
- [ ] 页面展示 Findings/已过滤、失败条件、证据与验证建议、覆盖、状态/阻断、版本、调用用途、已知费用与未结算预留、发布结果。使用现有 shadcn 与 TanStack Query，保持同源相对 API，不写 Next route handler；不增加未实现按钮。
- [ ] 按配置轮询最终和阻断状态停止刷新，401/403 处理授权失效；GitHub 外链只接受已知 host 和服务端远端 ID，不渲染模型提供的任意 HTML。运行上述测试及 pnpm exec turbo run typecheck lint build --filter=@reviewpilot/web --filter=@reviewpilot/api，预期通过。
- [ ] 提交 feat: show Chinese PR findings coverage and review costs。

## Task 8 隔离联调与阶段交付

**文件：** Create scripts/verify-review.ts、docs/runbooks/s2-local-review.md、docs/reports/s2-ai-review.md、apps/api/internal/reviews/pipeline_integration_test.go、packages/observability/src/review.ts；Modify scripts/s2-review-test.ts、tests/integration/s2-review.test.ts、packages/tooling/package.json、turbo.json、apps/api/internal/observability/setup.go、packages/observability/src/{index.ts,trace.ts}、infra/{compose.dev.yml,compose.prod.yml}、apps/worker-review/Dockerfile、infra/grafana/dashboards/s1.json、infra/prometheus/alerts.yml、README.md。

**接口：** runReviewScenario('pipeline') 返回持久化事实 verdict：acceptedResultCount、publishedRootCauseCount、modelCallCount、unknownReservationHeld、stalePublishBlocked、traceLinked、secretSentinelAbsent、simulated=true；verify-review.ts 顺序运行确定任务，无 verdict 或基础设施不可用即失败，不记为跳过成功。

- [ ] 联调断言真实临时 PostgreSQL + RabbitMQ、模拟 GitHub + fake Provider 下：签名事件建立 v2 Run，正常路径生成和验证合计 2 次；重复投递及结果确认丢失仍一份最终结果、0–3 个问题。第二 Run 模拟用量未知，持久化阻断且额度保留；模拟 Head 更新不发布。以上属于同一隔离场景，不新增模型矩阵。

```ts
expect(verdict.simulated).toBe(true);
expect(verdict.acceptedResultCount).toBe(1);
expect(verdict.modelCallCount).toBe(2); // The first normal run, excluding the unknown-usage run.
expect(verdict.publishedRootCauseCount).toBeLessThanOrEqual(3);
expect(verdict.unknownReservationHeld).toBe(true);
expect(verdict.stalePublishBlocked).toBe(true);
expect(verdict.traceLinked).toBe(true);
expect(verdict.secretSentinelAbsent).toBe(true);
```

- [ ] 执行 pipeline 场景，先保存当前缺少联调能力的实际失败，随后连接已有模块与必要配置，不重写一份绕过 REST 的全链路实现。
- [ ] 完善 test harness：随机端口、临时目录、唯一 Broker 容器与资源标签，只清理本次创建的资源；fake host 与 Run provenance 必须一致。Go 子进程通过现有 Turbo 包装器调用，场景必须写出本次 verdict。保证 tests 不启动用户已有 Compose 或读取 .env 里的真实密钥。
- [ ] 加入 review.model.calls、review.model.unknown、review.cost.micros、review.stage.duration 和失败/阻断指标；bounded purpose/mode/stage 标签，不用 runId/PR 当指标标签。接入现有安全 OTel，sentinel 不进入日志与 Trace；Collector 不可用时业务仍能推进，不采集完整模型输入输出。
- [ ] 仅更新部署模板、Worker 打包路径、密钥挂载与白名单；Go 进程不挂模型密钥，Web 不挂业务密钥。Compose 配置只读校验，完整应用镜像运行和真实 App/生产环境保留待验收。禁止把模板更新描述为部署完成。
- [ ] 补充可复现模拟演示说明、operator 对账/清理步骤、默认关闭配置、未知调用处置与恢复限制。报告真实执行命令、失败修正和待外部事项；不展示模拟精度或测试币种的真实成本。
- [ ] 执行一次 verify:review：契约漂移、受影响包 typecheck/lint/build、定向 TS 单元、API config/retention 定向单元、context/budget/stages/results/publication 场景、一次 pipeline 与 S1 message-recovery/cutover 必要回归、infra 只读配置校验。任务 6 已验证的 webhook-check 不因仪式重复跑；不执行六场景全部迁移矩阵或压力/浏览器/多模型测试。内部各命令仍使用 Turbo，最终退出 0 才写本地模拟通过。
- [ ] 自行复核从执行基线开始的整个 S2 diff：授权、哈希、价格、阶段恢复、未知事实、累计额度、删除和配置 fail-closed。修正发现的必要问题后只重跑受影响断言与一次最终汇总，保存证据。
- [ ] 提交 feat: verify and document local S2 review delivery；输出实现、验证、限制和待真实接入事项，停止，不进入 S3，不自动 merge/push。

## 计划自查与验收清单

| 已确认设计要求 | 实现任务 |
| --- | --- |
| 本地优先、真实默认关闭、隔离凭证与模式标识 | 1、4、6、8 |
| 固定版本、Diff、分页、资源上限、覆盖与不执行 PR | 2、5 |
| OpenAI-compatible、fake、Schema、脱敏、有限重试 | 1、4、5 |
| 原子预留、未知用量、跨尝试调用上限、部署者对账 | 3、4、5 |
| 持续租约、阶段结果、结果接受后 ack、旧连接兼容 | 3、5、6 |
| 证据与归因、Abstain、根因去重、累计三条 | 5、6 |
| 独立发布、响应丢失、远端身份、Stale/Cancelled | 6、8 |
| 中文 Web、状态/费用/覆盖、没有伪精度或假按钮 | 7 |
| 产物保留、到期过滤、删除隔离与最小审计 | 3、7、8 |
| 业务记录与安全遥测、必要 Golden 和全链路 | 5、8 |

计划批准后实施。仅当定向验证与最终隔离汇总都有证据时，才勾选本地完成；真实 S1/S2 GitHub、模型兼容与质量、实际账单、业务数据库切换、镜像运行及生产恢复未验收时仍保持待办。

S2 结束后向用户报告并停止。自进化、人工纠正、完整 Replay 和更强上下文没有实现前，不在演示、README 或简历中声称已交付这些能力。
