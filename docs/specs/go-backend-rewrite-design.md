# 业务后端迁到 Go（实现设计）

## 1. 状态

- 文档状态：Draft
- 产品合同：`docs/specs/go-backend-rewrite-prd.md`
- 阅读入口：只从 `docs/ROADMAP.md`「schema-v3 之后的工程运行时」进入。
- 当前实现：`backend/` FastAPI 四层 + Dramatiq + Alembic。本文是 v3 之后的目标内部设计。
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

权威边界与现在相同：PostgreSQL 保存状态；Pi 本地文件保存 transcript；Redis / asynq 不是 Run / Draft / 素材的状态源。

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

## 3. 冻结合同

开工前从当时 live API 导出合同包，Go 与剩余 Python 共用：

1. OpenAPI：路径、方法、状态码、`{"detail":"..."}`；结构化校验另带 `error.code` / `error.message` / `error.details.issues`。
2. Cookie 名 `session`。Cutover 默认发 Go 自己的签名 cookie，要求重新登录。
3. Agent internal HTTP 与 Turn SSE：`id: <sequence>`、`event: <kind>`、`data: <json>`，心跳 `: heartbeat`。浏览器断开不取消 Turn。
4. Durable 投递：先写业务行和 `async_dispatches`，再 `asynq.Enqueue`；失败把业务行标成可观察失败/pending，HTTP 503。worker `MaxRetry=0`。
5. 媒体路径与 `MediaObject` 身份；节点绑定继续使用 `ProductImageAsset` id。
6. 配置两层：Viper 只读环境变量 / 本地文件；`app_settings` / `provider_profiles` / `provider_bindings` 仍由设置页写库。

Web 与 `agent-service/` 默认零合同变更。某个 Go 实现无法保持兼容时，先改 Go。

## 4. 默认技术选型

| 层 | 默认 | 约束 |
|---|---|---|
| HTTP | Gin | session、SSE、上传校验、`x-request-id` 自己实现。binding 不替代 application 校验。 |
| ORM | GORM | 见 §6。关闭 AutoMigrate。 |
| 配置 | Viper | 只加载启动配置。运行时设置读 PostgreSQL。 |
| 日志 | zap | 字段对齐 request / run / node run / image-session task id。禁止 secret、cookie、完整 prompt、provider body、bytes。 |
| 队列 | asynq + go-redis | 替代 Dramatiq，不替代 `async_dispatches`。task 名与现有 actor 名对齐。 |
| 驱动 | pgx | 生产与测试都是 PostgreSQL。不支持 SQLite。 |
| 图片 | 与 Pillow 对拍的 Go 编解码 | preview / thumbnail / DeliverySpec 用 golden fixture。WebP 是明确风险。 |

不引入第二套消息总线，不拆微服务，不在 Go 里实现 Agent loop。

## 5. 内部结构：按功能竖切

对外仍是一个单体。对内每个业务模块自持该功能的 HTTP DTO、application 用例和只属于它的表。这对应现有 ARCHITECTURE 所有权表，避免再铺一套全局 `handlers/` + `dto/` + `models.go`。

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
  product/
  media/        MediaObject 原语
  library/      全局图库
  graph/
  draft/
  recipe/
  imagesession/
  delivery/
  agent/        Turn 投影、internal tools、SSE；不直接写 graph 表
```

每个功能包：

- `http.go`：路由与 wire DTO，只在 HTTP 边缘。
- `app.go`：用例。需要事务时接收 `tx`，自己不偷偷 commit。
- `store.go`：只访问本包的表。
- 稳定内部合同（Draft revision、GenerationSpec、DeliverySpec）放本包 `contract.go`，只此一份。

出包只暴露函数，例如 `draft.Confirm`、`graph.SubmitRun`。禁止别的包 import 本包 GORM model。

跨聚合在 **一个** 用例里编排，共用一次事务：

```text
product.CreateFromIntake
  ├─ media.StageUpload(tx, ...)
  ├─ draft.CreateEmpty(tx, ...)
  └─ agent.OpenOnboarding(tx, ...)
  commit 一次
