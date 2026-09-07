# Go / Node 后端事务与副作用审计：2026-09-08

这是直接交办的持续重构采证记录。负责人为主代理 `01a07d98-f3f6-7c32-850b-745107740339`，各切片均由同一负责人实现和自审，未委派独立审核。记录区分已交付、待验证风险和可选改善，不代表整个后端治理完成。稳定领域权威仍见 [CONTEXT](../../CONTEXT.md)，业务组风险口径见 [平台可靠性](../audits/performance-governance.md)。

## 现场与资源边界

开始于 `codex/development`，相对远端 ahead 100。图片标注任务 `category-image-annotation` 已由 `quota_policy` 持有；其 imageeval、命令、justfile 和文档改动保留。进行期间另有 Web 主题改动出现，也未纳入本次提交。没有修改环境文件、共享 provider 设置、开发服务或图片质量采证资源；没有调用付费模型、访问生产或推送远端。

现场 API/worker/dispatcher 使用 `/tmp/pf-identity-runtime-final/` 二进制，Node Agent 服务已在运行。没有重启这些服务。Go 验证经 `scripts/with_dev_env.sh` 加载环境，`platform/testdb` 使用由开发 URL 派生的 `_gotest_<package>` PostgreSQL 库；同包测试串行。新增数据库约束仅安装在测试库，通常约束当前任务 ID；连续生图命令的队列故障约束仅在该包独占测试期间拒绝新增生成信封，测试清理会删除约束。Provider 使用确定性实现，数据库没有 mock。

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
| 连续生图旧 worker 在条件 UPDATE 未命中后仍重排队或收口当前额度 | `imagesession.finishFailed` 先锁并核对当前 attempt，失败终态与额度同事务提交 | 原代码回归复现额外信封和当前 hold 被释放；修复后旧 attempt 无副作用，额度写失败回滚任务，重放仅一次释放事件 | `bc0a52c3` |
| 连续生图成功/未知先提交任务再收口额度，过期未知未同步 hold | `finishSucceeded`、`finishUnknown` 和 `recoverImageTaskState` 在当前任务事务调用 quota owner | success/unknown/recovery 三种额度写故障均保留 running 与当前 attempt；解除后任务与 hold 同步终态，重放不重复额度事件 | `6935a107` |
| 连续生图创建/重试分开预留额度，失败依靠事后释放；取消提交后吞掉额度错误 | `imagesession.Service.Generate/Retry/Cancel` 负责组合事务；Retry/Cancel 锁定任务后判断状态；删除 Service 的补偿释放路径 | 队列约束失败回滚任务、hold 和余额；取消额度失败保留 queued；解除故障可重放；两个并发 Retry 仅一个成功且预留保持 reserved | `1f9af348` |
| Graph HTTP 取消在事务外忽略额度失败，Agent 的 CancelRunTx 完全漏掉额度处理 | `cancelGraphRun` 持 run/node 锁后处理准确 attempt 的额度；两个 Service 入口共享命令 | 两种入口、Provider 前后四条真实数据库约束回归；失败时运行/节点/额度/取消事件回滚，AfterRunStatus 不发生，解除后重复取消幂等 | `adfe06ce` |
| Graph 恢复先提交 queued/unknown，再处理额度且吞掉写错误 | `recoverGraphRunState` 在恢复事务中使用原额度 owner，删除事务外动作列表并复用商家查询 | claimed/provider_call 两分支原代码均复现吞错；额度失败保留运行和当前 attempt、余额与事件，解除后重排队或标未知，重放仅一次额度事件 | `c475faab` |
| Graph 图像调用先 Reserve 再检查 attempt，取消后的旧 worker 可创建 hold 并依赖事后补偿 | `callImageProvider` 在一个事务中组合既有 prepare 与 Reserve，Provider 调用在提交之后 | 原代码复现取消后新增 hold；修复后无 hold，额度故障回滚阶段和 effect intent；外部调用期间另一连接可锁运行行 | `0d33ca14` |
| Graph Provider 错误与节点/运行失败可提交 unknown 而未在同事务处理额度 | `markNodeUnknown` 持锁确认当前 attempt 后同步额度，Provider 与恢复删除重复后处理 | 三条入口原代码复现额度故障未阻止 unknown；修复后保留运行/节点/attempt，解除故障后重放仅一次 mark_unknown 事件 | `635c9915` |
| Graph 图像资产/成功投影提交后再结算，结算失败仍留下可见成功产物 | `persistImageArtifact` 在原资产事务中结算，删除事务外 consumed 收口 | 原代码真实执行复现 1 artifact + 1 商品资产残留；修复后结算故障回滚产物、保留 unknown/待核对，Provider applied 与持久化失败原因仍可查，重投不再调用 Provider | `c137516d` |
| Graph 明确失败命令先提交终态，节点执行器才补做额度，运行失败入口遗漏预留 | `failClaimedNode/failGraphRunLocked` 在锁内释放原 attempt 预留，删除执行器后处理与重复分类 helper | 两种入口原代码复现额度故障不阻止 failed；修复后故障保留当前运行/节点/attempt，解除后释放幂等；未知分支仍使用 markNodeUnknown | `16c99e42` |
| Graph 成功结算忽略缺失 hold，付费异常与合法免费合成混在一起 | 复用 subjectPreserveImageDelivery，资产入口对本地合成显式跳过结算；付费 settle 原样返回缺失预留 | 原代码缺预留仍接受付费结算；修复后拒绝，真实本地合成不调用 Provider、持久化产物且余额不变，正常付费结算仍通过 | `1d56674d` |
| 连续生图 effect 完成只按任务/批次写入，旧 worker 结果没有 attempt 围栏 | `markEffect` 显式接收 attempt，锁任务并核对当前执行，再按 effect attempt 更新 | failed/unknown/applied 三种旧结果均被拒绝且新 effect 保持 pending；原实现三种写入均接受 | `e4591471` |
| 连续生图 effect 写失败被吞掉或作为业务失败自动重试，队列可能消费未完成持久化 | effectPersistenceError 保留底层错误，Execute 将其返回队列；各结果分支检查写入结果，Provider 错误仅分类一次再统一写账本 | 确认失败/未知/applied 三种 PostgreSQL 约束故障原代码均被消费；修复后 SQLSTATE 23514 透传、任务 running、信封 pending，恢复不重复 Provider | `18ad035b` |
| 连续生图恢复删除已保存结果的 pending effect，仅凭 completed 计数可安全重排队 | 恢复事务用同会话/同生成组完整轮次与资产证明批次完成，保留 effect 并标 applied；证据不足收敛 unknown | 原代码复现恢复后 effect 丢失及无轮次仍重排队；修复后调用记录保留，恢复 applied 写失败回滚任务，缺证据不再重投 | `bcd0fce5` |
| 连续生图接管 pending/忽略 unknown，重试 failed 时未重置结果与 Provider/hash | `ensureEffect` 只允许确认失败重新准备；未决 intent 保留原身份转 unknown，applied 复用 | 原实现三种缺陷复现；新调用 pending 元数据与请求一致，未决返回 unknown 且保留原 request/attempt，applied 批次数不变 | `4e17ba26` |
| 连续生图先提交 candidate_started，再插入调用 intent；插入失败仍被恢复判为调用未知 | `ensureEffect` 持锁事务同时写 intent、候选阶段和通知；删除独立 markCandidateStarted | 原代码复现 Provider 零调用却留下活动候选；intent 或阶段写失败均无残留 intent、保持 running，解除故障后安全重排队且仅调用一次 Provider | `58791772` |
| Graph 提案 Service 未装配自身 Products，直接调用 Internal，Agent 调用者被迫理解 context 注入 | CreateAgentProposal 在 Service 入口使用已有依赖；删除 Agent 手动注入 | 原代码正常商家与跨商家均复现缺守卫；修复后提案落库且 live 图不变、跨商家 NotFound 且零写入、数据库插入故障原样返回且无部分修改 | `d27b6a66` |
| Graph 图像/文稿 Provider 错误只留下统一 unknown 文案，原失败原因丢失 | 两种调用入口使用既有节点错误解释规则，将原因送入 markUnknownCommitted/markNodeUnknown | 原代码两个入口均复现原因丢失；修复后 run/node/effect 仍 unknown，节点与 effect 原因含调用失败细节，额度故障回滚语义不变 | `4216e3b0` |
| 真实图像适配器和文稿 JSON 解码将原错误替换为无原因 unknown，导致 Graph 终态仍丢失证据 | providers 保留 Graph 分类和底层错误链；Graph 避免重复未知提示 | 四种适配错误和 JSON SyntaxError 原代码均复现；真实适配器经 Graph 执行后数据库保留 trace，重复投递不再调用 Provider | `d1e194c2` |
| Graph effect 只记执行身份元数据，无法核对调用时的 typed request 与参考图字节 | callProvider/callImageProvider 持有同一个请求并直接传 Provider 方法；原 effect 记录完整请求字段、参考图元数据与 SHA-256 | Provider 内读取真实数据库，记录与收到的请求一致；256 KiB 参考图只存摘要，超限请求不写 intent/不占额度/不调用 Provider | `2610f2b0` |
| 局部编辑成功结算容忍缺失 hold，可提交未结算资产及 succeeded | settleEditQuota 对唯一付费成功路径返回原额度错误；取消和未调用释放不扩改 | 原代码真实数据库复现缺 hold 仍成功；修复后资产/任务/attempt 回滚，补足原 attempt 预留后成功结算 | `5deb31e2` |
| 连续生图活跃 hold 查询失败退回初始键，原数据库错误丢失甚至被后续 NotFound 容忍吞掉 | 三种 finalizer 直接消费 activeQuotaKey 的错误，删除 mustActiveQuotaKey，保留底层数据库 cause | 独立 PostgreSQL 关系不可用时三条入口透传 SQLSTATE 42P01；真实预留与余额不变 | 随本次提交 |

