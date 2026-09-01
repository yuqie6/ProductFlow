# Agent 运行时开发时间线

从 2026-08-12 接入自研 harness，到 2026-09-01 收口 Turn 写入所有权。本文只记录已经发生的决策和代码路径，不替代 ADR、ARCHITECTURE 或验收账本。

怎么读：

- 活事实以 [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md) 和当前代码为准。
- ADR 正文冻结。被取代的条款在后继 ADR 的状态行标明，不把旧 ADR 改写成今天的实现。典型例子：ADR 0007 仍写 FastAPI 和 continuation Turn；活代码是 Go BFF + 同一 Turn 的 `answer` / `resume`。
- 生产可靠性的唯一验收指标是 [`docs/audits/agent-production-readiness.md`](../audits/agent-production-readiness.md)。职责迁移证据在 [`docs/audits/agent-runtime-ownership.md`](../audits/agent-runtime-ownership.md)。
- 日期取自 `codex/development` 上对应提交的 author date（+08:00）。

## 主线

工作流 Agent 在三周内换了四次运行时边界：

1. **草皮自建**：Go 进程 vendor `agent-harness`，FastAPI 做业务权威。
2. **切 Pi SDK**：Node 22 嵌入 `@earendil-works/pi-coding-agent` 0.83，harness 迁到 `exp`。
3. **通信与渲染**：浏览器始终打 Go；直播从「每 token 写 PG + 500ms 轮询」改成「进程内 `waitForEvents`」，再改成「全量 PG journal + UI 协议」。
4. **持久化与所有权**：本地 JSONL WAL 只做 ACK 前缓冲；PostgreSQL `agent_turn_events` 是浏览器事实源；丢失执行的终态只由 Go lease 扫描写。

贯穿始终的不变量：Skill 不是权限层；Pi session 不是业务权威；浏览器不直连 agent-service；无法证明的结果保持 `unknown`。

## 阶段总表

| 日期 | 阶段 | 关键提交 / ADR | 用户可见结果 |
|---|---|---|---|
| 2026-08-12 | 接入自研 Go harness | `92a18e31` | 工作流对话可以调独立 Agent 服务 |
| 2026-08-16 | harness 上长工具步骤投影 | `9e3ab74d`、ADR 0005 | 对话里能看到有界工具行，不是只有终答 |
| 2026-08-17～18 | 全局 Agent / Dock / Task | 多条 `feat: 增加全局…` | 全局会话、素材 Draft、跑图请求 |
| 2026-08-19 | 写下切 Pi 的边界 | `60586913`、ADR 0007 | 主线目标 runtime 定为 Pi |
| 2026-08-20 | 实现切 Pi，并立刻补耐久 | `47731cdf`、`6419fd83`、`e5f1923f` | 模型 loop 换成 Pi；lease / checkpoint 留下 |
| 2026-08-21～25 | 图与创建合同 | ADR 0008 / 0009 | live schema-v3 图；人主控画布；商品路径去掉 WorkflowDraft |
| 2026-08-29～31 | 业务后端迁 Go，Python 离开主线 | ADR 0011 / 0016 | Web / Pi 合同尽量不动；BFF 换成 Go |
| 2026-08-30 | 直播放到 agent-service | `e3dcbe88`、ADR 0013 | token 不再每条写 PG；Assembler 按帧渲染 |
| 2026-08-31 | 全量 journal 与 UI 协议 | `f363429d`、ADR 0017 | 浏览器只读已提交的 PG 事件 |
| 2026-08-31 | 本地 WAL 与 ACK | `6180fbf3` | 重启按 PG 确认前缀再续写 |
| 2026-08-31～09-01 | 单商家生产就绪合同 | 生产就绪账本 | `unknown`、effect 对账、容量门、Luna 真链 |
| 2026-09-01 | Turn 写入所有权 | ADR 0018、checkpoint-0～6 | Go 写终态 / journal / effect；Node 只跑 Pi loop |

## 1. 草皮自建：Go + vendored `agent-harness`（2026-08-12～19）

