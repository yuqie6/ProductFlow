# Pi Agent Durability Rollout

## 当前状态

截至 2026-08-20，主线已经具备交互式 Agent Turn 的执行所有权、事件重放、问题 continuation、基础崩溃收敛、尚未开始模型的 queued Turn 安全 handoff、每次 provider request 的模型边界 checkpoint、Agent tool 和业务 provider effect 的显式对账能力，以及 ProductFlow `AsyncDispatch` 的常驻 recovery/dispatch 入口；真实 PostgreSQL、FastAPI、两个 Agent 进程、本机 Chrome 和单独的常驻 dispatcher 子进程已有 live gate。图片会话生成任务和 WorkflowRun 的 Prompt/Image provider 节点都已补上 provider 请求 ledger；结果不明时保持 `unknown`，不自动重放，并保留有限 provider 证据。Agent health 的后台耐久能力声明仍为 `background_durable_tasks: false`。

这份文档记录当前 checkout 的验收证据和停止条件。它不把测试替身、Pi session 文件或一次成功的模型调用当作后台执行可靠性证明。

## 已验证能力

| 能力 | 当前证据 | 结论 |
|---|---|---|
| PostgreSQL lease、heartbeat、attempt、fencing、semantic checkpoint | `backend/src/productflow_backend/application/agent/execution.py`、`backend/tests/test_agent_tasks.py`、`backend/tests/test_live_agent_execution.py` | Agent Turn 的执行所有权和过期 writer 隔离已进入主线合同 |
| Agent service 进程终止后的保守收敛 | `agent-service/src/process-restart.e2e.test.ts` | 已进入模型请求的 Turn 收敛为 `unknown`；provider 请求没有被自动重放 |
| 每次模型请求的边界和 provider 响应异常 | `agent-service/src/config.ts`、`agent-service/src/pi-runtime.ts`、`agent-service/src/pi-runtime.e2e.test.ts` | 每次实际 provider request 都写入带 attempt、fencing、连续 sequence 和 bounded request ID 的 `before_model_request` checkpoint；provider retry 关闭，并由 `AGENT_PROVIDER_REQUEST_TIMEOUT` 提供独立请求边界；本地 fake provider 已覆盖 SSE 流中断、响应头前连接重置和延迟响应超时，均只发送一次请求并以 `failed` 收口，不把部分输出标成 `succeeded` |
| 安全 queued Turn 的跨 Agent 实例 handoff 合同 | `backend/src/productflow_backend/application/agent/control.py`、`backend/src/productflow_backend/application/agent/sync.py`、`backend/tests/test_workflow_agent_service.py`、`backend/tests/test_live_agent_execution.py`、`agent-service/src/store.test.ts` | 只有过期 `claimed` 且无 checkpoint 的 queued Turn 可以用原 harness Turn ID materialize；真实 PostgreSQL、FastAPI 和两个 Node Agent 进程已验证 A 在 claim 窗口终止后由 B 接管；模型、工具和问题阶段不走此路径 |
| Pi session 上下文恢复 | `agent-service/src/pi-runtime.e2e.test.ts`、`agent-service/src/pi-runtime.test.ts` | 可恢复会话上下文；不承担模型请求 ownership 或副作用结果证明 |
| PostgreSQL event store 与 SSE cursor | `backend/src/productflow_backend/application/agent/event_stream.py`、`backend/tests/test_workflow_agent_service.py`、`web/src/pages/workbench/agent/useAgentTurnEvents.test.ts`、`backend/tests/test_live_agent_execution.py` | 浏览器断线只影响订阅；真实 Chrome 已验证首条事件后断开，再按 `after=1` 重连并只收到遗漏的终态事件 |
| 问题等待与 continuation | `backend/src/productflow_backend/application/agent/control.py`、`backend/tests/test_agent_tasks.py`、`backend/tests/test_live_agent_execution.py` | 用户回答持久化到原 projection，并创建幂等 continuation Turn；真实 PostgreSQL、FastAPI 和两个 Agent 进程已验证原 waiter 进程终止、PostgreSQL server restart 后由新进程继续执行，不依赖内存 Promise |
| AsyncDispatch consumer lease 与 worker termination | `backend/src/productflow_backend/application/async_delivery.py`、`backend/src/productflow_backend/workers.py`、`backend/tests/test_live_workflow_recovery.py` | 真实 worker 子进程 claim consumer lease 后被终止，旧 token 不能标记完成；过期扫描只产生一次新投递，新 token 才能收口同一 dispatch |
| ProductFlow 常驻 AsyncDispatch scanner | `backend/src/productflow_backend/commands/run_async_dispatcher.py`、`justfile`、`backend/tests/test_async_dispatcher_command.py`、`backend/tests/test_live_workflow_recovery.py` | `--watch` 持续执行业务 recovery 和 PostgreSQL dispatch loop，单轮异常不会结束进程，SIGINT/SIGTERM 有序退出；真实 PostgreSQL/Redis gate 已验证过期 `SENT` reconciliation 后重新发布到 Redis，并验证子进程正常退出 |
| Agent sync worker effect 的终止与重复投递 | `backend/src/productflow_backend/application/agent/sync.py`、`backend/src/productflow_backend/workers.py`、`backend/tests/test_live_agent_execution.py` | 真实 worker 在 Agent Turn 已绑定同一 harness Turn 后被终止，stale lease 重投后第二个 worker 只同步原 Turn；Agent runtime 只保留一个 Turn，provider 只收到一次请求 |
| Delivery rendition worker 的终止与重复投递 | `backend/src/productflow_backend/application/delivery_renditions/service.py`、`backend/src/productflow_backend/application/durable_recovery.py`、`backend/tests/test_live_delivery_renditions.py` | 真实 worker 在 renderer 已开始后终止，业务 recovery 重置旧 running attempt，stale dispatch 重新投递后第二个 worker 完成同一 job；最终 dispatch consumed、job 为 succeeded、attempt 为 2 且只产生一个派生资产 |
| 图片生成 provider 边界的未知结果收口与会话 effect ledger | `backend/src/productflow_backend/application/durable_recovery.py`、`backend/src/productflow_backend/application/image_sessions/service.py`、`backend/src/productflow_backend/application/image_sessions/provider_effects.py`、`backend/alembic/versions/20260820_0069_add_image_session_provider_effects.py`、`backend/tests/test_image_session_provider_seam.py`、`backend/tests/test_queue_recovery.py`、`backend/tests/test_live_generation_attempt_fencing.py` | stale running task 只有在 provider 前的 `running` 或已保存候选的 `candidate_saved` 阶段才会重新入队；已记录 `active_candidate_index`、`provider_polling` 或其他越过 provider 边界的任务转为终态 `unknown`，对应 provider 请求 ledger 的 pending 记录也转为 `unknown`；一次 provider 请求对应一个稳定 operation key，批量候选共享同一请求记录；受保护 reconciliation 只查询已支持的 provider 记录，不发起新的生图请求，裁决不改写原始任务终态。真实 provider response-loss 对账仍需 gate |
| schema-v3 graph 运行恢复 | `backend/src/productflow_backend/application/product_workflow/graph_execution.py`、`backend/src/productflow_backend/application/durable_recovery.py`、`backend/tests/test_live_workflow_recovery.py` | 滞留的 `WorkflowGraphRun` 按 `run_workflow_graph_run` 重新入队。V2 `WorkflowProviderEffect` / `v2_execution.py` 已随 `20260821_0080` 删除；v3 graph run 的 provider ledger 与 unknown 对账仍待补 |
| Redis server restart 后的已发送投递 | `backend/tests/test_live_workflow_recovery.py`、`justfile` | 真实 `productflow-redis` 容器重启后，已发送消息可由重建的 broker 连接取回并按 PostgreSQL lease 完成消费 |
| 常驻 Dramatiq worker 的 Redis 连接恢复 | `backend/tests/test_live_workflow_recovery.py`、`justfile` | 真实常驻 `Worker` 在 Redis 容器重启前后分别消费消息；连接中断触发 consumer 重建，worker 进程继续运行并消费重启后的消息 |
| `request_workflow_run_v1` 和 `create_product_workspace_v1` 对账 | `backend/src/productflow_backend/application/agent/effect_reconciliation.py`、`backend/tests/test_agent_effect_reconciliation.py`、`agent-service/src/tools.test.ts`、`backend/tests/test_live_agent_effects.py` | `unknown` 副作用可通过受保护入口得到持久化的 `applied`、`failed` 或 `unknown` 裁决；真实 FastAPI + PostgreSQL 已验证响应体丢失后使用同一幂等键查询对账且重复请求只保留一份业务事实；入口不重新发出 mutation |
| Agent 素材重命名的 mutation ledger 与对账 | `backend/src/productflow_backend/application/agent/tools.py`、`backend/src/productflow_backend/infrastructure/db/models.py`、`backend/tests/test_workflow_agent_service.py`、`backend/tests/test_live_agent_effects.py` | 真实 FastAPI + PostgreSQL 已验证响应体丢失后由 `AgentToolMutation` 账本裁决为 `applied`，重复请求复用同一结果且同一 conversation/key 只保留一条账本；没有匹配账本时仍保留冲突边界 |
| ProductFlow 业务 authority | `agent-service/src/tools.ts`、`backend/src/productflow_backend/application/agent/product_workspaces.py`、`docs/adr/0007-pi-agent-runtime-boundary.md` | Agent service 只能通过受控 ProductFlow Tool/application 执行业务动作，Pi 不直接拥有业务数据 |

