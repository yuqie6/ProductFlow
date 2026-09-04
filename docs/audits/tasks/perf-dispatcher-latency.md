# 任务：dispatcher PENDING→SENT 负载时延

状态：认领
类型：证据
认领者：主代理-0905-0352
认领于：2026-09-05T03:52:11+08:00
业务组：性能
父账本：performance-governance.md
完成后可拆：无

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](README.md)。执行前读取适用仓库规则、当前实现、调用链、测试和 diff。业务投递语义保持不变。

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

- N=
- p50/p95=
- dispatcher 副本数=
- 日期 / commit=