### 卡点

商品工作台需要多轮对话、工具、提问和草稿，但 FastAPI 业务后端不能同时长出一套通用 Agent loop。直接在 Python 里写模型循环，会把 provider 适配、会话压缩、工具调度和商品事务缠在一起。

当时对照的产品形态是 DeepSeek Harness 一类的对话壳：交错时间线、工具降噪、思考折叠。wire 却只投影终答、Question 和 WorkflowDraft，中间步骤看不见。

### 决策

- 独立进程 `agent-service`，语言是 Go。
- vendor `third_party/agent-harness` 快照，不把它当可随意改的内部包；SOURCE.md 声明 unmodified snapshot。
- FastAPI 继续拥有商品、Draft、确认和 WorkflowRun。Agent 只通过 internal HTTP 调业务 API。
- 浏览器只打 ProductFlow 业务 API，不直连 Agent 服务（ADR 0001）。

### 处理

`92a18e31`（2026-08-12）接入：`agent-service/cmd/productflow-agent-service`、`internal/app` 的 manager / server / tools、`internal/productflow` 客户端。harness 提供 Task、durable tool、HTTP 控制面和 journal。

`9e3ab74d`（2026-08-16）在 snapshot 上本地扩展 `ToolProjector`：durable journal 同步时投影有界 `tool.step`（`inspect_image` / `propose_draft` / `inspect_context` / `read_history` / `organize_assets`）。提交自己注明：原位改 vendored 快照，unmodified 声明需要后续刷新。这就是 ADR 0005「工具步骤投影」的第一版落地。

随后两天把全局 Dock、并行 Task、素材整理 Draft、工作流执行请求接到同一套 harness 合同上。

### 技术选型

| 选项 | 结论 | 理由 |
|---|---|---|
| FastAPI 内嵌 loop | 拒绝 | 业务事务和模型循环会一起膨胀 |
| 自研完整 coding agent | 拒绝 | 会话、压缩、provider 适配成本过高 |
| vendor harness + 薄 Go adapter | 采用 | 立刻有 Turn / journal / 提问 / 工具 HTTP |
| 浏览器直连 harness | 拒绝 | 还要再做一遍 session / ACL，工具副作用仍回 FastAPI |
| 把 filesystem 工具卡搬进工作台 | 拒绝 | ProductFlow 工具是看图、改图、提 Draft，不是 bash |

这一阶段的耐久语义来自 harness：本地 journal、Task、部分恢复。它覆盖了「能跑的对话」，没有覆盖 ProductFlow 的 lease、effect 对账和浏览器回放合同。

## 2. 切 Pi SDK（2026-08-19～20）

### 卡点

harness 已经承担通用 loop、Skill、模型适配、会话，又承担 ProductFlow 业务适配。继续扩自研底座，验证成本会叠在两类目标上：通用 runtime 和商品合同。原位改 vendor snapshot 也已经破坏「未修改快照」的前提。

### 决策（ADR 0007）

- `main` 的目标 runtime 是 Node.js 22 + Pi SDK ProductFlow adapter。
- `exp` 保留切换前的 Go Agent 与 `agent-harness`，研究后台 Task、崩溃恢复、效果重放。两条线不共享 journal / session / 调度实现。
- 主路径 **嵌入 SDK**（`createAgentSession`、隔离的 `DefaultResourceLoader`、custom tools、事件订阅）。Pi RPC 只作故障域隔离备选，不能把 Pi 原始 JSONL 暴露给浏览器。
- 默认关掉 Pi 的 `read` / `write` / `edit` / `bash` / `grep` / `find` / `ls`。只注册仓库审查过的 ProductFlow tools。
- Skills / Extensions 由仓库版本管理。用户输入不能动态写 Skill 文件。
- Pi session file 只服务模型 loop，不是商品、图、Draft、WorkflowRun 的权威。
- 不在 `main` 保留两个可自由切换的在线 runtime，也不做隐式 fallback。