局部编辑的成功资产提交、普通终态、取消、过期未知和调用前准备分别有明确事务入口；这些入口调用 `quota.Service`，不直接改额度账户或账本表。外部 `Provider.Edit` 仍位于事务之外。失败事务中的媒体文件沿已有 compensation 回滚。没有新增状态、数据库列、并行账本或兼容读取路径。

## 已检查调用链与保留的边界

| 业务链 | 当前实现证据与结论 | 尚未证明的部分 |
|---|---|---|
| 商品与图创建 | `product.Service.CreateDirect → createWithGraph/createCanonical → graph.WriteTx`，商品、资产与图在调用者的同一事务组合；媒体沿 compensation 处理。保留 product 的创建 owner。 | 各种中断点的全组合故障注入尚未逐一覆盖；不从包测试推定整条真实商品交付签收。 |
| 图修改 | `graph.Service → WriteTx → graph command/project`；商品、配方和 Agent 通过 Graph 写入口组合，Graph 用 ProductGuard 访问商品。 | `WithProductGuard` 的隐式依赖要求多个调用者懂装配；尚无本轮越权复现。 |
| 图执行 | `ExecuteRun → executeClaimedNode → runClaimedNode → prepare/finishProviderCall → persist*Artifact` 已有 lease、attempt、effect、投影晋升分层。 | 取消已在持锁命令收口；过期恢复已在同一事务收口；图像调用前 prepare/Reserve 已组合事务；成功已与结算同事务，明确失败终态也已在命令内处理；付费成功结算已要求 hold，本地合成显式跳过结算；其他终态的缺失 hold 合同仍需核实，需以数据库故障和竞争证明后修改；只改文件布局没有验收价值。 |
| 连续生图 | `Execute → runGeneration → ensureEffect → Generate → saveCandidate → markEffect → finish*`；每次信封处理一个批次，已有 applied 批次跳过 Provider。 | billing sequence 绑定、部分候选已保存后的失败、effect 写失败及额度最终收口需继续审计；不能直接套用 node/attempt 的额度键规则。 |
| 局部编辑 | 本轮覆盖 HTTP 创建/提交夹具、worker、task/attempt/asset/hold/账户、恢复与 queue.Consume。 | 未进行 SIGKILL 或真实 Provider 调用；进程崩溃按可持久化边界和实际恢复函数注入验证。 |
| 交付 | 已有本地 Render、资产派生、attempt 条件写入和恢复；此次收口 worker 失败终态错误传播。 | 原图/媒体存储的各类真实 IO 故障、交付性能和全部采用流程未作本轮专项验收。 |
| Agent 控制 | Node manager 持进程内调度，TurnRuntime 持 lease/checkpoint，工具经 Go effect reconciliation；checkpoint 写失败会中止执行。保留该分工。 | Go/Node lease、journal、工具业务提交之间完整 crash 矩阵及 opt-in 重启门尚未跑完；Node 本地测试不替代 PG 持久化证据。 |

