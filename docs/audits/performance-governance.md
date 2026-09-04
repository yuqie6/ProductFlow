# 平台可靠性组：执行、性能与生产 Gate

本账本记录 ProductFlow 当前运行时的性能模型、容量边界、锁与事务约束、可执行的优化顺序和验收方法。它服务于 Graph、Agent、连续生图、异步投递、SSE 和数据库查询的跨层改动。

生产 Gate 的详细合同保存在本文 [生产 Gate](#production-gates) 节；[历史时间线](../history/agent-runtime-timeline.md#runtime-ownership-evidence) 保留已关闭所有权重构的证据。当前代码、测试和真实运行仍是最终证据；账本中的目标、预算和未验证项必须标明状态。

**平台可靠性组章程。执行以已发布 issue 为界。** 合并原性能与生产可靠性职责，接收 journal 职责调查。当前任务、认领和阻塞见 [Issue 看板](tasks/README.md)。GraphRun 摘要/详情、SSE fanout、recovery 有界批次已经接线。详情、时延与容量指标已归档；PERF-12 仍缺目标规模 payload，本轮不发后续单。时延采证完成不表示单副本 p95 达标。

## 组职责与交接

- 负责 lease、journal、ACK、恢复、队列、锁序、容量、查询成本和实时通道基础；同一执行链的正确性与性能由本组统一权衡，不拆成两组分别验收后互相覆盖。
- 生产就绪 S1–S6 和已通过 Gate 保留历史基线，不重新开工。G-06 行为部分引用评测组可采信 run_id；G-07 必须绑定候选 checkout，全量门不能借用旧 HEAD 的结果。主代理汇总全部相关结果作发布裁定。
- AR-02 由 [arch-journal-assessment](tasks/archive/arch-journal-assessment.md) 于 2026-09-05 完成调查。结论：保留现状，不发布 journal 实现 issue。在线路径已拆出 batcher / 回执等式 / ACK 文件；重启前缀确认与 claim、phase、abandon 同属 `recoverDurableHandoff`，单独吸收 confirm 循环不会删掉 TurnRuntime 必知分支。指定 Node 测试 61 通过 / 1 跳过（10k WAL `runIf`）；未证明数据丢失。Go ConfirmEvents / fencing 与 Node recover claim 409 仍是改协议时才补的缺口，不构成开工许可。
- journal 合同保持 PG 权威、在线 append 的 lease 检查、confirmation 不 claim/续租、不重放丢失模型执行。权限与失败矩阵见归档 issue；不重启历史运行时所有权计划。
- 2026-09-05 新发现：[agent-question-answer-identity](tasks/agent-question-answer-identity.md) 承接 L3 live `20260904T234034Z-93b42b6d` 中同一 turn 第二个不同问题的答案被报冲突。首个回答已恢复执行，第二问仍未通过；既有单问题恢复证据不覆盖此场景，待生产答案身份边界修复。该发布没有修改生产代码，不改变历史 Gate 采证基线。
- 画布草稿基线、文稿采用规则与结果使用行为交工作流体验；Skill 行为交 Agent 能力；题库、grader、图片闸门交评测。跨层修复由维护者指定一张主 issue 的完整因果范围，不按目录拆断事务或权限合同。

## 使用规则

- 复核基线：2026-09-01 的运行时锁、Redis 和 SaaS 审计，以及其后的性能治理工作树改动。
- `完成`：当前 owner 已明确，相关行为有代码和测试证据，迁移或压测缺口已记录。
- `部分完成`：主路径已有实现，但仍有边界、批量、指标、迁移或专项测试缺口。
- `待实施`：已有明确方案，尚未改变运行时行为。
- `观察`：需要真实负载或生产数据确认，禁止只凭代码阅读宣布解决。
- 每个性能改动都要写清楚：受影响的请求/任务、数据库查询或锁、数据权威、容量上限、回滚方式和验证命令。
- 性能改动不能引入第二份业务权威。缓存、Redis、NOTIFY 和内存状态只能承担唤醒、调度或易失的 admission；业务状态继续以 PostgreSQL 为准。
- 事务、队列、schema、HTTP DTO 和前端列表合同属于同一个变更面。只改其中一层会留下新的放大器或漂移。

## 当前基线

### 运行单元

ProductFlow 当前是单管理员、单商家工作区，运行单元包括 React/Vite Web、Go API、Go worker、Go async dispatcher、Node.js/Pi Agent service、PostgreSQL、Redis 和本地媒体 storage。SaaS 的 tenant、计费、对象存储和真实用户身份仍属于新基线设计，见 `docs/ROADMAP.md`。

| 部分 | 当前行为 | 性能含义 | 权威 owner |
|---|---|---|---|
| 业务状态 | GraphRun、NodeRun、Agent Turn、ImageSession task、Delivery job 和 LocalEdit task 写 PostgreSQL | 读写会占用连接和事务；不能用 Redis 状态替代 | 对应 `go/internal/*` 包 |
| 异步投递 | API 只写业务行和 `async_dispatches=PENDING`；dispatcher 标 SENT 后写 Redis asynq；worker 回写 PostgreSQL | Redis 丢信封时依赖 PostgreSQL 对账；broker 重试不是业务重试 | `go/internal/platform/queue` |
| Redis | 当前主要承担 asynq broker 和投递唤醒 | 没有缓存、租户公平队列、限流或分布式 SSE 总线 | `productflow-dispatcher`、`productflow-worker` |
| Graph 执行 | 一个 GraphRun 由一个 worker 入口处理；互不依赖节点可以并发打 provider；执行权由 GraphRun 行上的 token/expiry lease 围栏 | lease 默认 35 分钟、每 5 分钟续租；同一进程仍有快速 mutex，跨实例不钉住长生命周期 SQL connection | `go/internal/graph`、`workflow_graph_runs` |
| Agent 执行 | PostgreSQL lease、fencing 和 journal 为权威；Node/Pi 只负责 adapter 和私有 WAL | lease 行锁只在短事务内；`AppendEvents` 先批量读取 batch 内已有 sequence，容量 gate P95 需持续观测 | `go/internal/agent`、`agent-service` |
| 连续生图 | Graph 与 ImageSession 共用全库 `generation_max_concurrent_tasks` 和 advisory capacity lock | 当前默认上限 3，所有商家共享一个 admission 点 | `graph/durability.go`、`imagesession/execute.go` |
| SSE | Agent、Graph、ImageSession 的 PostgreSQL NOTIFY 在同一进程按 pool 共享 fanout；通知丢失后按游标或状态轮询 | 每个有活跃订阅的 API 副本按 pool 通常持有 1 条 listener（无订阅时为 0）；不是每个浏览器一条连接 | `platform/notify` 与各 SSE handler |
| 认证与数据范围 | 管理员 session 是签名 cookie 布尔状态，没有 user/tenant id | 没有挂载租户限流、配额、审计和数据隔离的主键 | `auth`、`httpx/session` |

### 已知硬边界

以下数值是当前代码合同，不应在性能改动中悄悄改变：

| 项目 | 当前值或规则 | 代码锚点 |
|---|---:|---|
| dispatcher 默认投递批次 | 每轮 100 条，`--limit` 可调 | `platform/queue/actors.go`、`cmd/productflow-dispatcher/main.go` |
| dispatcher dispatch 间隔 | watch 模式默认 1 秒 | `cmd/productflow-dispatcher/main.go` |
| dispatcher recovery 间隔 | watch 模式默认 10 秒，首轮立即执行；`--recovery-interval` 可调 | `cmd/productflow-dispatcher/main.go` |
| asynq worker 并发 | 4 | `cmd/productflow-worker/main.go` |
| asynq task timeout | 30 分钟，`MaxRetry=0` | `platform/queue/asynq.go` |
| worker consumer lease | task timeout 加 5 分钟，即 35 分钟 | `platform/queue/actors.go` |
| PostgreSQL outbox 最大业务尝试 | 10；`ErrBusy` 退避 2 秒，`ErrLater` 退避 1 秒 | `platform/queue/actors.go`、`consume.go` |
| Agent recovery 候选 | dispatcher 默认每阶段最多 25 条；queued Task 与 pending projection 按活跃 outbox 过滤；summary 返回 `has_more` | `agent/recovery.go`、`cmd/productflow-dispatcher/main.go` |
| Graph/ImageSession/Delivery/LocalEdit recovery | 各域候选每轮最多 25 条，`SKIP LOCKED`，稳定时间/id 排序 | 各域 `recovery.go` |
| Agent SSE 上限 | 100 条，按进程计数 | `agent/dto.go`、`agent/sse.go` |
| Agent Session 列表 | cursor page，limit 1-100 | `agent/sessions.go` |
| GraphRun 列表 | 最近 20 条轻量摘要；node runs 只含状态/进度，snapshot、输入追踪和输出由单 run 详情读取 | `graph/service.go`、`graph/run_dto.go` |
| ImageSession 列表 | 按 `updated_at DESC, id DESC` 游标分页，默认 20、最大 100；摘要查询批量聚合 | `imagesession/service.go`、`imagesession/cursor.go`、`serialize.go` |
| 图片生成容量 | `generation_max_concurrent_tasks` 默认 3，限制 1-20 | `graph/durability.go`、`settings/catalog.go` |
| Agent service turn 并发 | 默认 3，进程内控制 | `agent-service/src/config.ts`、`pi-runtime.ts` |

## 当前治理状态

以下状态以当前工作树为准；最终完成状态必须在相关 Gate 通过后更新。

| ID | 项目 | 状态 | 当前措施与证据 | 剩余缺口 |
|---|---|---|---|---|
| PERF-01 | Graph 行锁反向边 | 部分完成 | Graph 事件追加收口为 `appendGraphRunEventLocked`；run mutation 使用 `run -> graph -> node` 或 `run -> node -> effect`；文稿自动采用改为先锁 run 再锁 live graph；`TestConcurrentCancelExecuteRecoveryDoesNotDeadlock` `-count=20` 通过 | 未单独断言自动采用结果；目标规模锁等待仍缺 |
| PERF-02 | Agent Task 与 Turn projection 反向边 | 部分完成 | 全局 Draft 确认先锁关联 projection，再写 conversation / Task；`TestConfirmDraftAndAppendTerminalDoNotDeadlock` 与既有 append/claim 锁序测试 `-count=20` 通过 | 继续核对稳定架构文档的 owner 表 |
| PERF-03 | node/run 隐式反向边 | 部分完成 | `failBlockedQueuedNodes` 先锁 run，再改 node 并追加事件；事件 helper 不再隐藏获取 run 锁；`TestConcurrentCancelExecuteRecoveryDoesNotDeadlock` 覆盖 cancel、ExecuteRun、recovery，`-count=20` 通过 | 目标规模锁等待仍缺 |
| PERF-04 | 生图容量锁顺序 | 部分完成 | Graph 为 `capacity advisory -> run -> node`；ImageSession claim 改为 `capacity advisory -> task`，取得 task 锁后重新核对状态。`just go-test-staging-field` 中 `TestReplicaFieldTwoWorkersRespectGenerationCapacity`：上限 1 时两 worker 同时 claim，1 running / 1 waiting_for_capacity | 全库单钥匙和 noisy neighbor 仍存在；SaaS 前需按 workspace/tenant 重构 |
| PERF-05 | Agent 与业务 recovery 长事务 | 部分完成 | Agent 过期 execution / queued Task / pending restage 已分阶段且每聚合一事务；`HasMore` 为各阶段 OR。Graph/ImageSession/Delivery/LocalEdit 为发现快照 → 单聚合状态 → 单聚合 outbox；每轮最多 25 条，`SKIP LOCKED`，单条失败不回滚整批。Graph 取消/执行/recovery 锁序 `-count=20` 已跑 | 仍需目标规模锁等待分布 |
| PERF-06 | dispatcher recovery 拖慢投递 | 部分完成 | dispatch/recovery cadence 解耦，默认 1s/10s；PENDING 写入同事务 NOTIFY，陈旧 SENT 对账最多 100 条；双副本 SKIP LOCKED 与 SIGKILL 幸存已验。`a0d1ba4f` 新增[真实投递时延采证](tasks/archive/perf-dispatcher-latency.md)：独立 PG 库/Redis，500 条突发 + 25 条延期，三轮单副本 PENDING→SENT p95 2.540–2.594s，双副本 0.772–0.784s；500/500 信封各一次，延期均未认领 | 单副本仍未达建议 p95 < 1s；后两批 claim 间隔约 1s，需调查 backlog 下通知合并/ticker 的批间等待；未到期唤醒仍跳过；无生产 SLO 或重 recovery 负载结论，`just staging-up` 杀容器未跑 |
| PERF-07 | SSE 连接占用 | 完成 | `platform/notify.Subscribe` 按 pool 在进程内共享一条 LISTEN，Agent/Graph/ImageSession 共用 fanout；`ListenerConnections`、`GraphSSEConnections`、Agent SSE gauge 已接入 API metrics；缓冲满时丢通知并依赖 PG 回读；notify/metrics/Agent shared-listener tests 通过。`TestReplicaFieldTwoAPIPoolsKeepIndependentLISTEN`：两个 pool 两条 LISTEN，停 A 后 B 仍收到 Run 通知；`TestReplicaFieldTwoListenersSurviveNotifyLoss`：dispatcher LISTEN 副本 A 取消后 B 仍被唤醒 | 指标是单进程 gauge；部署仍需按副本抓取并用 `replicas * (listener + SSE) + pool` 做容量告警；docker 杀 API 副本未跑 |
| PERF-08 | ImageSession 列表 N+1 | 部分完成 | 列表批量摘要、首屏 history 与 `GET /history` keyset 保留。详情活动/近期终态/首屏轮次任务各 LIMIT 20，去重最多 60；队列位置由 PG 排名后只返回所需 ID。`232e51f7` 包内回归与 query-plan 通过；25k 会话 / 10k 轮次 / 1k 任务下，三组任务返回 10/20/20 条，execution 0.037/0.059/0.133ms。[任务证据](tasks/archive/perf-imagesession-detail.md) | COUNT 仍 Seq Scan（1.552ms），首屏关联任务查询仍 Seq Scan；真实负载端到端 payload/延迟待观察，不把返回有界等同于扫描成本有界 |
| PERF-09 | GraphRun 列表 N+1 与排序 | 完成 | run/node 摘要批量读取；`(graph_id, started_at DESC, id DESC)` 索引已写入并完成本地 schema migration；列表 DTO 排除 snapshot、compiled context、input trace、output，单 run 详情按需读取；Graph projection 的 artifact、商品资料/fact、绑定资产和视觉版本改为批量投影；执行 loop 复用事务内完整 run；Graph SSE 建连/fallback 只读 status；HTTP 100 样本、目标规模 EXPLAIN、Playwright TTI/重复读取/详情打开和 Web bundle budget 均通过 | 生产详情打开率和跨副本连接预算需要部署后按 metrics/trace 观察；摘要仍按最近 20 条返回，`progress_metadata` 仍是列表所需的 JSON 读取 |
| PERF-12 | Agent Session 列表 N+1 | 部分完成 | 当前页 session、每个 session 最近 20 条 conversation 和 count 改成批量查询；21 条 conversation limit regression 通过。列表按维护列 `activity_at` 排序；25k 会话 `just go-test-agent-query-plan` dock/product 列表无 Seq Scan，execution 0.040ms / 0.042ms | 未测目标规模 payload；单条详情路径仍按一个 session 组装三类数据 |
| PERF-13 | Agent journal batch 写入 P95 | 部分完成 | `AppendEvents` 将 batch 内已有 sequence 从逐条 `Take` 改为一次查询，保留 projection lock、幂等 replay 和逐条 fold；历史容量 gate P95=240ms/211ms，实现基线 `fb658633` 在 2026-09-04 复核 P95=232.800884ms，100 SSE 通过 | 需要持续 batch histogram、目标规模负载与锁等待观测 |
| PERF-10 | tenant 和真实身份 | 待实施 | 当前明确维持单管理员、单商家合同，不提前给现有 live 表补 tenant_id | SaaS 起点需同时设计 principal、数据范围、配额、审计和存储，不做局部补丁 |
| PERF-11 | Graph 长生命周期 advisory | 完成 | GraphRun 行新增 token/expiry execution lease；CAS 接管过期 lease，5 分钟续租，所有状态/产物收口写在 lease 围栏下；同进程重复执行仍由 mutex 快速拒绝；过期接管与迟到 provider 结果测试通过 | 需要目标规模观察 lease 接管、续租失败和 recovery 锁等待；固定 35 分钟 lease 必须继续与 worker consumer lease 一起调整 |

## GraphRun 读路径专项审计（2026-09-01）

本节记录工作台运行历史性能回归的因果链、修复范围和验收证据。它是 `PERF-09` 的专项账本，不把旧 Python 对照服务写成当前产品运行时，也不把单次本地响应写成生产 SLO。

### 审计结论

当前代码 HEAD 已完成 GraphRun 列表的摘要/详情合同拆分、Graph projection 的批量读取和 Graph SSE 的状态读取边界。修复后 Go HTTP gate、目标规模摘要/详情 query plan、浏览器冷/热启动、重复请求、按需详情打开和 Web bundle budget 均已通过；生产详情打开率与跨副本容量仍属于部署后观察项，因此本节把代码验收与运行观测分开记录。

本次专项包含四条读取路径：

1. 工作台运行历史：`GET /api/v3/products/{product_id}/workflows/{workflow_id}/runs`。
2. 单次运行详情：`GET /api/v3/products/{product_id}/workflows/{workflow_id}/runs/{run_id}`。
3. Graph 运行事件：`GET /api/v3/products/{product_id}/workflows/{workflow_id}/runs/{run_id}/events`。
4. 工作台前置读取：当前图、运行历史、商品列表，以及前端组件首次可操作时间。

不在本次范围内：provider 调用时延、媒体字节传输、Graph 执行锁序和 Redis broker 吞吐。这些路径仍按本文件其它条目验收。

### 修复前基线

以下数据来自 2026-09-01 本地 loopback HTTP 复测：warmup 5 次、正式样本 40 次、串行请求、当前 dev PostgreSQL。它们是 `8d47a55a` GraphRun 摘要/详情拆分前的基线，原始临时输出在 `/tmp/pf-ab/`，未作为仓库 artifact 保存。

| 路径 | 栈 | 样本 | p50 | p95 | 响应体 | 说明 |
|---|---|---:|---:|---:|---:|---|
| `/api/v3/products/{id}/workflows/current` | Go | 40 | 8.05ms | 9.92ms | 34,514B | 当前图 projection，不是运行历史 |
| `/api/v3/products/{id}/workflows/{graph}/runs` | Go | 40 | 40.77ms | 43.18ms | 67,705B | 修复前完整 run/node projection |
| `/api/products/{id}/workflow/status` | 旧 Python 对照 | 40 | 24.43ms | 26.52ms | 28,704B | 历史对照接口，字段和数据形状不同 |
| `/api/v2/products?page=1&page_size=20` | Go，串行 | 40 | 3.06ms | 4.39ms | 未记录 | 商品列表基线 |
| `/api/v2/products?page=1&page_size=100` | Go，串行 | 40 | 3.35ms | 4.26ms | 未记录 | 商品列表大页基线 |
| `/api/v2/products?page=1&page_size=20` | Go，20 并发 | 20 | 19.73ms | 25.76ms | 未记录 | 并发批次基线 |
| `/api/products?page=1&page_size=20` | 旧 Python 对照，20 并发 | 20 | 未记录 | 492.08ms | 未记录 | 只用于说明历史实现差异，不作为 Go 目标 |

旧 Python 服务是 benchmark 对照工作树，当前生产主线仍是 Go API；A/B 结果必须标记为历史参考。Go `/runs` 与 Python `/workflow/status` 的返回字段、run/node 数和查询数量不等价，不能用两者的绝对 p95 直接宣告 Go 回归或达标。

### 数据形状复核

本地 dev 数据中，运行历史样本图 `a98aa8a2-87c1-4312-96a8-0c628b00857a` 有 19 条 run、26 条 node run；按 `workflow_graph_runs.graph_id` 汇总 `snapshot_json` 字节数为 379,971B，单行最大 23,856B。另一个较重图 `8591617e-e326-41fe-9667-d786b5ac7938` 有 13 条 run，snapshot 合计 559,552B。当前复核没有重现 817KB；若后续使用该数字，必须同时记录汇总 SQL、图 id、run 数和是否将 join 后的 snapshot 重复计数。

这组数据说明问题来自大 JSON 被列表 projection 读回和序列化放大，不能用“run 数只有十几条”作为安全依据。摘要列表的响应大小应随 run/node 摘要数量增长，不能随 snapshot、compiled context、input trace 或 output 增长。

### 当前因果链和合同

| 层 | 当前 owner | 当前行为 | 证据/约束 |
|---|---|---|---|
| HTTP 列表 | `go/internal/graph/service.go`、`run_dto.go` | `ListRuns` 返回 `GraphRunSummaryResponse`；最多最近 20 条 | `GraphRunListResponse.Items` 是摘要类型 |
| PG run 查询 | `go/internal/graph/runs.go:listGraphRuns` | run 查询显式选择状态、范围、时间、失败信息和 `progress_metadata`；不选择 `snapshot_json` | `Select("id", "graph_id", "status", ... )` |
| PG node 查询 | `go/internal/graph/runs.go:listGraphRuns` | 按 run id 批量查询节点状态、排序、尝试次数和进度；不选择 `compiled_context_json`、`output_json` | `Select("id", "graph_run_id", "node_id", ... )` |
| 列表序列化 | `go/internal/graph/run_dto.go` | 摘要节点没有 `compiled_context`、`input_trace`、`output` | `serializeGraphRunSummary` |
| 详情读取 | `go/internal/graph/service.go:GetRun` | 单 run 详情保留 snapshot、compiled context、input trace 和 output | 既有 detail HTTP regression |
| Web 运行历史 | `web/src/pages/workbench/canvas/GraphRunsPanel.tsx` | 先渲染摘要；用户打开详情时按 run id 请求完整详情 | `useQuery` 的 `enabled: detailsOpen` |
| Web 节点检查器 | `web/src/pages/workbench/canvas/GraphNodeInspector.tsx` | 状态来自摘要；只有用户展开 technical details disclosure 时，才按 run id 请求一条完整详情；共享 runs observer 使用 30 秒 stale window | 浏览器 gate 初始详情 0 次；显式展开最多 1 次 |
| Graph SSE | `go/internal/graph/run_sse.go`、`service.go` | 建连和无事件兜底只调用 `GetRunStatus`；事件仍按 PostgreSQL cursor 回放；当前连接数写入 `GraphSSEConnections` | `TestGraphRunStatusReadUsesOnlyIdentityColumns`、`TestGraphRunSSETracksActiveConnections`；API metrics 暴露 Graph SSE gauge |
| Graph projection | `go/internal/graph/project.go`、`product/graph_guard.go` | 节点行由 live graph 一次读取复用；artifact、绑定资产、视觉版本和商品资料/fact 按集合读取，避免逐节点/逐资产 N+1 | `TestGraphProjectionBatchesBoundAssetMetadata`、`TestLoadProductSourceSnapshotsBatchesProductAndFactReads` |
| 数据权威 | PostgreSQL | snapshot、run status、事件和产物继续由 PostgreSQL 保存 | Redis 不参与业务结果判断 |

`progress_metadata` 仍由列表读取，因为 `requested_node_ids`、`force` 和 `document_action` 目前存放在其中。当前写入内容来自有界的 run 请求和节点 id；它仍是 JSON 列，目标规模 gate 必须单独记录其字节量。若该列未来允许任意大 payload，应先迁移为有界 typed columns 或在写入边界增加约束，不能在列表 serializer 中无界展开。

### Graph SSE 收尾

修复前，SSE 连接初始化和每次无事件 fallback 都走完整 `GetRun`，造成活跃 run 在事件稀疏时重复读取大字段。本次代码改为：

1. 先用 `GetRunStatus` 校验商品/工作流/run 归属，并只读取 `workflow_graphs.id`。
2. 再从 `workflow_graph_runs` 只读取 `id,status`。
3. 事件列表仍从 `workflow_graph_run_events` 按 `sequence` 分页；terminal event 或 status 终态时关闭连接。
4. 初始错误仍保持工作流不存在和运行不存在的 HTTP 错误边界。

`go/internal/graph/run_http_test.go` 的 `TestGraphRunStatusReadUsesOnlyIdentityColumns` 通过 GORM query callback 检查实际 query selection，禁止 `snapshot_json`、`compiled_context_json` 和 `output_json` 出现在 status read。SSE route 没有另建业务状态缓存，通知丢失仍按 PostgreSQL 事件/状态回读。

### HTTP gate 合同

入口是 `scripts/bench_workbench_http.py`，通过 `just http-ab-gates` 调用。脚本只发 GET 和登录请求，不创建或修改商品、图、run、资产；`ADMIN_ACCESS_KEY` 只从环境读取，不写入报告。当前 Go gate 必须在已重启到目标工作树的 API 上执行，脚本不会替用户重启服务。

默认参数和阈值如下。它们是本地 loopback 的回归门，不是生产 SLO；真实部署仍需用指标和目标流量重新校准。

| Gate | 默认合同 | 失败含义 |
|---|---:|---|
| GraphRun summary HTTP | 100 样本、warmup 10、p95 <= 30ms | 摘要查询或连接池出现回归 |
| GraphRun summary payload | 每个成功响应最大 24KiB | 列表字段或 `progress_metadata` 放大 |
| GraphRun summary wire | 禁止 `snapshot`、`snapshot_json`、`compiled_context`、`compiled_context_json`、`input_trace`、`output`、`output_json` | 摘要合同泄漏详情字段 |
| GraphRun detail HTTP | 100 样本、p95 <= 100ms、单响应 <= 512KiB | 详情 payload 或 rich JSON 读取退化；该预算是本地回归门，不是生产 SLO |
| GraphRun detail contract | 每条 run detail 的第一条 node 保留 `compiled_context`、`input_trace`、`output` key | 详情能力被摘要拆分误伤 |
| current -> runs 串行序列 | 同一 client 先读 current 再读 runs，100 样本，p95 <= 30ms | 只测 API 关键链，不冒充浏览器 TTI |
| 商品列表串行 | page size 20/100 各 p95 <= 15ms | product summary 查询退化 |
| 商品列表并发 | page size 20，20 并发，p95 <= 80ms | 连接池或查询在并发下退化 |
| 旧实现 A/B | Go 与旧 Python 请求随机交错；只输出对照，不决定 Go pass/fail | 两边 fixture 或查询合同不等价 |

复现当前 Go gate：

```bash
HTTP_GATE_GO_PRODUCT=<product-id> \
HTTP_GATE_GO_GRAPH=<workflow-id> \
HTTP_GATE_OUT=/tmp/productflow-http-gates.json \
just http-ab-gates
```

启用历史对照时追加 `HTTP_GATE_MAIN_BASE` 和 `HTTP_GATE_MAIN_PRODUCT`：

```bash
HTTP_GATE_GO_PRODUCT=<go-product-id> \
HTTP_GATE_GO_GRAPH=<go-workflow-id> \
HTTP_GATE_MAIN_BASE=http://127.0.0.1:<legacy-port> \
HTTP_GATE_MAIN_PRODUCT=<legacy-product-id> \
HTTP_GATE_OUT=/tmp/productflow-http-ab.json \
just http-ab-gates
```

A/B 必须使用相同规模的 fixture，并在报告中记录 Go 图 id、旧服务商品 id、run/node 数、warmup、样本数和服务 commit。脚本虽然将请求随机交错，但无法消除两个服务使用不同数据库、schema 或缓存状态造成的偏差。

同一 Go 路径的 before/after 对照使用 `HTTP_GATE_REFERENCE_GO_BASE`、`HTTP_GATE_REFERENCE_GO_PRODUCT` 和 `HTTP_GATE_REFERENCE_GO_GRAPH`，两套服务应连接同一数据库且不启动 worker/dispatcher：

```bash
HTTP_GATE_GO_BASE=http://127.0.0.1:<current-port> \
HTTP_GATE_GO_PRODUCT=<product-id> HTTP_GATE_GO_GRAPH=<graph-id> \
HTTP_GATE_REFERENCE_GO_BASE=http://127.0.0.1:<reference-port> \
HTTP_GATE_REFERENCE_GO_PRODUCT=<product-id> HTTP_GATE_REFERENCE_GO_GRAPH=<graph-id> \
HTTP_GATE_WARMUP=20 HTTP_GATE_SAMPLES=400 \
HTTP_GATE_OUT=/tmp/productflow-http-before-after.json \
just http-ab-gates
```

报告中的 `go` 和 `reference` 是同一组 Go endpoint 的随机交错样本；旧 Python A/B 是另一种 wire contract，不能混用。

### SQL、HTTP 和浏览器三层验收

三层 gate 已落地并在当前代码 HEAD 通过。它们是本地回归证据，不替代生产 SLO；每次重新跑 gate 都要记录 fixture、服务 commit 和环境。

1. 目标规模 query plan：`go/internal/graph/query_plan_test.go` 在隔离迁移库插入 25,000 条 run 和目标图 100,000 条 node-run，执行摘要与详情的 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`。摘要 run 实际返回 20 行并命中 `ix_workflow_graph_runs_graph_started`；node 摘要实际返回 400 行并命中 `ix_workflow_graph_node_runs_run_node`；详情 run/node 分别验证 1/20 行和同样的主键/过滤索引路径，所有计划无 Seq Scan。
2. HTTP gate 与 before/after：正式报告为 `/tmp/productflow-http-gate-head-current.json` 和 `/tmp/productflow-http-gate-pre-split-current.json`，均为 warmup=20、每路径 400 样本、固定 fixture `product=18470ef4-d41a-4327-986f-4d9588198946`、`graph=a98aa8a2-87c1-4312-96a8-0c628b00857a`。2026-09-04 实现基线 `fb658633` gate：summary p50/p95/p99=3.49/4.09/4.65ms、最大 payload=15,629B；detail p50/p95/p99=3.90/4.71/5.67ms、最大 payload=2,623B；current→runs 串行 p95=13.59ms；商品列表 page 20/100 串行 p95=3.73/4.34ms，20 并发 p95=12.39ms；所有请求 100/100 且字段合同通过。

   - 2026-09-01 测量：本次改动净对照（`HEAD=6726a490` -> 当时 dirty working tree，同一路径随机交错）：`current` p95=10.32 -> 9.73ms（-5.7%），`runs` p95=4.44 -> 4.36ms（-1.8%）；两条响应 payload 均保持 34,514B 和 15,629B。这证明本次改动没有造成 API 回归，HTTP 毫秒收益在小 fixture 上有限。
   - 2026-09-01 测量：完整读路径对照（`2d9bba87`，GraphRun 摘要/详情拆分前 -> 当时 dirty working tree）：`current` p95=9.94 -> 9.32ms（-6.2%），payload 均 34,514B；`runs` p95=16.78 -> 4.67ms（-72.2%），payload=67,705 -> 15,629B（-76.9%）。后一个大收益包含已在 HEAD 中的 GraphRun 摘要/详情拆分，不能全部归因于本次 dirty diff。
   - Projection 伸缩性诊断 fixture 在 20/100/500 个 `product_source` 节点下：`HEAD` SQL=51/211/1011、projection GET=24.345/97.294/466.692ms；当前 SQL=10/10/10、projection GET=7.230/9.478/15.309ms。500 节点时 SQL 减少 99.0%，处理时间减少 96.7%；原始日志为 `/tmp/productflow-query-scale-head-20.log`、`/tmp/productflow-query-scale-head-100.log`、`/tmp/productflow-query-scale-head-500.log` 以及对应的 `current` 文件。这是隔离测试库的伸缩性证据，不替代目标规模 query plan 或生产流量。
   - 旧 Python A/B 未启用，不能从这些 Go 对照推导跨栈结论。
3. Playwright：`web/e2e/workbench-performance.spec.ts` 记录冷/热启动、`current`、`runs`、Graph SSE、rich detail 和显式详情动作。2026-09-04 当前 fixture 结果为 cold/warm TTI=2,159/2,247ms，current/runs 各 1 次，初始 rich detail=0，Graph SSE=0（fixture 没有进行中 run），节点检查器显式展开 0 次额外请求，运行历史显式打开 1 次详情请求，19 条可见 run。脚本对 4 条预期的 Agent bootstrap 409 做白名单处理，并检查其它 console/page/network/HTTP error；在 default 3,000ms TTI budget 下通过。
4. 详情打开率：浏览器 gate 用一次显式打开动作验证“初始 0、按需最多 1”合同，并输出 `visible_run_count` 与 `run_detail_requests_after_open`；当前系统没有生产行为分析/用户级打开率埋点，因此不能把这次 synthetic action 写成真实打开率。详情 payload 继续由 HTTP detail contract 和按需路径验证，若部署后打开率较高，再评估分段 detail API。

Web route split 和预算也已通过：bundle entry 为 928.6KB raw/253.2KiB gzip，`ProductWorkbenchPage` loader 为 6.7KB/2.9KiB，`AgentWorkbenchShell` 为 501.3KB/146.8KiB；`scripts/check_web_bundle_budget.py` 已接入 `just web-build`，入口预算为 1,000,000/300,000B，loader 为 100,000/40,000B，shell 为 550,000/170,000B。Vite 对超过 500KB 的通用提示仍可能出现，但显式预算 gate 已把可回归的 route 边界固定下来。

### Redis 决策

本专项不引入 Redis cache。GraphRun list 的权威状态、snapshot、事件 cursor 和详情继续读 PostgreSQL；Redis 仍只承担现有 asynq broker/投递唤醒职责。Graph SSE 已经通过轻量 PostgreSQL status read 降低稀疏事件轮询成本，当前没有证据证明需要缓存。

若未来加入摘要缓存，必须先定义版本键、GraphRun terminal/status 失效、snapshot/产物变更后的回源、进程重启和一致性测试；缓存命中不能决定业务终态，Redis 丢失不能影响 run 结果。

### 当前缺口

| ID | 缺口 | owner | 状态 |
|---|---|---|---|
| GRAPH-READ-01 | 摘要 wire 禁止详情字段、详情保留字段 | `go/internal/graph`、`web/src/pages/workbench/canvas` | focused HTTP contract、100 样本 validator、Web detail path 已过 |
| GRAPH-READ-02 | SSE 建连/fallback 不读取大字段 | `go/internal/graph/run_sse.go`、`runs.go` | `TestGraphRunStatusReadUsesOnlyIdentityColumns`、`TestGraphRunSSETracksActiveConnections` 与 Graph package gate 已过 |
| GRAPH-READ-03 | 修复后 HTTP p95/payload 与同 Go 版本 A/B | `scripts/bench_workbench_http.py`、`justfile` | 2026-09-04 实现基线 `fb658633`：summary/detail p95=4.09/4.71ms，最大 payload=15,629/2,623B，current→runs 串行 p95=13.59ms，商品列表 page 20/100 串行 p95=3.73/4.34ms、20 并发 p95=12.39ms，100/100 且字段合同通过；2026-09-01 的同一 fixture `HEAD -> dirty` A/B 与拆分前对照保留在验证记录中 | 旧 Python A/B 未启用；生产详情打开率和跨副本容量仍观察 |
| GRAPH-READ-04 | 目标规模 query plan 和 buffers | `go/internal/graph/query_plan_test.go`、`cmd/productflow-migrate` | 2026-09-05 `just go-test-graph-query-plan` 在本 HEAD 复跑通过：25,000 runs、100,000 target node-runs；摘要 run Index Scan `ix_workflow_graph_runs_graph_started` execution 0.022ms（20 行）；node 摘要 Incremental Sort + Index Scan `ix_workflow_graph_node_runs_run_node` execution 0.248ms（400 行）；详情 run PK execution 0.016ms、node Index Scan + Sort execution 0.022ms；无 Seq Scan |
| GRAPH-READ-05 | 浏览器 TTI、重复请求、SSE 连接数和 bundle 成本 | `web/src/pages/workbench`、Web e2e、`platform/metrics` | `just web-e2e-workbench-performance` 已过：2026-09-04 cold/warm=2,159/2,247ms，current/runs 各 1，初始 detail=0，显式 run detail=1，Graph SSE=0；4 条预期 Agent bootstrap 409 已白名单，其它 HTTP/page/network failure=0；bundle budget 已接入 `just web-build`；跨副本汇总仍需部署观测 |
| GRAPH-READ-06 | Graph detail/project 读取中的剩余 N+1 | `go/internal/graph/project.go`、`product/graph_guard.go` 和对应页面 owner | 节点行复用、artifact/asset/visual/source/fact 按集合读取；`TestGraphProjectionBatchesBoundAssetMetadata`、`TestLoadProductSourceSnapshotsBatchesProductAndFactReads` 与 Graph package gate 已过 |
| GRAPH-READ-07 | 生产详情打开率 | `web/src/pages/workbench/canvas`、部署 metrics/trace | 代码 gate 已证明初始不读、显式动作最多一条 detail；当前没有用户级行为埋点，真实打开率标为部署后观察项，不用 synthetic action 冒充 |

## 不变量

### 数据权威

1. PostgreSQL 保存业务状态、attempt、fencing、journal、effect ledger 和 `async_dispatches`。Redis 只保存易失投递态；worker 不能把 Redis 成功当成业务成功。
2. NOTIFY payload 只包含唤醒所需的聚合 id。任何 SSE 都必须按 cursor、状态或详情查询回 PostgreSQL；通知丢失、重复或乱序都不能改变结果。
3. Graph snapshot 是一次 run 的输入快照；性能优化不能把执行路径改回读取 live graph，也不能因为减少查询复制一份 snapshot 权威。
4. Agent 浏览器事件以 `agent_turn_events` 为准；Pi session 文件和本地 JSONL 只用于 Pi loop/WAL 恢复，不参与业务列表或终态判断。
5. 缓存失效不能改变业务语义。需要缓存时必须先定义失效、重建、过期和回源路径，并增加一致性测试。

### 锁序

性能改动必须把锁顺序写在调用方和 owner helper 中，禁止依赖被调用函数的隐藏 `FOR UPDATE`：

| 资源 | 规定顺序 | 适用路径 |
|---|---|---|
| GraphRun、live graph、NodeRun、provider effect | `run -> graph -> node -> effect`；不改 live graph 时用 `run -> node -> effect` | 取消、终态、节点 claim、provider result、文稿自动采用 |
| Graph capacity 与执行行 | `generation capacity advisory -> run -> node` | Graph node claim |
| Graph capacity 与 ImageSession task | `generation capacity advisory -> task` | ImageSession claim；拿 task 后必须再次检查 queued |
| Agent Turn projection、execution、Task | `projection -> execution -> task` | AppendEvents、终态投影、Task 同步 |
| Agent Draft confirmation | `projection -> task -> projection-owned journal append` | 全局组织 Draft 确认；同一事务内重入 projection 锁允许，但不得从 task 跳回未持有的 projection |
| async dispatch | `dispatch row` 短事务内完成 claim/lease 更新 | dispatcher 与 worker；不要在 dispatch 行锁内调用 provider |

Graph `appendGraphRunEventLocked` 的名字表达前置条件：调用方必须已经持有 run 行锁；事件 helper 不再隐式获取 run 锁。`MAX(sequence)+1` 只能在该 run 行锁下使用。

禁止以下写法：

- 先更新 NodeRun，再调用会锁 GraphRun 的事件或终态 helper。
- 先锁 Task，再查询并锁 Turn projection。
- 持有业务行锁或 capacity advisory 时调用外部 provider、磁盘慢 IO、远程 HTTP 或等待用户。
- 为规避锁等待添加第二把进程锁、Redis 锁或通用 lock service；先修复 PostgreSQL 锁顺序和事务边界。
- 在未证明 owner/fencing 的情况下，把 `unknown` 改为 `failed` 或自动重试。

GraphRun 的“一个 run 一个 worker”执行权现在由行上的 token/expiry lease 提供：CAS 抢占过期 lease，续租失败即取消旧执行，状态与产物写入检查 token/expiry。进程 mutex 只保留同进程快速门禁；它不承担跨副本执行权。容量 advisory 仍只用于全局生图 admission。

### 事务边界

事务应围绕一个可重试的数据库状态迁移建立：读必要身份、短锁、写状态/事件/outbox、提交。下列动作必须在事务外：provider HTTP、对象存储大文件复制、压缩整包、SSE 长连接、等待 Redis/asynq 响应。

Recovery 的默认边界：

1. Agent、Graph、ImageSession、Delivery、LocalEdit 的每阶段默认上限是 25 条；业务域候选使用稳定排序和 `SKIP LOCKED`。
2. `has_more` 是“本轮批次已填满、下一轮继续探测”的保守 hint，不是跨副本的精确 backlog 计数；锁竞争可能让它暂时低估待处理量。
3. 过期 lease 扫描和 fencing/终态写入是一批短事务，当前 Agent 默认最多 25 个 execution。
4. queued Task 的 `reserveTurn` 每个 Task 独立提交，单个坏 Task 只影响该条。
5. 待同步 projection 的读取与 outbox restage 分开；outbox 批量写入不应持有 execution、Task 或 Graph 行锁。
6. Graph、ImageSession、Delivery、LocalEdit 的 recovery 使用有限批次、稳定排序和可重入状态迁移。发现更多数据时由下一轮继续，而不是把全库行装入一个事务。
7. 事务出错时 summary 不能把内存中的计数当作已提交事实；只有提交成功的阶段才增加对外计数。

### 队列语义

| 状态 | owner | 允许的性能优化 |
|---|---|---|
| 业务行 queued/running/terminal | 领域包 | 可加索引、批量读取、有限扫描；不能只在 Redis 维护状态 |
| `async_dispatches=PENDING` | API/领域命令写入，dispatcher claim | 可用 `SKIP LOCKED`、稳定 keyset、NOTIFY 唤醒 |
| `async_dispatches=SENT` | dispatcher，worker 消费 lease | 必须先 SENT 后 enqueue；broker 失败等对账，不回滚成 PENDING |
| asynq envelope | Redis | 可丢；不要依赖 Unique、broker retry 或单队列公平性证明业务一次性 |
| unknown | 领域恢复/副作用对账 owner | 保持不可证明语义，不用性能重试掩盖证据缺失 |

## 优化顺序

### P0：先修正确性和锁序

状态：当前工作树已落地，相关包和全量 Go gate 已通过；专门并发 Gate 仍未补齐。

- Graph 事件写入使用显式 `appendGraphRunEventLocked`；所有会追加事件的 run mutation 先拿 run 锁。
- 文稿自动采用路径使用 `run -> graph -> node`；普通节点状态路径使用 `run -> node -> effect`。
- Agent 全局 Draft 确认先锁 projection，再锁 Task，并复用已经锁定的 projection id。
- Graph `failBlockedQueuedNodes` 先锁 run，再级联更新 node。
- ImageSession claim 先取得 capacity advisory，再锁 task，并在锁后再次验证 task 状态。

验收重点：Graph cancel/recovery/worker tick/automatic adoption 并发，Agent AppendEvents/Draft confirm 并发，ImageSession 两个 claim 并发。测试遇到 `40P01`、`55P03` 或 CAS 失败时，先区分预期的跳过与真实死锁。

### P1：解耦 dispatch、recovery 和容量 admission

状态：dispatch/recovery cadence、Agent recovery 事务、全部 recovery owner 的有界候选批次、dispatcher `has_more`/duration/error 日志、dispatcher 各域 recovery histogram、候选锁查询耗时、API 当前 PostgreSQL 锁等待和 queued/stale-running backlog 指标已落地；capacity wait/running/denied 指标已落地；批次内事务拆分和负载验证仍待实施。

1. dispatcher watch 模式持续以 dispatch interval 处理 outbox；recovery 使用独立 cadence。首轮 recovery 立即运行，失败不推进成功时间戳。
2. Agent、Graph、ImageSession、Delivery、LocalEdit 已使用默认 25 条上限；业务域使用 `status + stale predicate + SKIP LOCKED` 和稳定时间/id 排序，dispatcher 日志记录每个 owner 的 `has_more`、recovery duration 与错误，dispatcher `/metrics` 提供各域 recovery histogram/候选锁查询耗时，API `/metrics` 提供当前 PostgreSQL 锁等待和 queued/stale-running backlog。下一步是拆分批次内状态迁移与 restage，并用目标规模验证没有隐性饥饿。
3. 把当前每批事务继续拆成候选 claim、单聚合状态迁移、restage 的短事务；不要让批次大小随业务行数增长。
4. 单商家阶段继续用 PostgreSQL capacity advisory 保证准确 admission；capacity wait 用 `productflow_advisory_lock_wait_seconds`，running/denied 用 `productflow_generation_admission_*`。SaaS 前替换为按 workspace/tenant 的 admission token；PostgreSQL 只做对账。
5. GraphRun 行 lease 已替代执行 loop 的长 advisory：token/expiry CAS、5 分钟续租、过期接管、迟到 provider 结果 fencing 和 recovery 的 lease 过期条件已落地。继续观察续租失败、接管和 worker consumer lease 的时间预算。

### P2：查询、列表和实时通道

状态：SSE fanout、ImageSession/GraphRun/Agent Session 列表的批量摘要查询、两个排序索引、ImageSession 游标分页、GraphRun 摘要/详情路径、Graph projection 批量读取和 Graph SSE status-only fallback 已在工作树中；GraphRun 的 HTTP、目标规模 query plan、浏览器 TTI、bundle 和重复读 gate 已通过。剩余是生产详情打开率、跨副本容量和其它列表的真实规模观察。

- 高频列表采用 keyset cursor；cursor 必须绑定筛选条件、排序版本和 tie-breaker id。不要用大 offset 代替 cursor。
- 轻量列表只选择摘要列；snapshot、full node runs、全部 rounds、全部 effect ledger 和大 JSON 通过详情路由或按需页读取。
- ImageSession 列表使用批量 count/latest asset 查询和 `(updated_at,id)` keyset cursor；history 使用 `(session_id, created_at DESC, id DESC)`。Go DTO、HTTP query、`web/src/lib/api.ts`、React Query infinite pages、load-more 和列表测试必须一起维护。
- GraphRun 列表与详情分开：列表最多取 run 状态、时间、范围和 node progress 摘要；详情再读取 snapshot、node input trace 和 output。工作台列表使用摘要，运行详情按钮和节点检查器按需读完整 run；共享 runs observer 使用 30 秒 stale window，避免组件稍后挂载时重复 refetch。Graph SSE 建连和无事件兜底只读取 graph/run identity 与 status；事件仍按 cursor 回放。目标规模 query plan、`progress_metadata` 字节、payload 和浏览器重复读 gate 已有记录。
- Agent Session 列表已经分页，当前页的 session、conversation 摘要和 count 已批量取；单条 `loadSession` 仍保持同一投影 helper，保持 global conversation 补齐的写边界不变。真实数据规模下仍需检查 payload 和 query plan。
- SSE 使用共享 fanout；每条连接必须 `defer unsubscribe`。listener 关闭或池不可用时切换有限轮询。每个副本的 listener 和连接池预算都要纳入部署容量。
- 索引只服务于已确认的 predicate + order：当前有 `image_sessions(updated_at DESC,id DESC)`、`image_session_rounds(session_id, created_at DESC, id DESC)`、`image_session_generation_tasks(session_id, created_at DESC, id DESC)` 和 `workflow_graph_runs(graph_id,started_at DESC,id DESC)`，通过 `just go-migrate` 后用 `EXPLAIN (ANALYZE, BUFFERS)` 验证；无证据不新增大范围复合索引。

### 媒体和内存

当前性能风险集中在媒体 bytes 和大 JSON：

- `product/gallery_archive.go` 与 `delivery/export.go` 在事务内只冻结条目身份，事务外经 `mediaarchive.Begin`/`Add`/`Finish` 逐文件写入临时 ZIP。2026-09-05 `just go-test-zip-rss`：100 张 `GenerationMaxImageBytes`（10MiB）近上限图，额外 HeapInuse=0、额外 MaxRSS=13,205,504B（约 12.6MiB），低于 64MiB；两次写出 SHA-256 `76cde9973fbe74db086a4ea61d6817d85318442945cf3d1a5c71bafbf3773227`。图库 HTTP 仍有 100 张 / 512MiB 总上限，因此该闸门测共享写入器，不走 `BuildGalleryArchive` 的 100×10MiB 路径。
- 上传和变体生成要维持真实 MIME、像素、单图和批量限制；优化不能绕过验证。
- 连续生图调用前读取 base/reference 图片时要按当前最大输入数和字节预算控制峰值；不要让 `generation_count` 线性放大未受限的内存。
- Graph snapshot、Agent journal event 和 provider JSON 应保持有界；分页不能通过把大 JSON 复制到第二个 projection 解决。
- 大文件 IO、图片解码和压缩不应处在持有业务行锁的事务中。

## Dispatcher 与 recovery 指导

### 推荐循环

```text
watch loop
  -> dispatch due: reconcile outbox leases, claim PENDING, mark SENT, enqueue
  -> recovery due: run each domain's bounded recovery stages
  -> sleep until dispatch interval
```

当前实现保留顺序执行各 domain recovery，但 recovery 不再每一秒运行。后续可把 recovery 拆为独立 goroutine 或独立进程，前提是：

- 每个 owner 的候选状态有 PostgreSQL 条件和 `SKIP LOCKED`/CAS 围栏。
- recovery 与 live worker 之间的锁序保持一致。
- dispatcher 多副本不会重复写终态或增加错误 attempts。
- dispatch loop 在 recovery 故障时仍能投递已提交的 PENDING；恢复错误要有独立日志和指标。
- shutdown 会取消 listener、停止新 claim，并让当前短事务完成或回滚。

### 批次设计

一个 recovery 批次至少包含：

1. stable order：`stale_at, created_at, id` 或对应领域等价排序。
2. bounded read：只选本批 id/最小身份列。
3. short write tx：重新读取并锁单条或小批，检查 status/attempt/fencing，再更新。
4. restage tx：只处理 outbox，不带 provider 或大聚合查询。
5. progress：记录 processed、changed、unknown、skipped、has_more 和耗时。

同一个状态连续多轮没有推进时，要记录原因：有效 lease、前置锁等待、provider boundary、outbox active lease、状态不匹配或数据损坏。不能只提高轮询频率。

## 查询指导

### 查询前检查表

| 问题 | 要求 |
|---|---|
| 是否有明确范围 | 业务列表按 product/session/conversation/graph；全局扫描只用于 bounded recovery 或后台统计 |
| 是否有排序 tie-breaker | 所有分页和 recovery 排序追加稳定 `id` |
| 是否会读取大 JSON | 列表使用 `Select` 摘要列；详情或执行路径再解码 |
| 是否存在 N+1 | 先批量聚合或一次 join，再决定是否保留逐项 serializer |
| 是否依赖 Count | 只在 UI 明确需要总数时 count；队列位置使用有界/近似语义并标明可能过时 |
| 是否有对应索引 | predicate、join key 和 order 组合检查现有 ExtraDDL；用 query plan 证实 |
| 是否在事务中 | 只读查询可短事务；命令写使用 `tx.WithGorm`；禁止在事务里 provider/长 SSE |
| 是否会改变合同 | DTO、cursor、limit、排序、空列表和错误码变更必须跨 Go/Web/tests 一起处理 |

### 当前重点查询

- `imagesession.Service.List`：按 `updated_at DESC, id DESC` 使用版本化 keyset cursor，页面读取 `limit+1` 并批量查询 round count/latest asset；前端把 pages 合并为 infinite list。25k 会话列表与 10k 轮次 history keyset 的 query plan 已由 `just go-test-imagesession-query-plan` 记录；删除/并发更新下的 page hit rate 仍缺。
- `graph.Service.ListRuns`：当前最多 20 条，run 与 node runs 批量读取为状态/进度摘要；snapshot、input trace、compiled context 和 output 由单 run 详情读取。执行循环已复用同一 tick 的 run 投影，避免重复完整读取；`run_sse.go` 的建连/fallback 只读 status；目标规模摘要/详情 query plan 与修复后 payload/P95 已由 `just go-test-graph-query-plan` 和 `just http-ab-gates` 记录。
- `agent.listSessions/loadSession`：主查询已经 cursor page，当前页的 conversations/count 已批量读取；单条详情仍按一个 session 组装三类数据。后续需要目标规模 payload/query plan，并保持 global conversation 的补齐逻辑在命令事务边界内。
- `product.listProducts`：名称包含搜索是 `ILIKE '%q%'`。不要用普通 btree 索引冒充 contains 加速；需要时评估 `pg_trgm` 的部署权限、索引大小、迁移和搜索 SLO。
- `library.List`：已有 cursor page；搜索、标签 join、归档 predicate 需要以真实 query plan 决定索引，不能为每个筛选组合无限加复合索引。
- `metrics.snapshot`：当前会做多组全表状态 count，属于低频内部端点。若监控抓取频率提高，应缓存短 TTL 的观测结果或降低查询集合，但不能把 metrics 变成业务读路径。

### EXPLAIN 验证模板

对生产形状数据使用脱敏副本或 dev fixture，记录 query、行数、计划和 buffers：

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT id, status, started_at, finished_at
FROM workflow_graph_runs
WHERE graph_id = $1
ORDER BY started_at DESC, id DESC
LIMIT 20;
```

必须同时记录：数据规模、命中索引、实际 rows、shared hit/read、planning/execution time。只看到“使用了 index”不足以证明收益；需要比较旧计划和新计划，并确认写入成本没有超过收益。

### 2026-09-01 dev fixture plan

当前 dev 数据规模为 `workflow_graph_runs=66`、`workflow_graph_node_runs=192`；目标 Graph `a98aa8a2-87c1-4312-96a8-0c628b00857a` 有 19 条 run、26 条 node run。使用与 `listGraphRuns` 相同的摘要列，在未强制 planner 使用索引的情况下得到：

| 查询 | 实际 rows | 计划宽度 | shared buffers | execution time | 计划选择 |
|---|---:|---:|---:|---:|---|
| run summary，按 graph + started/id，limit 20 | 19 | 314B | hit=9 | 0.115ms | Seq Scan + Sort；表太小，未使用排序索引 |
| node summary，19 个 run id，按 run/sort/id | 26 | 197B | hit=52 | 0.195ms | Seq Scan + Sort；全表只有 192 条 |

这个结果证明当前 SQL 已经没有把大 JSON 列带入摘要计划，也证明小 dev 表不能用来判断目标规模下的索引收益。目标规模 gate 已由 `just go-test-graph-query-plan` 在隔离迁移库执行，结果如下：

| 查询 | fixture 行数 | 实际 rows | planning/execution | shared buffers | 计划选择 |
|---|---:|---:|---:|---|---|
| run summary，按 graph + started/id，limit 20 | 25,000 runs | 20 | 0.078/0.022ms | hit=3 | `Index Scan`，`ix_workflow_graph_runs_graph_started` |
| node summary，20 个 run id，按 run/sort/id | 100,000 target node-runs | 400 | 0.156/0.252ms | hit=49 | `Index Scan` + `Incremental Sort`，`ix_workflow_graph_node_runs_run_node` |
| run detail，id + graph_id | 同上 | 1 | 0.069/0.016ms | hit=3 | `Index Scan`，`workflow_graph_runs_pkey` |
| node detail，单 run + sort/id | 同上 | 20 | 0.081/0.021ms | hit=3 | `Index Scan` + memory sort，`ix_workflow_graph_node_runs_run_node` |

四条计划都没有 Seq Scan 或 shared read；该 gate 覆盖摘要和详情的数据库查询形状，但不把 provider payload 处理时间写成 SQL 结论。

## SSE 与连接池指导

### 事件通道

业务写事务提交时可以 `pg_notify` 唤醒订阅者。`platform/notify.Subscribe` 在同一 `*pgxpool.Pool` 上共享 listener，订阅者是有界缓冲 channel：

- payload 为空或不是当前聚合 id 时，SSE 继续回到自己的 cursor/状态读取。
- subscriber buffer 满时丢弃通知，调用方依靠 PostgreSQL 回读恢复，不得扩大 buffer 代替回放。
- listener 出错后，handler 切换有限 polling；polling 也必须有 heartbeat、终态退出和请求取消。
- handler 结束必须执行 unsubscribe，最后一个 subscriber 退出后释放 listener connection。
- 连接预算按“每个 API 副本一条 listener + 普通查询连接 + worker/dispatcher 连接”计算；不再按“浏览器数 × SSE 路由”估算 PostgreSQL listener 数。`notify.ListenerConnections` 记录实际 LISTEN 连接，API metrics 同时暴露 `productflow_notify_listener_connections`、`productflow_graph_sse_connections` 和 Agent SSE gauge；它们都是单进程值。

### 验收指标

当前 metrics 端点是 bearer 保护的文本端点，已有低基数状态、Graph/Agent SSE 和 PostgreSQL LISTEN gauge。性能改动应补充或采集：

- dispatch PENDING backlog、SENT stale 数、claim batch size、mark-SENT 到 enqueue 耗时。
- recovery 每域 processed/changed/unknown/skipped/has_more、事务耗时、候选锁查询耗时和错误次数。
- Graph capacity advisory wait、running count、capacity denied 次数：`productflow_advisory_lock_wait_seconds`、`productflow_generation_admission_running`、`productflow_generation_admission_denied_total`。denied 是进程内计数，跨副本按 instance 抓取。
- PostgreSQL pool acquired/idle/max、listener count、query latency 和 deadlock/serialization error count。当前 API metrics 已有 `productflow_notify_listener_connections`，pool 原始计数仍以 PostgreSQL/client exporter 为准。
- SSE active connections、listener reconnect、poll fallback、subscriber drop；当前 API metrics 已有 `productflow_graph_sse_connections` 和 Agent SSE gauge，跨副本指标要带 instance，不能把进程内 gauge 当总数。
- ImageSession/Graph/Agent 列表 rows、query time、payload bytes 和 cursor page hit rate。
- Agent 已有 `productflow_agent_event_batch_last_ms`、journal compact、expired lease、sequence conflict 和 SSE connection 指标，改变 batch/WAL 时必须继续跑容量门。

建议初始预算如下，正式 SLO 需用目标数据规模校准：

| 路径 | 初始目标 | 超标后的动作 |
|---|---:|---|
| 普通列表首屏 | p95 < 300ms，payload < 1MiB | 查 query plan、N+1 和 JSON 体积 |
| GraphRun summary（本地回归门） | p95 <= 30ms，单响应 <= 24KiB；摘要不得含详情字段 | 运行 `just http-ab-gates`，并核对 SQL 选列与目标规模 plan |
| GraphRun detail | 按需请求；本地 gate p95 <= 100ms、payload <= 512KiB，并记录打开率，不把详情预算倒灌到列表 | 查详情打开率、node/output 大小与剩余 N+1 |
| 不含 provider 的命令事务 | p95 < 500ms；锁等待 p95 < 100ms | 拆查询/锁，确认没有外部 IO |
| dispatch 从 PENDING 到 SENT | p95 < 1s（正常 Redis/PG） | 看 recovery 是否阻塞、batch 是否过大 |
| SSE 首帧/已持久化回放 | p95 < 1s | 查 listener、pool 和 fallback polling |
| recovery 单阶段事务 | p95 < 1s，且批次有上限 | 缩小批次、拆 restage、查锁等待 |
| Graph/Image capacity wait | 记录分布，不设伪装成功阈值 | 按 tenant/workspace admission 设计；不要只加 worker |

## SaaS 前置设计

当前不新增 tenant_id 或局部 RLS。进入 SaaS 基线时必须一起解决：

1. principal/user/session：认证结果携带稳定 subject，业务服务从 subject 得到 workspace/tenant 范围。
2. 数据范围：Products、Graph、Library、Agent Session/Task/Turn、ImageSession、Delivery、LocalEdit、provider profiles 和 outbox 都要有明确的 workspace owner；跨表 FK 和查询 helper 一起改。
3. 配额：capacity admission、provider rate limit、Redis/asynq queue fairness 和每租户 backlog 分开计量。
4. 存储：本地 `STORAGE_ROOT` 改为共享对象存储或明确的副本分片；媒体变体生成移出长请求路径。
5. 实时通道：跨副本 fanout、连接配额、断线重放和租户过滤必须同时设计。
6. 迁移：从新 SaaS baseline 建表或明确迁移窗口，不给现有单商户 live 表补一列后继续复用所有全局查询。

一个大客户占满全局生图容量、全局 metrics count 扫描所有租户、SSE 连接跨租户广播和 provider secret 全局可见，都是数据模型问题。提高 worker 并发或增加 Redis 实例不能解决这些边界。

## 代码 owner 与验收 Gate

| 变化 | owner | 最低 Gate |
|---|---|---|
| Graph lock order、事件序号、capacity claim | `go/internal/graph`、`go/internal/imagesession` | `go test -C go ./internal/graph ./internal/imagesession -count=1 -p 1`；并发 deadlock regression |
| Agent recovery、projection/task lock order | `go/internal/agent` | Agent focused recovery/lock tests；`go test -C go ./internal/agent`；SIGKILL recovery gate |
| dispatcher cadence/queue | `cmd/productflow-dispatcher`、`platform/queue` | dispatcher compile；queue tests；PENDING/SENT/worker lease tests |
| notify fanout/SSE | `platform/notify`、Graph/ImageSession/Agent SSE | notify tests；Agent shared listener test；各 SSE terminal/reconnect tests |
| schema indexes | `platform/db/schema`、`cmd/productflow-migrate` | `just go-migrate`；schema migrate regression；`EXPLAIN (ANALYZE, BUFFERS)` 记录 |
| 列表分页/N+1 | 对应 Go 包 + `web/src/lib/api.ts` + 页面 owner | DTO/API/frontend tests；Web build；query plan 与 payload 对比；有 fixture 时运行 `just http-ab-gates` |
| GraphRun HTTP 回归 gate | `scripts/bench_workbench_http.py`、`justfile`、Go/Web workbench owners | 新 API 进程上的 summary/detail/SSE contract、p50/p95/p99、payload、商品列表串行/并发；旧实现 A/B 只作辅助 |
| provider/媒体并发 | `providers`、Graph/ImageSession/Media | provider contract、unknown/timeout、内存/字节预算和真实 fixture |
| SaaS baseline | 新架构切片 owner | principal/data-scope/FK/queue/storage/SSE 全链路设计与迁移验证 |

### 必跑命令

```bash
# 依赖本地 PostgreSQL/Redis 的 backend gate
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./... -count=1 -p 1'

# schema 与文档
just go-migrate
just docs-check
git diff --check

# Agent service 和 Web 只在对应模块发生变化时追加
just agent-service-test
just web-build

# GraphRun 专项验收（按需；需要本地 PostgreSQL）
just go-test-graph-query-plan
HTTP_GATE_GO_PRODUCT=<product-id> HTTP_GATE_GO_GRAPH=<workflow-id> just http-ab-gates
PRODUCTFLOW_PERF_PRODUCT_ID=<product-id> WEB_BASE_URL=http://127.0.0.1:<web-port> just web-e2e-workbench-performance
```

推荐的容量专项：

- Agent journal：`just go-test-agent-journal-capacity`。
- Agent local WAL：`just agent-service-test-local-journal-capacity`。
- Graph/Image provider：使用 mock provider 跑并发 claim、容量满、timeout、unknown 和 recovery。
- SSE：每个 channel 的持久化回放、通知丢失、listener 关闭、heartbeat、终态退出、连接上限和 pool 使用量；Graph run 另验证 status-only 建连/fallback 不选大 JSON。
- GraphRun HTTP：确认 API 已重启到目标工作树后运行 `HTTP_GATE_GO_PRODUCT=... HTTP_GATE_GO_GRAPH=... HTTP_GATE_OUT=/tmp/productflow-http-gates.json just http-ab-gates`；报告保存到 `/tmp`，记录 p50/p95/p99、payload、错误数和 fixture 规模。当前 gate 还会检查摘要/详情字段合同。
- Browser：用 `just web-e2e-workbench-performance` 记录冷/热工作台导航、current/runs/SSE/detail 请求、可操作时间、显式详情动作和 console/network error；`just web-build` 内的 bundle budget 固定入口与工作台壳预算。
- DB：用 `just go-test-graph-query-plan` 以 25,000 runs/100,000 node-runs 目标形状跑 `EXPLAIN (ANALYZE, BUFFERS)`；生产复核仍需记录真实 rows、shared hit/read、p50/p95/p99，不只记录一次成功响应。

## 当前工作树验证记录

| 日期 | 变化 | 已验证 | 未完成 |
|---|---|---|---|
| 2026-09-01 | Graph lock helper、Graph event call sites、Agent Draft confirmation lock order | Graph focused execution tests；Agent focused lock/recovery tests | Graph 专门并发 deadlock test；全量 Go gate |
| 2026-09-01 | Agent recovery 分阶段事务、per-Task reserve、有限候选批次与 `has_more` | Agent recovery focused tests；精确 Pi integration 连续 3 次通过；第二次 `just go-test` 全量通过 | 外部 Pi integration 曾在一次受影响包集合中出现时序失败，需保留真实 Agent 环境容量复核；stale-running backlog 与锁等待 |
| 2026-09-01 | ImageSession capacity -> task lock order | ImageSession execute/recovery/HTTP focused tests | capacity wait/lock-order专项测试 |
| 2026-09-01 | `notify.Subscribe` fanout 下沉，Agent/Graph/ImageSession 共用 | notify tests、Agent shared listener test、ImageSession SSE test、`TestGraphRunSSETracksActiveConnections`；API metrics 暴露 listener/Graph SSE gauge | 跨副本连接预算需要部署抓取和告警；单进程 listener contract 已过 |
| 2026-09-01 | ImageSession summary 批量聚合 | ImageSession list/execute HTTP focused tests | 真实规模 query plan、无分页合同改造 |
| 2026-09-01 | GraphRun 与 Agent Session 列表批量投影 | Graph full package、Agent Session CRUD 和 21 conversation limit regression 通过；Graph projection 的 asset/source/fact batch tests 通过 | Agent Session 真实规模 payload/query plan；Graph 目标规模 gate 已补齐 |
| 2026-09-01 | dispatcher `--recovery-interval` 默认 10 秒与 recovery 错误隔离 | dispatcher package tests；第二次 `just go-test` 全量通过 | watch loop integration 和 dispatch latency load test |
| 2026-09-01 | ImageSession/GraphRun 排序索引 ExtraDDL | `just go-migrate`；`go test -C go ./internal/platform/db/schema -count=1 -p 1` 通过；Graph target-scale plan gate 通过 | ImageSession 目标 plan 仍待单独记录 |
| 2026-09-01 | Graph/ImageSession/Delivery/LocalEdit recovery 有界候选批次与 Agent `has_more` 汇总 | recovery focused tests；dispatcher/Agent focused tests；第二次 `just go-test` 全量通过 | stale-running backlog 分项、批次事务耗时与锁等待 |
| 2026-09-01 | API queued/stale-running recovery backlog 与 dispatcher recovery duration | metrics snapshot integration；dispatcher/metrics focused tests；最新 `just go-test` 全量通过 | recovery histogram 和锁等待 |
| 2026-09-01 | Agent `AppendEvents` batch sequence prefetch | Agent 全包回归；`just go-test-agent-journal-capacity` 连续两次通过，P95=240ms、211ms；100 SSE capacity 通过 | 生产 histogram 与更大规模并发 |
| 2026-09-01 | GraphRun execution lease 替代长 advisory | Graph lease takeover/fencing focused tests；`go test -C go ./internal/graph -count=1 -p 1` 通过 | 目标规模 lease 接管与续租失败观测；恢复锁等待 |
| 2026-09-01 | recovery duration、candidate lock query duration 与 PostgreSQL lock waiter metrics | `go/internal/platform/metrics`、dispatcher focused tests；metrics 文本包含固定域 histogram 和 lock waiter | 目标规模抓取成本与跨副本连接预算 |
| 2026-09-01 | ImageSession keyset cursor 与 Web infinite list | ImageSession cursor/HTTP focused tests；Web ImageSession API test、TypeScript/Vite build 通过 | 真实规模 query plan、删除/并发更新下的 page hit rate |
| 2026-09-01 | GraphRun summary/detail projection | Graph HTTP list omission/detail regression；GraphRunsPanel、GraphNodeInspector、run-event Web tests与 build 通过；同一 fixture 最新 summary p50/p95/p99=3.46/4.10/4.45ms、detail=3.80/4.87/5.29ms、最大 payload=15,629/2,623B；目标 plan 与 bundle budget 通过 | 生产详情打开率和多副本容量观察 |
| 2026-09-01 | Graph SSE status-only read 与 HTTP gate 入口 | `TestGraphRunStatusReadUsesOnlyIdentityColumns`、`TestGraphRunSSETracksActiveConnections`、`TestSubmitRunStagesPendingDispatch` 通过；`python3 -m py_compile scripts/bench_workbench_http.py` 与 `--help` 通过；新 API 隔离进程执行 `just http-ab-gates`：summary/detail 各 100/100，p95=4.10/4.87ms、最大 15,629/2,623B；current→runs 串行 p95=12.53ms；商品列表 page 20/100 串行 p95=3.31/4.00ms，20 并发 p95=15.55ms；所有 gate 通过；测量时 Go commit=`6726a49016d71a85cb9782a42f40c7a81f7a9e4b-dirty` | 旧 Python A/B 未启用；生产详情打开率和跨副本容量仍观察 |
| 2026-09-01 | Graph HTTP before/after、projection query count、target-scale EXPLAIN、workbench browser gate 与 route bundle budget | `just http-ab-gates` 同 Go A/B：2026-09-01 测量的 `HEAD -> dirty` current p95=10.32/9.73ms、runs p95=4.44/4.36ms；拆分前 -> 当前 runs p95=16.78/4.67ms，payload=67,705/15,629B，均 400/400；20/100/500 source-node projection SQL=51/211/1011 -> 10/10/10，GET=24.345/97.294/466.692ms -> 7.230/9.478/15.309ms；`just go-test-graph-query-plan`：25,000 runs/100,000 node-runs，摘要/详情 20/400/1/20 行，相关索引命中且无 Seq Scan；`just web-e2e-workbench-performance`：cold/warm TTI=2,170/2,044ms，current/runs 各 1，初始 detail=0，显式 run detail=1；预期 Agent bootstrap 409 已白名单，其它 HTTP/page/network failure=0；`just web-build` 与 `check_web_bundle_budget.py` 通过；Web tests 637/637、lint 通过 | Playwright fixture 没有 active run，因此 Graph SSE=0；真实多副本/用户详情打开率仍需部署观测 |

| 2026-09-04 | 实现基线 `fb658633` GraphRun HTTP、浏览器、bundle 与 query-plan 复核 | `just http-ab-gates` summary/detail 100/100，p95=4.09/4.71ms、最大 payload=15,629/2,623B，current→runs 串行 p95=13.59ms，商品列表串行/并发 p95=3.73/4.34/12.39ms；`just go-test-graph-query-plan` 通过，25,000 runs/100,000 node-runs 且无 Seq Scan；`just web-e2e-workbench-performance` 1 passed，cold/warm TTI=2,159/2,247ms，current/runs 各 1，初始 detail=0，显式 run detail=1，Graph SSE=0，4 条 console error 均为预期 Agent bootstrap 409；`just web-build` 与 bundle budget 通过，entry=928599B/253252B gzip，shell=501341B/146787B gzip；journal P95=232.800884ms、local WAL P95=0.81ms 另见 Agent 账本 | 旧 Python A/B、active-run Graph SSE、生产详情打开率和跨副本连接预算仍为观察项；Graph 性能 fixture 没有 active run |
| 2026-09-04 | 归档复核 | 只读核对 Graph、ImageSession、Agent、Delivery、LocalEdit recovery 与 metrics/list 查询 owner；未重跑性能专项 gate | 当时 HasMore 覆盖、详情 N+1、事务内整包 ZIP 仍为缺口；本账本不可归档 |
| 2026-09-04 | 恢复拆事务、NOTIFY、读路径与 ZIP | Agent `HasMore` 改为各阶段 OR；业务域单聚合 recovery；dispatcher `NOTIFY productflow_dispatch`；ImageSession history keyset；`mediaarchive` 事务外逐文件写临时 ZIP | 当时目标规模 query plan、ZIP MaxRSS、staging 故障注入未跑；本账本不可归档 |
| 2026-09-05 | 本 HEAD 复跑 Graph 目标规模 query plan | `just go-test-graph-query-plan`：25,000 runs / 100,000 node-runs；摘要 20/400 行 execution 0.022ms / 0.248ms，详情 1/20 行 0.016ms / 0.022ms；索引 `ix_workflow_graph_runs_graph_started`、`ix_workflow_graph_node_runs_run_node`；无 Seq Scan | 生产 buffers/p95 仍观察 |
| 2026-09-05 | 100 张近上限图 ZIP MaxRSS | `just go-test-zip-rss`：`TestStreamingZipNearLimitImagesKeepsExtraRSSUnderBudget` 通过；额外 MaxRSS=12.6MiB、额外 heap=0；ZIP 哈希稳定 | 图库 HTTP 512MiB 总上限仍阻止 100×10MiB 走 `BuildGalleryArchive` |
| 2026-09-05 | ImageSession 10k 轮次 / 1k 任务 history 索引与 query plan | ExtraDDL `ix_image_session_rounds_session_created`、`ix_image_session_generation_tasks_session_created`；`just go-test-imagesession-query-plan`：列表 0.019ms，history 首页/keyset 0.084ms/0.022ms，任务 LIMIT 21 为 0.025ms | 热会话独占整表时 COUNT/详情无 LIMIT 仍 Seq Scan；GET 详情仍装入全部匹配任务 |
| 2026-09-05 | 本地双副本进程级现场闸门 | `just go-test-staging-field`：通知丢失后另一 LISTEN 仍唤醒；两 API pool 独立 LISTEN；两 dispatcher SKIP LOCKED 各入队一次；两真实 dispatcher `--watch` 进程 SIGKILL 其一后另一副本仍 SENT（`TestReplicaFieldDispatcherSIGKILLSurvivor` 9.39s）；生图上限 1 时两 worker 1 running / 1 waiting；Graph 过期 lease 接管与迟到结果围栏测试纳入同一命令 | `just staging-up` 杀容器未跑；SSE 首帧 p95 未测；本账本不可归档 |
| 2026-09-05 | 连续生图详情任务读取有界，交付 `232e51f7` | 三组实际生产任务 SQL 各 LIMIT 20，去重最多 60；队列排名只返回所需 ID。包内测试 6.694s、目标规模 query-plan 3.141s、新增 HTTP/排名回归 race 三次 2.941s，均通过；详见[归档任务](tasks/archive/perf-imagesession-detail.md) | 首屏关联任务和 rounds COUNT 仍 Seq Scan；未验收生产 payload/延迟，PERF-08 保留部分完成 |
| 2026-09-05 | dispatcher 负载时延，测试交付 `a0d1ba4f` | queue 包回归 5.098s；`PRODUCTFLOW_RUN_DISPATCH_LATENCY=1` 单/双真实 dispatcher 连续三轮 31.652s，每场 500 个有效样本、25 个延期跳过；单副本 p95 2539.869/2567.677/2593.774ms，双副本 775.003/772.495/783.593ms。DB 时钟探针与 Redis 信封核对见[归档任务](tasks/archive/perf-dispatcher-latency.md) | 有效采证完成，单副本建议目标 FAIL；仅本地带探针突发负载，不作为生产 SLO；保留批间等待缺口 |
| 2026-09-05 | AR-02 journal 职责调查，认领 `324894a6` | 指定 Node 测试 61 passed / 1 skipped（10k WAL `runIf`）；`just docs-check` 通过。结论保留现状，见[归档任务](tasks/archive/arch-journal-assessment.md) | 本轮未跑 Go ConfirmEvents / fencing；Node recover claim 409 缺测；不发实现单 |
| 2026-09-05 | 生图 admission running/denied 指标 | `generationCapacityAvailable` 容量满打点；`/metrics` 输出 `productflow_generation_admission_running` 与带 `graph`/`imagesession` domain 的 `productflow_generation_admission_denied_total`。metrics 0.856s、graph 78.394s 通过，见[归档任务](tasks/archive/perf-capacity-metrics.md) | 未跑 imagesession replica field；入队路径满容量也会增加 denied；denied 非跨副本合计 |

验证记录不能把一次局部测试写成全量完成。工作树有其它未提交改动时，报告必须列出本次实际触碰的文件和测试范围，不得使用 clean checkout 作为默认假设。

## 暂不做

- 在当前单商户合同上提前加入 tenant_id、RLS、计费或兼容迁移。
- 把 Turn journal、Graph snapshot、effect ledger 或业务终态搬进 Redis。
- 用进程 mutex 伪装跨副本 GraphRun 执行权，或把 recovery 锁等待指标当成 capacity admission 指标。
- 为修复 N+1 给所有列表加无界 preload，或为修复慢查询复制大 JSON。
- 用提高 polling 频率掩盖通知丢失、outbox backlog、锁等待或 recovery 饥饿。
- 在没有 query plan、数据规模和回归测试时宣称索引或缓存“已经解决”。

## 更新规则

每次性能改动更新本文件的四个位置：

1. 当前基线或硬边界：只写已经存在且有代码 owner 的事实。
2. 当前治理状态：写状态、代码锚点、测试证据和缺口。
3. 优化顺序：写下一刀的依赖和禁止事项，不把计划写成已实现。
4. 验证记录：写实际命令、结果、数据规模、持续时间和未运行的 Gate。

稳定的数据权威、事务边界和锁序通过 Gate 后同步到 [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md)；尚未存在的方向留在 [`docs/ROADMAP.md`](../ROADMAP.md)。

<a id="production-gates"></a>

## 生产 Gate

本节承接原生产可靠性组的合同、逐项验收与历史基线。S1–S6 与 G-01–G-05、G-07 的已记录结果保留，G-07 只对登记基线有效；G-06 引用评测组可采信证据。采证任务关闭不等于生产 Gate 通过。

### 来源与使用规则

- 来源：Codex thread `01a05656-c0bf-7e92-94eb-046a74a6b71d` 在 2026-08-31 14:06:31 +08:00 输出的 `<proposed_plan>`《ProductFlow Agent 单商家生产就绪实施计划》。
- 原计划块 SHA-256：`d0d4ef1a1d67842bc0a610f74c1238218e57a74d6cb4ce27723028d013cf1115`，共 88 行。
- 本账本逐项转写该计划，不以较早的《Agent 企业级设计案》、memory、ADR 摘要或当前代码反向缩减范围。
- `完成`：当前代码、贴近合同的自动化测试和要求中的真实 gate 都有证据。只存在代码、只存在测试替身、只跑过窄测试均不能标完成。
- `部分完成`：已有可运行实现，但合同覆盖、故障场景、真实基础设施或容量证据不全。
- `缺失`：没有实现或没有足以判断的证据。
- `违背`：当前实现与计划明确合同相反。
- 每次状态变化必须同时填写代码 owner、测试/实测证据、最近验证日期；审计人不得只写“已实现”。

### 冻结决策

| ID | 决策 | 状态 | 当前证据与缺口 |
|---|---|---|---|
| D-01 | Turn 继续使用 `unknown`；中断通过 `terminal_reason_code` 和 `assistant/message.interrupted=true` 表达 | 完成 | 2026-08-31：`recovery.go` 按 `attempt_id` 组装 interrupted `assistant/message`；`AgentTurnTail` 用 `terminal_reason_code` 区分文案；`TestCrashAfterModelStartRecoversUnknownAndInterruptsInvocation` 与 compact 多 attempt 测试通过。 |
| D-02 | 工具恢复先对账；`applied` 自动收敛；`not_applied` 仅对 `reconcile_then_retry` 使用原幂等键重试；`conflict/unknown` 停止自动执行 | 完成 | 2026-08-31：HTTP 与 scanner 共用 `reconcileEffectIntent`；`TestReconcileEffectIntentEightToolStateMatrix` 覆盖八工具 applied/conflict/unknown/retry；二次 not_applied 不再持久化 `not_applied`；`TestReconcileTurnEffectAndScannerShareInterpreter` 通过。 |
| D-03 | 当前 provider 走 foreground；建立 `background_resumable` 合同和持久化结构；不增加绕过 Pi 的官方 OpenAI 执行器 | 完成 | 2026-08-31：Go `BackgroundResumable` = profile ∩ `agentAdapterBackgroundResumable=false`；Agent `effectiveBackgroundResumable`；`before_model_request` 拒绝 `execution_mode=background`；checkpoint 含 `model_response_bound/cursor`。无旁路 OpenAI 执行器。 |
| D-04 | 完成标准是单管理员、单商家生产就绪；保留 7 天 chunk 压缩；不含 SaaS tenant、计费和对象归档 | 部分完成 | 产品边界和 7 天压缩已按 attempt 绑定 `sourceEventSeqs`；G-03、G-04、G-05 有证据。G-06：2026-09-05 `just web-e2e-live-agent-workflow` 1 passed（审批卡 → 单一 WorkflowRun → 真实出图，7.4m）。评测 L1–L6 门槛仍以 [`agent-eval-system.md`](agent-eval-system.md) 的 `run_id` 为准，未过不得采信分数。G-07 干净 checkout 全量门尚未在本 HEAD 重跑。 |
| D-05 | 容量 gate 是 25 个并发 Turn、100 条 SSE、单 Turn 1 万事件、单会话 1000 Turn | 完成 | 2026-09-04 当前 HEAD 复核：`just go-test-agent-journal-capacity` 10k 事件 P95=232.800884ms、25 Turn×128、100 SSE overflow 503；`just agent-service-test-local-journal-capacity` 本地 10k WAL P95=0.81ms。2026-09-01 已有 `TestLastFiftyTurnsQueryP95`、`TestAgentSSETimeToFirstEventP95` 和 Chromium gap repair 证据，均在阈值内。 |

### 协议与数据合同

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

### 实施切片

#### S1 合同与 Schema 基础

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S1-01 | Manifest、生成器、Go 映射、checkpoint 枚举、provider capability DTO、Turn DTO、数据库模型与约束一致 | 完成 | 2026-08-31：`background_resumable` DTO、`model_response_*` checkpoint 枚举与 ExtraDDL、invocation unique 约束与 Go/TS 测试对齐。 |
| S1-02 | `before_model_request` 在同一事务幂等创建/读取 invocation；`assistant/message` 按 request ID 闭合状态与 usage | 完成 | 2026-08-31：`TestModelInvocationIdempotentCreateAndUsageDedup` 重放 checkpoint 与二次 message 不去重写 usage；`duration_ms=42` 与 response identity 写入。 |
| S1-03 | migration 走 GORM `CreateTable`/`AddColumn` + ExtraDDL；唯一约束覆盖 request ID、provider response ID、effect reconciliation identity | 完成 | 2026-08-31：`uq_agent_model_invocations_provider_response_id` ExtraDDL；`TestProviderResponseIDUniqueAcrossInvocations` 跨 invocation 409。 |

#### S2 崩溃恢复解释器

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

#### S3 审批和等待输入恢复

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S3-01 | workflow request、graph proposal、global draft、`ask_user` 使用稳定 pending identity；重复/迟到回答和取消竞争统一为 `not_pending`/409 | 完成 | 2026-08-31：`apperr.NotPending` HTTP `code=not_pending`；`TestPendingIdentityConflictsShareNotPendingCode` 覆盖迟到 ask_user、重复回答、已取消 workflow confirm、已结束 graph proposal、非待确认 draft。 |
| S3-02 | 重启后 pending 审批从 PG 重投影 requested；resolved 只回放 resolved，不重新开放 | 完成 | 2026-09-01：`TestCrashAfterApprovalKeepsPendingRequestWithoutSecondRun` 过期 lease 不把 awaiting_confirmation 改成 unknown。Chromium `agent-workbench-recovery.spec.ts` page reload 后仍 `awaiting_confirmation`；事件页 `approval.requested` 1 条、`approval.resolved` 0 条。 |
| S3-03 | waiting-input 重启仍显示原问题；答案已落库但未继续时创建新 attempt，并向 Pi 注入同一 question/tool result | 完成 | 2026-08-31：`TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult` 对真实 Pi 进程 SIGKILL；过期 lease 后问题仍 `requires_input`；503 后 PG 仍有答案；第二进程 `SyncTurn` 注入 `ask_user` tool result 并 `succeeded`。辅助合同：`TestClaimNewAttemptResetsCheckpointSequence`、`TestSyncTurnAppliesQueuedResumeAfterDurableAnswer`、`TestClaimAllowsExpiredRequiresInputWhenPhaseIsModel`；Agent `store.test.ts` 对带 `execution_attempt` 的 parked question 记 `waiting_input`。 |
| S3-04 | 恢复不得创建第二业务请求、proposal 或 WorkflowRun | 完成 | 2026-09-01：`TestConfirmWorkflowRunRequestDoesNotCreateSecondGraphRun`；`TestRecoverExpiredProposalTurnDoesNotCreateSecondProposal` 过期恢复后同幂等键仍同一 `proposal_id`、表内 1 行；`TestRecoverExpiredDraftTurnDoesNotCreateSecondDraft` 恢复不写第二 organization draft。含在 `just go-test`。 |

#### S4 ConversationRuntime Gap 自愈

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S4-01 | Runtime 有 connection generation；新连接后忽略旧 generation 的 open/error/message | 完成 | 2026-08-31：`runtime.test.ts` 与 Chromium `agent-conversation-runtime.spec.ts` 均忽略旧代 open/error/message。 |
| S4-02 | `sequence > cursor + 1` 进入 repairing，缓冲新事件、从 cursor 分页补齐；补洞期间保持当前 generation 的 EventSource（见决策变更 2026-09-05） | 完成 | 2026-09-05：`runtime.test.ts` 断言补洞后 `source.closed=false`；Chromium `just web-e2e-agent-sse` 5 passed，其中 gap 用例 `generationCount=1`、`liveClosed=false`、`received=[1,2,3]`、事件页调用 1 次；真实 PG 事件页补洞 ≤5s 仍通过。 |
| S4-03 | 补齐后按 seq 去重并连续应用，排空 buffer；未变化 Item 引用稳定 | 完成 | 2026-09-05：Chromium 与 `runtime.test.ts` 证明 repair 去重、连续 apply，并保持同一 EventSource。`agentEventReducer.test.ts`「keeps unchanged tool step object identity across later text deltas」断言未变化 tool step 引用稳定。 |
| S4-04 | buffer 上限 512 条或 1 MiB；超过上限、连续三代补洞失败或服务端事件矛盾才进入 protocol error | 完成 | 2026-09-05：`runtime.test.ts` 512 条上限；`stream.complete` 后事件页跳过下一 seq 立即 protocol error「未返回 sequence 2」，不再重连。Chromium matrix：overflow / terminal gap / parked approval 通过（`just web-e2e-agent-sse`）。 |
| S4-05 | terminal/finite 流也补洞到终态或 `stream_state=terminal`；正常 complete 不显示断线 toast | 完成 | 2026-08-31：finite/stream.complete 先 probe 再关闭；`onStreamError(null)`；Chromium terminal gap 与 parked approval 不误关。 |
| S4-06 | 同 Turn Runtime 单例；Workbench 和全局 Dock 不产生第二 EventSource | 完成 | 2026-09-01：registry/consumer 单例与 `loadHistory` 无 EventSource 已有单元覆盖。Chromium `agent-workbench-recovery.spec.ts` 工作台侧栏 Agent + 「放大至全局主控台」后，同一 live Turn 的 EventSource 路径集合 size=1。 |

#### S5 Usage、指标与告警

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S5-01 | durable usage source 仅 provider/estimated/unavailable；缺失记 unavailable，不用 0 冒充 | 完成 | 2026-08-31：`finishModelInvocation` 全 0 保持 unavailable；`usage_source=estimated` 写入；`durableAssistantUsage` 丢弃全 0；`TestModelInvocationMissingUsageStaysUnavailable` / `TestModelInvocationEstimatedUsagePersists`。 |
| S5-02 | 指标覆盖 Turn、事件批次/延迟/冲突、lease、恢复、pending approval、SSE、provider、usage missing、dispatcher backlog、图运行 | 完成 | 2026-08-31：Go snapshot 含 turns/runs/dispatches/executions/reconciliations、batch count/last ms、sequence conflict、recovery unknown、pending workflow/graph/draft/ask_user、expired leases、usage missing；Agent Service `/metrics` 含 batch/conflict/provider 4xx/5xx。 |
| S5-03 | label 仅 bounded status/phase/tool/provider/model；禁止业务 ID、用户文本、错误详情 | 完成 | 2026-08-31：`safeLabel` / `safeMetricLabel`；`TestAgentAlertRulesCoverRequiredNames` 禁止 conversation_id/turn_id/user_text/error_detail。 |
| S5-04 | 告警覆盖投影不一致、活动超时、unknown、effect unknown、lease lost、sequence conflict、usage missing、backlog、provider 5xx | 完成 | 2026-08-31：`ops/prometheus/agent-alerts.yaml` 九条规则；`TestAgentAlertRulesCoverRequiredNames` 通过。未做 live Prometheus scrape。 |
| S5-05 | token 和内部指标不进入商家工作台，只经 invocation 数据和 Prometheus 提供运维能力 | 完成 | Web 未读取或展示 token/内部 metrics；当前仅 PG invocation 与受保护 Go metrics 暴露。 |

#### S6 历史压缩与读取边界

| ID | 验收要求 | 状态 | 证据与缺口 |
|---|---|---|---|
| S6-01 | 保留 7 天窗口；只在 canonical assistant message 已存在时压缩 chunk；按 attempt/message 正确绑定 `sourceEventSeqs` | 完成 | 2026-08-31：`compact.go` 按 attempt 绑定 text/thinking；`TestCompactExpiredTurnJournalsKeepsThinkingWithoutCanonicalSnapshot` 与 `KeepsTextChunksWithoutMessageSnapshot`。 |
| S6-02 | tool result、approval、usage、terminal、checkpoint 不压缩；任务有界、幂等并暴露指标 | 完成 | 2026-08-31：只更新 text/thinking chunk；50 Turn 批次；`productflow_agent_journal_compact_turns/errors` 计数。 |
| S6-03 | 会话列表 cursor 分页且默认最近一页；历史 Item 按需读事件页；浏览器不常驻完整日志 | 完成 | 2026-08-31：`ListSessions` cursor + `next_cursor`；Web infinite query；settled Turn 走 `loadHistory` 不创建 EventSource；`agentTurnNeedsEventStream` 对 succeeded/failed/canceled/unknown 为 false。 |

### 测试与生产 Gate

| ID | 必须通过的 Gate | 状态 | 最近命令/环境/结果 |
|---|---|---|---|
| G-01 | 单元/合同：manifest policy codegen、checkpoint payload、invocation 幂等、reason code、事件分页、unknown ignorable、usage 去重 | 完成 | 2026-09-04 当前 HEAD：无缓存 `go test -C go ./... -count=1 -p 1` 通过；`just agent-service-test` 20 files passed、1 skipped，167 passed / 2 skipped；`pnpm --dir web test:run` 93 files、637 passed。Agent contract artifact check 在 pretest 中通过。 |
| G-02 | PostgreSQL：八类工具 effect 状态矩阵、同幂等键重复 scanner、多 dispatcher、旧 fencing writer | 完成 | 2026-09-01：八工具四态矩阵与共享解释器；`TestRecoverExpiredExecutionsRejectsStaleFencingWriter` 拒绝旧 lease writer 与过期 fencing；`TestConcurrentExpiredRecoverySkipLockedDoesNotDoubleTerminate` 两个 scanner 并发 SKIP LOCKED，4 条 Turn 各一 `turn/end`。含在 `just go-test`。 |
| G-03 | `kill -9` 四点故障注入：模型开始后、mutation 成功/result 前、approval 后、turn/end 落库/响应前；验证连续 seq、至多一次副作用、诚实终态、无永久活动 Turn | 完成 | 2026-09-01：`TestSIGKILLLeaseHolderAgainstGoPG` 对真实 httptest Go+PG 四点 SIGKILL helper：`model_start` / `mutation` / `approval` / `turn_end`；连续 journal、mutation 副作用至多一次、诚实终态。含在 `just go-test`。 |
| G-04 | 浏览器：普通断线、gap、重复帧、旧 generation 迟到帧、buffer overflow、terminal gap、approval 刷新；无错误断线 toast | 完成 | 2026-09-05 HEAD `ebc7630d` 工作树：`just web-e2e-agent-sse` 5 passed（PG 事件页补洞 ≤5s；原地事件页补洞不丢 live EventSource；generation/overflow/terminal gap/parked approval；duplicate seq1 再 seq2；raw EventSource cursor 重连）。 |
| G-05 | 标准容量：25 Turn、100 SSE、1 万事件/Turn、1000 Turn/会话；零洞、零重复副作用、零永久活动；P95 batch PG <=300ms、SSE <=1s、gap repair <=5s、最近 50 Turn <=500ms | 完成 | 2026-09-04 当前 HEAD 复核：`just go-test-agent-journal-capacity` deep_events=10000 concurrent_turns=25 batches=207 p95=232.800884ms；100 SSE overflow 503；`just agent-service-test-local-journal-capacity` 本地 10k WAL duration=6351.4ms p95=0.81ms。2026-09-01 的 `TestLastFiftyTurnsQueryP95`、`TestAgentSSETimeToFirstEventP95` 与 Chromium gap repair 证据仍在阈值内。 |
| G-06 | 真实 gate 使用 `gpt-5.6-luna`：完整 skill eval、真实审批到 WorkflowRun、真实图片图运行、真实 Chromium；确认 `background:true` unsupported | 部分完成 | 2026-09-05：`just web-e2e-live-agent-workflow` 1 passed（7.4m）；skip-Agent 建画布后打开对话，审批卡「确认并执行」，单一 WorkflowRun succeeded，生成图为真实 PNG/JPEG/WEBP。`just web-e2e-live-graph` 此前已通过。JSON 任务集 L1–L6 出口仍只以 [`agent-eval-system.md`](agent-eval-system.md) 登记的 `run_id` 与门槛为准；历史 15/15 冒烟不满足该账本阶段出口。Go/Pi 的 `background:true` 拒绝测试仍以无缓存 Go 全量门为准。 |
| G-07 | 全量：Go、Agent Service、Web test/lint/build、docs-check、migration fresh/upgrade、`git diff --check`、干净 checkout 重跑 | 完成 | 2026-09-04 当前 HEAD `fb658633`：全量 gate 启动时代码工作树干净；随后本次账本更新仅修改三份 `docs/audits/` 文档。无缓存 `go test -C go ./... -count=1 -p 1`、`just agent-service-test`（167 passed / 2 skipped）、`pnpm --dir web test:run`（637 passed）、`pnpm --dir web lint`、`just web-build`、`just docs-check`、`just go-migrate`、`git diff --check` 均通过。schema fresh/upgrade 由 `TestApplyEmptyDatabaseMatchesHeadConstraints`、`TestApplyTwiceDoesNotDeleteRows`、`TestApplyExistingHeadKeepsSchema` 覆盖并通过。 |

### 持久日志提交语义

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

### 验证记录

#### 2026-09-05 Agent 审批到 WorkflowRun Chromium

- HEAD：`ba4508d7` 之后工作树含本闸门 spec 修正；他人 fidelity / image-eval WIP 未纳入。
- `just web-e2e-live-agent-workflow`：1 passed，墙钟 7.4m。路径：只建画布 → 「创建并打开对话」→ 发送「请执行当前工作流」→ 审批卡「确认并执行」→ GraphRun succeeded → 图库真实生成图。

#### 2026-09-05 Agent SSE Chromium

- HEAD：`ebc7630d`；工作树另有他人 fidelity / image-eval WIP，未纳入本闸门。
- `just web-e2e-agent-sse`：5 passed（7.5s）。Vitest conversation runtime 12 passed。
- `stream.complete` 后事件页跳过下一 seq 立即 protocol error，不再 scheduleReconnect。

#### 2026-09-04 当前 HEAD

- HEAD：`fb658633`；全量 gate 启动时 `git status --short` 为空、代码工作树干净；随后本次审计更新仅留下三份 `docs/audits/` 文档未提交修改。
- 无缓存 Go 全量：`go test -C go ./... -count=1 -p 1` 通过；包含 Agent、Graph、schema fresh/upgrade 测试。
- Agent service：`just agent-service-test`，20 files passed、1 skipped；167 passed / 2 skipped；contract artifact check 通过。
- Web：`pnpm --dir web test:run`，93 files、637 passed；`pnpm --dir web lint` 通过；`just web-build` 通过，bundle budget 通过。
- Schema/docs：`just go-migrate`、`just docs-check`、`git diff --check` 通过。
- 容量：journal 10k/25 Turn/100 SSE P95=232.800884ms；local WAL 10k P95=0.81ms；Graph target-scale query plan 通过且无 Seq Scan。
- Graph read gate：`just http-ab-gates` summary/detail 100/100，p95=4.09/4.71ms；workbench performance Chromium 1 passed，cold/warm TTI=2159/2247ms，初始 detail=0，显式 run detail=1，预期 Agent bootstrap 409 之外无 page/network/HTTP failure。
- 真实 provider：`just agent-evals-live` 15/15；`just web-e2e-live-graph` 1 passed。当前 HEAD 尚未重跑真实 Agent 审批到 WorkflowRun 的完整 UI 链，因此保留 G-06 为部分完成。

#### 2026-09-01 当前 checkout

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

#### 2026-08-31 当前 checkout

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

### 决策变更

#### 2026-09-05 S4-02 / S4-03：补洞保持 live EventSource

- 日期：2026-09-05
- 提出者：验收闭环对照 `runtime.test.ts` 与 Chromium 闸门
- 原条款：`sequence > cursor + 1` 时关闭旧 SSE，再从新 cursor 重连
- 替代条款：补洞期间保持当前 generation 的 EventSource，用事件页填洞并排空 buffer；仅在 protocol error、终态或退订时关闭
- 迁移影响：无持久化变更；浏览器只消费已提交 PostgreSQL 事件的合同不变
- 验收 Gate：`pnpm --dir web exec vitest run src/pages/workbench/agent/conversation/runtime.test.ts` 12 passed；`just web-e2e-agent-sse` 5 passed

暂无其它变更。任何变更必须记录日期、提出者、替代条款、迁移影响和验收 Gate；不得直接覆盖旧条款。
