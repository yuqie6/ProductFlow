# ProductFlow 性能治理与容量指导

本账本记录 ProductFlow 当前运行时的性能模型、容量边界、锁与事务约束、可执行的优化顺序和验收方法。它服务于 Graph、Agent、连续生图、异步投递、SSE 和数据库查询的跨层改动。

本文不是用户功能说明，也不替代 [`agent-production-readiness.md`](agent-production-readiness.md) 或 [`agent-runtime-ownership.md`](agent-runtime-ownership.md)。当前代码、测试和真实运行仍是最终证据；账本中的目标、预算和未验证项必须标明状态。

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
| Graph 执行 | 一个 GraphRun 由一个 worker 入口处理；互不依赖节点可以并发打 provider | 当前 run advisory lock 会跨整个 execute loop；一个 asynq 槽可能持有整张 DAG | `go/internal/graph` |
| Agent 执行 | PostgreSQL lease、fencing 和 journal 为权威；Node/Pi 只负责 adapter 和私有 WAL | lease 行锁只在短事务内；journal 批量写入仍需观测 | `go/internal/agent`、`agent-service` |
| 连续生图 | Graph 与 ImageSession 共用全库 `generation_max_concurrent_tasks` 和 advisory capacity lock | 当前默认上限 3，所有商家共享一个 admission 点 | `graph/durability.go`、`imagesession/execute.go` |
| SSE | Agent、Graph、ImageSession 的 PostgreSQL NOTIFY 在同一进程按 pool 共享 fanout；通知丢失后按游标或状态轮询 | 每个 API 副本至少有一条 listener 连接；不是每个浏览器一条连接 | `platform/notify` 与各 SSE handler |
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
| GraphRun 列表 | 最近 20 条，当前仍带 node runs | `graph/service.go`、`graph/run_dto.go` |
| ImageSession 列表 | 当前无分页；摘要查询已改为批量聚合 | `imagesession/service.go`、`serialize.go` |
| 图片生成容量 | `generation_max_concurrent_tasks` 默认 3，限制 1-20 | `graph/durability.go`、`settings/catalog.go` |
| Agent service turn 并发 | 默认 3，进程内控制 | `agent-service/src/config.ts`、`pi-runtime.ts` |

## 当前治理状态

以下状态以当前工作树为准；最终完成状态必须在相关 Gate 通过后更新。

