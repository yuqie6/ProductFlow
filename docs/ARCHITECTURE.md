# ProductFlow Architecture

## 1. 系统边界

ProductFlow 是单管理员、单商家工作区，由七个运行单元组成：

1. React/Vite Web。
2. FastAPI 业务 API。
3. Dramatiq worker。
4. PostgreSQL async dispatcher。
5. Node.js 22 + Pi SDK ProductFlow Agent service。
6. PostgreSQL。
7. Redis 与媒体 storage。

浏览器只访问 Web 和 FastAPI。Agent service 使用独立 bearer token 调用 FastAPI internal API；FastAPI 通过 agent-service internal HTTP/SSE 控制 Turn。API、worker 和 async dispatcher 共享 PostgreSQL、Redis 和 storage。`just dev` 与 Docker Compose 都会启动 dispatcher。

本文只描述当前实现。模块所有权来自当前源码树，行为证据来自对应测试；产品合同见 `PRD.md`，长期理由见 `adr/`。改 Agent service 时再读 `adr/0007-pi-agent-runtime-boundary.md` 与 `specs/pi-agent-runtime-integration.md`。

## 2. 后端分层

`backend/src/productflow_backend/` 保持四层边界：

- `presentation/`：FastAPI 路由、请求/响应 schema、认证、上传读取和 HTTP 错误映射。
- `application/`：商品、Agent 会话、schema-v3 图、图片库、图片会话、配置和异步任务用例。
- `domain/`：枚举、业务异常和不依赖数据库的 DAG 规则。
- `infrastructure/`：SQLAlchemy、provider client、Redis/Dramatiq、storage、日志和 Agent service client。

路由不直接组织复杂事务或 provider payload。应用层拥有业务事务，基础设施层负责外部系统适配。Session 生命周期由 FastAPI dependency 或 worker 管理；public application command 可以拥有一次明确的 commit/rollback，内部 `stage_*` helper 只组装或 flush。

当前代码所有权：

| 能力 | Application/Domain owner | HTTP/External owner | 主要回归测试 |
|---|---|---|---|
| Agent 商品创建 | `agent/product_workspaces.py`, `application/product_intake.py` | `routes/agent_product_workspaces.py` | `test_agent_product_workspaces.py` |
| Agent Session 与 Task | `agent/sessions.py`, `tasks.py` | `routes/agent_sessions.py`, `routes/agent_tasks.py` | `test_agent_sessions.py`, `test_agent_tasks.py` |
| Agent Turn 与同步 | `agent/conversations.py`, `turn_projection.py`, `control.py`, `execution.py`, `sync.py`, `turn_status.py`, `idempotency.py` | `routes/agent_conversations.py`, `infrastructure/agent_service.py` | `test_workflow_agent_service.py` |
| Agent 工具与上下文 | `agent/tool_ledger.py`, `gallery_tools.py`, `media_library_tools.py`, `graph_tools.py`, `agent_context.py` | `routes/agent_internal.py` | `test_workflow_agent_service.py`, `test_graph_proposals.py`, `test_media_library_drafts.py` |
| 全局素材库 | `media_library/` (`queries.py`, `service.py`, `organization.py`, `workflow.py`) | `routes/media_library.py` | `test_media_library.py`, `test_media_library_api.py` |
| 全局素材整理 Draft | `media_library/draft_contracts.py`, `media_library/drafts.py`, `agent/control.py` | `routes/global_agent_conversations.py`, `routes/agent_internal.py` | `test_media_library_drafts.py` |
| 图 artifact 合同 | `domain/artifact_contracts.py` | `routes/workflow_graphs.py` | `test_workflow_draft_api.py`（退休 URL 404） |
| schema-v3 图与执行 | `domain/graph_catalog.py`, `domain/graph_rules.py`, `product_workflow/graph_*.py`, `graph_run_durability.py` | `routes/workflow_graphs.py`, `workers.py` | `test_graph_proposals.py`, graph compiler/run 测试 |
| 配方 | `workflow_recipes/service.py`, `live_apply.py` | `routes/workflow_recipes.py` | `test_workflow_recipes.py` |
| 交付图 | `delivery_renditions/` | `routes/delivery_renditions.py` | `test_delivery_renditions.py` |
| 商品图片库 | `product_images/` (`queries.py`, `mutations.py`, `archives.py`, `assets.py`), `media_objects.py` | `routes/products.py` | `test_product_gallery_explorer.py`, `test_media_objects.py` |
| 连续生图 | `image_sessions/` (`service.py`, `generation.py`), `infrastructure/image/chat_types.py` | `routes/image_sessions.py`, image adapters | image-session/provider tests |
| 设置与 provider | `settings.py`, `infrastructure/runtime_settings.py` | `routes/settings.py`, `infrastructure/provider_config.py` | settings/provider/runtime tests |
| 异步投递 | `async_delivery.py`, `durable_recovery.py` | `commands/run_async_dispatcher.py`, Compose `productflow-async-dispatcher` | `test_async_delivery.py`, `test_async_dispatcher_command.py` |
| 错误与日志 | `domain/errors.py` | `presentation/errors.py`, `infrastructure/logging.py`, request middleware and workers | `test_error_handling.py`, `test_logging_behavior.py` |

