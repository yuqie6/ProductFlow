# Agent 画布沙箱：创建路径、会话归属与 Turn 身份

## 1. 状态

- 文档状态：Draft
- 目标合同：[`docs/adr/0009-agent-canvas-sandbox.md`](../adr/0009-agent-canvas-sandbox.md)
- 已接受图权威：[`docs/adr/0008-free-canvas-agent-graph-authority.md`](../adr/0008-free-canvas-agent-graph-authority.md)
- 当前实现以代码为准：`CONTEXT.md`、`docs/PRD.md`、`docs/ARCHITECTURE.md`、`docs/USER_GUIDE.md` 在落地前不改写成目标行为
- Agent 对话壳（回访侧栏收起、手机对话 sheet、chip 裁切、首屏 spinner）不在本文。画布手感以 [`docs/specs/workbench.md`](workbench.md) 为准，不可往后推。

## 2. 相对当前产品多出来的东西

不可妥协：人离开 Agent 也必须能无感操作整张画布，交互质量与「从未打开对话」相同。对话、Task、Turn 状态都不是进画布的闸门。质量条见工作台规格：命令靠近对象、失败写在对象上、结果立刻可再操作。

用户目标（Agent 侧）：在一问一答里让 Agent 一点一点往画布上补节点、边和配置，直到满意。画布是生产物；Pi 是沙箱里的聊天。Agent 加速同一条 Graph Command，不挡住画布。

当前代码与目标的差：

| 当前 | 目标 |
|---|---|
| 「开始对话」写 collecting Draft + onboarding Task，并自动发开场 Turn | Product + live 图 + 画布会话；无 Task；用户第一句才 Turn |
| 直接创建无会话；工作台 `attach_agent_workspace_to_product` 要求已有 live 图 | 直接创建仍无会话；打开侧栏再新建画布会话 |
| 同一 Session 挂全局 Conversation 和商品 Conversation | 画布会话归属商品；全局 Dock 不能切画布会话，反之亦然 |
| 现图出现后合同才切到协作 prompt | 创建即现图，第一轮就是 `apply` / `propose` |
| 事件入库用 Conversation `harness_run_id` | 一律 `_expected_harness_run_id` |

本文不管：工作室镜头主区、官方配方、SaaS、把社区 Pi goal 插件装进 `agent-service`。

## 3. 当前实现锚点

创建：

- `application/agent/product_workspaces.py` `_stage_workspace_records`：collecting `WorkflowDraft`、product Conversation、可选 onboarding Task、必要时新建 Session，并给该 Session 补一条全局 Conversation。
- 前端 `web/src/pages/workbench/agent/useAgentConversation.ts` `INITIAL_AGENT_TURN_TEXT`：进入工作台即 `start_turn`，并带 `task_id`。
- 空图已存在：`create_empty_workflow_graph`（`graph_commands.py`，`test_graph_empty_create.py`）。有 live 图后挂会话：`attach_agent_workspace_to_product`（不建 onboarding Task）。

Turn 身份（已复现缺陷）：

- Task 合同覆盖 `harness_run_id`：`application/agent/tools.py` `get_agent_task_contract`。
- 状态同步认 Task run：`control.py` `_expected_harness_run_id`。
- 事件入库认 Conversation run：`execution.py` `append_agent_turn_event` 约 169 行；恢复自写事件约 832、896 行。
- 商品对话 SSE 用 `conversation.harness_run_id`：`AgentConversationPanel.tsx`。
- 绑 Task 的 Turn 在 claim 成功后、调模型前 409「Agent event run ID 与 conversation 不匹配」→ Agent-service `persistenceError` → `finishTurn("unknown")`。`started_at` 为空。UI 文案变成「Agent 执行状态不明确，请稍后重试」。无 Task 的单测绿（例如 `task_id=None` 的事件幂等测试）。

合同翻转：

- live graph persist 后 `has_live_graph` 与 system prompt 变化。Agent-service `sameRuntimeScope` 几乎整份 Scope JSON 相等（只排除 `current_draft_version`）。创建即现图后创建链路上会轻很多；确认改图后的长会话仍会打到。

## 4. 目标用户路径

### 4.1 直接创建

```text
表单齐（名称、类型、1～6 张参考图）
  -> Product + 参考图 + live schema-v3 图
  -> 不建 Session / Conversation / Task
  -> 工作台；打开画布侧栏时若无该商品会话则新建
```

图模板与现在的跳过 Agent 创建相同。

### 4.2 开始对话

