# 任务：固定候选测量多商家混合负载与共享中间件容量基线

状态：认领
类型：证据
认领者：root
认领于：2026-09-09T01:44:07+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：按实测瓶颈发布公平调度或读取优化；公开注册试用版固定候选复验

任务状态、认领及提交遵循 [Issue 协议](README.md)。证据有效且判读完整可交付 FAIL；缺运行或缺关键观测不得关闭。

## 问题来源

当前发行容量证据为空闲/轻负载，已有 HTTP/队列局部门绑定各自夹具；不能证明多商家的混合读取、长连接和生成争用。需要在保持现有 PostgreSQL 权威与 Redis broker 的条件下量出瓶颈，再决定是否新增缓存或调度机制。

## 做成什么样

- 冻结一个已提交候选，报告数据规模、资源限制、依赖版本、混合工作负载和多商隔离断言。
- 对两档负载记录延迟、错误、PG/Redis/进程资源及商家间等待差；给出支持或不支持目标预算的结论。
- 每项优化建议对应观测到的查询、锁等待、队列或连接原因；不在采证里顺手改生产实现。

## 前置与并行

- 前置：现有 R1 代码、可创建两商家测试夹具、PG/Redis/mock provider 及独立本机资源；不依赖新 SaaS 页面。
- 冻结输入：运行前固定完整 commit，测试工具和服务版本同时记录；执行期间不得 cherry-pick 其它任务实现。只消费该版本已有路由，未来路由不计失败或通过。
- 运行资源：独立 checkout、PG DB、Redis 实例/namespace、存储根、API/Web/worker/dispatcher 端口与 cgroup 配额；协调者确认机器总资源有余量后认领。共享主机干扰单列，不能用共享 dev 空闲值签容量。
- 可与产品代码开发并行，但不得用其可变二进制/DB；认领记录具体资源，清理仅本任务进程和目录。

## 只改这些文件

- 新 `go/internal/auth/saas_capacity_fixture_test.go`（仅 test fixture）、`scripts/` 下本任务运行与采集脚本、必要的独立 mock 辅助、`justfile` 最窄命令。
- 如需跨包 test fixture，先由协调者核对并登记精确测试路径，不能从生产包导出绕权 helper。
- 本文件；协调者整合父章程证据摘要。产物位于任务专用 storage 目录，不提交图片、大日志或含凭据配置。

## 不要碰

- 生产 auth/quota/graph/queue 逻辑、Redis 权威边界、正式阈值、现有评分材料、真实 provider、公开注册入口开关。

## 现在代码在哪

`go/internal/auth/dual_merchant_fixture.go`、`merchant_isolation_gate_test.go`、`go/internal/platform/generation/`、`go/internal/platform/queue/`；既有容量和发行脚本按父章程链接定位复用。不直接对真实开发库造十万行数据。

## 合同

