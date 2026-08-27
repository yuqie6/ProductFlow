# ADR 0009: Pi 沙箱 WebUI、画布生产物与会话归属

## 状态

Accepted；第 1～4 刀、商品路径 WorkflowDraft 退休，以及 Goal 托管环（显式 `AgentTask`，跑图结束不等于完成）已落地。当前事实见 `CONTEXT.md`、`docs/PRD.md`、`docs/ARCHITECTURE.md`。对话壳仍见 [`docs/specs/agent-canvas-sandbox.md`](../specs/agent-canvas-sandbox.md) 与 [`docs/ROADMAP.md`](../ROADMAP.md)。

Decision owner：ProductFlow repository owner。

## 背景

ProductFlow 接入 Pi，是为了在商品工作台里用对话操作**本产品内部**的对象：商品、图库、schema-v3 图、跑图请求。Pi 不是第二套工作流引擎。画布存在的原因是：流程可以保存、复用、按节点改、单独跑；如果只在 Agent 上下文里堆参考图直接生图，会撞上上下文膨胀、参考图过多和每张都走模型的成本。

当前实现与这个分工相反的部分：

- 「开始对话」写入 collecting `WorkflowDraft`、onboarding `AgentTask`，并自动提交开场 Turn。
- 同一 `AgentSession` 同时挂全局 Conversation 和商品 Conversation。
- 有 Task 时合同使用 Task 的 `harness_run_id`，事件入库仍按 Conversation 的 `harness_run_id` 校验，创建商品对话会在调模型之前变成 `unknown`。
- live graph 出现后合同翻转 `system_prompt` / `has_live_graph`，同一 Pi run 上会被当成换了 runtime scope。

ADR 0007 已决定 Pi 只跑 loop、业务写入走 ProductFlow Tool。ADR 0008 已决定图权威在 canonical graph，Agent 用 ChangeSet 协作，新商品应对空图或商品源节点提 ChangeSet。本 ADR 补上创建入口、会话归属和 Task/Goal 的产品边界，避免 Agent 侧继续膨胀。

## 决策

### 1. 人是画布主控（不可妥协）

工作台的完整生产能力属于人，不经过 Agent。关闭对话、从未打开对话、Turn 处于 running / unknown / failed，都必须能继续操作整张图：添加、连线、检查器、绑定、运行、取消、重试、撤销、分组、配方。

具体规则：

1. 对话、Task、GraphProposal、onboarding 文案都不是进画布、改配置或运行的闸门。有 live 图时不得再用「打开对话」挡住画布。
2. Agent 写入与人写入走同一套 Graph Command。Agent 刚 `apply` 完，人可以立刻改同一个节点，走同一条撤销栈，不必「从 Agent 收回控制权」。
3. 未应用的 GraphProposal 只约束提案触及的对象；不得锁整张画布。
4. 关闭对话面板后，工作台就是画布。对话是可关掉的叠加层，不是生产面的前置步骤。
5. 画布交互质量以 [`docs/specs/workbench.md`](../specs/workbench.md) 为准：命令靠近对象、失败写在对象上、结果立刻可再操作、1440 / 1024 / 390 都通。Agent 切片不得把这条降成「壳层以后再说」。Agent 侧栏收起、手机对话 sheet、建议 chip 裁切属于对话壳，可以另开，不能拿来推迟画布手感。

本条覆盖 ADR 0008 §14.3 与工作台规格「人是主控」。与本 ADR 其余条款冲突时，以本条为准。

### 2. 产品分工

| 层 | 职责 | 不负责 |
|---|---|---|
| Pi | 说话、选工具、一轮推理 | 磁盘、任意生图 API、业务事务、确认跑图 |
| Web（侧栏、全局 Dock） | 把 Pi 接到商品或全局作用域 | 第二份拓扑、第二套执行器、挡住画布 |
| schema-v3 画布 | 可保存、可复用、可分节点编辑和运行的生产物 | 依赖某条 Agent 对话存活 |

Agent 只能调用仓库审查过的 ProductFlow tools。默认 filesystem/process 工具保持关闭（ADR 0007）。

### 3. 两条创建路径

`/products/new` 仍是唯一创建入口。出生物是 live schema-v3 图，不是 collecting Draft。

**直接创建（表单齐：名称、图片类型、1～6 张参考图）**

- 写入 Product、参考图、可运行或可补全的 live 图。
- 不创建 `AgentSession` / `AgentConversation` / `AgentTask`。
- 工作台后挂画布会话：打开侧栏时没有该商品的画布会话再新建。

**开始对话**

- 写入 Product 和 live 图。名称-only 时图为一条 `product_source`（事实可空）；表单已齐时图与直接创建相同。
- 同时创建**归属该商品**的 `AgentSession` 与 product `AgentConversation`。
- 不创建 onboarding `AgentTask`。
- 不自动提交开场 Turn。用户第一句才 `start_turn`。
- 从第一轮起合同就是现图协作：`apply_graph_change_set`（一条可逆）或 `propose_graph_change_set`（多节点）。禁止 `propose_workflow_draft` 覆盖现图。

缺输入的节点可以存在，不能假装已经能跑。用户满意没有终态按钮：停下、去跑图、再开下一句都合法。Turn 成功表示这一轮问清或改对了，不是工作流完成。

