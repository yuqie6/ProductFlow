# Agent 单商家生产就绪验收账本

**实现切片已关闭。** S1–S6 与 G-01–G-05、G-07 已完成。剩余 G-06 的 L1–L6 行为门槛走 [`tasks/eval-skills.md`](tasks/eval-skills.md) 与 [`tasks/eval-live-layers.md`](tasks/eval-live-layers.md)，本账本只引用其 `run_id`。

本账本是 ProductFlow Agent 生产可靠性工作的唯一验收指标。实现、测试、ADR、ROADMAP 或会话摘要与本账本冲突时，不得通过缩小本账本范围来宣告完成；应修正实现，或把经过用户确认的新决策写入本账本的“决策变更”后再执行。

## 来源与使用规则

- 来源：Codex thread `01a05656-c0bf-7e92-94eb-046a74a6b71d` 在 2026-08-31 14:06:31 +08:00 输出的 `<proposed_plan>`《ProductFlow Agent 单商家生产就绪实施计划》。
- 原计划块 SHA-256：`d0d4ef1a1d67842bc0a610f74c1238218e57a74d6cb4ce27723028d013cf1115`，共 88 行。
- 本账本逐项转写该计划，不以较早的《Agent 企业级设计案》、memory、ADR 摘要或当前代码反向缩减范围。
- `完成`：当前代码、贴近合同的自动化测试和要求中的真实 gate 都有证据。只存在代码、只存在测试替身、只跑过窄测试均不能标完成。
- `部分完成`：已有可运行实现，但合同覆盖、故障场景、真实基础设施或容量证据不全。
- `缺失`：没有实现或没有足以判断的证据。
- `违背`：当前实现与计划明确合同相反。
- 每次状态变化必须同时填写代码 owner、测试/实测证据、最近验证日期；审计人不得只写“已实现”。

## 冻结决策

| ID | 决策 | 状态 | 当前证据与缺口 |
|---|---|---|---|
| D-01 | Turn 继续使用 `unknown`；中断通过 `terminal_reason_code` 和 `assistant/message.interrupted=true` 表达 | 完成 | 2026-08-31：`recovery.go` 按 `attempt_id` 组装 interrupted `assistant/message`；`AgentTurnTail` 用 `terminal_reason_code` 区分文案；`TestCrashAfterModelStartRecoversUnknownAndInterruptsInvocation` 与 compact 多 attempt 测试通过。 |
| D-02 | 工具恢复先对账；`applied` 自动收敛；`not_applied` 仅对 `reconcile_then_retry` 使用原幂等键重试；`conflict/unknown` 停止自动执行 | 完成 | 2026-08-31：HTTP 与 scanner 共用 `reconcileEffectIntent`；`TestReconcileEffectIntentEightToolStateMatrix` 覆盖八工具 applied/conflict/unknown/retry；二次 not_applied 不再持久化 `not_applied`；`TestReconcileTurnEffectAndScannerShareInterpreter` 通过。 |
| D-03 | 当前 provider 走 foreground；建立 `background_resumable` 合同和持久化结构；不增加绕过 Pi 的官方 OpenAI 执行器 | 完成 | 2026-08-31：Go `BackgroundResumable` = profile ∩ `agentAdapterBackgroundResumable=false`；Agent `effectiveBackgroundResumable`；`before_model_request` 拒绝 `execution_mode=background`；checkpoint 含 `model_response_bound/cursor`。无旁路 OpenAI 执行器。 |
| D-04 | 完成标准是单管理员、单商家生产就绪；保留 7 天 chunk 压缩；不含 SaaS tenant、计费和对象归档 | 部分完成 | 产品边界和 7 天压缩已按 attempt 绑定 `sourceEventSeqs`；G-03、G-04、G-05 有证据。G-06：2026-09-05 `just web-e2e-live-agent-workflow` 1 passed（审批卡 → 单一 WorkflowRun → 真实出图，7.4m）。评测 L1–L6 门槛仍以 [`agent-eval-system.md`](agent-eval-system.md) 的 `run_id` 为准，未过不得采信分数。G-07 干净 checkout 全量门尚未在本 HEAD 重跑。 |
| D-05 | 容量 gate 是 25 个并发 Turn、100 条 SSE、单 Turn 1 万事件、单会话 1000 Turn | 完成 | 2026-09-04 当前 HEAD 复核：`just go-test-agent-journal-capacity` 10k 事件 P95=232.800884ms、25 Turn×128、100 SSE overflow 503；`just agent-service-test-local-journal-capacity` 本地 10k WAL P95=0.81ms。2026-09-01 已有 `TestLastFiftyTurnsQueryP95`、`TestAgentSSETimeToFirstEventP95` 和 Chromium gap repair 证据，均在阈值内。 |

## 协议与数据合同

