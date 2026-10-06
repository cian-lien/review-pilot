# S1 本地运行与 GitHub 接入

当前已迁移为 Go/Gin/GORM API、RabbitMQ/Outbox、TypeScript Worker 和 Next Web；浏览器同源直连 REST，没有 BFF。旧 Next 认证、Webhook、仓库业务入口已删除。

步骤、默认只读预检、显式切换与回退、监控和用户凭证配置见 [S1 Go 迁移运行手册](s1-go-migration.md)。所有工程命令使用 pnpm exec turbo run。

本阶段只做 neutral 连接 Check，不调用模型、执行 PR 脚本或发布 AI 评论；真实 GitHub 安装尚未验收，不要设为正式合并门禁。自进化和人工纠错仍计划在 S4 实现。本轮停止，不继续 S2。
