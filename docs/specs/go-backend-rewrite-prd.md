# 业务后端迁到 Go（scoped PRD）

## 1. 状态

- 文档状态：Approved
- 决策：[`adr/0011-go-vertical-slice-rewrite.md`](../adr/0011-go-vertical-slice-rewrite.md)
- 阅读入口：`docs/ROADMAP.md`「工程运行时：业务后端迁 Go」
- 实现设计：`docs/specs/go-backend-rewrite-design.md`
- 当前运行事实：Go 业务 API / worker / dispatcher + PostgreSQL `async_dispatches` + Alembic。Python `backend/` 保留迁移与可选 Compose profile `python`。
- Gin / pgx / asynq 是当前实现；未使用 GORM。Web 与 Agent 合同仍以封印基线为准。

本文只定义这次工程的产品合同：用户能感知什么、运行单元换成什么、什么算完成。内部包结构、队列状态机和切片顺序见设计文档。

## 2. 背景

ProductFlow 是单管理员、单商家工作区。浏览器只打 Web 和业务 API；Agent service（Node.js 22 + Pi SDK）经 internal HTTP 调用业务后端，不拥有商品、graph、Run 或素材权威。

当前业务后端三个进程是 Python。宏观边界（七个运行单元、PostgreSQL 权威、Pi 文件非业务权威）成立。内部按横向分层组织，HTTP schema 与 ORM 摊在全局目录，按业务单独设计和 review 的半径过大。

这次迁移替换业务后端三个进程的实现语言和队列客户端，并在搬家时把内部代码改成按业务功能竖切。它不改变用户在 `docs/PRD.md` 里的能力，也不改 Agent service 的外部合同。

## 3. 目标

1. 浏览器继续使用现有页面、Cookie session、HTTP JSON 和 Agent SSE 投影；默认不改 Web。
2. Agent service 继续使用现有 internal HTTP / Tool / Context 合同；不把 Pi 或 `exp` Go harness 并进业务进程。
3. PostgreSQL 仍是商品、live graph、Run、素材、Agent 投影、`LibraryOrganizationDraft` 和任务状态的权威。Redis / asynq 只负责可恢复投递。
4. 媒体 bytes 仍在本地 storage；身份仍是 `MediaObject` + 业务侧资产 id。
5. 业务 API、worker、dispatcher 以 Go 二进制交付，Compose / `justfile` 在 cutover 后不再启动 uvicorn / dramatiq 作为默认运行时。
6. 内部代码按功能模块自持 HTTP DTO、application 用例和该聚合的持久化；共享核只保留真正跨功能的原语。一次改动落在一个功能包内。

## 4. 非目标

- 改用户功能、页面信息架构或画布交互。
- 把 Agent runtime 从 Pi 迁回 `exp` Go harness，或在业务后端内实现模型 loop。
- 引入 tenant、计费、对象存储云或其它 SaaS 合同。
- 用 Redis 缓存商品事实、graph 或素材列表。
- 用双写表或 GORM AutoMigrate 代替现有 schema 权威。
- 把 Python 测试一对一翻译成 Go。
- 把已删除的商品 `WorkflowDraft`、v1/v2 执行器或 Gallery 回填搬进 Go。
- 把工作台浏览器证明插入这场搬家的实施；证明仍约束 cutover。

## 5. 用户与操作者合同

对商家和浏览器：

- 登录、商品创建、工作台画布、素材库、连续生图、交付导出、设置页、Agent 对话与确认，行为与封印当日的 `docs/PRD.md` 一致。
- URL、状态码、错误 JSON（`detail`，以及结构化校验的 `error.code` / `message` / `details.issues`）、SSE 事件形状保持兼容。
- Cutover 允许要求重新登录。单管理员 self-host 接受这一次会话中断；默认不复刻 Python itsdangerous 时序签名。

对操作者：

- 已部署 PostgreSQL 与 storage 不得 reset。空库仍能从历史 Alembic 升到 v3 head，再接 Go 运行时。
- 进程名可变，职责不变：HTTP API、异步执行、dispatch 对账。
- 备份 / 恢复仍以 PostgreSQL + storage 为准；Redis 丢失不得丢掉业务终态，最多需要 dispatcher 重投。

## 6. 成功标准

整体 cutover 需要同时满足：

- 合同包中的 HTTP 路径在 Go 上状态码与错误码一致；Web 与 Agent service 默认零合同变更。
- graph run、image session、delivery rendition、local image edit 的 durable 投递仍是「先写 PostgreSQL，再投递 broker」；broker 失败留下可观察状态并返回队列不可用。
- live PostgreSQL + Redis：跑图、连续生图、交付、局部修、启动恢复、队列失败留痕。
- 真实 provider 至少覆盖 prompt 与 image 各一条成功 / 失败 / `unknown` 路径。
- 浏览器主链路：登录、创建商品、画布编辑与运行、素材库、Agent SSE 重连、设置页。桌面与 390px。工作台连续动作证明仍按 [`workbench.md`](workbench.md) 验收，不因 Go 切片宣称完成。
- residue：默认 Compose 不再启动 uvicorn / dramatiq；Python 仅可保留归档 CLI，直到单独切片迁完。
- 一次 backup / restore 演练后，已有商品仍能打开并跑图。

单个功能切片的成功标准见设计文档：该切片的路由或 actor 只由一个实现拥有，并用现有回归打到 Go 上。

## 7. 时机

封印基线见设计文档 §3。可以立即导出合同包并开始 `go/` host。工作台浏览器证明与实现并行；未证明不得 cutover，也不得把 Go 写进当前架构事实。

收益预期（约束实现优先级，不写进当前架构事实）：

- 用户可感知的出图速度和功能：无预期提升。瓶颈在 provider，不在 API 语言。
- 预期收益在部署面（静态二进制）、worker / SSE 的并发心智，以及按业务包缩小设计和 review 半径。
- 不把 QPS 或「少一门语言」当作本项目对外理由。Web 与 Agent 仍是 TypeScript。维护者选用 Go 写这三个进程是 ADR 0011 的输入。