| ID | 验收要求 | 状态 | 代码 owner | 测试与实测证据 | 缺口 |
|---|---|---|---|---|---|
| C-01 | Tool Manifest 生成 `recovery_policy: none \| reconcile_only \| reconcile_then_retry`；指定的 workflow request、intake、workspace、graph apply/propose/discard、run cancel 为 `reconcile_then_retry`；read、UI focus、`ask_user`、`propose_global_draft` 为 `none`；字段进入 manifest hash 和 Go 生成映射，CI 阻止 TS/Go drift | 完成 | `agent-service/src/tool-manifest.ts`、`scripts/generate-contract-artifacts.ts`、`go/internal/agent/tool_manifest_generated.go` | 2026-08-31：`tool-manifest.test.ts` 逐项断言 C-01 表；`pnpm --dir agent-service check-contract-artifacts` 通过；`just agent-service-test` pretest 改为 `--check` 不再静默重写；151 passed | 清单条目自带 `recovery_policy`，生成脚本从条目写 Go map；drift 时 check 退出 1 |
| C-02 | `tool_effect_intent` schema v1 固定保存 `tool_name`、`tool_call_id`、`idempotency_key`、`recovery_policy`、完整有界 `request_payload`；不得保存密钥、图片字节和原始 HTTP 响应 | 完成 | `agent-service/src/tool-effect.ts`、`tools.ts`、`go/internal/agent/effect_intent.go`、`execution.go` | 2026-08-31：`tool-effect.test.ts` schema 合同、`tools.test.ts` intake/workflow/workspace/apply 完整 payload；Go `TestParseToolEffectIntent*` 与 `TestAppendCheckpointEnforcesToolEffectIntentV1`；`just agent-service-test` 150 passed；`go test ./internal/agent` 通过 | 写入端只产生 v1；AppendCheckpoint 拒绝 `request` 和顶层平铺；intake 含 `task_id`，workflow 含 `source_step_id`/`scope`/`node_ids`/`force`/`document_action` |
| C-03 | `AgentTurnProjection.terminal_reason_code` nullable，允许值仅为 `provider_failed`、`execution_interrupted`、`effect_reconciled`、`effect_conflict`、`effect_unknown`、`persistence_failed`；旧行保持 null | 完成 | `go/internal/platform/db/schema/models.go`、`constraints.go` | 2026-08-31 `just go-migrate` 成功；真实 dev PG 存在约束 `ck_agent_turn_projections_terminal_reason_code` | |
| C-04 | `agent_model_invocations` 关联 projection/execution，持久化稳定 request ID、attempt/fencing、provider/model、execution mode、provider response ID/cursor、状态、耗时、token、usage source 和安全错误码；旧行不回填 | 完成 | `go/internal/platform/db/schema/models.go`、`constraints.go`、`go/internal/agent/execution.go` | 2026-08-31：ExtraDDL 部分唯一索引 `uq_agent_model_invocations_provider_response_id`；`finishModelInvocation` 写 response ID/cursor/error_code/`duration_ms`；`TestModelInvocationIdempotentCreateAndUsageDedup`、`TestProviderResponseIDUniqueAcrossInvocations`；`go test ./internal/agent` 通过 | 重复 `provider_response_id` 返回 409；全 0 usage 保持 `unavailable` |
| C-05 | checkpoint 支持 `model_response_bound`、`model_response_cursor`；foreground 当前不发送；`before_model_request` 含 provider/model/mode；`assistant/message` 含 request ID、usage、duration | 完成 | `go/internal/agent/helpers.go`、`execution.go`、`agent-service/src/turn-runtime.ts`、`pi-chunks.ts` | 2026-08-31：enum ExtraDDL `ADD VALUE IF NOT EXISTS`；`TestAppendCheckpointAcceptsModelResponseKindsWithoutWritingThemInForeground`；Pi `duration_ms`；Agent 155 passed | foreground 接受 bound/cursor checkpoint，Pi 当前不主动发送 |
| C-06 | provider config 暴露有效 `background_resumable`；必须同时由 provider profile 和 adapter 支持；当前 Pi adapter 固定 false，网关探测不得误写 true | 完成 | `go/internal/settings/agent.go`、`agent-service/src/provider-capability.ts`、`pi-runtime.ts` | 2026-08-31：`TestResolveAgentProviderBackgroundResumableIsNeverTrue`；`provider-capability.test.ts`；background checkpoint 400「当前 Agent adapter 不支持 background 模型调用」；effective true 时 Pi 抛 `background_unsupported` | DB CHECK 仍允许列值 `background` 供未来 adapter；写入路径拒绝 |
| C-07 | 产品/全局 Conversation 均提供鉴权事件页 `events/page?after=<seq>&limit=<1..250>`，返回稳定 UI 事件、`next_after`、`has_more`、`stream_state` | 完成 | `go/internal/agent/http.go`、`sse.go` | `journal_test.go`、`http_test.go`、route contract tests；Go 全量通过 | Web 对矛盾 terminal page 的处理另见 S4。 |
| C-08 | UI 事件保持兼容；恢复结果复用 `assistant/message`、`tool/result`、`turn.unknown`，不新增 `interrupted` Turn 枚举 | 完成 | `go/internal/agent/recovery.go`、Web reducer/runtime | Go/Web tests 通过 | |
| C-09 | Go `/metrics` 使用 env-only `METRICS_BEARER_TOKEN`；Agent Service `/metrics` 使用内部 token；未配置 token 时端点不注册 | 完成 | `go/internal/platform/metrics/http.go`、`agent-service/src/server.ts`、`metrics.ts` | 2026-08-31：Go metrics 鉴权测试；Agent `server.test.ts` 空 token 404 / 无 Bearer 401 / 有 token 200 | `/metrics` 在 catch-all 鉴权之前注册 |

