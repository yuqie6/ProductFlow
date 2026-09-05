# 任务：自动采用并发须无死锁并留下可观察结果

状态：完成
类型：实现
认领者：主代理-0905-1442
认领于：2026-09-05T14:42:46+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：无（目标规模锁等待仍观察，不自动发 PERF-12）

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，性能门槛与后续方向见[父账本](../../performance-governance.md)。文稿采用规则仍归工作流体验。

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。已确认认领后才开始读实现、追踪锁序或改测试。

## 问题来源

父账本 PERF-01 / P0。`TestConcurrentCancelExecuteRecoveryDoesNotDeadlock` 覆盖 cancel、ExecuteRun、recovery，不覆盖自动采用，也不断言采用后 live 文稿。自动采用走 `run -> graph -> node`（`lockGraphRunAndLiveGraph` / `persistContentArtifact`）；Inspector 写入走 `lockRunningGraphRunsForUpdate` 再锁 graph。缺专门并发 Gate 时不能把 P0 标完成。

文稿采用**规则**归工作流体验（O1–O3 已有具名钉死）。本任务只补采用**过程**的锁序与结果可观察性。

## 做成什么样

`go/internal/graph` 增加并发测试：seed 文稿整图跑期间，ExecuteRun（会 auto-adopt）、CancelRun、recovery、一次 live ChangeSet（只为抢同一把 graph 锁，不得用 `document_action` 改采用规则）重叠。

完成后：

- 无 `40P01` 死锁；`409` / `ErrBusy` / `ErrLater` 可记为预期跳过。
- 每个文稿节点 origin 只允许 `seed` 或 `generated`。`generated` 时 `config_status` 为 ready，且不得留下与 O1 矛盾的半采用状态。
- 若 run 终态为 succeeded，三个文稿节点 origin 均为 `generated`（与 `TestGeneratedContentNodeRemainsReadyAfterAdopt` 同口径）。
- 若取消抢在采用前，live 可仍为 seed。不得 origin=`generated` 而 config 不可用。

默认 `-count=20`。无死锁但结果合同失败则修锁序/采用提交边界，不改 O2/O3 业务规则。

## 前置与并行

- 前置：无。
- 冻结输入：不改文稿采用规则、undo 合同、浏览器 C4。
- 运行资源：包内 `testdb`，不暂停共享 worker、不切 mock、不碰 `STORAGE_ROOT/image-evals/`。发布时允许与 canvas-c4-remainder（仅 Web）并行；不得改其占用的 `web/e2e/canvas-document-mock.spec.ts` 与 `GraphCanvasPanel.tsx`。

## 只改这些文件

- `go/internal/graph/`（并发测试；仅当测试证明锁序/提交边界失败时改 adopt / run 锁 helper）
- 本文件

父章程 PERF-01 / P0 / 验证记录由维护者在验收关闭时更新。

## 不要碰

- `web/`、Agent journal、queue、ImageSession
- 文稿 O2/O3、AR-01 版本语义、C4 浏览器用例
- tenant_id、Redis 业务权威

## 现在代码在哪

认领时现场：

- `execute_node.go:persistContentArtifact`：seed 且无显式 `document_action` 时 `lockGraphRunAndLiveGraph` 再 `adoptGeneratedDocument`。
- `runs.go:lockGraphRunAndLiveGraph`：`run -> graph`。
- `mutate.go`：先 `lockRunningGraphRunsForUpdate` 再 `loadGraphForUpdate`。
- `lock_order_test.go:TestConcurrentCancelExecuteRecoveryDoesNotDeadlock`：现有三路并发，无采用断言。
- O1 参照：`cook_contract_test.go:TestGeneratedContentNodeRemainsReadyAfterAdopt`。

## 合同

- 自动采用：`run -> graph -> node`。不改 live 时：`run -> node -> effect`。
- mutate：先锁该图 running GraphRun，再锁 graph。
- 事件 helper 不再隐式抢 run 锁。
- PostgreSQL 仍是 live/snapshot 权威。
- seed 未分叉且无 `document_action` 时 cook 后 origin=`generated`；分叉则不改 live config。

## 怎么验收

```bash
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph -count=20 -p 1 -run "TestConcurrentCancelExecuteRecoveryDoesNotDeadlock|TestConcurrentAdopt"'
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph -count=1 -p 1'
just docs-check
```

完成条件：新测试名含 Adopt；`-count=20` 无死锁；采用结果按上面断言；包回归通过。不把目标规模锁等待写成完成。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：测试进程已退出。未改共享 provider，未暂停 worker。共享工作树另有评测 Skill / 题库未提交改动，不纳入本交付。

## 证据

- 日期：2026-09-05，Asia/Shanghai。采证时 HEAD `1636be9d` 加本任务 `lock_order_test.go`。
- 实现：新增 `TestConcurrentAdoptCancelMutateDoesNotDeadlock`。seed 整图跑期间重叠 ExecuteRun、CancelRun、过期 lease recovery、一次 `rename_node` ChangeSet。`40P01` 失败；`409` / `ErrBusy` / `ErrLater` 视为跳过。文稿 origin 仅 `seed` 或 `generated`；`generated` 时 `config_status=ready`。run `succeeded` 时三个文稿节点均为 `generated`。生产 cook / adopt / mutate 锁 helper 未改。
- 命令：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph -count=20 -p 1 -run "TestConcurrentCancelExecuteRecoveryDoesNotDeadlock|TestConcurrentAdopt"'`：通过，58.757s。
- 命令：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph -count=1 -p 1'`：通过，85.024s。
- `just docs-check`：通过。`git diff --check` 对本任务文件通过。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/perf-graph-adopt-concurrent.md` 查询）。

## 验收与关闭

- 审核者：主代理-0905-1442，自审；没有独立子代理审核。
- 审核结论：并发 Gate 覆盖自动采用与只抢 graph 锁的 live ChangeSet；采用结果可观察。未改 O2/O3、C4、queue、journal。
- Issue 结果：PASS。PERF-01 保留部分完成：目标规模锁等待仍缺。P0 的 Graph 自动采用并发已补；Agent/ImageSession 专项并发与目标规模观察仍开放。不发 PERF-12。
- 关闭时更新父账本 PERF-01、P0、验证记录与组索引；移除看板行；加入归档索引。