```text
名称必填；类型和参考图可选
  -> Product + live 图
       名称-only：一条 product_source（事实可空）
       表单已齐：与 4.1 同一张图
  -> 归属该商品的 Session + product Conversation
  -> 不建 onboarding Task，不自动 Turn
  -> 用户第一句 start_turn；每轮读当前 graph revision + 有界 page context
  -> apply 一条 或 propose 多节点；画布确认提案
```

全局 Dock 创建商品工作区：仍只开**新的画布会话**，不把全局 transcript 续进画布，不合并 run。

### 4.3 以后的 Goal（不在第 1～4 刀）

用户在已能跑的图上明确立目标。Task 文本 = 目标 + 完成标准。循环使用已有工具：`request_workflow_run` → 等结果 → inspect → `apply`/`propose` → 再请求跑图。状态：`active` / `paused` / `complete` / `cleared`。续跑 opt-in；完成用图上证据，不让干活模型自己喊 complete。main 不承诺进程外自己跑。

## 5. 对象与身份规则

| 对象 | 目标职责 |
|---|---|
| 画布 Session | 一个商品上的一条聊天线程 |
| 全局 Session | 不归属商品的线程 |
| Conversation | 该线程的作用域绑定；画布里 Session ↔ 一条 product conversation |
| Turn | 用户一句或 Goal 续的一轮 |
| Task | 可选 Goal；独立 `harness_run_id` |
| WorkflowGraph | 生产物 |
| WorkflowGraphRun | 这一次跑图 |
| GraphProposal | 多节点改图的待确认层 |

Turn runtime run：`projection.task.harness_run_id if task else conversation.harness_run_id`。Turn 投影对外给出该字段（建议 wire 名 `harness_run_id`，语义是这条 Turn 的事件 run）。SSE 用它。Conversation 行上的 `harness_run_id` 保持等于 conversation id。

## 6. 落地切片

切片必须可独立验证。第 1 刀是缺陷修复，即使创建路径尚未改，绑 Task 的 Turn 也不能立刻 unknown。UI 壳层另开，不塞进这些刀。Turn 重试若仍在 stash，在第 1 刀之后单独提交，不和创建改动搅在一起。

### 第 1 刀：Turn run 身份

**用户可见：** 绑了 Task 的 Turn 能调到模型。创建路径在改之前若仍自动开场，至少不再 200ms `unknown`。

**改：**

- `append_agent_turn_event` 与恢复自写 `AgentTurnEvent.run_id` 改用 `_expected_harness_run_id`。query 要能读到 `projection.task` 与 `conversation`。
- `AgentTurnResponse` 增加这条 Turn 的 runtime `harness_run_id`。
- 商品对话 SSE 用 Turn 的 run，不用 `conversation.harness_run_id`。全局 Dock 用同一字段，去掉 `runId: null` 绕过。

**测试：**

- 绑 Task：收 Task run，拒 Conversation run。
- 无 Task：收 Conversation run，拒 Task run。
- 保留现有 `task_id=None` 事件幂等测试。
- 前端：SSE 解析使用 Turn 投影上的 run。

**不做：** 改 Conversation.`harness_run_id` 的赋值；改 Task 合同；改创建入口。不得因为 Turn `unknown` 锁画布。

**证据：** `just backend-test` 覆盖 `test_workflow_agent_service.py` 新增用例；前端相关测试。创建商品对话在 `just dev` 下开场 Turn 的 `status` 不再因该 409 变成 unknown（若自动 Turn 仍在，应变为真正的模型执行或后续业务错误，而不是 run mismatch）。

### 第 2 刀：创建即现图，去掉 onboarding Task 和自动 Turn

**用户可见：** 「开始对话」进工作台时画布已在（名称-only 为商品源节点）。右侧对话空着等用户第一句。没有「请读取当前商品…」自动气泡。直接创建仍无会话。

**改：**

- `create_agent_product_draft_workspace` 在同一事务写 live 图：名称-only 调用现有空图出生再 `apply` 一条 `product_source`，或直接组一条含 `product_source` 的出生图。表单已齐时复用直接创建的图模板。
- 不再 `create_onboarding_task=True`。不再为创建插 `WAITING_USER` / `product_onboarding_intake`。
- 创建事务可以不再写 collecting `WorkflowDraft` 作为出生图的前置。若暂时保留 Draft 行，也不得再作为 Agent 提交完整拓扑的入口。
- 删除或停用 `INITIAL_AGENT_TURN_TEXT` 自动提交。
- 合同从第一轮走 live-graph prompt / Skill；不加载 `workflow-draft`。`propose_workflow_draft` 在已有 live 图时保持拒绝。
- 工作台有 live 图时走画布，不走「还没有图」的 onboarding hero 主路径。「打开对话」不得挡住添加、连线、检查器、运行。
- 关闭对话面板后，添加 / 详情 / 运行 / 图库 / 配方保持可点。Turn running 或 unknown 不得禁用画布命令。

