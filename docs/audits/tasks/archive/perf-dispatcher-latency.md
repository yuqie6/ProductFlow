# 任务：dispatcher PENDING→SENT 负载时延

状态：完成
类型：证据
认领者：主代理-0905-0352
认领于：2026-09-05T03:52:11+08:00
业务组：性能
父账本：performance-governance.md
完成后可拆：无

本任务已关闭，认领与归档流程见 [Issue 协议](../README.md)，性能门槛与后续方向见[父账本](../../performance-governance.md)。业务投递语义保持不变。

## 前置与并行

- 前置：可隔离的 PostgreSQL/Redis 与 dispatcher 测试进程。
- 冻结输入：测量期间固定 queue/dispatcher 代码、cadence 和负载参数。
- 运行资源：使用隔离 DB/Redis namespace，或独占共享服务的测量窗口；不得与同库 live、画布 worker 暂停、其他压测混跑。记录环境和其他负载。

## 做成什么样

在负载下测出 dispatch 从 `async_dispatches=PENDING` 到 `SENT` 的时延并写进本文件。现在 cadence 已拆开（投递 1s、recovery 10s），双 dispatcher SKIP LOCKED 与 SIGKILL 幸存已有现场闸门；缺的是负载时延，不是再读一遍代码。

建议初始目标：正常 Redis/PG 下 p95 < 1s。这是本地回归，不是生产 SLO。

## 只改这些文件

- `go/internal/platform/queue/` 测试
- `go/cmd/productflow-dispatcher/` 仅当测试入口需要
- 本文件

不要改 PENDING→SENT 状态机、`MaxRetry=0`、先 SENT 再 enqueue。

## 不要碰

- Graph 执行、Agent journal、ImageSession 详情
- Redis 当业务权威

## 合同

- HTTP 不入队；dispatcher 标 SENT 后再写 Redis。
- Redis 丢信封时靠 PostgreSQL 对账。
- 多 dispatcher 用 `SKIP LOCKED`，同一 PENDING 只入队一次。
- 退避未到期时被 NOTIFY 唤醒仍可跳过该行。测量时把「跳过」与「SENT」分开记。

已有锚点：`TestReplicaFieldTwoDispatchersClaimOnce`、`TestReplicaFieldDispatcherSIGKILLSurvivor`（`just go-test-staging-field`）。

## 怎么验收

新增或扩展测试：固定 N 条 PENDING，单/双 dispatcher，记录 claim→SENT 的 p50/p95。命令写入证据。

```bash
go test -C go ./internal/platform/queue -count=1 -p 1
```

完成条件：测试代码通过确定性回归；单/双 dispatcher 的有效负载报告齐全，写明 N、测量起止点、分位数和样本基线。p95 未达建议目标仍可关闭本次测量 issue，父章程保留性能缺口，由维护者决定后续因果修复。

## 证据

### 基线与方法

- 日期：2026-09-05，Asia/Shanghai。认领 `9d5196a4`，测试代码交付 `a0d1ba4f`；正式报告使用该提交的 queue/dispatcher 代码连续复跑三次，结束后确认相关目录没有新增 diff。工作树另有用户改动，不声称整树 clean。
- 环境：Linux WSL2 5.15.167.4 x86_64、Go 1.26.5、Docker PostgreSQL 16；每个场景新建并迁移独立数据库，使用独立本地 Redis 8.6.2 Unix socket 进程（关闭持久化，不复用 dev Redis 7）。同机 dev API/worker/dispatcher 和编辑会话仍运行，未独占 CPU/PostgreSQL 实例；没有其它进程读写本次测试库/Redis。
- 每次场景 N=525：同一事务通过生产 `queue.Stage` 写入 500 条可投递 PENDING 和 25 条 `available_at=当前时间+1h` 的延期 PENDING。单/双真实 dispatcher 进程均固定 `--interval 1 --recovery-interval 10 --limit 100`；逐副本确认 LISTEN 建立后才提交负载。
- PENDING→SENT 起点：全部 Stage 完成后、事务提交前最后一次 PostgreSQL `clock_timestamp()`。终点：测试库 AFTER UPDATE 触发器记录的实际 SENT 更新时间。该口径包含起始事务提交尾部、截止于 SENT 更新，不等同于 HTTP 请求耗时或严格的提交可见时间差。
- claim→SENT：同一测试触发器记录取得 lease 时刻与 SENT 更新时间之差。没有使用生产 `sent_at` 相减，因为该字段由整轮开始时刻赋值，会低估批内后续任务的等待。
- 触发器只存在于可销毁测试库；每条任务增加两次探针写入。测试每 10ms 读取探针完成情况，分位数按原始 DB 时间戳计算，不按轮询到达时刻计算。报告包含这部分测量开销，不作为无探针生产 SLO。
- 分位数采用 nearest-rank：排序后第 `ceil(q*N)` 个值。每个副本数、每次复跑单独计算，不把不同轮次混成一组样本。

