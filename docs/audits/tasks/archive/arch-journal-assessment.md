# 任务：核定 journal 发布与确认的职责收拢收益

状态：完成
类型：证据
认领者：主代理-0905-0439
认领于：2026-09-05T04:39:20+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：无。维护者裁定保留现状，不发布 journal 实现 issue。

本任务已关闭。认领与归档见 [Issue 协议](../README.md)，结论见[父账本](../../performance-governance.md)。用户可见对话行为未改。

任务合同以本文件为准；执行前读取适用仓库规则、当前实现、调用链、测试和 diff。

## 做成什么样

回答一个有界问题：让 journal module 吸收在线发布、回执校验、ACK 和重启前缀确认，能否实质减少 TurnRuntime 的协议知识与测试私有侵入，同时保持现有业务权威？

交付在本 issue 的证据节：当前职责/调用方清单、保留现状与最小收拢方案的比较、权限与失败矩阵、拟迁移测试和实现范围建议。明确选择“建议实施”或“保留现状”并说明证据。不得只按文件长度决定，也不得仅复述来源报告。

用户可见行为在本任务中保持不变：对话仍由 Go/PostgreSQL journal 投影，不改变重启恢复或事件发布能力。任务完成只表示调查证据足够，不表示重构已落地或可靠性门槛通过。

## 前置与并行

- 前置：安装锁定的 Agent service 依赖，Node 版本满足 `agent-service/package.json`；无需真实模型凭据或共享 dev DB。
- 冻结输入：每次测试固定所读的 `agent-service/src/` 和对应 Go journal 合同源码；记录 HEAD、相关路径 diff 与测试前后变化。并行 writer 改到输入时重跑受影响验证，不沿用旧结果。
- 运行资源：使用现有 fake HTTP/provider adapter、真实临时 WAL 目录和测试自行分配的本地端口；不得启动/重启共享 dev、暂停 worker、改 provider 配置或向共享数据库写业务数据。
- 可与画布任务并行调查；不得在画布 live 窗口改其代码或配置。共享章程、索引及 Git 写入由维护者串行整合。

## 只改这些文件

- 本文件：填写调查、设计比较、证据与交接。
- 父章程的 AR-02 结论由维护者验收后更新，执行者不直接修改。

如必须加测试/仪器化才能取证，先交回维护者扩展合同；本次发布不授权改 runtime 或测试实现。

## 不要碰

- `agent-service/`、`go/internal/agent/` 的实现、测试、wire schema、生成产物和持久化形状。
- lease 所有权、Go scanner 终态权限、模型请求恢复、Skill、grader、工具协议和能力。
- 画布 autosave、Graph Command、性能任务及其它业务组的活跃文件。
- 新建通用 journal/recovery 框架，或把接口草案写进稳定产品文档。

## 现在代码在哪

- `agent-service/src/turn-runtime.ts`：`appendJournalEvent` 写 WAL；`publishUnpublishedEvents` 入队；`appendPublishedBatch` 处理 5xx、回执与 ACK；`confirmPublishedPrefix` / `confirmMatchingUnpublishedPrefix` 负责恢复比对；`adoptConfirmedTerminal` 处理已确认终态。
- `agent-service/src/journal-publisher.ts`：batcher 的排队、批量、定时 flush、barrier 与 drain；`runtime-journal.ts`：wire 转换、回执匹配、重试分类和审批辅助函数。不要把审批编排顺带搬进发布 module。
- `agent-service/src/store.ts`：`unpublishedEvents` / `publishedThrough` / `markEventsPublished`；`productflow.ts`：`appendTurnEvents` / `confirmTurnEvents` 现有 HTTP adapter。
- `go/internal/agent/http_internal.go` -> `execution.go:AppendEvents` / `event_confirm.go:ConfirmEvents`：追加与确认的不同权限；确认只比对已提交事件，不取得执行权。
- `agent-service/src/pi-runtime.test.ts`：错误回执与 5xx 测试通过强转取得 runtime 并调用私有发布方法；另查该文件的恢复、分叉和 fencing 用例。
- `agent-service/src/journal-publisher.test.ts`、`store.test.ts`、`process-restart.e2e.test.ts`：队列、实际 WAL 与进程重启的已有测试入口。进程测试使用 fake 远端，不冒充真实 Go/PostgreSQL。

