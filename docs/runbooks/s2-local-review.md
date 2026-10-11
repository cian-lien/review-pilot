# S2 本地审查与运维

## 边界与默认状态

S2 仅公共 JS/TS 仓库，静态读取固定 Base/Head，不安装依赖、不执行 PR 代码。Go 管理数据库、GitHub、租约、预算、产物和发布；Worker 通过内部 REST 执行候选生成与独立验证；Web 同源直连 Go API。

默认 `REVIEW_MODE=disabled`、`REVIEW_REAL_ALLOWED=false`、`GITHUB_REVIEW_PUBLISH_ENABLED=false`。S1 的 v1 / connection-check / foundation-v1 保留；S2 独立使用 v2 / ai-review / review-v1。`/api/health` 与旧 bootstrap 的 foundation 标志不是 S2 开关；查看 Run 的 pipelineVersion / provenance 才能区分审查类型。

本阶段未授权调用真实模型、写真实 GitHub、迁移业务数据库、启动生产或购买服务。下列真实配置仅是未来预检说明，不是本次操作。

## 可复现的隔离演示

需要锁文件依赖、Node 24+、pnpm 12.8.1、Go 1.27.1、运行中的 Docker。Docker 仅创建具有 `reviewpilot.test-owned=true` 标签和随机端口的专用 Broker / Collector；PostgreSQL 数据目录位于本次临时目录。不会启动现有 Compose，也不会读取项目 `.env`。

```bash
pnpm install --frozen-lockfile
pnpm exec turbo run test:review --filter=@reviewpilot/tooling -- --case pipeline
```

该场景实际执行签名 Webhook → Go v2 Outbox → RabbitMQ → Review Worker → 内部 REST → 固定 SHA 上下文 → fake 模型两阶段 → Go 接受结果 → 假 GitHub 发布 → 带会话的公开查询 DTO。

同一场景包含三个 Run：正常 Run 的候选和验证共两次模型调用；重复投递及提交响应丢失不会增加最终结果或调用；未知用量 Run 保留预留额度并阻断；第三个 Run 在接受结果后发生 Head 更新，禁止发布。

成功时输出本次 `simulated=true` verdict（结果 1 份、正常调用 2 次、最多 3 个已发布根因，以及未知预留、Stale、Trace、敏感标记断言）。资源退出即清理，演示结果不是可长期浏览的业务数据；当前测试验证公开 DTO 和页面构建，没有浏览器实测。缺少测试、skip、过期/缺失报告和基础设施失败均返回非零，不算通过。

完整且有界的 S2 验收：

```bash
pnpm exec turbo run verify:review --filter=@reviewpilot/tooling
```

包括契约漂移、受影响静态/构建、定向 TS 与 Go 单元、六项 S2 数据库/流水线场景、S1 message-recovery/cutover 必要回归及只读基础设施配置验证。未运行压力、浏览器、多模型矩阵或重复 S1 六场景套件。

## 部署模板与未来真实接入预检

`infra/compose.dev.yml` / `compose.prod.yml` 只更新模板，完整应用镜像未运行验收。非敏感模型 JSON 由三个控制/执行服务共享只读挂载；模型密钥仅挂 Review Worker，Go / Web / Evolution 不挂。私钥、模型配置和凭证放仓库外；不得将真实密钥提交 Git 或粘贴给 AI。

Compose 关闭状态不加载价格占位文件。未来经授权接入时，须显式设置：

- `REVIEW_MODE=real`、`REVIEW_REAL_ALLOWED=true`；发布另需 `GITHUB_REVIEW_PUBLISH_ENABLED=true`，不能用前两个开关代替。
- 宿主 `REVIEW_MODEL_CONFIG_HOST_PATH` 为真实非敏感 JSON；容器 `REVIEW_MODEL_CONFIG_CONTAINER_PATH=/app/config/review-model.json`。宿主直跑使用 `REVIEW_MODEL_CONFIG_FILE`。
- 开发模板 `MODEL_API_KEY_HOST_PATH` 指向仓库外密钥文件；生产使用 `${SECRET_DIRECTORY}/model_api_key` secret。两种模板都需显式 `MODEL_API_KEY_CONTAINER_PATH=/run/secrets/model_api_key` 才读取密钥；默认空值。
- 三个服务使用相同 modelConfigVersion / priceVersion / 地址 / 配额；修改配置内容必须更换版本。Go 在首次预留前比较非敏感配置 hash，已冻结 Run 不自动改用新模型。
- 独立核对 HTTPS OpenAI-compatible Chat Completions、非流式结构化 JSON、明确 Token 用量、仅 Token 计费和价格单位。目前真实 tokenizer 仅允许已知 gpt-4o 系列的保守字节上界；不兼容即拒绝，不回退 fake。
- 将价格填写为正 int64 十进制 micros 字符串（每百万 Token），币种与 Run / 仓库日 / 全局月限额明确。`model-config.example.json` 是无效价格占位模板，不是厂商报价。
- GitHub App 需要 Contents Read、Pull requests Write、Checks Write、Metadata；S1 Check 权限不足以发布 Review。重新安装/权限确认、实际 API 兼容、质量评估、账单、镜像、TLS 与生产恢复另行验收。

