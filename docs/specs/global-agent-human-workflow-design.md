# 全局 Agent 与人工工作流协作设计

## 1. 状态

- 文档状态：Draft
- 形成依据：2026-08-17 对当前 ProductFlow 工作区的代码、设计文档、迁移和测试审计，以及用户对“人工直接操作工作流、Agent 提供辅助”的明确确认。
- 本文描述目标模型和实施顺序，不能当作当前已经交付的能力。当前实现以代码、测试和真实运行证据为准。

## 2. 当前代码基线

当前系统已经具备一条人工直接生产素材的工作流链路：

```text
工作流画布
  ├── 直接编辑节点、连线、文件夹和参数
  ├── 直接运行整个工作流
  ├── 直接运行单个节点
  ├── 查看 WorkflowRun / WorkflowNodeRun
  ├── 取消和重试
  └── 查看生成结果和运行历史
```

当前代码证据：

- 前端运行入口在 `web/src/pages/product-workflow-v2/ProductWorkflowV2CanvasPanel.tsx` 和 `V2NodeInspector.tsx`。
- API client 在 `web/src/lib/api.ts` 的 `runWorkflowV2`、`runWorkflowNodeV2`、`cancelWorkflowRunV2`、`retryWorkflowRunV2`。
- FastAPI route 在 `backend/src/productflow_backend/presentation/routes/workflow_drafts.py`。
- 业务执行用例在 `backend/src/productflow_backend/application/product_workflow/v2_runs.py`，worker 从 `backend/src/productflow_backend/workers.py` 进入执行器。
- `WorkflowRun` 和 `WorkflowNodeRun` 由 PostgreSQL 持有，Redis/Dramatiq 只承担投递和执行调度。

当前 Agent Turn 仍然以商品工作区或全局素材库作为 scope 边界：商品 `AgentConversation` 绑定 `product_id` 和 `workflow_draft_id`，新建商品和旧归档重建会同时创建一个独立的 `AgentSession` 记录；商品创建事务先返回商品 Conversation，商品创建路径产生的 Session 会在 Session 列表或 Global Agent Dock 访问时懒加载 Global Conversation，独立的新建 Session API 则在创建时直接生成 Global Conversation。Session 已有列表、创建、改名、归档、商品工作区摘要和按 Session 选择商品工作区的 API；工作台和应用级 Global Agent Dock 都可以打开会话列表。`AgentTask`、任务专属 harness run、任务级 Turn 关联和有界页面上下文快照已经落库并接入 Turn 请求，取消任务也有 API 和 Dock 控件。全局图库页面、工作流子图库关联层、Global Agent 素材查询和素材整理 Draft 的发布/确认已经落地；跨商品和跨工作流写操作、Task 摘要、暂停/恢复、独立调度器、页面级 WorkflowRun 投影和执行前 Fresh Observation 仍未交付。`ImageSession` 是连续生图会话，已有自己的会话列表和生图任务，但不承担全局业务 Agent 的职责。

## 3. 产品原则

### 3.1 人是主要操作者

用户可以只使用页面完成工作：创建商品、编辑工作流、点击运行、查看结果、重试失败节点、替换图片和调整提示词。上述操作不依赖 Agent Session。

### 3.2 Agent 是辅助入口

Agent 负责理解需求、补问信息、查找对象、提出修改建议、组织批量工作、监控执行状态和解释结果。用户可以把任务交给 Agent，也可以随时回到页面接管操作。

### 3.3 Workflow 是可反复使用的生产工具

Workflow 是用户确认后保存的可编辑 DAG。它可以被用户直接运行，也可以被 Agent 在明确授权和业务校验通过后请求运行。Agent Task 不拥有 Workflow 的执行权威。

### 3.4 UI 是完整工作面

全局 Agent Dock 是辅助面板，不替代工作流画布和运行面板。工作流页面必须保留：编辑、运行、单节点运行、取消、重试、进度、错误、输出预览和历史记录。

## 4. 对象职责