## 尚未通过的生产 Gate

- 完成真实部署拓扑下的 PostgreSQL、Redis 和多个 Agent service 进程故障矩阵，覆盖 heartbeat/fencing 竞争、旧 worker 恢复和事件追加竞争；当前已用真实 PostgreSQL、FastAPI 和两个 Node Agent 进程验证 queued handoff、问题 continuation、旧 owner fencing 和 event sequence 竞争，并已验证 Redis publish failure 后的 PostgreSQL stale reconciliation、AsyncDispatch consumer worker 被终止后的 consumer lease stale reconciliation 和一次重投、Redis server restart 后的已发送消息恢复、常驻 Dramatiq worker 在 Redis 连接中断后的 consumer 重建，以及常驻 dispatcher 对 stale `SENT` dispatch 的恢复投递；dispatcher/consumer 重连竞态、常驻 scanner 与 worker 的多实例竞争和完整部署级矩阵仍需 gate。
- 使用真实 provider 验证模型请求超时、连接中断、响应丢失、进程终止的故障矩阵；当前 Agent service 已配置独立 provider request timeout，并有本地 fake provider 覆盖响应头前连接重置、延迟响应超时和 SSE 流中断，只证明 adapter 的保守收口和不重放，不证明真实外部 provider 语义。
- 对每一种真实副作用完成 response-loss、worker termination、重复 delivery 和业务查询对账验证。当前 Agent effect、Agent tool mutation、WorkflowRun provider effect 和图片会话 provider effect 都已有持久 ledger/对账入口；真实 FastAPI/业务 mutation response-loss、稳定幂等键及 Agent ledger 对账已通过，WorkflowRun 和图片会话 ledger 已通过 SQLite/真实 PostgreSQL schema、provider 边界与 fake provider 对账 gate。通用 AsyncDispatch consumer worker termination、旧 lease fencing、Agent sync worker 和 delivery rendition worker 的终止后重复投递已通过。真实外部 provider 查询、图片生成的真实 response-loss、素材处理 response-loss 和其他未来暴露的副作用仍需各自 gate。
- 在真实运行的 FastAPI、Agent service 和浏览器之间验证 SSE 断开、重连、cursor 重放、页面刷新和 terminal 收敛；Chrome live gate 已覆盖断开、重连、cursor 重放、页面刷新和终态收敛，浏览器会话恢复及真实部署拓扑仍需 gate。
- 验证进程或数据库重启后 durable question continuation 的完整部署路径，包括等待问题、用户回答、continuation 提交和旧 Turn 收口；真实 PostgreSQL server restart gate 已覆盖旧 waiter 终止、连接池恢复、过期 execution recovery、用户回答和 continuation 终态，常驻 dispatcher 的完整多实例调度、真实部署切换和运维路径仍需 gate。
- 完成部署级 crash/network/provider 故障矩阵、监控告警、unknown 对账操作流程和上线停止条件。

