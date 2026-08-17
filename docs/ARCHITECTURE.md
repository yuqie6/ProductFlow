# ProductFlow Architecture

## 1. 系统边界

ProductFlow 是单管理员、单商家工作区，由六个运行单元组成：

1. React/Vite Web。
2. FastAPI 业务 API。
3. Dramatiq worker。
4. Go workflow Agent service。
5. PostgreSQL。
6. Redis 与媒体 storage。

浏览器只访问 Web 和 FastAPI。Agent service 使用独立 bearer token 调用 FastAPI internal API；FastAPI 通过 agent-service internal HTTP/SSE 控制 Turn。API 和 worker 共享 PostgreSQL、Redis 和 storage。

本文只描述当前实现。模块所有权来自当前源码树，行为证据来自对应测试；产品合同见 `PRD.md`，长期理由见 `adr/`，未完成部署证据见 `rollout/`。

## 2. 后端分层

`backend/src/productflow_backend/` 保持四层边界：

- `presentation/`：FastAPI 路由、请求/响应 schema、认证、上传读取和 HTTP 错误映射。
- `application/`：商品、Agent 会话、WorkflowDraft、V2 工作流、图片库、图片会话、配置和异步任务用例。
- `domain/`：枚举、业务异常和不依赖数据库的 DAG 规则。
- `infrastructure/`：SQLAlchemy、provider client、Redis/Dramatiq、storage、日志和 Agent service client。

路由不直接组织复杂事务或 provider payload。应用层拥有业务事务，基础设施层负责外部系统适配。Session 生命周期由 FastAPI dependency 或 worker 管理；public application command 可以拥有一次明确的 commit/rollback，内部 `stage_*` helper 只组装或 flush。

当前代码所有权：

| 能力 | Application/Domain owner | HTTP/External owner | 主要回归测试 |
|---|---|---|---|
| Agent 商品创建 | `agent_product_workspaces.py`, `agent_product_intake.py` | `routes/agent_product_workspaces.py` | `test_agent_product_workspaces.py` |
| Agent Turn 与同步 | `agent_conversations.py`, `agent_control.py`, `agent_sync.py` | `routes/agent_conversations.py`, `infrastructure/agent_service.py` | `test_workflow_agent_service.py` |
| 全局素材整理 Draft | `media_library/draft_contracts.py`, `media_library/drafts.py`, `agent_control.py` | `routes/global_agent_conversations.py`, `routes/agent_internal.py` | `test_media_library_drafts.py` |
| Draft 与物化 | `workflow_drafts/contracts.py`, `service.py`, `materialization.py` | `routes/workflow_drafts.py` | `test_workflow_draft_contracts.py`, `test_workflow_draft_materialization.py` |
| V2 图与运行 | `domain/workflow_rules.py`, `product_workflow/v2_*.py`, `execution.py` | `routes/workflow_drafts.py`, `workers.py` | workflow domain/run/node/recovery tests |
| 商品图片库 | `gallery_assets.py`, `gallery_mutations.py`, `gallery_archives.py`, `media_assets.py` | `routes/products.py` | `test_product_gallery_explorer.py`, `test_media_objects.py` |
| 连续生图 | `image_sessions.py`, `image_generation_core.py` | `routes/image_sessions.py`, image adapters | image-session/provider tests |
| 设置与 provider | `settings.py`, `runtime_settings.py` | `routes/settings.py`, `infrastructure/provider_config.py` | settings/provider/runtime tests |
| V1 归档切换 | `legacy_archives.py`, `legacy_retirement/` | `routes/legacy_archives.py`, `commands/` | legacy archive/cutover/migration tests |
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
- `/gallery`
- `/history` 与 `/history/:archiveKind/:archiveId`
- `/settings`
- `/help`

页面级代码位于 `web/src/pages/`。共享视觉组件位于 `web/src/components/`，HTTP client、DTO、i18n 和浏览器偏好位于 `web/src/lib/`。

商品工作台由三组现有组件组合：

- `agent-workbench/`：对话、SSE 事件、问题确认、Draft 确认和 materialization reveal。
- `product-workflow-v2/`：V2 画布、命令栏、节点详情、运行、配方和交付图。
- `product-detail/`：已复用的画布 chrome、节点卡片、侧栏、快捷键和图片 Explorer。

