# 任务：连续生图故障后给出可解释的用户可见状态

状态：完成
类型：实现
认领者：主代理-perf-0905-1948
认领于：2026-09-05T19:48:11+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：活动 Status/SSE 负载调查（章程第 2 项）由维护者核对照后再发，本单不连开

本任务已验收，随交付提交关闭；认领与关闭遵循 [Issue 协议](../README.md)。

## 问题来源

父账本「下一步如何选择」第 1 项：商家提交后必须能区分排队、执行、等待输入和终态，不能因进程退出永久停在活动状态。工作流体验组 [canvas-run-recovery-proof](canvas-run-recovery-proof.md) 覆盖画布重试入口，不覆盖 worker 丢失后的执行收敛。

连续生图已有 `RecoverUnfinished`：queued 补 PENDING；过期 running 未过 provider 边界则重排队；已打 provider 或存在 covering effect 则 `unknown` 且不可自动重试。测试用人工把 `started_at`/`progress_updated_at` 拨到 2 小时前，不构成用户可见等待上限。dispatcher 默认 `image_session_stale_running_after_minutes=90`，Graph 默认闲置 30 分钟，asynq `TaskTimeout` 30 分钟。设置文案声称按 progress heartbeat 判断闲置。这四段时间如何合成页面上的「一直在执行」，以及超时是否会杀死仍在心跳的多候选任务，尚未用同一执行链钉死。连续生图没有 parked question/approval，该边界记不适用。

## 做成什么样

选择连续生图执行链，把故障点映射到用户可重新读取的状态，并给出等待上限依据：

1. 未提交 / 未过 provider 边界：worker 丢失后应变回 `queued` 并补 dispatch，不得留下无 worker 的 `running`。
2. 已提交副作用：已 `applied` 的 candidate 不得因恢复再写一次业务效果。
3. provider 结果不可证明：保持 `unknown`，`is_retryable=false`，不得当失败自动重放。
4. 等待用户：本链无 question/approval，证据中写明不适用。

等待上限必须能用「最后一次 progress heartbeat + 闲置阈值 + recovery cadence/批次」解释，不能把 90 分钟闲置、30 分钟信封超时和 10 秒扫描混成同一个数。若证明 asynq 超时会打断仍在心跳的合法任务，或超时后、闲置前存在无合同的僵尸 `running`，在本单按因果点收口，并补晚到 writer / 心跳未过期不得恢复的回归。测量或对账也可以结论为保持现行 90 分钟闲置，但必须写出商家可见等待和未覆盖边界。

## 前置与并行

- 前置：无新组外输入。`imagesession/recovery_test.go`、Graph `defaultStaleRunningAfter=30m`、`queue.TaskTimeout=30m` 已存在，本单消费现行代码。
- 冻结输入：不改 provider 绑定、生图模型或共享 `app_settings` 生产值。测量与回归使用隔离库和测试内阈值。
- 运行资源：仅隔离 `testdb` / httptest；不重建共享 dev DB，不调用真实 provider，不停 `just dev` 的 API/worker/dispatcher，不占图片采集浏览器或 inbox/pool。原始日志目录 `/tmp/productflow-perf-imagesession-recovery-visible-0905/`。
- 占用核对：`eval-development-baseline` 使用独立 checkout 与 STORAGE_ROOT；`image-eval-pool` 占用共享 API/worker 与真实 provider，本单不与之争用；`eval-skills` 阻塞且冻结 `agent-service/` 与 Skill，本单不改这些路径。看板 README 已有其他组未提交行，提交时只暂存本任务 hunk。

## 只改这些文件

- `go/internal/imagesession/execute.go`、`provider.go`、`recovery.go` 及对应测试
- `go/cmd/productflow-dispatcher/main.go`：闲置默认改读 `DefaultStaleRunningAfter`，数值仍为 90 分钟
- `go/internal/settings/catalog.go`：设置说明与等待合同对齐
- `docs/ARCHITECTURE.md`、`docs/ARCHITECTURE.en.md`
- 本文件、父账本、归档索引

未改 `platform/queue` 的全局 `TaskTimeout`，未改 Web / imageeval / 共享 provider。

## 不要碰

- Agent journal/Turn、Graph 文稿采用语义、delivery/localedit 恢复策略
- Web 页面、imageeval、评测题目/grader、真实 provider 设置
- 共享 dev 数据库、图片池目录、其他任务未提交 diff
- 不为限制返回数量丢掉活动任务，不引入缓存或兼容层来掩盖等待合同

## 现在代码在哪

