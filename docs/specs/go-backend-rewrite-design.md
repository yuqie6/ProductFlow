# 业务后端迁到 Go（实现设计）

## 1. 状态

- 文档状态：Approved
- 产品合同：`docs/specs/go-backend-rewrite-prd.md`
- 决策：`docs/adr/0011-go-vertical-slice-rewrite.md`
- 阅读入口：`docs/ROADMAP.md`「工程运行时：业务后端已切 Go」
- 当前运行事实：Go API / worker / dispatcher（Gin + GORM + asynq，驱动仍是 pgx）+ `productflow-migrate`。Python `backend/` 保留封印树与可选 Compose profile `python`。
- schema 权威是 `productflow-migrate`：GORM `CreateTable`/`AddColumn` 加 ExtraDDL。不使用 AutoMigrate。命令事务走 `tx.WithGorm`；`FOR UPDATE SKIP LOCKED` 与 advisory lock 仍走 raw SQL。
- 不复用：`exp` 上的 Go Agent service / `agent-harness`。那条线是 Agent runtime 实验。

宏观进程图保持现有七个运行单元，只替换其中三个业务进程。内部从横向分层改成按功能竖切的模块化单体。

## 2. 目标拓扑

```text
Browser
  React + Vite + TS
  HTTP / Cookie Session                 HTTP / SSE（经后端投影）
        │                                        │
        ▼                                        ▼
┌───────────────────────────────┐   ┌─────────────────────────────────┐
│  Go Business Backend          │   │  Agent Service（不变）            │
│  Gin + GORM + Viper + zap     │◄──┤  Node.js 22 + Pi SDK             │
│  + go-redis + asynq           │   │  仅交互式 Turn runtime            │
│  REST（Web）                  │──►│  不拥有业务权威                   │
│  Internal API（Agent）        │   └─────────────────────────────────┘
└───────────────┬───────────────┘
                │
        ┌───────┴────────┐
        ▼                ▼
┌─────────────────┐  ┌──────────────────────────────────────┐
│  Go Worker      │  │  PostgreSQL  业务权威                  │
│  asynq consumer │  │  Redis / asynq  可恢复投递，非终态     │
│  + dispatcher   │  │  Local Storage  媒体 bytes             │
└─────────────────┘  └──────────────────────────────────────┘
```

权威边界与现在相同：PostgreSQL 保存状态；Pi 本地文件保存 transcript；Redis / asynq 不是 Run / 整理 Draft / 素材的状态源。

仓库位置（双轨期间）：

```text
go/
  go.mod                    github.com/yuqie6/productflow
  cmd/productflow-api/
  cmd/productflow-worker/
  cmd/productflow-dispatcher/
  internal/platform/        真共享，保持瘦
  internal/<capability>/    功能模块，自持 HTTP / app / store
```

`backend/` 在 cutover 前保留。Compose 增加 Go 进程，不在中途删除 uvicorn / dramatiq。

## 3. 封印基线（2026-08-29 live Python）

Go 与剩余 Python 共用同一把尺子。用户可观察行为以当时 `docs/PRD.md`、`docs/ARCHITECTURE.md` 和测试为准；目录布局不是封印对象。机器可读合同在仓库根 `contracts/`：`openapi.json` 与 `http-routes.json` 是 2026-08-29 Python 封印快照，不是默认路径上从 live FastAPI 重新生成。

### 3.1 必须带走的不变量