## 保持的运行边界

- `background_durable_tasks` 是 Agent health 的能力声明，不是运行时 feature flag。在上述真实 gate 形成部署证据前，该声明继续为 `false`，不能通过改值扩大后台执行承诺。
- 只有尚未进入模型调用的 queued Turn 可以自动重新入队或在恢复 scanner 判定安全后由新 Agent 实例 handoff。
- Agent service 只有在本地没有 execution attempt/fencing 证据时才会自动重新入队；已经 claim 但本地 snapshot 仍为 queued 的 Turn 交由 ProductFlow recovery scanner 判断。
- 已进入模型、工具、外部任务或未知副作用阶段的 Turn 不自动重放；无法证明结果时保持 `unknown`。
- 图片生成任务在 provider 边界之后无法证明结果时保持终态 `unknown`，不自动重试；当前用户需要通过 provider effect reconciliation 查询已支持的 provider 记录，再决定是否创建新的业务操作。图片会话和 WorkflowRun 的 reconciliation 都只写 ledger，原始 unknown task/run 不被改写为成功，也不自动重放原请求。
- `AgentTurnEffectReconciliation` 和 `WorkflowProviderEffect` 只裁决副作用，不把原始 `unknown` Turn/WorkflowRun 改写为模型轮次或工作流成功，也不替代后续 continuation Turn。
- WorkflowRun、图片生成、素材处理和 delivery rendition 继续由 ProductFlow 的 PostgreSQL、Redis、Dramatiq 和业务 worker 承载。

