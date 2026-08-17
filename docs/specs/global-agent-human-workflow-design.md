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

当前 Agent Turn 仍然以商品工作区或全局素材库作为 scope 边界：商品 `AgentConversation` 绑定 `product_id` 和 `workflow_draft_id`。新建商品时，同一事务会创建一个 `AgentSession`、一个商品工作区 Conversation、一个商品 onboarding `AgentTask` 和一个 sibling Global Conversation；商品创建对话负责收集商品事实、确认输入和生成 WorkflowDraft，全局对话负责后续跨页面操作，onboarding Task 记录等待人工 Intake 的业务目标。迁移窗口内的旧 Session 仍由 Session 列表访问路径懒加载 Global Conversation。Session 已有列表、创建、改名、归档、商品工作区摘要和按 Session 选择商品工作区的 API；工作台和应用级 Global Agent Dock 都可以打开会话列表。Global Agent Dock 还提供 Session/Task 原地改名，以及按 Global Conversation 或单个 Global Task 查看消息的范围选择，避免并行 Task 的消息默认混在一起。`AgentTask`、任务专属 harness run、任务级 Turn 关联和有界页面上下文快照已经落库并接入 Turn 请求，取消、暂停安全边界、恢复首轮 Turn、Task/Session 摘要、监控 Agent 请求创建的 WorkflowRun 也已经接入。Global Agent Dock 已挂在认证后的应用路由外层；页面通过 `web/src/lib/agentPageContext.ts` 发布当前页面的有界事实，全局图库发布真实选中/可见素材和筛选条件，商品工作台发布工作流 revision、打开文件夹和选中节点，Dock 只在 route 匹配时使用该快照。全局图库页面、工作流子图库关联层、Global Agent 素材查询、全局商品与当前有效工作流摘要查询、明确 workflow ID 的有界运行状态查询、素材整理 Draft 的发布/确认、把全局素材关联到明确工作流的 Draft 操作，以及在全局会话中针对明确商品生成和确认 WorkflowDraft 的操作已经落地；独立调度器和统一的执行前 Fresh Observation harness 抽象仍未交付。`ImageSession` 是连续生图会话，已有自己的会话列表和生图任务，但不承担全局业务 Agent 的职责。

Agent service 当前还通过共享 admission 限制所有 Task/Conversation Turn 的活动执行数，等待中的 Turn 保持 durable queued；这项限制不等同于按业务优先级调度 Task。业务级独立调度器和统一的执行前 Fresh Observation harness 抽象仍未交付。

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
| `AgentSession` | 用户和全局 Agent 的长期交流入口，可包含多个任务 | 已实现 Session 元数据、bounded operational summary、Global/Product Conversation 关联、列表/创建/改名/归档、工作区切换、Global Agent Dock 控制面板和 Dock 内改名；全局会话可以针对明确商品生成和确认 WorkflowDraft，物化后的工作流仍由商品工作区人工维护和执行 |
| `AgentTask` | 一个明确的业务目标，可跨页面、跨 Turn、后台运行 | 已实现独立记录、独立 harness run、bounded Task summary、cursor 分页、列表/创建/改名/取消、在安全边界暂停/恢复、Dock 内改名、按 Global Task 筛选消息、工作台打开、全局素材整理/工作流关联 Draft 投影和 Agent 请求 WorkflowRun 的状态同步；按业务优先级调度仍未实现 |
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
  -> Agent 只创建待确认的 WorkflowRunRequest
  -> 用户在 Agent 工作台确认
  -> ProductFlow 复用现有 WorkflowRun application use case
  -> AgentTask 监控 Run，并把状态投影给用户
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

### 6.1.1 商品创建时的会话关系

商品创建需要 Agent 参与时，系统使用商品工作区 Conversation 作为 onboarding 对话，并创建一个绑定该 Conversation 的商品 onboarding `AgentTask`。Conversation 绑定当前商品和 `WorkflowDraft`，负责：