1. OpenAPI：路径、方法、状态码、`{"detail":"..."}`；结构化校验另带 `error.code` / `error.message` / `error.details.issues`。
2. Cookie 名 `session`。Cutover 默认发 Go 自己的签名 cookie，要求重新登录。
3. Agent internal HTTP 与 Turn SSE：`id: <sequence>`、`event: <kind>`、`data: <json>`，心跳 `: heartbeat`。浏览器断开不取消 Turn。回答问题的**可观察**结果是一条 continuation Turn（现实现：`control.answer_agent_question` 预留下一条 Turn 并尝试取消旧 harness Turn），不是把 Python 嵌套 commit 原样搬过去。
4. Durable 投递：HTTP 在同一事务写业务行和 `async_dispatches` PENDING。dispatcher 把行标 SENT 再 `asynq.Enqueue`。HTTP 不直接 enqueue，入队失败不走 HTTP 503。worker `MaxRetry=0`。线上只有这一条投递 seam；专用 Dramatiq actor 不作为 HTTP 默认路径进入 Go。
5. 媒体路径与 `MediaObject` 身份；节点绑定继续使用 `ProductImageAsset` id。
6. 配置两层：Viper 只读环境变量 / 本地文件；`app_settings` / `provider_profiles` / `provider_bindings` 仍由设置页写库。
7. 正式工作流只有 schema-v3 live graph。用户、Agent、配方都走 Graph Command。不完整 DAG 可保存。编译只读 incoming edges。执行读 run snapshot。
8. Agent 立即改图只接受一条 operation；多节点走 `WorkflowGraphProposal`，画布确认。
9. 商品 Goal 是显式 `AgentTask`。Turn 或 `WorkflowGraphRun` 终态不得把商品 Goal 标成完成。全局 Task 仍可在 Turn 成功或 `LibraryOrganizationDraft` 确认后完成。
10. 创建不写商品 `WorkflowDraft`，不建 onboarding Task，不自动开场 Turn。`workflow_drafts` 表已删除。

Web 与 `agent-service/` 默认零合同变更。某个 Go 实现无法保持兼容时，先改 Go。

### 3.2 出生路径（可观察行为封印，内部用命名变体）

图模板可以相同，落库集合不同。Go `product` 包用命名命令表达，禁止合成一条「万能创建」。

| 命令 | HTTP | Product | intake | 封面 | Session / Conversation | 图 |
|---|---|---|---|---|---|---|
| Agent 名称-only | `POST /api/v2/agent-product-workspaces/drafts` | 仅名称 | 空 | 无 | 有，`product_id` 非空 | 单 `product_source` |
| Agent 表单齐 | `POST /api/v2/agent-product-workspaces` | 名称 + 1..6 图 | 写入 | 不设 | 有 | `build_direct_create_template` |
| 直接创建 | `POST /api/v3/products` | 名称 + 图 + 可选价格类字段 | 不写 | 第一张图 | 无 | 同一模板 |
| 无图商品 | `POST /api/v2/products` | 名称 + 图 + 封面 | 无 | 有 | 无 | 无；可后补空图 |

### 3.3 不搬进 Go 的残留

| 残留 | 处理 |
|---|---|
| 商品 `WorkflowDraft`、物化、onboarding Task | 删除，不立 `internal/draft` |
| `source_draft_revision_id`（恒为 None） | 不出现在 Go DTO |
| `ImagePromptPayloadV1` / `image_plan_key` 可写形状 | Catalog 继续拒绝；不复活写入 |
| `gallery_*` 函数名 | 商品图库用 image-library 命名 |
| Python `AgentToolStepKind` 缺少 Pi 已发出的 `apply_graph` / `propose_graph` | Go 白名单与 Pi `contracts.ts` 对齐 |
| FastAPI 仍有、Pi 工具表未注册的 gallery organize tools | 不偷偷加回 Pi 工具表；除非另开产品切片 |
| 专用 Dramatiq actor 作为 HTTP 默认 enqueue | 只保留 AsyncDispatch 等价物；测试可走同一 worker 入口 |

## 4. 默认技术选型