## 验收记录

截至 2026-08-20，本轮 checkout 已完成以下验证：

- `pnpm --dir agent-service test -- --runInBand`：8 个测试文件、45 个测试通过；覆盖了每次 provider request 的模型 checkpoint、本地响应流中断、响应头前连接重置和延迟响应超时后的单次请求与 `failed` 收口、当前两类副作用的 HTTP 响应体丢失后稳定幂等键对账、本地 queued snapshot 已带 execution attempt/fencing 身份时交由 ProductFlow recovery scanner 判断，以及使用 ProductFlow 指定 harness Turn ID materialize 的边界。
- `pnpm --dir agent-service build`：通过。
- `pnpm --dir web test:run`：57 个测试文件、284 个测试通过。
- `pnpm --dir web lint`、`pnpm --dir web build`：通过。
- `uv run --directory backend ruff check .`：通过。
- `uv run --directory backend pytest -q`：538 个测试通过、34 个 skip；最终完整复跑通过，输出包含 63 个依赖弃用警告。
- `PRODUCTFLOW_RUN_LIVE_AGENT_EXECUTION=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_agent_execution.py`：4 个 PostgreSQL live gate 通过；覆盖两个独立 session 并发 claim 和 event sequence 竞争的一方成功、一方冲突、真实 FastAPI + 两个 Node Agent 进程的 queued handoff、旧 owner 的 heartbeat/event/checkpoint/release fencing、问题等待进程终止和 PostgreSQL backend connection termination 后的 continuation，以及本机 Chrome 的真实 SSE 断开、页面刷新和 `after` cursor 重连。
- `just backend-test-live-agent-question-postgres-restart`：1 个真实 PostgreSQL server restart question gate 通过；覆盖旧 Agent waiter 终止、实际 PostgreSQL 容器重启、连接池重建、过期 execution recovery、新进程回答和 continuation 终态。
- `just backend-test-live-agent-worker-effects`：1 个真实 Agent sync worker effect gate 通过；覆盖 worker 在原 harness Turn 已创建后终止、stale lease 重投、第二个 worker 收口同一 dispatch、runtime 只保留一个 Turn 和 provider 单次请求。
- `just backend-test-live-delivery-rendition-worker-effects`：1 个真实 PostgreSQL + Redis delivery rendition worker gate 通过；覆盖 renderer 已开始后的 worker 终止、业务 recovery 重置 stale running attempt、同一 delivery dispatch 重投、第二个 worker 完成和派生资产唯一性。
- `just backend-test-live-generation-attempt-fencing`：8 个真实 PostgreSQL generation attempt gate 通过；覆盖旧 attempt fencing、stale image task 的 CAS 恢复、已进入 `provider_polling` 且保留 provider response 证据的任务转为 `unknown`、WorkflowRun provider effect 已越过调用边界后转为 `unknown` 且不重新入队，以及真实 PostgreSQL enum/进度字段迁移后的执行。
- `just backend-test-live-agent-effects`：1 个真实 FastAPI + PostgreSQL effect gate 通过；覆盖 `create_product_workspace_v1`、`request_workflow_run_v1` 和 Agent 素材重命名的 mutation 响应体丢失、同一幂等键 reconciliation、重复请求、`AgentToolMutation` 账本唯一性和业务事实唯一性。
- `just backend-test-live-agent-postgres-restart`：1 个真实 PostgreSQL server restart gate 通过；覆盖容器重启后的 SQLAlchemy pool reconnect、Agent execution 事实保持和过期 lease recovery。
- `just backend-test-live-recovery`：5 个 PostgreSQL+Redis recovery live gate 通过；覆盖 Agent Turn projection 从 PostgreSQL recovery 经 AsyncDispatch 投递到真实 Redis broker、Redis publish failure 后保留 `SENT`、stale reconciliation 后只重新投递一次，以及 consumer worker 被终止后旧 lease 被拒绝、新 lease 完成一次重投。
- `just backend-test-live-agent-dispatcher-watch`：1 个真实 PostgreSQL + Redis 常驻 dispatcher gate 通过；覆盖独立 `--watch` 子进程执行 stale `SENT` reconciliation、重新发布同一 AsyncDispatch，并在 SIGTERM 后正常退出。
- `just backend-test-live-agent-redis-restart`：1 个真实 Redis server restart gate 通过；覆盖已发送的 AsyncDispatch 在实际 `productflow-redis` 容器重启后由重建的 broker 连接取回，并使用 PostgreSQL consumer lease 完成收口。
- `just backend-test-live-agent-redis-connection`：1 个真实常驻 Dramatiq worker Redis connection interruption gate 通过；覆盖 Redis 容器重启导致的 consumer 连接中断、consumer 重建和同一 worker 继续消费重启后的消息。
- `just backend-test-live-async-delivery`：1 个 PostgreSQL async delivery live gate 通过。
- 本机 smoke check：PostgreSQL、Redis 容器健康；FastAPI `/healthz` 和 Agent service `/healthz` 均返回 200；Agent health 明确返回 `background_durable_tasks: false`、`active_turns: 0`、`queued_turns: 0`。
- `just backend-migrate`：真实开发数据库完成 `20260820_0068 -> 20260820_0069`；当前 head 为 `20260820_0069`，数据库同时包含 `workflow_provider_effects` 和 `image_session_provider_effects` provider effect ledger。
- `just docs-check` 和 `git diff --check`：通过。

