# Go / Node 后端事务与副作用审计：2026-09-08

这是直接交办的持续重构采证记录。负责人为主代理 `01a07d98-f3f6-7c32-850b-745107740339`，各切片均由同一负责人实现和自审，未委派独立审核。记录区分已交付、待验证风险和可选改善，不代表整个后端治理完成。稳定领域权威仍见 [CONTEXT](../../CONTEXT.md)，业务组风险口径见 [平台可靠性](../audits/performance-governance.md)。

## 现场与资源边界

开始于 `codex/development`，相对远端 ahead 100。图片标注任务 `category-image-annotation` 已由 `quota_policy` 持有；其 imageeval、命令、justfile 和文档改动保留。进行期间另有 Web 主题改动出现，也未纳入本次提交。没有修改环境文件、共享 provider 设置、开发服务或图片质量采证资源；没有调用付费模型、访问生产或推送远端。

现场 API/worker/dispatcher 使用 `/tmp/pf-identity-runtime-final/` 二进制，Node Agent 服务已在运行。没有重启这些服务。Go 验证经 `scripts/with_dev_env.sh` 加载环境，`platform/testdb` 使用由开发 URL 派生的 `_gotest_<package>` PostgreSQL 库；同包测试串行。新增数据库约束仅安装在测试库、仅约束当前测试生成的任务 ID，测试清理会删除约束。Provider 使用确定性实现，数据库没有 mock。

## 已交付切片

| 问题与业务损害 | 修改范围 / 规则负责人 | 验收行为 | 提交 |
|---|---|---|---|
| 局部编辑终态事务报错后 worker 返回 nil，队列误记消费；过期 Provider 任务在事务内返回冲突导致 unknown 回滚 | `localedit/execute.go` 的终态与 claim；`localedit` 拥有任务和 attempt 的一致性 | PostgreSQL 拒绝 task 或 attempt 终态写入时两者回滚，队列保留 pending；过期任务提交 unknown，旧 attempt 不覆盖新状态 | `0ccd24ac` |
| 旧局部编辑 worker 查询“最新活跃预留”，可能释放、结算或标未知另一个 attempt 的额度 | `localedit/quota_wire.go`；额度键绑定原 task + attempt | 两个真实 hold：旧 hold 已释放、新 hold 活跃；旧 worker 三种收口均不改变新 hold、账户余额或其事件 | `6e78a06d` |
| 取消任务先提交、额度后处理且吞错，取消成功后额度可能仍占用 | `localedit.Service.Cancel` 组合事务，`quota.Service` 保留额度规则 | Provider 前/后取消遇额度写失败，任务、attempt、账户一起回滚；解除故障后重复取消幂等完成 | `52810247` |
| Agent 工具调用已成功，checkpoint/编码失败却重新进入业务失败分类 | Node `withEffect` 仅解释 mutation 的错误，后处理错误原样传播 | 补强原测试先复现 `applied → failed`；修复后不追加 failed，不重复 mutation，不因后处理 503 启动业务对账 | `c385b3e8` |
| 交付失败终态写入被忽略，信封已消费而作业仍 running | `delivery.Executor.fail` 返回事务错误 | 删除测试源文件并让数据库拒绝 failed 写入，实际 `queue.Consume` 返回写错误、信封 pending、作业 running | `32448743` |
| 局部编辑成功/未知/恢复与额度收口分开提交，崩溃或写失败可形成终态与 hold 分歧 | `persistResult`、`finish`、`markUnknownLocked` 组合现有 quota 事务；恢复调用者不再携带额度后处理元组 | 成功结算或标未知失败时资产、任务、attempt、额度同步回滚；恢复故障也回滚；解除后 unknown + pending_reconciliation，重复投递不调用 Provider | `24d38931` |
| 取消先于预留完成时，旧 worker 可在取消后创建预留；阶段写失败被当作围栏失效吞掉 | `prepareProviderCall` 在同一事务中持 attempt 围栏、推进 provider_pending、Reserve | 取消先完成后不得创建 hold；Reserve 写失败保留 claimed 且不调用 Provider；数据库错误与 errAttemptFenced 分别处理 | `5ea42351` |
| Graph 复制了最新活跃预留回退，旧节点 attempt 可影响新 hold | `graph/quota_wire.go` 与 recovery 改用准确 node_run + attempt 键 | Graph 整包回归与三种旧 attempt 收口数据库回归通过；目标规模查询门跳过 | `8c5937ea` |
| Agent journal 压缩候选对每个 chunk 重复扫描该 Turn 消息，拖慢恢复扫描 | `agent/compact.go` 内层 message 与当前 event 关联，让 PostgreSQL 使用集合连接 | 约 23 万事件测试库上定位真实慢查询；5,000 未匹配 chunk 在有界时间内保留，原有压缩及 Agent 整包通过 | `c7fb2ba2` |
| 连续生图旧 worker 在条件 UPDATE 未命中后仍重排队或收口当前额度 | `imagesession.finishFailed` 先锁并核对当前 attempt，失败终态与额度同事务提交 | 原代码回归复现额外信封和当前 hold 被释放；修复后旧 attempt 无副作用，额度写失败回滚任务，重放仅一次释放事件 | 随本次提交 |

