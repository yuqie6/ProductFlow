# 任务：dispatcher PENDING→SENT 负载时延

状态：开放
认领者：—
认领于：—
父账本：performance-governance.md
完成后可拆：无

读完本文件就可以改测试/测量。认领前不要改「只改这些文件」。认领步骤见 [README.md](README.md)。不要改业务投递语义。

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

## 证据

- N=
- p50/p95=
- dispatcher 副本数=
- 日期 / commit=
