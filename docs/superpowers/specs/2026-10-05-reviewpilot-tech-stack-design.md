# ReviewPilot 技术栈与 S1 迁移设计

日期：2026-10-05。状态：书面设计已于 2026-10-05 获用户确认；S1 迁移计划于 2026-10-06 获批，正在实施，尚未完成切换。

本设计服务于两个目标：做出能持续使用的 AI 代码审查产品，并形成体现 Go 后端、Web 全栈和 AI 应用工程能力的作品。技术复杂度用于解决任务可靠性、审查质量和自进化治理，不以组件数量作为竞争力指标。

本轮只交付设计文档。用户审阅书面设计后，再编写实现计划；计划获批并明确执行方式后才开始开发。实施时直接使用现有 review-pilot 项目，每完成一个阶段即停下，不使用 subagent，仅进行与风险相称的必要验证。

## 设计范围与现有实现

产品仍面向 GitHub 公共 JavaScript 和 TypeScript 仓库，提供 GitHub App 与中文 Web 控制台。默认每个 PR 发布 0–3 条有证据的问题，证据不足允许放弃；支持仓库记忆自进化、人工纠错、版本追踪、Replay、Shadow 与回滚。首版只静态读取和解析，不执行 PR 代码。

当前 S1 是 TypeScript 实现：Next.js 同时提供页面和业务 API，PostgreSQL 保存业务与租约任务，Review 和 Evolution Worker 独立运行。登录、安装归属、维护权限、Webhook 验签去重、基础连接 Check 和中文仓库页已有本地实现。真实 GitHub 安装验收仍需凭证和专用仓库；S1 不调用模型，连接检查完成不代表 AI 审查完成。Next.js 根环境文件加载修复已单独提交。

本设计确定目标技术栈，以及迁移 S1 必须保持的行为。它不是详细实现计划，也不代表已安装 Go、RabbitMQ、pgvector、LangChain、LangGraph 或监控组件。

文档关系如下：

- [产品与系统设计](2026-10-04-reviewpilot-design.md) 的产品范围、质量门禁、规则信任、安全与成本约束继续有效。
- 本设计替代旧设计中的 Web/API 合一、全 TypeScript 业务后端、PostgreSQL 作为主要消息队列，以及向量检索延期的技术方向。规则和历史案例向量检索从 S4 Memory 阶段引入，不提前进行全仓库 Embedding。
- [ADR 0001](../../adr/0001-runtime-and-dependencies.md) 保留为已交付 S1 的运行时记录，不据此声称新架构已经实现。
- [原分阶段计划](../../../2026-10-04-reviewpilot-implementation-plan.md) 的 S2–S6 产品顺序保留，但旧模块和任务命令不能直接作为新架构的执行计划。新计划应先安排独立的 S1 迁移阶段，并更新受影响任务。

新增混合语言、消息队列和完整监控会增加工作量，原 4–6 周属于初始预算，不是本次选型后的交付承诺。具体依赖版本和工期在计划与实施时根据兼容性验证锁定。

## 技术选择

| 层次 | 选择 | 主要用途 |
| --- | --- | --- |
| 前端 | Next.js、React、TypeScript | Web 页面与交互，不设置 BFF |
| UI | shadcn/ui、Tailwind CSS | 中文审查与治理界面 |
| 前端数据 | TanStack Query | REST 数据缓存、刷新、变更和状态轮询 |
| 业务后端 | Go、Gin | 认证、GitHub 接入、调度、发布与治理 API |
| 业务持久化 | GORM、PostgreSQL | 事务、权限、运行记录、预算与规则版本 |
| AI Worker | TypeScript、Node.js | JS/TS 代码分析、模型工作流与评测 |
| AI 框架 | LangChain、LangGraph | 模型适配、结构化输出与受控可恢复工作流 |
| 消息传递 | RabbitMQ、PostgreSQL Outbox | 跨语言任务交付与可靠事务入队 |
| 规则检索 | PostgreSQL、pgvector | 精确范围匹配与关键词、向量混合召回 |
| 契约 | REST、OpenAPI、版本化 JSON Schema | HTTP API 与队列消息约定 |
| 工程 | pnpm workspace、Turborepo | 统一任务入口与依赖图 |
| 部署 | 单服务器 Docker Compose | 本地环境和首版受控 Beta |
| 可观测性 | OpenTelemetry、Collector、Prometheus、Loki、Tempo、Grafana | 指标、日志、链路、仪表盘与告警 |