局部编辑的成功资产提交、普通终态、取消、过期未知和调用前准备分别有明确事务入口；这些入口调用 `quota.Service`，不直接改额度账户或账本表。外部 `Provider.Edit` 仍位于事务之外。失败事务中的媒体文件沿已有 compensation 回滚。没有新增状态、数据库列、并行账本或兼容读取路径。

## 已检查调用链与保留的边界

| 业务链 | 当前实现证据与结论 | 尚未证明的部分 |
|---|---|---|
| 商品与图创建 | `product.Service.CreateDirect → createWithGraph/createCanonical → graph.WriteTx`，商品、资产与图在调用者的同一事务组合；媒体沿 compensation 处理。保留 product 的创建 owner。 | 各种中断点的全组合故障注入尚未逐一覆盖；不从包测试推定整条真实商品交付签收。 |
| 图修改 | `graph.Service → WriteTx → graph command/project`；商品、配方和 Agent 通过 Graph 写入口组合，Graph 用 ProductGuard 访问商品。 | `WithProductGuard` 的隐式依赖要求多个调用者懂装配；尚无本轮越权复现。 |
| 图执行 | `ExecuteRun → executeClaimedNode → runClaimedNode → prepare/finishProviderCall → persist*Artifact` 已有 lease、attempt、effect、投影晋升分层。 | Provider 前预留、取消和恢复仍有事务外额度收口，需以数据库故障和取消竞争证明后再修改；只改文件布局没有验收价值。 |
| 连续生图 | `Execute → runGeneration → ensureEffect → Generate → saveCandidate → markEffect → finish*`；每次信封处理一个批次，已有 applied 批次跳过 Provider。 | billing sequence 绑定、部分候选已保存后的失败、effect 写失败及额度最终收口需继续审计；不能直接套用 node/attempt 的额度键规则。 |
| 局部编辑 | 本轮覆盖 HTTP 创建/提交夹具、worker、task/attempt/asset/hold/账户、恢复与 queue.Consume。 | 未进行 SIGKILL 或真实 Provider 调用；进程崩溃按可持久化边界和实际恢复函数注入验证。 |
| 交付 | 已有本地 Render、资产派生、attempt 条件写入和恢复；此次收口 worker 失败终态错误传播。 | 原图/媒体存储的各类真实 IO 故障、交付性能和全部采用流程未作本轮专项验收。 |
| Agent 控制 | Node manager 持进程内调度，TurnRuntime 持 lease/checkpoint，工具经 Go effect reconciliation；checkpoint 写失败会中止执行。保留该分工。 | Go/Node lease、journal、工具业务提交之间完整 crash 矩阵及 opt-in 重启门尚未跑完；Node 本地测试不替代 PG 持久化证据。 |

## 后续优先级

优先级按可能损害排序；修改频率与扩散范围目前只有静态调用者证据，没有生产统计。

1. **高：Graph 和连续生图的终态/额度事务分裂。** `graph.Service.CancelRun`、`graph/recovery.go` 有事务外额度调用且忽略错误；`imagesession/service.go`、`execute.go`、`quota_wire.go` 的 billing sequence 与终态组合需继续沿真实调用顺序核实。`imagesession.finishFailed` 的旧 attempt 越界已修复，成功/未知/取消/手工重试和恢复的额度边界仍待收敛。当前属于已确认的代码风险，尚未全部做数据库故障复现和修复。不得宣称所有入口已原子收口。
2. **中：局部编辑 claimed 历史预留与缺失 hold 的合同。** 当前调用前预留已原子化；仍应检查恢复前已存在的 claimed + hold 是否可安全释放，以及 `finalizeQuotaIgnoreMissing` 对真实异常与合法未预留路径的区分。不能因夹具未建 hold 就放宽生产成功合同。
3. **中：Graph context 服务依赖。** `WithProductGuard` 跨 product/recipe/agent 装配，形成编译期不可见的前置。候选方向是显式 Graph 用例依赖与已有事务入口；必须维持跨商家统一 404、事务组合及 worker 无 HTTP 商家上下文的执行合同。未实施，不把风格判断升级成安全缺陷。
4. **可选：节点执行职责与跨生成入口共享机制。** 保留三种不同的业务计费身份和 Provider 合同；只在同一规则的重复已导致漂移时抽取 owner。当前不建立统一生成框架，也不因 `execute_node.go` 较长拆文件。Graph 的 cook、效果记录、资产晋升、交付组合是后续逐边界验证对象。

