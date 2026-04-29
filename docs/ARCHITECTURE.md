# ProductFlow Architecture

[中文](ARCHITECTURE.md) | [English](ARCHITECTURE.en.md)
当前架构健康度、已完成治理和剩余风险见 `docs/ARCHITECTURE_HEALTH_REVIEW.md`；本文保持为系统结构说明。

## 1. 系统概览

ProductFlow 由前端、后端 API、后台 worker、PostgreSQL、Redis 和本地文件存储组成：

```text
React/Vite web
  -> FastAPI backend
    -> PostgreSQL metadata
    -> Redis/Dramatiq queue
    -> local storage files
    -> text provider / image provider
  -> Dramatiq worker
    -> same database, queue, storage and providers
```

默认自托管路径由根目录 `docker-compose.yml` 驱动。`docker compose up -d --build` 会构建并启动 PostgreSQL、Redis、FastAPI 后端、Dramatiq worker 和 nginx-served Web 静态站点；API/worker 在容器内通过 `productflow-postgres:5432` 与 `productflow-redis:6379` 连接依赖，并共享挂载到容器 `/app/storage` 的持久化 storage。未设置 `STORAGE_HOST_PATH` 时，storage 使用 Docker named volume `productflow-storage`；迁移旧 systemd 生产环境时，可以设置 host-only 变量 `STORAGE_HOST_PATH=/home/cot/ProductFlow-release/shared/storage` 将既有宿主机 storage 目录 bind-mount 到 `/app/storage`，容器运行时仍保持 `STORAGE_ROOT=/app/storage`。后端容器启动时先执行 Alembic 迁移，再启动 `uvicorn`。

生产更新入口是 `just release`，底层调用 `scripts/release.sh` 执行 Compose 配置校验、停止 legacy user-level systemd 服务（`productflow-backend.service`、`productflow-worker.service`、`productflow-web.service`，用于释放旧发布占用的 29280/29281 端口）、`docker compose up -d --build --remove-orphans` 和 HTTP health checks。`just release-dry-run` 只做配置校验与计划输出，不停止旧服务、不构建、不启动容器。普通更新不会删除 Docker volumes。

本地热重载开发仍由根目录 `justfile` 驱动：可以只启动 `productflow-postgres` 与 `productflow-redis`，API、worker、前端分别由 `just backend-run`、`just backend-worker`、`just web-dev` 启动。开发环境使用 `.env.dev` 中的 `STORAGE_ROOT=./backend/storage-dev`，与生产 Compose storage 隔离；不要通过 shell-sourcing 生产 `.env` 来启动本地开发进程。

## 2. 后端分层

后端代码位于 `backend/src/productflow_backend/`，按以下层组织：

- `presentation/`：FastAPI app、路由、鉴权依赖、Pydantic schemas、上传校验。
- `application/`：商品、文案、海报、画廊、图片会话、商品工作流等用例逻辑。商品工作流已拆成 graph /
  mutations / query / execution / context / artifacts / dependencies 等 page-facing use case 模块，由
  `product_workflows.py` 作为兼容 facade 对外暴露。
- `domain/`：稳定枚举，如任务状态、素材类型、工作流节点类型。
- `infrastructure/`：SQLAlchemy models/session、队列、storage、text/image provider、海报 renderer。
- `workers.py`：Dramatiq actor 入口。
- `config.py`：环境变量配置、运行时配置定义、数据库覆盖读取。

路由层只做输入适配、鉴权、错误映射和序列化；provider 调用、任务状态变更、工作流推进都在 application/infrastructure 边界内完成。

## 3. 前端结构

前端代码位于 `web/src/`：

- `pages/`：登录、商品列表、创建商品、商品详情、画廊、设置、图片会话页面（当前路由为 `/image-chat` 和
  `/products/:productId/image-chat`）。
- `components/`：共享 UI，如顶栏、状态标签、图片拖拽上传区和产品内引导。
- `lib/api.ts`：集中封装 REST API 请求。
- `lib/types.ts`：前端 DTO 类型，需与后端 schemas 保持一致。
- `lib/onboarding.ts`：产品内 guided onboarding 的轻量步骤和浏览器本地进度状态。