## 实施切片

### S1 合同与 Schema 基础

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S1-01 | Manifest、生成器、Go 映射、checkpoint 枚举、provider capability DTO、Turn DTO、数据库模型与约束一致 | 完成 | 2026-08-31：`background_resumable` DTO、`model_response_*` checkpoint 枚举与 ExtraDDL、invocation unique 约束与 Go/TS 测试对齐。 |
| S1-02 | `before_model_request` 在同一事务幂等创建/读取 invocation；`assistant/message` 按 request ID 闭合状态与 usage | 完成 | 2026-08-31：`TestModelInvocationIdempotentCreateAndUsageDedup` 重放 checkpoint 与二次 message 不去重写 usage；`duration_ms=42` 与 response identity 写入。 |
| S1-03 | migration 走 GORM `CreateTable`/`AddColumn` + ExtraDDL；唯一约束覆盖 request ID、provider response ID、effect reconciliation identity | 完成 | 2026-08-31：`uq_agent_model_invocations_provider_response_id` ExtraDDL；`TestProviderResponseIDUniqueAcrossInvocations` 跨 invocation 409。 |

### S2 崩溃恢复解释器

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S2-01 | Dispatcher 用有界批次和 `FOR UPDATE SKIP LOCKED` 领取过期 execution；先递增 fencing 并清旧 owner，拒绝旧 writer | 完成 | 2026-08-31：`expiredExecutionBatchLimit=25`；`TestExpiredExecutionRecoveryUsesBoundedSkipLockedBatch` 一次只收敛 1 条；fencing 递增与 owner 清除仍在 `recoverExpiredExecutions`。 |
| S2-02 | 未进模型的 queued/claimed 可安全重排队；等待输入释放 lease 并保留问题/答案，回答创建新 attempt | 完成 | 2026-08-31：Go `persistQuestionAnswer` 后仍 `requires_input` 并入队 sync；scanner 对任意 `requires_input` 不写 unknown；新 claim 重置 `last_checkpoint_sequence`；`requires_input` 即使 phase=`model` 也可 claim。`TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult` 真实 Go+PG+Pi：SIGKILL 后答案落库、新 attempt、第二次 Responses 含 `function_call_output`，Turn `succeeded`。 |
| S2-03 | 已有 `turn/end` 时只幂等重投影并释放 execution，不追加第二终态 | 完成 | 2026-08-31：`recoverExpiredExecutions` 先读 `turn/end` 并调用 `projectTerminalEvent`；`TestExpiredExecutionRecoveryReprojectsExistingTerminalWithoutSecondEnd` 证明 succeeded 终态不被改成 unknown、事件数仍为 2、execution phase=terminal |
| S2-04 | 未配对 effect intent 统一调用 `reconcileEffectIntent`；applied 合成结果；not_applied + retry policy 原键重试一次；conflict/unknown 禁止重试 | 完成 | 2026-08-31：`appendInterruptedTurnEvents` 与 HTTP 均调用 `reconcileEffectIntent`；八工具四态矩阵通过；scanner 不再走独立 ledger 白名单。 |
| S2-05 | 重试后再次对账；最终仅收敛 applied/conflict/unknown；网络错误不得推断 not_applied | 完成 | 2026-08-31：retry 终态断言禁止 `not_applied`；lookup 错误落 `unknown`；`TestReconcileTurnEffectAndScannerShareInterpreter` 共享解释器。 |
| S2-06 | 不续跑丢失的模型 loop；从已落 PG chunk 组装 interrupted message，再写 `turn/end unknown` 和 reason code | 完成 | 2026-08-31：按 `attempt_id` 绑定 chunk seq；已有 canonical message 的 attempt 不再重复 interrupted；`TestCrashAfterModelStartRecoversUnknownAndInterruptsInvocation`。 |
| S2-07 | effect applied 但模型后续不可证明时 Turn 仍为 unknown，UI 明确区分操作完成与回复中断 | 完成 | 2026-09-01：Go 保持 unknown 并合成 reconciled tool result；`AgentTurnTail`/`data-agent-turn-reason` 与组件测试断言「操作已完成，回复在中断前未写完」。`PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH=1` `e2e/agent-workbench-recovery.spec.ts` 在真实工作台会话断言 `data-agent-turn-reason='effect_reconciled'` 与该文案可见。 |
| S2-08 | 手工 API 与自动 scanner 共用解释器，覆盖 manifest 全部可恢复工具，删除三工具硬编码白名单 | 完成 | 2026-08-31：`recoverableToolNames()` 与 manifest 八工具一致；`TestReconcileTurnEffectAndScannerShareInterpreter` 在 drain leftovers 后通过。 |