- 读取用户提交的参考图和图片需求；
- 只追问会影响工作流的缺失事实；
- 生成待确认的 WorkflowDraft；
- 在用户确认后进入工作流工作台。Task 初始处于 `WAITING_USER`，表示等待人工提交参考图和图片需求；Intake 成功后变为 `SUCCEEDED`。这个 Task 只覆盖商品创建输入收集，不替代人工工作流编辑和执行。

这条创建对话和 onboarding Task 都属于一个 `AgentSession`，同一 Session 下还有一个 Global Conversation。用户从全局 Dock 的“创建商品”入口进入时，入口把当前 Session ID 带入 `/products/new`；用户在全局 Agent 中明确说“创建一个商品”时，Agent 通过 `create_product_workspace_v1` 在原 Session 下建立商品草稿工作区和 `WAITING_USER` onboarding Task，并返回带有 `workspace`、`agent_session_id` 和 `agent_task_id` 的创建页入口。商品创建对话加入原 Session；用户直接打开 `/products/new` 时没有指定 Session，系统为本次商品创建新建一个 Session。两阶段创建页完成 Intake 后，ProductFlow 在同一业务事务中把 onboarding Task 收口。已完成的 Task 不会自动启动工作流。同一个 Session 创建多个商品时复用同一个 Global Conversation，不重复创建全局入口。两个 Conversation 共享 Session 的归属，但使用各自的 harness run 和对话历史，商品创建的长对话不会把全局图库对话的历史一起塞进模型上下文。

Global Agent Dock 的 Session 列表保留 Global Conversation 作为会话入口，并展开同一 Session 下的商品工作区行。用户点击商品行进入对应商品路由；空的 `WorkflowDraft` 会继续重定向到商品创建表单，已完成 intake 的商品进入工作台。这个入口只切换页面，不改变 Session、Conversation 或 Task 的归属。

商品创建阶段会创建一个专门的 onboarding `AgentTask`，但它不会把人工表单变成后台自动执行。Task 在等待参考图和图片需求时保持 `WAITING_USER`，用户之后要求 Agent 在后台执行、整理或检查时，可以再创建其他独立 `AgentTask`；所有 Task 都关联同一个 Session 和对应的商品 Conversation，并使用自己的 harness run。用户切换 Session 时，商品创建对话、全局对话和后台 Task 的身份都保持不变。

商品创建 Conversation 的终态需要合法的 WorkflowDraft artifact；后台 Task 的固定目标由 Task 记录提供，使用自己的工具和执行结果，不要求每次后台执行都重新提交一份 WorkflowDraft。

### 6.2 Task 负责一个清晰目标

每个 Task 至少需要保存：目标摘要、范围、关联商品/工作流/媒体对象、状态、当前等待原因、Draft 引用、WorkflowRun 引用、创建时间、更新时间和取消信息。

建议状态：

```text
queued -> running -> waiting_user -> succeeded
                    ├-> failed
                    ├-> canceled
                    └-> paused
```

同一个 Task 内的 Agent Turn 默认串行，避免同一目标互相覆盖。不同 Task 可以并行，但由共享 admission 控制模型 Turn 的并发额度。用户可以暂停尚未开始的 Task，或暂停等待回答/确认的 Task；正在运行模型 Turn 或 WorkflowRun 的任务必须使用现有取消链。拥有一百个 Task 不代表同时启动一百个模型请求，Dock 使用 cursor 分页读取任务列表。

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
- 每个 Conversation/Task 使用自己的 harness run；模型使用的工作上下文按 `AGENT_MODEL_CONTEXT_WINDOW` 和 `AGENT_AUTO_COMPACT_TOKEN_LIMIT` 的 token 预算压缩，完整 journal 仍可恢复。
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
- Dock 创建带目标的全局 Task 后，会为该 Task 提交一次固定幂等键的首轮 Turn；已有 Turn、非 `queued` Task 或缺少目标时不会重复启动。商品工作区可以由 Dock 入口或全局 Agent 工具建立，参考图上传、图片需求提交和继续 onboarding 仍由创建页面承接。
- Dock 采用控制面板定位，显示 Session/Task、目标、状态和进入对应商品工作区的入口；支持在 Dock 内改名，并在 Global Conversation 与具体 Global Task 之间切换消息范围；现有工作流画布和人工运行控件保持原有 owner。
- 当前跨商品和跨工作流开放有界只读商品、有效工作流摘要、明确 workflow ID 的最近运行状态查询，以及针对明确商品的 WorkflowDraft 生成/审核；全局图库的整理 Draft 仍有独立入口。全局 Agent 现在还可以针对明确商品和明确工作流创建待确认的 WorkflowRun 请求，确认后复用现有人工执行链；工作流正式编辑、直接运行、取消和重试仍由商品工作区负责。