前端使用 TanStack Query 管理服务端状态。商品详情页和连续生图页对运行中状态采用轻量 status 轮询：

- 连续生图运行中轮询 `['image-session-status', selectedSessionId]`，只合并任务状态，完成后再刷新完整 session。
- 商品工作流运行中轮询 `['product-workflow-status', productId]`，只合并 node/run 状态，完成后再刷新完整 workflow
  和商品产物查询。

不要重新给完整 `ImageSessionDetailResponse` 或完整 `ProductWorkflowResponse` 加 active 轮询；它们包含历史图片、
节点配置、产物引用和运行记录，运行中高频刷新会放大前端渲染和后端序列化压力。

商品详情页当前是 ProductFlow workbench：画布负责节点、连接线、缩放、平移和节点拖拽；右侧侧栏负责 Details、Runs、Images。画布缩放比例和侧栏宽度是浏览器本地偏好，工作流节点、连接、运行状态和产物仍以数据库为准。

## 4. 数据模型主线

传统商品素材链路：

```text
Product
  -> SourceAsset(original/reference/processed)
  -> CreativeBrief
  -> CopySet(draft/confirmed)
  -> PosterVariant(main_image/promo_poster)
```

连续生图链路：

```text
ImageSession
  -> ImageSessionAsset(reference_upload/generated_image)
  -> ImageSessionRound(one generated candidate per row)
  -> ImageSessionGenerationTask(durable async generation task)
  -> optional Product attachment
  -> optional ImageGalleryEntry
```

商品 DAG 工作流链路：

```text
ProductWorkflow
  -> WorkflowNode(product_context/reference_image/copy_generation/image_generation)
  -> WorkflowEdge
  -> WorkflowRun
  -> WorkflowNodeRun
```

PostgreSQL 是元数据和运行状态的权威存储；Redis/Dramatiq 只负责投递后台执行消息。

工作流节点的用户语义：

- `product_context`：一个商品工作流的商品资料入口。
- `reference_image`：单张当前参考图槽位；手动上传或上游生图填充会替换当前图，旧素材保留在商品历史/素材表。
- `copy_generation`：文案生成和可编辑文案字段。
- `image_generation`：生图触发/配置节点；图片产物填充到下游参考图节点，而不是在生图节点本身展示。

## 5. 异步任务与恢复

当前有两套后台执行入口：

1. `WorkflowRun`：用于商品 DAG 工作流执行。
2. `ImageSessionGenerationTask`：用于连续生图异步生成。

共同原则：

- 数据库记录先落地，Redis 消息只是可恢复的投递尝试。
- 同一商品工作流通过数据库约束避免重复 active run。
- enqueue 失败时会把新建 run 标记为失败，避免 active 状态卡死。
- API 启动时会恢复 queued 的未完成任务/工作流。
- worker 启动时可重置 stale running 状态后重新投递。
- 连续生图不再用用户可配置的硬总超时作为产品语义。运行中任务会持久化 `progress_updated_at`、
  `completed_candidates`、当前候选和 provider response 状态；stale running 恢复按最近 progress heartbeat
  判断 idle，旧行才回退到 `started_at`。
- 连续生图 worker 的 Dramatiq `time_limit` 只保留为内部 failsafe，避免进程永久占用，不作为用户可调的生成总时限。
- Dramatiq actor 对 terminal/currently-running 的重复消息应 no-op。
- 全局生成并发上限通过数据库中的 active `WorkflowRun`、`ImageSessionGenerationTask` 计数实现。
- `/api/generation-queue` 返回全局 durable 队列概览；连续生图 status 响应会带回当前任务的队列位置。

相关入口：

- `productflow_backend.infrastructure.queue.recover_unfinished_workflow_runs`
- `productflow_backend.infrastructure.queue.recover_unfinished_image_session_generation_tasks`
- `productflow_backend.workers`

## 6. Provider 架构

ProductFlow 把模型能力按模态拆分。

文本 provider 位于 `infrastructure/text/`，统一接口为：

- `generate_brief(product_input)`
- `generate_copy(brief, product_input)`

当前实现：

- `mock`
- `openai`（Responses API 兼容）

图片 provider 位于 `infrastructure/image/`，统一服务于海报生成和图片会话。当前实现：