### S3 审批和等待输入恢复

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S3-01 | workflow request、graph proposal、global draft、`ask_user` 使用稳定 pending identity；重复/迟到回答和取消竞争统一为 `not_pending`/409 | 完成 | 2026-08-31：`apperr.NotPending` HTTP `code=not_pending`；`TestPendingIdentityConflictsShareNotPendingCode` 覆盖迟到 ask_user、重复回答、已取消 workflow confirm、已结束 graph proposal、非待确认 draft。 |
| S3-02 | 重启后 pending 审批从 PG 重投影 requested；resolved 只回放 resolved，不重新开放 | 完成 | 2026-09-01：`TestCrashAfterApprovalKeepsPendingRequestWithoutSecondRun` 过期 lease 不把 awaiting_confirmation 改成 unknown。Chromium `agent-workbench-recovery.spec.ts` page reload 后仍 `awaiting_confirmation`；事件页 `approval.requested` 1 条、`approval.resolved` 0 条。 |
| S3-03 | waiting-input 重启仍显示原问题；答案已落库但未继续时创建新 attempt，并向 Pi 注入同一 question/tool result | 完成 | 2026-08-31：`TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult` 对真实 Pi 进程 SIGKILL；过期 lease 后问题仍 `requires_input`；503 后 PG 仍有答案；第二进程 `SyncTurn` 注入 `ask_user` tool result 并 `succeeded`。辅助合同：`TestClaimNewAttemptResetsCheckpointSequence`、`TestSyncTurnAppliesQueuedResumeAfterDurableAnswer`、`TestClaimAllowsExpiredRequiresInputWhenPhaseIsModel`；Agent `store.test.ts` 对带 `execution_attempt` 的 parked question 记 `waiting_input`。 |
| S3-04 | 恢复不得创建第二业务请求、proposal 或 WorkflowRun | 完成 | 2026-09-01：`TestConfirmWorkflowRunRequestDoesNotCreateSecondGraphRun`；`TestRecoverExpiredProposalTurnDoesNotCreateSecondProposal` 过期恢复后同幂等键仍同一 `proposal_id`、表内 1 行；`TestRecoverExpiredDraftTurnDoesNotCreateSecondDraft` 恢复不写第二 organization draft。含在 `just go-test`。 |

### S4 ConversationRuntime Gap 自愈

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S4-01 | Runtime 有 connection generation；新连接后忽略旧 generation 的 open/error/message | 完成 | 2026-08-31：`runtime.test.ts` 与 Chromium `agent-conversation-runtime.spec.ts` 均忽略旧代 open/error/message。 |
| S4-02 | `sequence > cursor + 1` 进入 repairing，缓冲新事件、从 cursor 分页补齐；补洞期间保持当前 generation 的 EventSource（见决策变更 2026-09-05） | 完成 | 2026-09-05：`runtime.test.ts` 断言补洞后 `source.closed=false`；Chromium `just web-e2e-agent-sse` 5 passed，其中 gap 用例 `generationCount=1`、`liveClosed=false`、`received=[1,2,3]`、事件页调用 1 次；真实 PG 事件页补洞 ≤5s 仍通过。 |
| S4-03 | 补齐后按 seq 去重并连续应用，排空 buffer；未变化 Item 引用稳定 | 完成 | 2026-09-05：Chromium 与 `runtime.test.ts` 证明 repair 去重、连续 apply，并保持同一 EventSource。`agentEventReducer.test.ts`「keeps unchanged tool step object identity across later text deltas」断言未变化 tool step 引用稳定。 |
| S4-04 | buffer 上限 512 条或 1 MiB；超过上限、连续三代补洞失败或服务端事件矛盾才进入 protocol error | 完成 | 2026-09-05：`runtime.test.ts` 512 条上限；`stream.complete` 后事件页跳过下一 seq 立即 protocol error「未返回 sequence 2」，不再重连。Chromium matrix：overflow / terminal gap / parked approval 通过（`just web-e2e-agent-sse`）。 |
| S4-05 | terminal/finite 流也补洞到终态或 `stream_state=terminal`；正常 complete 不显示断线 toast | 完成 | 2026-08-31：finite/stream.complete 先 probe 再关闭；`onStreamError(null)`；Chromium terminal gap 与 parked approval 不误关。 |
| S4-06 | 同 Turn Runtime 单例；Workbench 和全局 Dock 不产生第二 EventSource | 完成 | 2026-09-01：registry/consumer 单例与 `loadHistory` 无 EventSource 已有单元覆盖。Chromium `agent-workbench-recovery.spec.ts` 工作台侧栏 Agent + 「放大至全局主控台」后，同一 live Turn 的 EventSource 路径集合 size=1。 |

