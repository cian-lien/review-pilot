# ADR 0002：Go 迁移运行时与契约

日期：2026-10-06。状态：实施中，尚未完成 S1 切换。

使用当前已安装 Go 1.27.1（.go-version 与 go.mod），Gin 固定 1.12.0，要求 Go 1.25+。持久化已接入 GORM 1.31.2、PostgreSQL driver 1.6.0，记录于 go.mod/go.sum。保留原 pnpm/Turbo/Next 等已锁定依赖，不随迁移升级。

消息使用 amqp091-go 1.15.0、amqplib 2.2.0（已自带 TypeScript 类型）；专用 Broker 测试固定官方 RabbitMQ 4.3.6-management-alpine。持久队列、mandatory 与确认共同决定 Outbox 投递成功，Worker 手动确认且 prefetch=1。Go 连接写入与关闭有界，断线重建 channel；Worker 重连前清理信号监听器。Worker 启动器使用环境白名单，不继承 DATABASE_URL、GITHUB_*、SESSION_SECRET 或另一个角色的服务 token。

OpenAPI 使用 3.0.3；生成器固定 oapi-codegen 2.8.0 与 openapi-typescript 7.13.0。消息采用独立 Draft 2020-12 JSON Schema 与 Ajv 8.20.0。消息 Schema 经受限投影写入 OpenAPI，生成文件仅由 codegen 产生，contracts:check 比较生成结果与源文件。

命令统一由 pnpm exec turbo run 调用。deps 只下载 Go 模块，不启用服务、不迁移数据库。build 按实际已有 cmd 入口产生 bin 文件。Go dev 默认公网 127.0.0.1:4000、内网 127.0.0.1:4001；同源代理在后续任务接入。缺 GitHub 配置不能伪造登录或 AI 审查。

下载出现 EOF 时重试官方代理，不关闭证书或模块校验，不自动切换镜像源。2026-10-06 一次诊断重试临时使用 Turbo loose 模式；没有代理变量，不能据此认定 strict 模式是根因。工程默认保持严格环境模式。