### 阶段 2：接入页面上下文和任务恢复

- 已在 Web 到 FastAPI 的 Turn 请求中加入结构化 PageContextSnapshot；FastAPI 持久化快照并计算 digest，Go harness 只接收有界合同。
- Global Agent Dock 读取当前页面注册的快照；页面卸载会清理注册内容，route 不匹配时回退到路由级基础上下文，避免把上一个页面的选区带入新页面。
- 全局图库的快照包含当前已选资产、当前已加载资产和搜索/来源/文件夹/标签/归档筛选；商品工作台的快照包含工作流 revision、侧栏模式、打开文件夹和有限数量的选中节点。
- 已把 Task goal 注入任务专属 harness 的固定系统上下文；页面快照作为当前 Turn 的 ambient context，不会覆盖 Task goal。
- 未指定 Task 的普通商品对话继续使用 conversation run，不会因为页面快照自动出现在后台 Task 列表中。
- 已保留既有 Agent Turn 恢复同步，并让 Task Turn 通过任务专属运行路径恢复。恢复逻辑会发现已经落库但尚未创建首轮 Turn 的 `queued` Task，使用固定首轮幂等 key 补建一次 Turn；显式暂停的 Task 不会被恢复逻辑重新排入队列。商品 onboarding Task 在人工 Intake 完成前保持 `waiting_user`，不自动创建模型 Turn。PostgreSQL 保存有界的 Session/Task operational summary，供 Dock、任务列表和恢复索引使用；每次 Turn 启动前 Agent service 重新读取这两个摘要并与页面快照一起作为有界输入，完整 transcript 仍由 harness journal 和 compaction 负责。副作用执行时的 Fresh Observation 继续由各业务 application use case 读取当前 revision、权限和引用状态来完成，独立业务优先级调度器仍待实现。
- Agent service 的共享 admission 只限制活动 Turn 数量，不改变 durable queued 状态；页面切换只更新后续 Turn 的 ambient context，不修改既有 Task 目标。暂停/恢复 API 对运行中的模型 Turn fail closed，避免前端状态与 harness 执行状态分离。

### 阶段 3：接入人工作流执行

- Agent 已有只读 `inspect_workflow_runs_v1` 工具，通过内部 API 读取商品工作区的有界 WorkflowRun/WorkflowNodeRun 列表；全局 Agent 另有 `inspect_global_workflow_runs_v1`，按明确 workflow ID 返回多个商品/工作流的最近运行状态摘要和节点状态计数。
- Agent 请求运行已经开放：商品工作区和 Global Agent 都使用 `request_workflow_run_v1`，但 Global Agent 必须显式提供 `product_id`、`workflow_id` 和 `expected_workflow_revision`；工具只创建带 revision 和幂等键的待确认请求，从不直接启动运行。确认接口复用 `v2_runs.py` 的现有 application use case，并在运行 metadata 中记录 Agent 来源和 Task 关联。
- 用户确认后，WorkflowRun 仍由 PostgreSQL、队列和原有 worker 负责；工作流页面和 Agent 工作台读取同一条运行记录。工作流页面保留直接运行、取消、重试和运行历史。
- 全局 Agent Dock 可以取消关联的 Agent Task；如果 Task 已关联 WorkflowRun，取消会进入现有 WorkflowRun 取消流程。Task 列表读取时会同步 WorkflowRun 的终态。
- Agent 请求重试仍未开放。重试继续由工作流运行面板负责，避免在 Agent 侧重复设计运行重试策略。

