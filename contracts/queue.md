# Durable 队列合同

PostgreSQL 是业务终态。Redis / broker 只负责投递。HTTP 默认不在请求里打 broker。

```text
业务行 queued + async_dispatches pending
  -> COMMIT
  -> dispatcher 将 PENDING 标 SENT 再 enqueue
  -> worker claim SENT，按 actor_name 执行
  -> 成功 / 失败 / unknown
```

- HTTP 不 enqueue broker。dispatcher 将 PENDING 标 SENT 再 enqueue。dispatcher 入队失败留在 dispatch 行与日志，不把 HTTP 打成 503。
- Worker `max_retries = 0`。asynq/Dramatiq retry 不是 WorkflowRun 状态机。
- SENT 不等于业务成功。
- Provider 调用之后不能证明结果：保持 `unknown`，不自动当失败重试。Delivery 没有 unknown。

## Actor 名

HTTP 默认入口：`run_async_dispatch`（args：`dispatch_id`, `aggregate_id`）。

内部 `actor_name`（`async_dispatches` 行上，也是历史专用 Dramatiq 名）：

| actor_name | 业务行 |
|---|---|
| `run_workflow_graph_run` | `WorkflowGraphRun` |
| `run_image_session_generation_task` | `ImageSessionGenerationTask` |
| `run_delivery_rendition_job` | `DeliveryRenditionJob` |
| `run_local_image_edit_task` | `LocalImageEditTask` |
| `run_agent_turn_sync` | `AgentTurnProjection` |

Go 不把专用 actor 作为 HTTP 默认 enqueue 路径。测试可走同一 worker 入口。
