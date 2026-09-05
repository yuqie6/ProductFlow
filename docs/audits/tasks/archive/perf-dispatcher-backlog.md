# 任务：dispatcher 积压时批间不再空等 interval

状态：完成
类型：实现
认领者：主代理-0905-1428
认领于：2026-09-05T14:28:34+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：无（达标后不自动发生产 SLO、重 recovery 负载或 PERF-12）

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，性能门槛与后续方向见[父账本](../../performance-governance.md)。业务投递语义保持不变。

## 问题来源

父账本 PERF-06。证据任务 [perf-dispatcher-latency](perf-dispatcher-latency.md) 已关闭：独立 PG/Redis、500 条可投递 PENDING + 25 条延期，单副本三轮 PENDING→SENT p95 2.540–2.594s，未达建议 p95 < 1s；claim→SENT p95 约 200–221ms。单副本第 301/401 条认领距释放约 1s，后两批空等与每轮 100 条上限、`runScheduledLoop` 通知合并和 ticker 同向。本任务修积压时的批间等待，不重做那次测量方法学。

## 做成什么样

watch 模式下一轮 claim 已满 `limit`、库里仍有到期 PENDING 时，下一轮 claim 不得再空等整个 `--interval`（默认 1s）。空闲时仍可按 interval / NOTIFY 等待。延期 `available_at` 未到的行继续跳过，不提前投递。

完成条件：确定性回归证明满批后续投；用现有 `TestDispatcherPendingSentLatency` 复跑单副本 500 条，PENDING→SENT p95 < 1s。双副本不得出现重复信封。p95 仍 FAIL 则本 issue 不得标完成，把原因和未改合同写回证据，保持认领或交回阻塞。

## 前置与并行

- 前置：[perf-dispatcher-latency](perf-dispatcher-latency.md) 已交付测量口径与 `PRODUCTFLOW_RUN_DISPATCH_LATENCY=1` 入口。
- 冻结输入：测量与修复期间固定 queue/dispatcher 投递语义、cadence 默认值和 `DefaultClaimLimit=100`；不得靠加大 `limit` 或缩短默认 interval 冒充达标。
- 运行资源：Go 包测使用测试库。负载复跑使用隔离 DB/Redis namespace，或独占测量窗口；不得与同库 live、画布 worker 暂停、其他压测混跑。记录环境。不得改共享 provider 设置。

## 只改这些文件

- `go/cmd/productflow-dispatcher/`（watch 调度；满批后续投，recovery 仍走独立 cadence）
- `go/internal/platform/queue/`（一轮 claim 是否还有到期 backlog 的返回值；既有延迟测试与包内回归）
- 本文件

父章程 PERF-06 / P1 / 验证记录由维护者在验收关闭时更新。

## 不要碰

- PENDING→SENT 状态机、`MaxRetry=0`、先 SENT 再 enqueue
- 把 Redis 当业务权威
- Graph 执行、Agent journal、ImageSession、recovery 批次上限
- 默认 `--interval` / `--recovery-interval` / `--limit` 的产品默认值
- tenant / SaaS 队列公平性

## 现在代码在哪

认领时现场：

- `queue.RunDispatcherOnce`：对账后 `claimPending` 最多 `limit` 条，然后逐条 `sendClaimed`；`Summary` 没有满批 hint。
- `claimPending`：`SKIP LOCKED`，`available_at <= now`，稳定 `available_at, id` 排序。
- `productflow-dispatcher` watch：`runWatchLoops` 里 dispatch 与 recovery 分 loop；dispatch 的 `runScheduledLoop` 在 ticker 或 size-1 合并 wake 上再跑一轮。`runDispatchCycle` 每轮只调一次 `RunDispatcherOnce`。
- 已有证据：`go/internal/platform/queue/dispatch_latency_test.go` 的 `TestDispatcherPendingSentLatency`；调度单测在 `go/cmd/productflow-dispatcher/coordinator_test.go`。

## 合同

- HTTP 不入队；dispatcher 标 SENT 后再写 Redis。
- Redis 丢信封时靠 PostgreSQL 对账。
- 多 dispatcher 用 `SKIP LOCKED`，同一 PENDING 只入队一次。
- 退避未到期时被 NOTIFY 唤醒仍可跳过该行。
- recovery 失败不得挡住已提交 PENDING 的投递；dispatch 满批续跑不得改 recovery 的 10s cadence，也不得把 recovery 重新绑回 dispatch goroutine。
- `has_more` 类 hint 只表示本轮批次已满、下一轮继续探测，不是精确 backlog 计数。

