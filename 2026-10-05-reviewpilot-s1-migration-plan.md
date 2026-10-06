# ReviewPilot S1 技术栈迁移实现计划

> 面向执行者：使用 superpowers:executing-plans，在当前会话和现有项目逐任务执行。用户已明确禁止 subagent，包括独立审查代理；执行者自行复核。所有步骤以复选框记录，完成本阶段后停止。用户已于 2026-10-06 批准实施。

**目标：** 保留现有 S1 接入能力，将业务 API 和治理迁到 Go，通过 RabbitMQ 驱动 TS Worker，并接入可运行的完整监控基础。

**架构：** Next.js 仅展示，浏览器经同源入口直接调用 Go。Go API、调度发布进程共享模块和 PostgreSQL，以 Outbox 连接 RabbitMQ；TS Worker 不读业务表、不持 GitHub 写凭证。S1 只产生 neutral 连接 Check，不调用模型。

**技术栈：** Go、Gin、GORM、PostgreSQL、RabbitMQ、Next.js、TypeScript、shadcn/ui、Tailwind CSS、TanStack Query、OpenAPI、JSON Schema、Turborepo、Docker Compose 与 OpenTelemetry 监控栈。

**设计依据：** [已确认的技术栈与 S1 迁移设计](docs/superpowers/specs/2026-10-05-reviewpilot-tech-stack-design.md)。同时阅读 [原产品设计](docs/superpowers/specs/2026-10-04-reviewpilot-design.md)，但只执行本计划的 S1 迁移范围。

日期：2026-10-05。位置：review-pilot 根目录。状态：2026-10-06 获批，实施中；任务记录位于本计划专属执行账本，不提前宣称阶段完成。

## 全局约束

- 在现有项目执行，不自行创建 worktree、不使用 subagent，每完成一个阶段即停下。
- 只迁移 S1。LangChain、LangGraph、模型调用、Finding、预算调用网关和自进化实现从后续阶段开始；pgvector 从 S4 Memory 引入。
- 不执行 PR 代码，不申请 Contents 写权限，不把基础 Check 解释为代码已审查。
- Web 无 BFF，不访问业务数据库，不持 GitHub Token；所有业务写入和权限治理归 Go。
- 保留安装、仓库、Run、Check 标识和现有去重键；不改写已执行的 SQL 0001 和 0002，不使用启动时 AutoMigrate。
- RabbitMQ 至少一次交付；手动 ack、publisher confirms、不可路由检测、幂等结果和业务 fencing 同时存在，不宣称外部 exactly-once。
- 默认沿用 S1：Webhook 上限 2 MiB、事件稳定窗口 2 秒、租约 90 秒、心跳 20 秒、最多 5 次执行尝试；退避为 min(300, 30 × 2^min(attempt−1, 3)) 秒。
- OAuth state 有效 10 分钟且只能消费一次；会话最多 8 小时。Cookie 名称保留 reviewpilot_session 和 reviewpilot_oauth。
- 密文兼容旧 AES-256-GCM：32 字节 Base64 密钥，12 字节 nonce，16 字节 tag，格式为 base64url(nonce).base64url(tag).base64url(ciphertext)。会话 token 仍以 SHA-256 十六进制哈希存储。
- Run 冻结 Base/Head、Pipeline 和配置；S1 保留 modelConfigVersion=none、memorySetVersion=empty、trustedRulesSha 为空字符串。
- 日志和 Trace 默认保留 7 天、指标 30 天；不采集密钥、完整源码、完整提示词或模型隐藏思维链。
- 工程入口统一为 pnpm exec turbo run；pnpm 管理 JS 依赖，Go modules 管理 Go 依赖。服务、迁移、部署、集成和格式化不缓存，不启用远程缓存。
- 不购买服务、不自动部署真实 GitHub，不使用用户现有数据库做测试。真实安装验收不足时明确记录，不用 fake server 冒充。

## 重点复核的五类输入

| 条件 | 必须保持的行为 | 归属 |
| --- | --- | --- |
| 同一 deliveryId 携带不同请求体 | 返回冲突且事务不新增 Run 或 Outbox | 任务 5 |
| confirm 丢失、Worker 提交成功但响应或 ack 丢失 | 重投不重复提交，不重复生成连接 Check | 任务 4、5 |
| 旧 Worker 租约过期后仍提交，或安装在执行中被撤销 | 拒绝旧 token 和未授权提交、发布 | 任务 4、5 |
| Go 读取旧会话、旧迁移账本和数字仓库 ID | 可验证兼容，不静默改密文、标识或创建平行表 | 任务 2、3 |
| 切换中未完成任务、已发布但未落库记录混合存在 | 先排空与对账，再重通知；不能同时启用两套消费者 | 任务 8 |

## 范围和任务顺序

八个任务顺序执行，每个任务含必要的失败断言、最小实现、定向验证与提交。任务是本阶段的内部检查点，不代表用户已授权后续产品阶段。任务 1–7 可独立检查，任务 8 收敛切换与最终验收；发生权限或外部环境阻碍时只做安全的未受阻工作，不将阻碍绕过。

