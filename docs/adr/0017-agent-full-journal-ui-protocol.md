# ADR 0017：全量 Turn 日志与稳定 UI 协议

agent-service 的 append-only journal 全量写入 PostgreSQL（含合帧后的 `text.chunk` / `thinking.chunk`）。Go 仍是浏览器 BFF：鉴权、lease、Graph Command、Question 落库。浏览器只消费 Go 投影后的 Turn / Item / approval 通知，不直连 agent-service。

## 状态

Accepted。取代 ADR 0013 中「token 不进 PostgreSQL / 结束后用快照 fold」的直播边界；Go 作为浏览器 BFF、Pi session 不是业务权威、浏览器断连不取消 Agent 仍成立。

## 背景

ADR 0013 去掉了每条 token 写 PG 再 500ms 轮询的第二份 live journal，直播走 agent-service `waitForEvents`。代价是 PG 序列允许空洞、崩溃后流式正文无法回放、历史 Turn 只能按 `thinking_text → tool_steps → output_text` 折叠。审批曾靠 `turn.succeeded` 再 remap 成 `awaiting_confirmation`。

## 决策

- 全部 journal 事件按连续 `sequence` 批量写入 `agent_turn_events`。未知类型带 `ignorable` 时读端跳过，不再 409 卡死 Turn。
- 浏览器 SSE 只读取已经进入 `agent_turn_events` 的事件。agent-service 的本地 `/events` 路由、`streamEvents` 和 Store waiter 已删除，任何本地文件事件都不能在 PG 写入前对浏览器可见。
- Go `projectTurnEvent` 把日志翻译成稳定 UI 通知：`turn.started`、`item.*`、`approval.requested` / `approval.resolved`、`turn.completed | awaiting_confirmation | failed | canceled | unknown`。
- `turn/end(reason)` 由运行时原生发出，含 `awaiting_confirmation`。`ask_user`、workflow run 确认、artifact 确认共用 approval 形状。
- 会话 SSE 只发内容。`GET /api/v2/agent-control/events` 发 Session/Task 列表变更；图运行走节点级 `run.event`。轮询只作为 SSE 失败后的显式降级。
- 终态 Turn 的 SSE 回放 PG 日志后发 `stream.complete`。Turn 快照三列只作列表摘要和空日志兜底。超过保留期的 chunk 行把 payload 收成 `{"compacted":true}`，seq 保留。
- 浏览器发现 sequence 缺口时调用同一 scope 下的 `events/page?after=&limit=`，补齐稳定 UI 事件后再排空有界缓存。连接 generation 隔离旧 EventSource；连续三次补洞失败或缓存超过 512 条 / 1 MiB 才报告协议错误。
- `before_model_request` 建立 `agent_model_invocations` 行；`assistant/message.model_request_id` 闭合状态、时延和 provider usage。缺少 usage 记为 `unavailable`，不写伪造的零用量。
- Turn 保持 `unknown` 终态，并用 `terminal_reason_code` 区分 provider、执行中断、副作用和持久化失败。过期模型执行会把已落盘 chunk 收成 `interrupted` assistant message，再追加 `turn/end(reason_code=execution_interrupted)`。
- 工具清单生成 `recovery_policy`。恢复器只自动确认幂等账本已经证明 `applied` 的效果；`not_applied` 自动重试和无法构造 Fresh Observation 的冲突仍不自动执行。

## 后果

- 断线或进程崩溃后，已落盘的流式前缀可从游标回放；in-flight 模型请求仍标 `unknown`，不续跑。
- 历史 Turn 保持真实交错顺序，只要该 Turn 的 journal 非空。
- 证据：`go/internal/agent/sse.go`、`execution.go`、`recovery.go`、`control.go`、`compact.go`；`agent-service/src/store.ts`、`turn-runtime.ts`、`pi-runtime.ts`、`tool-manifest.ts`；`web/src/pages/workbench/agent/conversation/runtime.ts`。

## 排除方案

- 浏览器直连 agent-service。
- 恢复 ADR 0013 的 live-only denylist。
- 把 Pi session 文件当作业务权威。