| ID | 项目 | 状态 | 当前措施与证据 | 剩余缺口 |
|---|---|---|---|---|
| PERF-01 | Graph 行锁反向边 | 部分完成 | Graph 事件追加收口为 `appendGraphRunEventLocked`；run mutation 使用 `run -> graph -> node` 或 `run -> node -> effect`；文稿自动采用改为先锁 run 再锁 live graph；当前全量 Go gate 通过 | 需要专门的并发取消/自动采用 regression，不只依赖普通执行测试 |
| PERF-02 | Agent Task 与 Turn projection 反向边 | 部分完成 | 全局 Draft 确认先 `FOR UPDATE projection` 再锁 Task，与 journal 终态投影路径一致；Agent lock-order tests 和全量 Go gate 通过 | 需要保留跨事务死锁 gate，并继续核对稳定架构文档的 owner 表 |
| PERF-03 | node/run 隐式反向边 | 部分完成 | `failBlockedQueuedNodes` 先锁 run，再改 node 并追加事件；事件 helper 不再隐藏获取 run 锁；Graph 全量 Go gate 通过 | 仍需 Graph 并发 regression，覆盖 recovery、cancel、worker tick |
| PERF-04 | 生图容量锁顺序 | 部分完成 | Graph 为 `capacity advisory -> run -> node`；ImageSession claim 改为 `capacity advisory -> task`，取得 task 锁后重新核对状态 | 全库单钥匙和 noisy neighbor 仍存在；SaaS 前需按 workspace/tenant 重构 |
| PERF-05 | Agent 与业务 recovery 长事务 | 部分完成 | Agent 的过期 execution 回收、queued Task reserve、projection 查询、outbox restage 已分阶段提交；所有 recovery owner 默认每轮最多 25 条，业务域使用 `SKIP LOCKED`，queued 行过滤活跃 outbox；summary 的 `has_more` 已进入 dispatcher 结构化日志，API `/metrics` 提供 queued backlog | 各域仍在一个有界事务内处理一批聚合；仍需 stale-running backlog 分项、批次耗时与锁等待指标 |
| PERF-06 | dispatcher recovery 拖慢投递 | 部分完成 | dispatch loop 与 recovery cadence 解耦；watch 默认每秒投递、每 10 秒 recovery | 仍需 pg_notify/Redis 唤醒和 recovery 任务分层；需要负载下验证 dispatch latency |
| PERF-07 | SSE 连接占用 | 部分完成 | `platform/notify.Subscribe` 按 pool 在进程内共享一条 LISTEN，Agent/Graph/ImageSession 共用 fanout；缓冲满时丢通知并依赖 PG 回读 | 每个 API 副本仍有 listener；需要跨副本连接预算和 listener 健康指标 |
| PERF-08 | ImageSession 列表 N+1 | 部分完成 | 最新轮次与资产、round count 改成批量查询；`image_sessions(updated_at DESC,id DESC)` 索引已写入并完成本地迁移 | 列表仍全表读取且无分页；详情页任务/effect/队列总览仍有多次查询 |
| PERF-09 | GraphRun 列表 N+1 与排序 | 部分完成 | run 与 node runs 改成批量读取；`(graph_id, started_at DESC, id DESC)` 索引已写入并完成本地 schema migration | 列表仍加载 snapshot 与 node runs；需要轻量摘要 DTO 或分离详情读取和 query plan 记录 |
| PERF-12 | Agent Session 列表 N+1 | 部分完成 | 当前页 session、每个 session 最近 20 条 conversation 和 count 改成批量查询；21 条 conversation limit regression 通过 | 需要真实规模 payload/query plan；单条详情路径仍按一个 session 组装三类数据 |
| PERF-10 | tenant 和真实身份 | 待实施 | 当前明确维持单管理员、单商家合同，不提前给现有 live 表补 tenant_id | SaaS 起点需同时设计 principal、数据范围、配额、审计和存储，不做局部补丁 |
| PERF-11 | Graph 长生命周期 advisory | 观察 | `pg_try_advisory_lock` 目前保证一个 GraphRun 一个 worker，provider 调用期间持有 session connection | 需要 row lease 或短事务 claim 的完整替代设计；迁移前不能直接删除 advisory |

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

Graph 当前还有一条独立于短事务行锁的长生命周期 advisory：它服务于“一个 GraphRun 一个 worker”的现有合同，并会钉住一条 SQL connection 跨过 execute loop。它应作为单独设计处理，不能被容量锁、SSE fanout 或普通行锁重构顺手删除。

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

状态：dispatch/recovery cadence、Agent recovery 事务、全部 recovery owner 的有界候选批次、dispatcher `has_more` 日志和 API queued backlog 指标已落地；批次内事务拆分、stale-running 指标和 admission 仍待实施。

1. dispatcher watch 模式持续以 dispatch interval 处理 outbox；recovery 使用独立 cadence。首轮 recovery 立即运行，失败不推进成功时间戳。
2. Agent、Graph、ImageSession、Delivery、LocalEdit 已使用默认 25 条上限；业务域使用 `status + stale predicate + SKIP LOCKED` 和稳定时间/id 排序，dispatcher 日志记录每个 owner 的 `has_more`，API `/metrics` 直接读取 PostgreSQL 的 queued backlog。下一步是 stale-running 分项、批次耗时和锁等待指标，验证没有稳定排序下的隐性饥饿。
3. 把当前每批事务继续拆成候选 claim、单聚合状态迁移、restage 的短事务；不要让批次大小随业务行数增长。
4. 单商家阶段继续用 PostgreSQL capacity advisory 保证准确 admission，但把查询/锁等待和 running count 记录出来。SaaS 前替换为按 workspace/tenant 的 admission token；PostgreSQL 只做对账。
5. 评估 GraphRun 长 advisory 的 row lease 替代方案：需要 owner id、lease expiry、fencing 或 CAS、续租、超时接管、迁移期间双 worker 行为和 crash test。没有完整设计时保留现行合同。

### P2：查询、列表和实时通道

状态：SSE fanout、ImageSession/GraphRun/Agent Session 列表的批量摘要查询和两个排序索引已在工作树中；ImageSession 分页、GraphRun 轻量摘要和详情路径优化待实施。

