# S1 Go 迁移、运行与回退

## 边界

S1 只验证 GitHub 接入与 neutral 连接 Check，不分析源码、不调用模型、不发布问题评论。Completed 不表示代码正确/安全。Go API/dispatcher 独占数据库、认证、GitHub 和任务事实；Next 只展示，浏览器经 Caddy 同源直连 REST；Worker 仅有 Broker、内部 REST 与自身角色 token。

## 开发运行

需要 Docker、Node 24+、pnpm 12.8.1；宿主 Go 开发需要 Go 1.27.1。pnpm 安装依赖，所有工程任务通过 Turbo：

```bash
pnpm install --frozen-lockfile
cp .env.example .env
pnpm exec turbo run infra:up --filter=@reviewpilot/tooling
pnpm exec turbo run infra:migrate --filter=@reviewpilot/tooling
pnpm exec turbo run infra:cutover --filter=@reviewpilot/tooling
```

打开 http://localhost:8080/repositories。3000/4000/4001、数据库和 Broker 不公开宿主端口。最后一条默认只读；新库迁移后 mode=legacy，不能领取 Go 任务，这是保护。需要启用 Go 时执行下方显式切换流程。

开发 reviewpilot_pg 映射旧默认卷名 review-pilot_reviewpilot_pg。若曾指定不同 project name，先核对真实旧卷再调整，不要用新空卷冒充迁移。infra:stop 不删除卷。宿主 dev 需自行提供本项目专用数据库/Broker与同源 Caddy，启动 `pnpm exec turbo run dev --filter=@reviewpilot/api --filter=@reviewpilot/web`，并分别启动 dispatcher/worker:review；不要把 3000 当成完整入口。

## GitHub 配置（用户操作）

公共测试仓库；权限 Metadata read、Pull requests read、Checks write。S1 不需要 Contents 写入或 PR 评论写入。订阅 Pull request/Repository，安装生命周期事件由 App 接收。

在 .env 填 App ID/slug、Client ID/secret、Webhook secret、随机 32 字节标准 base64 的 SESSION_SECRET，以及两个独立强随机 Worker token。私钥放仓库外：GITHUB_PRIVATE_KEY_PATH 为宿主绝对路径；开发容器 GITHUB_PRIVATE_KEY_CONTAINER_PATH=/run/secrets/github_private_key。不要把凭证发到聊天或提交 Git。

APP_URL 为同源根域名，OAuth 回调 /auth/github/callback，Setup /installations/callback，Webhook /api/github/webhook。真实接入使用用户控制的 HTTPS 域名/隧道；更改域名须同步开发 Compose/Caddy 的 APP_URL/APP_HOST 和 GitHub App 设置，不只是改宿主 .env。服务不会自动改外部设置。不要记录 OAuth 查询参数或令牌。

实际验收：登录 → 安装公共仓库 → 同步 → maintain/admin 者启用 → 新 PR → Run 完成 → “ReviewPilot · 连接检查” neutral 且明确未 AI 审查。未提供 App 时保持待验收。

## 切换：先实际停止旧进程

旧 TS 版本不理解 runtime_control；mode 不能证明旧进程停了。先停止所有部署副本的旧消费者，暂停新消费者和 dispatcher，必要时暂停变更入口。保存备份和切换后新增事实清单。

```bash
pnpm exec turbo run infra:stop --filter=@reviewpilot/tooling -- --consumers
pnpm exec turbo run infra:cutover --filter=@reviewpilot/tooling -- --consumers-stopped
```

等待执行/发布最长 90 秒及 Outbox 30 秒租约失效，并核实在途远端写入结束。报告字段：mode、epoch、activeLeaseCount、unsubmittedCount、publicationPendingCount、unreconciledCheckCount、canSwitch。consumers-stopped 是操作者确认，不是进程发现。

空 check_id、非终态 Run 与 Failed 发布记录均核对 external_id；缺凭证、远端错误、进行中 Check 或已知 Check 消失都会阻断。核对 canSwitch=true 和最新 epoch 后才切换。0 仅适用于首次新库：

