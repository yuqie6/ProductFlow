# ProductFlow 架构健康度复审

> 复审日期：2026-04-28
> 范围：当前仓库 live facts、已落地治理、仍未实现的边界和下一步架构风险。
> 结论用途：作为当前架构健康度入口，替代已删除的历史后端审查清单和历史架构审查快照。

## 1. 总体结论

**当前健康度：8.0 / 10。**

ProductFlow 当前架构处在“单商家自托管工作台已经可持续迭代”的状态。后端保持 FastAPI presentation /
application / domain / infrastructure 四层结构，传统任务、商品 DAG 工作流、连续生图 durable task 三条异步链路都以
PostgreSQL 状态为权威，Redis/Dramatiq 只承担投递和后台执行。前端使用 React、TypeScript、TanStack Query，
API client 和 DTO 集中在 `web/src/lib/`，商品详情页已经开始按 page-local 组件和工具拆分。

相比历史审查，几项关键治理已经落地：

- ProductWorkflow application 已拆成 graph、mutations、query、execution、context、artifacts、dependencies 等模块，
  `product_workflows.py` 现在主要承担兼容 facade。
- 前端已有 ESLint 和 Vitest 脚本，不再是只有 TypeScript build 的状态。
- `ProductDetailPage.tsx` 已把 Runs、Images、Inspector、NodeCard、canvas utils、download helpers 等拆到
  `web/src/pages/product-detail/`。
- 连续生图已经使用 durable `ImageSessionGenerationTask`，并接入启动恢复、队列位置和失败状态。
- 生成图画廊已经落地，连续生图结果可保存到 `/gallery`。
- 连续生图已有移动端单栏布局，降低手机操作时的面板拥挤。
- 连续生图和商品工作流运行中都使用轻量 status polling，完成后再刷新完整详情。

当前主要风险已经从“热点模块过大、缺少前端质量门禁”转向“异步链路继续增多后的状态一致性、前端交互回归覆盖和生产化边界表达”。没有发现需要立即阻断功能开发的 P0 架构问题。

## 2. 当前真实模块结构

### 后端

后端代码位于 `backend/src/productflow_backend/`：

- `presentation/`：FastAPI app、路由、schemas、鉴权依赖、上传校验和错误映射。
- `application/`：商品、传统文案/海报任务、连续生图、画廊、生成准入和商品工作流 use case。
- `domain/`：共享枚举和领域错误类型。
- `infrastructure/`：SQLAlchemy models/session、Alembic、Redis/Dramatiq queue、storage、text/image provider、poster renderer、logging。
- `workers.py`：Dramatiq actor 入口。
- `config.py`：环境变量、运行时业务配置定义和 DB override 读取。

ProductWorkflow application 当前已经不是单文件承载全部职责。当前模块包括：

- `product_workflow_graph.py`：工作流查询、默认图结构和轻量 status snapshot。
- `product_workflow_mutations.py`：节点、连线、参考图槽位和文案节点编辑。
- `product_workflow_query.py`：商品工作流详情查询。
- `product_workflow_execution.py`：运行创建、节点执行、失败和完成状态推进。
- `product_workflow_context.py`：上游上下文收集和 provider 输入上下文构建。
- `product_workflow_artifacts.py`：CopySet、SourceAsset、PosterVariant 等产物写回。
- `product_workflow_dependencies.py`：DAG 依赖、目标节点和执行顺序计算。
- `product_workflows.py`：对路由保持稳定的 facade。

当前有三类后台执行状态：

- `JobRun`：传统文案生成和海报生成。
- `WorkflowRun` / `WorkflowNodeRun`：商品 DAG 工作流运行。
- `ImageSessionGenerationTask`：连续生图 durable 异步任务。

全局生成准入由 `application/admission.py` 基于数据库中的 active `JobRun`、`WorkflowRun` 和
`ImageSessionGenerationTask` 计数完成。`/api/generation-queue` 暴露当前队列概览；连续生图 status 响应会返回队列位置。

### 前端

前端代码位于 `web/src/`：