| 层 | 默认 | 约束 |
|---|---|---|
| HTTP | Gin | session、SSE、上传校验、`x-request-id` 自己实现。binding 不替代 application 校验。 |
| SQL | GORM（postgres/pgx 驱动） | `productflow-migrate` 用 `CreateTable`/`AddColumn` 建/补表和列。CHECK、PG enum、部分唯一索引、FK 走 ExtraDDL。不使用 AutoMigrate。生产与测试都是 PostgreSQL，不支持 SQLite。命令事务用 `tx.WithGorm`；`FOR UPDATE SKIP LOCKED` 与 advisory lock 仍走 raw SQL。 |
| 配置 | Viper | 只加载启动配置。运行时设置读 PostgreSQL。 |
| 日志 | zap | JSON 同时写 stderr 与 `STORAGE_ROOT/logs/productflow-{api,worker,dispatcher}.log`（滚动）。字段对齐 request / run / node run / image-session task id。禁止 secret、cookie、完整 prompt、provider body、bytes。 |
| 队列 | asynq + go-redis | 替代 Dramatiq，不替代 `async_dispatches`。task 名与现有 actor 名对齐。 |
| 图片 | 与 Pillow 对拍的 Go 编解码 | preview / thumbnail / DeliverySpec 用 golden fixture。WebP 是明确风险。 |

不引入第二套消息总线，不拆微服务，不在 Go 里实现 Agent loop。

## 5. 内部结构：按功能竖切

对外仍是一个单体。对内每个业务模块自持该功能的 HTTP DTO、application 用例和只属于它的表。禁止再铺一套全局 `handlers/` + `dto/` + `models.go`。

```text
internal/
  platform/
    httpx/      cookie, request id, error JSON, SSE helper
    tx/         Begin/Commit 规矩；public command 拥有事务
    log/
    config/
    storage/    bytes，不懂商品
    queue/      asynq 信封 + async_dispatches
    clockid/
  auth/
  settings/
  product/      商品、intake、facts、商品图身份
  media/        MediaObject 原语
  library/      全局图库与 LibraryOrganizationDraft
  graph/        live graph、ChangeSet、Run、提案
  recipe/       从 live graph 提取 / 预览 / 应用
  imagesession/
  delivery/
  localedit/
  providers/    prompt / image HTTP 适配；按 DB binding 解析
  agent/        Session/Task/Turn 投影、internal tools、SSE；不直接写 graph 表
```

每个功能包：

- `http.go`：路由与 wire DTO，只在 HTTP 边缘。
- `app.go`：用例。需要事务时接收 `tx`，自己不偷偷 commit。
- `store.go`：只访问本包的表。
- 稳定内部合同（GenerationSpec、DeliverySpec、ChangeSet 操作）放本包 `contract.go`，只此一份。

出包只暴露函数，例如 `graph.ApplyChangeSet`、`graph.SubmitRun`、`library.ConfirmOrganizationDraft`。禁止别的包 import 本包持久化行类型。

跨聚合在 **一个** 用例里编排，共用一次事务。创建示例：

```text
product.CreateAgentDraftWorkspace
  ├─ product.StageCanonical(tx, ...)
  ├─ media.StageUpload(tx, ...)          // 表单齐时
  ├─ graph.StageBirthGraph(tx, ...)      // product_source 或直接创建模板
  └─ agent.OpenCanvasSession(tx, ...)    // 不建 onboarding Task
  commit 一次
```

Agent 包只做投影和 tool 入口。改 live graph、请求 Run、改全局库走 `graph` / `library` 的 app 函数。

### 5.1 共享核（不准复制到各业务包）

| 共享核 | 原因 |
|---|---|
| `MediaObject` + storage 补偿 | 商品图、图库、会话、交付、workflow 结果共用身份 |
| domain error → HTTP | Web 依赖统一 `detail` / 409 / `unknown` |
| `async_dispatches` + asynq 信封 | 所有 actor 同一套先落库再投递 |
| Auth / session | 所有路由一道门 |
| Provider profile / binding 读取 | prompt / image / agent 三用途一份配置 |
| 生成容量 advisory lock（当前 key `42630001`） | 跨 graph run 与 image-session |
| graph catalog / 拓扑 / 端口规则 | command、compiler、run 必须同一份 |

`platform` 只准变厚到「中间件 + bytes + 队列信封 + 错误」。业务校验进功能包。

### 5.2 模块与表