明确排除：超长 system prompt 塞全部业务规则；把 Skill 当权限 / 事务层；Pi 直连 PostgreSQL / Redis / storage / provider。

### 处理

`47731cdf`（2026-08-20）一次性删掉 Go `agent-service` 与 `third_party/agent-harness`，换成 TypeScript：

- 依赖：`@earendil-works/pi-ai` 0.83.0、`@earendil-works/pi-coding-agent` 0.83.0。
- 入口：`src/main.ts` → `server.ts` → 当时的 `pi-runtime.ts`（单文件同时管 HTTP、Pi session、工具、store）。
- Skill 放在 `agent-service/.pi/skills/`（后来收口所有权，正文仍走受控 loader）。
- 对 FastAPI 的 HTTP / SSE 外部合同尽量保持，方便对照样本。

同一天立刻补耐久，而不是等 Pi session persistence「自动等于」业务恢复：

- `6419fd83`：PostgreSQL execution lease、owner / attempt / phase / fencing、semantic checkpoint。
- `e5f1923f`：durable execution；启动只重放尚未开始的 queued Turn；无法证明的 in-flight 标 `unknown`。
- `5bbe67b4`：问题回答落库后续接。当时合同仍是「答案写入后创建 continuation Turn，复用同一 Conversation 和 Pi session」——这条后来被删掉。

### 技术选型

| 选项 | 结论 | 理由 |
|---|---|---|
| 继续扩 Go harness | 拒绝 | 通用 loop 和商品规则会继续缠在一起 |
| Pi SDK 嵌入 | 采用 | 复用模型适配、会话、压缩、事件流 |
| Pi RPC 独立进程 | 备选未作默认 | 多一个故障域，但仍须翻译成 ProductFlow 事件；默认嵌入更简单 |
| 官方 OpenAI 执行器绕过 Pi | 拒绝 | 生产就绪 D-03 再次冻结：不增加旁路执行器 |
| 打开 Pi 默认 coding tools | 拒绝 | 没有内置权限系统，会碰到磁盘和进程 |
| 把 Pi session 当业务权威 | 拒绝 | session 证明不了 Graph Command、确认和多实例 claim |

Pi 切入后立刻暴露的语义差：Pi 的 session persistence ≠ harness 的 durable journal。后台 Task、崩溃后原地续跑模型请求、跨进程 effect 重放，全部单独列为验收项，没有因为「Pi 能存 session」就标完成。

## 3. 产品合同收口：图、画布、创建路径（2026-08-21～25）

Agent runtime 换底座的同时，业务对象也在换权威。

| 卡点 | 决策 | 处理 |
|---|---|---|
| Agent 提交完整 WorkflowDraft，和画布 live 图形成双拓扑 | ADR 0008：只有一份 canonical graph；写入走 Graph Command / ChangeSet | schema-v3 替换在线图；商品路径删除 Draft 确认闸门 |
| 「开始对话」写入 collecting Draft、onboarding Task，并自动开场 Turn；有 Task 时 `harness_run_id` 不一致 | ADR 0009：人是画布主控；创建写出生物是 live 图；Session 按商品 / 全局拆开 | `44e2a05d` 落地沙箱与 Turn 运行身份；`180c7c58` 商品路径退休 WorkflowDraft，intake 挂 Product |
| 对话挡住画布，Turn running 锁整张图 | 关闭或从未打开对话也必须能加节点、连线、运行 | 工作台手感不跟 Agent 切片捆绑；Goal 是显式 `AgentTask`，跑图结束不等于完成 |

选型细节：单步可逆改图立即 `apply`；多节点重构写成未应用的 `WorkflowGraphProposal`，画布预览后确认。Agent 与人走同一套 Graph Command，没有「从 Agent 收回控制权」的步骤。

## 4. 业务 BFF 换成 Go（2026-08-29～31）

### 卡点

Python FastAPI 仍是业务 API / worker / dispatcher。Agent 已经在 Node。继续让 FastAPI 当浏览器 BFF，等于三条运行时（Python、Go 实验线、Node）同时解释 Turn。