### S5 Usage、指标与告警

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S5-01 | durable usage source 仅 provider/estimated/unavailable；缺失记 unavailable，不用 0 冒充 | 完成 | 2026-08-31：`finishModelInvocation` 全 0 保持 unavailable；`usage_source=estimated` 写入；`durableAssistantUsage` 丢弃全 0；`TestModelInvocationMissingUsageStaysUnavailable` / `TestModelInvocationEstimatedUsagePersists`。 |
| S5-02 | 指标覆盖 Turn、事件批次/延迟/冲突、lease、恢复、pending approval、SSE、provider、usage missing、dispatcher backlog、图运行 | 完成 | 2026-08-31：Go snapshot 含 turns/runs/dispatches/executions/reconciliations、batch count/last ms、sequence conflict、recovery unknown、pending workflow/graph/draft/ask_user、expired leases、usage missing；Agent Service `/metrics` 含 batch/conflict/provider 4xx/5xx。 |
| S5-03 | label 仅 bounded status/phase/tool/provider/model；禁止业务 ID、用户文本、错误详情 | 完成 | 2026-08-31：`safeLabel` / `safeMetricLabel`；`TestAgentAlertRulesCoverRequiredNames` 禁止 conversation_id/turn_id/user_text/error_detail。 |
| S5-04 | 告警覆盖投影不一致、活动超时、unknown、effect unknown、lease lost、sequence conflict、usage missing、backlog、provider 5xx | 完成 | 2026-08-31：`ops/prometheus/agent-alerts.yaml` 九条规则；`TestAgentAlertRulesCoverRequiredNames` 通过。未做 live Prometheus scrape。 |
| S5-05 | token 和内部指标不进入商家工作台，只经 invocation 数据和 Prometheus 提供运维能力 | 完成 | Web 未读取或展示 token/内部 metrics；当前仅 PG invocation 与受保护 Go metrics 暴露。 |

### S6 历史压缩与读取边界

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S6-01 | 保留 7 天窗口；只在 canonical assistant message 已存在时压缩 chunk；按 attempt/message 正确绑定 `sourceEventSeqs` | 完成 | 2026-08-31：`compact.go` 按 attempt 绑定 text/thinking；`TestCompactExpiredTurnJournalsKeepsThinkingWithoutCanonicalSnapshot` 与 `KeepsTextChunksWithoutMessageSnapshot`。 |
| S6-02 | tool result、approval、usage、terminal、checkpoint 不压缩；任务有界、幂等并暴露指标 | 完成 | 2026-08-31：只更新 text/thinking chunk；50 Turn 批次；`productflow_agent_journal_compact_turns/errors` 计数。 |
| S6-03 | 会话列表 cursor 分页且默认最近一页；历史 Item 按需读事件页；浏览器不常驻完整日志 | 完成 | 2026-08-31：`ListSessions` cursor + `next_cursor`；Web infinite query；settled Turn 走 `loadHistory` 不创建 EventSource；`agentTurnNeedsEventStream` 对 succeeded/failed/canceled/unknown 为 false。 |

## 测试与生产 Gate

