# 任务：生图 admission 指标

状态：开放
类型：实现
认领者：—
认领于：—
业务组：性能
父账本：performance-governance.md
完成后可拆：无

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](README.md)。执行前读取适用仓库规则、当前实现、调用链、测试和 diff。容量钥匙语义与 SaaS 分租户不在本任务范围。

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

不要改 `go/internal/imagesession/`（避免和「详情有界」任务抢包）。不要改 claim 的 `run → node` 锁序。

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

- 指标名：
- 测试名：
- 日期 / commit：