### 正式结果

下表单位均为 ms，每行 500 个有效样本；25 条延期任务不计入分位数。

| 复跑 | 副本数 | PENDING→SENT p50 | PENDING→SENT p95 | claim→SENT p50 | claim→SENT p95 | p95 < 1s |
|---|---:|---:|---:|---:|---:|---|
| 1 | 1 | 688.152 | 2539.869 | 130.393 | 199.931 | FAIL |
| 1 | 2 | 398.696 | 775.003 | 136.415 | 212.227 | PASS |
| 2 | 1 | 728.390 | 2567.677 | 135.766 | 212.141 | FAIL |
| 2 | 2 | 397.765 | 772.495 | 134.803 | 210.250 | PASS |
| 3 | 1 | 747.867 | 2593.774 | 145.529 | 221.212 | FAIL |
| 3 | 2 | 402.653 | 783.593 | 139.476 | 213.246 | PASS |

- 每个场景 500/500 条 SENT，attempts=1、last_error 为空，Redis 中恰好 500 个对应信封，dispatch_id 无重复、`MaxRetry=0`。双副本三次均实际参与 claim，分配为 300/200。
- 每个场景的 25 条延期任务均保持 PENDING、attempts=0、lease/sent_at 为空，探针没有 claim/SENT 记录。它们属于未到期跳过，不是投递失败或漏样本。
- 单副本第 1/101/201/301/401 条的首个 claim 距释放时刻分别为：复跑 1 `11/279/534/1337/2336ms`；复跑 2 `10/309/579/1359/2359ms`；复跑 3 `11/293/571/1365/2365ms`。后两批存在约 1s 的认领间隔，而 claim→SENT p95 约 200–221ms。结合 `runScheduledLoop` 的通知合并、ticker 和每轮 100 条上限，下一项因果调查应检查有 backlog 时批间等待；本任务没有调整调度策略。

### 命令与产物

```bash
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/queue -count=1 -p 1 -v'
PRODUCTFLOW_RUN_DISPATCH_LATENCY=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/queue -count=3 -p 1 -run "^TestDispatcherPendingSentLatency$" -v -timeout 3m'
just docs-check
```

- 包内回归通过，5.098s。新增 `TestDispatchLatencyQuantile`、`TestDispatchLatencyProbeTracksClaimSentAndDeferred` 验证分位数、真实转移时钟、先 SENT 后 enqueue 和延期跳过。默认不运行的进程级性能采证已由下一条命令执行；既有 SIGKILL opt-in 本次未重跑。
- 三轮单/双真实进程采证通过，31.652s；这里“通过”表示采证完整、信封和状态不变量成立，单副本性能建议目标仍为 FAIL。
- 归档及链接调整后 `just docs-check`、`git diff --check` 通过。
- 原始日志及每条任务的 claim/SENT 时间戳、认领副本保存在 `storage-dev/audits/dispatcher-latency-a0d1ba4f/productflow-dispatch-latency-a0d1ba4f.log`；包内回归日志为同目录 `productflow-dispatch-latency-regression.log`。产物不提交 Git。
- 测试结束后已检查：临时 dispatcher/Redis 进程全部退出，`pf_dlat_*` / `pf_dprobe_*` 数据库全部删除；没有暂停或重启 dev 服务。

## 验收与关闭

- 审核者：主代理-0905-0352，自审；没有独立审核。
- Issue 完成条件满足：两种副本数均有三轮有效报告、原始样本和确定性回归。性能结论为单副本 FAIL、双副本在本次条件下 PASS；PERF-06 保留部分完成，不代表生产 SLO 达标。
- 代码仅改 queue 包的两个测试文件；生产状态机、cadence、`MaxRetry=0`、SENT 后 enqueue、Graph、Agent 和 ImageSession 均未修改。
- 认领提交 `9d5196a4` 意外包含另一会话当时已暂存的架构任务文档。没有重写该提交；后续交付与归档按本任务路径提交。
- 本次归档更新父账本结果、任务板、归档索引及 ROADMAP 中英文链接。没有后续自动开工项；容量指标任务尚未验收，PERF-12 不在本轮发布。