- `go/internal/imagesession/recovery.go`：`COALESCE(progress_updated_at, started_at)` 与 `DefaultStaleRunningAfter`（90 分钟）比较；批次 25。
- `go/cmd/productflow-dispatcher/main.go`：recovery 间隔 10s；`imageStale` 读设置，默认与 `DefaultStaleRunningAfter` 相同。
- `go/internal/platform/queue/asynq.go`、`actors.go`：`TaskTimeout=30m`，consumer lease 再加 5 分钟。
- `go/internal/graph/recovery.go`：`defaultStaleRunningAfter=30m`。
- `go/internal/imagesession/execute.go`：claim 后写 progress heartbeat；provider 调用在业务行锁外；handler ctx 取消后用 `persistContext` 写终态。
- `go/internal/imagesession/recovery_test.go`、`execute_test.go`：unknown / 不重放 applied / 无 provider 边界则重排队 / 心跳未过期 / 默认 90 分钟 / 晚到 writer / 取消 ctx。
- 设置定义：`go/internal/settings/catalog.go` 的 `image_session_stale_running_after_minutes`。

## 合同

- PostgreSQL 是业务权威；asynq 信封超时不等于已经恢复。
- 不可证明的供应商结果保持 `unknown`；`conflict/unknown` 不自动重放。
- 已 `applied` 的 candidate 不得第二次成为业务副作用。
- worker 丢失执行权后不得继续落状态或产物；晚到 writer 必须被拒绝。
- 不在持有业务行锁或 capacity advisory 的事务里调用 provider。
- 列表/详情/SSE 仍须返回必要活动任务；本单不改 Status 活动集截断。
- 父账本锁与时间预算：dispatcher recovery 10s；连续生图闲置阈值是设置项，不是 HTTP SLO。

## 怎么验收

- 新增或扩展真实 PG 回归：上述 1–3 的用户可见状态、心跳未过期不得恢复、晚到 writer 拒绝；适用时覆盖 asynq/闲置阈值关系。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1'` 及受影响包（queue/dispatcher，仅当改到）通过；涉及并发的恢复测试按场景 `-race` 或 `-count` 重复。
- 不调用真实 provider；默认跳过的 opt-in 不冒充本单通过。
- `just docs-check` 与本任务 `git diff --check` 通过。
- 父账本 PERF-05/11 连续生图条目写下等待上限、有效证据和未覆盖边界。缺少状态映射或有效回归不得关闭。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：主代理-perf-0905-1948
- 交接：测试进程已退出。隔离库由 `testdb` 清理。未调用真实 provider，未停共享 API/worker/dispatcher，未改图片池或其他任务 diff。

## 证据

- 日期：2026-09-05，Asia/Shanghai。基线工作树 HEAD `13c54b4e` 加本任务 diff；共享工作树含其他组 WIP，不声明整树验收。
- 根因：asynq `TaskTimeout` 取消 handler `context` 后，`tx.WithGorm(ctx)` 无法把 `unknown` 写入 PostgreSQL，任务保持 `running` 直到 90 分钟闲置恢复。供应商 HTTP 客户端上限 15 分钟；心跳在 candidate 边界更新，健康单次调用不会触达 90 分钟闲置。
- 实现：`Execute` / `recordGenerateFailure` 用 `context.WithoutCancel` + 5s 超时写终态；`IsUncertainProviderFailure` 包含 `context.Canceled` / `DeadlineExceeded`。闲置默认仍 90 分钟，抽成 `DefaultStaleRunningAfter`，dispatcher 与设置说明与之对齐。未改全局 `TaskTimeout`。
- 用户可见映射：未过 provider 边界 → `queued` + `requeued_after_idle`；已 applied covering → `unknown` 且 effect 仍为 applied；不可证明 / ctx 取消 → `unknown` 且 `is_retryable=false`；parked question/approval 不适用。
- 等待上限：进程崩溃后为最后一次 progress heartbeat + 90 分钟 + recovery 10s 与 25 条批次。asynq 30 分钟墙钟取消后，worker 立即写 `unknown`，不等待闲置阈值。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1'`：PASS，7.454s。日志 `/tmp/productflow-perf-imagesession-recovery-visible-0905/package.log`。
- 同包装 ` -race -count=1 -run TestRecoverUnfinished|TestExecuteCanceled|TestExecuteTimeout|TestIsUncertainProviderFailure`：PASS，2.901s。`settings` 与 `productflow-dispatcher` 包测 PASS。新增四项 `-count=10` PASS。
- `just docs-check` 与本任务 `git diff --check`：PASS。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/perf-imagesession-recovery-visible.md` 查询）。
- 审核者 / 结论：主代理-perf-0905-1948 自审，无独立子代理审核。复核完整 diff：因果点在取消 ctx 后的终态写入；晚到 writer 与心跳合同有回归；未改 queue 全局超时、未截断活动任务、未碰其他任务文件。
- Issue 结果：PASS。业务门槛：连续生图故障可见状态已可解释；未宣称组完成或生产 SLO。剩余缺口：顺序多候选（非 `openai-images` 批量）仍可能在 30 分钟墙钟被写成 unknown；`Consume` 的信封 CONSUMED 仍用原 ctx；Graph/Agent 链的用户可见等待未在本单展开。