## 验证入口

新增持久化证据：

- [局部编辑终态与队列](../../go/internal/localedit/terminal_persistence_test.go)：`TestTerminalPersistenceFailureDoesNotConsumeDispatch`、`TestClaimCommitsStaleProviderUnknown`、`TestFinishStaleAttemptDoesNotOverwrite`。
- [局部编辑额度](../../go/internal/localedit/quota_wire_test.go)：`TestLateAttemptCannotFinalizeNewAttemptQuota`、`TestCancelQuotaFailureRollsBackTaskAndAttempt`、`TestTerminalQuotaFailureRemainsRecoverable`、`TestProviderPreparationCannotReserveAfterCancellation`、`TestProviderPreparationQuotaFailureRollsBackBoundary`。
- [Graph attempt 额度隔离](../../go/internal/graph/quota_attempt_test.go)：`TestLateGraphAttemptCannotFinalizeNewQuota`；整包回归通过，目标规模查询计划门跳过。
- [交付终态与队列](../../go/internal/delivery/terminal_persistence_test.go)：`TestFailurePersistenceDoesNotConsumeDelivery`。
- [Agent 压缩扫描](../../go/internal/agent/compact_test.go)：`TestCompactExpiredTurnJournalsBoundsUnmatchedStream` 写入 5,000 条缺少同 attempt 完整消息的 chunk，10 秒上限内查询结束且全部保留；本次该用例 0.73 秒，不推广为生产 SLA。
- [Node 工具效果边界](../../agent-service/src/tool-effect.test.ts) 与 [实际工具包装器](../../agent-service/src/tools.test.ts)：后处理失败不得改写业务效果。

已执行局部编辑、交付、额度、queue 包回归；新增 PostgreSQL 回归均实际运行，没有跳过。Node `pnpm --dir agent-service test` 为 37 文件通过、2 文件跳过，311 测试通过、9 跳过；之后新增两条针对性用例，两份测试文件 31 测试通过。`pnpm --dir agent-service build` 和自动执行的生成合同检查通过。跳过用例不计持久化、模型质量或重启证据。

跨包验证使用 `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/product ./internal/graph ./internal/imagesession ./internal/localedit ./internal/delivery ./internal/recipe ./internal/quota ./internal/agent ./internal/providers ./internal/platform/queue -count=1 -p 1'`。它不包含正在由其他任务修改的 imageeval 包，不代表全仓库 release gate。原始整批运行在 Agent 包的压缩扫描处由负责人于 493.4 秒主动中止，保留为 FAIL；其余九个包通过。慢查询定位在 `TestCompactExpiredTurnJournalsAdvancesPastEachBatch → CompactExpiredTurnJournals`，只读 EXPLAIN 显示原查询的相关 SubPlan，候选改为事件集合 Semi Join。终止本任务测试进程后，按确切 backend PID 取消其遗留 SELECT，以释放测试 schema 检查等待；未停止开发库连接。最终 Graph 整包另跑 92.4 秒通过（目标规模查询门跳过），Agent 整包另跑 88.6 秒通过（8 个 opt-in/子进程辅助测试跳过）。不把前后不同运行的耗时比值当性能收益。

本记录截至已列切片，不关闭持续重构目标。必要的稳定架构说明需在共享活文档所有权允许后归入 ARCHITECTURE；本记录不复制另一任务未提交的活文档修改。

2026-09-08 后续切片：`TestStaleAttemptFailureCannotRequeueOrFinalizeQuota` 在原代码分别复现 1 个额外信封与 hold 被释放；`TestTerminalFailureQuotaWriteRollsBackTask` 验证额度写失败、故障解除和重复终态回放。最终 `go test ./internal/imagesession -count=1 -v`（经 dev env）整包通过，持久化回归实际执行；详见 [失败围栏回归](../../go/internal/imagesession/attempt_failure_test.go)。