| 对象 | 作用 | 当前状态 |
|---|---|---|
| `AgentSession` | 用户和全局 Agent 的长期交流入口，可包含多个任务 | 已实现 Session 元数据、Global/Product Conversation 关联、列表/创建/改名/归档、工作区切换和 Global Agent Dock 控制面板；Session 摘要、跨商品/跨工作流写操作仍未实现 |
| `AgentTask` | 一个明确的业务目标，可跨页面、跨 Turn、后台运行 | 已实现独立记录、独立 harness run、列表/创建/改名/取消、工作台打开和全局素材整理 Draft 投影；暂停/恢复、Task 摘要、调度额度仍未实现 |
| `AgentTurn` | 一次用户消息、本轮上下文、工具调用和结果投影 | 已关联 Task 和页面上下文快照；同一 Task 的活动 Turn 保持串行 |
| `AgentRun` | harness 内部一次可恢复的执行运行 | 每个 AgentTask 使用自己的 harness run；未指定 Task 的旧工作区 Turn 继续使用 conversation run |
| `PageContextSnapshot` | 某次消息发送时的路由、页面对象、选择、过滤器和 revision 摘要 | 已实现 FastAPI/Go 有界合同和持久化；可以挂在显式 Task Turn 或普通商品对话 Turn 上；执行前 Fresh Observation 仍需补齐 |
| `WorkflowDraft` | Agent 或用户提出的待确认工作流变更 | 已实现 |
| `Workflow` | 用户确认后的可编辑、可执行 DAG | 已实现 |
| `WorkflowRun` | 某次完整工作流执行记录 | 已实现 |
| `WorkflowNodeRun` | 某次执行中的节点状态和产物 | 已实现 |
| `ImageSession` | 连续文/图生图会话和生成任务 | 已实现，职责保持独立 |

`AgentSession` 不能通过重命名 `ImageSession` 或现有商品 `AgentConversation` 得到。`WorkflowRun` 也不能被包装成 Agent Task；Task 可以引用和监控 Run，但运行状态、队列、重试和产物继续由工作流执行域负责。

## 5. 用户操作的三条路径

### 5.1 用户直接运行

```text
用户编辑 Workflow
  -> 点击运行整个工作流或单个节点
  -> ProductFlow 创建 WorkflowRun / WorkflowNodeRun
  -> worker 执行
  -> UI 刷新状态和结果
```

这条路径不创建 Agent Session、Agent Task 或 Agent Turn。用户点击执行本身是明确的操作授权，仍然经过权限、active workflow、revision、队列容量、幂等和 attempt fencing 校验。

### 5.2 Agent 提议修改

```text
用户向 Agent 描述目标
  -> Agent 读取有界事实
  -> Agent 生成 WorkflowDraft 或业务 Draft
  -> 用户在 UI 中查看差异和影响范围
  -> 用户确认
  -> ProductFlow 原子物化
```

Agent 不能通过工具描述或自然语言结果直接写入正式 Workflow、图库或工作流关联。

### 5.3 Agent 请求执行

```text
用户明确授权或确认执行范围
  -> AgentTask 记录目标和范围
  -> ProductFlow 复用现有 WorkflowRun application use case
  -> AgentTask 只监控 Run，并把状态投影给用户
```

Agent 请求执行时不能复制一套工作流执行器，也不能绕过工作流的取消、重试、队列和业务约束。用户可以在工作流页面直接查看或接管这个 Run。

## 6. Session、Task 和并行工作

### 6.1 Session 只负责长期交流

Session 保存会话标题、摘要、用户偏好和任务索引。它不绑定当前 URL、当前商品、当前工作流或当前选区。

用户可以维护多个 Session，例如：

```text
春季新品素材
  ├── 商品 A 主图
  ├── 商品 A 场景图
  └── 商品 B 素材检查

图库整理
  ├── 最近生成图片归档
  └── 缺少标签的图片
```

切换 Session 只改变 Agent 对话入口，不取消后台 Task。

### 6.2 Task 负责一个清晰目标