### 4. 会话归属

创建时写死归属，不存在无归属 Session。

1. **画布会话**归属一个商品。画布侧栏只列、建、切这些会话。一个商品允许多条画布会话；每条自己的 Conversation 和 Pi run，不续另一条的 runtime 文件。
2. **全局会话**不归属商品。全局 Dock 不能切到画布会话；画布侧栏不能切到全局会话。
3. 全局可以谈某个商品、给工作台链接，或明确开一条**新的**画布会话。不能在全局 run 上 `apply_graph_change_set`。
4. 同一 `AgentSession` 不得同时作为全局线程和画布线程的容器。

词汇保持拆开：Session 是用户看见的线程；Conversation 是作用域绑定；Turn 是一轮；Task 见下一节。

### 5. Task 是可选 Goal，不是开聊入场券

`AgentTask` 对应 Codex / Grok Build 里「要办成的事」：目标文本 + 完成标准，有自己的状态和 task-specific Pi run。它不是聊天记录，也不是图。

- 一问一答：`AgentSession` + `AgentConversation` + `AgentTurn` 即可。开聊不要求 Task。
- Goal：图已经能跑之后，用户**明确立目标**才创建 Task。循环是跑图 → 看结果 → 改画布 → 再跑，停在验收条件、用户暂停或轮次/预算上限。
- Goal 状态宜薄：`active` / `paused` / `complete` / `cleared`。`WorkflowGraphRun` 继续只描述这一次跑图。
- page context 仍只描述这一轮看见什么，不改写 Task goal。
- 不把社区 Pi `/goal` 插件装进 `agent-service`。权威在 PostgreSQL。main 不承诺进程外自己续跑（ADR 0007）。自动确认跑图是以后单独的产品权限，默认仍要画布确认。

创建商品不得再插入 onboarding Task。

### 6. 每轮读当前图

第一句把当前图画进动态 context。之后每一轮仍读取当前 graph revision 与有界 page context。画布上的用户编辑和上一轮 `apply` 必须被下一轮看见。禁止只注入一次、后轮只靠 transcript。

### 7. Turn 的 runtime run

有 Task 的 Turn 使用 Task 的 `harness_run_id`；否则使用 Conversation 的 `harness_run_id`。规则已经写在 `application/agent/turn_projection.py` 的 `expected_harness_run_id`。事件入库、恢复自写事件、Turn 投影和前端 SSE 必须用同一条规则。不得把 Conversation 的 `harness_run_id` 改成等于 Task id：一个 Conversation 以后可以挂多条 Task。

Pi `sameRuntimeScope` 的 run 身份只包括 conversation / task / run / scope_type 与商品、draft id。`system_prompt` 和 `has_live_graph` 是合同刷新，不是换 run。

## 后果

- 创建对话不再依赖 collecting Draft 和自动 Turn，开场成本下降，合同不再中途翻转。
- 人可以随时离开 Agent，画布手感与从未打开对话相同。Agent 故障不得把工作台变成等待 Agent 的状态。
- Task 变薄之后，创建路径不再触发 Task run 与 Conversation run 的身份分叉；分叉仍须在事件层修掉，因为以后 Goal Turn 仍会绑 Task。
- WorkflowDraft 作为「完整第二份拓扑」退出创建主路径。库整理 Draft、GraphProposal 确认仍然存在。
- ADR 0001 的「Agent 只产出 WorkflowDraft、确认后物化 DAG」对**创建路径**由本 ADR 与 0008 取代；对无现图的历史 Draft 确认仍有效，直到 Draft 拓扑收敛完成。

## 排除方案

- 把 Agent 创建做成必须先交齐类型和参考图的第二张表单。
- 取消 Task 独立 `harness_run_id`，让所有 Turn 共用 Conversation run。
- 事件上报填 Conversation run、本地文件按 Task run 存。
- 安装未审查的社区 Pi goal 插件作为业务权威。
- 让全局 Dock 和画布侧栏共用一份 Session 列表。
- 发明无归属 Session。
- 用 Goal 状态机接管 `WorkflowGraphRun`。
- 把侧栏收起、手机对话 sheet、chip 裁切塞进本决策的实现刀。
- 让 Turn 状态、确认 Draft 或打开对话成为编辑/运行画布的前置条件。
- 因为要做 Agent 沙箱，把工作台连续动作（复制、拖线、失败写在对象上、390 底抽屉）标成后续项。

## 相关决策

- ADR 0001：无现图时的 Draft 确认边界仍有效；创建路径改为现图出生。
- ADR 0005：工作台 UI。画布占主导区域；对话是可关掉的一层。Agent 对话壳的响应式不在本 ADR 实现刀内。工作台手感以 `docs/specs/workbench.md` 为准。
- ADR 0007：Pi 仍是 loop；本 ADR 明确 ProductFlow 是该 loop 的沙箱 WebUI。
- ADR 0008：图权威、ChangeSet、空图合法。本 ADR 落实其 §11 的创建入口，并加上会话归属与可选 Goal。
- `CONTEXT.md`：Session / Task / WorkflowRun 不得合并。本 ADR 收紧「Session 不绑定商品」：画布 Session 绑定商品，全局 Session 不绑定。