TanStack Query 管理服务端状态；局部表单、选择和画布交互使用 React state。`api.ts` 是浏览器 HTTP 的统一入口。

当前前端所有权：

| 能力 | Owner | 主要测试 |
|---|---|---|
| Agent 创建表单 | `AgentProductCreatePage.tsx`, `pages/product-create/` | selection/form/workspace API tests |
| Agent 对话、SSE、Draft 确认 | `pages/agent-workbench/` | reducer, event, conversation, confirmation and reveal tests |
| V2 画布与详情 | `pages/product-workflow-v2/` | graph, canvas, command, draft, history and rendition tests |
| 全局素材库与工作流子图库 | `MediaLibraryPage.tsx`, `product-workflow-v2/WorkflowMediaLibraryPanel.tsx` | media library/application tests, web build |
| 共享工作台与图片库 | `pages/product-detail/` | shortcuts, interaction and image-explorer tests |
| HTTP 和 wire DTO | `lib/api.ts`, `lib/types.ts` | `lib/*Api.test.ts`, TypeScript build |
| 历史只读页 | `LegacyHistoryPage.tsx`, `pages/legacy-history/` | legacy history/model/API tests |

## 4. Agent 创建链路

```text
image types + quantities + 1..6 uploads
  -> Product + ProductImageAsset + WorkflowDraft + AgentSession + AgentConversation
  -> ProductFlow submits Agent Turn
  -> agent-service / agent-harness durable execution
  -> ProductFlow internal read and mutation tools
  -> versioned WorkflowDraft artifact
  -> user confirmation
  -> schema-v2 workflow materialization
  -> reveal event stream
  -> product workbench
```

ProductFlow 是业务数据权威。Agent service 保存 durable Turn transcript、tool call/result 和 token delta；PostgreSQL 保存 AgentSession、AgentTask、AgentConversation、AgentTurnProjection、PageContextSnapshot、问题状态和 WorkflowDraft revision。每个 AgentTask 有自己的 harness run 和 scope；未指定 Task 的旧工作区 Turn 继续使用 conversation run，页面快照可以独立挂在这类 Turn 上。当前 Task 已支持独立 Turn、取消和恢复同步；Agent service 通过共享 admission 限制所有 Service 实例的活动 Turn 数量。Task 摘要、暂停/恢复、业务级统一调度器和 Fresh Observation 仍在后续阶段。
商品创建会在一个业务事务中创建 Product、WorkflowDraft、商品工作区 AgentConversation、商品 onboarding AgentTask 和 AgentSession；onboarding Task 初始为 `WAITING_USER`，等待人工提交参考图和图片需求，Intake 成功后在同一事务中收口为 `SUCCEEDED`。商品创建路径产生的 Session 会在 Session 列表或 Global Agent Dock 访问时懒加载 Global Conversation，独立的新建 Session API 则在创建时直接生成 Global Conversation。Global Conversation 与商品 Conversation 共用 Session 归属，但保留各自的 scope、harness run、Turn 和 Draft，不合并 transcript。onboarding Task 只记录创建商品这段业务目标，不取得工作流执行权；人工编辑、运行、取消和重试继续走工作流页面的原有链路。

Agent service 通过 `tool.step` SSE 事件和 Turn 状态 `tool_steps` 暴露有界工具步骤投影，字段固定为 `step_id`、`kind`、`summary`、`status`。当前 kinds 为 `inspect_image`、`inspect_context`、`read_history`、`organize_assets`、`propose_draft`；statuses 为 `running`、`succeeded`、`failed`、`unknown`。`question.required` 继续独立拥有 Question，不投影为 tool step；当前没有真实 `generate_image` Agent tool，不提前加入。`AgentTurnProjection.tool_steps_json` 保存这份 web projection：缺失 `tool_steps` 表示兼容旧服务并保留现有 snapshot，显式 `[]` 才清空。

Agent 读取商品资产时先获取有界元数据列表，再选择需要检查的图片。图片工具结果使用版本化多模态合同，不把整个图库或 data URL 拼进文本历史。