```

Agent 包只做投影和 tool 入口。写 Draft、请求 Run、改素材走 `draft` / `graph` / `library` 的 app 函数。

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

### 5.2 模块与表（开工前按当时 schema 再核对）

| 模块 | 主要表 | 主要路由 / actor |
|---|---|---|
| settings | `app_settings`, `provider_profiles`, `provider_bindings` | `/api/settings` |
| product | `products`, `product_asset_folders`, `product_image_assets`, fact / visual system 版本表 | `/api/products` |
| media | `media_objects` | 上传校验与下载原语 |
| library | `media_library_*` | `/api/media-library` |
| graph | `workflow_graphs` 及 node/edge/group/run/artifact，`workflow_media_library_assets` | `/api/workflow-graphs`，`run_workflow_graph_run` |
| draft | `workflow_drafts`, `workflow_draft_revisions`，物化 / reveal | `/api/workflow-drafts` |
| recipe | `workflow_recipes`, `workflow_recipe_versions` | `/api/workflow-recipes` |
| imagesession | `image_sessions*` | `/api/image-sessions` |
| delivery | `delivery_rendition_jobs` | 交付导出 actor |
| agent | `agent_*`, `library_organization_drafts` | `/api/agent-*`，internal tools，SSE |
| platform/queue | `async_dispatches` | dispatcher |

V1 archive 表与 cutover CLI 第一波仍可留在 Python。

## 6. GORM

当前写入依赖 `FOR UPDATE`、确定性锁顺序、幂等 key 和 JSON 列形状。GORM 规则：

1. 关闭 AutoMigrate。双轨期 schema 权威仍是 Alembic。
2. 每个 public command 显式 Begin/Commit/Rollback。内部 helper 只组装或 flush。
3. claim / 确认 / 绑定使用 `SELECT ... FOR UPDATE`（`clause.Locking` 或原 SQL），锁顺序与现有 Python 相同：先聚合后成员。
4. 生成容量用 PostgreSQL advisory lock 原语句，不改成 Redis 信号量。
5. JSON 列按现有 payload 读写。合同测试用持久化 fixture。
6. 禁止 association 级联删除替代应用删除路径。
7. 表达不了的锁或部分更新，该函数用 `tx.Exec` / pgx。

连续两个切片的写路径都被迫绕开 GORM 时，另开 ADR 考虑 sqlc。那不是本 Draft 的开工条件。

Cutover 后从当时 `alembic upgrade head` dump 做 Go migration 基线（goose 或等价），再冻结 Alembic。

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

actor 名对齐：`run_workflow_graph_run`、`run_agent_turn_sync`、image-session generation、delivery rendition、`run_async_dispatch`。

dispatcher 必须能单独启动（独立 binary 或 worker 子命令）。启动恢复扫描 PostgreSQL 里可安全重投的 queued 行。provider 调用之后不能证明结果的，保持 `unknown`，不自动重试。

## 8. Session、图片、测试

- Session：Go 发自己的签名 cookie。默认不实现 itsdangerous 兼容读。
- 图片：上传校验、preview/thumbnail、DeliverySpec（cover/contain、锚点、jpeg/png/webp、体积上限）与现有 Pillow 路径对拍。对不上时改 Go，不改 DeliverySpec。
- 测试只用 PostgreSQL。不把 SQLite 方言带进 Go。
- 不把现有 2.8 万行 pytest 对译。带走 HTTP 合同、domain 规则、live 恢复和浏览器主链路。

代码量预期：生产 Go 约 4.5～7 万行（归档 CLI 暂不迁）；测试约 1.5～2.5 万行。行数不是成功标准。成功标准是一次改动落在一个功能包内。

## 9. Strangler 切片

同一时刻一条路由或一个 actor 只有一个实现拥有。反向代理或 Go 网关按路径转发剩余 Python。两边共享同一 PostgreSQL、storage、Redis。禁止平行表双写。

| 切片 | 结果 | 依赖 |
|---|---|---|
| 0. 合同包 | OpenAPI / SSE / session / queue 导出 | 可在 v3 尾声准备，不切换默认进程 |
| 1. Go host | `/healthz`、Viper、zap、pgx | 0 |
| 2. Auth / settings 读 | 登录与设置只读 | 1 |
| 3. Storage + MediaObject + 上传 | 上传下载 preview | 2 |
| 4. Product / facts / 商品图 | 商品 API | 3 |
| 5. Media library | `/media-library` | 3 |
| 6. Graph query + command | 工作台读写 | 4，且 v3 leftover 已删 |
| 7. Draft / recipe / materialize | 确认物化 | 6 |
| 8. asynq worker + dispatcher | 运行与恢复 | 6 |
| 9. Image session + delivery | 连续生图与交付 | 8、3 |
| 10. Agent 投影 / SSE / tools | 对话与内部工具 | 8 |
| 11. Legacy CLI | 归档命令；可继续暂留 Python | 10 非阻塞 |
| 12. Cutover | 默认进程改 Go | 10 与 PRD §6 |

Agent 切片靠后，因为它和 lease、fencing、SSE cursor、worker 恢复耦合。Graph 写与 run 必须在 v3 leftover 删除之后再迁。

## 10. 明确排除

- 改写 `agent-service/`。
- 为迁 Go 而改 Web DTO。
- 在迁移中引入 SaaS。
- Redis 业务缓存。
- GORM AutoMigrate 或新表双写。
- 把即将删除的 v2 executor 搬进 Go。

## 11. Key Decisions

1. 时机放在 schema-v3 治理基线之后。
2. 只换业务后端三个进程。Agent、Web、PostgreSQL 权威和 storage 布局不动。
3. Gin + GORM + Viper + zap + asynq；asynq 不是状态源，保留 `async_dispatches`。
4. 内部按功能竖切，不按全局 `handlers/dto/models` 横向复制。
5. 跨聚合走同一 `tx` 上的 app 函数，不拆微服务。
6. 测试与生产都用 PostgreSQL。
7. 不把 `exp` Go Agent 当业务后端起点。
8. Cutover 默认重新登录。

## 12. PR Plan

每个 PR 可独立审查，对应 §9 切片。标题用中文摘要。

| PR | 标题 | 依赖 |
|---|---|---|
| P0 | 导出 HTTP/SSE/session/queue 合同包 | 无（v3 尾声可做） |
| P1 | 增加 `go/` host：healthz、Viper、zap、pgx | P0 |
| P2 | Auth cookie 与 settings 读取 | P1 |
| P3 | Storage、MediaObject、上传校验 | P2 |
| P4 | Product / facts / product images | P3 |
| P5 | Media library | P3 |
| P6 | Graph query + command | P4，v3 leftover 已删 |
| P7 | Draft、recipe、materialize | P6 |
| P8 | asynq worker、dispatcher、durable recovery | P6 |
| P9 | Image session 与 delivery rendition | P8、P3 |
| P10 | Agent projection、SSE、internal tools | P8 |
| P11 | Legacy CLI 迁入或明确继续由 Python 托管 | 非阻塞 |
| P12 | Cutover：默认进程改 Go | P10 与 PRD 成功标准 |
