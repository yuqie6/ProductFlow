# ProductFlow Architecture

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

开发环境由根目录 `justfile` 和 `docker-compose.yml` 驱动。`docker compose up -d` 只启动 PostgreSQL 和 Redis；API、worker、前端分别由 `just backend-run`、`just backend-worker`、`just web-dev` 启动。

## 2. 后端分层

后端代码位于 `backend/src/productflow_backend/`，按以下层组织：

- `presentation/`：FastAPI app、路由、鉴权依赖、Pydantic schemas、上传校验。
- `application/`：商品、文案、海报、图片会话、商品工作流等用例逻辑。
- `domain/`：稳定枚举，如任务状态、素材类型、工作流节点类型。
- `infrastructure/`：SQLAlchemy models/session、队列、storage、text/image provider、海报 renderer。
- `workers.py`：Dramatiq actor 入口。
- `config.py`：环境变量配置、运行时配置定义、数据库覆盖读取。

路由层只做输入适配、鉴权、错误映射和序列化；provider 调用、任务状态变更、工作流推进都在 application/infrastructure 边界内完成。

## 3. 前端结构

前端代码位于 `web/src/`：

- `pages/`：登录、商品列表、创建商品、商品详情、设置、图片会话页面（当前路由为 `/image-chat` 和 `/products/:productId/image-chat`）。
- `components/`：共享 UI，如顶栏和状态标签。
- `lib/api.ts`：集中封装 REST API 请求。
- `lib/types.ts`：前端 DTO 类型，需与后端 schemas 保持一致。

前端使用 TanStack Query 管理服务端状态。商品详情页会轮询任务/工作流状态，并在 mutation 成功后更新相关 query cache。

## 4. 数据模型主线

传统商品素材链路：

```text
Product
  -> SourceAsset(original/reference/processed)
  -> CreativeBrief
  -> CopySet(draft/confirmed)
  -> PosterVariant(main_image/promo_poster)
  -> JobRun(copy_generation/poster_generation)
```

连续生图链路：

```text
ImageSession
  -> ImageSessionAsset(reference_upload/generated_image)
  -> optional Product attachment
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

## 5. 异步任务与恢复

当前有两套后台执行入口：

1. 传统 `JobRun`：用于文案生成和海报生成。
2. `WorkflowRun`：用于商品 DAG 工作流执行。

共同原则：

- 数据库记录先落地，Redis 消息只是可恢复的投递尝试。
- 同一商品同类任务/工作流通过数据库约束避免重复 active run。
- enqueue 失败时会把新建 run 标记为失败，避免 active 状态卡死。
- API 启动时会恢复 queued 的未完成任务/工作流。
- worker 启动时可重置 stale running 状态后重新投递。
- Dramatiq actor 对 terminal/currently-running 的重复消息应 no-op。

相关入口：

- `productflow_backend.infrastructure.queue.recover_unfinished_jobs`
- `productflow_backend.infrastructure.queue.recover_unfinished_workflow_runs`
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
- `openai_responses`（Responses API `image_generation` 工具，支持 `input_image` 和连续上下文）

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

1. Env-only 基础设施配置：`DATABASE_URL`、`REDIS_URL`、`SESSION_SECRET`、`ADMIN_ACCESS_KEY` 等。这些配置在应用访问数据库前就必须可用，因此不支持运行时 DB 覆盖。
2. 运行时业务配置：provider、模型、图片尺寸、上传限制、任务重试、海报模式等。它们可由 `.env` / `.env.dev` 提供默认值，也可通过 `/api/settings` 写入 `app_settings` 并覆盖。

Secret 类配置在 API 响应中不回显已有值。

## 9. 文件存储与下载

本地文件由 `infrastructure/storage.py` 中的 `LocalStorage` 管理。它把相对路径约束在配置的 `STORAGE_ROOT` 下，并拒绝绝对路径或路径穿越。

用户可下载的文件通过受控路由读取，例如：

- `/api/posters/{poster_id}/download`
- `/api/source-assets/{asset_id}/download`
- `/api/image-session-assets/{asset_id}/download`

不要绕过 storage 服务直接拼接用户可控路径。

## 10. 安全边界

当前安全模型是“单管理员自托管”：

- 管理员密钥登录，不是公开注册。
- Session cookie 由 `SESSION_SECRET` 签名。
- CORS 由 `BACKEND_CORS_ORIGINS` 控制。
- 上传文件有 MIME、大小、像素和数量限制。
- Provider API key 保存在 env 或数据库配置中，接口不回显 secret。

当前不提供多用户隔离、对象级权限、审计日志或生产 WAF 配置。