| ID | 必须通过的 Gate | 状态 | 最近命令/环境/结果 |
|---|---|---|---|
| G-01 | 单元/合同：manifest policy codegen、checkpoint payload、invocation 幂等、reason code、事件分页、unknown ignorable、usage 去重 | 完成 | 2026-09-04 当前 HEAD：无缓存 `go test -C go ./... -count=1 -p 1` 通过；`just agent-service-test` 20 files passed、1 skipped，167 passed / 2 skipped；`pnpm --dir web test:run` 93 files、637 passed。Agent contract artifact check 在 pretest 中通过。 |
| G-02 | PostgreSQL：八类工具 effect 状态矩阵、同幂等键重复 scanner、多 dispatcher、旧 fencing writer | 完成 | 2026-09-01：八工具四态矩阵与共享解释器；`TestRecoverExpiredExecutionsRejectsStaleFencingWriter` 拒绝旧 lease writer 与过期 fencing；`TestConcurrentExpiredRecoverySkipLockedDoesNotDoubleTerminate` 两个 scanner 并发 SKIP LOCKED，4 条 Turn 各一 `turn/end`。含在 `just go-test`。 |
| G-03 | `kill -9` 四点故障注入：模型开始后、mutation 成功/result 前、approval 后、turn/end 落库/响应前；验证连续 seq、至多一次副作用、诚实终态、无永久活动 Turn | 完成 | 2026-09-01：`TestSIGKILLLeaseHolderAgainstGoPG` 对真实 httptest Go+PG 四点 SIGKILL helper：`model_start` / `mutation` / `approval` / `turn_end`；连续 journal、mutation 副作用至多一次、诚实终态。含在 `just go-test`。 |
| G-04 | 浏览器：普通断线、gap、重复帧、旧 generation 迟到帧、buffer overflow、terminal gap、approval 刷新；无错误断线 toast | 完成 | 2026-09-05 HEAD `ebc7630d` 工作树：`just web-e2e-agent-sse` 5 passed（PG 事件页补洞 ≤5s；原地事件页补洞不丢 live EventSource；generation/overflow/terminal gap/parked approval；duplicate seq1 再 seq2；raw EventSource cursor 重连）。 |
| G-05 | 标准容量：25 Turn、100 SSE、1 万事件/Turn、1000 Turn/会话；零洞、零重复副作用、零永久活动；P95 batch PG <=300ms、SSE <=1s、gap repair <=5s、最近 50 Turn <=500ms | 完成 | 2026-09-04 当前 HEAD 复核：`just go-test-agent-journal-capacity` deep_events=10000 concurrent_turns=25 batches=207 p95=232.800884ms；100 SSE overflow 503；`just agent-service-test-local-journal-capacity` 本地 10k WAL duration=6351.4ms p95=0.81ms。2026-09-01 的 `TestLastFiftyTurnsQueryP95`、`TestAgentSSETimeToFirstEventP95` 与 Chromium gap repair 证据仍在阈值内。 |
| G-06 | 真实 gate 使用 `gpt-5.6-luna`：完整 skill eval、真实审批到 WorkflowRun、真实图片图运行、真实 Chromium；确认 `background:true` unsupported | 部分完成 | 2026-09-05：`just web-e2e-live-agent-workflow` 1 passed（7.4m）；skip-Agent 建画布后打开对话，审批卡「确认并执行」，单一 WorkflowRun succeeded，生成图为真实 PNG/JPEG/WEBP。`just web-e2e-live-graph` 此前已通过。JSON 任务集 L1–L6 出口仍只以 [`agent-eval-system.md`](agent-eval-system.md) 登记的 `run_id` 与门槛为准；历史 15/15 冒烟不满足该账本阶段出口。Go/Pi 的 `background:true` 拒绝测试仍以无缓存 Go 全量门为准。 |
| G-07 | 全量：Go、Agent Service、Web test/lint/build、docs-check、migration fresh/upgrade、`git diff --check`、干净 checkout 重跑 | 完成 | 2026-09-04 当前 HEAD `fb658633`：全量 gate 启动时代码工作树干净；随后本次账本更新仅修改三份 `docs/audits/` 文档。无缓存 `go test -C go ./... -count=1 -p 1`、`just agent-service-test`（167 passed / 2 skipped）、`pnpm --dir web test:run`（637 passed）、`pnpm --dir web lint`、`just web-build`、`just docs-check`、`just go-migrate`、`git diff --check` 均通过。schema fresh/upgrade 由 `TestApplyEmptyDatabaseMatchesHeadConstraints`、`TestApplyTwiceDoesNotDeleteRows`、`TestApplyExistingHeadKeepsSchema` 覆盖并通过。 |

## 持久日志提交语义

这一节固定当前审计必须回答的问题，不预设实现已经合格。

1. 原始 token 可以在装配层合帧为 `text.chunk` / `thinking.chunk`，日志事件以有界批次提交 PostgreSQL。
2. `agent_turn_events` 必须保持连续 seq；批次失败必须保持原序重试或使 Turn 诚实终止，不能跳过失败事件继续确认后续序列。
3. 浏览器只能消费已经提交到 PostgreSQL 的事件；本地文件不能形成浏览器可见的第二事实源。
4. 本地 append-only journal 可以辅助 Pi loop 和进程内恢复，但不得使 PostgreSQL 降级为无限期 eventual sink。
5. 生产 gate 约束批量写入 P95 不超过 300ms。实现必须同时证明有界等待、结构性 flush barrier、终态 flush 和失败传播；“每原始 token 单独一次 PG”与“本地返回后无界后台提交”都不是计划目标。

当前结论：`完成`（提交语义与容量 P95）。G-07 已在 2026-09-04 账本更新前的 clean HEAD 以无缓存全量门复核；G-06 的审批到 WorkflowRun Chromium 链已在 2026-09-05 重跑通过，评测 L1–L6 门槛仍缺。