1. 目标验证档（尚未达标声明）：单主机应用栈总预算 4 vCPU / 8 GiB，媒体卷独立记录；10 商家、各 1000 商品与 5000 素材、全库 100000 quota events。两商家正反隔离贯穿全程。若机器不具备则报告资源阻塞，不能降规模冒充相同门。
2. L1：20 同时在线客户端，总 5 read req/s，20 条 SSE，3 个同时执行的 mock 生成；L2：50 客户端，总 15 read req/s，50 SSE，同样受 3 个全库生成槽约束。read mix 商品列表 40%、素材列表 30%、会话/运行状态 20%、额度 10%；按已有分页和字段使用完整合法输入。
3. 每档预热 60 秒、采样 5 分钟、三轮；mock provider 固定 2 秒响应并保留实际调度链，不用真实模型延迟掩盖平台成本。必须记录每轮而不只最好值。HTTP 错误分预期 4xx 与非预期失败，吞吐按完成请求算。
4. 目标预算：核心分页读 p95≤300ms、p99≤1000ms；非预期失败率<1%；权限/商家串读/重复结算为 0；静默 SSE 不全量重复推送且在有新状态时 p95≤2s（PG commit→客户端观察）。栈 RSS 峰值≤6 GiB，PG 连接峰值≤配置上限80%；Redis used_memory、evictions、blocked_clients 与命令延迟逐轮记录。
5. 争用实验：A 持续积压 100 个短 mock 作业，B 每 5 秒提交一个共 20 个；报告 B 排队等待 p50/p95/max 与 A 吞吐，区分 accept→dispatch、dispatch→provider、provider→persist。B p95 等待目标≤10秒；当前全库槽不承诺公平，通过测量决定后续。对长任务另做非抢占解释，不能套同阈值。
6. 故障场景独立于稳定负载统计：停止专用 Redis 30 秒再恢复、重启一个 worker/API，检查已受理业务和账本可恢复且无重复结算。不得用 Redis FLUSHALL 模拟重启；保留 PENDING/unknown 事实，不把未知自动算失败重跑。
7. 最终对照每项目标 PASS/FAIL/不可测；不可测写缺哪一个 timestamp/metric，不能根据 broker 消息数推导 provider start。若需最小新增生产观测点，另发有界实现前置后再采证，本任务暂阻塞。

## 怎么验收

- 提交可重复执行命令/脚本、固定版本 manifest、fixture 规模核对、原始 CSV/JSON 摘要、按轮图表或表格、隔离断言与故障后 DB 核对。
- 测量工具本身通过参数/聚合边界测试；不改变被测业务以让预算过关。`just docs-check`、完整任务 diff 检查。
- 完成可报告预算 FAIL；只有达标项支持相应范围，旧 R6 不自动升级为 SLA。后续 SaaS 改动后在新固定候选复验受影响部分。

## 阻塞与交接

- 原因：发布时无合同依赖阻塞；认领须确认隔离资源和机器预算，未满足即按协议记录阻塞。
- 解除条件：可用隔离资源与冻结候选；无需真实模型凭据。
- 跟进者：开发协调者。
- 交接：未采证、未占用运行环境。

## 证据

- 命令 / 日期 / 结果：待执行。
- 基线 commit / run_id / artifact：采证前固定。
- 交付定位：随本任务提交。
- 审核者 / 结论：待审核。
- Issue 结果 / 业务门槛结果 / 剩余缺口：未执行；目标数字均为待验证预算。

## 本次独立资源窗口

root核对本机16 CPU、约15GiB RAM（可用约10GiB）、磁盘余量794GiB，Docker cgroup v1可用。capacity_baseline认领并准备固定e183b66b的独立checkout `/tmp/productflow-saas-capacity-0908`，产物 `storage-dev/saas-capacity-0908`。运行栈限额总和不超过4vCPU/8GiB（目标峰值RSS仍6GiB）；使用专用Docker网络、PG/Redis与容器名称前缀pf-capacity-0908，应用端口30182起，先检查空闲。不得使用主dev数据库/Redis或停止共享服务。观察机器余量，环境不足按原合同报告，不降规模或预算冒充通过。