## 3. 前端结构

`web/src/App.tsx` 注册当前页面：

- `/login`
- `/products`
- `/products/new`
- `/products/new/agent`，只重定向到 `/products/new`
- `/products/:productId`
- `/image-chat`
- `/media-library`
- `/settings`
- `/help`

页面级代码位于 `web/src/pages/`。共享视觉组件位于 `web/src/components/`，HTTP client、DTO、i18n 和浏览器偏好位于 `web/src/lib/`。

商品工作台位于 `pages/workbench/`，按职责分成三组：

- `workbench/agent/`：页面编排、对话、SSE 事件、问题确认、图提案确认和 Goal 托管环。
- `workbench/canvas/`：当前 Graph 画布、节点详情、运行、配方和交付图。
- `workbench/chrome/`：画布 chrome、节点卡片、侧栏、快捷键和图片 Explorer。

依赖方向固定为 `agent -> canvas, chrome`，`canvas -> chrome`。`chrome` 不得引用 agent 或 canvas。

TanStack Query 管理服务端状态；局部表单、选择和画布交互使用 React state。`api.ts` 是浏览器 HTTP 的统一入口。

当前前端所有权：

| 能力 | Owner | 主要测试 |
|---|---|---|
| Agent 创建表单 | `AgentProductCreatePage.tsx`, `pages/product-create/` | selection/form/workspace API tests |
| Agent 对话、SSE、Goal | `pages/workbench/agent/` | reducer, event, conversation, Goal 与提案测试 |
| Graph 画布与详情 | `pages/workbench/canvas/` | graph catalog/layout/canvas, inspector, runs and rendition tests |
| 全局素材库与工作流子图库 | `MediaLibraryPage.tsx`, `workbench/canvas/WorkflowMediaLibraryPanel.tsx` | media library/application tests, web build |
| Global Agent Dock | `components/GlobalAgentDock.tsx` | `GlobalAgentDockComponents.test.ts` |
| 共享工作台与图片库 | `pages/workbench/chrome/` | shortcuts, interaction and image-explorer tests |
| HTTP 和 wire DTO | `lib/api.ts`, `lib/types.ts` | `lib/*Api.test.ts`, TypeScript build |

## 4. Agent 创建链路

```text
product name (+ optional types and 1..6 uploads)
  -> Product + live schema-v3 graph + product-owned AgentSession + AgentConversation
  -> user first message
  -> ProductFlow submits Agent Turn
  -> agent-service / Pi SDK ProductFlow adapter
  -> apply / propose ChangeSet on the live graph
  -> product workbench
```

ProductFlow 拥有商品、图提案确认、WorkflowGraphRun 和 Web projection。Agent service 使用 Pi SDK 运行模型 loop，并在自己的数据根保存 session/event 文件；这些文件不是业务权威。PostgreSQL 保存 AgentSession、AgentTask、AgentConversation、Turn projection、PageContextSnapshot、问题状态、`LibraryOrganizationDraft` revision，以及跨实例浏览器事件源 `agent_turn_events`。Turn 事件 `run_id` 与 Turn 投影 `harness_run_id` 使用 `application/agent/turn_projection.py` 的 `expected_harness_run_id`：绑 Task 用 Task run，否则用 Conversation run。