- 当前不是逐事件等待 PG。`JournalEventBatcher` 使用 20ms、64 events、768 KiB 三个上限；普通 chunk 先入队，tool/question/approval/assistant message/terminal 是等待 drain 的结构屏障；每个 batch 调一次 `/events/batch`。
- Go 的 batch append 在同一事务校验连续 seq、写事件并更新终态投影；浏览器 SSE 只查询 PG。失败批次保留在队首，屏障和 terminal 失败会传播并中止 Turn。
- 该方向符合计划。不得回退到每个日志事件都立即 `await` PG；那会使 batch API 实际退化为单事件提交，无法满足容量和 P95 目标。
- 本地事件存储已改为逐行 append + fsync 的 JSONL WAL；独立原子 ACK watermark 只在 Go receipt 全字段匹配后推进。尾部半条记录在启动时截断并 fsync，中间损坏直接阻止恢复。10k 端到端本地 WAL gate 已证明没有整文件 O(n²) 重写。
- durable handoff 在首次事件前即建立 ACK=0 边界；启动先用空 confirm probe 读取 PG journal head/权威终态，再 exact-confirm 本地前缀。日志内容 409 跳过 divergent 本地前缀并服从 `persisted_through`，不再当 lease 竞争、也不再让单 Turn 阻断 listen；PG-only terminal 以 PG projection snapshot 收敛本地状态。
- 进程内 publisher 在短暂 PG 故障后按 250ms 起步、最高 30s 的有界指数退避重试，受 `maxConcurrentTurns` 约束；启动恢复和后台恢复遇到暂时性 claim 409 都会继续退避，每轮先 exact-confirm，旧 owner 终态或 lease 释放后可自动收敛。首个原终态提交后会裁掉纯 `persistence_failed` fallback，不提交第二终态。
- 恢复态问题取消先 fsync `turn/cancel_requested` intent，再写状态；`cancel_requested` 是可重试、可排队、可跨重启修复的状态。首次 claim 连续失败后重复取消不会写第二 intent，后续 claim 成功会落唯一 canceled 终态。
- 软 timed-event 预算已删除；20秒慢流与 10k WAL gate通过。Go 负载 gate 已通过 25 Turn/1万事件、P95 <=300ms 和100 SSE连接上限。
- 2026-09-01 补证：PG batch P95 226.3ms；SSE 首事件 P95 24.82ms；1000 Turn cursor 零洞；本地 10k WAL P95 1.01ms。content-409 启动恢复：`pi-runtime.test.ts` 跳过 divergent 前缀仍 recovered；单 leftover 500 不阻止其余 handoff。
- 2026-09-01 锁顺序：过期 execution 扫描改为先 `FOR UPDATE OF agent_turn_projections SKIP LOCKED` 再锁 execution，与 `AppendEvents`/`HeartbeatExecution` 一致。`TestAppendEventsAndExpiredRecoveryDoNotDeadlock` 在 live writer 持有 projection 时扫描跳过且无 `40P01`。Agent `appendPublishedBatch` 对 ProductFlow 5xx 按 250ms 起步、最高 30s 原序重试；`pi-runtime.test.ts`「retries a 5xx journal batch then ACKs the original sequence」通过。

## 验证记录

### 2026-09-05 Agent 审批到 WorkflowRun Chromium

- HEAD：`ba4508d7` 之后工作树含本闸门 spec 修正；他人 fidelity / image-eval WIP 未纳入。
- `just web-e2e-live-agent-workflow`：1 passed，墙钟 7.4m。路径：只建画布 → 「创建并打开对话」→ 发送「请执行当前工作流」→ 审批卡「确认并执行」→ GraphRun succeeded → 图库真实生成图。

### 2026-09-05 Agent SSE Chromium

- HEAD：`ebc7630d`；工作树另有他人 fidelity / image-eval WIP，未纳入本闸门。
- `just web-e2e-agent-sse`：5 passed（7.5s）。Vitest conversation runtime 12 passed。
- `stream.complete` 后事件页跳过下一 seq 立即 protocol error，不再 scheduleReconnect。

### 2026-09-04 当前 HEAD

- HEAD：`fb658633`；全量 gate 启动时 `git status --short` 为空、代码工作树干净；随后本次审计更新仅留下三份 `docs/audits/` 文档未提交修改。
- 无缓存 Go 全量：`go test -C go ./... -count=1 -p 1` 通过；包含 Agent、Graph、schema fresh/upgrade 测试。
- Agent service：`just agent-service-test`，20 files passed、1 skipped；167 passed / 2 skipped；contract artifact check 通过。
- Web：`pnpm --dir web test:run`，93 files、637 passed；`pnpm --dir web lint` 通过；`just web-build` 通过，bundle budget 通过。
- Schema/docs：`just go-migrate`、`just docs-check`、`git diff --check` 通过。
- 容量：journal 10k/25 Turn/100 SSE P95=232.800884ms；local WAL 10k P95=0.81ms；Graph target-scale query plan 通过且无 Seq Scan。
- Graph read gate：`just http-ab-gates` summary/detail 100/100，p95=4.09/4.71ms；workbench performance Chromium 1 passed，cold/warm TTI=2159/2247ms，初始 detail=0，显式 run detail=1，预期 Agent bootstrap 409 之外无 page/network/HTTP failure。
- 真实 provider：`just agent-evals-live` 15/15；`just web-e2e-live-graph` 1 passed。当前 HEAD 尚未重跑真实 Agent 审批到 WorkflowRun 的完整 UI 链，因此保留 G-06 为部分完成。

### 2026-09-01 当前 checkout

