# 全局 Agent、Session、Task 与人工工作流边界

## 1. 文档职责

- 文档状态：Approved
- 批准依据：当前对象边界已写入 `CONTEXT.md` 与 `docs/ARCHITECTURE.md`；Pi runtime 边界见 `docs/adr/0007-pi-agent-runtime-boundary.md`。剩余调度和 Fresh Observation 项见 `docs/ROADMAP.md`。
- 本文负责 Global Agent、AgentSession、AgentTask、人工接管和 WorkflowRun 的产品语义。
- 当前实现以代码、测试和真实运行证据为准；本文不替代 `CONTEXT.md`、`PRD.md` 或 `docs/ARCHITECTURE.md`。
- Pi runtime、Skill、动态 Context、ProductFlow Tool、事件翻译和迁移验收由 `docs/adr/0007-pi-agent-runtime-boundary.md` 与 `docs/specs/pi-agent-runtime-integration.md` 负责。

## 2. 产品目标

ProductFlow 的目标是帮助商家获得可实际使用的商品素材图。Agent 是辅助入口，负责理解目标、补充关键事实、查找对象、提出 Draft、组织批量工作、监控运行和解释结果。用户始终可以直接使用页面完成生产。

人工工作流页面继续拥有：

- 工作流编辑、节点和连线管理。
- 整个工作流或单个节点运行。
- 运行状态、结果、取消、重试和历史。
- 图片选择、提示词编辑、文件夹整理和交付操作。

Agent 不取代这些入口，也不创建第二套工作流执行器。

## 3. 对象边界

| 对象 | 负责什么 | 不负责什么 |
|---|---|---|
| `AgentSession` | 长期交流、标题、摘要、任务索引 | 当前 URL、当前选区、工作流运行状态 |
| `AgentConversation` | 一个具体 Agent 对话及其 scope | 把不同商品或全局对话的 transcript 合并 |
| `AgentTask` | 一个明确业务目标、范围、状态和关联对象 | 取代 `WorkflowRun` 的执行状态和重试语义 |
| `AgentTurn` | 一次用户消息、本轮输入、工具调用和结果投影 | 成为商品或工作流业务事实的 owner |
| `PageContextSnapshot` | 发送消息时的页面、选区、筛选器和 revision 摘要 | 授权、改变 Task goal、覆盖后端事实 |
| `WorkflowDraft` | 待审阅的工作流提案和 revision | 已确认的正式 Workflow |
| `WorkflowRun` | 一次工作流执行及其节点状态和产物 | Agent 对话或 Task 的别名 |
| `ImageSession` | 连续文/图生图会话 | 全局业务 Agent Session |

同一个 `AgentSession` 可以包含多个 Conversation 和多个 Task。切换 Session 只改变对话入口，不取消后台 Task。Task 目标来自 Task 记录，不随路由变化。

## 4. 三条用户路径

### 4.1 用户直接运行

```text
用户编辑 Workflow
  -> 点击运行整个工作流或单个节点
  -> ProductFlow 创建 WorkflowRun / WorkflowNodeRun
  -> worker 执行
  -> UI 展示状态和结果
```

这条路径不依赖 Agent Session、Task 或 Turn。

### 4.2 Agent 提议修改

```text
用户描述目标
  -> Agent 读取有界事实
  -> Agent 生成 WorkflowDraft 或业务 Draft
  -> 用户查看差异和影响范围
  -> 用户确认明确 revision
  -> ProductFlow 原子物化
```

未确认的 Draft 不得修改正式 Workflow、图库组织或工作流关联。

### 4.3 Agent 请求执行

```text
用户明确授权范围
  -> AgentTask 记录目标和范围
  -> Agent 创建待确认 WorkflowRun request
  -> 用户在 Agent 工作台确认
  -> ProductFlow 复用现有 WorkflowRun application use case
  -> Task 投影运行状态
```

Agent 可以监控和解释 WorkflowRun，不能复制工作流执行器，也不能绕过取消、重试、队列和版本约束。

## 5. Session、Task 和页面上下文

- Session 负责长期交流；它不绑定某个页面或商品。
- 新建 Session 不要求用户先填写名称；创建时使用临时名称，首条全局 Turn 根据用户任务内容自动生成，人工重命名后不再被自动命名覆盖。
- Task 负责一个清晰目标，同一 Task 内的 Turn 默认串行。
- 不同 Task 可以并行，但并发额度和后台调度必须由 runtime/application 明确控制。
- 页面上下文只描述用户当时正在看的对象，例如 route、page type、选中资产、筛选器和 workflow revision。
- 页面上下文不决定 Task scope，也不授予权限。
- 执行 Draft、素材组织或 WorkflowRun request 前，后端必须重新读取当前对象归属、权限、revision 和可用状态。
- 发生 revision 冲突时，保留结构化 conflict，要求重新观察或重新生成 Draft；不能静默覆盖。

详细 Context schema、Pi 注入位置和 runtime recovery 规则见 `docs/specs/pi-agent-runtime-integration.md`。权威边界见 `CONTEXT.md`。

## 6. 当前实现和剩余方向

当前代码已经具备：

- AgentSession、Global Conversation、商品 Conversation 和 AgentTask 的关联。
- Global Agent Dock、Session/Task 列表、工作区跳转、Task 摘要和页面上下文快照。
- 商品工作流 Draft、全局素材整理 Draft、明确 WorkflowRun 的待确认请求。
- 用户直接运行 WorkflowRun 的完整工作流链路。

仍需单独验证或规划：

- ProductFlow 业务级 Task 调度器。
- 统一的副作用前 Fresh Observation runtime 抽象。
- Pi runtime 的交互式 Turn 兼容、后台 Task 恢复和效果对账。
- 更完整的受影响对象跳转和 Agent 请求重试体验。
- 画布会话归属商品、全局会话不进画布线程、创建不再插 onboarding Task：见 `docs/adr/0009-agent-canvas-sandbox.md` 与 `docs/specs/agent-canvas-sandbox.md`。落地前本文「Session 不绑定某个页面或商品」「同一个 AgentSession 可以包含多个 Conversation」仍描述当前代码。

这些事项的实现顺序和验收标准不写在本文，分别由路线图、0009 和 Pi runtime 规范负责。

## 7. 验收条件

- 用户不创建 Agent Session，也能完成工作流编辑、运行、取消、重试和结果查看。
- 同一 Session 的多个 Task 目标和上下文不会互相污染。
- 页面切换不会静默改写已有 Task。
- Agent 提议的 Workflow、图库和关联变更在确认前没有正式业务副作用。
- 用户直接运行和 Agent 请求执行最终使用同一个 WorkflowRun application use case、队列和 worker。
- WorkflowRun 状态、节点状态、错误和取消结果在工作流页面与 Agent 工作台一致。
- Agent 失败时，用户可以回到商品工作台、素材库或运行面板接管操作。

## 8. 明确排除

- 不把 Agent 变成唯一操作入口。
- 不把 `AgentSession`、`AgentTask`、`ImageSession` 和 `WorkflowRun` 合并。
- 不让页面上下文覆盖 Task 目标或后端 revision。
- 不让 Agent journal 代替 PostgreSQL 业务事实。
- 不在 Global Agent Dock 里重写工作流画布和运行链路。