商品创建在一个业务事务中写入 Product、live schema-v3 图、商品 Conversation 和归属该商品的 AgentSession。不创建 onboarding Task，不自动提交开场 Turn。名称-only 的图含 `product_source`；表单齐了则与直接创建同一套模板。直接创建不建 Session。画布 Session 的 `product_id` 非空；全局 Dock 列表只含 `product_id` 为空的 Session。独立新建全局 Session 不要求名称；临时名称来自首条全局 Turn，人工重命名优先。全局 Agent 创建商品工作区会新开画布 Session，使用 `creation_idempotency_key` 和 `creation_request_hash` 做只读对账。

`GlobalAgentDock` 负责 Session/Task 列表、搜索、跳转和待确认整理 Draft，不拥有画布或 WorkflowGraphRun。全局素材整理只发布 `LibraryOrganizationDraft`；用户确认后由 ProductFlow 重新观察事实并应用。

主线承诺交互式 Turn、取消、问题回答、SSE 重连，以及 Pi session 上下文的跨进程加载。不承诺模型请求原地恢复、后台 durable Task、完整多实例调度或全量副作用对账。lease、fencing、continuation Turn、`tool_steps` 白名单和 effect reconciliation 以 `application/agent/`、`agent-service/src/pi-runtime.ts` 与 `test_workflow_agent_service.py`、`test_agent_product_workspaces.py`、`test_media_library_drafts.py` 为准。

实现入口：`routes/agent_product_workspaces.py` → `agent/product_workspaces.py`；Turn 控制 `agent/control.py` → `infrastructure/agent_service.py` → `agent-service/src/pi-runtime.ts`；投影 `agent/sync.py`；全局素材 Draft `media_library/drafts.py`。商品 `WorkflowDraft` HTTP 已删除，对应 URL 返回 404。商品 Goal 是显式 `AgentTask`：Turn 或 `WorkflowGraphRun` 结束不会把 Goal 标成完成；用户通过 `POST /api/v2/agent-tasks/{id}/complete` 完成。

## 5. 商品 intake 与已移除的 WorkflowDraft 拓扑

商品图种、数量和参考图 ID 存在 Product 的 intake 上。创建路径不插入 `WorkflowDraft`。商品 Conversation 只要求 `product_id`。产品路径上的 `propose_workflow_draft` / 确认 / persist 不存在；对应 HTTP 返回 404。`workflow_drafts` 表已删除。

全局库整理仍使用 `LibraryOrganizationDraft`。多节点改图确认走 `WorkflowGraphProposal`。

## 6. 在线 schema-v3 图

在线工作流保存在 `workflow_graphs`，schema 固定为 3。为什么是 live graph 而不是第二份 Draft 拓扑，见 `adr/0008-free-canvas-agent-graph-authority.md`。

图上只有三类权威对象：Node（配置与当前输出引用）、Edge（类型、角色、顺序、依赖）、Artifact（一次运行的不可变结果）。用户、Agent 和配方都通过 `apply_graph_change_set` 写入。ChangeSet 操作：`create_node`、`update_node_config`、`rename_node`、`delete_node`、`connect_nodes`、`disconnect_edge`、`move_nodes`、`create_group`、`move_nodes_to_group`、`rename_group`、`dissolve_group`。不完整 DAG 可以保存；运行前再查完整性。处理节点在画布上最多一个聚合输入端口。运行时上下文只读目标节点的 incoming edges，见 `domain/graph_catalog.py` 与 `domain/graph_rules.py`。

节点类型为：

- `product_source`：商品事实入口。
- `image_asset`：一对一绑定 ProductImageAsset。
- `creative_brief`：运行时根据商品资料和参考图生成创作要求，结果写入节点并可以再编辑。
- `visual_system`：运行时根据商品资料和参考图生成风格与背景约束，结果写入节点并可以再编辑。
- `prompt_generation`：运行时根据上游上下文生成提示词，结果写入节点并可以再编辑。
- `image_generation`：根据已生成的提示词和 GenerationSpec 生成图片。运行此节点会入队目标以及 digest 过期或尚无产物的上游处理节点；未连接的视觉规范或创作要求不会被扫进来。需要强制重跑全部上游时使用“运行到此节点”。跑内容节点不会自动跑下游生图。