### 决策

- ADR 0011：业务后端按垂直切片迁 Go。封印线是当时 live Python 的 HTTP / SSE / queue 合同，不是 Python 目录布局。Web 与 Pi 默认零合同变更。
- 明确拒绝把 `exp` 上的 Go Agent / harness 当业务后端起点。那是 runtime 实验。
- ADR 0016：Python 树打到 `retired/python`，主线删除 `backend/`。仓库根 `contracts/` 留下 2026-08-29 HTTP 封印快照。

### 处理

`2411a5e0` 用 Go 实现 Agent 投影、SSE 与内部工具。`ac5b55a8` 把默认业务后端切到 Go。`b0d843de` 退休 Python。

这一步对 Agent 的意义：浏览器 cookie、Turn 归属、Graph Command、Question 落库、lease 的作者变成 `go/internal/agent`。agent-service 继续只被 internal token 调用。

## 5. 通信与渲染：三次改写直播路径（2026-08-30～31）

这是用户问的「通信渲染怎么处理」的核心。同一条浏览器 URL 换了三次实现。

### 5.1 第一次：每 token 写 PG + 500ms 轮询

Pi 切入后的默认形状：

```
Pi token
  → agent-service 本地 log
  → HTTP AppendEvent（lease + 一行 PG）
  → Go SSE 每 500ms ListEvents
  → 浏览器
```

卡点：直播固定多 500ms + 一次 HTTP/PG；高 token 率会打满 journal 写入；崩溃时 token 行也救不回正在飞的模型请求。

### 5.2 第二次：ADR 0013，直播留在 agent-service（2026-08-30）

卡点：第二份 live journal 多余。Turn 投影已有 `output_text` / `thinking_text` / `tool_steps`。进程崩溃时 in-flight 标 `unknown`，token 行救不回模型请求。

决策：

- 浏览器 URL 不变：`GET /api/v2/agent-conversations/:id/turns/:projection_id/events`。
- Go 验 session 后，把内部 `streamEvents`（agent-service `waitForEvents`）原样写成 EventSource。`sequence` 是 **runtime cursor**。
- `publishDurableEvent` 只收低频控制事件：`question.*`、`tool.step` 状态、`turn.*` 终态、lease checkpoint。`text.delta` / `thinking.delta` 不进 PostgreSQL。
- Pi `@earendil-works/pi-ai` 0.83 的 `assistantMessageEvent` 在 `pi-chunks.ts` 归一。未知 type 丢掉。`toolcall_*` 原始 arguments 不进 UI。
- 前端 `web/src/pages/workbench/agent/conversation/`：`ConversationAssembler` + `requestAnimationFrame` 发布 + keyed 节点。流式正文轻量渲染，settled 后再 GFM。

处理：`e3dcbe88`、`d35b787e`、`3cdc8c67`。提问续跑在这次提交里改为不再新开 Turn（ADR 0007 正文仍写 continuation，活代码已变）。

代价（后来被 0017 收回）：

- PG `agent_turn_events` 允许 sequence 空洞。
- 崩溃后流式正文无法从 PG 回放。
- 历史 Turn 只能按 `thinking_text → tool_steps → output_text` 折叠。
- 审批曾靠 `turn.succeeded` 再 remap 成 `awaiting_confirmation`。

排除方案：浏览器直连 agent-service；继续每 token 写 PG 再用 LISTEN/NOTIFY 优化轮询；Cordis / Slot / Typert。

### 5.3 第三次：ADR 0017，全量 journal + 稳定 UI 协议（2026-08-31）

卡点：0013 换来了低延迟，丢掉了断线回放和交错历史。本地文件若对浏览器可见，会形成第二事实源。

决策：