## 后续优先级

优先级按可能损害排序；修改频率与扩散范围目前只有静态调用者证据，没有生产统计。

1. **高：Graph 和连续生图的终态/额度事务分裂。** Graph 取消已归入持锁命令，Graph 过期恢复已同步额度；图像成功持久化已与结算同事务；明确失败终态额度已归入命令，付费成功结算已要求 hold；取消/未知/释放的缺失 hold 合同仍待核实；`imagesession/service.go`、`execute.go`、`quota_wire.go` 的 billing sequence 与终态组合需继续沿真实调用顺序核实。`imagesession.finishFailed` 的旧 attempt 越界已修复，成功/未知/过期恢复的额度事务已收敛，创建、取消和手工重试已改为用例内组合事务；billing sequence 的精确绑定和缺失 hold 处理仍待核实。当前属于已确认的代码风险，尚未全部做数据库故障复现和修复。不得宣称所有入口已原子收口。
2. **中：局部编辑 claimed 历史预留与缺失 hold 的合同。** 当前调用前预留已原子化；仍应检查恢复前已存在的 claimed + hold 是否可安全释放，以及 `finalizeQuotaIgnoreMissing` 对真实异常与合法未预留路径的区分。成功结算已在后续切片取消缺 hold 容忍；不能因夹具未建 hold 就放宽生产成功合同。
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