开工时按当时 Alembic head 再核对一次。下表以 2026-08-29 schema 为准。

| 模块 | 主要表 | 主要路由 / actor |
|---|---|---|
| settings | `app_settings`, `provider_profiles`, `provider_bindings` | `/api/settings` |
| product | `products`, `product_asset_folders`, `product_image_assets`, fact / visual system 版本表 | `/api/v2/products`，Agent 工作区创建 |
| media | `media_objects` | 上传校验与下载原语 |
| library | `media_library_*`，`library_organization_drafts*` | `/api/media-library`，整理 Draft 确认 |
| graph | `workflow_graphs` 及 node/edge/group/run/artifact/proposal，`workflow_media_library_assets` | `/api/v3/.../workflows`，`run_workflow_graph_run` |
| recipe | `workflow_recipes`, `workflow_recipe_versions`, `workflow_recipe_applications` | `/api/v3/workflow-recipes` |
| imagesession | `image_sessions*` | `/api/image-sessions` |
| delivery | `delivery_rendition_jobs` | 交付导出 actor |
| localedit | `local_image_edit_*` | `/api/v3/local-image-edits` |
| agent | `agent_*` | `/api/v2/agent-*`，internal tools，SSE |
| platform/queue | `async_dispatches` | dispatcher |

没有 `workflow_drafts` 模块。V1 archive 表与 cutover CLI 第一波仍可留在 Python。

## 6. 持久化

当前写入依赖 `FOR UPDATE`、确定性锁顺序、幂等 key 和 JSON 列形状。schema 由 `productflow-migrate` 持有。命令事务用 GORM session 上的 raw SQL。

1. `productflow-migrate` 用 `CreateTable`/`AddColumn` 补表和列，不 drop 退休表。约束/enum/部分唯一索引/FK 走 ExtraDDL。不使用 AutoMigrate。
2. 每个 public command 显式 Begin/Commit/Rollback。内部 helper 只组装，不偷偷 commit。
3. claim / 确认 / 绑定使用 `SELECT ... FOR UPDATE`，锁顺序与封印 Python 相同：先聚合后成员。
4. 生成容量用 PostgreSQL advisory lock 原语句，不改成 Redis 信号量。
5. JSON 列按现有 payload 读写。合同测试用持久化 fixture。
6. 禁止 association 级联删除替代应用删除路径。

`backend/alembic/` 是封印历史，默认路径不再 `alembic upgrade head`。

## 7. 队列与 dispatcher

```text
application command
  -> 业务行 queued + async_dispatches pending
  -> COMMIT
  -> asynq.Enqueue
  -> worker FOR UPDATE claim + lease
  -> 成功 / 失败 / unknown
  -> dispatcher 扫描未投递或租约过期的行并重投
```

asynq 只替换 Dramatiq。asynq 的 retry / dead letter / uniqueness 不是 WorkflowRun 状态机。task `MaxRetry=0`。

actor 名对齐：`run_async_dispatch` 为 HTTP 默认入口；内部再分发 graph run、image-session、delivery、local edit、`run_agent_turn_sync`。

dispatcher 必须能单独启动（独立 binary 或 worker 子命令）。启动恢复扫描 PostgreSQL 里可安全重投的 queued 行。provider 调用之后不能证明结果的，保持 `unknown`，不自动重试。Delivery 没有 unknown。

## 8. Session、图片、测试

- Session：Go 发自己的签名 cookie。默认不实现 itsdangerous 兼容读。
- 图片：上传校验、preview/thumbnail、DeliverySpec（cover/contain、锚点、jpeg/png/webp、体积上限）与现有 Pillow 路径对拍。对不上时改 Go，不改 DeliverySpec。
- 测试只用 PostgreSQL。不把 SQLite 方言带进 Go。
- 不把现有 pytest 对译。带走 HTTP 合同、domain 规则、live 恢复和浏览器主链路。

