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

## 2. 后端分层

`backend/src/productflow_backend/` 保持四层边界：

- `presentation/`：FastAPI 路由、请求/响应 schema、认证、上传读取和 HTTP 错误映射。
- `application/`：商品、Agent 会话、WorkflowDraft、V2 工作流、图片库、图片会话、配置和异步任务用例。
- `domain/`：枚举、业务异常和不依赖数据库的 DAG 规则。
- `infrastructure/`：SQLAlchemy、provider client、Redis/Dramatiq、storage、日志和 Agent service client。

路由不直接组织复杂事务或 provider payload。应用层拥有业务事务，基础设施层负责外部系统适配。

## 3. 前端结构

`web/src/App.tsx` 只注册当前页面：

- `/products`
- `/products/new`
- `/products/:productId`
- `/image-chat`
- `/gallery`
- `/settings`
- `/help`

页面级代码位于 `web/src/pages/`。共享视觉组件位于 `web/src/components/`，HTTP client、DTO、i18n 和浏览器偏好位于 `web/src/lib/`。

商品工作台由三组现有组件组合：

- `agent-workbench/`：对话、SSE 事件、问题确认、Draft 确认和 materialization reveal。
- `product-workflow-v2/`：V2 画布、命令栏、节点详情、运行、配方和交付图。
- `product-detail/`：已复用的画布 chrome、节点卡片、侧栏、快捷键和图片 Explorer。

TanStack Query 管理服务端状态；局部表单、选择和画布交互使用 React state。`api.ts` 是浏览器 HTTP 的统一入口。

## 4. Agent 创建链路

```text
image types + quantities + 1..6 uploads
  -> Product + ProductImageAsset + WorkflowDraft + AgentConversation
  -> ProductFlow submits Agent Turn
  -> agent-service / agent-harness durable execution
  -> ProductFlow internal read and mutation tools
  -> versioned WorkflowDraft artifact
  -> user confirmation
  -> schema-v2 workflow materialization
  -> reveal event stream
  -> product workbench
```

ProductFlow 是业务数据权威。Agent service 保存 durable Turn transcript、tool call/result 和 token delta；PostgreSQL 保存 AgentConversation、AgentTurnProjection、问题状态和 WorkflowDraft revision。

Agent 读取商品资产时先获取有界元数据列表，再选择需要检查的图片。图片工具结果使用版本化多模态合同，不把整个图库或 data URL 拼进文本历史。

图库重命名、建文件夹、移动等 Agent 写操作使用 prepare/apply/reconcile 合同和幂等键，便于在网络中断或重启后对账。

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

WorkflowRun 和 WorkflowNodeRun 保存运行状态。worker 根据已成功的上游节点调度 ready 节点。提示词产物使用版本记录；图片结果写入 ProductImageAsset，并绑定回目标节点。

WorkflowRecipe 保存用户主动创建的完整工作流或局部片段。recipe payload 只保存可复用结构和配置，不保存商品身份、生成结果或媒体字节。

## 7. 图片模型

`MediaObject` 保存 storage 路径、MIME、字节数、尺寸、哈希和核验状态。`ProductImageAsset` 保存商品作用域内的显示名、来源、文件夹、父图和图片类型。

当前图片来源：

- `upload`
- `workflow_generation`
- `image_session_attach`

商品图片库、节点参考绑定、封面和交付图都使用 ProductImageAsset id。图片会话的资产也必须关联 MediaObject；保存到商品时创建 ProductImageAsset。

DeliveryRenditionJob 从 ProductImageAsset 读取原始媒体，按裁切、缩放和格式规范异步生成交付文件。交付文件不替换源图。

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

## 11. Schema 演进

SQLAlchemy metadata 只描述当前在线模型。Alembic 历史 revision 保留，以支持空数据库完整升级；破坏性 schema 清理集中在 migration 中。运行时代码不读取已退出的数据模型，也不保留双路由或双执行器。

## 12. 质量门

- Backend：Ruff、完整 pytest、SQLite migration 和 opt-in PostgreSQL/Redis live tests。
- Frontend：Vitest、ESLint、TypeScript 和 Vite production build。
- Agent service：`go test ./...` 和 opt-in live provider transcript test。
- 跨层变更补真实浏览器、真实数据库或真实 provider 验证，验证强度由变更风险决定。