连续生图终态额度补充验证：整包回归初跑仅新增 success 夹具的 group ID 超过 varchar(36) 失败，其余用例通过；改用既有 clockid 后三条 `TestTerminalQuotaTransitionsAreAtomic` 全通过。夹具错误不作为业务缺陷；恢复故障验证使用真实 PostgreSQL，未触发 Provider。

连续生图命令切片由本任务主代理独立负责，范围为 `service.go`、删除无调用者的额度补偿方法及 [命令原子性回归](../../go/internal/imagesession/command_atomicity_test.go)。真实 PostgreSQL 队列故障明确匹配注入约束错误，避免将业务参数校验失败误算为事务证据；初始测试夹具因已有任务却未选基图而修正为空会话。外部 Provider 不参与命令事务，原有重试 billing sequence 未改为另一套身份模型。

该命令切片最终 `go test ./internal/imagesession -count=1 -v`（经 dev env）整包通过，耗时 30.853 秒；五个 opt-in 规模/负载或子进程辅助测试跳过，新增数据库回归均实际执行。`just docs-check` 通过；主代理已自审完整切片和删除残留，仅选择性提交本切片文件。

Graph 取消切片由本任务主代理负责，范围限定 `graph/service.go`、`runs.go` 与 [取消额度原子性回归](../../go/internal/graph/cancel_quota_atomicity_test.go)。`CancelRun` 删除预扫描和事务外后处理；`CancelRunTx` 的 Agent 调用者无需知道额度键或额外调用额度服务。取消命令持运行锁后锁节点并保存当前 attempt，再通过原有 quota owner 处理预留。Provider 外部调用不进入取消事务。缺失 hold 的旧容忍规则及调用前预留竞争仍属待审计项。

Graph 取消最终整包回归通过；新增四种数据库故障回归实际执行，目标规模查询门仍 opt-in。整包前一次运行保留为 FAIL：新增 effect 夹具使用了不合法的 request hash，修正为现有 64 位散列合同后针对性回归和最终整包均通过，未放宽 schema。Agent 的 `TestCancelWorkflowRunRequest*` 与 `TestCancelTaskParksProductGoalAfterWorkflowRunRequest` 消费者回归通过（1.953 秒）。主代理自审确认删除旧事务外额度路径，未修改 Agent、共享运行环境或其他任务文件。

Graph 恢复切片由本任务主代理负责，范围为 `graph/recovery.go` 与 [恢复额度回归](../../go/internal/graph/recovery_quota_atomicity_test.go)。原代码两分支均实际复现额度写故障被吞掉；修复后恢复调用者只有状态结果和错误，不再另持额度后处理列表。未改变原有 claimed/prepared 可重排队、Provider 已开始必须未知的判定，也未触发外部 Provider。

恢复切片最终 Graph 整包通过；新增数据库回归实际执行，原有恢复分页/隔离和取消执行恢复并发回归通过。`just docs-check` 与完整 diff 自审通过。期间账户任务修改 auth/schema，按其已登记范围保留；本切片不包含这些修改，也不占用其数据库前缀。

Graph 图像调用前准备切片由本任务主代理负责，范围为 `execute_node.go` 和 [预留与围栏回归](../../go/internal/graph/provider_quota_preparation_test.go)。保留 `prepareProviderCall` 对内容与其他入口的合同，在图像调用用例内以事务绑定的 Executor 组合 prepare 与 Reserve，删除事后查询 effect 并补偿额度的路径。

因账户任务的未完成 auth/settings 文件短暂不能编译，使用已提交基线 `a12e10ee` 的临时隔离 checkout 验证：原实现复现取消后新增 hold，带入本切片后两条真实数据库回归通过。调用期间用另一连接 `FOR UPDATE NOWAIT` 核实运行锁已释放。隔离证据不等价于包含他人未提交修改的工作区集成通过；不会改动对方文件修复该编译状态。

调用前准备切片验证：隔离基线 `a12e10ee` 加本切片的 Graph 整包通过（83.643 秒）。账户任务修复临时编译状态后，当前共享工作区的 `TestGraphProviderPreparation*`、额度不足、成功结算、取消/执行/恢复并发回归通过（4.875 秒）。共享工作区整包两次构建失败分别来自对方未完成的 auth/settings，不记为通过；本切片不声称其账户功能已验收。`just docs-check` 与主代理完整 diff 自审通过，临时 checkout 仅包含本切片文件并在验证后清理。