| 任务 | 独立交付 |
| --- | --- |
| 1 | Go 应用可通过 Turbo 启动和构建，HTTP 与消息契约可生成并校验 |
| 2 | Go 在专用数据库读取旧 S1 Schema，安全执行新增迁移 |
| 3 | Go 认证与仓库 API 可用，旧会话和维护权限延续 |
| 4 | RabbitMQ 到 Worker 到 Go 的任务处理可幂等恢复 |
| 5 | Webhook 到 neutral Check 的 Go 发布链路闭环 |
| 6 | Web 直接使用 Go，现有仓库管理体验保留 |
| 7 | Compose、日志、指标、Trace、仪表盘与告警可运行 |
| 8 | 专用环境切换可对账、可回退，交付并停在 S1 |

本计划不拆成八个独立产品 spec：这些任务共同迁移同一条已有接入链路。AI Review 和 Memory 是后续子项目，不在此混合实施。

## 文件职责与共用契约

所有下方路径相对项目根目录，Create 是拟新增，Modify 是现有文件；它们现在不一定存在。

Go 模块固定为 reviewpilot.local/api，workspace 名为 @reviewpilot/api。apps/api/cmd/api 提供公网 API 和独立内网监听；cmd/dispatcher 负责 Outbox、恢复调度和发布；cmd/migrate 只负责迁移与切换管理。internal 下按 config、httpapi、auth、github、repositories、jobs、persistence、publisher、observability 划分职责。

