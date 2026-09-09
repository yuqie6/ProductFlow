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

当前部署从单管理员 bootstrap 开始；公开注册可创建普通 User 与其自有 Merchant。正式版身份合同是：普通账号只拥有一个 Merchant；商品、素材、Graph、Agent/Delivery 等后台任务均按 `merchant_id` 归属。目标合同允许平台管理员在显式 `merchant_id` 上管理所有商家的已有商品，管理不改变归属；不存在跨账号的全局商品实体，也不以工作区或商家切换、Owner/Editor/Viewer 团队或支持会话实现此能力。完整多商家 SaaS 仍是目标。身份、隔离与商业额度由 [商家平台](merchant-platform.md) 负责；本组增加发行、安装、备份恢复、稳定版升级、调用消费事实及多商家资源限制职责。它们是正式版必要增量，不能继续以 live demo 边界无限后置；实现仍须按 [总纲](../ROADMAP.md) 的固定合同拆分，不零散添加抽象 tenant 字段。

组内 [发行基线调查](tasks/archive/release-readiness-baseline.md) 已在源码只读基线 `d6709c4a` 上给出运行单元、持久面、差距分类与演练合同（见 [自托管发行与恢复基线](#self-host-release-baseline)）。备份必须含数据库、媒体、必要 Pi 数据及可解密/可启动配置；首个稳定版起提供明确升级路径，不恢复 retired V1/v2。真实安装/恢复/升级路径与演练证据已有 B1–B5 及 B6 汇总；干净候选重跑见 [release-r6-clean-candidate-gate](tasks/archive/release-r6-clean-candidate-gate.md)（G-07 在 `e8cb494d`+最窄修补上 PASS，修补已入库于 `67b0f309`）。[release-r6-pin-and-formal-d4](tasks/archive/release-r6-pin-and-formal-d4.md) 已产出 pin `0.0.0-67b0f3092158`（≡G-07 交付 SHA）并对 `0.0.0-5ed2b916b569`→该 pin 跑通正式 D4（非等价 retag）。[release-r6-resource-budget](tasks/archive/release-r6-resource-budget.md) 已在隔离项目对该 pin 采证空闲/轻负载容器足迹与部署规模对应表（观测依据建议 ≥2 vCPU / ≥4 GiB；**非 SLA**）。维护者裁定见 [release-r6-close-ruling](tasks/archive/release-r6-close-ruling.md)：**总纲 R6 通过**（残余满载/多商/staging 等为非宣称，≠SLA/≠R1–R5）。商业定价、余额与运营授权裁定归商家平台，实际调用事实、重复执行、unknown、原子预留执行正确性与公平资源调度由本组承担必要实现，一条完整交易链只建一套账本。

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
| 接受和投递 | 业务命令同事务写业务行与 River job；原生 Worker 领取 | River job 完成不代表业务成功；业务执行身份与 attempt 围栏负责副作用 |
| Graph 执行 | `graph/lease.go` 用 token/expiry CAS、续租和写入检查约束 worker；`graph/durability.go` 负责节点生图 admission | 进程 mutex 只处理本进程重复入口；生图容量锁与执行 lease 是不同职责 |
| Agent 在线写入 | `agent/execution.go` 校验 lease、批量写 PG journal、fold 投影；Node `journal-publisher.ts` 合帧并等待结构屏障 | WAL、Pi session 和浏览器内存都不能替代 PG 业务权威 |
| Agent 重启 | `turn-runtime.ts:recoverDurableHandoff` 先确认 PG 前缀，再按条件 claim、提交可证明的未发布事件；Go `agent/recovery.go` 收敛丢失执行 | confirm 接口本身不 claim/续租；整个 handoff 流程可以 claim，不能概括为“Node 重启只读” |
| 后台恢复 | watch 中投递及五个恢复域分别定时，域内串行且有界；one-shot 仍按固定域顺序恢复后投递 | 域间不再等待其他域批次完成，但仍共享 PG 连接与 IO；有界候选不等于所有内部查询常数成本，Graph 单聚合变更仍复核并锁行 |
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
| worker / Graph lease | River timeout 30 分钟、rescue 阈值 35 分钟；独立 Graph lease 35 分钟，每 5 分钟续租 | `platform/queue/river_client.go`、`graph/lease.go` |
| dispatcher | dispatch 空闲间隔 1s，recovery 间隔 10s；默认 claim 100，满批立即再查 | `go/cmd/productflow-dispatcher/main.go`、`coordinator.go` |
| 连续生图闲置恢复 | 默认 90 分钟无 progress heartbeat 后重排队或 `unknown`；River 30 分钟取消时 worker 尝试持久化 `unknown` | `imagesession/recovery.go`、`execute.go`；`recovery_test.go`、`execute_test.go` |
| 并发 | River 单进程 generation/delivery/local_edit/agent 为 3/2/2/2；Node Turn 默认 3/进程；全库生图槽默认 3，可设 1-20 | worker `main.go`、Node `config.ts`、`platform/generation` |
| journal batch | 20ms / 64 events / 768KiB 三个批次上限；结构事件有 flush barrier | `agent-service/src/journal-publisher.ts` |

这些是当前机制，不是用户等待时间的承诺。35 分钟 lease 不能直接解释为“重启很快恢复”；恢复时间要计入 lease 剩余时间、扫描 cadence、积压和副作用对账。是否调整须用故障现场和晚到 writer 回归共同证明。

`has_more` 不统一解释为“满批”或“精确 backlog”：queue 的 `Summary.HasMore` 使用满 claim 批次提示；Graph 候选用 `limit+1` 和剩余额度探测；ImageSession、Delivery、LocalEdit 候选发现用 `FOR UPDATE SKIP LOCKED`，跳过锁定行后按 `limit+1` 判断；Agent 合并各阶段结果。它们都不提供跨副本精确计数；锁定候选仍可留在库中而 `HasMore=false`。锁竞争、发现后状态变化和不同阶段的候选范围分别看源码。

## 如何判读证据

2026-09-06 [后端集成复验与现行路由合同](../history/agent-runtime-timeline.md#2026-09-06-后端集成复验与现行路由合同)：无缓存全 Go 26 包通过、3 包失败、9 包无测试；运行期间其他组代码变化，不作为固定候选全量证据。已修复历史路由差异检查并通过完整 API 包 race 回归；Agent 夹具漂移和图片评测在途编译结果保留原始边界。

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
| 执行权与恢复，PERF-05、PERF-11 | Graph 行 lease、迟到结果围栏；各域有界发现与单聚合事务。连续生图每信封至多一次 provider 调用，确认批次后续投；已 applied 不重放，不可证明结果 unknown 且不可自动重试，无 parked question。心跳未过期不恢复，晚到终态不能覆盖 unknown。 | [故障可见状态](tasks/archive/perf-imagesession-recovery-visible.md)；[批次续投回归](../history/agent-runtime-timeline.md#2026-09-06-连续生图按已确认批次续投)；`graph/execute_test.go`、各域 `recovery_test.go`；[历史进程证据](../history/agent-runtime-timeline.md#platform-reliability-evidence) | 连续生图顺序候选不再累计占用同一 30 分钟 handler，但单 provider 调用与写库仍受该上限约束。进程崩溃后的闲置恢复仍为最后 heartbeat + 默认 90 分钟 + 10s 扫描及批次等待。长积压、坏条目和 checkpoint 后信封释放失败的恢复延迟仍需对应证据。 |
| 入队、投递和容量，PERF-04、PERF-06 | [入队 admission 修复](tasks/archive/perf-imagesession-enqueue-admission.md)、[满批续投](tasks/archive/perf-dispatcher-backlog.md) 已交付 | 500 条突发单/双副本 PENDING→SENT 历史 p95 0.438/0.279s；[慢恢复共存](../history/agent-runtime-timeline.md#2026-09-05-慢恢复与正常投递共存)：1000 条过期连续生图任务、恢复写入人为延迟 100ms，最终投递 p95 0.442/0.265s，25 条延期未认领；采证起点曾出现一次未复现的时间矛盾，已保留失败并加强提交证据屏障 | 该固定场景支持保留双循环；数据库连接/IO 饱和、不同域长积压及坏条目对后续恢复域的影响仍待测。全库生图槽是服务级并发约束，任务仍按 merchant_id 归属；不要把共享容量槽误读为跨商家数据权限，也不能用加 worker 代替容量推导 |
| 实时通道，PERF-07 | 共享 LISTEN、退订释放、通知丢失回 PG、Agent gap repair | `platform/notify/replica_field_test.go`；历史 `web-e2e-agent-sse` 5 passed | 多副本订阅回读、fallback、连接池和慢客户端总成本；LISTEN 数与浏览器 SSE 数分别计量 |
| Graph 工作台读取，PERF-09 | 摘要/详情、批量投影、轻量 status read 与 bundle gate | [Graph 专项原始记录](../history/agent-runtime-timeline.md#platform-graph-read-history)：25k runs/100k node-runs；HTTP、TTI、按需详情各有证据 | 历史浏览器 fixture 无 active run，Graph SSE=0；活跃执行与真实详情打开分布不能借用该结果 |
| 连续生图读取，PERF-08 | 详情任务三组各最多 20、去重最多 60；history keyset；[目标规模 HTTP gate](tasks/archive/perf-imagesession-http-load.md) 已交付。Status/SSE 返回该会话全部 queued/running，省略 `prompt`，固定宽度 300 条活动任务仍 `<1MiB`。 | 25k 会话 HTTP：详情 258,037B、p95 16.33ms。活动集：[perf-imagesession-active-status](tasks/archive/perf-imagesession-active-status.md) 26/100/300 条 queued+running+effect，查询次数恒为 8；修改前 300 条 Status ≥1MiB，省略 prompt 后包内 HTTP 与隔离规模门通过 | COUNT/关联扫描、真实 prompt 宽度超过夹具、多订阅并发和生产分布仍待评估 |
| Agent Session 读取，PERF-12 | cursor page、批量会话摘要/count、`activity_at` 排序；每会话最多返回 20 conversation 摘要。Turn 列表改为批量关联读取 | [目标规模 HTTP](../history/agent-runtime-timeline.md#2026-09-05-agent-读取容量与-turn-批量投影)：25k sessions、27k conversations、单对话 1000 Turns，四条真实读取路径通过固定宽度门；50 条 Turn 页查询 54→5，p95 28.70→8.93ms，正文不变 | 无独立 Session GET 详情接口，详情成本按实际 Turn GET 测量。并发、更长正文、journal/SSE 与真实访问分布不由该单客户端门推定 |
| journal/ACK 与可观测性，PERF-13 | 批量 PG append、WAL/ACK、恢复前缀确认；[journal 调查](tasks/archive/arch-journal-assessment.md)结论为保留现状 | 标准容量历史 PG batch p95 232.8ms，本地 WAL p95 0.81ms；[问题答案身份修复](tasks/archive/agent-question-answer-identity.md)含第二问 SIGKILL | 当前 batch 指标是 count 和 `last_ms` gauge，不能计算持续 p95；histogram、锁等待和告警是否需要补，跟随具体诊断/部署需求 |
| 媒体内存 | 共享 ZIP writer 逐文件写临时包，事务只冻结条目身份；商品图库 HTTP 在打包完成后发送文件并清理 | writer 历史门保留；[真实 HTTP 门](../history/agent-runtime-timeline.md#2026-09-05-商品图库-zip-http-内存与传输)：100 张有效 PNG 共 469.19MiB，ZIP 469.35MiB，额外进程 RSS 上界 37.37MiB；鉴权、哈希、传输中断清理与限制拒绝通过；[首文件核验点取消](../history/agent-runtime-timeline.md#2026-09-05-商品图库打包期间-http-取消)已测 | 单客户端、同内容硬链接、暖文件缓存；不覆盖并发导出、临时存储饱和、压缩/磁盘写入中断、完整像素解码、JPEG/WebP 或交付导出 HTTP |
| SaaS，PERF-10 | 当前 bootstrap/验证环境常用单商家；目标产品合同为普通账号各自一个 Merchant，允许平台管理员按显式商家上下文管理所有商家的已有商品。商品与任务按 `merchant_id` 归属 | [ROADMAP](../ROADMAP.md)、[商家平台](merchant-platform.md) | 已进入正式版主线；本组负责调用事实、容量与发行恢复，按完整隔离合同协作 |

所有权调查的结论可以是保持现状。AR-02 未证明 journal 数据丢失，拆 confirm 循环也未消除 TurnRuntime 必知分支；不能因为还有协议边界测试缺口就自动重启运行时重构。

2026-09-05 连续生图状态读取窄修：`serialize.go:loadStatus` 的最新轮次查询仅取 `id/generation_group_id`，状态与详情共用的 effect 摘要查询不再加载 `request_json/result_json`。`status_projection_test.go` 在真实 PG 查询结果上检查字段未加载，并验证 26 个 queued/running 任务不截断、effect 响应字段不变；fixture 中排除的原始 JSON 共 3,539,646 字节。原始数据仍存于 PG，执行/对账读取未改。ImageSession 包测、race 与原目标规模 HTTP gate 通过；该 gate 的历史页面形状不构成活动 SSE 负载验收，也不据此前后两次跑分宣称延迟收益。活动集数量与必要响应字节见 [perf-imagesession-active-status](tasks/archive/perf-imagesession-active-status.md)；轮次计数和每订阅回读仍随活动会话存在。

连续生图 SSE 重复快照已过滤，量化见 [2026-09-05 对照记录](../history/agent-runtime-timeline.md#2026-09-05-连续生图-sse-重复快照对照)：4 订阅、26 个 queued 任务、15.25s 静默窗口，状态帧 32→4，SSE 正文 2,875,352→359,468B（约 -87.50%），PG 状态回读仍为 32 次。心跳、无通知进度更新、终态与重连均保留；每连接额外保留一份序列化快照。该优化不消除活动集查询/编码放大，也未证明生产容量。

活动标志与任务列表的一致性已修复：移除独立 COUNT，避免并发入队时 `has_active=false/tasks=1` 导致 SSE 提前关闭，以及最后一个任务结束时 `has_active=true/tasks=0`。真实 PG 双向交错回归及 20 次 race 重复通过；同 SSE fixture 中生图任务表 COUNT 从 96→64，状态回读和传输不变。详见 [一致性与查询对照](../history/agent-runtime-timeline.md#2026-09-05-连续生图活动标志一致性与查询对照)。不引入锁或额外快照，不将该局部保证推广为整个 Status 的全局一致读。

空任务投影不再读取无用的队列概览。其他会话有 25,010 个 queued 任务时，空详情/Status 各 100 次请求的队列查询均从 500→0，p95 分别从 10.63→6.29ms、9.47→4.54ms；返回终态任务的详情仍保留队列字段。输入、字节和取样口径见 [空任务读取对照](../history/agent-runtime-timeline.md#2026-09-05-连续生图空任务读取与全局积压隔离)。该结果不覆盖仍需返回任务的活动会话成本。

展示用队列总览的 4 条独立 COUNT 已合为单条聚合 SQL，修复任务/图节点 running↔queued 切换时的双计与漏计。4 个交错及 5 个分类场景各重复 20 次 race 通过；同活动 SSE fixture 的总览查询（含配置）160→64，每次回读 5→2。该口径不同于前述“任务表 COUNT”；容量配置与 admission 仍独立。详见 [队列快照对照](../history/agent-runtime-timeline.md#2026-09-05-展示队列单语句快照与查询对照)。后续活跃 Graph 测量发现关联 EXISTS 重复探测；按 run 聚合活动节点后，25k 活动 runs / 100k nodes 的总览 p95 从 474.15→40.23ms，单语句快照与 running 优先语义保留。[活跃规模记录](../history/agent-runtime-timeline.md#2026-09-05-队列总览活跃-graph-聚合) 另覆盖 100/0 活动 run；此为隔离 PG 本地读取证据，未覆盖多订阅 HTTP/SSE 容量。

2026-09-05 连续生图故障可见状态：[perf-imagesession-recovery-visible](tasks/archive/perf-imagesession-recovery-visible.md) 钉死四类结果。未过 provider 边界则重排队；已 applied 不重放；不可证明结果为 `unknown` 且不可自动重试；无 parked question。asynq 取消 handler ctx 后 worker 仍写 `unknown`。心跳未过期不恢复；晚到 `finishSucceeded` 不能覆盖 `unknown`。进程崩溃后 running 上限为最后一次 heartbeat + 默认 90 分钟 + recovery 10s / 25 条批次。2026-09-06 批次续投已避免多个 provider 调用累计占用同一 handler；全局 `TaskTimeout` 未改，单次调用仍可能超时。[部分完成 deadline 回归](../history/agent-runtime-timeline.md#2026-09-06-连续生图部分完成时限边界) 确认第二候选超时后首张素材保留、副作用 applied/unknown、重入不打 provider；[检查点原子续投](../history/agent-runtime-timeline.md#2026-09-06-连续生图检查点原子续投) 覆盖已提交检查点后的退出窗口，不替代检查点前的恢复证据。

2026-09-06 [失败状态持久化错误传播](../history/agent-runtime-timeline.md#2026-09-06-连续生图失败落库不得确认消费)：`finishFailed` 的事务错误返回 consumer，避免重排队/失败终态写入失败后仍确认信封。真实 PG/Consume 的两个故障场景各三次 race 通过，错误信封 PENDING、无 lease、无 consumed_at，已确认 provider 失败保留。任务仍 running，未缩短闲置恢复，也未证明数据库持续不可用后的完整恢复时间。

2026-09-05 连续生图活动 Status：[perf-imagesession-active-status](tasks/archive/perf-imagesession-active-status.md) 在真实 HTTP 上测量 26/100/300 条 queued/running（各一条 effect、2160B prompt、512B 进度注记）。查询次数恒为 8，不随活动集线性增加。修改前 300 条 Status 超过 `<1MiB`；根因是每 2s 回读重复下发 prompt。Status/SSE 改为不下发 prompt，详情与提交响应仍带完整提示词；前端 overlay 保留缓存 prompt，新任务 id 触发详情回源。未分页丢掉活动任务，未加缓存。

## 下一步如何选择

2026-09-05 Agent 迟到 sync 信封启动检查，见 [执行边界回归](../history/agent-runtime-timeline.md#2026-09-05-agent-迟到-sync-信封启动检查)：仅补投递筛选无法处理已经入队的信封。原 worker/直接绑定对已终态或停等的未绑定 Turn 仍调用 StartTurn；两处复用 `turnNeedsSync` 后，14 个状态/入口组合及首次读取后取消场景均不发启动请求，迟到取消信封正常 CONSUMED。末次读取到远端请求仍非原子，PG claim 的终态拒绝保留；mock 调用减少不能折算为真实模型费用或副作用。

2026-09-05 Agent pending Turn 补投递与 worker 同步条件对齐，见 [资格矩阵](../history/agent-runtime-timeline.md#2026-09-05-agent-补投递与-worker-同步资格对齐)：72 种状态组合原来补 14 个信封，实际仅 8 个需要 worker 同步；SQL 发现排除已绑定活动 Turn，逐条复核复用 `turnNeedsSync`，消除该夹具的 6 个无效信封。已回答问题、未绑定启动、resume_required 和终态边界保留。绑定后复核已覆盖，读取后到 outbox 写入的竞态与 outbox 行锁仍不由本轮保证。

2026-09-05 Agent queued Task 补首轮：[conversation 锁跳过](../history/agent-runtime-timeline.md#2026-09-05-agent-queued-task-恢复跳过-conversation-锁)。原实现前 25 条共用被锁 conversation 时，首轮在等待中取消，第 26 条未恢复且错误被吞；发现和逐条处理均跳锁后，发现前持锁时第 26 条首轮恢复，发现后持锁时第二轮恢复。解锁后前缀各补唯一首轮，取消返回 context 错误。Task/Session 行锁、持续错误前缀与 pending Turn 补投递仍待验证。

2026-09-05 Agent 过期 execution 的锁定前缀已有直接回归，见 [扫描证据](../history/agent-runtime-timeline.md#2026-09-05-agent-过期-execution-锁定前缀)：25 条 projection 持锁时，第 26 条仍在第一轮收敛；此路径已在候选查询跳锁，无需套用其他域的修复。`HasMore` 仍探测全部过期 owner，持锁前缀存在时为 true。queued Task 补首轮、pending Turn 补投递、execution 单独持锁与持续错误候选未由此关闭。

2026-09-05 [交付与局部编辑恢复锁定前缀](../history/agent-runtime-timeline.md#2026-09-05-交付与局部编辑恢复跳过锁定前缀)：两个域分别复现 25 条持锁候选使第 26 条连续 3 轮无法恢复。各自候选发现前移跳锁后，后续任务第一轮恢复且不重复，解锁后前缀正常补回。保留 Delivery 可重排队和 LocalEdit 已过 provider 边界为 unknown 的差异。此项不覆盖 Graph/Agent 的不同候选结构，也未解决持续错误前缀。

2026-09-05 [连续生图恢复锁定前缀](../history/agent-runtime-timeline.md#2026-09-05-连续生图恢复跳过锁定前缀)：最早 25 个候选持锁时，原实现连续 3 轮均未恢复第 26 条正常任务。候选发现阶段前移 `SKIP LOCKED` 后，第一轮即为后续任务补回唯一 outbox；持锁任务不变，解锁后全部恢复。发现查询默认返回至多 26 个候选 ID 并短暂锁行，事务结束即释放；状态迁移仍逐条重新锁行和复核。此结论只覆盖 ImageSession 行锁前缀，持续错误条目和其他域候选公平性未据此关闭。

2026-09-05 [恢复域独立调度](../history/agent-runtime-timeline.md#2026-09-05-watch-恢复域独立调度)：原实现首域阻塞时，后续域无法启动或按 cadence 再次运行。watch 改为五个独立串行恢复循环，域内不重入、取消等待全部退出；one-shot 顺序与既有锁/状态合同保留。20 次 race 重复验证慢域、健康域和失败域并存。真实四场景投递门通过，慢恢复单/双副本 p95 385.253/244.671ms。最多并发恢复批次由 1 增至 5；未新增连接池、缓存或超时，资源饱和与域内坏条目仍待验证。

后续方向按业务风险安排，不按表格里哪个空格最容易补齐来选任务。以下是调查顺序和发布条件；具体认领只维护在看板。

1. **明确故障后的用户可见结果。** 连续生图执行链已由 [perf-imagesession-recovery-visible](tasks/archive/perf-imagesession-recovery-visible.md) 交付。另选 Graph 或 Agent 链时仍定义故障点、等待上限和四种边界；与工作流体验组运行/重试入口重叠时先交接。
2. **验证活动负载，不只验证历史页面。** 连续生图活动 Status/SSE 已由 [perf-imagesession-active-status](tasks/archive/perf-imagesession-active-status.md) 交付：固定宽度下保持全量活动任务，省略 prompt 使 300 条仍低于 `<1MiB`。dispatch 已有慢连续生图恢复与正常投递的共存证据；watch 恢复域已独立调度，后续调查域内坏条目饥饿和共享资源饱和，不能把调度独立推广为数据库资源隔离。不预设分页或缓存方案。
3. **补足可解释的测量。** 确定慢在锁等待、查询、JSON、队列还是 provider；只有现有指标无法回答已选问题时才补 histogram/trace。`last_ms`、瞬时 waiter 数和局部 query duration 不能互相替代。
4. **准备候选发布证据。** 候选 checkout、测试输入与运行资源固定后执行受影响门和全量门；Agent 行为消费质量组可采信版本。没有冻结候选时不反复跑 G-07 追逐移动的 HEAD。

每张新任务说明：商家影响、当前可观察现象或待验证假设、独立结果、必要源码入口、冻结输入和隔离资源、成功/失败判据。测量可以得出“不需要改运行时”，但不能只有一张没有决策用途的跑分表。具体认领、审核与归档复用 [看板协议](tasks/README.md)，不另建组内流程。

## 验收与预算

连续生图检查点已改为 [任务与续投信封原子提交](../history/agent-runtime-timeline.md#2026-09-06-连续生图检查点原子续投)：批次确认后同事务回 queued/PENDING 并释放旧消费 lease，使用现有 1s 续投延迟。进程退出回归不再修改时间戳，三轮正常/取消路径重投均在 0.968–0.998s 内；正常只补第二张，取消不打 provider，首张下载字节保持。信封写入失败回滚检查点，旧 token 不得修改新消费 lease。此窗口消除旧 35 分钟 lease 等待；provider 执行中退出及检查点提交前失败仍受原恢复条件约束。

验证强度取决于改动：局部字段/查询测试、真实 PG 事务与并发、跨进程故障、浏览器和真实 provider 分别证明不同边界。涉及对应合同或准备候选发布时扩大范围；默认测试跳过 opt-in 不等于专项通过。

| 改动或问题 | 必要验证入口 | 判定重点 |
|---|---|---|
| Graph 锁、lease、取消与自动采用 | `go test -C go ./internal/graph`；锁序和 lease 测试按场景重复/race | 无死锁、迟到 writer 拒绝、用户文稿与产物状态一致 |
| 投递、恢复、admission | queue/受影响领域包；原生 River 双客户端轮询回归；`just go-test-staging-field` 按隔离资源执行 | 业务与 job 同事务、延期不提前、双消费者领取、恢复不重复副作用；原 PENDING→SENT 指标随旧投递层退役，统一队列门按路线 §16 验证受理到实际 provider 的等待；进程测试不替代容器拓扑 |
| Graph 读取 | `just go-test-graph-query-plan`、`just http-ab-gates`、`just web-e2e-workbench-performance`、`just web-build` | 实际 SQL、响应字段/字节、TTI、按需请求、active-run fixture |
| 共享展示队列总览 | `just go-test-queue-overview-load`、generation 快照交错回归 | 25k runs / 100k nodes，活动 run 为 25k/100/0；running 优先、终态父 run 排除、实际 SQL 计划与总览读取 p95 <300ms。本地回归预算不构成 HTTP 或生产 SLO |
| ImageSession 读取 | `just go-test-imagesession-query-plan`、`just go-test-imagesession-http-load`、包内 HTTP/SSE 回归 | 详情、历史、Status 各自的数据形状；静态页面 gate 不覆盖活动 SSE 成本 |
| ImageSession 活动 Status | `just go-test-imagesession-active-status`；包内 `TestImageSessionStatusActiveSetHTTP` | 26/100/300 活动任务不截断、`has_active` 与列表一致、无原始 JSON、固定夹具 `<1MiB`；查询次数不随活动集线性增加 |
| ImageSession SSE 重复快照 | `just go-test-imagesession-sse-load` | 固定订阅/字段宽度下的帧数、正文、PG 回读、心跳、无通知变化、终态及重连；不等同完整容量门 |
| Agent 读取 | `just go-test-agent-query-plan`、`just go-test-agent-read-load`、`agent/capacity_test.go` | 固定宽度下 Session 列表 p95 <300ms、Turn 页/详情 <500ms、正文 <1MiB；50 条 Turn 页每次 5 个 Query/Row 回调，1000 条分页不重不漏。事件/journal 深度另测 |
| Agent batch/WAL/恢复 | `just go-test-agent-journal-capacity`、`just agent-service-test-local-journal-capacity`；`event_confirm_test.go`、`sigkill_gopg_test.go`、Node journal/turn 测试 | 连续 seq、ACK、fencing、结构/终态屏障、恢复和诚实终态 |
| Agent SSE/页面恢复 | `just web-e2e-agent-sse`、`just go-test-agent-browser-gap` 与对应 conversation runtime 测试 | gap 保持 live 流、重复/迟到帧、终态补洞、parked approval、共享订阅；[订阅取消](../history/agent-runtime-timeline.md#2026-09-06-agent-补洞请求随订阅取消) 和 [重连期间旧请求占用](../history/agent-runtime-timeline.md#2026-09-06-agent-重连补洞请求隔离) 有局部回归；隔离浏览器门实测 3/10k 分页补洞及三事件原生 SSE 截断重连，范围和证据见 G-04/G-05 |
| 媒体内存 | `just go-test-zip-rss`、`just go-test-zip-http-rss` 与受影响上传/导出测试 | HTTP 门固定 10/100 张有效 PNG、额外 RSS 保守上界 ≤128MiB，校验 ZIP 字节身份、鉴权和临时文件清理；明确 writer、商品图库 HTTP 与交付导出的范围 |

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

G-01 至 G-07 保留为发布合同，状态绑定候选而非永久关闭。S1-S6 实现切片已交付并收入 [原生产计划及历史验收](../history/agent-runtime-timeline.md#platform-production-gate-history)，不重新开工。历史 PASS 保留；后续局部实现与验证见本页证据，不自动构成新候选全量 PASS。

| Gate | 必须证明 | 现有证据资格 |
|---|---|---|
| G-01 合同回归 | manifest/schema/DTO 对齐、checkpoint/invocation 幂等、usage/reason code、事件分页与 unknown 语义 | 有历史无缓存全量记录；新候选按影响面重跑，不能引用旧版本自动通过 |
| G-02 PG 正确性 | manifest 中可恢复工具的 effect 状态矩阵、幂等键、并发 scanner、旧 fencing writer 拒绝 | 历史八工具矩阵保留；工具集合变化时按当前 manifest 核验覆盖 |
| G-03 崩溃边界 | 模型开始后、mutation 成功/result 前、approval 后、turn/end 落库/响应前 SIGKILL；seq 连续、副作用至多一次、诚实终态、无永久活动 Turn | Go+PG helper 四点历史已过；Node/真实 provider 各自证据不能混称全覆盖 |
| G-04 浏览器恢复 | 普通断线、gap、重复、旧 generation、overflow、terminal gap、approval 刷新；无错误断线提示 | 2026-09-05 `ebc7630d` 工作树 `web-e2e-agent-sse` 5 passed；[2026-09-06 原生 EventSource 截断恢复](../history/agent-runtime-timeline.md#2026-09-06-agent-原生-sse-连接截断恢复) 三轮 cursor 0→1、序号各一次、无运行时错误，261.7–262.8ms。单次有限重放，不替代其余矩阵或完整 UI 复验 |
| G-05 标准容量 | 25 并发 Turn、100 SSE、单 Turn 10k 事件、单会话 1000 Turn，及上表时延与正确性断言 | [2026-09-06 分场景复验](../history/agent-runtime-timeline.md#2026-09-06-agent-容量分场景与实时-sse)：1×10k 深度 / 25×128 并发写 p95 68.57/203.30ms；100 SSE 同一新事件 p95 37.52ms。[浏览器补洞](../history/agent-runtime-timeline.md#2026-09-06-agent-隔离浏览器补洞容量) 10k 事件三轮 595.0/587.3/664.3ms，满足 5s。各维度独立、固定小正文，未测同时满载或持续推送；当前移动工作树不签固定候选全量通过 |
| G-06 真实业务 | 冻结真实模型/配置的完整 Skill 评测、审批到单一 WorkflowRun、真实图运行、Chromium；确认有效 background 能力为 false | 真实审批/出图已有历史 PASS，行为门仍由 [Agent 质量组](agent-eval-system.md) 的固定 `run_id` 裁定；旧 `gpt-5.6-luna` 15/15 冒烟不满足 L1-L6 出口 |
| G-07 候选全量 | 干净固定 checkout，无缓存 Go、Agent Service、Web test/lint/build、docs-check、migration fresh/upgrade、diff check | `fb658633` 于 2026-09-04 通过；2026-09-06 全 Go 集成复验失败且运行期间代码变化，不能签收固定候选；B6 窗口 FAIL；**2026-09-07** 独立 checkout `e8cb494d`+最窄修补无缓存全量 **PASS**（见 [release-r6-clean-candidate-gate](tasks/archive/release-r6-clean-candidate-gate.md)）。≠ R6 |

当前有效能力保持 D-03：provider profile 与 adapter 能力取交集，Pi adapter 的 `background_resumable` 为 false；不新增绕过 Pi 的模型执行器。字段存在不表示可以后台续跑。PG 权威、7 天 chunk 压缩、不可恢复模型中断与 effect 对账的 D/C 合同在历史来源和现行代码中保留，不因文档重排放宽。

组内局部交付无需等待其他组所有目标完成；宣告产品可发布时，协调者必须消费同一候选及可采信固定输入的完整证据。模型能力未过、未执行、无效运行和可复现的 FAIL 分别记录，不能通过关闭采证 issue 改写结论。

## 自托管发行与恢复基线

<a id="self-host-release-baseline"></a>

调查任务：[release-readiness-baseline](tasks/archive/release-readiness-baseline.md)。源码基线：认领时 HEAD `d6709c4aacb2e26bb30ab70a99d08b1dca05f487`（短 `d6709c4a`）。方法：只读 `docker-compose.yml`、`docker-compose.staging.yml`、`scripts/release.sh`、各 Dockerfile、`.env.example`、`go/cmd/productflow-migrate`、`go/internal/platform/db/schema`、`go/internal/platform/storage`、`go/internal/media`、`go/internal/settings`、`agent-service` 持久路径与配置加载器；未启动/停止 Docker、worker、DB，未读 `.env` 秘密或真实 storage，未跑 `just dev` / `just release`。本节约束 [总纲 §9/§10](../ROADMAP.md#9-自托管与自营站点交付) 与 R6：开发 Compose/`release.sh` 健康检查 ≠ 商用可交付。

### 运行单元与构建

| 单元 | 镜像/构建 | 进程命令 | 角色 |
|---|---|---|---|
| `productflow-postgres` | 上游 `postgres:16` | 官方入口 | 业务权威库 |
| `productflow-redis` | 上游 `redis:7`，`appendonly yes` | `redis-server` | 认证限流；不承担任务队列 |
| `productflow-migrate` | `go/Dockerfile` 多二进制之一 | `productflow-migrate` | 启动前 schema：`CreateTable`/`AddColumn` + ExtraDDL；`restart: "no"` |
| `productflow-go-api` | 同上 | `productflow-api` | HTTP、媒体读写、设置、internal Agent API |
| `productflow-go-worker` | 同上 | `productflow-worker` | River 消费；metrics 可选 |
| `productflow-go-dispatcher` | 同上 | `productflow-dispatcher --watch` | PENDING→SENT→enqueue；域恢复 |
| `productflow-agent-service` | `agent-service/Dockerfile` | `node dist/main.js` | Pi 适配器；`AGENT_DATA_ROOT=/data` |
| `productflow-web` | `web/Dockerfile`，构建参数 `VITE_API_BASE_URL=""` | nginx | 静态前端；`/api/` 反代 |

`docker-compose.staging.yml` 仅叠加第二组 API/worker/dispatcher（默认宿主端口 29290/29296/29295），共享同一 PG/Redis/storage；不能据此宣称多主机或共享媒体一致性已验收。版本/镜像：**B2** 提供不可变 tag 与 `release/` 安装包（`scripts/release-build-images.sh` / `release-pack.sh`）；开发路径仍可用本地 `docker compose build`。`just release` = 源码树 `up -d --build` + 四项健康检查，且不删 volumes；空主机应使用发行物而非该脚本。

### 必要配置与密钥来源

| 输入 | 来源 | 用途 | 备注 |
|---|---|---|---|
| `POSTGRES_PASSWORD` | `.env`（env-only） | PG 与 `DATABASE_URL` | Compose 强制；样例见 `.env.example` |
| `DATABASE_URL` / `REDIS_URL` | env | 库与 broker | Go 启动后不从库改写这两项 |
| `ADMIN_ACCESS_KEY` | env-only | 部署者 bootstrap 与 Operator 登录 | 公开注册创建普通 User、Merchant 与 Membership |
| `SMTP_HOST`、`SMTP_PORT`、`SMTP_SECURITY`、`SMTP_USERNAME`、`SMTP_PASSWORD`、`SMTP_FROM_ADDRESS`、`SMTP_FROM_NAME` | `.env.dev` 默认；`app_settings` 可覆盖 | 公开邮箱注册邮件 | 设置页恢复默认删除数据库覆盖并回到环境值；密码不回显、导出或写日志 |
| `SESSION_SECRET` | env-only | 会话 cookie | 更换会使既有会话失效 |
| `AGENT_SERVICE_INTERNAL_TOKEN` | env-only，≥32 字符 | API↔Agent | Agent `config.ts` 硬校验长度 |
| `METRICS_BEARER_TOKEN` 等 | env，可空 | `/metrics` | 空则不注册或不开 metrics 服 |
| `SESSION_COOKIE_SECURE`、`BACKEND_CORS_ORIGINS`、上传上限 | env / 可被 `app_settings` 覆盖部分 | 公网与上传 | 生产公网地址未在 Compose 固化 |
| `STORAGE_HOST_PATH` | 可选宿主绝对路径 | 绑定 `/app/storage` | 未设则用 named volume `productflow-storage` |
| `provider_profiles.api_key` | PostgreSQL **明文** | prompt/image/agent 调用 | HTTP 不回明文；**无独立密文层**。「解密配置」在现行实现 = 可启动的 `.env` + 完整 PG（含密钥列） |
| `AGENT_PROVIDER_*` | 可选 env | 覆盖 Agent 供应商 | 默认空，走设置页绑定 |

缺凭据：无完整 `.env` 时 Compose 因 `${…:?}` 拒启；无 provider 密钥时仍应能用 `ADMIN_ACCESS_KEY` 登录并看到生成不可用（总纲「可配置」；**缺隔离演练证据**）。

### 外露端口（开发 Compose 默认）

| 宿主端口（默认） | 容器 | 总纲期望 |
|---|---|---|
| `WEB_PORT` 29281 | web:80 | 可对公网（经反向代理） |
| `APP_HOST_PORT` 29280 | api:29280 | 可经代理；开发栈直接映射 |
| `POSTGRES_HOST_PORT` 15432 | 5432 | **不应**按开发设置暴露公网 |
| `REDIS_HOST_PORT` 16379 | 6379 | 同上 |
| dispatcher/worker metrics 29285/29286 | 同端口 | 同上 |
| Agent 29284 | **未** publish 到宿主 | 正确默认；`release.sh` 用 `docker compose exec` 探活 |

生产式叠用：`docker compose -f docker-compose.yml -f docker-compose.prod-ports.yml …` 去掉 PG/Redis/metrics 的宿主 `ports`（策略为**不发布**，非仅绑 127.0.0.1）；Web/API 宿主端口仍保留。开发本机调试可继续只用 `docker-compose.yml`。

### 持久数据与备份对象

| 持久面 | 路径/卷 | 权威性 | 备份 |
|---|---|---|---|
| PostgreSQL | `productflow-postgres-data` → `/var/lib/postgresql/data` | 业务、journal、outbox、`provider_profiles`、`app_settings` | **必备**；一致点以逻辑 dump 或停写后文件系统快照为准 |
| 媒体与日志 | `productflow-storage` 或 `STORAGE_HOST_PATH` → `/app/storage`：`media/{uuid前2位}/{uuid}{ext}`、同目录 `.variants/`、`logs/` | 字节权威在磁盘；DB `media_objects.storage_path` 引用 | **必备**原图；变体可重建但恢复后缺原图会 `missing_file`/`verification_status=missing` |
| Pi / Agent 本地 | `productflow-agent-data` → `/data`：`publisher.json`、`agent-service.lock`、`runs/`、`sessions/`、`workspaces/` | **非**业务权威；模型 loop / handoff 材料（运行目录，不是用户工作区或切换状态） | **必备**（总纲与任务合同）；缺则重启后 lease owner / 会话续跑材料受损，PG journal 仍在 |
| 演化轨迹 | `productflow-agent-traces`；默认 `AGENT_EVOLUTION_TRACES=0` | 诊断，非业务 | 可选；开启则纳入备份清单 |
| Redis AOF | `productflow-redis-data` | 认证限流计数 | 备份策略按认证限流连续性要求确定；任务恢复以 PostgreSQL 业务行与 River 作业为准 |
| 启动密钥文件 | 部署者持有的 `.env`（勿入库） | 启动与会话 | **必备**；与 PG 同代 |

一致备份点（合同；**B3 已提供脚本**，完整 D3 实跑见 B4）：（1）停止或排空 worker/dispatcher/Agent 接受新作业（`CONSISTENCY_MODE=drain`），或接受崩溃一致并记录在途 risk（`crash`）；（2）同窗口备份 PG + storage + agent-data + `.env`；（3）可选 Redis；（4）记录镜像 digest/源码 commit、迁移命令身份、备份起止时间。运行中作业：持有 lease 的 Graph/ImageSession/Agent/Delivery/LocalEdit 在恢复后按既有 recovery 合同收敛；不可证明供应商结果保持 `unknown`，不自动当失败重放。

### 迁移流程（现行机制）

1. Compose：`productflow-migrate` 在 postgres healthy 后跑一次；API/worker/dispatcher `depends_on: service_completed_successfully`。
2. 命令：`schema.Apply`（EnumDDL → CreateTable/AddColumn → ExtraDDL）再 `graph.BackfillDocumentOrigin`；**不**删退役表/列；**不用** AutoMigrate。
3. 包测：`go/internal/platform/db/schema/migrate_test.go`（空库 Apply、二次 Apply 保行、补列探针）——**开发库测**，不是发行 N→N+1 夹具。
4. 升级起点：首个**稳定发行版**之后；retired V1/v2 / 实验库仍不支持。B5 已收窄 [CONTEXT Mainline Scope](../../CONTEXT.md)、[`docs/README.md`](../README.md) 中对**经营数据 N→N+1** 的歧义表述（退役范式兼容仍禁止；稳定版 `schema.Apply` 升级不在禁止之列）。

### 逐单元：配置 → 持久 → 备份 → 恢复 → 业务断言

| 单元 | 配置输入 | 持久 | 备份项 | 恢复动作 | 业务断言（演练时） |
|---|---|---|---|---|---|
| postgres | `POSTGRES_*` / `DATABASE_URL` | PG 数据目录 | `pg_dump` 或停写快照 | 新卷 restore → migrate 幂等成功 | 商品/资产行、设置、`river_job`、Agent journal 可读 |
| redis | `REDIS_URL` | AOF | 可选同代 | 空 Redis 亦可 | dispatcher 能把 PENDING/需重投条目送回队列 |
| migrate | `DATABASE_URL` | 无自有卷 | — | 失败则依赖服务不启动 | exit 0；指纹稳定（现有 head 测） |
| go-api | 全套 go-env + `STORAGE_ROOT` | storage 卷 | 与媒体同备 | 挂同代 storage + `.env` | `/healthz`；登录；媒体下载非 missing |
| go-worker / dispatcher | 同 api + metrics | 共享 storage | 同左 | 同左 | 无永久活动态；unknown 保留；不重复 applied 副作用 |
| agent-service | token、`AGENT_DATA_ROOT`、`PRODUCTFLOW_INTERNAL_BASE_URL` | agent-data（+traces） | `/data` 树 | 恢复 publisher/runs/sessions | `/healthz` `runtime=productflow-pi`；parked 不因重启误失败 |
| web | 构建期 API base 空 | 无业务持久 | 镜像即可 | 重建镜像 | `/healthz`；**`/api/healthz` 经 nginx 到 `productflow-go-api`（B1 已修）** |

### 差距分类

**已可用机制（源码/Compose 存在，≠ R6 通过）**

- 单主机 Compose 全栈、migrate 前置、local storage、Agent `/data` 卷、`.env.example`、`release.sh` 四项探活、`migrate_test` 空库/幂等、README 安装步骤。

**缺实现**

1. **Web 反代上游名**：**已由 B1 修正**——`web/nginx.conf` 现指向 Compose 服务名 `productflow-go-api:29280`（见 [release-compose-proxy-overlay](tasks/archive/release-compose-proxy-overlay.md)）。
2. **无版本化发行物**：**B2 已交付安装包路径**——不可变镜像 tag、`release/` 锁定 compose/env/`VERSION`、`scripts/release-{build,push,pack}*`；空主机按 [release/README.md](../../release/README.md) 安装。仍缺：正式 registry 上的稳定版冻结、完整 D1 隔离实跑证据（见任务）、R6。
3. **无备份/恢复工具与一致点自动化**：**B3 已交付脚本与 runbook**；**B4 已交付隔离全栈 D3 演练证据**（登录/媒体/Agent health/版本身份；在途作业 UNKNOWN）。仍缺：R6；真实在途 lease / unknown 作业夹具实跑。
4. **无稳定版 N→N+1 升级包**：**B5 已交付合同与脚本**——`scripts/release-upgrade.sh`（预检→备份→换 pin→migrate→冒烟；migrate 失败停 app 层并钉回 N pin / 指引 `release-restore`）；`release/README.md` 与 CONTEXT / docs/README 歧义已收窄。等价夹具见 [release-n-to-n1-upgrade](tasks/archive/release-n-to-n1-upgrade.md)。**正式 D4**（非 retag）已在 pin 对 `0.0.0-5ed2b916b569`→`0.0.0-67b0f3092158` 采证（见 [release-r6-pin-and-formal-d4](tasks/archive/release-r6-pin-and-formal-d4.md)）。默认单副本资源足迹观测见 [release-r6-resource-budget](tasks/archive/release-r6-resource-budget.md)。资源预算门已归档；**总纲 R6 已通过**（见 close-ruling；满载等为残余非宣称）。
5. **开发端口默认可达 PG/Redis/metrics**：开发 `docker-compose.yml` 仍映射本机调试端口；**生产式叠用** `docker-compose.prod-ports.yml` 已提供（B1），不把 PG/Redis/metrics 发布到宿主。
6. **正式 Operator/商家初始化**：管理员 bootstrap、SMTP 普通账号注册与密码会话已有实现；发行候选仍需验证多商家隔离与管理员商品管理对接点。
7. **诊断包**：无脱敏一键诊断交付物。

**缺验证（机制或文档有，无隔离证据）**

- 空主机等价 D1 主路径已由 B6 在 pin `0.0.0-5ed2b916b569` 采证（登录/设置读写/`provider_profiles=0`/重启；**浏览器缺 provider UI 仍未采证**）；D3 隔离恢复主路径已由 B4 采证（在途 lease/unknown 仍缺夹具）；迁移失败停机与回退的合同/脚本已由 B5 交付；正式 D4（`0.0.0-5ed2b916b569`→`0.0.0-67b0f3092158`）已采证；干净候选 G-07 PASS 且 pin `0.0.0-67b0f3092158` 已对齐；默认单副本空闲/轻负载资源足迹已由 [release-r6-resource-budget](tasks/archive/release-r6-resource-budget.md) 采证（**非 SLA**；满载/多商未测）；重启后 unknown 作业；staging 双副本共享卷一致性；**总纲 R6 已通过**（[close-ruling](tasks/archive/release-r6-close-ruling.md)）；残余：满载/多商容量、registry.npmjs TLS 默认全量 build 备注、registry push、在途 lease 夹具等（不阻塞 R6）。

**需 Operator 决策**

- 公网入口与 TLS、`SESSION_COOKIE_SECURE`、CORS、哪些端口绑定 localhost。
- `STORAGE_HOST_PATH` vs named volume；备份落盘位置与保留代际（**不杜撰 RPO/RTO**）。
- 备份窗口：排空作业 vs 接受崩溃一致。
- 首个稳定版标签何时冻结；自营与对外自托管是否同一 tag。
- 主机规格与并发上限：空闲/轻负载足迹与观测依据建议见 [release-r6-resource-budget](tasks/archive/release-r6-resource-budget.md)；**满载容量未测，禁止写入 SLA**。
- provider 密钥是否接受 PG 明文存储或要求后续加密（现状明文）。

### 场景推演（合同级，未实跑）

| 场景 | 预期可观察结果 | 归属 |
|---|---|---|
| 空主机 | 仅有 Docker/发行物/填写后的 env 样例应能拉取或 load 镜像、migrate、起栈、管理员登录；**B1/B2** 已提供上游修正与发行物路径。仍缺：正式 registry 上的 HEAD 全量 build/push 证据、登录/缺 provider 的完整 D1 业务断言、R6 | 缺验证（机制已有） |
| 缺凭据 | 缺强制 env → Compose 拒启；缺 provider → 登录成功但生成/Agent Unavailable | 部分机制有，缺验证 |
| 恢复缺媒体 | DB 有 `storage_path`，读文件 → missing；标记 `verification_status=missing` | 机制有，缺恢复演练 |
| 恢复缺密钥 | 缺 `.env` 无法启动或会话/Agent 令牌失败；缺 PG 密钥列则 provider Unavailable | 同上 |
| 迁移失败 | migrate 非 0 → API/worker/dispatcher 不达 healthy；B5 升级脚本 fail-stop 并钉回 N pin | Compose 依赖有；B5 合同/等价夹具有；正式 D4 fail-stop 已在 `5ed2b916b569`→`67b0f3092158` 采证 |
| 重启有 unknown | 不可证明结果保持 unknown，不自动当失败重试；lease 过期后 recovery 收敛 | 域内测试有，缺发行拓扑实跑 |

### 可执行演练合同（后续隔离任务）

共同冻结：候选 commit、镜像 digest、env 样例哈希（无秘密）、空项目名、禁用共享 dev 卷。禁止在开发站跑 `scripts/release.sh` 当作 R6。

**D1 隔离干净安装**

1. 空目录放入发行物（**B2**：`release-pack` 产出的 `productflow-<tag>/` 或 `.tar.gz`）与从 `.env.example` 生成的密钥；用 `images.env` pin 镜像，不依赖 git checkout。
2. `docker compose config`；up；migrate completed；四项 health（修上游后）。
3. 断言：管理员登录；设置页可开；未配 provider 时生成入口明确不可用；PG/Redis/metrics **未**对非信任网暴露（按 Operator 生产 overlay）。

**D2 一致备份**

1. 写入探针：一商品、一媒体原图、一设置/provider 行（或 mock）、一可识别 Agent/Pi 文件或 Turn 投影。
2. 按一致点备份 PG + `/app/storage` + `/data` + `.env`。
3. 断言：清单含四类对象与 commit/digest；记录是否排空在途作业。

**D3 恢复后权限/资产/任务**

1. 新项目/新卷 restore 四类对象；migrate 幂等；起栈。
2. 断言：同一 `ADMIN_ACCESS_KEY` 可登录；探针媒体可下载且非 missing；provider `has_api_key` 真且（若曾配置）可解析；PG 中任务/outbox 与恢复策略一致；Agent `/healthz` 与 publisher 身份可解释。

**D4 稳定 N→N+1**（首个稳定版标签之后）

1. 夹具：N 版数据 + 媒体 + agent-data。
2. 预检 → 备份 → 换 N+1 镜像 → migrate → 冒烟。
3. 失败则停止并文档化回退到备份；禁止引入 V1/v2 路径。

验收命令方向（实跑任务填写具体隔离项目名）：`docker compose config`；healthz 四处；针对性 `pg_restore`/`psql` 探针查询；媒体 HTTP；`go test` migrate 包（不替代 D4）；候选全量仍走 G-07。本调查仅跑 `just docs-check` 与 `git diff --check`。

下方 B1–B6 表保留各次交付当时的中间状态；其中 B6 的早期 FAIL 是历史窗口，后续干净候选、pin+D4 和关闭裁定已经更新当前结论。历史证据不重写，当前发行判断以最新关闭裁定为准。

### 后续修复批次（建议顺序）

| 批次 | 结果 | 环境 | 验收 |
|---|---|---|---|
| B1 | 修正 web→API 上游（或 compose alias）；生产端口 overlay（PG/Redis/metrics 默认不公网） | 隔离 compose | **已交付**（2026-09-07）：`web/nginx.conf` 上游改为 `productflow-go-api:29280`；新增 `docker-compose.prod-ports.yml`（`ports: !override []` 去掉 PG/Redis/dispatcher·worker metrics 宿主映射）；README 写明叠用。隔离项目 `pf-b1-proxy-20260907` 四项探活通过（含经 web 的 `/api/healthz`）。证据见 [release-compose-proxy-overlay](tasks/archive/release-compose-proxy-overlay.md)。≠ R6；≠ B2 发行物；验证未用当前 HEAD 全量 `docker compose build`（见任务证据）。 |
| B2 | 版本化镜像 tag + 锁定安装包（compose、env 样例、版本文件） | 镜像仓库或本地 registry | **已交付路径（2026-09-07）**：不可变 tag `<VERSION>-<sha12>`；`scripts/release-build-images.sh` / `release-push-images.sh` / `release-pack.sh`；`release/` 锁定 compose + prod-ports + `.env.example` + 空主机 [release/README.md](../../release/README.md)。包输出 `dist/release/productflow-<tag>/`。自营与自托管同一发行物。≠ R6；≠ B3 备份。构建环境若 registry TLS 失败须如实记录，不得伪装 HEAD 全量 build。D1 隔离实跑可另证据或同窗口；合同与缺口见 [release-versioned-artifact](tasks/archive/release-versioned-artifact.md)。 |
| B3 | 备份/恢复脚本与一致点 runbook（含 Pi 与 `.env`） | 隔离卷 | **已交付路径（2026-09-07）**：`scripts/release-backup.sh` / `release-restore.sh` + `release_backup_common.sh`；覆盖 PG（`pg_dump -Fc`）、storage 媒体、agent `/data`、部署 `.env`，可选 Redis / agent traces；`CONSISTENCY_MODE=drain|crash` 记录是否排空在途作业与 commit/digest；默认拒绝共享项目名 `productflow`；发行包经 `release-pack` 带入同脚本；runbook 见 [release/README.md](../../release/README.md)。D2 清单由 MANIFEST 对象与标志对齐。≠ D3 全项实跑（B4）；≠ R6；不写 RPO/RTO/SLA。证据见 [release-backup-restore](tasks/archive/release-backup-restore.md)。 |
| B4 | 执行 D3 恢复演练并留证据 | 新目录/实例 | **已交付（2026-09-07）**：隔离项目 `pf-d3-src-20260907` → `release-backup`（drain）→ `pf-d3-dst-20260907` 全栈 `release-restore`；发行 pin `0.0.0-5ed2b916b569`。断言 PASS：migrate、四项 health（含 Agent `runtime=productflow-pi`）、`ADMIN_ACCESS_KEY` 登录/会话、探针媒体 HTTP（非 missing）、假 provider `has_api_key`、Agent `/data` 探针、MANIFEST 版本身份 + `CHECKSUMS`。在途作业收敛 **UNKNOWN**（无 lease 夹具）。≠ R6 / ≠ B5；不写 RPO/RTO/SLA。详见 [release-d3-restore-drill](tasks/archive/release-d3-restore-drill.md)。 |
| B5 | 首个稳定版起 N→N+1 合同、夹具与文档歧义收窄任务 | 双版本夹具 | **已交付（2026-09-07）**：`scripts/release-upgrade.sh`（预检→drain 备份→换 N+1 pin→migrate→四项 health；失败停 app 层、钉回 N pin、打印 `release-restore` 回退）；`release-pack` 带入脚本；`release/README.md` B5/D4 节；收窄 CONTEXT Mainline Scope 与 `docs/README.md` 对经营数据升级的歧义。隔离等价夹具（发行 pin `0.0.0-5ed2b916b569` retag → `0.0.0-b5n1equiv`）主路径与 `UPGRADE_SIMULATE_MIGRATE_FAIL` fail-stop 证据见 [release-n-to-n1-upgrade](tasks/archive/release-n-to-n1-upgrade.md)。≠ R6；不写 RPO/RTO/SLA；等价夹具 ≠ 冻结稳定版对。 |
| B6 | 冻结候选上 R6 + 所需 G-07 | 隔离资源 | **已汇总（2026-09-07）证据门 FAIL / R6 未通过**：等价 D1（pin `0.0.0-5ed2b916b569`，项目 `pf-r6-d1-20260907`）登录/设置/空 provider/重启 PASS；D3 沿用 B4 PASS（在途 UNKNOWN）；N→N+1 初为 B5 等价夹具；G-07 在起点 `63d41d0b` **FAIL**。详见 [release-r6-readiness-gate](tasks/archive/release-r6-readiness-gate.md)。干净候选 [release-r6-clean-candidate-gate](tasks/archive/release-r6-clean-candidate-gate.md)：G-07 **PASS**。跟进 [release-r6-pin-and-formal-d4](tasks/archive/release-r6-pin-and-formal-d4.md)：pin `0.0.0-67b0f3092158`≡`67b0f309`；正式 D4 `5ed2b916b569`→`67b0f3092158` 主路径+fail-stop **PASS**。资源足迹观测见 [release-r6-resource-budget](tasks/archive/release-r6-resource-budget.md)（空闲/轻负载；**非 SLA**）。资源预算见 [release-r6-resource-budget](tasks/archive/release-r6-resource-budget.md)。维护者关闭裁定 [release-r6-close-ruling](tasks/archive/release-r6-close-ruling.md)：**总纲 R6 通过**（残余非宣称见该文）。 |

未知输入（保持开放）：生产主机 OS/磁盘、备份介质、公网 DNS/TLS、真实商用数据规模、可接受停机策略、首个稳定版日期、是否要求 provider 密钥加密、多商家就绪时间线。不在此填写容量、RPO/RTO 或 SLA 数字。

## 维护约定

本节日期条目是历史变更记录，按原样保留；较早的“R6 未通过”只描述当时窗口，不能覆盖后续 pin/D4/关闭裁定。当前结论见上方目标段与最新关闭裁定链接。

- 新事实回写其唯一 owner：稳定行为写 ARCHITECTURE/PRD/USER_GUIDE，未来方向写 ROADMAP，本页写风险与验收判断，详细复现和采证留 issue。
- 本页不重复维护开放任务数量或认领状态；任务关闭时只更新受影响结论与证据链接。无开放 issue 不代表无风险，有观测缺口也不自动产生实现单。
- 旧设计允许纠正。记录当前因果依据、替代判断与未验证项；不把历史方案、评测阈值或架构拆分天然视为正确，也不通过改写目标掩盖失败。
- 2026-09-05 重写依据：当前 queue/dispatcher、Graph lease/recovery、Agent execution/turn-runtime/batch、ImageSession serializer/SSE、metrics 和容量测试。修正了统一 `has_more`、详情与状态成本混用、capacity 维度相乘和 G-07 永久完成等误读；这是源码与证据整理，未改运行时，未做新一轮生产验收。
- 2026-09-07 发行基线：只读 Compose/存储/迁移/Agent 持久面，写入本节差距与演练合同；未改运行时，未跑安装/恢复，R6 仍未通过。
- 2026-09-07 B1：nginx 上游改为 `productflow-go-api`；增加 `docker-compose.prod-ports.yml`；隔离项目四项 health 通过。详见任务证据。
- 2026-09-07 B2：版本化镜像 tag 约定与 build/push/pack 脚本；`release/` 锁定安装包与无 git 空主机 D1 方向文档。详见 [release-versioned-artifact](tasks/archive/release-versioned-artifact.md)。未宣称 R6 / B3。
- 2026-09-07 B3：备份/恢复脚本与一致点 runbook（PG + storage + agent `/data` + `.env`，可选 Redis）；MANIFEST 记录版本身份与在途作业处置。详见 [release-backup-restore](tasks/archive/release-backup-restore.md)。未宣称 D3 全项实跑 / R6。
- 2026-09-07 B4：隔离全栈 D3 恢复演练证据（发行物 `0.0.0-5ed2b916b569`；`pf-d3-src/dst-20260907`）。登录/媒体 HTTP/Agent health/版本身份+CHECKSUMS 通过；在途作业收敛 UNKNOWN。详见 [release-d3-restore-drill](tasks/archive/release-d3-restore-drill.md)。≠ R6 / ≠ B5。
- 2026-09-07 B5：N→N+1 升级合同与 `release-upgrade.sh`；文档歧义收窄；隔离等价夹具主路径 + migrate fail-stop。详见 [release-n-to-n1-upgrade](tasks/archive/release-n-to-n1-upgrade.md)。≠ R6；≠ 冻结稳定版对正式 D4。
- 2026-09-07 B6：R6 汇总门证据写入 [release-r6-readiness-gate](tasks/archive/release-r6-readiness-gate.md)；等价 D1 补登录/设置/空 provider；G-07 FAIL；**总纲 R6 未通过**。跟进干净候选重跑见 [release-r6-clean-candidate-gate](tasks/archive/release-r6-clean-candidate-gate.md)。
- 2026-09-07 干净候选门：独立 checkout `e8cb494d`；最窄修补后 G-07 无缓存全量 PASS；修补入库 `67b0f309`。证据见 [release-r6-clean-candidate-gate](tasks/archive/release-r6-clean-candidate-gate.md)。
- 2026-09-07 pin+正式 D4：新 pin `0.0.0-67b0f3092158`（≡`67b0f309`）；隔离项目 `pf-r6-d4-n-20260907` 上 `0.0.0-5ed2b916b569`→新 pin 主路径四项 health + `UPGRADE_SIMULATE_MIGRATE_FAIL` fail-stop **PASS**（非等价 retag）。证据见 [release-r6-pin-and-formal-d4](tasks/archive/release-r6-pin-and-formal-d4.md)。
- 2026-09-07 资源预算：隔离项目 `pf-r6-budget-20260907`、pin `0.0.0-67b0f3092158`；空闲稳态 + 四项 health + bootstrap/登录/`/api` 探针下各容器 CPU/内存；部署规模对应表与观测依据建议（≥2 vCPU / ≥4 GiB，**非 SLA**）。证据见 [release-r6-resource-budget](tasks/archive/release-r6-resource-budget.md)。关闭裁定见 [release-r6-close-ruling](tasks/archive/release-r6-close-ruling.md)：**总纲 R6 通过**。
- 2026-09-07 R6 关闭裁定：按 ROADMAP 六项对照归档证据，**总纲 R6 通过**；残余非宣称见 [release-r6-close-ruling](tasks/archive/release-r6-close-ruling.md)。≠R1–R5/≠SLA。

## 2026-09-09 多商家固定采样

[容量任务](tasks/saas-multimerchant-capacity-baseline.md) 已在固定 e190e250、4 核/8 GiB 限额、10 商家与本地 1px mock 输出下执行 L1/L2 各三轮、争用和故障实验。sample 阶段 18000 次读取无失败，最大路由 p95 140.005ms、p99 217.289ms；2220 个生成任务成功，provider 归因完整，最大并发 3。实际 RSS 峰值约 0.2163 GiB、PG 峰值 39/100；这些结果不覆盖真实大图内存或生产 SLA。

整体未通过：A100/B20 争用中 B 等待 p95 为 74.629s，超过 10s；故障短窗口内未恢复，但没有覆盖 90 分钟 stale 阈值。L2 SSE 标记受 EOF 后状态读取竞态影响，commit-to-SSE 缺真实提交时间；两项不能冒充有效通过或确证生产根因。root 对预热统计和 Decimal 账户聚合做了绑定原始 hash 的独立重算，原始 FAIL 未覆盖；10 商家账本快照算术一致，争用等待 FAIL 保留。后续应处理实际公平性/恢复问题并补测量缺口，不能仅增加 worker 或放宽阈值。

[商家生成公平性](tasks/archive/generation-merchant-fairness.md) 已在 6a418d8f 实现并用 ca1a3cac 修正测量器复验：dispatcher 按商家服务历史选择有限 generation 预取，Graph 在其他商家等待时停止续取、自然完成在途节点后释放消费轮；单商家仍可用满全局槽。固定同规格 A100/B20 全部成功、provider 各一次、额度一致，B 等待 p95=3.5961s（e190 为 74.629s）、max=3.6144s，HTTP 在途峰值 3；独立真实 Graph/ImageSession 混合场景也完成。长任务非抢占等待单列，未改短任务阈值。实现与本次争用验收完成；整体容量仍缺故障完整恢复窗口、修订后正式 SSE 采样及 commit-to-SSE 时间，不能升级为生产 SLA。

## 2026-09-09 取消后的队列收尾

`platform/queue.Consume` 原来在 Actor 返回后继续用 handler context 更新信封。Actor 内发生取消时，即使业务终态已经保存，`MarkConsumed`、`ReleaseForRetry` 或 `MarkFailed` 仍会因 context canceled 留下 SENT 和消费租约。取消回归在修复前四种返回路径均失败。

当前 Actor 仍接收原执行上下文；返回后，信封使用保留 context values、独立限时 5 秒的上下文完成收尾，写入仍校验原 lease token。重排队和失败收尾的数据库错误向调用方返回。该修改不延长 provider 执行时间，也不把无法证明的供应商结果转成成功或可自动重试。

真实 PostgreSQL 回归覆盖正常完成、Busy、Later、失败及各自租约已被替换的八种情况；ImageSession 集成回归通过实际取消 provider，确认业务与 effect 保持 unknown、不可重试，信封 consumed 且清空租约，重复消费不再调用 Actor。两处定向 race 检查通过（queue 2.941s、ImageSession 3.851s）；旧 auth 包级库的身份迁移约束失败单列，新隔离库 auth 全包通过（24.406s）并已清理。全量 Go 检查已结束，除上述旧 auth 库迁移失败外其余包通过，不能将这次全量命令记为全绿。命令与检查日志保存在 `storage-dev/queue-cancel-finalize-0909/`。集成夹具最初受旧包级库积压影响未执行到 provider，已改为任务独立数据库，保留原失败日志。

此结果覆盖 Actor 能返回时的取消收尾；进程被强制终止无法执行收尾，仍受原恢复扫描及闲置阈值约束，不据此宣称 90 分钟崩溃等待已经解决。

## 2026-09-09 统一队列方案的机制验证

设计与切换门由 [总纲第 16 节](../ROADMAP.md#16-统一后台任务队列选型与切换) 管理。候选代码已接入 River，独立验收记录由统一队列任务维护；共享开发服务切换须另核运行资源及旧停止记录。

独立 [队列探针](../../scripts/queue-evaluation/README.md) 固定 River 0.47.0、asynq 0.25.1 和 Go 1.26.5。2026-09-09 在隔离 PG 库与私有 Unix socket Redis 执行：GORM 外层事务、嵌套 savepoint 的业务行/job 共同回滚通过；共同提交后新客户端消费且可见业务行通过。实际 asynq 重试两次，合成执行器在 PG 记录 unknown 后第二次退出，模拟外部效果仅一次。

原始失败是探针尚未归一化开发 DATABASE_URL 的 driver scheme，发生在建库前；修正后两项 PASS（0.656s），日志分别为 storage-dev/queue-choice-0909/probe.log 与 probe-r2.log。未调用真实供应商；不是进程崩溃、公平性、吞吐或五类生产适配证明。原始失败未覆盖，任务私有数据库和 Redis 子进程随测试清理。root 自审，不宣称独立审核或完整迁移完成。

上述机制探针在仓库路径执行 race 检查通过（2.146s）。追加真实子进程 SIGKILL 验证：子 Worker 已调用独立 HTTP 假供应商但未落业务结果，杀进程后新 Worker 经 River rescue 重新领取（attempt≥2）并持久化 unknown，HTTP 请求数保持 1；PASS，21.972s，日志 storage-dev/queue-choice-0909/crash.log。阈值为测试专用 100ms job timeout、1s rescue，实际等待还包含 leader/维护周期；没有倒拨数据库时间，不将此耗时作为生产上限。相同 SIGKILL 场景 race 检查通过（21.410s，总测试耗时），日志 crash-race.log；现场复核 pf_queue_probe 数据库残留为 0，无私有 Redis 或子 Worker 进程。其它故障点、商家公平与五类业务适配仍开放。

## 2026-09-09 River 五类任务交付

[统一队列验收](tasks/archive/pg-queue-integration.md)记录固定 ff23c2aa 的实现与隔离证据：A100/B20 全部成功、B 等待 p95 4.749s、无重复效果及结算；972 次业务读取成功，PG 连接峰值 32，公平选择 SQL 平均 0.562ms。真实 SIGKILL、短暂 PG 不可用、无通知双客户端、停机、迁移及发布构建已取得证据。最终全包执行仅旧告警名断言失败，更新断言后的 metrics 回归通过；原失败日志保留。该交付支持统一 PG/River，不替代完整多商家容量基线，也未验收生产恢复 SLA。共享服务切换须处置旧停止信封和未收敛业务。

## 2026-09-09 本地开发环境启用 River

用户明确快速开发阶段不兼容旧队列任务。旧应用进程停止后保存本地数据库快照，退役 async_dispatches 的 362 条历史队列记录，结束两个遗留 Agent 执行、释放六条无效执行权；保留账号、商家、483 个商品与原素材。新 API/Worker/业务恢复/Pi/Web 已启动；浏览器真实 API 提交交付转码返回 202，River completed、attempt=1，下载 PNG 为 337×251，页面无运行时错误。schema 退役及幂等回归通过 3.067s。证据位于 storage-dev/river-dev-cutover-20260909；首次探测选择了不满足交付原图合同的素材而返回 400，改用既有成功工作流原图后通过，未修改或绕过校验。无真实模型费用。主代理自审，不宣称独立审核。
