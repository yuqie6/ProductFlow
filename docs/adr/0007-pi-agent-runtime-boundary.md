# ADR 0007: Pi Agent 运行底座与 ProductFlow 业务边界

## 状态

Accepted; main interactive Pi adapter and startup recovery guard implemented, live rollout gates pending

Decision owner：ProductFlow repository owner。本文记录主线运行底座的选择和边界；当前 `main` checkout 使用 Node.js 22 + Pi SDK ProductFlow adapter。真实 provider、PostgreSQL/Redis、浏览器和长时间恢复验收仍需单独完成，不能把未验证能力描述成已支持。

## 背景

当前 `agent-service/` 使用 Node.js 22 + Pi SDK ProductFlow adapter 处理 Agent Turn、工具调用、问题、artifact、SSE、session 和事件文件。ProductFlow 后端通过 internal HTTP/SSE 调用它，前端只消费 ProductFlow 的 Web projection。

现有底座已经覆盖了不少 durable、恢复、Task 和工具投影语义，但它仍然承担了通用 Agent loop、Skill 读取、模型适配、会话处理和 ProductFlow 业务适配。后续继续扩大自研底座，会让通用运行时和 ProductFlow 业务规则同时增长，验证成本也会继续叠加。

主线采用 Pi 作为通用 Agent 运行底座。ProductFlow 通过 Skills、动态 Context 和受控 Tools 适配自己的商品、素材库、WorkflowDraft 和工作流执行合同。现有自研 harness 移到 `exp` 分支，继续承担 durable runtime、后台任务和故障恢复方向的研究。

Pi 当前官方提供 SDK、RPC、Skills、Extensions、会话管理、事件流和上下文压缩能力。Pi 默认工具会接触文件系统和进程，且 Pi 不提供内置权限系统，因此 ProductFlow 不能直接使用默认 coding-agent 配置作为线上业务 Agent。

## 决策

### 1. 主线和实验线

- `main` 的目标运行时是基于 Pi SDK 的 ProductFlow Agent adapter。`agent-service` 对 FastAPI 的 HTTP/SSE 外部合同继续作为稳定边界；实现语言或内部包可以替换。
- `exp` 保留主线切换前的 Go Agent service、`third_party/agent-harness` snapshot 和相关 durable runtime，研究后台 Task、崩溃恢复、效果重放、调度和更深的 ProductFlow 定制能力。
- 两条线共享 ProductFlow 的业务 API、Draft schema、Context schema、Tool schema、事件 projection 和验收样本。两条线不共享各自的 runtime journal、session storage 或调度实现。
- 运行时切换不能作为 ProductFlow 后端的隐式 fallback。每个部署必须明确使用一个 runtime，并在 health/status 中报告 runtime 名称和版本。

### 2. 各层职责

| 层 | 唯一职责 | 不负责的内容 |
|---|---|---|
| Pi | Agent loop、模型调用、会话消息、工具选择、上下文压缩、运行时事件 | ProductFlow 权限、业务事务、Draft 确认、WorkflowRun 状态 |
| Skill | 业务知识、流程顺序、何时读取、何时追问、何时提交 Draft、何时停止 | 权限、版本校验、幂等、数据库写入、真实副作用 |
| Context | 当前 Session/Task/页面/商品/工作流的有界事实和目标 | 授权、完整数据库快照、媒体 bytes、最终事实裁决 |
| ProductFlow Tool | 向 Agent 暴露一个有界的业务能力或提案入口 | 直接访问数据库、绕过 application use case、替用户确认 |
| FastAPI application/domain | scope、权限、当前事实、revision、schema、幂等、事务、队列和业务结果 | Agent 的自然语言推理和对话历史 |
| Web | 对话、问题、Draft、确认、工具步骤和 WorkflowRun 的用户投影 | 重建 transcript、执行未确认副作用 |

核心规则是：Skill 教 Agent 怎么做，Context 告诉 Agent 当前是什么情况，Tool 让 Agent 请求一次业务动作，ProductFlow 决定这次动作是否有效并承担副作用。

### 3. Pi 接入方式

- 主路径使用 Pi SDK 直接嵌入 ProductFlow Agent service。SDK 的 `createAgentSession`、隔离的 `DefaultResourceLoader`、custom tools 和事件订阅负责运行时组合；ProductFlow Skill metadata 由 adapter 管理，正文通过受控 loader 按需进入 Pi Turn。
- Pi RPC 只作为进程隔离、故障域隔离或独立升级的备选方案。不能因为使用 RPC 就把 Pi 的原始 JSONL 直接暴露给浏览器。
- ProductFlow runtime 默认关闭 Pi 的 `read`、`write`、`edit`、`bash`、`grep`、`find`、`ls` 等 filesystem/process 工具，只注册 ProductFlow 明确允许的 custom tools。
- Skills 和 Extensions 由仓库版本管理并经过代码审查。用户输入不能动态写入 Skill 文件、Extension 文件或运行时配置。
- Pi 的 session file 只记录 Agent 运行时对话历史和恢复所需的 runtime 数据。它不能成为商品、资产、Draft、WorkflowRun 或确认状态的业务权威。