### 阶段 4：完成全局图库与工作流子图库

- 已完成 `MediaLibraryAsset` 到工作流子图库的 `WorkflowMediaLibraryAsset` 关联表和同步 command；关联只保存关系，不复制媒体 bytes。
- 已完成 `/media-library` 全局图库页面、工作流子图库入口、批量组织、归档/恢复、关联移除和工作流引用保护。
- ImageChat 的保存动作已经直接写入 `/api/media-library/from-session`；`/gallery` 已重定向到 `/media-library`。旧 `/api/gallery`、历史 DTO 和在线 runtime owner 已退休；旧表仍由迁移 reader 和 backfill command 提供对账证据，物理表清理继续受部署级迁移闸门约束。
- Agent 已支持有界 inspect、生成图库整理 Draft，以及在 UI 中查看影响范围并确认；当前组织操作限于 rename、move、set_tags、archive、restore，工作流关联仍由独立的工作流同步 command 负责。

### 阶段 5：全局 Agent Dock 和跨域业务能力

- 在多个页面挂载同一个全局 Agent 入口，Session 不随路由改变；全局 Agent 可以在当前 Session 下创建商品 onboarding 工作区，返回页面入口后继续使用现有人工创建流程。Dock 支持 Task 状态摘要、cursor 分页和安全暂停/恢复；运行中的模型 Turn 仍由取消链控制。
- 通过 Skill registry 暴露商品、工作流、图库和 Draft 能力；全局 Agent 已可分页查询商品，按明确的商品 ID 查询当前有效工作流摘要，并按明确的 workflow ID 比较最近运行状态。
- 每个有副作用的 Skill 都绑定权限、scope、revision、confirmation policy、idempotency 和验证方式。
- 全局素材整理、明确工作流关联 Draft，以及全局会话针对明确商品的 WorkflowDraft 跨页面确认已经可用；Global Agent 针对明确商品和工作流的执行请求、Global Dock 中的确认卡和按 Task 隔离的运行状态投影已经可用；Session/Task 摘要会在每个 Turn 启动时动态注入；剩余工作是统一的 Fresh Observation harness 抽象、业务优先级调度和更完整的受影响对象跳转。

## 9. 验收条件

- 用户不创建 Agent Session，也可以从工作流页面完成编辑、运行、取消、重试和结果查看。
- 用户在同一个 Session 中处理两个 Task 时，Task 目标和上下文不会互相污染。
- 用户切换商品、工作流或图库页面时，已有 Task 不会被路由静默改写或停止。
- Agent 上下文接近模型限制时，完整 journal 可恢复，Session summary、Task summary 和最近 Turn 仍能重建工作上下文。
- 页面选区只作为意图辅助；执行前使用最新后端事实和 revision。
- Agent 提议的 Workflow、图库和关联变更在确认前不产生副作用。
- 用户点击执行和 Agent 请求执行最终经过同一个工作流 application use case、队列和 worker。
- WorkflowRun 状态、节点状态、错误和取消结果在工作流页面与 Agent 工作台保持一致；重试仍由工作流页面提供。
- 全局图库和多个工作流共享同一个媒体 bytes；解除工作流关联不删除全局资产。

## 10. 明确排除

- 不把 Agent 变成唯一操作入口。
- 不把工作流运行包装成聊天消息并由 Agent journal 代替 PostgreSQL 业务事实。
- 不让当前页面上下文决定已有 Task 的业务范围。
- 不把 `ImageSession`、`AgentConversation`、`AgentTask` 和 `WorkflowRun` 合并成一个大 Session。
- 不在全局 Agent Dock 交付前重写现有工作流画布和运行链路。
