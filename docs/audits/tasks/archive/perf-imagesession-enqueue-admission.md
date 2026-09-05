# 任务：连续生图入队不再占用 admission 锁或误计 denied

状态：完成
类型：实现
认领者：主代理-0905-1515
认领于：2026-09-05T15:15:24+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：无（不自动发 tenant 分钥匙或 PERF-12）

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，容量门槛与后续方向见[父账本](../../performance-governance.md)。SaaS 分钥匙不在本任务范围。

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。已确认认领后才开始读实现、改生产路径。测试只用来钉住所修行为，不另开无生产改动的并发 Gate。

## 问题来源

父账本 PERF-04 / P1 admission，以及 [perf-capacity-metrics](perf-capacity-metrics.md) 已知缺口：`Service.Generate` 在写 queued 任务的同一事务里调用 `graph.GenerationCapacityAvailable`，忽略返回的 bool。容量满时：

- HTTP 仍把任务写成 queued 并 Stage PENDING（产品语义：入队不因 worker 忙而 409）。
- `productflow_generation_admission_denied_total{domain="imagesession"}` 仍 +1，claim 再满时会再 +1，denied 不再等于 later。
- 该事务全程持有全库 `pg_advisory_xact_lock(42630001)`，却还要 `listAssets` / `validateGeneration` / insert / outbox。章程规定 capacity advisory 只服务 claim admission，且不得在锁内做额外工作。queued 行不占 admission 槽，入队持锁不保护任何计数不变量。

admission 真正发生在 `Executor.claim`：capacity → task FOR UPDATE，满则 `waiting_for_capacity` + `ErrLater`。

## 做成什么样

`POST /api/image-sessions/{id}/generate`（`Service.Generate`）在容量已满时仍写入 queued，**不得**增加 denied，也不得为了入队去拿 generation capacity advisory。随后 claim 满容量时 denied +1，任务保持 queued 并标 `waiting_for_capacity`。

完成条件：一条行为测试先红后绿；`Generate` 事务不再调用 `GenerationCapacityAvailable`；claim 路径与双 worker 上限合同保持。不把 SaaS 分钥匙写成完成。

## 前置与并行

- 前置：无。
- 冻结输入：不改 claim 锁序 `capacity advisory -> task`、上限默认 3、Redis 权威、tenant。
- 运行资源：包内隔离 `testdb`。不暂停 worker、不切 mock、不碰 `STORAGE_ROOT/image-evals/`。评测占用 `agent-service/evals/`、`.pi/skills/`、`imageeval` 与图片池目录，本任务不写那些路径。

## 只改这些文件

- `go/internal/imagesession/`（`Generate` 入队事务；覆盖入队 vs claim 的 denied/queued 行为测试）
- 本文件

父章程 PERF-04 / P1 / 验证记录由维护者在验收关闭时更新。

## 不要碰

- `web/`、Agent journal、queue dispatcher、Graph cook/adopt
- tenant_id、把 Redis 当 admission 权威
- 图片评测阈值与池目录

## 现在代码在哪

认领时现场：

- `service.go:Generate`：`tx.WithGorm` 开头调用 `graph.GenerationCapacityAvailable`，丢弃 bool，随后 list/validate/insert/stage。
- `execute.go:claim`：先 `GenerationCapacityAvailable`，再 `FOR UPDATE` task；满则 `waiting_for_capacity` 并返回 `errWaitingCapacity`。
- 已有 `TestReplicaFieldTwoWorkersRespectGenerationCapacity` 覆盖两个 claim 的上限，不覆盖入队误计 denied。
- 指标：`metrics.ObserveGenerationAdmissionDenied` / `GenerationAdmissionDeniedCount`。

## 合同

- HTTP 不入队 broker；Generate 写 queued + PENDING。
- 容量满时 claim 返回 later，不把任务标 failed。
- 全库一把 PostgreSQL advisory + running 计数；queued 不占槽。
- denied 只在 admission 检查发现容量满时增加，且 label 仅 `graph|imagesession`。

## 怎么验收

```bash
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1 -run "TestGenerateWhenCapacityFullStillQueuesWithoutDenied|TestReplicaFieldTwoWorkersRespectGenerationCapacity"'
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1'
just docs-check
```

先写失败测试再改 `Generate`。禁止只加测试、生产路径不动。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：测试进程已退出。未改共享 provider，未暂停 worker。共享工作树另有评测未提交改动，不纳入本交付。

## 证据

- 日期：2026-09-05，Asia/Shanghai。采证时 HEAD `659a1d59` 加本任务 `imagesession` 文件。
- RED：`TestGenerateWhenCapacityFullStillQueuesWithoutDenied` 在满容量入队时 `denied before=0 after=1`。
- GREEN：`Generate` 事务删除 `graph.GenerationCapacityAvailable`；入队 denied 不变，随后 claim 才 +1 并 `waiting_for_capacity`。`service.go` 不再 import `graph`。
- 命令：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1 -run "TestGenerateWhenCapacityFullStillQueuesWithoutDenied|TestReplicaFieldTwoWorkersRespectGenerationCapacity"'`：通过，4.430s。
- 命令：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1'`：通过，9.096s。
- `just docs-check`：通过。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/perf-imagesession-enqueue-admission.md` 查询）。

## 验收与关闭

- 审核者：主代理-0905-1515，自审；没有独立子代理审核。
- 审核结论：入队不再持全库 capacity advisory，也不再误计 denied；claim 上限合同保持。未改 Graph cook、tenant、Redis。
- Issue 结果：PASS。PERF-04 保留部分完成：全库单钥匙与 noisy neighbor 仍在，SaaS 前需按 workspace/tenant 重构。
- 关闭时更新父账本 PERF-04、P1、验证记录与组索引；移除看板行；加入归档索引。