在线调用链：`TurnRuntime -> TurnStore WAL -> JournalEventBatcher -> appendPublishedBatch -> ProductFlowClient -> Go AppendEvents -> PG journal -> 回执校验 -> WAL ACK`。恢复从 `confirmPublishedPrefix` / `confirmMatchingUnpublishedPrefix` 进入 `confirmTurnEvents`，只比对 PG 已提交前缀后推进 ACK。候选来源与原复核基线保留在 [架构候选历史记录](../../../history/agent-runtime-timeline.md#architecture-assessment-history)，当前职责归父章程。

## 合同

- AR-02-A：PostgreSQL 是 journal 权威，ACK 只推进已证明前缀。错误回执不 ACK；重试原事件、原序列，不生成替代事件。
- AR-02-B：在线 append 受 execution/owner/lease 约束；confirmation 不 claim、不续租、不生成业务终态。方案必须分清“确认已存在”和“获准追加”。
- AR-02-C：矩阵至少覆盖错误回执、5xx、已提交但响应丢失、部分前缀缺失、内容分叉、旧 lease fencing、barrier drain，以及已有终态的确认。Node 重启不重放丢失的模型执行，Go scanner 保留丢失执行的终态裁定。
- AR-02-D：TurnRuntime 保留生命周期、等待问题/确认及终态编排。方案应列出哪些调用方必知状态可以删除、哪些仍必须保留；只搬文件或多一层转发不足以支持实施。
- 保留真实临时目录的 WAL 测试与进程重启回归，复用已有 HTTP adapter/fake；不得以更多文件操作 mock 替代持久化证据。

## 怎么验收

```bash
pnpm --dir agent-service exec vitest run src/journal-publisher.test.ts src/pi-runtime.test.ts src/store.test.ts src/process-restart.e2e.test.ts
just docs-check
```

完成条件：

1. 逐一对应当前函数、调用者、状态和副作用，画清在线 append 与重启 confirmation 两条链；标注真实缺陷、维护摩擦、尚未验证风险。
2. 给出至少“保留现状”和“最小职责收拢”两种比较，说明被删除的重复知识与保留的生命周期职责；接口草案仅保留在本任务中，不预先规定新类或方法数量。
3. 权限/失败矩阵每项对应已有测试名、有效执行结果，或明确的缺测及未来回归断言；不把 skipped、没跑或 fake HTTP 结果当作真实 Go gate。
4. 执行上面的无筛选测试，记录通过/失败/跳过数及基线。有效 FAIL 可以构成调查证据，但必须分析它对实施建议的限制；环境缺失或未执行不能关闭。
5. 维护者审核“建议实施/保留现状”的理由。建议实施时列出独占路径、依赖、必须删除的旧调用与测试入口、未来验证门；执行者停止等分配。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：调查完成。无归属本任务的未提交实现、运行进程或冻结资源。冻结输入路径在认领后未被其它 writer 修改。

## 证据

### 命令 / 日期 / 结果

2026-09-05（Asia/Shanghai）。Node v22.20.0，`agent-service/node_modules` 已按 lockfile 安装。工作树另有他人 `eval-go-loader`、首页展示和画布认领改动，未纳入本命令。

```bash
pnpm --dir agent-service exec vitest run src/journal-publisher.test.ts src/pi-runtime.test.ts src/store.test.ts src/process-restart.e2e.test.ts
```

4 files passed；Tests 61 passed / 1 skipped / 0 failed；Duration 8.74s。stdout 出现一次 `Agent durable handoff recovery failed ... ProductFlow request failed`，对应用例 `keeps listening after one leftover durable handoff fails to recover`（单条 leftover 500 不阻断其余 handoff），不是本命令失败。

跳过：`store.test.ts` 的 `it.runIf(PRODUCTFLOW_RUN_AGENT_LOCAL_JOURNAL_CAPACITY === "1")`「appends and reloads 10k durable WAL events without quadratic rewrite」。本 issue 要求的无筛选命令未设置该环境变量，因此跳过。它是本地 WAL 容量门，不是 AR-02 矩阵项；父章程 G-05 已有独立 `just agent-service-test-local-journal-capacity` 历史记录。跳过不构成 FAIL，也不用来证明容量。

```bash
just docs-check
```

Documentation contract check passed。

建组筛选基线（history 中 4 条 journal/pi-runtime 用例）不能替代本轮无筛选结果。本轮未跑 Go/PostgreSQL journal 测试；矩阵中凡依赖 Go 的项标为「代码存在、本 issue 未执行」。

### 基线 commit / 相关路径 diff / 测试输入变化

- 认领 commit：`324894a6`（`chore: 认领 tasks/arch-journal-assessment`）。
- 调查开始时 HEAD：`324894a6e8c4f914235db75ade2f1a66ad34ee60`。
- 冻结输入 `git diff --stat`（`turn-runtime.ts`、`journal-publisher.ts`、`runtime-journal.ts`、`store.ts`、`productflow.ts`、对应测试、`http_internal.go`、`execution.go`、`event_confirm.go`）：空。
- 调查期间另有 `0d3a1220 chore: 认领 tasks/canvas-inspector-midrun`。`324894a6..0d3a1220` 对上述冻结路径仍为空，未重跑。
- 测试前后上述路径无 diff。未改 runtime 或测试实现。

### 职责清单

两条链的当前 owner、调用方、状态和副作用如下。审批编排（`hasUnresolvedApproval` / `artifactFromPendingApproval`）留在恢复生命周期里，不计入 journal 发布 module。

**在线 append**

| 步骤 | 函数 / 入口 | 调用方 | 必知状态 | 副作用 |
|---|---|---|---|---|
| 写本地 WAL | `TurnStore.appendEvent` / `appendLocalEvent` | `TurnRuntime.appendJournalEvent`、`setJournalToolStep`、`writeJournalTerminal`；`runtime-manager` 的 cancel/resume 经 `appendJournalEvent` | run/turn 序列、payload 上限 | JSONL WAL + fsync；未 ACK 的事件仍 unpublished |
| 入队 | `TurnRuntime.publishUnpublishedEvents` → `publishDurableEvent` → `JournalEventBatcher.enqueue` | 每次 WAL 写入后；流式 chunk 经 `emitStreamChunk` | `executionLease.harness_turn_id` 必须等于事件 turn；无 lease 时 `turn/resume_requested` 与 `turn/cancel_requested` 直接返回，其它事件记 `persistenceError` | 内存队列；barrier kind 立即 `drain` |
| 分批 | `JournalEventBatcher` | 仅 TurnRuntime 构造 | 20ms / 64 / 768KiB；失败批次留在队首 | 调用 `appendPublishedBatch` |
| HTTP 追加 | `TurnRuntime.appendPublishedBatch` → `ProductFlowClient.appendTurnEvents` → `POST .../events/batch` | batcher callback | 必须有 lease；请求带 `owner_id` + `lease_token`；5xx 最多 8 次、250ms 起指数退避至 30s，原事件原序 | Go `AppendEvents`：`requireLease`、连续 seq、幂等 replay、`turn/end` 投影终态 |
| 回执 | `eventReceiptMatches`（`runtime-journal.ts`）在 `appendPublishedBatch` 内 | 同上 | sequence/kind/execution_id/projection_id/schema_version/ignorable | 不匹配则 `event_receipt_mismatch`，不 ACK |
| ACK | `TurnStore.markEventsPublished` | 回执全部匹配后 | ACK 只允许前进到已有 WAL seq | 独立 ACK 文件；unpublished 尾随缩短 |

**重启 confirmation**

| 步骤 | 函数 / 入口 | 调用方 | 必知状态 | 副作用 |
|---|---|---|---|---|
| 编排 | `TurnRuntime.recoverDurableHandoff` | `runtime-manager` 启动恢复 / 后台恢复 | 先 confirm，未完成才 `claimExecution` | 可能清 handoff、改 phase、或把 in-flight 留给 Go scanner |
| 空探针 | `confirmPublishedPrefix` 先 `confirmTurnEvents({events:[]})` | recover 入口 | `status=confirmed`、`confirmed_through=0`、`items=[]` | 无 lease、不续租 |
| 前缀比对 | 每次最多 250 条 unpublished → `confirmTurnEvents` | 同上 | `persisted_through` / `confirmed_through` 形状；回执必须匹配 | 匹配前缀才 `markEventsPublished` |
| 内容 409 | `confirmMatchingUnpublishedPrefix` 逐条 confirm | batch confirm 返回 `event_sequence_conflict` | 最长匹配前缀 ACK；冲突则放弃本地后缀 | 调用 `abandonUnpublishedJournal`（本地 unknown，不覆盖 PG） |
| 已有终态 | `adoptConfirmedTerminal` | 探针或确认带 `terminal` | 校验 `turn/end` 身份后 `reconcileConfirmedTerminal` 或 `adoptAuthoritativeTerminal` | 收敛本地快照；禁止 claim |
| PG 权威 | Go `ConfirmEvents` | 内部 `POST .../events/confirm` | 锁 projection 只读 execution/事件；缺行 `missing`；内容冲突 409 | 不 claim、不 projectTerminal、不改 Goal |
| 丢失执行 | recover 在 WAL 排空且无 `turn/end` / 答案 / 问题 / 审批时清 `executionLease` | recover 末尾注释与 `leaves a recovered in-flight handoff for Go lease expiry` | Node 不补第二终态 | Go scanner 在 lease 过期后裁定 unknown |

现场缺陷：本轮未发现 ACK 越过未证明前缀、confirm 误带 lease、或 Node 重放丢失模型请求的代码路径。维护摩擦：`pi-runtime.test.ts` 两条回执/5xx 用例强转 `runtimeFor` 后调用私有 `appendPublishedBatch` 并注入 `executionLease`；长输出用例读私有 `eventBatcher.pendingCount`。尚未验证风险：Node `recoverDurableHandoff` 在 claim 409 时 `return false` 的路径，四份指定测试里没有对应用例；Go fencing / ConfirmEvents 本 issue 未执行。

### 方案比较

**保留现状**（本 issue 选择）

TurnRuntime 继续拥有 lease 门控的在线发布和 `recoverDurableHandoff` 状态机。`JournalEventBatcher`、`runtime-journal.ts` 纯函数、`TurnStore` ACK、Go Append/Confirm 权限分裂保持现拆分。两条私有发布测试继续经 TurnRuntime 强转取证。

**最小职责收拢**（对照，不实施）

把「带 lease 的 append + 回执 + 5xx 原序重试 + ACK」和「无 lease 的空探针 / 前缀 confirm / 409 逐条缩短」收到现有 journal 发布邻域，由调用方注入当前 lease（仅 append）和 store。TurnRuntime 仍调用：有租约才 publish、barrier drain、confirm 结果为 adopted / missing / conflict 时决定 claim、abandon、改 phase。接口草案只写在本段：handoff 对象吃 `ProductFlowClient` + `TurnStore` + conversation/run 身份；`enqueue(event, barrier)` / `drain()` / `confirmRestartPrefix()` 返回 adopted-terminal、drained-missing、content-conflict、confirmed-through。不规定新类或方法数量。禁止把审批编排或 `projectTerminalEvent` 搬进该对象。

| 项目 | 保留现状 | 最小收拢 |
|---|---|---|
| 可从 TurnRuntime 删除的调用方知识 | 无新增删除 | 回执字段循环、5xx 退避、空探针形状、409 后逐条 confirm、batcher 构造细节 |
| 必须保留的生命周期知识 | lease 与 queued Turn 隔离、`persistenceError` 中止 Pi、先 confirm 再 claim、adopt/abandon、问题/审批/终态、把无终态 in-flight 交给 Go | 相同；confirm 结果枚举仍要在 recover 里分支 |
| AR-02-D | 满足：没有多一层转发 | 若只搬 `appendPublishedBatch`/`confirmPublishedPrefix` 而 recover 仍解释同样六种结果，属于转发，不够实施 |
| 测试侵入 | 2 条私有 append + 1 处 pendingCount；恢复已走 `recoverDurableHandoff` / `recoverAfterRestart` | 回执/5xx 可改打 handoff 公开面；恢复测试仍应打 recover，不能改成只测 confirm 循环 |
| 生产调用方 | append 协议只有 TurnRuntime 一处 | 仍是一处生产调用方；第二适配器只是测试假客户端（HTTP 缝已经在 `ProductFlowClient`） |
| 业务权威 | PG journal + Go lease/scanner 不变 | 必须保持同一权限分裂；本任务也未授权改 Go |

选择保留现状的证据：重启前缀确认与 claim/phase/abandon 编在同一个 `recoverDurableHandoff` 里，单独吸收 confirm 循环不会删掉调用方必知分支。在线路径已经把排队（batcher）、回执等式（`eventReceiptMatches`）、ACK 文件（store）拆出，剩下的是「当前 lease 才能 append」的胶水，且只有一个生产调用方。指定测试 61 通过，未观察到协议丢失。来源报告提出收拢；当前代码与无筛选测试不支持「实质减少协议知识」这一实施门槛。

### 权限 / 失败矩阵

| 场景 | 合同 | Node 测试（本轮） | Go 测试 | 本轮结果 | 对实施建议的限制 |
|---|---|---|---|---|---|
| 错误回执 | 不 ACK，保留 unpublished | `does not advance the local ACK for a mismatched event receipt`（私有 `appendPublishedBatch`） | 无对等 Node 回执测试；Go 侧是写入时 sequence 冲突 | 通过（fake HTTP） | 不能当 Go gate；证明 ACK 门在 Node |
| 5xx | 原事件原序重试后 ACK | `retries a 5xx journal batch then ACKs the original sequence` | 未跑 | 通过（fake HTTP） | 退避与次数是 Node 行为 |
| 已提交但响应丢失 | 重启 confirm 已提交前缀再 ACK | `process-restart.e2e.test.ts` `confirms a server-committed batch after SIGKILL before the local ACK`；期望 `confirmRequests` 为 `[[], [1]]` | 未跑 | 通过（fake ProductFlow HTTP + 真 WAL + 真进程） | 不是真实 Go/PG |
| 部分前缀缺失 | confirm `status=missing`，ACK 已证明前缀，随后走 append | Go `TestConfirmEventsReadsActiveAndReleasedJournalWithoutMutation` 断言 missing seq=4；Node recover 多处 fake 对非空 batch 返回 `missing` 再 `appendTurnEvents` | 代码在 `event_confirm_test.go` | Node 指定文件通过；Go 本 issue 未执行 | 缺本轮 Go 证据 |
| 内容分叉 | 409 后逐条 confirm，放弃 divergent 本地后缀，服从 PG | `skips a divergent local prefix on event content 409 and still recovers` | Go 同文件冲突 payload → 409 `event_sequence_conflict` | Node 通过（fake HTTP） | Go 冲突路径未在本轮执行 |
| 旧 lease fencing | append 受 fencing；confirm 不续租；旧 writer 追加失败 | 指定四文件无「recover claim 409」用例；`leaves a recovered in-flight handoff for Go lease expiry` 是成功 claim 后把无终态交给 scanner | `TestRecoverExpiredExecutionsRejectsStaleFencingWriter` | Node 指定测试通过但未覆盖 claim 409；Go 未执行 | 实施若改 recover 必须补 Node claim 409 与 Go fencing，不能用本轮结果放行 |
| barrier drain | tool/question/approval/message/terminal 先 drain；失败批次留队首 | `journal-publisher.test.ts` 合并 barrier、失败留队首、分类 barrier | 不涉及 | 通过 | 与 HTTP 权限无关 |
| 已有终态确认 | 空探针带 terminal 则 adopt，禁止 claim；本地 persistence fallback 可裁 | `adopts a PG-only terminal discovered by an empty confirmation probe`；`publishes one original terminal and removes its local persistence fallback during recovery` | `TestConfirmEventsReadsActiveAndReleasedJournalWithoutMutation` 含空 confirm 的 terminal | Node 通过；Go 未执行 | Node 侧已禁止 probe 后 claim |
| Node 不重放丢失模型 | SIGKILL 后不二次打 provider | `process-restart.e2e.test.ts` `does not replay the model after SIGKILL once the provider request has started` | Go scanner 终态在其它包测 | 通过（fake HTTP + 真进程） | 不冒充 Go scanner |
| leftover 5xx 隔离 | 单条 confirm 500 不阻断其余 handoff | `keeps listening after one leftover durable handoff fails to recover` | 未跑 | 通过 | 进程继续 listen；broken handoff 仍占用候选 |

拟迁移测试（仅当未来维护者推翻本结论时）：把回执不匹配与 5xx ACK 两条从 `pi-runtime.test.ts` 私有方法挪到 journal 公开面，并删除对 `appendPublishedBatch` 的强转；`recoverDurableHandoff` / process-restart / store WAL 用例留在原入口。当前不迁移。

### 交付 commit

关闭本 issue 的 Git 提交（证据类无代码交付，审核/完成/归档同一提交）。认领提交 `324894a6`。

### 审核者 / 结论

审核者：主代理-0905-0439（自审，非独立审核）。结论：接受「保留现状」。不发布 journal 实现 issue。父章程 AR-02 登记本结论与本归档链接。

### Issue 结果 / 业务门槛结果 / 剩余缺口

- Issue 结果：完成。调查问题的答案是：把在线发布、回执、ACK 和重启前缀确认一并吸收进 journal module，不能实质减少 TurnRuntime 的协议知识；测试私有侵入限于两条发布用例和一处 pendingCount，恢复路径已有公开 `recoverDurableHandoff`。用户可见行为未改。
- 业务门槛：AR-02 证据任务完成，结论为保留现状。不表示生产 Gate 或 journal 容量门槛新通过。G-05/G-07 沿用父章程既有记录。
- 剩余缺口：本轮未跑 Go ConfirmEvents / fencing；Node recover claim 409 缺测；10k WAL 容量门按 `runIf` 跳过。这些缺口不改变「不发实现单」的裁定，若日后改 recover/append 协议须在对应实现 issue 补齐。
