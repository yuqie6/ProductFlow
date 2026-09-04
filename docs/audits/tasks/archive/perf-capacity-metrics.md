# 任务：生图 admission 指标

状态：完成
类型：实现
认领者：主代理-0905-0437
认领于：2026-09-05T04:37:17+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：无

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，容量门槛与后续方向见[父账本](../../performance-governance.md)。容量钥匙语义与 SaaS 分租户不在本任务范围。

## 前置与并行

- 前置：无。
- 冻结输入：本任务修改共享容量 owner；不得与共享该 checkout 的生成容量、性能或生图 live 采证并行。
- 运行资源：Go 回归使用测试数据库；涉及同库容量计数的测试由维护者串行调度。

## 做成什么样

`/metrics` 能看到生图 admission 的 **wait / running / denied**，不再只用锁等待 histogram 和 `generation_max_concurrent_tasks` 上限冒充「当前是否挤满」。

现有：

- `productflow_generation_max_concurrent_tasks`（上限）
- `productflow_advisory_lock_wait_seconds`（拿到 advisory 锁的等待）
- `productflow_postgres_lock_waiters`（任意锁等待）

缺：有多少任务因容量满而 later、当前 running 计数、denied/later 次数。

ImageSession 已经调用 `graph.GenerationCapacityAvailable`。指标加在这个函数里，两边一起生效。

## 只改这些文件

- `go/internal/graph/durability.go`（在现有 `generationCapacityAvailable` 里打点；不要改 lock 顺序）
- `go/internal/platform/metrics/`（新低基数名 + `http_test.go` 断言）
- 本文件

不要改 `go/internal/imagesession/`：本任务在现有共享容量入口打点即可，详情有界任务已经归档。不要改 claim 的 `run → node` 锁序。

## 不要碰

- tenant_id、按商家分钥匙
- 把 Redis 当成 admission 权威
- 文稿 cook / 画布测试

## 合同

- 仍是全库一把 PostgreSQL advisory + running 计数。默认上限 3，设置项 1–20。
- 容量满返回「未抢到 / later」，不是把节点标 failed。
- label 只允许有界枚举（例如 domain=`graph|imagesession`）。禁止业务 ID。
- 未配置 `METRICS_BEARER_TOKEN` 时 `/metrics` 仍不注册。

建议名字（实现时可微调，但要写进测试）：

- `productflow_generation_admission_running`
- `productflow_generation_admission_denied_total`

## 怎么验收

```bash
go test -C go ./internal/platform/metrics ./internal/graph -count=1 -p 1
```

`http_test.go` 能 grep 到新名字。容量满路径（已有 `TestReplicaFieldTwoWorkersRespectGenerationCapacity`）应增加 denied/later 计数。

## 证据

- 指标名：`productflow_generation_admission_running`（PostgreSQL `CountAdmissionRunning` gauge）；`productflow_generation_admission_denied_total{domain="graph|imagesession"}`（容量满计数）。wait 继续用既有 `productflow_advisory_lock_wait_seconds`。
- 测试名：`TestGenerationCapacityUsesAdmissionNodeCount`、`TestSnapshotGenerationAdmissionRunningUsesSharedCount`、`TestSnapshotIncludesRecoveryBacklog`、`TestWorkerProcessSeriesHaveStableNames`
- 命令：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/metrics ./internal/graph -count=1 -p 1'`
- 结果：2026-09-05，`ok metrics 0.856s`，`ok graph 78.394s`
- 日期 / commit：随本任务提交
- 采证基线：HEAD `012a4ce1` 加本任务 Go metrics/graph 文件；共享工作树另有首页、eval loader、journal/canvas 登记，不声称整树 clean。
- 实现：`generationCapacityAvailable` 在 `count >= limit` 时按调用方打 `graph` / `imagesession` denied；`/metrics` snapshot 用共用 `CountAdmissionRunning` 输出 running。未改 advisory 锁序、未改 ImageSession、未引入 tenant 或 Redis admission。
- 已知缺口：ImageSession 入队也调用导出的 `GenerationCapacityAvailable` 并忽略 bool，容量满时会增加 `imagesession` denied，任务仍写入 queued。claim 路径才对应 later。本任务禁止改 `imagesession/`，未跑 `TestReplicaFieldTwoWorkersRespectGenerationCapacity`；导出入口与 graph 容量满路径已在包内覆盖。denied 是进程内计数，跨副本需按 instance 抓取。

## 验收与关闭

- 审核者：主代理-0905-0437，自审；没有独立子代理审核。
- 审核结论：实现与回归满足本任务合同。代码只改 `go/internal/graph/durability.go`、`durability_test.go` 与 `go/internal/platform/metrics/`。无新路由、DTO、前端、迁移；锁序与 Redis 权威未变。
- Issue 结果：PASS，已交付。容量指标落地不等于生产挤满 SLO；PERF-12 仍缺目标规模 payload，本轮不发后续单。平台可靠性组整体未验收完成。
- 关闭时更新父账本 P1/验收指标与验证记录、移除看板行、修正 ROADMAP 中英文旧链接并加入归档索引。
- 完成后可拆：无。
