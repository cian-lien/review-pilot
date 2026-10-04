# S1 本地实现交付记录

日期：2026-10-04。工作目录：projects/review-pilot。分支：feat/s1-foundation。

## 范围

| 任务 | 本地实现 | 真实环境验收 |
| --- | --- | --- |
| 01 工程与领域契约 | pnpm/Next/TypeScript、状态机、Fixture、健康接口、两个 Worker、ADR | 目标 Node 24 未做额外版本矩阵 |
| 02 PostgreSQL 队列 | 迁移、事务入队、唯一键、租约、心跳、重试、检查点、失效令牌防护 | 真实 PostgreSQL 18.4 临时库已验证；Docker 未启动，Compose 未演练 |
| 03 登录与仓库 | OAuth/PKCE、服务端会话、安装校验、公共仓库同步、maintain/admin 管理权限、中文页面 | 没有 App 凭证，真实登录与安装待验收 |
| 04 Webhook 与 Check | 原始验签、事务去重、乱序保护、2 秒稳定窗口、撤销与私有化取消、neutral Check、远端对账 | 模拟 GitHub 已验证；真实事件与 Check 待验收 |

S1 是本地代码交付，不宣称已满足“真实 GitHub 事件到达后 Check 工作”的外部阶段验收。需要按 [接入说明](../runbooks/s1-local-setup.md) 配置专用安装。

## 验证记录

- 核心测试：10 个单元测试、9 个集成测试；含真实 PostgreSQL 停止/重启、并发租约、事务回滚、授权撤销、一次性 OAuth state、Webhook 重投与乱序、旧 SHA 不发布、Check 丢失响应对账。
- TypeScript、ESLint 与 Next 生产构建已通过。
- 本机生产启动检查：health 与中文仓库配置页均为 200，缺凭证登录为 503；两个 Worker 的 --check 正常。依赖 peer 检查无冲突。
- 未执行浏览器自动化或完整视觉验收；本阶段网页是接入基础页，不是最终仪表盘。
- 测试数据库、测试私钥均由测试自行创建在临时目录并清理；没有使用用户现有数据库或真实 GitHub 写权限。

## 执行调整

- 用户要求在实际项目目录开发，已迁入并直接修改；不继续使用临时副本开发。
- 用户明确不用 subagent，包括开发与收尾复核；代码由当前会话直接实现与检查。
- 用户要求不过量测试：保留阶段关键边界，不扩展测试矩阵、浏览器自动化、评测集或压力测试。
- 相对 TypeScript 导入使用无扩展名以兼容 Turbopack；数据库迁移单独导出，Web 不打包迁移操作。
- 无凭证时展示配置缺项，不以演示仓库或模拟精度冒充真实结果；模拟 GitHub 仅存在于测试。
- S1 Check 结论是 neutral，不伪装成完成 AI 审查。Evolution Worker 明确处于未启用状态。

## 停止位置

本轮在 S1 本地交付处停下。S2 AI Review、S3 上下文、S4 自进化与人工纠错、S5 评测治理、S6 Beta 均未启动。下一轮需用户明确继续；完成真实 S1 接入验收后再进入 S2。
