# ADR 0013：Go 是浏览器 BFF，不是 live journal

浏览器 Agent 对话的直播字节来自 `agent-service` 进程内 `waitForEvents`。Go 继续做 cookie 鉴权、Turn 归属、Graph Command、Question 落库和 lease；它不再把每条 `text.delta` / `thinking.delta` 写成 PostgreSQL `agent_turn_events`，也不再用 500ms `ListEvents` 轮询当直播。

Pi session 文件仍只服务模型 loop。ADR 0001「不能拼第二份供模型调用的 transcript」仍然成立。ADR 0005 的有界工具步骤和思考投影仍然成立；「薄投影 = 不要 UI journal」不再作为直播路径的读法。

## 状态

Accepted

## 背景

ProductFlow 不是 Harness：商品、图、cookie、Graph Command 在 Go，Pi loop 在 Node `agent-service`。浏览器直连 agent-service 会再做一遍 session/ACL，工具副作用仍打回 Go。真正多余的是第二份 live journal：

```
Pi → agent-service 本地 log + waitForEvents
  → HTTP AppendEvent（lease + 一行 PG）
  → Go SSE 每 500ms ListEvents
  → 浏览器
```

Turn 投影已有 `output_text` / `thinking_text` / `tool_steps`。进程崩溃时 in-flight Turn 标 `unknown`，token 行救不回模型请求。

## 决策

- 浏览器 URL 不变：`GET /api/v2/agent-conversations/:id/turns/:projection_id/events`。Go 验 session 和归属后，把内部 `streamEvents`（`waitForEvents`）原样写成 EventSource。`sequence` / `Last-Event-ID` / `after` 是 **runtime cursor**。
- `publishDurableEvent` / `AppendEvent` 只收低频控制事件：`question.*`、`tool.step` 状态变化、`turn.*` 终态、lease 需要的 checkpoint。`text.delta`、`thinking.delta`、`assistant.finish` 不进 PostgreSQL。Turn 行继续更新 `output_text` / `thinking_text`，供列表和结束后的静态渲染。
- 进行中重连：把 cursor 交给仍活着的 agent-service。结束后重连：没有 live stream，用 Turn 快照 + 已持久化的控制事件 fold，不回放 token 行。
- Pi `@earendil-works/pi-ai` 0.83 `assistantMessageEvent` 在 `agent-service/src/pi-chunks.ts` 归一。UI 只认 ProductFlow chunk。未知 type 丢掉并打日志。`toolcall_*` 原始 arguments 不进 UI；工具卡仍是 ProductFlow `tool.step`。
- 前端 `web/src/pages/workbench/agent/conversation/`：Assembler + animation-frame 发布 + keyed 节点。流式正文轻量渲染，settled 后再 GFM。

## 后果

- 直播延迟接近 Pi → agent-service，不再固定多 500ms + 一次 HTTP/PG。
- PostgreSQL `agent_turn_events` 允许 sequence 空洞（runtime 序号仍连续，durable 子集递增）。
- 主线不保留旧 token 行双序列化。

## 排除方案

- 浏览器直连 agent-service。
- 继续每 token 写 PG，再用 LISTEN/NOTIFY 优化轮询。
- Cordis / Slot / Typert，或把 filesystem 工具卡搬进 ProductFlow。

## 证据

- `agent-service/src/pi-chunks.ts`、`store.ts`、`pi-runtime.ts`
- `go/internal/agent/sse.go`、`gateway.go`、`execution.go`
- `web/src/pages/workbench/agent/conversation/`
- `just agent-service-test`、`just go-test`、`pnpm --dir web test:run`
