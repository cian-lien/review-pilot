# S1 Go 技术栈迁移交付记录

日期：2026-10-06。分支 feat/s1-go-migration。只执行 S1，不进入 S2。

## 已实现

Go/Gin/GORM 与生成式 Go/TS 契约；保留旧 SQL/迁移哈希、Run/taskId 和 AES-GCM 会话；Go OAuth、固定 CSRF、当前安装/维护权、仓库 CAS；RabbitMQ/事务 Outbox、租约/epoch fencing、持久化结果与独立发布对账；原始字节 Webhook 验签、限额/幂等/乱序、neutral 连接 Check；中文直连 Go 仓库页；OTLP 日志/Trace/指标及完整监控/Compose；只读预检与显式可逆切换。

9 个旧 Next 业务文件已删除，代码可从 Git 历史恢复；旧 SQL/兼容 Fixture 保留，但不再是应用运行时。主集成验收改为 Go 六个固定场景。

## 实测证据

任务 1–6 定向场景/静态/构建已分别通过并提交。任务 7 日志负例先失败再 2/2 通过；真实 Go → Outbox → RabbitMQ → TS Worker → Go result → 假 GitHub Check 完成，三服务同 Trace、敏感 sentinel 未进入日志，停 Collector 后下一任务仍 Completed，公网 internal 404、内网未授权 401。

Collector 0.161.0、Prometheus 3.14.0、Caddy 2.11.7、Loki 3.7.8、Tempo 3.1.0 官方配置校验通过；开发/生产端口隔离与旧卷名检查通过，无业务卷创建/迁移。任务 8 专用 PostgreSQL 切换场景通过：旧有效租约阻断、taskId 保留、已完成不重通知、Failed/空 check_id 对账、旧 epoch 409、回退后 Go 领取 503、accepted result 保留。

最终汇总结果会在下方追加，不用定向绿灯代替最终验证。源码路由删除后发现旧 .next/dev 类型缓存，使用官方 typegen 生成新鲜生产路由校验，不把缓存中的旧业务入口当成当前源码。

## 待外部验收

真实 GitHub 登录/安装/PR/Check、实际业务数据库切换、完整应用镜像构建/运行、生产部署/TLS、异机备份与恢复演练尚未验收。手工登录态仓库页清单待真实 App，不算自动通过；告警未接外部通知，容器文件系统指标不等于宿主探测。

consumers-stopped 是操作者确认，不是数据库发现旧进程；未知远端写入会阻断，不盲目恢复旧备份。跨 GitHub/数据库不宣称 exactly-once。未调用模型或执行用户 PR 脚本；自进化/人工纠错/AI Review 均未实现。

## 最终验收

2026-10-06 执行 pnpm exec turbo run verify:migration --filter=@reviewpilot/tooling，exit 0，总时长 44.651 秒。契约漂移、受影响静态/lint/Go-Web 构建、关键单元、只读配置均通过；六个固定集成场景 6/6，通过时未连接实际业务库或真实 GitHub。手工登录态页面与生产事项仍保持待外部验收，没有把未执行项算成功。