```bash
pnpm exec turbo run infra:cutover --filter=@reviewpilot/tooling -- --target go --expected-epoch 0 --consumers-stopped
pnpm exec turbo run infra:up --filter=@reviewpilot/tooling
```

Switch 在事务中重新核对租约、远端事实与 CAS epoch，保留 taskId、完成任务不重通知、生成唯一 Outbox、作废旧 token/epoch。短锁等待或未知事实导致回滚，不做 GitHub Check POST/PATCH。宿主数据库可用 `pnpm exec turbo run cutover:check --filter=@reviewpilot/api`；默认只读，不执行 SQL 迁移。

## 回退

1. 实际停止 Go 消费者和 dispatcher，旧消费者仍停止；排空/隔离在途写入。
2. 对账远端 Check、publication、Outbox，保存 accepted result、Check ID 和新增 Run。
3. 未知或进行中的 Check 先人工处理，不能跳过。已确认 neutral/cancelled 写回本地终态；确认远端不存在的 result_ready 才转成旧版本支持的 queued，accepted result 保留。
4. 最新 epoch=N、预检安全后显式指定 `--target legacy --expected-epoch N --consumers-stopped`；Go 领取/恢复/发布被 mode 拒绝。
5. 从 Git 历史恢复独立旧部署，检查新增事实兼容后才启旧消费者。绝不同时跑新旧消费者，绝不盲目恢复旧备份抹掉新事实。

本次只在专用临时库验证往返；没有切换用户业务库。9 个旧 Next 业务文件已移除，任务 7 或更早 Git 版本可恢复；旧 SQL 和兼容 Fixture 保留。

## 监控与单机 Beta

开发监控可选：先填独立 GRAFANA_ADMIN_PASSWORD，再运行 `pnpm exec turbo run infra:up --filter=@reviewpilot/tooling -- --monitoring`。Grafana localhost:3001，账号 reviewpilot，禁用匿名。仅私网 OTLP；stdout/OTLP 同样使用字段白名单，无源码/token/原始错误体，不传播 Baggage。日志/Trace 7 天、指标 30 天并有容量限制；runId/taskId 不做指标标签。

仪表盘只展示实际积压、等待、失败、心跳、耗时与基础服务。告警规则尚未接入外部通知。Collector 文件系统只代表容器可见范围，不替代宿主磁盘探测；进程心跳不证明 Broker 可用或任务租约有效。Collector 断开不阻止业务，但遥测可能丢失。

infra/compose.prod.yml 使用独立 reviewpilot_beta_* 卷、文件 secrets、HTTPS Caddy，仅 80/443 公网，Grafana loopback。SECRET_DIRECTORY 为仓库外绝对路径，文件名与 secrets 名一致；database_url/rabbitmq_url 指向 postgres/rabbitmq，和初始密码一致。Node UID=1000，宿主 secret 文件须实际允许该 UID/受控组读取；bind secret mode 不会自动修正宿主所有权。已有 DB/Broker 密码不能靠修改初始化变量轮换。

生产未部署；TLS、外部探测、异机备份/恢复演练、容量及真实 App 接入待用户环境。命名卷不是备份，配置校验不等于上线。Dockerfile 应用镜像完整构建/运行尚待部署环境验收；infra:up 只管理开发，拒绝 --prod，不自动开生产。

## 最小验证与手工清单

```bash
pnpm exec turbo run verify:migration --filter=@reviewpilot/tooling
```

一次汇总契约、受影响静态/构建、关键单元、只读配置与六个固定隔离场景，无模型调用或压力矩阵。以下登录态清单未配置 App 前不算通过：

- [ ] 8080 页面可用，无凭证注入；缺配置/未登录/空列表/服务不可用提示明确。
- [ ] 仅当前可访问公共仓库及最近 20 个 Run。
- [ ] 启用携带 CSRF/configVersion；409 刷新且不伪装成功。
- [ ] 后台暂停轮询、退出清缓存；neutral/未 AI 审查说明明确。
- [ ] 真实 App 登录、安装、PR 与 Check 完整一次验收。

停在 S1；真实接入、数据库切换和入口验收完成后再单独确认 S2。