每个 Task 至少需要保存：目标摘要、范围、关联商品/工作流/媒体对象、状态、当前等待原因、Draft 引用、WorkflowRun 引用、创建时间、更新时间和取消信息。

建议状态：

```text
queued -> running -> waiting_user -> succeeded
                    ├-> failed
                    ├-> canceled
                    └-> paused
```

同一个 Task 内的 Agent Turn 默认串行，避免同一目标互相覆盖。不同 Task 可以并行，但由统一 admission/scheduler 控制模型调用、图片生成和数据库写操作的并发额度。拥有一百个 Task 不代表同时启动一百个模型请求。

### 6.3 前端投影

全局 Dock 展示 Session 列表、Task 状态、待回答问题、待确认 Draft、WorkflowRun 进度和跳转入口。任务的实际目标必须来自 Task 记录，不能随着路由变化被当前页面描述覆盖。

## 7. 上下文策略

Agent 的模型上下文按以下层次组装：

```text
Session summary
  + Task goal and scope
  + recent Turn messages and tool results
  + current PageContextSnapshot
  + execution-time Fresh Observation
```

### 7.1 PageContextSnapshot

每次用户发送消息时，浏览器提交一个有界快照，至少可以表达：

```json
{
  "route": "/products/p1/workflows/w2/library",
  "page_type": "workflow_sub_library",
  "product_id": "p1",
  "workflow_id": "w2",
  "selected_asset_ids": ["asset-1"],
  "visible_asset_ids": ["asset-1", "asset-2"],
  "filters": {"folder": "scene", "tag": "hero"},
  "workflow_revision": 42,
  "library_revision": 108,
  "captured_at": "..."
}
```

快照帮助 Agent 理解“这些图片”和“当前工作流”的指代。它不授予权限，也不决定 Task scope。后端必须重新读取对象归属、权限、当前 revision 和可用状态。

### 7.2 上下文长度

- Go harness 继续保留完整 durable journal。
- 模型使用的工作上下文按 token 预算压缩。
- Session summary 和 Task summary 分开维护，避免一条长期对话吞掉所有任务。
- 页面快照只保存有界 ID、筛选和 revision，不把媒体 bytes 或全量图库塞入历史。
- 执行有副作用的操作前重新读取 Fresh Observation；页面快照过期时要求刷新、重算 Draft 或明确处理冲突。

## 8. 分阶段落地策略

### 阶段 0：守住现有人工工作流

- 保持当前工作流页面的直接编辑、整图运行、单节点运行、取消、重试和历史查询。
- 为用户启动的 WorkflowRun 保持独立的状态投影和错误反馈。
- Agent 接入前先补齐真实浏览器验收，证明用户不打开 Agent 也能完成生产。

### 阶段 1：建立全局 Agent 外壳

- 已新增独立的 `AgentSession` 和 `AgentConversation.session_id` 关联；当前商品 `AgentConversation` 保持原有 Draft 绑定，不改名充当全局 Session。
- 已提供 Session 列表/创建/改名/归档、商品工作区摘要和当前工作台切换入口；切换通过 Session 关联的商品工作区重新读取 bootstrap。
- 当前工作台入口采用可搜索的会话面板，按进行中/已归档分组，显示会话关联的商品工作区；会话管理动作与商品工作流的编辑、审阅和人工运行入口保持分离。
- 已新增独立的 `AgentTask`、Task 专属 harness run、Turn 关系、Task 列表/创建/改名/取消 API，以及应用级 Global Agent Dock。
- Dock 采用控制面板定位，显示 Session/Task、目标、状态和进入对应商品工作区的入口；现有工作流画布和人工运行控件保持原有 owner。
- 当前仍只接入商品工作区范围的 Agent 工具；跨商品、跨工作流和全局图库的副作用能力不在本阶段开放。

### 阶段 2：接入页面上下文和任务恢复