代码量预期：生产 Go 约 4.5～7 万行（归档 CLI 暂不迁）；测试约 1.5～2.5 万行。行数不是成功标准。成功标准是一次改动落在一个功能包内，该包可以单独设计和 review。

## 9. Strangler 切片

同一时刻一条路由或一个 actor 只有一个实现拥有。反向代理或 Go 网关按路径转发剩余 Python。两边共享同一 PostgreSQL、storage、Redis。禁止平行表双写。

| 切片 | 结果 | 依赖 |
|---|---|---|
| 0. 合同包 | OpenAPI / SSE / session / queue 导出 | 无；不切换默认进程 |
| 1. Go host | `/healthz`、Viper、zap、GORM/pgx | 0 |
| 2. Auth / settings 读 | 登录与设置只读 | 1 |
| 3. Storage + MediaObject + 上传 | 上传下载 preview | 2 |
| 4. Product / facts / 商品图 | 商品 API 与 §3.2 四条出生命令 | 3 |
| 5. Media library | `/media-library` | 3 |
| 6. Graph query + command | 工作台读写 live graph | 4 |
| 7. Recipe apply | 预览/应用走 Graph Command | 6 |
| 8. asynq worker + dispatcher | 运行与恢复 | 6 |
| 9. Image session + delivery + local edit | 连续生图、交付、局部修 | 8、3 |
| 10. Agent 投影 / SSE / tools | 对话与内部工具 | 8 |
| 11. Legacy CLI | 归档命令；可继续暂留 Python | 10 非阻塞 |
| 12. Cutover | 默认进程改 Go | 10 与 PRD §6（含工作台证明） |

Agent 切片靠后，因为它和 lease、fencing、SSE cursor、worker 恢复耦合。

## 10. 明确排除

- 改写 `agent-service/`。
- 为迁 Go 而改 Web DTO。
- 在迁移中引入 SaaS。
- Redis 业务缓存。
- 用 AutoMigrate drop 退休表，或新表双写。
- 把已删除的 WorkflowDraft 或 v2 executor 搬进 Go。
- 为迁 Go 而改工作台交互规格。

## 11. Key Decisions

1. 封印 live Python 行为后即可写 `go/`。工作台证明约束 cutover，不约束 host 与功能切片开工。
2. 只换业务后端三个进程。Agent、Web、PostgreSQL 权威和 storage 布局不动。
3. Gin + GORM + Viper + zap + asynq；asynq 不是状态源，保留 `async_dispatches`。schema 走 GORM `CreateTable`/`AddColumn` 加 ExtraDDL，不使用 AutoMigrate。
4. 内部按功能竖切，不按全局 `handlers/dto/models` 横向复制。
5. 跨聚合走同一 `tx` 上的 app 函数，不拆微服务。
6. 测试与生产都用 PostgreSQL。
7. 不把 `exp` Go Agent 当业务后端起点。
8. Cutover 默认重新登录。
9. 没有商品 Draft 包。全局整理 Draft 属于 `library`。

## 12. PR Plan

每个 PR 可独立审查，对应 §9 切片。标题用中文摘要。

| PR | 标题 | 依赖 |
|---|---|---|
| P0 | 导出 HTTP/SSE/session/queue 合同包 | 无 |
| P1 | 增加 `go/` host：healthz、Viper、zap、GORM/pgx | P0 |
| P2 | Auth cookie 与 settings 读取 | P1 |
| P3 | Storage、MediaObject、上传校验 | P2 |
| P4 | Product / facts / product images 与四条出生命令 | P3 |
| P5 | Media library | P3 |
| P6 | Graph query + command | P4 |
| P7 | Recipe 预览与应用 | P6 |
| P8 | asynq worker、dispatcher、durable recovery | P6 |
| P9 | Image session、delivery rendition、local image edit | P8、P3 |
| P10 | Agent projection、SSE、internal tools | P8 |
| P11 | Legacy CLI 迁入或明确继续由 Python 托管 | 非阻塞 |
| P12 | Cutover：默认进程改 Go | P10 与 PRD 成功标准 |