Graph 未知转换切片由本任务主代理负责，范围为 `effects.go`、删除 `execute_node.go/recovery.go` 重复后处理以及 [未知额度回归](../../go/internal/graph/unknown_quota_atomicity_test.go)。所有 `markNodeUnknown` 调用者包括 `failClaimedNode` 和 `failGraphRun` 消费同一持久化不变量；准确 attempt 在持锁节点内解析，失效 attempt 仍不产生副作用。针对性回归覆盖三条未知入口、过期恢复、旧 attempt 额度隔离和 Provider 未知，真实 PostgreSQL 全部通过。

未知转换切片最终当前工作区 Graph 整包通过；新增数据库用例实际执行。`just docs-check` 与主代理完整 diff 自审通过；没有修改当前账户任务的 auth/settings/schema，也未使用自进化采证任务的冻结资源。

Graph 图像结算切片由本任务主代理负责，范围为 `execute_node.go` 与 [结算原子性回归](../../go/internal/graph/image_settlement_atomicity_test.go)。经真实商品创建、图运行、确定性 Provider 和真实商品资产 writer 复现；结算故障时资产/artifact/成功投影回滚，原 Provider effect 的 applied 不改写为 failed，节点记录持久化失败原因。未知运行的重复投递直接调用 Executor，不使用会清除 ledger 并重置节点的 reclaim 夹具。

图像结算切片当前工作区 Graph 整包通过（89.156 秒）；之后补强的 Provider applied/持久化失败原因断言针对性通过（2.552 秒）。原有成功结算、未知和取消等整包消费者合同通过，opt-in 规模门不作为验收证据。主代理自审完整 diff，确认三条资产持久化分支均在原事务结算，删除旧 consumed 和事务外额度调用；`just docs-check` 通过。

Graph 明确失败切片由本任务主代理负责，范围为 `durability.go`、删除 `execute_node.go/quota_wire.go` 事务外后处理及 [失败额度回归](../../go/internal/graph/failure_quota_atomicity_test.go)。已过 Provider 边界仍走 unknown；未过边界释放预留。原代码两条命令均复现额度约束错误未阻止 failed；修复后持久化故障保持可恢复状态，故障解除后只释放一次。

缺失 hold 合同补充：`runClaimedNode` 的 `DeliveryFromCompose` 分支使用本地 `subject_compose`，明确跳过付费 Provider/Reserve，但与付费生成共用 `persistImageArtifact`。因此成功结算的缺失预留容忍既掩盖付费异常，也承接合法免费合成；后续须显式区分合同，不能全局删除 NotFound 容忍后让合成误失败。此发现来自当前调用链，后续成功结算切片已显式分离两种合同；其他终态未全局改写。

明确失败切片当前工作区 Graph 整包通过（82.635 秒），新增 PostgreSQL 回归实际执行，针对性组合包含未知转换、结算失败和取消/执行/恢复竞争（3.631 秒）。`just docs-check`、完整 diff 自审与已删除 helper 的代码/文档残留扫描通过。本切片未占用账户或图片评价任务的资源。

Graph 成功计费合同切片由本任务主代理负责，范围为 `execute_node.go/quota_wire.go`、复用上传参考图夹具的窄扩展，以及 [合成零额度执行链](../../go/internal/graph/compose_quota_test.go) 和 [付费预留合同](../../go/internal/graph/quota_attempt_test.go)。将原本只传 RouteMap 的内部持久化参数改为已有交付结果对象，保留实际字节来源的决定；没有增加收费 flag、Provider 字段或持久化形状。原代码复现无 hold 仍接受结算；真实白底主体合成、付费成功与结算故障回归均通过。

成功计费合同切片当前工作区 Graph 整包通过（86.340 秒）；新增付费缺预留拒绝与本地合成持久化回归实际执行，正常付费及结算故障针对性组合通过（4.978 秒）。主代理自审确认仅内部交付参数复用已有对象，wire/持久化 JSON 不变，未引入第二套计费实现；`just docs-check` 通过。

连续生图 effect 围栏切片由本任务主代理负责，范围为 `imagesession/execute.go` 和 [effect attempt 回归](../../go/internal/imagesession/effect_attempt_test.go)。当前工作区 imagesession 整包通过（31.140 秒），新增真实数据库用例实际执行；`just docs-check` 与完整 diff 自审通过。effect 写入错误的传播和业务失败分类仍待后续切片验证，不把围栏通过视为该缺口已修复。