- 高频列表采用 keyset cursor；cursor 必须绑定筛选条件、排序版本和 tie-breaker id。不要用大 offset 代替 cursor。
- 轻量列表只选择摘要列；snapshot、full node runs、全部 rounds、全部 effect ledger 和大 JSON 通过详情路由或按需页读取。
- ImageSession 列表保留现有 response shape 时，先继续使用批量 count/latest asset 查询；要增加 cursor 必须同步 Go DTO、OpenAPI、`web/src/lib/api.ts`、前端 query key 和列表测试。
- GraphRun 列表与详情分开：列表最多取 id/status/timestamps/progress 摘要，详情再读取 snapshot 和 node runs。先确认前端是否依赖当前 node runs 后再改合同。
- Agent Session 列表已经分页，当前页的 session、conversation 摘要和 count 已批量取；单条 `loadSession` 仍保持同一投影 helper，保持 global conversation 补齐的写边界不变。真实数据规模下仍需检查 payload 和 query plan。
- SSE 使用共享 fanout；每条连接必须 `defer unsubscribe`。listener 关闭或池不可用时切换有限轮询。每个副本的 listener 和连接池预算都要纳入部署容量。
- 索引只服务于已确认的 predicate + order：当前补充 `image_sessions(updated_at DESC,id DESC)` 和 `workflow_graph_runs(graph_id,started_at DESC,id DESC)`，通过 `just go-migrate` 后用 `EXPLAIN (ANALYZE, BUFFERS)` 验证；无证据不新增大范围复合索引。

### 媒体和内存

当前性能风险集中在媒体 bytes 和大 JSON：

- `product/gallery_archive.go` 可能把整个 ZIP 聚合在 `bytes.Buffer`；目标是受限流式写出或明确总字节预算，超过预算返回可理解错误。
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

- `imagesession.Service.List`：当前保留不分页合同，`image_sessions` 全量读取后使用批量 round count/latest asset 查询。下一刀应先设计 cursor，再处理前端无限列表和删除后的 cursor 行为。
- `graph.Service.ListRuns`：当前最多 20 条，但每条序列化 node runs 并携带 snapshot-derived data。下一刀应以实际前端字段为依据拆分 summary/detail。
- `agent.listSessions/loadSession`：主查询已经 cursor page，当前页内仍可能产生 conversations/count N+1。批量查询必须保留 global conversation 的补齐逻辑在命令事务边界内。
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

## SSE 与连接池指导

### 事件通道

业务写事务提交时可以 `pg_notify` 唤醒订阅者。`platform/notify.Subscribe` 在同一 `*pgxpool.Pool` 上共享 listener，订阅者是有界缓冲 channel：

- payload 为空或不是当前聚合 id 时，SSE 继续回到自己的 cursor/状态读取。
- subscriber buffer 满时丢弃通知，调用方依靠 PostgreSQL 回读恢复，不得扩大 buffer 代替回放。
- listener 出错后，handler 切换有限 polling；polling 也必须有 heartbeat、终态退出和请求取消。
- handler 结束必须执行 unsubscribe，最后一个 subscriber 退出后释放 listener connection。
- 连接预算按“每个 API 副本一条 listener + 普通查询连接 + worker/dispatcher 连接”计算；不再按“浏览器数 × SSE 路由”估算 PostgreSQL listener 数。

### 验收指标

当前 metrics 端点是 bearer 保护的文本端点，已有低基数状态和 Agent 指标。性能改动应补充或采集：

- dispatch PENDING backlog、SENT stale 数、claim batch size、mark-SENT 到 enqueue 耗时。
- recovery 每域 processed/changed/unknown/skipped/has_more、事务耗时、锁等待和错误次数。
- Graph capacity advisory wait、running count、capacity denied 次数。
- PostgreSQL pool acquired/idle/max、listener count、query latency 和 deadlock/serialization error count。
- SSE active connections、listener reconnect、poll fallback、subscriber drop；跨副本指标要带 instance，不能把进程内 gauge 当总数。
- ImageSession/Graph/Agent 列表 rows、query time、payload bytes 和 cursor page hit rate。
- Agent 已有 `productflow_agent_event_batch_last_ms`、journal compact、expired lease、sequence conflict 和 SSE connection 指标，改变 batch/WAL 时必须继续跑容量门。