- `App.tsx`：当前路由入口，包含 `/login`、`/products`、`/products/new`、`/products/:productId`、
  `/image-chat`、`/products/:productId/image-chat`、`/gallery`、`/settings`。
- `pages/`：登录、商品列表、创建商品、商品详情、连续生图、画廊、设置页。
- `pages/product-detail/`：商品详情 workbench 的 page-local 组件、canvas helpers、下载 helpers、测试和类型。
- `pages/image-chat/`：连续生图状态合并和分支选择 helpers。
- `pages/gallery/`：画廊布局和选择 helpers。
- `components/`：顶栏、状态标签、图片拖拽区、图片参数控件和产品内引导。
- `lib/api.ts` / `lib/types.ts`：REST API client 和前端 DTO。

前端当前质量入口来自 `web/package.json`：

- `pnpm --dir web lint`
- `pnpm --dir web test:run`
- `pnpm --dir web build`

## 3. 已完成治理

### 3.1 文档与产品现实对齐

`README.md`、`docs/PRD.md`、`docs/ARCHITECTURE.md`、`docs/ROADMAP.md`、`docs/USER_GUIDE.md` 已经覆盖当前主线：

- 单管理员自托管，而不是多租户 SaaS。
- ProductFlow workbench、连续生图、画廊、设置页和运行时配置。
- 三类异步执行入口和轻量 status polling。
- Docker Compose 自托管路径和本地开发路径。
- 当前明确不包含多租户、支付、自动投放、对象存储、Helm 或发布版镜像。

历史后端审查清单和历史架构审查快照已经移除，避免旧行数、旧测试入口和旧问题表继续被当成当前事实。

### 3.2 商品工作流拆分

商品 DAG 工作流已经从集中 application 文件拆成按职责命名的模块。路由仍通过稳定 facade 调用，降低了拆分对 API 层的影响。

这次拆分解决了历史审查中的最大后端热点风险，但还没有把领域层扩展为完整 workflow domain model。当前做法仍然是 application use case 直接编排 SQLAlchemy models、provider 输入和产物写回。

### 3.3 前端质量门禁

前端现在具备 ESLint 和 Vitest 脚本，并已有 helper 层测试：

- `web/src/lib/imageSizes.test.ts`
- `web/src/pages/gallery/helpers.test.ts`
- `web/src/pages/image-chat/branching.test.ts`
- `web/src/pages/product-detail/canvasUtils.test.ts`
- `web/src/pages/product-detail/galleryImages.test.ts`
- `web/src/pages/product-detail/useWorkflowCanvas.test.ts`
- `web/src/pages/product-detail/utils.test.ts`

这已经覆盖轻量 status 合并、画布辅助逻辑、画廊布局、连续生图分支和图片尺寸等关键 helper 行为。组件级交互测试仍然偏少。

### 3.4 连续生图 durable task

连续生图已从同步生成请求演进为 durable task：

- API 创建 `ImageSessionGenerationTask` 后入队。
- worker 执行任务并写回 `ImageSessionRound` / generated asset。
- status endpoint 返回轻量任务快照、队列位置、失败原因和最新 round 信息。
- API/worker 启动恢复 unfinished image-session generation tasks。
- 重复执行 terminal/currently-running task 时保持 no-op 或受控状态推进。

### 3.5 画廊与素材回看

画廊已作为独立页面和后端资源落地：

- `GET /api/gallery` 列出收藏生成图。
- `POST /api/gallery` 将连续生图 generated asset 保存为画廊条目。
- `ImageGalleryEntry` 保留来源会话、round、关联商品、提示词、尺寸、模型和下载入口。
- 前端 `/gallery` 提供集中浏览和预览。

### 3.6 运行中轻量轮询

当前运行中刷新策略已经从“高频拉完整详情”改为轻量 status polling：

- 商品工作流运行中轮询 `/api/products/{product_id}/workflow/status`。
- 连续生图运行中轮询 `/api/image-sessions/{image_session_id}/status`。
- status 响应只带运行状态、节点/任务轻量字段、队列信息和必要计数。
- 前端在 status 到达 terminal 或发现新结果时，再刷新完整 workflow/session 详情。

