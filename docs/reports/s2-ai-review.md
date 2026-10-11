# S2 最小 AI 审查交付记录

日期：2026-10-11。结论：八项任务已在本地实施，定向断言及最终 13 项有界汇总通过；仅代表隔离模拟验收，不代表真实接入或生产可用。

执行目录：`/Users/cianlien/development/projects/review-pilot`。分支：`feat/s1-go-migration`。执行基线：`4c9cd7c2aea7001036fb169e65f0c85b1386c431`。保留用户原有未跟踪 `AGENTS.md`，未修改或提交。未创建 subagent/worktree，未 merge、push、部署或进入 S3。

## 已交付能力

- Go 独占数据库、GitHub、租约、额度、结果与发布；TypeScript Worker 通过角色受限的内部 REST 调用；Web 同源直连 Go，无 BFF。
- 独立的消息 v2 / ai-review / review-v1，保留 S1 connection-check 的消息、结果与中性 Check。默认关闭 S2，真实调用及发布分别受开关控制。
- 固定 Base/Head 与分页封存上下文，检查 Tree 模式、路径、字节大小及 Diff 与文本一致性；不安装依赖或执行 PR 代码。缺失或超限降低覆盖，不伪装为完整审查。
- 受控 LangGraph 串行流程：context → candidates → verification → finalize；LangChain OpenAI-compatible 适配器、隔离 fake Provider、结构化 Schema 和脱敏。正常两次调用，零候选跳过验证；SDK 隐式重试为零。
- int64 / 十进制字符串 / BigInt 费用，Run、仓库 UTC 日及全局 UTC 月原子预留；80% 月度提示、100% 阻断。未知用量/发送/结算事实保留预留，持久化阻断，不自动重发。
- 租约/epoch fencing、不可变阶段及结果、服务端哈希与证据再核验。保存最终结果后才 ack；提交响应丢失使用只读确认，重复消息不重复调用模型。
- PR 跨 Run/Head 累计三根因、局部代码锚点去重、固定 Head COMMENT 发布与中性覆盖 Check。App Bot 身份及不可变发布计划对账；未知远端写入保留占位，不盲目再次 POST。
- 中文 PR 列表、Run 历史、Finding/过滤结果、失败条件、证据、验证建议、覆盖、费用、版本与发布事实。沿用现有 shadcn 组件与 TanStack Query；终结、阻断、授权失效时停止自动刷新，不增加未实现按钮。
- 30 天内容保留、12 个月元数据、即时到期过滤、有界清理及部署者 scoped deletion/reconciliation CLI。删除不释放未知财务或外部写入事实。
- 安全 OTel、模型调用/未知/费用 counters、阶段耗时及失败/阻断指标；固定维度，模拟 TEST 与真实模式分开。更新部署模板和 Review Worker 最小打包路径，模型凭证只属于 Review Worker。

## 提交顺序

| 任务 | 本地提交 | 内容 |
| --- | --- | --- |
| 1 | 17fdc1f | v2 契约与默认关闭配置 |
| 2 | cb959f9 | 固定 SHA 上下文与 Diff |
| 3 | 2cf8dd2 | 预算、阶段恢复与保留 |
| 4 | 10b5462 | 模型网关与凭证隔离 |
| 5 | ccfa51f | 串行引擎与可信最终结果 |
| 6 | 22d3f6f | 消息路由及发布对账 |
| 7 | 28e35a3 | 中文 PR 结果与费用页 |
| 8 | 本记录所属提交 | 隔离全链路、指标、最终修正、手册与报告 |

使用 `git log --oneline 4c9cd7c..HEAD` 查看实际本地提交；没有远端交付或 production SHA 声明。

## 最终验收证据

实际工程入口：

```bash
pnpm exec turbo run verify:review --filter=@reviewpilot/tooling
```

自查修正后的最终执行退出 `0`，13/13 步完成，Turbo 总耗时 `45.939s`。这是开发机本次耗时，不是产品审查延迟/SLA。最终标记：

```text
S2_LOCAL_SIMULATED_VERIFICATION_PASSED
```