- `mock`
- `openai_responses`（Responses API `image_generation` 工具，支持 `input_image`；连续生图优先使用 background
  response + retrieve polling，把 provider status 写入任务 progress）

Provider 选择由 `config.py` 和对应 factory 控制。路由不直接依赖具体 SDK。

## 7. 海报生成

海报有两种模式：

- `template`：使用本地 Pillow 模板渲染，适合无图片模型密钥的开发/测试。
- `generated`：把确认版文案、商品图和参考图组织为图片 provider 输入，由远程模型生成结果。

两种模式都面向两类产物：

- `main_image`：1:1 电商主图。
- `promo_poster`：3:4 促销海报。

## 8. 配置层级

配置分为两类：

1. Env-only 基础设施配置：`DATABASE_URL`、`REDIS_URL`、`SESSION_SECRET`、`ADMIN_ACCESS_KEY`、`SETTINGS_ACCESS_TOKEN` 等。这些配置在应用访问数据库前就必须可用，或用于保护登录/配置页二次解锁，因此不支持运行时 DB 覆盖。
2. 运行时业务配置：provider、模型、图片尺寸、上传限制、任务重试、全局生成并发上限、海报模式、提示词模板、登录门禁开关、业务删除开关等。它们可由 `.env` / `.env.dev` 提供默认值，也可在登录并二次解锁设置页后通过 `/api/settings` 写入 `app_settings` 并覆盖。

Secret 类配置在 API 响应中不回显已有值。

登录门禁开关 `admin_access_required` 默认开启；开启时私有 API 通过 `require_admin` 要求 Cookie session 中存在管理员登录标记，错误 `ADMIN_ACCESS_KEY` 仍返回 401。关闭时普通工作台和私有 API 可免管理员密钥访问，`GET /api/auth/session` 返回 `authenticated=true` 和 `access_required=false`；但 `/api/settings` 的完整配置读取/写入仍必须先通过独立的 `SETTINGS_ACCESS_TOKEN` 解锁。

业务删除开关 `deletion_enabled` 默认关闭；关闭时后端在路由边界拒绝商品整删和连续生图会话整删，避免体验站违规内容被整条删除后无法溯源。工作流节点/连线编辑和参考图删除不受该开关影响。`DELETE /api/auth/session` 和设置页恢复数据库覆盖值不属于业务删除保护范围。

提示词模板覆盖范围包括商品理解、文案生成、工作台生图和连续生图。基础设施配置和 secret 读取仍保持后端边界；前端只展示配置项、来源和保存状态。

## 9. 文件存储与下载

本地文件由 `infrastructure/storage.py` 中的 `LocalStorage` 管理。它把相对路径约束在配置的 `STORAGE_ROOT` 下，并拒绝绝对路径或路径穿越。生产 Compose 容器内的 `STORAGE_ROOT` 固定为 `/app/storage`；`STORAGE_HOST_PATH` 只控制宿主机 bind mount 来源，不应传入应用逻辑替代 `STORAGE_ROOT`。

用户可下载的文件通过受控路由读取，例如：

- `/api/posters/{poster_id}/download`
- `/api/source-assets/{asset_id}/download`
- `/api/image-session-assets/{asset_id}/download`

不要绕过 storage 服务直接拼接用户可控路径。

## 10. 安全边界

当前安全模型是“单管理员自托管”：

- 管理员密钥登录，不是公开注册。
- `ADMIN_ACCESS_KEY` 只从环境变量读取，不进入数据库配置；登录门禁可通过 `admin_access_required` 运行时开关关闭，但默认保持开启。
- 配置页使用独立的 `SETTINGS_ACCESS_TOKEN` 二次解锁；session 只保存已解锁标记，不保存令牌明文。关闭登录门禁不会关闭这个二次解锁。
- Session cookie 由 `SESSION_SECRET` 签名。
- CORS 由 `BACKEND_CORS_ORIGINS` 控制。
- 上传文件有 MIME、大小、像素和数量限制。
- Provider API key 保存在 env 或数据库配置中，接口不回显 secret。

当前不提供多用户隔离、对象级权限、审计日志或生产 WAF 配置。