契约源位于 packages/contracts/openapi/s1.yaml 和 schemas/task-v1.json。采用 OpenAPI 3.0.3、JSON Schema Draft 2020-12；Go 使用 oapi-codegen，TS 使用 openapi-typescript 与薄 fetch 客户端，Worker 使用独立 Ajv 2020 实例校验。Go Broker 客户端使用 rabbitmq/amqp091-go，TS 使用 amqplib。具体稳定补丁版在任务 1 或 4 核对最低要求后锁定，不安装 main 分支或使用浮动 latest。[oapi-codegen](https://github.com/oapi-codegen/oapi-codegen)、[openapi-typescript](https://openapi-ts.dev/introduction)、[Ajv Schema 版本](https://ajv.js.org/json-schema.html)、[Go AMQP 客户端](https://github.com/rabbitmq/amqp091-go)、[RabbitMQ JS 示例](https://www.rabbitmq.com/tutorials/tutorial-two-javascript)。

OpenAPI 生成的共用类型为 TenantRef、RunSnapshot、TaskEnvelope、TaskLease、ClaimRequest、ConnectionResult、ClaimResponse、CommitResponse、APIError。Go 类型位于 internal/contracts/s1.gen.go，TS 类型位于 packages/contracts/src/s1.gen.ts；不要另外手写一套相似类型。消息字段以 task-v1.json 为源，生成脚本将其受限字段投影到 OpenAPI 的 TaskEnvelope（const 转单值 enum、引用映射到 components.schemas），不直接把 Draft 2020-12 文件当作 OpenAPI 3.0 Schema 引用；contracts:check 检查投影和两端生成文件一致。

| 类型 | 固定字段与约束 |
| --- | --- |
| TenantRef | installationId、repositoryId，正整数且不超过 9007199254740991 |
| RunSnapshot | 与现有 packages/domain/src/types.ts 的 RunSnapshot JSON 字段完全一致 |
| TaskEnvelope | schemaVersion=1、messageId、taskId、runId、tenant、headSha、kind=connection-check 或 evolution、notificationGeneration、可选 traceparent/tracestate |
| TaskLease | taskId、runId、leaseToken、attempt、expiresAt、snapshot、inputHash、executionEpoch；UUID 使用字符串，时间使用 RFC3339 |
| ClaimRequest | workerId、envelope；任务 URL 的 taskId 必须等于 envelope.taskId |
| ClaimResponse | outcome=claimed、busy、not-ready、finished、cancelled、exhausted、superseded；仅 claimed 携带 lease |
| ConnectionResult | resultId、taskId、runId、leaseToken、executionEpoch、inputHash、resultHash；kind 固定 connection-check，不携带源码、密钥或发布自由文本 |
| CommitResponse | outcome=accepted 或 already-accepted；业务进入待发布，不代表 Run 已完成 |
| APIError | code、message、requestId；冲突 409，未授权 401/403，输入错误 400 |

resultId 固定为 `${taskId}:connection`；resultHash 为 UTF-8 字符串 taskId、runId、inputHash 依次以换行拼接（无末尾换行）后的 SHA-256 十六进制值。inputHash 由 Go 对持久化快照生成并随领取响应提供，Worker 不重新序列化跨语言 JSON 计算。内部接口必须核对所有字段与任务记录，不能只相信合法 UUID。

公开 REST：GET /api/health、GET /api/bootstrap、GET /api/session、GET /api/repositories、GET /api/runs?limit=20、POST /api/repositories/sync、POST /api/repositories/{installationId}/{repositoryId}、POST /api/github/webhook，以及现有 /auth/github、/auth/github/callback、/auth/logout、/installations/callback。保留现有仓库 POST 表单兼容，新增 JSON 请求支持预期 configVersion；并发不匹配返回 409。

内部 REST：POST /internal/tasks/{taskId}/claim、POST /internal/tasks/{taskId}/heartbeat、POST /internal/tasks/{taskId}/connection-result、GET /internal/tasks/{taskId}/result、POST /internal/tasks/{taskId}/fail。内部独立监听，不由公网代理转发；使用服务 Bearer 凭证，角色约束为 review-worker 或 evolution-worker。

Go 领域类型与生成 DTO 分开。persistence.Store 封装 *gorm.DB；auth.Session、github.Gateway、jobs.Service、publisher.Service 由所属任务定义。跨任务统一使用 context.Context 和生成的 contracts 类型，避免通过 ORM Model 直接返回 API。

## 工程任务入口

以下是任务 1 和后续任务要建立的约定，不是当前已可运行的命令：

| Turbo 任务 | 所属 workspace 与执行行为 |
| --- | --- |
| codegen、contracts:check | @reviewpilot/contracts；生成与校验契约，check 验证生成文件无漂移 |
| build、typecheck、lint、format、test:unit | @reviewpilot/api；scripts/go-task.mjs 分别调用 Go 构建、编译检查、vet、gofmt 和定向 go test |
| db:migrate | @reviewpilot/api；唯一迁移入口，读取现有 SQL 目录及迁移账本 |
| dev、start | @reviewpilot/api 与 @reviewpilot/web；不隐式启动旧 Worker |
| worker:review、worker:evolution | 两个现有 Worker；后者 S1 保持不领取进化任务 |
| dispatcher | @reviewpilot/api；Outbox Relay、恢复扫描和 Go 发布消费 |
| infra:up、infra:check、test:migration、verify:migration、cutover:check | @reviewpilot/tooling；通过明确脚本执行 Compose、专用集成、迁移预检和收尾 |

Go test:unit 包装器透传 package 和 -run 参数，默认 ./...；所有带 -count=1 的定向验证关闭对应 Turbo 缓存。test:migration 包装器使用 --case 选择下方场景，不重复跑整套旧测试。

测试 harness 位于 scripts/s1-migration-test.ts：创建专用临时 PostgreSQL，仅对需要 Broker 的场景启动唯一名称、随机宿主端口的 RabbitMQ 测试容器；不接收生产 DATABASE_URL 作为默认输入，只向子进程注入 TEST_DATABASE_URL、测试 Broker 和假 GitHub 地址。一般场景在专用测试库准备 mode=go，cutover 场景保留初始 legacy。清理只处理本次创建的资源；Docker 不可用时明确失败或记录受阻，不降级为“真实 RabbitMQ 已验证”。保留 scripts/integration-test.ts 的 JSON verdict 防假绿约束。

## 任务 1 Go 工程与契约

**文件：** Create apps/api/{package.json,go.mod,go.sum,scripts/go-task.mjs}、apps/api/cmd/api/main.go、apps/api/internal/{config/config.go,httpapi/router.go,httpapi/router_test.go,contracts/s1.gen.go}、packages/contracts/{package.json,openapi/s1.yaml,schemas/task-v1.json,scripts/generate.mjs,scripts/check.mjs,src/index.ts,src/validate.ts,src/s1.gen.ts,tests/schema.test.ts}；Modify turbo.json、pnpm-lock.yaml、packages/tooling/package.json、.env.example；Create .go-version、docs/adr/0002-go-migration-runtime.md。

**接口：** httpapi.NewRouter() *gin.Engine；config.Load() (Config,error)，Config 定义 APP_URL、API_BIND_ADDR、INTERNAL_BIND_ADDR、DATABASE_URL、RABBITMQ_URL、INTERNAL_API_URL、REVIEW_WORKER_SERVICE_TOKEN、EVOLUTION_WORKER_SERVICE_TOKEN 和原有 GitHub 配置；后续路由按模块 Register 到 Router。contracts.validateTask(input:unknown):TaskEnvelope 由 src/validate.ts 实现并从 index.ts 导出，使用 Ajv schema，额外字段与未知版本拒绝。

- [x] 建立最小 Go workspace 包装和契约生成入口；读取现有 Go 版本，核对 Gin、GORM、生成器兼容要求，锁定可用稳定版本并记录，不自动升级整套 JS 依赖。
- [x] 编写 router_test.go 的 TestHealthNoSecrets 和 contracts schema 断言：未知 schemaVersion、负数 ID、包含 token 字段的消息均拒绝。HTTP 测试的核心断言如下：

```go
r := httpapi.NewRouter()
w := httptest.NewRecorder()
r.ServeHTTP(w, httptest.NewRequest("GET", "/api/health", nil))
if w.Code != 200 || !strings.Contains(w.Body.String(), "foundation") { t.Fatal(w.Body.String()) }
if strings.Contains(w.Body.String(), "DATABASE_URL") { t.Fatal("config leaked") }
```

- [x] 执行 pnpm exec turbo run test:unit --filter=@reviewpilot/api -- ./internal/httpapi -run TestHealthNoSecrets -count=1，记录健康响应未实现导致的失败；契约检查同样记录实际失败，不用安装失败冒充行为测试失败。
- [x] 实现 NewRouter 与 Config 校验，健康响应明确 phase=foundation；构建输出 bin/api、bin/dispatcher、bin/migrate 按入口创建进度逐步加入。生成类型和校验器，缺配置只输出字段名，不输出值。GitHub 配置未齐时仍可启动健康与 bootstrap 路由，登录等操作返回 503，不伪造认证成功。
- [x] 执行上述定向测试及 pnpm exec turbo run contracts:check --filter=@reviewpilot/contracts；要求 PASS、生成文件与源契约一致。Go build 缓存包含 Go 版本、go.mod/go.sum、源码、生成契约，服务与副作用任务关闭缓存。
- [x] 仅提交本任务文件，提交信息 feat: scaffold Go API and cross-language contracts。

## 任务 2 兼容持久化与新增迁移

**文件：** Create apps/api/internal/persistence/{store.go,models.go,migrations.go,store_test.go}、apps/api/internal/testsupport/database.go、apps/api/cmd/migrate/main.go、packages/persistence/migrations/0003-outbox-and-results.sql、scripts/s1-migration-test.ts、tests/integration/s1-migration.test.ts；Modify packages/tooling/package.json、packages/persistence/package.json。0001 和 0002 原封不动。

**接口：** persistence.Open(ctx context.Context,dsn string)(*Store,error)；persistence.ApplyMigrations(ctx context.Context,store *Store,directory string) error；Store.DB() *gorm.DB；testsupport.NewDatabase(t *testing.T) *gorm.DB 只连接 harness 注入的专用数据库。运行器导出 runScenario(name:string):Promise<Record<string,unknown>>，本任务定义 legacy-schema 场景及下方可观测结果字段，后续任务扩展场景。

- [x] 编写 legacy-schema：用现有 SQL 建表并保存哈希，种入旧 Run、会话和仓库，Go 连续迁移两次，比较旧 ID、密文和表名；篡改已执行迁移副本必须拒绝。断言固定为：

```ts
expect(await runScenario('legacy-schema')).toMatchObject({
  runIdPreserved: true, sessionCiphertextPreserved: true,
  parallelTablesCreated: false, noDuplicateMigrationRows: true, changedMigrationRejected: true,
});
```

- [x] 执行 pnpm exec turbo run test:migration --filter=@reviewpilot/tooling -- --case legacy-schema，记录 Go 迁移缺失时的失败。
- [x] 实现 Store 与迁移器，沿用 schema_migrations 的 name/hash/applied_at、SHA-256 文件哈希和 advisory lock 782914630；显式映射旧表，不调用 AutoMigrate。此任务即移除 persistence workspace 的旧 db:migrate 脚本，改为唯一 Go 入口，不等到最后才解决双迁移器调度。
- [x] 新增 outbox_events：id、task_id、notification_generation、kind、payload、available_at、sent_at、relay_token、relay_expires_at，唯一键为 task_id/kind/notification_generation；新增 job_results（task_id 唯一、result_id/result_hash/input_hash/payload/accepted_at）和 publication_requests（run_id 唯一、状态、租约、attempts、available_at、last_error_code）。扩展 jobs 状态允许 result_ready，新增 notification_generation 和 execution_epoch；新增 runtime_control 单例，mode=legacy/go、epoch，初始 legacy/0；sessions 新增可空 csrf_token。旧列、旧 ID 和状态约束兼容保留。
- [x] 重跑 legacy-schema，要求断言通过且重跑不重复迁移；SQL 仍保存在现有目录，Docker 构建显式包含，Go 迁移不依赖 TS 数据库执行器。（Dockerfile 包含 SQL 由任务 7 验证。）
- [x] 仅提交本任务文件，提交信息 feat: preserve S1 schema with Go persistence and outbox。

## 任务 3 Go 登录与仓库治理

**文件：** Create apps/api/internal/auth/{crypto.go,service.go,http.go,service_test.go}、apps/api/internal/github/{client.go,oauth.go,access.go}、apps/api/internal/repositories/{service.go,http.go}、apps/api/internal/auth/testdata/legacy-session.json；Modify apps/api/cmd/api/main.go、契约源与生成文件、tests/integration/s1-migration.test.ts。

**接口：** auth.SealToken(token,key string)(string,error)、auth.OpenToken(sealed,key string)(string,error)；auth.Service.RequireSession(ctx context.Context,opaqueToken string)(Session,error)，Session 定义 UserID、Login 和仅服务端可见的 UserToken；github.Gateway.RequireAccess(ctx context.Context,userToken string,tenant contracts.TenantRef,manage bool) error；repositories.Service.SetEnabled(ctx context.Context,session auth.Session,tenant contracts.TenantRef,enabled bool,expectedVersion int64) error；各模块提供 Register(r *gin.Engine,service *Service)。本任务在 OpenAPI 定义 BootstrapResponse、SessionResponse、RepositoryListResponse、RunListResponse 和 SetRepositoryEnabledRequest，后者正文只有 enabled/configVersion，CSRF 由独立请求头或表单字段承载。

- [ ] 使用现有 TS sealToken 创建只含测试密钥与 test-token 的兼容 Fixture，不读用户 .env；编写 legacy-auth 场景。已有 Cookie 可登录、state 再次消费拒绝、非维护者不能启用、他人安装不能同步、过期会话失效。核心断言如下：

```ts
expect(await runScenario('legacy-auth')).toMatchObject({
  decryptedToken: 'test-token', legacySessionStatus: 200, replayedStateStatus: 400,
  nonMaintainerStatus: 403, foreignInstallationStatus: 403, expiredSessionStatus: 401,
});
```

- [ ] 运行 pnpm exec turbo run test:migration --filter=@reviewpilot/tooling -- --case legacy-auth，记录认证迁移尚不可用的失败。
- [ ] 实现旧密文和 Cookie 兼容、state/PKCE 一次性消费、会话最多 28800 秒；同步前验证 App ID、安装归属、仓库公开状态和当前 maintain/admin。GitHub HTTP 客户端可注入假服务地址、设置超时和 API 版本，不记录原始错误体或凭证。
- [ ] 实现公开会话、仓库、近期 20 个 Run、同步、配置 bootstrap 与回调；列表按当前可访问安装与仓库过滤。JSON 变更验证 expectedVersion，表单兼容仍校验 Origin 与维护权限；CSRF 为会话绑定的 32 字节随机 base64url token，旧会话首次读取时原子填入 csrf_token，后续不轮询旋转，通过 /api/session 返回并要求 X-CSRF-Token，表单使用隐藏字段。缺 token 的旧页面变更提示刷新，不绕过防护。登录与安装回调不套业务变更 CSRF，但执行各自 state 和归属校验。
- [ ] 重跑 legacy-auth，确认日志与响应不包含密钥和 UserToken；保存配置缺失、未登录和远端不可用三类不同状态。
- [ ] 仅提交本任务文件，提交信息 feat: move authentication and repository governance to Go。

## 任务 4 Outbox 与 Worker 可靠执行

**文件：** Create apps/api/internal/jobs/{service.go,http.go,recovery.go}、apps/api/internal/messaging/{rabbitmq.go,outbox.go}、apps/api/cmd/dispatcher/main.go、packages/worker-runtime/{package.json,src/client.ts,src/consumer.ts,src/result.ts,tests/consumer.test.ts}；Modify apps/worker-review/src/{main.ts,process-job.ts}、apps/worker-review/package.json、apps/worker-evolution/src/main.ts、tests/integration/s1-migration.test.ts、.env.example、turbo.json。

**接口：** jobs.Service.Claim(ctx context.Context,envelope contracts.TaskEnvelope,workerID string)(contracts.ClaimResponse,error)、Heartbeat(ctx context.Context,lease contracts.TaskLease) error、CommitConnection(ctx context.Context,result contracts.ConnectionResult)(contracts.CommitResponse,error)、ReadResult(ctx context.Context,taskID string)(contracts.CommitResponse,error)、Fail(ctx context.Context,lease contracts.TaskLease,code string) error；messaging.Relay.Flush(ctx context.Context,limit int) error；worker-runtime.processDelivery(envelope:TaskEnvelope,client:InternalClient):Promise<'ack'|'retry'|'quarantine'>。InternalClient 对应上述 REST 端点且只持服务身份。

- [ ] 编写 message-recovery 场景：确认丢失后重投、两次领取竞争、旧 token 提交、提交响应丢失后查询、不同 hash 提交冲突。关键断言如下：

```ts
expect(await runScenario('message-recovery')).toMatchObject({
  activeExecutions: 1, acceptedResultRows: 1, oldLeaseStatus: 409,
  changedResultStatus: 409, recoveredByRead: true, acknowledgedBeforeCommit: false,
});
```

- [ ] 运行 pnpm exec turbo run test:migration --filter=@reviewpilot/tooling -- --case message-recovery，记录尚未实现消息路径的失败。
- [ ] 建立 reviewpilot.tasks.v1 direct exchange、reviewpilot.review.v1 和 reviewpilot.evolution.v1 持久队列、持久隔离队列；Go 用 publisher confirms 加 mandatory/return 检测，只有确认且可路由才标记 Outbox sent。Broker 中断后重连并重建 channel 状态；TS prefetch 默认 1、手动 ack。
- [ ] 按 taskId 事务领取并核对消息 tenant/runId/headSha/notificationGeneration，检查 mode=go、安装、仓库、快照与有效期，记录当前 executionEpoch；busy/finished/cancelled/superseded 的重复通知可确认，由每 5 秒恢复扫描保证失联任务重新通知。not-ready 只在数据库安排到 available_at 的通知后确认；临时 API 失败有界延迟重试，不形成立即 requeue 热循环。attempt 最多 5 次，旧 token 或旧 epoch 不可新提交、续租。
- [ ] 在同一事务保存 job_results、jobs=result_ready、Run=Publishing 和唯一 publication_requests，再响应 accepted；相同结果已提交时先按服务权限、身份和哈希查询确认，允许恢复已失效旧租约的同一已接受结果，但禁止它新写入。Worker 查询确认后才 ack；Evolution S1 不领取。Worker 环境移除业务 DATABASE_URL 和全部 GITHUB_*。
- [ ] 重跑 message-recovery 及定向 consumer.test.ts；要求真实 Broker 完成一次确认与重投，所有幂等断言通过。
- [ ] 仅提交本任务文件，提交信息 feat: add outbox delivery and fenced Worker execution。

## 任务 5 Go Webhook 与基础 Check 发布

**文件：** Create apps/api/internal/webhooks/{handler.go,service.go}、apps/api/internal/github/checks.go、apps/api/internal/publisher/{service.go,recovery.go}；Modify apps/api/cmd/{api,dispatcher}/main.go、tests/integration/s1-migration.test.ts、packages/contracts/openapi/s1.yaml 及生成文件。

**接口：** webhooks.Service.Accept(ctx context.Context,deliveryID,event string,raw []byte)(accepted bool,err error)；publisher.Service.PublishConnection(ctx context.Context,runID string) error；publisher.Service.ProcessNext(ctx context.Context)(handled bool,err error)。Go 发布进程从 publication_requests 领取带租约的待发布意图，该表是业务发布事实，不替代跨语言 RabbitMQ 任务交付。

- [ ] 编写 webhook-check 场景，涵盖原始字节改变、超限、同 ID 不同 body、重复事件、2 秒稳定窗口、相同时间戳乱序、发布响应丢失、执行中私有化或撤销。断言如下：

```ts
expect(await runScenario('webhook-check')).toMatchObject({
  invalidSignatureStatus: 401, oversizedStatus: 413, deliveryConflictStatus: 409,
  logicalRunCount: 1, logicalOutboxCount: 1, checkCreateCountAfterLostResponse: 1,
  staleHeadPublished: false, revokedPublished: false, conclusion: 'neutral',
});
```

- [ ] 运行 pnpm exec turbo run test:migration --filter=@reviewpilot/tooling -- --case webhook-check，记录当前 Go 接入与发布缺失的失败。
- [ ] 移植原事件语义：Delivery、Run、job、Outbox 原子提交；严格更旧时间戳不覆盖，相同秒发布前查询当前远端 Head；安装移除、私有化和停用取消在途执行，不自动重新启用仓库。Webhook 按原始字节 HMAC 验证，最多 2 MiB，尽快返回 202。
- [ ] 发布意图使用独立租约、重试和恢复，不借用已 ack 的 Worker 租约。每次写前检查安装、仓库、PR、Head 和发布租约；保留 Check 名称 ReviewPilot · 连接检查、external_id=reviewpilot:{runId}、neutral 和未分析代码说明。按远端 external_id 对账，返回丢失时不直接再次 POST；全部成功才将 Run 和 job 完成。
- [ ] 重跑 webhook-check，确认假 GitHub 记录中无 PR 评论、无 Contents 写操作；远端写和数据库仍不原子，交付说明不宣称 exactly-once。
- [ ] 仅提交本任务文件，提交信息 feat: migrate webhook ingestion and connection Check publishing to Go。

## 任务 6 Web 直连与仓库界面

**文件：** Create apps/web/src/{components/providers.tsx,features/repositories/repository-console.tsx,lib/api-client.ts,lib/repository-actions.ts,lib/repository-actions.test.ts}、apps/web/components.json、apps/web/postcss.config.mjs 及实际使用的 shadcn Button/Card/Badge；Modify apps/web/src/app/{layout.tsx,globals.css,repositories/page.tsx}、apps/web/package.json、apps/web/scripts/next.mjs、pnpm-lock.yaml、apps/web/next.config.ts。

**接口：** createAPIClient(fetcher:typeof fetch):APIClient，对接生成 REST 类型、credentials=include 与 X-CSRF-Token；APIClient.getBootstrap/getSession/listRepositories/listRuns 无参数，分别返回 Promise<BootstrapResponse/SessionResponse/RepositoryListResponse/RunListResponse>；syncRepositories(csrfToken:string):Promise<void>、setEnabled(input:SetEnabledInput):Promise<void>；repository-actions.toggleRepository(client:APIClient,input:SetEnabledInput):Promise<void>。SetEnabledInput 在 api-client.ts 定义为 {tenant:TenantRef;enabled:boolean;configVersion:number;csrfToken:string}，正文使用生成的 SetRepositoryEnabledRequest，不把 csrfToken 放进 JSON 正文。

- [ ] 编写 repository-actions.test.ts：启用调用直接到 Go 路径，携带 configVersion/CSRF；409 不更新成功状态；401/403 显示登录或权限提示。最小断言如下：

```ts
expect(request.url).toBe('/api/repositories/1/2');
expect(request.headers['X-CSRF-Token']).toBe('test-csrf');
expect(request.body).toMatchObject({ enabled: true, configVersion: 3 });
await expect(toggleRepository(client, input)).rejects.toMatchObject({ code: 'VERSION_CONFLICT' });
```

- [ ] 运行 pnpm exec turbo run test:unit --filter=@reviewpilot/web -- src/lib/repository-actions.test.ts，记录客户端未迁移时的失败；本任务新增 Web test:unit 包装，测试数据来自本文件的 fetch stub，不假装端到端。
- [ ] 把 repositories/page.tsx 改成展示壳与客户端组件，TanStack Query 管理会话、仓库与最近 20 个 Run。继续中文说明、配置缺项、未登录、不可用、空列表和 neutral 连接状态，不加入模拟仓库、虚构指标或未实现治理开关。
- [ ] 使用 shadcn 与 Tailwind 只完成当前仓库页基础组件，不做整站重设计；保留 next.mjs 的 Node 环境加载修复，前端仅允许展示配置，不注入数据库、GitHub、模型或内部服务凭证。API 使用相对同源 URL，首次轮询默认 5 秒、页面后台暂停或降频。
- [ ] 重跑定向测试，执行 pnpm exec turbo run typecheck build --filter=@reviewpilot/web；要求 Web 无 persistence/github 运行时依赖，仍保留旧业务路由到任务 8 的切换点，不把保留期作为新架构 BFF。
- [ ] 仅提交本任务文件，提交信息 feat: connect repository console directly to Go API。

## 任务 7 Compose 与完整监控基础

**文件：** Create packages/observability/{package.json,src/index.ts,src/log.ts,src/trace.ts,tests/redaction.test.ts}、apps/api/internal/{observability/setup.go,httpapi/readiness.go}、infra/compose.{dev,prod}.yml、infra/{caddy/Caddyfile,otel/collector.yaml,prometheus/prometheus.yml,prometheus/alerts.yml,loki/loki.yaml,tempo/tempo.yaml,grafana/provisioning/datasources.yml,grafana/provisioning/dashboards.yml,grafana/dashboards/s1.json,rabbitmq/enabled_plugins}、apps/api/Dockerfile、apps/web/Dockerfile、apps/worker-review/Dockerfile、apps/worker-evolution/Dockerfile、scripts/{infra.mjs,s1-observability-check.ts}；Modify compose.dev.yml、.env.example、Turbo 环境白名单、各进程入口和 packages/tooling/package.json。

**接口：** Go observability.Setup(ctx context.Context,service string)(shutdown func(context.Context)error,err error)；TS setupObservability(service:string):Promise<()=>Promise<void>>、safeLog(fields:LogFields):void，LogFields 仅允许 service/requestId/runId/taskId/attempt/traceId/stage/code/durationMs；httpapi.RegisterReadiness(r *gin.Engine,check func(context.Context)error)，不展示连接串。

- [ ] 编写 observability 场景与 redaction.test.ts：同一任务跨 Go、AMQP、Worker 带可关联 traceId；字段白名单拒绝 token、源码和完整错误体；Collector 不可用时业务仍完成；未授权访问 internal API 失败。核心断言如下：

```ts
expect(await runScenario('observability')).toMatchObject({
  linkedServices: ['api', 'dispatcher', 'worker-review'], secretsCaptured: false,
  completedWithCollectorDown: true, publicInternalRouteStatus: 404,
});
```

- [ ] 运行 pnpm exec turbo run test:migration --filter=@reviewpilot/tooling -- --case observability，记录追踪尚未贯通的失败。
- [ ] 接入 OpenTelemetry，Go slog 与 TS JSON 同时保留 stdout 并通过受控 OTLP 日志桥接进入 Collector；Collector 转发 Loki/Tempo，暴露 Prometheus 指标供抓取；显式传播 HTTP 和消息上下文，单次数据发送有超时与队列上限。默认日志和 Trace 7 天、指标 30 天，不以 runId/PR number 作为指标 label。
- [ ] 配置 Grafana 数据源、S1 实际任务仪表盘和失败/积压/失联/磁盘告警，模型和 Memory 指标不展示假值。开发公开入口为 localhost:8080；页面内部 3000，Go 公网 4000、内网 4001，Caddy 只转发公网 Go 路径，internal 不转发。容器内监听 0.0.0.0，宿主可选调试端口只绑定 loopback。[Caddy 反向代理](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy)。
- [ ] 建立 Compose 健康、重启、资源与卷配置，保留现有 reviewpilot_pg 开发卷映射，不切换到无数据的新卷冒充成功迁移；prod 使用独立明确卷和 secrets，不使用示例密码。监控 profile 开发可选、Beta 开启；Dockerfile 的应用启动仍经 Turbo，Caddy 不做业务。
- [ ] 执行 pnpm exec turbo run infra:check --filter=@reviewpilot/tooling 与 observability 场景，要求配置可解析、无公网数据库/Broker/internal 端口、真实 Trace 可关联且 Collector 失效不阻塞。记录生产未部署、TLS、外部探测和异机备份仍需用户环境，不把 Compose 校验写成上线验收。
- [ ] 仅提交本任务文件，提交信息 feat: add Compose observability and protected service routing。

## 任务 8 切换预检与交付

**文件：** Create apps/api/internal/cutover/{service.go,service_test.go}、scripts/s1-cutover-check.ts、docs/runbooks/s1-go-migration.md、docs/reports/s1-go-migration.md；Modify README.md、docs/runbooks/s1-local-setup.md、原分阶段计划的当前状态和 S2 前置条件、packages/persistence/package.json、packages/tooling/package.json、根 package.json。切换后删除已替代的 apps/web/src/lib/server.ts、apps/web/src/app/api/**、apps/web/src/app/auth/**、apps/web/src/app/installations/callback/route.ts；删除前列出精确实际文件，保留 Git 历史与回退版本，不删除数据库或用户文件。

**接口：** cutover.Service.Preflight(ctx context.Context)(Report,error)、Switch(ctx context.Context,expectedEpoch int64,target string)(Report,error)；Report 明确定义 mode、epoch、activeLeaseCount、unsubmittedCount、publicationPendingCount、unreconciledCheckCount、canSwitch，使用任务 2 的 runtime_control。Go dispatcher、领取和恢复每次核对 mode/epoch；切换 CLI 只通过 Turbo 的 cutover:check 调用 cmd/migrate，不默认执行 Switch。

- [ ] 编写 cutover 场景：旧有效租约阻止切换，过期待迁移任务保持 taskId，已完成任务不重通知，待发布记录先对账，epoch 冲突拒绝。断言如下：

```ts
expect(await runScenario('cutover')).toMatchObject({
  switchWithActiveLeaseRejected: true, pendingTaskIdPreserved: true,
  completedRenotified: false, unreconciledPublishBlocksSwitch: true,
  staleEpochStatus: 409, rollbackLeavesTwoConsumers: false,
});
```

- [ ] 运行 pnpm exec turbo run test:migration --filter=@reviewpilot/tooling -- --case cutover，记录预检与切换缺失的失败。
- [ ] 实现预检和显式切换，初始 mode=legacy；停旧消费者并确认排空或租约失效后，在事务中核对 epoch，给未完成可执行任务创建唯一 Outbox，切到 go、增加 epoch、作废旧 token，再启动新消费者。预检对所有非终态 Run 按远端 external_id 对账，即使本地 check_id 为空也不能跳过。旧历史版本不理解 mode，因此必须实际停止，不能仅修改数据库就认为它已停。
- [ ] 编写回退流程：停新消费者、等待或隔离在途写入、对账 publication 和 Outbox、记录切换后新增事实，明确可由旧服务接管的任务再转回 legacy；result_ready 等旧版本不认识的状态只在对账后安全转换。不能盲目恢复旧备份；有无法确认的远端写入就停下请求人工处理。
- [ ] 运行 cutover 场景后切换本地明确的专用环境；移除旧 Next 业务处理路径和应用侧 TS GitHub/DB 依赖，现有 packages 中需要的兼容 Fixture 和旧 SQL 保留为非应用运行时。db:migrate 只剩 Go workspace，避免 Turbo 同时运行两套迁移器；旧集成断言移植到新路径，旧测试不因导入已删除入口而假绿。
- [ ] 执行一次 pnpm exec turbo run verify:migration --filter=@reviewpilot/tooling。该任务只汇总契约检查、受影响包构建/静态检查、上面六个固定集成场景和一个手工仓库页检查清单；不重复执行浏览器矩阵、压力或模型评测。报告每项实际结果和受阻项，不将缺服务跳过算通过。
- [ ] 在用户提供并授权专用 GitHub App 后，按运行手册执行登录、安装、启用、PR、neutral Check；未提供则保持外部待验收，不进行真实写入。切换 APP_URL 到 8080 或生产域名需同步配置真实 App 回调，不自动修改外部设置。
- [ ] 仅提交已验收实现与文档，提交信息 feat: finish S1 Go migration with reversible cutover；停止并等待用户，不进入 S2。

## 设计覆盖与后续边界

| 设计要求 | 本计划对应 | 后续阶段 |
| --- | --- | --- |
| Go、REST、无 BFF、会话与仓库管理 | 1、3、6、8 | S2 扩展审查结果 API |
| GORM、旧 SQL、标识和权限兼容 | 2、3、5、8 | S4 增加规则与向量表 |
| Outbox、RabbitMQ、fencing、幂等与发布对账 | 2、4、5、8 | S2 复用实际 AI 结果 |
| LangChain、LangGraph、固定快照与模型预算 | S1 保留快照与权限边界，不安装 AI 运行框架 | S2 AI 子项目另写计划 |
| pgvector、自进化、人工纠错、Replay/Shadow | 不在 S1 实现 | S4 和评测门禁计划 |
| 监控、隐私、Trace、单机部署与告警 | 7、8 | S2/S4 增加业务指标；Beta 验证备份与外部探测 |
| 质量、采纳率与人工标签口径 | S1 不显示虚构质量数据 | S4/S5 评测实现 |
| 每阶段停下、最小验证、无 subagent | 全局约束与 8 | 后续阶段继续执行 |

## 执行交接与审阅

- [x] 用户确认本计划符合预期，再在当前会话按 executing-plans 自行实施，不安排 subagent 或独立代理复核。
- [ ] 每个任务提交前自行检查 diff、定向验证证据和与契约的一致性；若需求超出计划或需购买、真实部署、破坏数据，则报告并请求方向。
- [ ] S1 迁移结束后准确区分本地代码验收与真实 GitHub 验收；真实接入通过前，不宣称具备进入 S2 的全部条件。

本计划已获用户批准；执行方法沿用已指定的当前会话直接开发，不重新推荐或询问 subagent 方案。只执行 S1 迁移，完成后停止。