- 合帧后的 `text.chunk` / `thinking.chunk` **全部**按连续 `sequence` 写入 `agent_turn_events`。
- 浏览器 SSE **只读已经进入 PG 的事件**。删除 agent-service 本地 `/events`、`streamEvents`、Store waiter。
- Go `projectTurnEvent` 把 journal 译成稳定 UI 通知：`turn.started`、`item.*`、`approval.requested` / `approval.resolved`、`turn.completed | awaiting_confirmation | failed | canceled | unknown`。
- `turn/end(reason)` 由运行时原生发出，含 `awaiting_confirmation`。`ask_user`、workflow 确认、artifact 确认共用 approval 形状。
- 会话 SSE 只发内容。Dock / lease 走 `GET /api/v2/agent-control/events`。图运行走节点级 `run.event`。
- 浏览器发现 `sequence > cursor + 1` 时调用 `events/page?after=&limit=` 补洞。连接 generation 隔离旧 EventSource。buffer 上限 512 条或 1 MiB；连续三代失败才报协议错误。
- 终态 Turn 回放 PG 后发 `stream.complete`。超过保留期的 chunk payload 收成 `{"compacted":true}`，seq 保留。

处理：`f363429d`、`8c910d3d`。前端 `ConversationRuntime` 成为 Workbench 与全局 Dock 的单例，避免第二 EventSource。

### 渲染管线（当前）

| 层 | Owner | 做什么 |
|---|---|---|
| Pi runtime event | `pi-runtime.ts` / `pi-chunks.ts` | 归一成 ProductFlow chunk；合帧，不是每 token 一行 PG |
| 本地 WAL | `store.ts` | append-only JSONL + fsync；未 ACK 前缀私有 |
| 批量提交 | `journal-publisher.ts` | 20ms / 64 events / 768 KiB；tool / question / approval / message / terminal 是 drain 屏障 |
| PG journal | `go/internal/agent` `AppendEvents` | 同事务校验连续 seq、写事件、增量 fold 三列摘要 |
| UI 投影 | `sse.go` `projectTurnEvent` | journal kind → Turn / Item / approval 通知 |
| 浏览器 runtime | `conversation/runtime.ts` | generation、gap repair、去重、有界 buffer |
| 浏览器 assembler | `conversation/assembler.ts` | token / thinking 按 animation-frame 发布；结构事件立即发布 |
| 浏览器 reducer | `agentEventReducer.ts` | 按 seq 折进 thinking / text / tool item；未变化对象保持引用 |

对照 DeepSeek Harness 的取舍（ADR 0005）：布局、降噪、思考折叠、Send/Stop 可以学；不搬 terminal / read / search / web 工具卡；chip token（`/skill`、`@subagent`）没有语义对象时不引入。

## 6. 持久化：四层各管什么（2026-08-31～09-01）

当前不是「一个数据库存一切」，也不是「Pi session 就是恢复」。四层职责：

| 层 | 存什么 | 权威范围 | 不是什么 |
|---|---|---|---|
| Pi session 文件 | 模型 loop 消息、compaction、Skill 加载所需 runtime | 仅当前进程内的模型续跑 | 不能证明 Graph Command、确认、跨进程 claim |
| 本地 JSONL WAL | 已合帧、尚未被 PG ACK 的连续前缀 | 持有 lease 的 Node 进程私有缓冲 | 浏览器不可见；ACK 失败不得跳 seq |
| PostgreSQL `agent_turn_events` | 全量 Turn journal，连续 sequence | 浏览器 SSE、历史回放、列表 fold 的事实源 | 不是模型 transcript；压缩后 chunk 只留 seq |
| PostgreSQL 业务表 | Turn 投影、lease / fencing、`agent_model_invocations`、`agent_tool_mutations`、Question、Graph、Goal | 商品、图、副作用证明、终态 | worker 不得用 Node Turn 快照回写活状态 |

### WAL 与批量提交的选型

生产就绪账本「持久日志提交语义」冻结了两端都不能走的路：

- 每原始 token 单独 `await` PG：batch API 退化为单事件，打不过 25 Turn / 1 万事件、P95 ≤ 300ms。
- 本地返回后无界后台提交：PG 变成无限期 eventual sink，浏览器会看到未提交事件或跳号。

