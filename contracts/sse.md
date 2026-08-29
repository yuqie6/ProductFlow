# Agent Turn SSE 合同

浏览器只消费 ProductFlow 的 SSE，不直连 Agent service 文件流。

- `Content-Type`：`text/event-stream`
- 头：`Cache-Control: no-cache, no-transform`，`Connection: keep-alive`，`X-Accel-Buffering: no`
- 事件块：

```text
id: <sequence>
event: <kind>
data: <json>

```

- `data` JSON 字段：`schema_version`、`run_id`、`turn_id`、`sequence`、`created_at`、`kind`、`payload`
- 心跳：`: heartbeat\n\n`，约 0.5s 一次
- Cursor：查询参数 `after` 与 `Last-Event-ID` 取较大值；只重放 PostgreSQL `agent_turn_events`
- 浏览器断开只停止本次 generator，不取消 Turn
- Turn 进入终态且没有新事件后结束流