这降低了商品详情和连续生图页面在运行中反复序列化大对象、重渲染历史图像和覆盖本地交互状态的风险。

## 4. 当前主要风险

### R1. Application 仍承担较多领域规则

ProductWorkflow 已拆分，但业务规则仍主要在 application 层直接围绕 SQLAlchemy models 编排。短期可接受；如果继续增加节点级 retry、跳过、复制、版本对比和 provider 分流，规则会继续分散在 execution、mutations、context 和 artifacts 之间。

建议保持渐进治理：只有当同一规则在多个 use case 中重复出现，或测试必须绕过大量数据库状态才能验证时，再抽 domain service/value object，不提前做“大领域模型重写”。

### R2. 前端组件级回归覆盖不足

Vitest 已经覆盖 helper 和部分 hook，但 ProductDetail workbench 的真实交互仍依赖人工验证，包括节点拖拽、连接线创建/删除、右侧 panel 切换、保存草稿后运行、图片填充和下载误触发保护。

下一步质量投资应优先补最容易回归、最难人工穷举的交互，而不是泛泛追求覆盖率数字。

### R3. 异步状态一致性仍是长期核心风险

系统现在有 `JobRun`、`WorkflowRun` 和 `ImageSessionGenerationTask` 三套 durable 状态。它们共享“数据库为权威、queue 可恢复、重复消息 no-op”的原则，但每条链路仍有自己的状态推进和失败处理。

后续任何新增后台任务，都应优先复用这些原则，并加入 queue recovery、enqueue failure、duplicate message、terminal no-op 和 API status snapshot 测试。

### R4. 生产化边界仍需明确表达

当前已有 Docker Compose 自托管路径，但仍不是完整生产平台：

- 不是多用户或多租户系统。
- 没有对象存储适配层，当前 storage 是本地文件系统。
- 没有 SSE/WebSocket 推送，当前运行中状态依赖轮询。
- 没有 Helm chart 或发布版容器镜像，当前是仓库内 Compose 构建。
- 没有审计后台、对象级权限、支付或托管账号体系。

这些不是当前实现缺陷，但文档和 roadmap 必须持续把它们标为未实现或未来方向，避免误导部署预期。

### R5. Provider 错误分类和可观测性仍可继续加强

Provider 调用已经被隔离在 infrastructure 层，但真实 OpenAI-compatible provider 的失败分类、重试提示、限流提示和日志关联仍可继续细化。当前日志和错误处理足够支撑开发/小规模自托管，但还不是面向复杂生产排障的完整 observability 体系。

## 5. 下一步建议

1. **优先补 ProductDetail workbench 关键交互测试**  
   目标不是一次性测全页面，而是覆盖最容易回归的状态转换：status 合并后不丢节点结构、运行完成触发完整刷新、节点拖拽坐标稳定、图片填充/下载不触发错误选择。

2. **为三类 durable task 沉淀共享检查清单**  
   每次新增后台任务都必须回答：DB 状态何时落地、enqueue 失败如何回写、worker 重复消息如何 no-op、API 启动恢复哪些状态、status endpoint 是否轻量。

3. **保持 docs 的 current/future 分层**  
   当前事实继续放在 README、PRD、ARCHITECTURE、USER_GUIDE 和本复审；对象存储、SSE/WebSocket、Helm、多租户等只放在 roadmap 的未来方向或暂不计划，不写成已实现能力。

4. **继续按真实热点拆分，不做全局重写**  
   ProductWorkflow 和 ProductDetail 的拆分方向已经有效。后续拆分应跟着新增功能的真实修改热点走，避免为了架构完整性提前引入 repository、domain service 或复杂前端状态层。

## 6. 当前验证入口

后端：

- `just backend-test`
- `uv run --directory backend pytest`

前端：

- `pnpm --dir web lint`
- `pnpm --dir web test:run`
- `just web-build`
- `pnpm --dir web build`

文档：

- `git diff --check`

当前复审是文档级审查，不声明已经重新跑完整 backend/frontend 测试矩阵。业务行为事实来自当前源码、路由、脚本和现有测试入口。