独占新auth/saas_capacity_fixture_test.go、scripts/saas-capacity-0908*或scripts/saas_capacity/*；justfile如需改由root整合。root持有本单/父章程/文档/Git；其它任务只写Node/Go eval观察和独立评委证据。被测版本只消费固定提交，不吸收共享树后续变化。只用mock provider，不调用真实模型。夹具/测量工具验证和完整原合同采证由执行者负责；必须先记录冻结版与实际限额，证据不足明确失败/不可测，不顺手改生产。完成自审、必要脚本测试、资源清理后交root，执行者不commit/push。

## root执行中审核

root发现初版run_load在3个生成完成后才开始读采样，预热未发送混合读，且将所有4xx归为expected。已要求保留初轮为preflight，修正为预热/采样同步混合读、SSE和持续生成并发后使用新目录完整测量；合法读取4xx应计入非预期失败。不能把初轮宣称L1/L2混合负载达标。

只读辅助审核分配给eval_baseline：仅查隔离checkout中的run_contention、run_fault_recovery、collect_metrics证据因果，不修改代码、文档、运行资源或启动负载；capacity_baseline仍是唯一实现写者，root拥有最终验收。


root续审发现mixed-load-final/l1-round-1.json仍记录provider.max_concurrency=6，不能签为3槽达标；当前mock_provider.py又在provider_slots信号量之后记录started_epoch，会隐藏HTTP已到达但在mock内等待的请求。已要求执行者暂停完整轮次试错，用短mock复现区分历史事件污染、观测器误差与真实应用超发，并记录HTTP到达至响应的在途并发及mock排队时间。模拟服务自身限流不构成应用槽位合同通过证据。SSE当前每客户端约5帧，还需核实订阅是否持续覆盖后续生成任务；未确认前不签持续状态传播预算。


短诊断追加证据：root核对diagnostic/l1-round-96.json及mock/provider-events.jsonl，33个独立请求均为2秒mock，实际到达至末次响应跨度24.177秒，HTTP及provider执行最大并发均为3，33任务成功且无未完成响应。当前mock源码已移除provider_slots信号量，分别记录received/started/finished/responded。该证据仅证明本次短诊断，没有解释旧max6来源，也不替代原定六轮混合负载、持续SSE及争用/在途恢复验收；旧结果保留，等待执行者交回原因与剩余窗口。


争用证据续审：contention-final.json中B的20个请求全部获得provider与终态观测，accept→provider等待p95为61659.55ms，超过10000ms目标，保留FAIL。A原始submissions为100次HTTP 202，但其中52次返回快照未包含对应prompt，submit将其记为task_id=None，使summary.a_submitted及后续账本检查仅覆盖48条。已要求从独立DB以本轮唯一prompt及merchant/session绑定已受理任务，将完整100条纳入核对；不得重发52次请求或把HTTP已受理写为未提交。未补齐前本报告不具备完整任务/结算分母。


分母修复复核：contention-final-repaired.json保留原请求与B等待数值，独立DB补齐A的100个任务，100个均成功且各有一次provider观测；包括长任务在内124个受理请求全部绑定，无歧义或未绑定项，未重发请求。账本摘要覆盖124任务，各记录一次reserve和一次settle；完整金额/余额不变量仍随执行者完整脚本审核验收。该修复不改变B等待p95超标结论。当前queue/dispatcher.go的claimPending按available_at/id排序，未按merchant轮转；这仅是后续排队因果调查入口，尚不能把整个61.66秒归因于该排序。


执行者已交付并清理专用容器/网络，root核对无pf-capacity-0908运行容器，容量独占计时窗口释放。root接管本单剩余脚本审核和缺口处理；当前保留正式轮次并发超额、争用等待超标、故障恢复失败及未完成六轮/commit时延证据，不视为全合同验收通过。capacity_baseline转接r7执行，不再写容量任务源码或启动采样。


root合同复核纠正：固定e183b66b的imagesession/recovery.go明确DefaultStaleRunningAfter=90分钟，settings默认一致，夹具未覆盖image_session_stale_running_after_minutes。fault实验只在Redis恢复后观察旧任务约240秒、新任务180秒，未跨过既有闲置回收阈值。故该FAIL限定为本次短观察窗内未恢复，不能证明90分钟合同失效或永不恢复；三个running继续占槽可解释后续queued反复投递，需要进一步按实际admission因果验证。现有90分钟崩溃可见等待对用户体验不理想，但不能为取得绿灯在采证中擅自缩短生产阈值。后续应独立设计故障识别/安全unknown与槽位释放并验证活跃长请求不被误回收，或在冻结旧合同下覆盖完整恢复窗口。原始fail与时间线保留。


root分配image_live_review辅助只读审核已交付容量fixture/测量脚本与关键原始观测，独占其review产物目录，不修改被测代码、脚本、任务文档或运行资源；root仍是本单交付/修改所有者。审核关注正式max6区间、read/SSE实际覆盖和有效时间窗、quota金额/状态匹配、资源指标分母及可重复命令。恢复等待90分钟与短观测不足已由root确认，不能重复归为未知生产故障。

## 测量工具修订与再次冻结前置

独立复审确认正式事件存在负时长，且旧轮次缺HTTP received/responded，不能将max6直接归因为应用超发；SSE单次连接在服务正常idle结束后停止观察；额度只验计数，缺实际金额/余额；Docker MemUsage不是RSS，collector与load未共用完整时间窗；image tag和共用事件路径不足以绑定实际代码。可用旧事实限实际记录的L1读取、124任务身份/终态与B等待超标，旧未测量字段不回填。

image_live_review现接管同一容量任务的工具修订，独占/tmp/productflow-saas-capacity-0908内scripts/saas_capacity与auth/saas_capacity_fixture_test.go；保留root已改INFO clients/缺字段检测及test_metrics.py。root持文档/Git，不改生产业务。修订须使用单调时钟及不可变轮次事件/指纹、持续SSE连接覆盖、真实quota金额/hold及账户前后算术、共同测量窗口/RSS/实际CPU限额与缺失检测、固定生产源码/工具/镜像身份。初始余额作为manifest显式opening balance，不为补历史事件改变100000条规模。

先交无模型确定性正反测试与可审完整脚本，root验收并提交工具候选后再冻结新的完整运行；允许此必要候选提交，任务仍认领。旧e183测量不吸收后续提交、不覆写事件。修订期间不启动完整六轮、故障、真实模型或共享服务；如需短独立mock工具验证先登记资源。下一生产候选和运行窗口由root确定，不能自行切换或重采至绿。

工具首轮交付通过 21 项 Python 离线测试及静态检查，fixture 仅编译并 skip，尚不能作为真实采集链验收。root 批准 e183 checkout 加修订工具执行一次 diagnostic 短预检：新目录 `storage-dev/saas-capacity-tools-preflight-0908`，专用 `pf-capacity-0908` 容器与原 30182–30190 端口，启动前核闲置；实际 fixture、5 秒预热及 20 秒混合采样和 collector。不执行正式六轮或故障，不作为生产候选 907 的恢复证据，结束清理专用资源。

root 审查要求补查：非 settle hold 的 settled_units、资源窗口中间缺测、故障前实际 running 前置、恢复后新任务终态，以及实际 provider/effect 的重复调用或 unknown 重放。原 commit-to-SSE 延迟仍为 unmeasurable，不能用 persisted timestamp 的近似值替代。缺口未关闭前不提交已验收工具候选或启动正式容量轮次。

上述工具缺口已修订。独立短预检得到 33 个成功任务与 33 个完整 provider 生命周期，HTTP/执行峰值并发均为 3；125 次读取无失败，覆盖 10 个商家；20 个 SSE 客户端持续重连，无错误或提前关闭。8 个有效资源样本完整，最大间隔 3.0857 秒，RSS/CPU/PG/Redis 指标实际可读。证据位于 `/tmp/productflow-saas-capacity-0908/storage-dev/saas-capacity-tools-preflight-0908`，两个失败准备目录保留。运行后专用容器和端口已清理。

root 已将审核后的工具复制到 `scripts/saas_capacity/` 及 `go/internal/auth/saas_capacity_fixture_test.go`，修正一项离线测试对开发工作树清洁状态的无关依赖；25 项 Python 测试与 ruff 通过。此提交仅冻结可复现工具候选，任务继续认领；短预检不代表完整容量或故障验收，commit-to-SSE 时间仍未测。后续须从新候选构建独立镜像并重新采证，不能沿用 e183 的生产版本身份。

## e190 正式采证与 root 判读

固定 `e190e250b68bdd2b623bfbeeb4b0db8b2520f412`、source identity `476f596e7c4cb50117379c816440327e81a499158e61903ab8b1f62280abfadd`、image `sha256:a606460a0f9de2996bf519576389f5e05ff196d26594f476b8af96ee91fc2837`。完整原始产物位于 `/tmp/productflow-saas-capacity-e190e250/storage-dev/saas-capacity-e190e250-0909`；启动路径遗漏单列 startup-failed，没有计入正式分母。六轮、争用和故障均已执行，专用容器/网络/端口已清理。

- L1 三轮各 1800 次读取、L2 三轮各 5400 次读取（含预热），全部无意外失败；六轮共 2220 个生成任务成功，provider 归因完整，HTTP/执行并发均未超过 3。资源窗口完整，实际 CPU 限额 4 核，PG 峰值 39/100，RSS 峰值约 0.2163 GiB。此为固定夹具及 1px mock 输出，不代表真实大图内存峰值。
- 旧汇总 p95/p99 混入预热；root 从不可变 CSV 的 sample 行独立重算，L1 每轮 1500 次、L2 每轮 4500 次，共 18000 次正式读取。最大路由 p95 140.005ms、p99 217.289ms，均满足读取目标。派生证据 `storage-dev/saas-capacity-root-review-0908/sample-only-read-replay-e190.json` 含源 CSV hash，原汇总未覆盖；工具现已将读取成功数、失败率及延迟统一限制为 sample 阶段，保留 warmup 计数和全部 CSV 行；覆盖慢预热、预热失败与无正式样本的回归后 28 项离线测试通过。
- L1 SSE 检查通过；L2 各记录 238/387/357 次 premature closure 并返回失败。EOF 后读取可变 active task 的竞态，以及重连初始快照重复，尚不足以归因生产 SSE 故障。原始失败保留，SSE 语义判读待补；六轮 commit-to-observation 仍 unmeasurable。
- 争用 A100/B20 全部完成，124 个任务的 quota 生命周期与 provider once 完整；B 等待 p95 74.629s，超过 10s，是真实 FAIL。账户总账检查误拒 psycopg Decimal：root 以新回归复现旧 e190 失败、修正精确整数聚合类型后 26 项测试通过；原始 10 商家快照重算全部一致。派生证据 `storage-dev/saas-capacity-root-review-0908/decimal-account-replay-e190.json` 绑定原始 contention SHA，整体争用仍 FAIL。
- fault 前置真实读回 3 running + 1 queued；Redis 实际中断约 44.1s。短观察结束时 3 long 仍 running、新任务 queued/timed_out，provider 仅记录 3/5 预期请求。不能据此证明 90 分钟 stale 恢复失效，也不能宣称恢复通过。

root 接管剩余判读与工具修正；本单尚未完整验收。商家公平性、故障后短窗口不可用、SSE/commit 测量缺口分别保留，不以修复统计器掩盖实际业务失败。

root 已核对生产 SSE 关闭合同：服务发出 `has_active_generation_task=false` 的状态后正常关闭。测量器现以同一连接最后状态判定 EOF，避免新任务进入可变集合造成误报；无状态或最后仍 active 的关闭仍失败。收帧时刻在数据库探测前记录。30 项离线回归通过，包含正常空闲关闭后新任务已 running、active 中断及空连接；尚未运行修订后的真实 SSE 采样，旧 L2 失败不会直接改为通过。

争用阶段时间字段已按实际数据库语义改名：sent_at 在重新投递时覆盖，报告现使用 last_sent_at、accept_to_last_dispatch_ms、last_dispatch_to_provider_ms，并读回 dispatch_attempts。首次 SENT 明确标为未测，不能把重试等待全部归因于首次调度前积压。原始证据不改；新增多次投递回归后 31 项离线测试通过。此修改只纠正报告含义，首次投递及逐次重试时间仍需独立观测。