连续生图 effect 持久化切片由本任务主代理负责，范围为 `imagesession/execute.go` 与 [队列/effect 持久化回归](../../go/internal/imagesession/effect_persistence_test.go)。只将 effect 账本写入错误从业务重试分类中分离，保留底层数据库错误供追踪；尝试失效仍使用 errStale。确认失败、未知与图片已保存三种故障均通过真实 queue.Consume；故障解除后已有恢复流程分别保持未知或完成已保存结果，不再次调用 Provider。

发现过的追踪缺口：图片已保存但 applied 写失败时，旧恢复删除 pending effect。后续恢复账本切片已改为根据实际轮次/资产恢复 applied 并保留原请求；没有证据的 pending 保留为未知。该修复不代表其他对账路径已全部验收。

effect 持久化切片当前工作区 imagesession 整包通过（31.598 秒）；新增数据库用例实际运行，校验原始约束 SQLSTATE、队列 pending、恢复终态及 Provider 调用次数。`just docs-check` 与主代理完整 diff 自审通过，所有 markEffect 调用者已扫描，不再忽略其返回错误。

连续生图恢复账本切片由本任务主代理负责，范围为 `imagesession/recovery.go`、补强 [effect 持久化回归](../../go/internal/imagesession/effect_persistence_test.go) 和 [缺失轮次证据回归](../../go/internal/imagesession/recovery_effect_test.go)。原代码两个负例均复现。恢复只提升当前 attempt 的 pending effect，要求批次全部索引都有同会话、同组轮次及资产；完整已保存批次保留原 request/attempt，其他未解释的 pending/unknown 阻止重排队。删除原恢复中的 pending effect 删除路径。

恢复账本切片验证：账户任务未完成的 auth 签名曾阻挡编译，未修改其文件；在隔离基线 `18ad035b` 加本切片的 imagesession 整包通过（35.401 秒）。随后当前工作区的 effect 持久化、缺证据与恢复回归通过（4.060 秒）。新增数据库用例实际运行，包含恢复 applied 更新被约束拒绝时任务回滚。主代理完整 diff 自审与 `just docs-check` 通过；临时 checkout 只含本切片文件，验证后清理。

连续生图 effect 复用切片由本任务主代理负责，范围为 `imagesession/execute.go` 与 [四种效果状态合同](../../go/internal/imagesession/effect_reuse_test.go)。保留既有按批次唯一账本，只将 confirmed failed 作为允许替换的冲突行；更新新尝试的 pending、Provider、request hash/JSON，清除旧响应与失败详情。pending 的未知转换在事务提交后再返回 unknownErr，避免因返回分类错误回滚；已有 unknown 不继续调用。账本创建/更新错误同样保持持久化错误语义，数据库读取错误不再伪装成 attempt 失效。

复用切片最终当前工作区 imagesession 整包通过（37.238 秒），新增四状态数据库回归实际执行。首次整包因新状态夹具遗留 running 占用包级容量而出现 queue.ErrLater，保留为 FAIL；改用独立 `pf_effectreuse_*` 测试库，测试结束自动清理。复核包级库未发现仍运行的本轮 effect-reuse 任务，没有删除其他测试数据。`just docs-check` 与完整 diff 自审通过；旧 pending 接管条件及旧 failed 元数据残留已删除。


连续生图调用准备切片由本任务主代理负责，范围为 `imagesession/execute.go`、[调用前事务回归](../../go/internal/imagesession/provider_preparation_test.go) 和恢复负例夹具调整。候选开始投影与批次 intent 复用同一持锁事务，Provider 仍在提交后调用；已 applied 的批次不再额外写 candidate_started。准备持久化失败沿既有 effectPersistenceError 返回队列，未发出的调用可以由过期恢复安全重排队。新增回归分别拒绝 intent 插入和候选阶段更新，直接检查真实数据库的阶段、活动候选和 effect 行数，以及恢复结果与 Provider 调用次数。测试使用独立 `pf_preparation_*` 库并自动清理。

调用准备切片当前工作区 imagesession 整包通过（38.849 秒），两个真实数据库故障子例实际执行；既有四状态 effect 复用与缺轮次证据的针对性组合通过（5.481 秒）。主代理完整 diff 自审确认 Provider 不在事务内，独立 markCandidateStarted 无残留，恢复负例仍明确模拟 candidate_saved 而非依赖准备阶段。`just docs-check` 与 diff 空白检查通过，未修改其他任务的账户、schema 或运行资源。