### 4. ProductFlow 的副作用边界

- Agent 只能读取有界事实、提交待审阅 Draft，或提交一个待确认的 WorkflowRun request。
- `confirm_workflow_draft`、`confirm_library_organization` 和实际 WorkflowRun materialization 由用户 UI/API 触发，不注册为可由 Agent 自主调用的确认工具。
- 所有副作用通过 FastAPI application use case 完成。Pi Extension 和 Tool adapter 不直接访问 PostgreSQL、Redis、storage 或 provider。
- 需要修改素材、工作流或工作流关联时，工具必须携带 scope、expected revision、canonical payload 和 idempotency key。后端重新读取当前事实，冲突时拒绝整次操作或要求重新生成 Draft。
- Tool 名称、参数和返回值使用版本化 JSON Schema。Schema 变化必须同步 TypeScript adapter、FastAPI client、Web projection、exp adapter 和 contract tests。

### 5. 当前功能与目标功能的关系

当前 `docs/ARCHITECTURE.md` 描述的 Node.js + Pi Agent service 是 main 的 live truth。ProductFlow 的业务权威边界、WorkflowDraft 确认流程、WorkflowRun 执行器、素材身份和 Session/Task 产品语义保持不变；变化集中在 Agent loop、Skill loading、runtime session 和事件翻译层。旧 Go runtime 只在 `exp` 分支保留。

长期后台 Task、进程崩溃后的模型 Turn 原地恢复和工具效果重放不因为 Pi 有 session persistence 就自动成立。当前实现允许浏览器断开后继续当前进程内的 Turn；浏览器事件由 ProductFlow PostgreSQL `agent_turn_events` 按 cursor 提供，问题答案写入 ProductFlow 后会创建新的 continuation Turn，复用同一 Conversation 和 Pi session。Agent service 在 PostgreSQL execution lease 中记录 owner、attempt、phase 和 fencing token，并以 semantic checkpoint 记录模型、副作用、问题、外部任务和终态边界。启动时只重新入队尚未开始的 queued Turn；如果安全 queued Turn 的旧 Agent 实例已经丢失本地 state，ProductFlow recovery scanner 可以要求新实例使用原 harness Turn ID handoff，Agent service 必须返回同一 ID。无法证明执行结果的 in-flight Turn 结束为 unknown。业务副作用对账、后台调度、完整多实例竞争验收和进程崩溃后的原地模型恢复仍单独列为验收项；在证明之前，Pi runtime 只承诺已经验证的交互式 Turn 和短任务能力。

## 后果

### 正面后果

- 通用 Agent loop、模型 provider 适配、Skill discovery、会话和上下文压缩可以复用 Pi 的成熟实现。
- ProductFlow 的业务规则集中在后端 application/domain 和版本化 Tools 中，Agent runtime 更换不会重新定义业务状态。
- `main` 可以围绕 ProductFlow 的实际用户结果迭代，`exp` 可以独立研究 durable runtime，不让两类目标互相拖慢。
- 通过固定的 HTTP/SSE、Tool、Context 和 Draft 合同，可以用相同样本比较 Pi 和自研 harness 的质量。

### 代价和风险

- 需要实现 Pi 到现有 Agent service wire contract 的事件、Question、artifact、取消和错误翻译。
- Pi 的 session persistence 与当前 harness 的 durable journal 语义不同，后台任务和故障恢复需要额外设计和测试。
- Pi 默认能力包含操作系统工具；若 allowlist 或 sandbox 配置错误，Agent 可能获得超出 ProductFlow 业务范围的权限。
- Skills 质量会直接影响 Agent 的工具选择和追问路径，需要版本化、场景测试和运行时观测。

## 明确排除

- 不把 ProductFlow 的业务规则全部塞进一段超长 system prompt。
- 不把 Skill 当作权限控制、事务控制或副作用保护机制。
- 不让 Pi 直接连接 ProductFlow 数据库、对象存储、Redis 或 provider。
- 不为了迁移 Pi 重写工作流画布、WorkflowRun worker、媒体 owner 或 Draft materialization。
- 不在 `main` 同时保留两个可自由切换的在线 Agent runtime，并用 fallback 掩盖两套语义没有对齐的问题。
- 不把 Pi 的 session file、transcript 或 tool result 当作 ProductFlow 业务事实的第二份 owner。

## 参考

- [Pi repository](https://github.com/earendil-works/pi)
- [Pi SDK documentation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/sdk.md)
- [Pi RPC mode](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/rpc.md)
- [Pi Extensions](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/extensions.md)
- `docs/adr/0001-agent-draft-authority.md`
- `docs/adr/0005-agent-workbench-ui.md`
- `docs/adr/0006-media-library-authority.md`