应用级 `GlobalAgentDock` 位于认证后的应用壳层，负责 Session/Task 控制面板、状态搜索、任务创建、会话归档、任务取消和工作区跳转。它不承载商品工作流的编辑器、运行按钮或 WorkflowRun 状态 owner；这些能力继续由商品工作台和 `v2_runs.py` 提供。

全局图库 Agent 的重命名、移动、标签和归档/恢复通过 `LibraryOrganizationDraft` 完成：发布只写 Draft revision，用户确认后由 ProductFlow 重新观察事实、校验 revision 和引用保护，再在一个事务中应用；确认请求使用幂等键和 request hash。

实现链路：`routes/agent_product_workspaces.py` 创建 workspace，`agent_product_workspaces.py` 在一个业务事务中保存 Product、资产、WorkflowDraft、Session 与 Conversation；`agent_control.py` 调用 `infrastructure/agent_service.py`；`agent_sync.py` 投影 durable Turn；商品 artifact 由 `workflow_drafts/service.py` 接收并校验，`workflow_drafts/materialization.py` 原子写入 V2 图和 reveal events；全局素材 artifact 由 `media_library/drafts.py` 接收、确认并物化。

## 5. WorkflowDraft

WorkflowDraft 是 Agent 和用户确认之间的持久化边界。revision payload 包含：

- 商品事实及其来源、状态和冲突。
- 用户选择的图片类型和每类数量。
- 工作流级视觉体系。
- 每张图片的提示词、参考图绑定和生成规格。
- 文件夹、节点和连线计划。
- 可选的用户配方 seed。

Draft 状态依次覆盖 collecting、awaiting_confirmation、confirmed、materializing、ready，以及 failed/cancelled 终态。每次 Agent artifact 都追加 revision；确认针对明确 revision，避免并发覆盖。

## 6. V2 工作流

ProductWorkflow 的在线 schema 固定为 2。节点类型为：

- `product_context`：商品事实和视觉体系入口。
- `reference_image`：一对一绑定 ProductImageAsset。
- `prompt_generation`：生成、编辑和版本化单图提示词。
- `image_generation`：根据上游内容和 GenerationSpec 生成图片。

WorkflowFolder 是局部视觉分组；它不改变 DAG 执行语义。WorkflowEdge 表示上游依赖。领域层拓扑排序拒绝跨工作流引用和循环。

文件夹只支持一层，不拥有运行状态、端口、嵌套、取消或重试语义。聚合状态由成员节点推导。

WorkflowRun 和 WorkflowNodeRun 保存运行状态。worker 根据已成功的上游节点调度 ready 节点。提示词产物使用版本记录；图片结果写入 ProductImageAsset，并绑定回目标节点。

工作流运行由 ProductFlow 业务接口直接创建和校验。工作流页面可以直接提交整个 DAG 或单个节点，用户不需要先创建 Agent Conversation；未来 Agent 代为请求运行时，必须复用这些 application use case、权限、版本和队列约束。

WorkflowRecipe 保存用户主动创建的完整工作流或局部片段。recipe payload 只保存可复用结构和配置，不保存商品身份、生成结果或媒体字节。

图规则由 `domain/workflow_rules.py` 负责；结构命令、节点编辑、reference binding 和运行分别位于 `application/product_workflow/v2_*.py`。worker 只从 `workers.py` 进入 `application/product_workflow/execution.py`，不维护第二套执行逻辑。

## 7. 图片模型

`MediaObject` 保存 storage 路径、MIME、字节数、尺寸、哈希和核验状态。`ProductImageAsset` 保存商品作用域内的显示名、来源、文件夹、父图和图片类型。

当前图片来源：

- `upload`
- `workflow_generation`
- `image_session_attach`

`legacy_import` 只表示迁移期可读的 canonical 历史资产，不是在线写入来源。

商品图片库、节点参考绑定、封面和交付图都使用 ProductImageAsset id。图片会话的资产也必须关联 MediaObject；保存到商品时创建 ProductImageAsset。

DeliveryRenditionJob 从 ProductImageAsset 读取原始媒体，按裁切、缩放和格式规范异步生成交付文件。交付文件不替换源图。