- `just go-migrate`：通过。
- `just go-test`：通过；`./internal/agent` 83.802s。
- `just agent-service-test`：19 files passed、1 skipped；159 passed / 2 skipped。含 content-409 handoff、5xx journal 原序重试、SIGTERM ask_user / unknown flush。
- `just agent-service-test-local-journal-capacity`：10k WAL duration=7352.8ms p95=1.01ms。
- `just go-test-agent-journal-capacity`：10k 事件 P95=226.300005ms；25 Turn；100 SSE overflow 503。
- `TestAgentSSETimeToFirstEventP95`：P95=24.82251ms n=20。
- `TestLastFiftyTurnsQueryP95`：1000 Turn 会话 cursor 读满。
- `pnpm --dir web test:run`：89 files、609 tests passed。
- `pnpm --dir web lint` 与 `pnpm --dir web build`：通过。
- `just docs-check`：通过。
- 整树 `git diff --check`：通过。
- `PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH=1 pnpm --dir web exec playwright test e2e/agent-workbench-recovery.spec.ts`：3 passed（effect_reconciled 文案、审批 reload、Workbench+Dock 单 EventSource）。
- Agent 启动：content-409 leftover 不再退出进程；`/healthz` 200，listening `127.0.0.1:29284`。
- `just agent-evals-live`：15/15 通过（316.54s，calls=60，tokens=530621，usage_unavailable=0）。
- Cursor 独立浏览器标签：商品 `16d616b7-b422-46a3-985c-e0024cc53f7a` 上 Agent 审批确认写出 `workflow_graph_runs` `cd57c363-63ff-4505-b910-8a342b2863f7`（succeeded / graph）。
- Cursor 独立浏览器标签：同商品 Agent `force=true` `scope=node` 确认写出 `workflow_graph_runs` `cb6e3185-4438-431b-90f9-001f212259ee`；节点 `planned_action=generate`；新 PNG `2e9faa8a-…` 1 551 082 字节。Turn `dbdd8601-…` `succeeded`（无 `服务器内部错误`）。
- `go test ./internal/agent -run TestAppendEventsAndExpiredRecoveryDoNotDeadlock` 与并发 SKIP LOCKED 扫描通过。
- `pnpm --dir agent-service exec vitest run src/pi-runtime.test.ts -t "retries a 5xx journal batch"` 通过。
- 未宣告生产就绪：G-07 干净 checkout 仍缺。

### 2026-08-31 当前 checkout

- `just go-migrate`：通过；dev PostgreSQL 实际存在 `agent_turn_events`、`agent_model_invocations`、`agent_turn_effect_reconciliations` 及 invocation/reason-code 约束。
- `go test -C go ./internal/agent -count=1`：本轮通过，约 64s；含 `TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult`（Go httptest + testdb PG + 真实 Node/Pi，SIGKILL 后注入 `ask_user` 并 `succeeded`，4.45s）。
- `pnpm --dir agent-service exec vitest run src/store.test.ts`：21 passed / 1 skipped。
- `just go-test`：历史记录通过；本轮只重跑 `./internal/agent`。
- `pnpm --dir agent-service exec vitest run`：19 files passed、1 skipped；155 tests passed、2 skipped。
- `pnpm --dir web test:run`：历史 87 files、590 tests；本轮 focused `AgentConversationComponents` / `runtime` / `agentConversationApi` 59 passed。
- `just docs-check`：通过。
- 整树 `git diff --check`：通过。
- `PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH=1 pnpm --dir web exec playwright test e2e/agent-conversation-runtime.spec.ts`：真实 Chromium 3 passed，含 PG 事件页补洞 ≤5s。
- `just go-test-live-providers`：通过，用时 201.061s（既有记录）。
- `PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH=1 ... agent-sse-reconnect.spec.ts`：真实 Chromium 1 passed；raw EventSource cursor reconnect（既有记录）。
- Luna skill eval 稳定服务后通过：15/15 scenarios（既有记录）。
- `just web-e2e-live-graph`：skip-Agent 真图 Chromium 通过（既有记录）。
- `just agent-service-test-local-journal-capacity`：10k WAL 通过（既有记录）。
- `just go-test-agent-journal-capacity`：10k 事件 + 25 Turn + 100 SSE 通过（既有记录）。
- Agent Docker 镜像因 npm registry 超时未完成，不能记为通过。
- 未宣告生产就绪：G-06 本轮 live Luna / Agent→WorkflowRun / 图片链、G-07 干净 checkout 全量 gate 仍缺。

## 决策变更

### 2026-09-05 S4-02 / S4-03：补洞保持 live EventSource

- 日期：2026-09-05
- 提出者：验收闭环对照 `runtime.test.ts` 与 Chromium 闸门
- 原条款：`sequence > cursor + 1` 时关闭旧 SSE，再从新 cursor 重连
- 替代条款：补洞期间保持当前 generation 的 EventSource，用事件页填洞并排空 buffer；仅在 protocol error、终态或退订时关闭
- 迁移影响：无持久化变更；浏览器只消费已提交 PostgreSQL 事件的合同不变
- 验收 Gate：`pnpm --dir web exec vitest run src/pages/workbench/agent/conversation/runtime.test.ts` 12 passed；`just web-e2e-agent-sse` 5 passed

暂无其它变更。任何变更必须记录日期、提出者、替代条款、迁移影响和验收 Gate；不得直接覆盖旧条款。
