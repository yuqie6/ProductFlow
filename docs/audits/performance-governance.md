# 平台可靠性组：执行、恢复与资源成本

本组负责商家任务的执行正确性、故障收敛和可承受的资源成本。方向保持不变：已接受的操作有明确去向，重复投递不重复业务副作用，重启后状态可信，数据增长和并发不会让正常工作失去响应。

本文拥有本组的风险判断、验收口径和后续工作选择。产品边界见 [PRD](../PRD.md)，运行单元与代码所有权见 [ARCHITECTURE](../ARCHITECTURE.md)，稳定数据权威见 [CONTEXT](../../CONTEXT.md)。任务与认领只维护在 [公共看板](tasks/README.md)。旧实施切片、逐次跑分和已关闭计划收入 [历史证据](../history/agent-runtime-timeline.md#platform-reliability-evidence)，不构成当前任务清单。

## 目标与范围

本组以五个可观察结果判断工作是否有价值：

1. 用户提交后能区分排队、执行、等待输入和终态；不能因进程退出永久停在活动状态。
2. 重试、恢复和重复确认不产生未经授权的第二次业务操作；不可证明的供应商结果保持 `unknown`。
3. 已提交的状态与产物可重新读取；浏览器断线、通知丢失和 Node 重启不形成第二份业务事实。
4. 正常交互与后台恢复可以共存；锁、连接、扫描、响应体和媒体内存的成本与实际工作量相称。
5. 发布判断绑定代码版本、配置、数据形状和有效证据；能说明哪些已验证，哪些仍不确定。

当前产品是单管理员、单商家工作区。tenant、真实用户身份、计费、公平配额和对象归档属于 [SaaS 新基线](../ROADMAP.md#saas)，不记作当前可靠性缺陷，也不通过零散补 tenant 字段开工。

| 交付问题 | 本组负责 | 交接边界 |
|---|---|---|
| 任务丢失、重复执行、恢复后状态错误 | lease、fencing、journal/ACK、outbox、幂等和副作用对账 | Agent 如何理解商家意图归 Agent 质量；执行协议故障仍由本组追完整调用链 |
| 页面慢、队列拥堵、数据库或内存压力 | 对应请求/任务的资源放大点、必要查询/事务/调度修复 | 画布操作语义归工作流体验；图片内容质量归图片质量 |
| 断线或重启后的实时状态 | 持久状态回读、事件补洞、订阅生命周期、连接预算 | 组件交互与后端合同冲突时，由一张主 issue 覆盖必要前后端范围 |
| 候选发布是否可用 | 本组执行/故障/容量证据与受影响回归 | 协调者汇总全产品发布条件；消费其他组固定证据，不复制评分权 |

业务组不永久占有代码目录。修复边界跟随因果链；不把同一事务或授权合同拆成多个互相等待的组任务。

## 当前执行模型

以下为 2026-09-05 源码复核，用于选择调查入口；本次文档重写没有重新运行服务或生产 Gate。

| 路径 | 当前实现与 owner | 需要避免的误读 |
|---|---|---|
| 接受和投递 | 业务命令事务写业务行与 PENDING outbox；`platform/queue` claim 后先 SENT 再 enqueue；dispatcher 满批续投 | asynq 信封不代表业务成功；测试中信封各一次不能推广为 broker 永远 exactly-once |
| Graph 执行 | `graph/lease.go` 用 token/expiry CAS、续租和写入检查约束 worker；`graph/durability.go` 负责节点生图 admission | 进程 mutex 只处理本进程重复入口；生图容量锁与执行 lease 是不同职责 |
| Agent 在线写入 | `agent/execution.go` 校验 lease、批量写 PG journal、fold 投影；Node `journal-publisher.ts` 合帧并等待结构屏障 | WAL、Pi session 和浏览器内存都不能替代 PG 业务权威 |
| Agent 重启 | `turn-runtime.ts:recoverDurableHandoff` 先确认 PG 前缀，再按条件 claim、提交可证明的未发布事件；Go `agent/recovery.go` 收敛丢失执行 | confirm 接口本身不 claim/续租；整个 handoff 流程可以 claim，不能概括为“Node 重启只读” |
| 后台恢复 | dispatcher 的 dispatch/recovery 独立循环；域内候选发现与单聚合状态迁移分开，恢复各域仍顺序调用 | 有界候选不等于所有内部查询常数成本；Graph 发现阶段无锁，单聚合变更再复核并锁行 |
| 实时通道 | `platform/notify/fanout.go` 按进程内 pool 共享 LISTEN；Agent/Graph 回读事件或状态，ImageSession SSE 调 `Service.Status` 发状态快照 | 共享 LISTEN 不消除每个订阅的回读成本；ImageSession 状态快照不使用 Agent 的逐事件补洞协议 |

Go 路径相对 `go/internal/`，dispatcher 入口为 `go/cmd/productflow-dispatcher/`，Node 路径相对 `agent-service/src/`。

### 必须守住的合同

- PostgreSQL 保存业务状态、Graph 产物/事件、Agent journal、effect ledger 和 outbox。Redis、NOTIFY、内存门禁只能承担 broker、唤醒或可丢失调度信息。
- 业务状态变化、对应事件和所需 outbox 由实际用例的事务边界保证；数据库提交成功以后才能向浏览器发布为事实。
- 不在持有业务行锁或 capacity advisory 的事务里调用 provider、处理大文件、长时间等待 broker、维持 SSE 或等待用户。
- worker 丢失执行权后不得继续落状态或产物。供应商调用已发生但结果无法证明时先对账；只有领域合同允许的 `not_applied` 才能按原幂等键重试，`conflict/unknown` 不自动重放。
- Agent journal 保持连续 seq 和原序重试；tool/question/approval/message/terminal 的结构屏障等待提交。ACK 只在回执身份与内容匹配后前进。确认失败不能跳过事件或产生第二终态。
- parked question/approval 不因重启自动失败或重新开放；答案绑定具体 question，迟到或不同答案覆盖需拒绝。丢失的模型 loop 不续跑，已完成副作用与未完成回复分别表达。
- 列表、详情、轮询和历史页各有独立合同。不得为了限制返回数量丢掉必要活动任务，也不得把完整历史重新塞回列表或轮询。
- 改超时、重试、状态、JSON、游标或 provider 字段之前查齐读写方；不能用缓存、额外进程锁、兼容层或降低验收门槛掩盖不一致。

### 锁与时间预算

| 资源链 | 现行顺序或默认值 | 核对入口 |
|---|---|---|
| Graph 执行状态 | `run -> node -> effect`；自动采用涉及 live graph 时 `run -> graph -> node` | `graph/lock_order_test.go`；事件 helper 要求调用方已持 run 锁 |
| 生图 admission | Graph `capacity advisory -> run -> node`；ImageSession `capacity advisory -> task`，拿 task 锁后重查状态 | `graph/durability.go`、`imagesession/execute.go` |
| Agent 执行与投影 | `projection -> execution -> task`；Draft 确认先持关联 projection，再写 Task | `agent/execution.go`、`agent/draft_confirm_lock_order_test.go` |
| worker / Graph lease | task timeout 30 分钟，consumer lease 为其加 5 分钟；Graph lease 复用该时长，每 5 分钟续租 | `platform/queue/asynq.go`、`actors.go`、`graph/lease.go` |
| dispatcher | dispatch 空闲间隔 1s，recovery 间隔 10s；默认 claim 100，满批立即再查 | `go/cmd/productflow-dispatcher/main.go`、`coordinator.go` |
| 连续生图闲置恢复 | 默认 90 分钟无 progress heartbeat 后重排队或 `unknown`；asynq `TaskTimeout` 30 分钟取消时 worker 直接写 `unknown` | `imagesession/recovery.go`、`execute.go`；`recovery_test.go`、`execute_test.go` |
| 并发 | asynq worker 4；Node Turn 默认 3/进程；全库生图槽默认 3，可设 1-20 | worker `main.go`、Node `config.ts`、`platform/generation` |
| journal batch | 20ms / 64 events / 768KiB 三个批次上限；结构事件有 flush barrier | `agent-service/src/journal-publisher.ts` |

这些是当前机制，不是用户等待时间的承诺。35 分钟 lease 不能直接解释为“重启很快恢复”；恢复时间要计入 lease 剩余时间、扫描 cadence、积压和副作用对账。是否调整须用故障现场和晚到 writer 回归共同证明。

`has_more` 不统一解释为“满批”或“精确 backlog”：queue 的 `Summary.HasMore` 使用满 claim 批次提示；Graph 候选用 `limit+1` 和剩余额度探测；Agent 合并各阶段结果。它们都不提供跨副本精确计数。锁竞争、发现后状态变化和不同阶段的候选范围分别看源码。

## 如何判读证据

| 证据层次 | 能支持什么 | 不能支持什么 |
|---|---|---|
| 当前源码与合同测试 | owner、字段、锁顺序、状态迁移和具体反例 | 实际负载延迟、部署可用性 |
| 隔离 PG/Redis 与进程故障测试 | 真实事务、重复投递、fencing、指定崩溃点的恢复 | 所有部署拓扑和生产恢复时间 |
| 固定规模 SQL/HTTP/浏览器测量 | 指定 fixture、路径、并发和版本下的成本与行为 | 没有实际执行的路径、真实用户分布或长期稳定性 |
| 部署观测与候选发布验证 | 被测候选、环境和窗口内的服务表现 | 之后的 checkout 或其他配置自动合格 |

每项结论同时保留实现状态、有效证据和未覆盖边界。已修复问题可以关闭，部署观测继续保留；不把“还没生产观测”写成原实现永远未完成，也不把旧 PASS 改写成当前 PASS。

一次有效测量至少记录：被测服务/测试代码版本、配置、数据总量与热数据分布、字段字节规模、并发、预热/样本数、计时起止、p50/p95、payload、错误和状态断言。脚本自己的 HEAD 不自动证明远端 API 版本；临时日志路径失效时不能只凭路径重复宣称已复验。

能量化的性能改动保留同输入的修改前后对照，同时记录绝对值、变化比例、未改善的成本与新增代价。样本不足不报分位数；字段体积、实际传输、查询次数、CPU/堆/RSS 分别取证，不能相互换算为未经测量的收益。

## 当前风险与证据

保留 PERF 编号作为历史交叉引用。以下按交付结果合并，不再把同一锁问题拆成多项进度百分比。

| 范围 / 旧编号 | 已实现或已交付 | 有效证据与边界 | 当前待回答的问题 |
|---|---|---|---|
| Graph/Agent 锁序，PERF-01、PERF-02、PERF-03 | 显式持 run 锁追加事件；自动采用先 run 后 graph；Agent 先 projection 后 execution/Task | [Graph 并发任务](tasks/archive/perf-graph-adopt-concurrent.md)；Agent `recovery_fencing_scanner_test.go`、`draft_confirm_lock_order_test.go` 有回归 | 实际积压与并发下的等待、饥饿和取消响应；已通过的窄测试不重复开修复单 |
| 执行权与恢复，PERF-05、PERF-11 | Graph 行 lease、迟到结果围栏；各域恢复拆成有界发现与单聚合事务。连续生图：未过 provider 边界则重排队；已打 provider 或 covering effect 则 `unknown` 且不可自动重试；无 parked question。asynq 取消 handler ctx 后仍落 unknown。心跳未过期不恢复；晚到 `finishSucceeded` 不能覆盖 unknown。 | [连续生图故障可见状态](tasks/archive/perf-imagesession-recovery-visible.md)；`graph/execute_test.go`、各域 `recovery_test.go`；[历史进程故障证据](../history/agent-runtime-timeline.md#platform-reliability-evidence) | 进程崩溃后页面保持 running 的上限为最后一次 heartbeat + `image_session_stale_running_after_minutes`（默认 90）+ recovery 10s 扫描与 25 条批次。asynq 30 分钟墙钟仍可能打断顺序多候选（非 `openai-images` 批量）的合法心跳任务并写成 unknown。长积压和坏条目是否拖慢其他域仍待测。 |
| 入队、投递和容量，PERF-04、PERF-06 | [入队 admission 修复](tasks/archive/perf-imagesession-enqueue-admission.md)、[满批续投](tasks/archive/perf-dispatcher-backlog.md) 已交付 | 500 条突发单/双副本 PENDING→SENT p95 0.438/0.279s；25 条延期未认领 | 重 recovery 与正常投递共存时的尾延迟；全库槽是现行单商家设计，不能用加 worker 代替容量推导 |
| 实时通道，PERF-07 | 共享 LISTEN、退订释放、通知丢失回 PG、Agent gap repair | `platform/notify/replica_field_test.go`；历史 `web-e2e-agent-sse` 5 passed | 多副本订阅回读、fallback、连接池和慢客户端总成本；LISTEN 数与浏览器 SSE 数分别计量 |
| Graph 工作台读取，PERF-09 | 摘要/详情、批量投影、轻量 status read 与 bundle gate | [Graph 专项原始记录](../history/agent-runtime-timeline.md#platform-graph-read-history)：25k runs/100k node-runs；HTTP、TTI、按需详情各有证据 | 历史浏览器 fixture 无 active run，Graph SSE=0；活跃执行与真实详情打开分布不能借用该结果 |
| 连续生图读取，PERF-08 | 详情任务三组各最多 20、去重最多 60；history keyset；[目标规模 HTTP gate](tasks/archive/perf-imagesession-http-load.md) 已交付 | 25k 会话/10k 轮次/1k 任务，单热会话、单客户端：详情 258,037B、p95 16.33ms；每路径 100 样本。只读元数据 | `serialize.go:loadStatus` 仍全量读取 queued/running 并组装 effects，SSE 复用它；详情 gate 未测这条活动集放大路径。COUNT/关联扫描和真实并发仍待评估 |
| Agent Session 读取，PERF-12 | cursor page、批量会话摘要/count、`activity_at` 排序；每会话最多 20 conversations | `agent/query_plan_test.go` 的 25k 会话列表计划；[历史记录](../history/agent-runtime-timeline.md#platform-reliability-evidence) | 目标规模 payload 与单会话详情成本；需先固定 conversation 摘要宽度和真实访问形状，不能只增加总行数 |
| journal/ACK 与可观测性，PERF-13 | 批量 PG append、WAL/ACK、恢复前缀确认；[journal 调查](tasks/archive/arch-journal-assessment.md)结论为保留现状 | 标准容量历史 PG batch p95 232.8ms，本地 WAL p95 0.81ms；[问题答案身份修复](tasks/archive/agent-question-answer-identity.md)含第二问 SIGKILL | 当前 batch 指标是 count 和 `last_ms` gauge，不能计算持续 p95；histogram、锁等待和告警是否需要补，跟随具体诊断/部署需求 |
| 媒体内存 | 共享 ZIP writer 流式逐文件写入，事务只冻结条目身份 | `just go-test-zip-rss` 历史 100×10MiB 写入器额外 MaxRSS 约 12.6MiB | 图库 HTTP 总字节上限不同，该测试未经过整条 HTTP 下载路径；参考图解码与真实传输另看工作量 |
| SaaS，PERF-10 | 现行合同明确为单商家 | [ROADMAP](../ROADMAP.md#saas) | 产品基线扩展时统一设计身份、范围、配额、存储与审计；当前不发布局部补丁 |

所有权调查的结论可以是保持现状。AR-02 未证明 journal 数据丢失，拆 confirm 循环也未消除 TurnRuntime 必知分支；不能因为还有协议边界测试缺口就自动重启运行时重构。

2026-09-05 连续生图状态读取窄修：`serialize.go:loadStatus` 的最新轮次查询仅取 `id/generation_group_id`，状态与详情共用的 effect 摘要查询不再加载 `request_json/result_json`。`status_projection_test.go` 在真实 PG 查询结果上检查字段未加载，并验证 26 个 queued/running 任务不截断、effect 响应字段不变；fixture 中排除的原始 JSON 共 3,539,646 字节。原始数据仍存于 PG，执行/对账读取未改。ImageSession 包测、race 与原目标规模 HTTP gate 通过；该 gate 的历史页面形状不构成活动 SSE 负载验收，也不据此前后两次跑分宣称延迟收益。活动集数量、必要响应字节、轮次计数和每订阅回读仍需独立评估。

连续生图 SSE 重复快照已过滤，量化见 [2026-09-05 对照记录](../history/agent-runtime-timeline.md#2026-09-05-连续生图-sse-重复快照对照)：4 订阅、26 个 queued 任务、15.25s 静默窗口，状态帧 32→4，SSE 正文 2,875,352→359,468B（约 -87.50%），PG 状态回读仍为 32 次。心跳、无通知进度更新、终态与重连均保留；每连接额外保留一份序列化快照。该优化不消除活动集查询/编码放大，也未证明生产容量。

活动标志与任务列表的一致性已修复：移除独立 COUNT，避免并发入队时 `has_active=false/tasks=1` 导致 SSE 提前关闭，以及最后一个任务结束时 `has_active=true/tasks=0`。真实 PG 双向交错回归及 20 次 race 重复通过；同 SSE fixture 中生图任务表 COUNT 从 96→64，状态回读和传输不变。详见 [一致性与查询对照](../history/agent-runtime-timeline.md#2026-09-05-连续生图活动标志一致性与查询对照)。不引入锁或额外快照，不将该局部保证推广为整个 Status 的全局一致读。

空任务投影不再读取无用的队列概览。其他会话有 25,010 个 queued 任务时，空详情/Status 各 100 次请求的队列查询均从 500→0，p95 分别从 10.63→6.29ms、9.47→4.54ms；返回终态任务的详情仍保留队列字段。输入、字节和取样口径见 [空任务读取对照](../history/agent-runtime-timeline.md#2026-09-05-连续生图空任务读取与全局积压隔离)。该结果不覆盖仍需返回任务的活动会话成本。

展示用队列总览的 4 条独立 COUNT 已合为单条聚合 SQL，修复任务/图节点 running↔queued 切换时的双计与漏计。4 个交错及 5 个分类场景各重复 20 次 race 通过；同活动 SSE fixture 的总览查询（含配置）160→64，每次回读 5→2。该口径不同于前述“任务表 COUNT”；容量配置与 admission 仍独立。详见 [队列快照对照](../history/agent-runtime-timeline.md#2026-09-05-展示队列单语句快照与查询对照)，大规模活跃 Graph 的执行计划和延迟仍未签收。

2026-09-05 连续生图故障可见状态：[perf-imagesession-recovery-visible](tasks/archive/perf-imagesession-recovery-visible.md) 钉死四类结果。未过 provider 边界则重排队；已 applied 不重放；不可证明结果为 `unknown` 且不可自动重试；无 parked question。asynq 取消 handler ctx 后 worker 仍写 `unknown`。心跳未过期不恢复；晚到 `finishSucceeded` 不能覆盖 `unknown`。进程崩溃后 running 上限为最后一次 heartbeat + 默认 90 分钟 + recovery 10s / 25 条批次。asynq 30 分钟墙钟仍可能打断顺序多候选任务。未改全局 `TaskTimeout`。

## 下一步如何选择

后续方向按业务风险安排，不按表格里哪个空格最容易补齐来选任务。以下是调查顺序和发布条件；具体认领只维护在看板。

1. **明确故障后的用户可见结果。** 连续生图执行链已由 [perf-imagesession-recovery-visible](tasks/archive/perf-imagesession-recovery-visible.md) 交付。另选 Graph 或 Agent 链时仍定义故障点、等待上限和四种边界；与工作流体验组运行/重试入口重叠时先交接。
2. **验证活动负载，不只验证历史页面。** 连续生图优先调查 queued/running 增长时 Status/SSE 的任务、effects、字节与回读次数；dispatch 优先调查恢复积压和正常提交共存时的投递尾延迟。当前只确认了潜在放大路径，未确认生产故障，不预设分页或缓存方案。
3. **补足可解释的测量。** 确定慢在锁等待、查询、JSON、队列还是 provider；只有现有指标无法回答已选问题时才补 histogram/trace。`last_ms`、瞬时 waiter 数和局部 query duration 不能互相替代。
4. **准备候选发布证据。** 候选 checkout、测试输入与运行资源固定后执行受影响门和全量门；Agent 行为消费质量组可采信版本。没有冻结候选时不反复跑 G-07 追逐移动的 HEAD。

每张新任务说明：商家影响、当前可观察现象或待验证假设、独立结果、必要源码入口、冻结输入和隔离资源、成功/失败判据。测量可以得出“不需要改运行时”，但不能只有一张没有决策用途的跑分表。具体认领、审核与归档复用 [看板协议](tasks/README.md)，不另建组内流程。

## 验收与预算

验证强度取决于改动：局部字段/查询测试、真实 PG 事务与并发、跨进程故障、浏览器和真实 provider 分别证明不同边界。涉及对应合同或准备候选发布时扩大范围；默认测试跳过 opt-in 不等于专项通过。

| 改动或问题 | 必要验证入口 | 判定重点 |
|---|---|---|
| Graph 锁、lease、取消与自动采用 | `go test -C go ./internal/graph`；锁序和 lease 测试按场景重复/race | 无死锁、迟到 writer 拒绝、用户文稿与产物状态一致 |
| 投递、恢复、admission | queue/dispatcher/受影响领域包；已有 `dispatch_latency_test.go`；`just go-test-staging-field` 按隔离资源执行 | PG 状态与信封对账、延期不提前、恢复不重复副作用；进程测试不替代容器拓扑 |
| Graph 读取 | `just go-test-graph-query-plan`、`just http-ab-gates`、`just web-e2e-workbench-performance`、`just web-build` | 实际 SQL、响应字段/字节、TTI、按需请求、active-run fixture |
| ImageSession 读取 | `just go-test-imagesession-query-plan`、`just go-test-imagesession-http-load`、包内 HTTP/SSE 回归 | 详情、历史、Status 各自的数据形状；静态页面 gate 不覆盖活动 SSE 成本 |
| ImageSession SSE 重复快照 | `just go-test-imagesession-sse-load` | 固定订阅/字段宽度下的帧数、正文、PG 回读、心跳、无通知变化、终态及重连；不等同完整容量门 |
| Agent 读取 | `just go-test-agent-query-plan`、`agent/capacity_test.go` | conversation 宽度、事件深度、分页、payload 和读取时延分别记录 |
| Agent batch/WAL/恢复 | `just go-test-agent-journal-capacity`、`just agent-service-test-local-journal-capacity`；`event_confirm_test.go`、`sigkill_gopg_test.go`、Node journal/turn 测试 | 连续 seq、ACK、fencing、结构/终态屏障、恢复和诚实终态 |
| Agent SSE/页面恢复 | `just web-e2e-agent-sse` 与对应 conversation runtime 测试 | gap 保持 live 流、重复/迟到帧、终态补洞、parked approval、共享订阅 |
| 媒体内存 | `just go-test-zip-rss` 与受影响上传/导出测试 | 峰值内存、总字节/像素限制、事务外 IO；明确 writer 与完整 HTTP 的范围 |

Go 包测需要 PG 时通过 `bash scripts/with_dev_env.sh bash -lc 'go test -C go ...'` 运行。使用测试库/隔离库，不重建 dev DB；故障或 provider 测试必须确认共享服务、浏览器和配置占用。

预算分成现有回归门和待校准目标，不能在事后按测量结果放宽：

| 类型 | 当前口径 | 使用限制 |
|---|---|---|
| Graph 本地 HTTP 回归门 | summary p95 <=30ms / <=24KiB；detail p95 <=100ms / <=512KiB | 固定字段和 fixture；修改预算需要独立原因和对照 |
| ImageSession HTTP 回归门 | 列表/history p95 <300ms；所有固定 fixture 响应 <1MiB；详情延迟记录不判阈值 | 来自已归档 HTTP 任务，不能推广成任意 prompt/effect 大小或并发 SLO |
| Agent 标准容量门 | PG batch p95 <=300ms；SSE <=1s；gap repair <=5s；最近 50 Turn <=500ms | G-05 维度和测试形状见下节；需分别采证 |
| 初始运维目标，未签生产 SLO | 普通列表 p95 <300ms / <1MiB；无 provider 命令事务 p95 <500ms、锁等待 p95 <100ms；PENDING→SENT p95 <1s；recovery 单阶段事务 p95 <1s | 定义负载、起止点与窗口后使用；不把 HTTP、SQL、恢复整轮与单事务混为同一指标 |

## 生产 Gate

<a id="production-gates"></a>

G-01 至 G-07 保留为发布合同，状态绑定候选而非永久关闭。S1-S6 实现切片已交付并收入 [原生产计划及历史验收](../history/agent-runtime-timeline.md#platform-production-gate-history)，不重新开工。历史 PASS 保留；本次只改文档，未产生新候选的全量 PASS。

| Gate | 必须证明 | 现有证据资格 |
|---|---|---|
| G-01 合同回归 | manifest/schema/DTO 对齐、checkpoint/invocation 幂等、usage/reason code、事件分页与 unknown 语义 | 有历史无缓存全量记录；新候选按影响面重跑，不能引用旧版本自动通过 |
| G-02 PG 正确性 | manifest 中可恢复工具的 effect 状态矩阵、幂等键、并发 scanner、旧 fencing writer 拒绝 | 历史八工具矩阵保留；工具集合变化时按当前 manifest 核验覆盖 |
| G-03 崩溃边界 | 模型开始后、mutation 成功/result 前、approval 后、turn/end 落库/响应前 SIGKILL；seq 连续、副作用至多一次、诚实终态、无永久活动 Turn | Go+PG helper 四点历史已过；Node/真实 provider 各自证据不能混称全覆盖 |
| G-04 浏览器恢复 | 普通断线、gap、重复、旧 generation、overflow、terminal gap、approval 刷新；无错误断线提示 | 2026-09-05 `ebc7630d` 工作树 `web-e2e-agent-sse` 5 passed；当前候选按变更复验 |
| G-05 标准容量 | 25 并发 Turn、100 SSE、单 Turn 10k 事件、单会话 1000 Turn，及上表时延与正确性断言 | `capacity_test.go` 独立测 1×10k 深度、25×128 并发写和 SSE/历史，并非 25×10k 同时运行；历史 PG batch p95 232.8ms 不等于持续生产 p95 |
| G-06 真实业务 | 冻结真实模型/配置的完整 Skill 评测、审批到单一 WorkflowRun、真实图运行、Chromium；确认有效 background 能力为 false | 真实审批/出图已有历史 PASS，行为门仍由 [Agent 质量组](agent-eval-system.md) 的固定 `run_id` 裁定；旧 `gpt-5.6-luna` 15/15 冒烟不满足 L1-L6 出口 |
| G-07 候选全量 | 干净固定 checkout，无缓存 Go、Agent Service、Web test/lint/build、docs-check、migration fresh/upgrade、diff check | `fb658633` 于 2026-09-04 通过；证据只属于该基线，当前候选未在本轮重跑 |

当前有效能力保持 D-03：provider profile 与 adapter 能力取交集，Pi adapter 的 `background_resumable` 为 false；不新增绕过 Pi 的模型执行器。字段存在不表示可以后台续跑。PG 权威、7 天 chunk 压缩、不可恢复模型中断与 effect 对账的 D/C 合同在历史来源和现行代码中保留，不因文档重排放宽。

组内局部交付无需等待其他组所有目标完成；宣告产品可发布时，协调者必须消费同一候选及可采信固定输入的完整证据。模型能力未过、未执行、无效运行和可复现的 FAIL 分别记录，不能通过关闭采证 issue 改写结论。

## 维护约定

- 新事实回写其唯一 owner：稳定行为写 ARCHITECTURE/PRD/USER_GUIDE，未来方向写 ROADMAP，本页写风险与验收判断，详细复现和采证留 issue。
- 本页不重复维护开放任务数量或认领状态；任务关闭时只更新受影响结论与证据链接。无开放 issue 不代表无风险，有观测缺口也不自动产生实现单。
- 旧设计允许纠正。记录当前因果依据、替代判断与未验证项；不把历史方案、评测阈值或架构拆分天然视为正确，也不通过改写目标掩盖失败。
- 2026-09-05 重写依据：当前 queue/dispatcher、Graph lease/recovery、Agent execution/turn-runtime/batch、ImageSession serializer/SSE、metrics 和容量测试。修正了统一 `has_more`、详情与状态成本混用、capacity 维度相乘和 G-07 永久完成等误读；这是源码与证据整理，未改运行时，未做新一轮生产验收。