GenerationSpec 保存模型生成意图；provider effective values 和解码后的 actual output 保存在运行/生成记录中。DeliverySpec 是独立确定性合同，不能触发图片模型调用。

全局素材库读取、文件夹/标签/归档、来源保存和工作流关联由 `application/media_library/`、`routes/media_library.py`、`MediaLibraryPage.tsx` 和 `WorkflowMediaLibraryPanel.tsx` 负责，列表使用有界 cursor page 和 preview/thumbnail URL。`/gallery` 只保留旧书签兼容重定向；旧 `/api/gallery` route、DTO 和在线 runtime owner 已移除。`application/legacy_retirement/media_library.py` 与 `commands/backfill_media_library.py` 只为旧物理表提供有界迁移读取。商品工作台中的 `product-detail/image-explorer/` 继续负责商品作用域的人工选图和绑定。

## 8. Provider 架构

`ProviderProfile` 保存 endpoint、secret、能力、默认模型和 provider 级配置。`ProviderBinding` 把一个 profile 绑定到用途：

- `prompt`：提示词节点。
- `agent`：workflow Agent。
- `image`：工作流和图片会话生图。

FastAPI 解析 prompt/image 绑定；Agent service 通过受内部 token 保护的 endpoint 获取 agent 绑定。缺少绑定、profile 被禁用或模型为空时返回明确配置错误。

运行时图片工具设置经过允许字段校验，再由具体 provider adapter 映射。候选数量来自图片类型数量或图片会话 generation_count，不属于高级 tool option。

## 9. 异步与恢复

- Dramatiq 负责工作流节点、生图会话候选和交付图任务。
- Redis 承担 broker 和并发 admission。
- PostgreSQL 保存 queued/running/terminal 状态、attempt 和错误摘要。
- worker 启动恢复可安全重投的未完成任务。
- Agent service 依赖 agent-harness durable journal；SSE event sequence 支持 Last-Event-ID 重放。
- ProductFlow 的 Turn sync 只信任可对账的 harness 状态，未知结果保留 unknown 语义。

## 10. 配置与安全

环境变量只保存启动前必需的基础设施和 secret：

- database、Redis、storage
- admin/session/settings token
- Agent service 地址和内部 token
- 上传限制、日志和 worker 基础参数

Provider profile、purpose binding 和业务运行时设置由 `/settings` 写入数据库。设置页需要管理员 session 和独立 `SETTINGS_ACCESS_TOKEN`。

上传在持久化前校验 MIME、真实图片格式、字节数、像素数和数量。下载接口按数据库资产定位 storage，不接受任意文件路径。

## 11. Schema 演进与 V1 退役

SQLAlchemy metadata 只描述当前在线模型与有边界的 archive/cutover 模型。Alembic 历史 revision 保留，以支持空数据库完整升级。运行时代码不读取 V1 source shape，也不保留双路由或双执行器。

`20260816_0042` 只建立 `legacy_cutover_gates` evidence boundary，保留 V1 source/archive rows。源码已退出 V1 在线路径不代表某个部署已经完成数据切换；生产 source/archive/canonical hash、恢复演练时间和零 active/unknown run 必须按 runbook 产生。任何后续破坏性清理都要在同一事务内先通过 gate。

## 12. 质量门

- Backend：Ruff、完整 pytest、SQLite migration 和 opt-in PostgreSQL/Redis live tests。
- Frontend：Vitest、ESLint、TypeScript 和 Vite production build。
- Agent service：`go test ./...` 和 opt-in live provider transcript test。
- 跨层变更补真实浏览器、真实数据库或真实 provider 验证，验证强度由变更风险决定。

代码与文档同步规则：

- 路由变化同时核对 `App.tsx`、`lib/api.ts`、PRD 页面表和用户指南。
- enum/DTO 变化同时核对 `domain/enums.py`、Pydantic schema、`lib/types.ts`、label map 和 parser tests。
- transaction/queue/recovery 变化沿 application entrypoint、durable row、broker call、worker claim 和恢复测试验证。
- 历史值变化使用真实持久化 fixture 走完 migration、API serialization 和 frontend rendering；只构造当前 DTO 的单元测试不能证明兼容。
- 模块移动同时更新本页所有权表和 package `AGENTS.md`，删除旧路径引用。