- 已在 Web 到 FastAPI 的 Turn 请求中加入结构化 PageContextSnapshot；FastAPI 持久化快照并计算 digest，Go harness 只接收有界合同。
- 已把 Task goal 注入任务专属 harness 的固定系统上下文；页面快照作为当前 Turn 的 ambient context，不会覆盖 Task goal。
- 未指定 Task 的普通商品对话继续使用 conversation run，不会因为页面快照自动出现在后台 Task 列表中。
- 已保留既有 Agent Turn 恢复同步，并让 Task Turn 通过任务专属运行路径恢复；Session summary、Task summary、stale observation 和独立调度器仍待实现。
- 页面切换只更新后续 Turn 的 ambient context，不修改既有 Task 目标。

### 阶段 3：接入人工作流执行

- Agent 已有只读 `inspect_workflow_runs_v1` 工具，通过内部 API 读取有界 WorkflowRun/WorkflowNodeRun 列表。
- Agent 请求运行、取消和重试尚未开放；这些操作仍由工作流页面直接触发，并继续复用 `v2_runs.py` 的现有 application use case。
- 后续开放 Agent 请求运行时，需要记录来源类型和关联 Task 的审计信息，且保持 WorkflowRun 的业务 owner 不变。
- UI 继续提供直接运行和人工接管；Global Agent Dock 当前提供任务取消，不接管工作流运行按钮。

### 阶段 4：完成全局图库与工作流子图库

- 已完成 `MediaLibraryAsset` 到工作流子图库的 `WorkflowMediaLibraryAsset` 关联表和同步 command；关联只保存关系，不复制媒体 bytes。
- 已完成 `/media-library` 全局图库页面、工作流子图库入口、批量组织、归档/恢复、关联移除和工作流引用保护。
- 旧 Gallery 的保存接口目前作为迁移桥接，同时创建 canonical 全局素材；`/gallery` 已重定向到 `/media-library`，旧表、旧 API 和历史 DTO 仍保留作迁移证据，尚未完成物理 owner 退休。
- Agent 已支持有界 inspect、生成图库整理 Draft，以及在 UI 中查看影响范围并确认；当前组织操作限于 rename、move、set_tags、archive、restore，工作流关联仍由独立的工作流同步 command 负责。

### 阶段 5：全局 Agent Dock 和跨域业务能力

- 在多个页面挂载同一个全局 Agent 入口，Session 不随路由改变。
- 通过 Skill registry 暴露商品、工作流、图库和 Draft 能力。
- 每个有副作用的 Skill 都绑定权限、scope、revision、confirmation policy、idempotency 和验证方式。
- 全局素材整理 Draft 的跨页面确认已经可用；剩余工作是跨页面后台 Task 的完整恢复、WorkflowRun 监控和受影响对象跳转。

## 9. 验收条件

- 用户不创建 Agent Session，也可以从工作流页面完成编辑、运行、取消、重试和结果查看。
- 用户在同一个 Session 中处理两个 Task 时，Task 目标和上下文不会互相污染。
- 用户切换商品、工作流或图库页面时，已有 Task 不会被路由静默改写或停止。
- Agent 上下文接近模型限制时，完整 journal 可恢复，Session summary、Task summary 和最近 Turn 仍能重建工作上下文。
- 页面选区只作为意图辅助；执行前使用最新后端事实和 revision。
- Agent 提议的 Workflow、图库和关联变更在确认前不产生副作用。
- 用户点击执行和 Agent 请求执行最终经过同一个工作流 application use case、队列和 worker。
- WorkflowRun 状态、节点状态、错误、取消和重试在工作流页面与 Agent Dock 中保持一致。
- 全局图库和多个工作流共享同一个媒体 bytes；解除工作流关联不删除全局资产。

## 10. 明确排除

- 不把 Agent 变成唯一操作入口。
- 不把工作流运行包装成聊天消息并由 Agent journal 代替 PostgreSQL 业务事实。
- 不让当前页面上下文决定已有 Task 的业务范围。
- 不把 `ImageSession`、`AgentConversation`、`AgentTask` 和 `WorkflowRun` 合并成一个大 Session。
- 不在全局 Agent Dock 交付前重写现有工作流画布和运行链路。