## 怎么验收

```bash
go test -C go ./internal/platform/queue ./cmd/productflow-dispatcher -count=1 -p 1
PRODUCTFLOW_RUN_DISPATCH_LATENCY=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/queue -count=1 -p 1 -run "^TestDispatcherPendingSentLatency$" -v -timeout 3m'
just docs-check
```

1. 新增或扩展确定性测试：满 `limit` 后仍有到期 PENDING 时，不等待 dispatch interval 就开始下一轮 claim。空闲循环仍等待。
2. 包内 queue / dispatcher 回归通过，含既有 SKIP LOCKED、延期跳过、先 SENT 再 enqueue。
3. 负载命令至少单副本 500 条有效样本；报告 p50/p95、测量起止点与是否 < 1s。双副本若跑了，核对无重复信封。
4. 不把一次 p95 写成生产 SLO。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：测试进程已退出。隔离 `pf_dlat_*` 库由测试 Cleanup 删除。没有改共享 provider、没有暂停 dev worker。共享工作树另有评测/画布未提交改动，不纳入本交付。

## 证据

- 日期：2026-09-05，Asia/Shanghai。采证基线 HEAD `f7f7213d` 加本任务 queue/dispatcher 文件。
- 实现：`Summary.HasMore` 在 `len(claimed)==limit` 时为 true。watch 用 `dispatchWhileHasMore` 在满批后立即再跑 `RunDispatcherOnce`，不空等 `--interval`；one-shot 仍只跑一轮。同一轮已 claim 行按 `sendClaimedConcurrency=16` 并发 SENT+enqueue，每条仍先 SENT 再 enqueue。未改默认 `--interval` / `--recovery-interval` / `--limit`、`MaxRetry=0` 或 PENDING→SENT 状态机。
- 确定性测试：`TestRunDispatcherOnceHasMoreWhenClaimFillsLimit`、`TestDispatchWhileHasMoreContinuesUntilCaughtUp`、`TestDispatchWhileHasMoreStopsOnError`、`TestDispatchWhileHasMoreStopsOnCancel`、`TestRunWatchLoopsDispatchDrainsBacklogBeforeTicker`。
- 命令：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/queue ./cmd/productflow-dispatcher -count=1 -p 1'`：queue 4.724s，dispatcher 0.764s，通过。
- 命令：`go test ... -race -run 'TestRunDispatcherOnceHasMore|TestDispatchLatencyProbe|TestReplicaFieldTwoDispatchers|TestEnqueueFailure|TestDispatchWhileHasMore|TestRunWatchLoopsDispatchDrains|TestReconcileStaleSent'`：通过。
- 命令：`PRODUCTFLOW_RUN_DISPATCH_LATENCY=1 ... -run "^TestDispatcherPendingSentLatency$" -count=1`：通过，8.715s。隔离 PG 库 + 本地 Redis Unix socket；`--interval 1 --recovery-interval 10 --limit 100`。口径同归档采证任务：提交前 `clock_timestamp()` 到 SENT 触发器。

| 副本数 | PENDING→SENT p50 | PENDING→SENT p95 | claim→SENT p50 | claim→SENT p95 | p95 < 1s | 认领分配 |
|---:|---:|---:|---:|---:|---|---|
| 1 | 259.834ms | 437.666ms | 42.566ms | 63.33ms | PASS | 500 |
| 2 | 185.175ms | 278.819ms | 48.286ms | 70.853ms | PASS | 200 / 300 |

- 每场 500/500 SENT，25 条延期未认领；双副本无重复信封。只跑一轮，不是生产 SLO。
- `just docs-check`：通过。`git diff --check` 对本任务文件通过。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/perf-dispatcher-backlog.md` 查询）。

## 验收与关闭

- 审核者：主代理-0905-1428，自审；没有独立子代理审核。
- 审核结论：满批后续投与有界并发 SENT 满足本任务合同。代码限于 `go/cmd/productflow-dispatcher/` 与 `go/internal/platform/queue/`。未改 Graph、Agent、ImageSession、默认 cadence/`limit`。
- Issue 结果：PASS。本地 500 条突发单副本 p95 从约 2.54s 降到 0.438s。PERF-06 保留部分完成：无生产 SLO、无重 recovery 负载、`just staging-up` 杀容器未跑；未到期唤醒仍跳过。
- 关闭时更新父账本 PERF-06、P1、验证记录与 ARCHITECTURE 异步节；移除看板行；加入归档索引。