生产模板声明了 `model_api_key` secret，即使模式关闭，Compose 创建服务仍要求 `${SECRET_DIRECTORY}/model_api_key` 文件存在；关闭时可由部署者提供空文件，Worker 不会读取它。本次只验证配置，没有创建该文件或启动应用镜像。开发关闭状态使用 `/dev/null` 占位挂载。

正常发布使用 COMMENT，固定 Head；不 APPROVE / REQUEST_CHANGES。每个 PR 跨 Run/Head 累计最多三根因，行级和摘要共用额度，未知写入也占位。只有存在已接受 job_results 的 Publishing Run 才能建立发布计划；行级定位选证据范围内真正新增行，否则摘要。0 个问题只发中性覆盖 Check，不表示代码安全。

## 未知费用和阶段恢复

所有调用先原子预留 Run / 仓库 UTC 日 / 全局 UTC 月预算。全局月 80% 提示、100% 拒绝新调用。每阶段最多一次明确重试、每 Run 最多四次，SDK 隐式重试为零。

新配置收紧预算时，新预留立即受更低限额约束；已有预留不会被退款。已建立的 UTC 窗口不因新配置静默提高限额；需要更高额度应等待新窗口或另行设计、批准显式运维调整。

超时、发送授权响应丢失、用量缺失或无法确认的费用保留预留并设置 needsReconciliation，禁止自动重发。先使用部署者数据库只读检查 model_calls、budget_reservations、review_stage_artifacts；只看本仓库/Run 的明确 ID，不在诊断日志复制原始 Prompt/Response。

拿到 Provider 可追溯账单或未发送凭证后，使用专用部署者 CLI（不是公开 Web 操作）：

```bash
pnpm exec turbo run review:reconcile --filter=@reviewpilot/api -- --call-id <UUID> --expected-revision <整数> --outcome charged --actual-cost-micros <金额字符串> --currency <币种> --receipt-ref <非敏感凭证编号>
pnpm exec turbo run review:reconcile --filter=@reviewpilot/api -- --call-id <UUID> --expected-revision <整数> --outcome not-sent --receipt-ref <非敏感凭证编号>
```

默认只修正账务事实；没有阶段正文不能凭“已收费”虚构成功或自动再生成。需要明确恢复且仍有该阶段第二次调用额度时，部署者才可增加 `--retry-stage`。金额、币种、revision 或收据无效则拒绝；reconciliation 不等于退款。

阶段按 inputHash / contextHash / stage / Prompt / modelConfigVersion 恢复，Go 计算 outputHash。费用已结算但产物未知则阻断。部署者可设置 `REVIEW_RETAIN_CONTENT=false`，由 Go 固定到新 Run 的 retainContent policy；旧 Run 不会热修改。此模式仅在当前 Go 进程保留有界临时上下文（16 项，每项最多 8 MiB，5 分钟空闲或到期清除）；重启/淘汰后的源码不能恢复，明确拒绝，不重复调用。

发布响应丢失先查当前 App 的数字 Bot 身份、固定 Head、不可变计划标记、Review / Comment / Check IDs，持久化已观察事实；普通用户复制标记无效。首次发送已记录而远端事实未能确认时继续阻断，不重新 POST。Dispatcher 可以只读对账恢复有完整内容且仍授权的发布；正文到期或删库后的未知写入需人工核查，当前 CLI 不提供“强制释放评论额度”。

## 保留、清理与删除

带源码片段的内容最多 30 天，最小元数据通常 12 个月。调用清理是有界批次；大量积压需部署者重复执行，不保证一次清完。未知财务预留和防重复发布的最小 PR 占位不因 TTL 或删除自动释放。

```bash
pnpm exec turbo run review:cleanup --filter=@reviewpilot/api
pnpm exec turbo run review:cleanup --filter=@reviewpilot/api -- --delete-installation <ID> --delete-repository <ID>
```

第二条是破坏性部署者操作：必须先核对两个 ID 并取得删除授权，本次未对业务仓库执行。它禁用仓库、取消任务、删除内容，保留匿名全局费用/未知预留和最小根因占位；不会删除 GitHub 远端评论，也不能恢复已清理正文。Web 将正文到期与“未发现问题”区分。

## 可观测性与限制

OTel 链路包含 API → Outbox → Worker → 内部 REST，发布读取 Outbox 的 traceparent。指标：review.model.calls、review.model.unknown、review.cost.micros、review.stage.duration、review.failed / review.blocked；标签仅固定 purpose / mode / stage，不用 Run/PR 当指标标签。账务 counters 只记录已提交的新转换，重放不重复累加；metrics 不是账单权威，需以持久化事实对账。

只导出安全事件、身份和耗时，不导出完整模型输入输出、隐藏推理或源码；敏感字符串经脱敏。Collector 不可用时异步导出可能丢失，但不参与业务提交判定。模拟 TEST 费用与真实币种分开，不能据此推算真实单次成本。

受限 Diff+邻近上下文不是全仓库分析；保守资源上限可能降低覆盖。没有准确率承诺，未实现完整 Replay、上下文增强、自进化或人工纠错。legacy 切换会拒绝任何未终结 S2 Run，应先完成/对账 S2，不能将 AI 结果降格为连接 Check。