实际参数：`JOURNAL_BATCH_FLUSH_MS = 20`、`MAX_EVENTS = 64`、`MAX_BYTES = 768KiB`。普通 chunk 先入队；结构事件等待 drain。失败批次留在队首，原序重试（250ms 起、最高 30s 指数退避）或诚实终止。10k WAL P95 ≈ 1ms；PG batch P95 ≈ 220ms。

启动 handoff：先空 confirm 读 PG head / 权威终态，再 exact-confirm 本地前缀。内容 409 跳过 divergent 前缀并服从 `persisted_through`。PG-only terminal 以 PG projection 收敛本地状态。

### 恢复与副作用

| 卡点 | 决策 | 处理 |
|---|---|---|
| 进程被 kill -9，模型请求丢了 | 不续跑丢失的模型 loop；从已落盘 chunk 组装 interrupted message，Turn 为 `unknown` | `recovery.go`；`terminal_reason_code` 区分 provider / 中断 / effect / 持久化失败 |
| 工具可能已经改了图，回复没写完 | 先对账 mutation ledger；`applied` 自动收敛；`not_applied` 只对 `reconcile_then_retry` 用原幂等键重试一次 | 八工具四态矩阵；二次 `not_applied` 不再持久化该状态 |
| Node 和 Go 都解释 `recovery_policy` | 策略解释只在 Go | ADR 0018 S5：Node 只写 intent、发一次 mutation、5xx 查终态 |
| 提问后重启 | 同一 Turn 写答案，Pi `resume` 注入 `ask_user` tool result；不创建 continuation Turn | S3 删除 `ContinuationTurnID` 列和 UI orphan filter |
| waiting_input / awaiting_confirmation 被标 unknown | parked 状态跳过 lease-expiry unknown | scanner 显式跳过；新 claim 才继续 |

容量合同（D-05）：25 并发 Turn、100 SSE、单 Turn 1 万事件、单会话 1000 Turn。100 SSE 超限 503。7 天 chunk 压缩只在 canonical assistant message 已存在时进行。

## 7. 写入所有权收口（2026-09-01）

### 卡点

PG journal 已经是浏览器权威，但 Node 仍同时参与：Store publisher 回环、启动时自写 recovery `turn/end`、worker 读 Node Turn 快照更新活投影、TypeScript 第二份 `recovery_policy` 解释器。lease 竞争时会出现两个终态作者。

### 决策（ADR 0018）

Go 唯一拥有：Turn 投影与 `terminal_reason_code`、连续 journal、lease / fencing、mutation ledger、Question / approval / Graph Command 事务。

Node 只拥有：Pi session、Skill catalog、chunk 合帧、Tool HTTP、有界 WAL。

丢失的 in-flight 只由 Go lease 过期扫描写终态。Node 重启只 confirm / drain 可证明前缀。

### 处理（一刀一提交）

| 刀 | 做了什么 | Gate |
|---|---|---|
| S0 | 建账本、ADR 0018、ROADMAP 入口 | `just docs-check` |
| S1 | 删除 `store.setEventPublisher` 回环；TurnRuntime 显式读 WAL 连续前缀，receipt 匹配后 ACK | Agent 166 passed；WAL / PG 容量门 |
| S2 | Node 不再写 recovery `turn/end`；Go scanner 是唯一丢失执行终态作者 | 四点 SIGKILL：model start / mutation / approval / turn/end |
| S3 | 删除 `ContinuationTurnID` 及 OpenAPI / Web 残留 | migrate + go-test + 提问测试 |
| S4 | `AppendEvents` 同事务 fold 活状态；删除 `GetTurn` snapshot 读路径 | Go agent + Web SSE 测试 |
| S5 | live / manual / scanner 共用 Go `reconcileEffectIntent` | 八工具四态矩阵 |
| S6 | `pi-runtime.ts` 拆成 session adapter；manager / scope / turn-runtime / journal / tool-step-projection 分文件 | 全量 Go / Agent / Web / docs-check |