Go 适合用户已有经验及业务治理；TypeScript Worker 复用 JS/TS 生态并支持 LangChain 和 LangGraph。首版不另建 Python 服务，不使用 gRPC，也不引入 Redis、Kubernetes 或自主多 Agent 调度。

GORM 是已选择的持久化方式，不替换为 sqlc。复杂锁、Outbox 和检索 SQL 可以在存储模块内使用参数化 SQL；领域服务不依赖 GORM Model 作为 HTTP 契约。GORM 支持事务与锁，pgvector-go 提供 GORM 集成。[GORM 事务](https://gorm.io/docs/transactions.html)、[GORM 锁与高级查询](https://gorm.io/docs/advanced_query.html)、[pgvector-go](https://github.com/pgvector/pgvector-go)。

## 组件职责与权限边界

| 组件 | 负责 | 不负责 |
| --- | --- | --- |
| Next.js Web | 仓库、Review、Memory、评测、设置与反馈界面 | 业务数据库访问、GitHub Token、模型调用和业务 API |
| Go API | 登录、会话、维护权限、Webhook、配置、规则与预算治理 | 生成 Prompt 和自主模型规划 |
| Go 调度与发布进程 | Outbox 投递、执行租约、重试恢复、GitHub 发布与对账 | AI 候选生成与代码语义分析 |
| Review Worker | 固定快照的受限分析、候选生成、证据验证和发布建议 | 直接发布 GitHub、改变权限或激活 Memory |
| Evolution Worker | 反馈归因、候选记忆、Replay 和 Shadow 评测 | 改写人工锁定规则或绕过门禁激活 |
| PostgreSQL | 业务事实、审计、预算、版本与业务阶段产物 | 代替 RabbitMQ 的消息交付职责 |
| RabbitMQ | 任务通知、消费确认和失败隔离 | 决定仓库授权、最终状态或发布结果 |

Go API 与调度进程共享同一个 Go 模块，以不同入口运行，不拆成大量微服务。TS Worker 通过受鉴权的内部 REST 读取所需业务数据和提交结果，不直接读写业务表。

LangGraph 检查点可保存在 PostgreSQL 独立 schema，由仅具备该 schema 权限的 Worker 数据库角色访问。它保存工作流执行状态，不是业务规则库。业务 Memory、预算、GitHub 凭证和发布记录只由 Go 管理。检查点和业务 Artifact 均按任务身份与保留策略隔离，不把完整源码写进遥测。

首版少量阶段结果由 Go 保存到 PostgreSQL JSONB。大量源码和大型 Artifact 的存储扩展另行设计，不在 S1 迁移阶段自动增加对象存储服务；始终保留内容哈希、归属、大小限制和删除能力。

## Web 与 REST 契约

浏览器直接请求 Go API。生产入口通过同源反向代理把页面请求转给 Next.js，把业务 API、OAuth 回调、安装回调和 Webhook 转给 Go。反向代理只路由请求，不承担 BFF 业务。内部 Worker API 不映射到公网入口，仍使用服务身份鉴权。

Go 独立拥有 OAuth state、PKCE、会话、令牌加解密和仓库权限校验。浏览器只持有 HttpOnly 不透明会话 Cookie。变更操作验证 Origin 和 CSRF，生产使用 Secure Cookie；开发若不同源，只允许明确配置的来源，不使用携带凭证的通配 CORS。

REST 使用 OpenAPI，生成 Go DTO 和 TypeScript 客户端或类型，错误统一包含稳定错误码与 requestId。规则更新等并发操作携带预期版本，冲突明确返回，不覆盖别人的修改。生成类型不能代替运行时校验；Schema、租户、范围与授权均由服务端检查。

RabbitMQ 消息使用独立的版本化 JSON Schema，至少包含 messageId、taskId、runId、任务类型、契约版本、归属标识与追踪上下文。消息只承载通知与引用，不携带密钥、完整仓库或可执行模型指令。消息里的安装、仓库和 SHA 必须与 Go 的任务记录一致，不能直接作为授权依据。

`packages/contracts` 保存契约源文件，生成代码不手工修改。OpenAPI 规格版本以 Go 和 TS 生成器共同支持的版本为准，不默认最新规格已被工具支持。[OpenAPI](https://spec.openapis.org/oas/latest.html)、[JSON Schema](https://json-schema.org/overview/what-is-jsonschema)。

## 任务数据流与故障恢复

### 创建与投递

1. Go 按原始字节验证 Webhook 签名，并校验事件、安装、仓库和 PR。
2. 同一 PostgreSQL 事务保存 Delivery、固定版本的 Run、任务记录与 Outbox；重复事件不能重复创建逻辑任务。事务提交后尽快响应 GitHub。
3. Go Relay 将 Outbox 发送到持久化 RabbitMQ 队列，使用持久消息、publisher confirms，并处理不可路由返回。确认后标记成功；确认丢失可能导致重发。
4. Review 和 Evolution 使用分离的队列与并发配额，避免后台学习占满实时审查资源。

任务按至少一次交付设计。数据库唯一键和 messageId 防止重复业务创建，不声称外部 GitHub 操作恰好执行一次。[RabbitMQ 确认机制](https://www.rabbitmq.com/docs/confirms)。

### 执行与完成

Worker 使用手动 ack 与有限 prefetch。收到通知后，通过 Go 原子领取执行租约，取得有效期和 fencing token。Go 以数据库时间判断租约有效性；Worker 心跳续租。租约保护业务写入，ack 确认消息处理，两者不能相互替代。

Worker 每次读取敏感上下文、开始高成本阶段或提交结果时都验证执行权。内部 API 校验任务归属、当前 token、版本和输入哈希。旧执行者即使仍在运行，也不能覆盖新执行者的结果。

Worker 先把结果和业务阶段状态提交给 Go，再 ack 消息。提交携带稳定标识和内容哈希；响应丢失时先查询或重复提交同一结果。已完成任务可安全确认重复通知；同一任务不同内容的重复提交应被拒绝，不能静默覆盖。

结果被接受不等于 GitHub 已发布。Go 保存待发布状态并安排发布；发布前重新验证安装授权、仓库公开状态、PR Head 与运行快照。发布完成后才更新对应最终状态，部分覆盖、过期和取消不能包装成全量成功。

### 重试与恢复

| 失败条件 | 必须行为 |
| --- | --- |
| Outbox 发送成功但确认丢失 | 重发稳定消息，消费侧按逻辑任务去重 |
| Worker 崩溃或失联 | 租约到期后由 Go 恢复调度，旧 token 失效 |
| 重复通知遇到有效执行者 | 不并行执行；过期任务恢复机制保证后续不丢任务 |
| 模型、GitHub 或临时网络失败 | 按类型进行有上限的退避，限流遵守服务端提示 |
| 非法消息或不可恢复输入 | 记录并隔离，不立即 requeue 形成热循环 |
| 结果已保存但 ack 丢失 | 重投时读取已提交状态，不重复生成或发布 |
| PR 更新、仓库私有化或授权撤销 | 旧 Run 标记 Stale 或 Cancelled，拒绝未授权读取和发布 |
| GitHub 写入成功但响应丢失 | 查询远端标识并对账，再决定是否重试 |

Go 按业务任务状态调度重试，通过新 Outbox 重新通知。RabbitMQ 死信用于失败隔离，不等于自动完成退避和次数上限；需要检查队列与死信持久性，不把配置 DLX 等同于绝无丢失。[RabbitMQ 死信机制](https://www.rabbitmq.com/docs/dlx)。

## AI 工作流与模型治理

LangChain 从首个 AI Review 阶段用于模型适配、结构化输出和受限读取工具。LangGraph 编排固定节点与有限分支，例如变更理解、上下文检索、候选生成、反证与证据验证、去重排序和提交结果。不允许模型自主增加工具、无限重试或直接调用 GitHub 写操作。[LangGraph 工作流](https://docs.langchain.com/oss/javascript/langgraph/workflows-agents)。

恢复只复用匹配 Run 快照、节点输入哈希和流程版本的稳定检查点。不同执行尝试隔离可写检查点，防止旧 Worker 污染新执行；跨尝试恢复仍需 Go 授权。LangGraph thread 或 checkpoint ID 不是业务完成、仓库权限或 Memory 激活凭证。[LangGraph 持久化](https://docs.langchain.com/oss/javascript/langgraph/persistence)。

Run 冻结 Base/Head SHA、Pipeline、Prompt、模型配置、可信仓库规则、Active Memory 集合及检索配置。模型输出必须通过 Schema 和确定性校验：路径、行号、证据归属、失败条件、是否本次引入、是否已有保护和重复根因。通过结构化输出不等于 Finding 正确。

模型调用只经过 Worker 的 Model Gateway。Go 负责调用前原子预算预留，Worker 持供应商凭证调用并提交用量结算。Review、验证、重试、Embedding、Replay 和 Evolution 共用总预算。费用记录价格版本与估算口径；响应丢失或用量未知时保守保留预留并对账，不假定未收费。备用模型需显式配置且满足同样门禁，不偷偷更换质量和成本条件。

首版不在本设计固定供应商或具体模型。计划阶段验证结构化输出、限流、价格与数据处理条款后再锁定配置；LangChain 适配器不能消除供应商之间的能力差异。

## 规则记忆与混合检索

Memory 使用关系字段保存归属、类型、范围、来源、状态、人工锁定、当前版本与证据；变化创建不可变版本和审计事件。JSONB 保存需要演进的结构化正文，不把所有可查询元数据塞入自由文本。

强制规则按权限、有效状态和精确 Scope 加载，不经过向量 top-K 淘汰。其他规则与历史案例限制在授权仓库、快照和范围内，再做关键词与向量混合召回，最终验证适用条件。相似度不代表证据成立。

优先级继续使用人工锁定规则、可信已合并仓库规则、人工纠正 Memory、自动 Memory、平台通用知识的顺序。同级冲突与例外按产品设计处理，检索分数不能覆盖权限或人工锁定。

从 S4 起对规则说明与历史失败案例生成 Embedding。代码上下文依赖 Diff、路径、Import、AST 和符号引用，不默认全仓向量化。pgvector 提供向量存储与相似度检索，不会自己调用 Embedding 模型。[pgvector](https://github.com/pgvector/pgvector)。

Embedding 单独记录模型、维度、内容哈希和索引版本，变化后不直接混用旧向量。独立验证所需接口，不假定支持聊天就支持 Embedding。服务不可用时降级到结构化和关键词检索，记录覆盖变化，不阻塞普通 Review；上下文不足时放弃相应 Finding。关键词分词按实际中英文内容验证，不假定默认全文配置已解决中文检索。

### 自进化与人工纠错

反馈先校验维护权限、独立来源和作用范围。未回复不是正确标签，resolved 不是确定正确，同一 PR 的多个行为不算多个独立证据。单次误报默认只纠正本次 Finding，不能成为全仓永久豁免。

Evolution Worker 提出候选并执行独立 Replay 和未来 PR Shadow；Go 根据版本、锁定、证据与风险门禁决定激活、隔离或回滚。普通自动 Memory 达到门槛后自动生效，不逐条等待人工审批；标签不足保持 Shadow。自进化不等于在线训练基础模型，不允许自动改全局 Prompt 或扩大工具权限。

保持原设计的 Balanced 初始门槛：3 个独立强支持观察或一次明确长期指令、内部评分至少 0.85、至少 5 次 Shadow 命中、至少 10 个有标签适用案例且含 3 个反例、经验有效率至少 80%、没有已知高风险回归。这是工程门槛，不证明统计显著提升。候选生成案例不能同时作为独立验证证据。

人工可以纠正、限定范围、添加例外、锁定、暂停或撤销。自动激活使用预期版本与事务检查，人工修改后旧评测不能直接激活新内容；人工锁定不被自动覆盖。高影响业务和安全保护不能仅凭负反馈自动取消。

## 可观测性与产品评估

| 组件 | 保存或处理的内容 |
| --- | --- |
| OpenTelemetry SDK | Go 与 TS 服务的链路和指标采集、上下文传播 |
| OpenTelemetry Collector | 统一接收、脱敏、限流、批处理和转发 |
| Prometheus | 时间序列指标与规则计算 |
| Loki | 结构化日志 |
| Tempo | 分布式 Trace |
| Grafana | 查询、仪表盘与告警 |

这套组合是本项目的架构选择，不是某个框架自动提供的全套能力。[Collector](https://opentelemetry.io/docs/collector/)、[Prometheus](https://prometheus.io/docs/introduction/overview/)、[Loki](https://grafana.com/docs/loki/latest/get-started/overview/)、[Tempo](https://grafana.com/docs/tempo/latest/)、[Grafana](https://grafana.com/docs/grafana/latest/)。

Go 使用 slog，TS 输出统一字段的 JSON 日志，日志由采集管道进入 Loki。关联字段包含 requestId、runId、taskId、attempt、traceId、阶段与错误码；HTTP 和 RabbitMQ 边界显式传播追踪上下文。[Go slog](https://pkg.go.dev/log/slog)。

仪表盘关注积压及最老等待时间、Worker 心跳、失败与重试率、阶段耗时、模型超时、Token 与估算成本、预算余量和基础设施健康。告警关注长期积压、持续失败、Outbox 无法投递、Worker 失联、预算阈值和磁盘容量。指标标签不使用 runId、PR number 等无限增长的值，这些信息放日志或 Trace。

默认日志与 Trace 保留 7 天，指标保留 30 天，并配置磁盘与资源上限；这是可调整的运维默认值，不改变业务审计与源码保留政策。显式配置采样和遥测过滤。监控写入采用有界异步机制，监控不可用不能阻塞业务；同机监控不能报告整台主机断电，Beta 发布前需另配置外部存活探测，不把仪表盘当成备份或高可用。

不采集密钥、完整提示词、完整源码或模型隐藏思维链。默认自托管监控，不自动向 LangSmith 或其他第三方上传仓库数据；外部服务需另行确认数据处理和授权。

产品 Review Trace 由业务数据库保存结构化阶段记录、证据引用、过滤原因、规则版本、覆盖范围与估算成本，不能依赖会被采样和过期清理的监控 Trace。Evaluation Lab 展示固定版本的 Baseline/Candidate、人工标签与回归结果。采纳率是行为指标，不作为准确率；精确率、漏报和引用有效性必须注明数据集、样本数和标签覆盖。

S1 迁移接入基础链路、日志和基础设施指标；S2 接入模型与 Review 指标；S4 接入 Memory 和评测指标。受控 Beta 交付完整监控，S1 不为展示图表虚构 AI 精度或调用数据。

## Monorepo 与命令约定

以下是目标目录职责，不代表文件已经创建：

| 目录 | 职责 |
| --- | --- |
| apps/web | Next.js 展示与交互 |
| apps/api | Go module，包含 API、调度发布入口、领域与存储适配 |
| apps/worker-review | TypeScript Review 消费入口 |
| apps/worker-evolution | TypeScript Evolution 消费入口 |
| packages/contracts | OpenAPI、消息 Schema 与生成客户端 |
| packages/model-gateway | 模型适配、输出校验与预算调用协议 |
| packages/analyzer 与 packages/retrieval | 静态代码上下文、Scope 和记忆召回 |
| packages/review-engine 与 packages/evaluation | 受控流程、验证与版本评测 |
| packages/observability | TS 遥测和脱敏约定 |
| packages/tooling | 全仓检查与跨语言集成任务入口 |
| infra | Compose、监控、反向代理和部署资产 |

Go 内部保留领域与持久化模块，不强行共享 TS 业务实现。现有 TS domain、github、persistence 在职责切换前保留，迁移验收后仅移除已替代部分，不删历史迁移、Fixture 或用户文件。

pnpm 管理 JS 依赖，Go modules 管理 Go 依赖；Turbo 编排两者，不能代替 Go 编译器或模块管理。Go 应用提供 workspace 任务包装，统一工程入口仍为 `pnpm exec turbo run <task>`。Go build、test、静态检查和迁移由任务内部调用，不提供绕过 Turbo 的另一套主要流程。

Go 缓存输入包含源码、go.mod、go.sum、契约源文件与生成代码，输出包含编译产物。TS 保留现有依赖图。服务、Worker、迁移、部署、格式化、集成测试和付费评测关闭缓存；纯构建和确定性检查可用本地缓存，不自动启用远程缓存。

现有命令在迁移阶段保持兼容。新增契约生成、Go 检查与部署任务的名称在计划中确定，不把未创建的命令写成当前可执行步骤。

## 数据库迁移与 S1 切换

### 迁移范围

独立 S1 迁移阶段只改变基础工程和接入实现：把业务 API、认证、Webhook、GitHub 写入和任务治理迁到 Go；接入 RabbitMQ、Outbox、REST 契约、Web 直连和基础完整监控配置。Review Worker 保持不调用模型的连接任务处理；Evolution 保持未启用学习。S2 才引入模型工作流，S1 不提前实现 Memory 或 AI 审查。

保留安装和仓库标识、启用开关、维护权限、Run UUID、去重键、快照与远端 Check 对账标识。Go 使用显式表名和列映射读取现有 Schema，不让 ORM 默认命名创建平行业务表。

继续使用版本化 SQL，保留已执行文件哈希、迁移账本与互斥机制。不改写既有 0001 和 0002，不在服务启动时调用 GORM AutoMigrate。新增表和列使用后续迁移；不兼容变化先扩展、后切换、再清理。pgvector 扩展和 Embedding 表在 S4 按实际需要引入。

### 切换与回退

1. 在专用环境验证授权、Cookie、OAuth PKCE、密文格式、Schema 读取和基础 Check，保存数据库备份与已知可运行版本。
2. 暂停领取新任务，排空旧 Worker 或让租约过期；旧 PostgreSQL 消费者与新 RabbitMQ 消费者不能同时处理同一任务。
3. 将未完成任务按稳定 taskId 映射到 Outbox；完成、失效或取消的任务不重新运行。切换期 Webhook 要么可靠入库，要么明确返回失败，并检查 GitHub 失败投递记录、安排人工补投，不能响应成功后丢弃。GitHub 不会自动重投失败的 Webhook，不能只靠返回失败保证恢复。[GitHub 失败投递处理](https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries)。
4. 切换公网业务路由到 Go，再启用新消费者；对账数据库、任务和 GitHub Check，确认没有重复发布。
5. 失败时先停新消费者并隔离执行权，检查发布与 Outbox 状态，再恢复兼容旧服务。不能盲目恢复备份抹掉切换后事实，也不能回退成同时运行两套消费者。

认证迁移优先兼容现有会话和密文；无法安全兼容时明确使会话失效并重新登录，不复制明文令牌或绕过校验。破坏性数据清理不属于本轮授权，实施时需要精确范围与恢复方案。

### 阶段验收与停止点

保留关键验证：OAuth 与维护权限、Webhook 原始验签和事务去重、Outbox 重投与结果幂等、租约失效、旧 SHA 不发布、GitHub 响应丢失对账、Web 直连，以及一条 Go 到 Worker 的可追踪链路。集成使用专用 PostgreSQL/RabbitMQ 和可控 GitHub 服务，不操作用户现有数据。

通过 Turbo 执行受影响包的必要构建、静态检查和关键测试，不增加全面浏览器矩阵、压力测试或大规模模型评测。模拟服务不能替代真实安装；凭证不足时准确记录待验收项，不宣称线上闭环完成。

S1 迁移交付后停止，报告保留能力、验证证据、外部待验收项和恢复方式。用户确认继续且真实接入满足进入条件后才实施 S2。每个阶段完成后停下的约束同样适用于后续阶段。

## 部署与运维边界

单服务器 Compose 运行 Web、Go API、Go 调度进程、两个 TS Worker、PostgreSQL、RabbitMQ 与监控组件。开发按需启动监控 profile，Beta 开启完整监控。数据库、Broker 和采集端口不默认暴露公网，管理界面需鉴权和网络限制，公网入口使用 HTTPS。

Compose 支持单服务器生产运行，但本设计不是高可用架构。命名卷不等于备份；配置异机备份、保留期限、恢复演练、健康检查、重启策略与资源限制，防止监控磁盘增长拖垮业务。[Compose 生产部署](https://docs.docker.com/compose/how-tos/production/)、[Docker 数据卷](https://docs.docker.com/engine/storage/volumes/)。

安装撤销或仓库私有化时停止任务并撤销相关读取与发布能力，不将暂停自动解释为立即删除全部历史。用户发起删除时，清理范围包含业务数据、Embedding、Artifact、检查点和遥测关联元数据；保留原产品的最小化审计与备份删除策略，不以可观测性为由永久保留源码。

设计确认不授权购买服务器、模型或外部告警服务，也不授权真实 GitHub 部署。外部配置需专用环境和相应授权；机器规格、预算上限和性能结论以实测与账单为准。

## 对个人竞争力的交付要求

最终展示真实闭环：PR 触发、可靠执行、证据审查、人工反馈、Memory 版本验证及回滚。作品材料包含职责与信任边界、失败恢复复盘、带样本口径的版本对比、部署恢复说明和可复现的受控演示。

Go 体现认证授权、事务与任务治理；Web 体现结果追踪和人工纠错；AI 工程体现受控工作流、证据验证、评测与预算；监控体现跨语言排障和运行责任。所有质量、时延和成本结果标明实测条件，不用演示数值替代真实成果。