Graph 提案依赖切片由本任务主代理负责，范围为 `graph/service.go`、`agent/tools_graph.go` 和 [Service 依赖回归](../../go/internal/graph/proposal_dependency_test.go)。现有 ProductGuard 保留商品归属规则，Graph Service 负责装配自身配置，Agent 不再为该方法补 context。真实 PostgreSQL 正常商家、其他商家与插入约束故障均验证提案行数、图 revision 和节点数；故障断言要求 SQLSTATE 23514，避免缺依赖提前失败造成假通过。尚未删除 Graph 内部和其他低层事务调用者的 context 注入合同，不能据此声称依赖治理完成。

提案依赖切片验证：Graph 新增三个数据库场景及确认/丢弃消费者通过（1.045 秒）；Agent 图写入权威与提案持久化消费者通过（4.247 秒），均实际执行。标准 `go vet` 因既有 `ocr_trace_test.go` 的 testing.Context 和 `eval_provenance_regression_test.go` 的 testing.Chdir 与模块 Go 1.23 声明不符而失败；关闭 stdversion 分析项后的其余 vet 检查通过，此结果不代表标准 vet 全绿。未修改这些无关测试或工具链版本。主代理完整 diff 自审、调用者与旧补偿注释残留检查、`just docs-check` 通过；本切片未跑整包或真实模型评价。


Graph Provider 原因切片由本任务主代理负责，范围为 `graph/execute_node.go`、[Provider 失败原因回归](../../go/internal/graph/provider_failure_detail_test.go) 和原未知额度回归调用签名。复用原节点失败的 apperr.Detail 提取规则，两类 Provider 错误都保留未知提示与具体原因，经既有 markNodeUnknown 写入节点、effect 和事件；不改变重试分类、额度合同或持久化结构。真实 PostgreSQL 两条调用原代码均复现原因丢失，修复后与三条未知额度事务入口的针对性组合通过（1.079 秒）。

Provider 原因切片当前工作区 Graph 整包通过（87.623 秒），新增数据库原因断言实际执行；未运行 opt-in 规模或真实模型门。复用已确认的标准 vet 版本声明缺口，`go vet -stdversion=false ./internal/graph` 通过，不将其称为标准 vet 全绿。主代理完整 diff 自审、markUnknownCommitted 全调用者扫描、`just docs-check` 和空白检查通过；未改变其他任务资源。


Provider 错误适配切片由本任务主代理负责，范围为 `providers/adapt/adapt.go`、`providers/prompt.go`、Graph 未知提示去重及对应测试。继续追踪真实适配器发现上个切片只证明 Graph 收到原因后能保存，不能证明适配前原因未被删除；本切片用原生多错误包装保留 Graph unknown 和底层 error，不新增错误类型。四类图片失败可 errors.Is 原错误，文稿解码失败可 errors.As json.SyntaxError；未把原响应正文写入错误。真实数据库 [适配器集成回归](../../go/internal/graph/provider_adapter_cause_test.go) 经过商品创建、图执行、正式 GraphImage 适配器、终态写入与再次投递，节点和 effect 保存 trace，未知提示只出现一次，客户端仅调用一次。

请求追踪审计的新证据：`callProvider/callImageProvider` 的 request_json 仅含 node_id、node_type、input_digest、attempt_id；实际 PromptRequest 的文稿动作/内容和 ImageRequest 的生成参数/变体/参考图没有直接记录。该 hash 当前是执行身份摘要，不能作为实际 Provider 请求证明。下层 `providers/adapt.graphImage.GenerateImage` 还执行 CompileImageModelPrompt 并转为 GenerateRequest；因此 Graph 边界证据也不等同 HTTP 出站正文。未发现这些缺字段导致实际 Provider 输入错误的复现，当前归为追踪缺口。后续实现须绑定记录与调用的同一 typed request，参考图只记录身份和实际字节摘要，避免 base64 进入具有 64 KiB 限制的 effect JSON；需真实数据库比对实际捕获请求、参考图摘要及超限时 Provider 零调用。后续请求证据切片已实现 Graph Provider 接口边界记录；HTTP 出站正文仍不在本切片证明范围。

错误适配切片验证：providers 整包通过（0.109 秒），providers/adapt 整包通过（0.012 秒）；最终 Graph 实际适配器持久化、双入口原因和未知额度回滚组合通过（2.564 秒）。`go vet -stdversion=false` 的 providers/adapt/graph 检查通过；标准 vet 的既有版本声明缺口未改变。主代理完整 diff 自审、相关返回路径扫描、`just docs-check` 与空白检查通过；没有真实模型费用，没有触碰账户/schema 或共享进程。本次没有重跑 Graph 整包，前一切片整包证据不当作本次整包结果。