| 项目 | 本次结果 |
| --- | --- |
| 契约生成漂移 | 通过 |
| 受影响包 typecheck/lint/build | 通过；Web 构建通过，不等于浏览器验收 |
| TS contracts/repository/model-gateway/review-engine | 12 / 8 / 13 / 9 条通过 |
| TS worker-runtime/observability/Web | 8 / 2 / 6 条通过 |
| Go config/retention/contracts/reviews/publisher 定向单元 | 通过 |
| S2 context/budget/stages/results/publication | 4 / 6 / 1 / 3 / 4 项 Go 断言通过，各场景新 verdict 通过 |
| S2 pipeline | 1 项隔离全链路通过，Go TestReviewPipeline 为 9.49s |
| S1 message-recovery / cutover | 两项必要回归通过 |
| Compose / Collector / Prometheus / Caddy / Loki | 只读配置校验通过；10 条 Prometheus 规则有效 |

实际环境是本次创建的临时 PostgreSQL 18.4、随机端口的 RabbitMQ 4.3.6 和 Collector 0.161.0；GitHub 与 Provider 为 loopback 模拟服务。子进程只继承白名单进程元数据，不读取 `.env` 或业务密钥。只清理本次已验证身份的资源；完成后查询 `reviewpilot.test-owned=true` 容器列表为空。

全链路不是绕过模块的手写单元模拟：实际 HMAC Webhook 创建 v2 Run，Go Outbox/Relay 投递 RabbitMQ，实际 TS Worker 经内部 REST 读取上下文、运行两阶段、提交结果，Go Publisher 调用假 GitHub，公开查询经真实会话鉴权返回 DTO。

本次 pipeline 的持久化事实 verdict：

```json
{
  "acceptedResultCount": 1,
  "modelCallCount": 2,
  "publishedRootCauseCount": 1,
  "unknownReservationHeld": true,
  "stalePublishBlocked": true,
  "traceLinked": true,
  "secretSentinelAbsent": true,
  "simulated": true,
  "passed": true,
  "scenario": "pipeline"
}
```

`modelCallCount=2` 仅计正常 Run，不把未知用量 Run 混在正常调用断言里。重复 v2 通知实际送入 Broker，并等待 accepted Claim/Confirm；结果已持久化而响应损坏后，经 GET 确认，不新增调用。第二 Run 的未知调用对应 held 预留仍大于零；第三 Run 接受结果后 Head 更新，远端发布数不增加。

实际 Collector 文件里可关联同一正常 Run 的 API、dispatcher 与 Worker Trace；新增三个 Go 账务指标有实际导出。凭证与源码敏感标记未出现于捕获的遥测、Worker 输出、持久化上下文或公开 DTO。这是指定 sentinel 检查，不是“任何秘密都绝不会泄露”的证明。遥测不参与业务提交判定，指标不是账单权威。

任务 6 已单独运行一次 S1 webhook-check 并通过，最终不为仪式重复运行。没有执行全套 S1 六场景、压力、多模型、浏览器或真实远端请求。最终汇总前已经执行过一次汇总，自查根因锚点修正后仅重跑受影响断言与一次最终汇总。

## 实际失败、修正与自查

先观察必要失败，再实现/修正；以下保留的是本次真实遇到的边界，不是额外的测试承诺：

- v2 原先不被契约接受；生成器的重复枚举名称及 nullable 引用投影经定向断言修正，未放宽 S1。
- 上下文缺少封存、UTF8 流式读取无界、symlink 可被读取等断言先失败；增加字节计数、Tree 检查与固定版本核验。临时 harness 缺 HOME 的环境错误单独处理，没有把它当业务 RED。
- 预算并发最初允许两个零值预留；费用重放受 JSONB 格式影响；已结算但阶段缺失曾允许恢复调用。分别通过原子预留、类型化比较及缺失产物阻断修正。
- Worker 曾继承其他角色凭证；现使用白名单启动且拒绝混合配置。新增 modelConfigHash 在首次预留前识别 Go/Worker 同版本但内容不同。
- 新 LangGraph 节点与 channel 同名冲突、阶段内容过期、元数据模式缺源码再核验等问题在实际执行中暴露，按职责修正，未把“账单成功”解释为产物成功。
- 发布出现 SQL UUID/text 比较及测试槽位交叉污染，修正后并发、外来 marker、响应丢失与权限/Head 断言通过。
- Task 7 的追加 Finding 到期断言在提交时尚未通过：占位 category 不在契约内。Task 8 改为 `other` 并重新获得 results 3/3 证据；本报告不掩盖先前失败。
- 全链路初版遇到 session secret 编码、Outbox 尚未到 available_at、不存在的 timestamp 查询，以及过早结束 Worker 丢失 OTel 批导出。修正 fixture/等待条件后由实际运行得到 verdict；没有通过删除断言获得成功。
- 进一步自查修正发布“证据范围最后一行未必是新增行”：现在选择可信 patch 内新增 RIGHT 行，无合法行锚点走摘要。删除清空不可变计划时保留 erased 标记，正文过期即时禁止重建/发布。
- 收紧限额曾沿用已建立 UTC 窗口的旧上限。新增精确断言复现多预留，改为窗口上限只降不升；测试初稿的错误 scalar scan 可能掩盖结果，先改为类型化快照并要求明确 `REVIEW_BUDGET_EXHAUSTED`，再接受业务 RED→GREEN。
- TS 去重曾仅看相似 trigger/consequence，错误合并同文件不同代码缺陷；新增三候选断言复现第二个被过滤，加入引用范围的局部代码锚点后保留两个不同根因、过滤真正重复项，engine 9/9 通过。
- 新增 `REVIEW_RETAIN_CONTENT` 的严格解析、修正最大合法 PR 编号的分页边界，并让静态 Go 包装器在隔离模式也不加载 `.env`。