画布分组是一层视觉分组，可进入局部视图并分记视口，不改变 DAG 执行语义。跨组边在全图可见。分组没有端口、运行、取消或重试。边由 Node Catalog 决定 data_type 与 role。节点详情表单按同一份 `config_fields` 渲染，保存走 `update_node_config`。

`WorkflowGraphRun` 和 `WorkflowGraphNodeRun` 保存运行状态。执行读 run snapshot，不再读 live graph。图片结果写入 ProductImageAsset 和 `WorkflowGraphArtifact`。同一 run 由一个 worker 持有；互不依赖的处理节点可同时打 provider，上限为 runtime `generation_max_concurrent_tasks`。一个节点失败或 unknown 不中止同层独立节点；上游失败的下游标失败。证据：`graph_execution.py`、`test_graph_execution.py`。

工作流运行由 ProductFlow 业务接口直接创建和校验。工作流页面可以直接提交整图或单个节点，用户不需要先创建 Agent Conversation。Agent 通过 `agent_workflow_run_requests.py` 创建待确认请求；用户确认后走同一套 `graph_runs.py` / `graph_execution.py` 约束。商品路径 Agent Turn 不能提交 Draft artifact。单次可逆改图走 `apply_graph_change_set`（Agent 立即写入也只接受一条 operation）；多节点重构写入未应用的 `WorkflowGraphProposal`，画布幽灵预览，确认和取消只在画布完成。

WorkflowRecipe 保存用户主动创建的完整工作流或局部片段。配方库只列出用户从 live graph 保存的配方，不预置画布模板。保存从 live schema-v3 graph 提取，payload 是节点/边/分组片段，不含商品身份、绑定素材、生成结果或媒体字节。完整配方只在目标商品还没有 live graph 时创建；已有图时返回冲突。片段配方合并进已有 schema-v3 工作流，无法合并时返回明确冲突，不会写成 Draft 或退休模型。HTTP 保存入口是 `POST /api/v3/products/{product_id}/workflows/{workflow_id}/recipes`；预览/应用是 `POST /api/v3/products/{product_id}/workflow-recipes/{recipe_id}/preview` 与 `.../apply`。

无 live graph 时，`POST /api/v3/products/{product_id}/workflows` 写入一张空的 schema-v3 图（revision 1，无节点/边）；已有 active graph 时返回冲突。空图出生不是空 ChangeSet（operations 至少一条）。节点、边、分组写入仍走 `apply_graph_change_set`。

图规则由 `domain/graph_catalog.py` 与 `domain/graph_rules.py` 负责。目录同时给出端口合同和可编辑配置字段；ChangeSet 写入会拒绝未登记的 `config` 键。结构写入走 `graph_commands.py`（`apply_graph_change_set` / `stage_apply_graph_change_set`）；内存 apply 引擎是 `graph_apply.py` 的 `apply_workflow_change_set`。运行走 `graph_runs.py` / `graph_execution.py` / `graph_run_durability.py`。HTTP 入口是 `presentation/routes/workflow_graphs.py`。

## 7. 图片模型

`MediaObject` 保存 storage 路径、MIME、字节数、尺寸、哈希和核验状态。`ProductImageAsset` 保存商品作用域内的显示名、来源、文件夹、父图和图片类型。

当前图片来源：

- `upload`
- `workflow_generation`
- `image_session_attach`

商品图片库、节点参考绑定、封面和交付图都使用 ProductImageAsset id。图片会话的资产也必须关联 MediaObject；保存到商品时创建 ProductImageAsset。

DeliveryRenditionJob 从 ProductImageAsset 读取原始媒体，按裁切、缩放和格式规范异步生成交付文件。交付文件不替换源图。内置 DeliverySpec 模板由 `application/delivery_renditions/presets.py` 提供，只读 API 顺序为淘宝/天猫首屏 3:4、京东主图 1:1、Amazon 主图 1:1、详情竖图 3:4、场景横图 4:3。模板是便捷默认值，不构成平台审核或合规保证；用户可以覆盖宽高、格式和体积。来源标记为 `docs/ARCHITECTURE.md §7`。

GenerationSpec 保存模型生成意图；provider effective values 和解码后的 actual output 保存在运行/生成记录中。DeliverySpec 是独立确定性合同，不能触发图片模型调用。