Graph 请求证据切片由本任务主代理负责，范围为 `graph/execute_node.go/effects.go`、调用签名回归调整与 [请求/数据库比对](../../go/internal/graph/provider_request_evidence_test.go)。调用包装器接收既有 PromptRequest/ImageRequest，分别将该值直接传给 Provider 方法；移除捕获请求的零参数闭包。记录复用 typed request 的字段，参考图复制元数据并清空 Bytes，按原顺序记录实际字节 SHA-256，不改变 Provider 收到的引用或字节。request_hash 覆盖整份记录，既有 operation_key、attempt 围栏和单节点账本不变；没有新增表、兼容读取或另一套调用框架。

新数据库回归首次因 recovery 夹具从默认库取得商家 ID 而在隔离库外键失败；修复夹具为可显式传 merchantID，原调用者仍消费原默认商家夹具。四个独立 pf_reqtrace_* 数据库场景通过（5.044 秒）：文稿/图像的正常与超限输入。正常场景在 Provider 回调中读取已提交 intent，完整比较 typed request、实际参考图 hash、记录 hash；超限场景保持 claimed、零 intent、零 hold 和零 Provider 调用。256 KiB 参考图不会触发 JSON 大小限制；非字节请求证据仍受既有 64 KiB 上限约束，超出时明确拒绝调用，未评估生产请求大小分布。记录对应 Graph Provider 接口输入，不代表 providers 内部编译后 HTTP 正文或最终生效模型参数已全部留存。

请求证据切片最终当前工作区 Graph 整包通过（91.740 秒），`go vet -stdversion=false ./internal/graph`、完整 diff 自审与 `just docs-check` 通过。首次整包停滞在 TestImageNodeRejectsInsufficientQuota，主代理仅对本任务测试 PID 611052 发 SIGQUIT 采栈并终止，记为 FAIL；栈包含并发文稿 persistContentArtifact 的 run 锁和 LoadFactSet 读取，尚无稳定因果复现。该用例单独重跑通过（2.205 秒），带 180 秒超时的整包再次通过；不据此声明并发停滞已修复。复核首轮最后观察到的独立库已不存在，无需删除其他数据库。未修改共享运行服务或其他任务资源。


Graph 停滞跟进：当前调用链保持文稿采用先 run 后 graph，尚未发现足以解释旧栈停滞的确定性锁序反转。`TestImageNodeRejectsInsufficientQuota -count=10 -timeout 60s` 全部通过（14.852 秒），没有新增稳定复现，不修改并发控制；既有停滞仍为待验证风险。停止无新证据的重复尝试，保留原栈和复现命令。

局部编辑成功 hold 切片由本任务主代理负责，范围为 `localedit/quota_wire.go` 与 [成功预留合同回归](../../go/internal/localedit/quota_wire_test.go)。当前唯一成功入口 persistResult 必经付费 Edit，没有 Graph 本地主体合成例外；取消/失败前未 Reserve 的合法路径不受此次改动影响。真实 PostgreSQL 原代码复现缺失 hold 的持久化返回 nil；修复后返回 NotFound，商品资产数不变、任务保持 running 且无 result_asset_id、attempt 保持 pending。为同一 attempt 补充预留后调用相同成功边界可完成结算。该负例验证持久化不变量，不声称已复现生产数据丢失。

局部编辑成功 hold 切片当前工作区 localedit 整包通过（4.502 秒），标准 go vet 通过。主代理自审完整 diff，取消/未知/释放合同未扩大。共享工作区 docs-check 因账户任务正在归档、架构文档链接尚不存在的 saas-account-team 归档文件而失败；在隔离基线 2610f2b0 加本切片三个文件执行 docs-check 通过，确认临时 checkout 仅有本切片后已清理。共享文档失败不计为通过，也未修改其他任务文档。


连续生图额度查找切片由本任务主代理负责，范围为 `imagesession/quota_wire.go` 和 [数据库查询失败回归](../../go/internal/imagesession/quota_lookup_error_test.go)。原 mustActiveQuotaKey 在查询报错时退回首次生成键，后续额度查询错误会掩盖原故障；未建额度账户的原始负例甚至三条入口均返回 nil。最终回归补齐真实账户和 reserved hold，在独立 pf_quotaread_* 测试库暂时重命名 hold 表，验证 settle/release/unknown 保留 PostgreSQL 42P01、余额与原 hold 不变。该表仅属于一次性测试库，清理先恢复名称再关闭测试库。实际没有活跃 hold 时的旧键合同、缺失 hold 容忍和 billing sequence 绑定尚未在本切片改变。

额度查找切片 imagesession 整包通过（40.192 秒），随后补强真实预留的回归通过（1.803 秒），标准 go vet 与当前共享工作区 docs-check 通过。完整 diff 自审及 mustActiveQuotaKey 删除残留检查通过；无新增 schema 或 API，无其他任务资源改动。