当前模块边界：

```
浏览器
  → Go HTTP / SSE（只读 PG）
  → runtime-manager（进程队列、并发、handoff）
  → turn-runtime（lease、journal 生命周期、提问 / 终态）
  → pi-runtime（单 Turn 的 Pi session / model / chunk / Skill / Tool）
  → tools.ts → Go Graph Command / intake / 确认 API
```

`pi-runtime.ts` 在 S6 之后不再是 God module。

## 当前数据流

```
Browser cookie
  → Go /api/v2/... 提交 Turn
  → PG AgentTurnProjection queued
  → Go worker StartTurn → agent-service
  → Node claim lease / fencing
  → Pi createAgentSession.prompt
  → chunk 合帧 → 本地 WAL
  → events/batch → PG agent_turn_events（连续 seq，同事务 fold 摘要）
  → Go SSE 从 PG 投影 item.* / approval.*
  → 工具副作用：checkpoint intent → mutation → Go reconciler
  → 丢失执行：lease 过期 → Go recovery 写 unknown / reason code
```

虚线：Pi session 文件只给下一轮模型调用；浏览器看不到。

## 反复出现的卡点模式

这三周里真正反复出现的不是「选哪个 SDK」，而是同一类边界：

1. **第二事实源**。本地文件、Turn 快照、Pi session、稀疏 PG 控制事件，只要对浏览器或恢复器可见，就会和权威 journal 打架。每次都收回到「浏览器只读已提交的 PG」。
2. **两个终态作者**。Node 启动恢复和 Go lease 扫描都想写 `turn/end`。最后只留 Go scanner；Node 只 confirm 前缀。
3. **把通用 loop 的能力误当成业务保证**。Pi session persistence 不能推出后台 Task、原地续跑模型、跨进程 `not_applied` 重试。这些仍在 ROADMAP，未宣告完成。
4. **投影过厚**。原始 tool arguments、完整 Skill 正文、图片 bytes、token 用量不能进工作台。工具步骤只有白名单字段。
5. **continuation 身份**。提问续跑一度新开 Turn，wire / DB / UI 留下 `ContinuationTurnID`。活语义改成 same-Turn 后，残留列会让恢复和 orphan 清理走错路，必须删掉而不是继续读。

## 明确不做（到 2026-09-01 仍成立）

- 后台 durable Task、模型请求原地续跑、跨进程 `not_applied` 自动重试。
- 用所有权重构宣告生产就绪，或把 G-07（干净 checkout 全量重跑）改成完成。
- 浏览器直连 agent-service，或读取本地 event file。
- 给 `ProductFlowClient` 加无行为包装层；把 Skill 当权限层；在 Node 写 Graph Command。
- 替换 Pi；增加绕过 Pi 的官方 OpenAI 执行器。
- 新对话壳、建议 chip、新 Tool 等用户可见扩面（ROADMAP「Agent 对话壳」仍单列）。

## 证据锚点

| 主题 | 代码 | 测试 / Gate |
|---|---|---|
| Pi 装配 | `agent-service/src/pi-runtime.ts`、`skills.ts`、`tools.ts` | `just agent-service-test` |
| WAL / batch | `store.ts`、`journal-publisher.ts`、`turn-runtime.ts` | `just agent-service-test-local-journal-capacity` |
| Journal / SSE | `go/internal/agent/sse.go`、`execution.go` | `just go-test-agent-journal-capacity`、SSE TTFB |
| 恢复 | `go/internal/agent/recovery.go`、`effect_reconcile.go` | `TestSIGKILLLeaseHolderAgainstGoPG`、八工具矩阵 |
| 浏览器 runtime | `web/src/pages/workbench/agent/conversation/runtime.ts` | Chromium `agent-conversation-runtime.spec.ts`、`agent-workbench-recovery.spec.ts` |
| 合同漂移 | `tool-manifest.ts` → `tool_manifest_generated.go` | `pnpm --dir agent-service check-contract-artifacts` |