**测试：**

- `test_agent_product_workspaces.py`：创建结果有 active graph、无 onboarding Task、无自动 Turn。
- 名称-only：图含 `product_source`，无生图节点亦可。
- 表单齐 + 开始对话：图与直接创建模板一致，且有 product conversation。
- 直接创建：无 conversation。
- 前端：进入工作台不发 initial turn。
- Skill/合同：现图路径不带完整 Draft schema。

**不做：** 会话列表隔离（第 3 刀）；Goal；Agent 对话壳视觉。不得把工作台连续动作（复制、拖线、对象上失败、390 底抽屉）标进本刀的「以后」。

### 第 3 刀：会话归属

**用户可见：** 画布侧栏只出现该商品的会话。全局 Dock 只出现全局会话。打开侧栏没有画布会话则新建一条。

**改：**

- Session 创建时写死归属（商品或全局）。列表 API 按归属过滤。
- `_stage_workspace_records` 不再给画布 Session 补全局 Conversation。
- 全局创建商品工作区：新建画布 Session，或在用户确认后跳进该商品的画布会话，不把全局 Session 复用为画布容器。
- 一个商品允许多条画布 Session。

**测试：** 列表过滤；禁止跨归属切换的 API 4xx；全局 `apply_graph_change_set` 拒商品图。

**不做：** Goal 续跑；手机侧栏交互。

### 第 4 刀：一轮只补一块 + runtime scope

**用户可见：** Agent 一次加一个节点或提一份可预览提案，不会一轮生成整张图。同一画布会话在出图后继续聊，不再 409 `conflict`。

**改：**

- Skill / live-graph prompt：禁止「一轮生成整张图」；单次可逆 `apply`，多节点 `propose`。
- Agent-service `sameRuntimeScope`：比较 conversation_id / task_id / run_id / scope_type / 商品与 draft id。`system_prompt`、`has_live_graph` 不参与 run 身份。
- 冲突码保持可区分：身份冲突 vs 合同刷新 vs 幂等冲突，避免再洗成笼统 unknown。

**测试：** persist 后同一 harness_run_id 再开一轮不 409；prompt 变化不换 run。Skill 场景测试：名称-only 第一轮只加参考或问缺口，不提交完整 Draft。

### 第 5 刀（以后）：Goal 托管环

单独规格。需要：显式开始、四态、opt-in 续跑、完成证据、仍走画布确认跑图。不把 `@narumitw/pi-goal` 等 CLI 包装进生产 adapter。不在第 1～4 刀做。

## 7. 验收

第 1～4 刀之后，在 `just dev` 真实浏览器走：

0. 直接创建或开始对话进入工作台后：**关掉对话**（或从未打开），完成添加节点、连线、改检查器、运行一个节点。Turn 即使 unknown，这些命令仍可用。Agent `apply` 之后人立刻改同一节点并可撤销。
1. 直接创建 → 工作台有图、无自动对话；打开侧栏才有会话。
2. 名称-only 开始对话 → 有 `product_source`、无自动 Turn；发一句能进模型（非立刻 unknown）。
3. 对话中上传参考图、说明图种 → Agent 用 ChangeSet 往现图上补，画布可见。
4. 点重试（若已接入）不叠重复用户气泡；unknown 不再由 run mismatch 造成。
5. 全局 Dock 看不到该画布会话；画布侧栏看不到全局会话。

`just web-e2e-live-graph` 仍覆盖跳过 Agent 的直接创建。对话创建补一条 focused 浏览器或 API 回归，不把质量样本（追问数量、视觉一致性）当作本规格的完成条件。

## 8. 明确排除

- 不把 Agent 创建改回「必须先填完表单」。
- 不把 Conversation.`harness_run_id` 改成 Task id。
- 不安装未审查 Pi extension。
- 不让 Goal 合并 Session / Task / WorkflowGraphRun。
- 不在本规格修 Agent 对话壳：手机对话 sheet、chip、回访展开、登录 spinner。画布连续动作仍按 `specs/workbench.md` 验收，不因本规格延期。
- 不让 Turn 状态或「打开对话」成为编辑/运行的前置条件。
- 不把「状态不明确」文案治理当作第 1 刀完成条件；第 1 刀修身份后该类 409 应不再发生。若仍要拆 persistence-unknown 与合同拒绝，另开。