建议初始预算如下，正式 SLO 需用目标数据规模校准：

| 路径 | 初始目标 | 超标后的动作 |
|---|---:|---|
| 普通列表首屏 | p95 < 300ms，payload < 1MiB | 查 query plan、N+1 和 JSON 体积 |
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
| 列表分页/N+1 | 对应 Go 包 + `web/src/lib/api.ts` + 页面 owner | DTO/API/frontend tests；Web build；query plan 与 payload 对比 |
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
```

推荐的容量专项：

- Agent journal：`just go-test-agent-journal-capacity`。
- Agent local WAL：`just agent-service-test-local-journal-capacity`。
- Graph/Image provider：使用 mock provider 跑并发 claim、容量满、timeout、unknown 和 recovery。
- SSE：每个 channel 的持久化回放、通知丢失、listener 关闭、heartbeat、终态退出、连接上限和 pool 使用量。
- DB：以目标行数跑列表、recovery、outbox claim 和 `EXPLAIN (ANALYZE, BUFFERS)`；记录 p50/p95/p99，不只记录一次成功响应。

## 当前工作树验证记录

| 日期 | 变化 | 已验证 | 未完成 |
|---|---|---|---|
| 2026-09-01 | Graph lock helper、Graph event call sites、Agent Draft confirmation lock order | Graph focused execution tests；Agent focused lock/recovery tests | Graph 专门并发 deadlock test；全量 Go gate |
| 2026-09-01 | Agent recovery 分阶段事务、per-Task reserve、有限候选批次与 `has_more` | Agent recovery focused tests；精确 Pi integration 连续 3 次通过；第二次 `just go-test` 全量通过 | 外部 Pi integration 曾在一次受影响包集合中出现时序失败，需保留真实 Agent 环境容量复核；stale-running backlog 与锁等待 |
| 2026-09-01 | ImageSession capacity -> task lock order | ImageSession execute/recovery/HTTP focused tests | capacity wait/lock-order专项测试 |
| 2026-09-01 | `notify.Subscribe` fanout 下沉，Agent/Graph/ImageSession 共用 | notify tests、Agent shared listener test、ImageSession SSE test | Graph SSE 专项命名测试；跨副本连接预算 |
| 2026-09-01 | ImageSession summary 批量聚合 | ImageSession list/execute HTTP focused tests | 真实规模 query plan、无分页合同改造 |
| 2026-09-01 | GraphRun 与 Agent Session 列表批量投影 | Graph full package、Agent Session CRUD 和 21 conversation limit regression 通过 | 真实规模 payload/query plan |
| 2026-09-01 | dispatcher `--recovery-interval` 默认 10 秒与 recovery 错误隔离 | dispatcher package tests；第二次 `just go-test` 全量通过 | watch loop integration 和 dispatch latency load test |
| 2026-09-01 | ImageSession/GraphRun 排序索引 ExtraDDL | `just go-migrate`；`go test -C go ./internal/platform/db/schema -count=1 -p 1` 通过 | 目标数据规模的 `EXPLAIN (ANALYZE, BUFFERS)` |
| 2026-09-01 | Graph/ImageSession/Delivery/LocalEdit recovery 有界候选批次与 Agent `has_more` 汇总 | recovery focused tests；dispatcher/Agent focused tests；第二次 `just go-test` 全量通过 | stale-running backlog 分项、批次事务耗时与锁等待 |
| 2026-09-01 | API queued recovery backlog 与 dispatcher recovery duration | metrics snapshot integration；dispatcher/metrics focused tests；最新 `just go-test` 全量通过 | stale-running backlog 分项、Prometheus histogram 和锁等待 |

验证记录不能把一次局部测试写成全量完成。工作树有其它未提交改动时，报告必须列出本次实际触碰的文件和测试范围，不得使用 clean checkout 作为默认假设。

## 暂不做

- 在当前单商户合同上提前加入 tenant_id、RLS、计费或兼容迁移。
- 把 Turn journal、Graph snapshot、effect ledger 或业务终态搬进 Redis。
- 直接删除 GraphRun advisory lock，或用进程 mutex 伪装跨副本互斥。
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