全局素材库读取、文件夹/标签/归档、来源保存和工作流关联由 `application/media_library/`、`routes/media_library.py`、`MediaLibraryPage.tsx` 和 `WorkflowMediaLibraryPanel.tsx` 负责，列表使用有界 cursor page 和 preview/thumbnail URL。商品工作台中的 `workbench/chrome/image-explorer/` 继续负责商品作用域的人工选图和绑定。

## 8. Provider 架构

`ProviderProfile` 保存 endpoint、secret、能力、默认模型和 provider 级配置。`ProviderBinding` 把一个 profile 绑定到用途：

- `prompt`：视觉规范、创作要求和提示词节点。
- `agent`：workflow Agent。
- `image`：工作流和图片会话生图。

FastAPI 解析 prompt/image 绑定；Agent service 通过受内部 token 保护的 endpoint 获取 agent 绑定。缺少绑定、profile 被禁用或模型为空时返回明确配置错误。

运行时图片工具设置经过允许字段校验，再由具体 provider adapter 映射。候选数量来自图片类型数量或图片会话 generation_count，不属于高级 tool option。

## 9. 异步与恢复

- Dramatiq 负责工作流节点、生图会话候选和交付图任务。
- Async dispatcher 扫描 PostgreSQL 中的 durable dispatch/recovery 状态并向 Redis 投递；`just dev` 与 Compose 都启动该进程。
- Redis 承担 broker 和并发 admission。
- PostgreSQL 保存 queued/running/terminal 状态、attempt 和错误摘要。
- worker 启动恢复可安全重投的未完成任务。
- Agent service 使用 Pi session 和本地事件文件做 runtime 恢复；带当前 lease/fencing 的事件写入 PostgreSQL `agent_turn_events`，FastAPI SSE 按 cursor 重放，浏览器断开不取消 Agent。启动只重放尚未开始的 queued Turn。无法证明的结果保持 `unknown`。后台 durable Task 与全量对账见 `ROADMAP.md` 与 `rollout/pi-agent-durability.md`。
- ProductFlow 的 Turn sync 只信任符合 Agent service wire contract 的状态；无法证明的外部结果继续保留 `unknown` 语义。

## 10. 配置与安全

环境变量只保存启动前必需的基础设施和 secret：

- database、Redis、storage
- admin/session/settings token
- Agent service 地址和内部 token
- 上传限制、日志和 worker 基础参数

Provider profile、purpose binding 和业务运行时设置由 `/settings` 写入数据库。设置页需要管理员 session 和独立 `SETTINGS_ACCESS_TOKEN`。

上传在持久化前校验 MIME、真实图片格式、字节数、像素数和数量。下载接口按数据库资产定位 storage，不接受任意文件路径。

## 11. Schema 演进

SQLAlchemy metadata 描述当前在线模型。Alembic 历史 revision 目前仍用于空数据库 `upgrade head`。主仓库不写旧数据回填、冻结或 cutover gate。残留 archive/gallery 表和兼容桩按 ADR 0010 删除，而不是继续包一层。跟上主仓库可以重建数据库和 storage。

## 12. 质量门

- Backend：Ruff、完整 pytest、SQLite migration 和 opt-in PostgreSQL/Redis live tests。
- Frontend：Vitest、ESLint、TypeScript 和 Vite production build。跳过 Agent、真实 prompt/image provider 跑完整图的浏览器 gate 是 opt-in：`just web-e2e-live-graph`。
- Agent service：`pnpm --dir agent-service test`、`pnpm --dir agent-service build`，以及真实 provider/依赖的显式 live gate。
- 跨层变更补真实浏览器、真实数据库或真实 provider 验证，验证强度由变更风险决定。

代码与文档同步规则：

- 路由变化同时核对 `App.tsx`、`lib/api.ts`、PRD 页面表和用户指南。
- 用户操作变化同时核对 `USER_GUIDE.md` 和 `web/src/pages/HelpPage.tsx`。
- enum/DTO 变化同时核对 `domain/enums.py`、Pydantic schema、`lib/types.ts`、label map 和 parser tests。
- transaction/queue/recovery 变化沿 application entrypoint、durable row、broker call、worker claim 和恢复测试验证。
- 模块移动同时更新本页所有权表和 package `AGENTS.md`，删除旧路径引用。