自行审查覆盖本轮执行基线后的授权、哈希、价格、恢复、未知事实、累计评论额度、删除与 fail-closed 配置。重要修正进入本地提交；没有由独立 fresh-context reviewer 审查，不能把作者自查当成独立审查或穷尽性安全审计。

## 实施裁定及代价

这些是按已确认设计执行时的明确取舍，不是未告知的范围扩张：

1. 当前会话实施，不用 worktree/subagent，包括最终审查。遵守用户要求；代价是缺少独立第二双眼睛。
2. 使用定向 TDD 和有界汇总代替全仓库验证。遵守少量测试要求；无关路径、浏览器及外部环境回归覆盖较低。
3. 契约生成器旁增加声明式 review-components 投影，保留单份 OpenAPI。集中边界定义；多出的生成产物需要漂移检查同步。
4. v2 预留请求增加非敏感 modelConfigHash。同名版本不足以发现内容漂移；代价是 v2 调用方必须传新增字段，S1 不变。
5. context 阶段保存有界末页 receipt，完整源码从 Go 的 sealed manifest 分页恢复。2 MiB 上下文不能装入 256 KiB 阶段正文；代价是恢复仍需读取缓存页面并复核固定 Head。
6. retainContent=false 使用有界进程内临时源码完成正在执行的证据核验。否则 Go 无法核验；重启/淘汰后必须拒绝恢复，不能承诺持久源码恢复。
7. 有未终结 S2 Run 时阻止 legacy 切换。legacy 无法表达已接受 AI/未知费用与发布事实；代价是切换前必须处理 S2，不能强制降格为连接任务。
8. 已建立 UTC 窗口上限仅收紧，不因新模型配置静默提升。防止超预算并保留已有预留；提高现窗口额度需未来独立设计/授权，而非现有热配置。
9. 自查后的最终有界汇总已实际退出 0；任务登记只读取其成功证据，不为登记再跑第三次汇总。避免重复测试；不增加额外回归覆盖。
10. 阶段结束保留当前分支，不执行 merge/push 或额外集成选择。用户已要求停在 S2；交付目前仅本地。

延后小项：页面只经构建与视图模型检查，完整交互、视觉细节与浏览器可访问性验收未做。当前 Web 复用已有组件，没有额外设计系统或视觉重构。

## 待外部验收与明确未实现

- [ ] 真实 GitHub App 权限、安装、Bot/API 兼容、真实 Review/Check 发布对账。
- [ ] 真实 OpenAI-compatible Provider 的结构化输出、用量、非 Token 收费排除、429 未收费语义、实际价格与账单核对；当前真实 tokenizer 支持范围有限。
- [ ] 真实 PR 的质量评估与误报率。模拟数据没有准确率或实际单次成本证明。
- [ ] 业务数据库执行新增 0004–0007、S1/S2 真实切换验收；原 0001–0003 未改，新增迁移只在测试临时库执行。
- [ ] 完整应用 Docker 镜像、secret 文件、生产网络、TLS、故障恢复与监控告警投递。只读配置验证不能替代部署验收。
- [ ] 浏览器实际验证 PR 页和错误/授权分支。当前临时场景没有持久可浏览的数据。

自进化、人工纠错、治理型规则记忆、增强检索和完整 Replay 均未实施，不能写成已完成产品能力或简历成果。未知发布到期后人工核查的操作限制、关闭内容保存的恢复降级、模型配置与密钥的未来预检见 [S2 运行手册](../runbooks/s2-local-review.md)。S2 到此停止。