这些结果证明当前实现、PostgreSQL lease 基础 gate、真实跨进程 queued handoff、PostgreSQL server restart 后的 question continuation、Chrome SSE cursor 重连和页面刷新、Redis 长任务投递及 publish failure 后的数据库收敛、consumer worker termination 后的 stale lease 重投、Redis server restart 后的 broker 连接恢复、常驻 Dramatiq worker 的 Redis consumer 重建、常驻 dispatcher 的 stale dispatch recovery/re-publish、Agent sync worker 和 delivery rendition worker 终止后的重复投递收口、每次模型请求的 checkpoint、本地 provider 响应头前连接重置、延迟超时和响应流中断后的保守收口、图片会话与 WorkflowRun provider 边界之后的未知结果不重放、两类 provider effect ledger 的 SQLite/真实 PostgreSQL 迁移和受保护 reconciliation 入口，以及当前 Agent effect 和 Agent tool mutation 在真实 FastAPI response-loss 下的稳定幂等键或账本对账可工作。它们没有覆盖真实 provider 响应丢失和 provider 侧查询对账、图片生成和素材处理等具体 effect worker termination 和重复 delivery、非 Responses provider 的 provider 查询能力、dispatcher/consumer 重连竞态、完整部署级多实例竞争或完整运维切换；跨进程 gate 中的 provider 仍是本地 fake/failure 响应。仅有单元测试、fake provider 或单进程 smoke check 通过时，不能把 `background_durable_tasks` 的能力声明改为 `true`。
