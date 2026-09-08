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
| 连续生图活跃 hold 查询失败退回初始键，原数据库错误丢失甚至被后续 NotFound 容忍吞掉 | 三种 finalizer 直接消费 activeQuotaKey 的错误，删除 mustActiveQuotaKey，保留底层数据库 cause | 独立 PostgreSQL 关系不可用时三条入口透传 SQLSTATE 42P01；真实预留与余额不变 | `a32f72bc` |
| Pi 默认并行工具同时追加 checkpoint，Node 为两个请求分配同一序号，Go 对不同内容返回 Conflict | TurnRuntime 串行持久化 checkpoint；清理等待待写链，新 Turn 重置；沿既有 lease 错误中止后续项 | 原代码两种并发场景发送 [1,1]；修复后确认成功发送 [1,2]、首条失败只发一次且两调用失败，清理等待；真实 PG 验证序号绑定内容 | 2b4355ec |
| 连续生图结算吞掉缺失预留错误，允许终态与实际额度结算分离 | settleGenerationQuota 直接返回 quota.Settle 的错误；原终态事务回滚 | 独立 PostgreSQL 验证成功和已调用失败终态拒绝缺 hold，恢复原预留后同 attempt 可提交并结算 | `c8e00923` |
| 手动重试缺失活动预留时退回首次已结算键，旧结算幂等结果放行当前终态 | 额度键查询返回是否命中活动预留；结算必须命中，取消/释放消费者保持原合同 | 两种终态的 retry=true 原实现均返回 nil；修复后拒绝并回滚，恢复同一重试 hold 后结算 | `bca12de2` |
| Agent 统计可运行节点时需注入 Graph 依赖并编排三步内部查询 | Graph Service.CountRunnableNodesTx 拥有依赖和查询步骤，Agent 保留审批 Conflict 解释 | PostgreSQL 正常计数/跨商家/缺依赖/零运行写入；隔离基线 Agent 空图、确认与创建消费者通过 | `d26d71dd` |
| 局部编辑调用前数据库读取故障被归为不可重试业务失败，队列收到 nil | Execute 区分输入错误、失效 attempt 和基础设施读取错误；ReadIO 保留 cause | 独立 PG 42P01 经 queue.Consume 返回、信封 pending、task claimed，恢复后执行成功；三类媒体 I/O 映射保留 cause | `5b6105a3` |
| 局部编辑结果写入失败后使用非法 attempt phase，且原持久化原因被终态错误覆盖 | 复用合法 unknown phase；errors.Join 保留结果与终态错误，失效 attempt 停手 | 真实 PG 资产失败/终态同时失败两场景；恢复后额度待对账、重复信封 consumed、Provider 仅一次 | `6eb57645` |
| 交付结果事务失败被 failed 终态的 nil 或第二个错误覆盖 | Execute 保留原结果错误并组合 failed 持久化错误，业务保持可重试 failed | PostgreSQL 双故障分支返回原因、零派生资产、信封 pending；Retry/过期恢复后重复执行只有一个结果资产 | `6aa9dac5` |
| 交付自行更新商品排序时间且忽略语句错误，最终只见事务提交回滚提示 | 复用 product.Touch，商品模块维护写表细节，交付直接返回错误 | 真实 PG 原实现丢失 ConstraintName；修复后原因保留、结果事务回滚，Retry 后时间推进 | `9510a457` |
| 额度账户/预留/事件写入错误被替换为通用 Internal，调用者无法获取数据库原因 | quota 服务用 errors.Join 保留原应用错误与数据库错误，不改余额和 HTTP 合同 | 13 个 PG 写入故障回滚与 HTTP 隔离场景；局部编辑原始 SQLSTATE 透传恢复回归通过 | 7a14ac06 |
| 同商家首次并发建账，唯一键冲突后在失败事务内重读，导致一个调用失败 | quota 以指定 merchant_id 的冲突忽略保持事务有效，仅插入者发试用额度 | PG 双事务固定缺行时序，两个调用成功且仅一笔试用额度事件；额度整包通过 | 5aad0c66 |
| 额度事件插入唯一键错误被吞没，事务提交只返回笼统回滚错误 | appendEvent 统一返回数据库原因；重放仍由原账户锁及业务状态负责 | PG 唯一约束故障保留 23505 与约束名，余额/hold/事件整体回滚；额度整包通过 | 随本次提交 |

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
| Agent 控制 | Node manager 持进程内调度，TurnRuntime 持 lease/checkpoint，工具经 Go effect reconciliation；checkpoint 写失败会中止执行。保留该分工。 | 已分别验证 Go 辅助进程崩溃和 Node 替身服务重启；后续整包实际通过真实 Node → Go → PostgreSQL 的问题回答恢复（一/两个问题）。完整联动 crash 矩阵仍未验收，测试 Provider 不代表真实模型质量。 |

## 后续优先级

优先级按可能损害排序；修改频率与扩散范围目前只有静态调用者证据，没有生产统计。

1. **高：Graph 和连续生图的终态/额度事务分裂。** Graph 取消已归入持锁命令，Graph 过期恢复已同步额度；图像成功持久化已与结算同事务；明确失败终态额度已归入命令，付费成功结算已要求 hold；取消/未知/释放的缺失 hold 合同仍待核实；`imagesession/service.go`、`execute.go`、`quota_wire.go` 的 billing sequence 与终态组合需继续沿真实调用顺序核实。`imagesession.finishFailed` 的旧 attempt 越界已修复，成功/未知/过期恢复的额度事务已收敛，创建、取消和手工重试已改为用例内组合事务；billing sequence 的精确绑定和缺失 hold 处理仍待核实。当前属于已确认的代码风险，尚未全部做数据库故障复现和修复。不得宣称所有入口已原子收口。
2. **中：局部编辑未知/释放的缺失 hold 合同与额度底层错误。** 当前唯一运行时 Reserve 入口与 provider_pending 同事务，不产生 claimed + hold；新增真实数据库准备失败后恢复执行回归确认旧 attempt 零 hold、新 attempt 正常结算，不为历史组合新增恢复分支。unknown/release 的缺失 hold 容忍仍待核实。成功结算已拒绝缺 hold。quota 的账户/hold/事件写入原因丢失已在后续切片修复并验证 HTTP 文案保持；账户初始化、锁定及读取错误转换仍待核实。
3. **中：Graph context 服务依赖。** `WithProductGuard` 仍跨 product/recipe 装配，形成编译期不可见的前置。候选方向是显式 Graph 用例依赖与已有事务入口；必须维持跨商家统一 404、事务组合及 worker 无 HTTP 商家上下文的执行合同。Agent 的运行资格查询已归入 Graph Service，非测试 Agent 调用不再装配该 context 依赖；Graph 内部及 product/recipe 低层调用仍未整体迁移，不把依赖改善升级成已复现安全缺陷。
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


## Agent 控制链路复核

本轮由同一主代理只读复核 Node manager → TurnRuntime → ProductFlowClient → Go execution/journal。当前 Node 控制实现保留：manager 拥有 admission 和 handoff 调度；TurnRuntime 拥有 lease、checkpoint、问题等待者和终态编排；PiSessionAdapter 拥有 session/model。没有发现需要将这些职责合并的证据，没有修改业务代码。

- `TurnRuntime.checkpoint` 在 lease 缺失、停止或已丢失时拒绝；追加错误保存为 executionLeaseError，并 abort 当前 runtime/session。序号在追加确认后推进，每次新 Turn 重置。初读时尚未确认并发可达；后续已核对安装的 Pi 0.83.0 默认并行工具，并复现重复序号，后续切片已串行化 checkpoint 持久化。
- `updateExecutionPhase` 复用 Promise 链串行发送，发送时读取最新 phase；`stopExecutionHeartbeat` 停止计时并等待该链完成。现有测试显式延迟第一个 heartbeat，验证旧请求不能覆盖 waiting_input。无需再增加 heartbeat 队列或第二套租约。
- Node 的终态通过 journal 发布；PG 已确认的 terminal 不重复提交 checkpoint。cleanup 等待 eventChain，处理终态发布失败并释放执行；问题 waiter 被拒绝并清理。Go 负责实际 fencing、事件唯一性和 journal/projection 的事务权威，Node 本地文件结果不替代它。

当前 checkout 验证：`pnpm --dir agent-service test` 为 37 文件通过、2 文件跳过，313 测试通过、9 跳过（12.32 秒）；build 与生成合同检查通过。Node 测试中的 ProductFlow client 替身只证明本地编排，不计为 PostgreSQL 持久化证据。

对应真实 PostgreSQL 边界组合实际执行并通过（5.125 秒）：`TestClaimNewAttemptResetsCheckpointSequence`、`TestJournalBatchExactReplayReturnsOriginalReceiptWithoutDuplicateRows`、`TestRejectedTurnEndCannotSplitJournalAndProjection`、`TestRecoverExpiredExecutionsRejectsStaleFencingWriter`、`TestAppendEventsAndCheckpointDoNotDeadlock`。分别核对新尝试序号、重复事件原 receipt/零重复行、非法终态不改投影、过期恢复后旧 lease 写入被拒绝，以及可控交错下的 checkpoint/journal 锁序。这不等同全部 Node/Go 进程 SIGKILL 矩阵或真实模型质量验收。

该轮未运行付费模型或规模门，未改动冻结评价资源。后续核实 process-restart.e2e.test.ts 属于默认 Vitest include，并非 opt-in；其独立执行结果及证据边界见下文。Node 与 Go Agent 路径无新增 diff，正常 build 输出未提交。该复核补充当前证据并保留有价值的现有边界，不将测试全绿作为整体控制链路完成声明。


Node checkpoint 顺序切片由本任务主代理负责，范围为 `agent-service/src/turn-runtime.ts`、既有 runtime 回归及 [Go 序号数据库合同](../../go/internal/agent/checkpoint_sequence_test.go)。已安装 pi-agent-core 0.83.0 的 agent.js 默认 toolExecution 为 parallel，agent-loop.js 在没有 sequential 工具时通过 Promise.all 执行工具；ProductFlow 工具及 session 未配置 sequential。多个工具经 withEffect 进入同一 checkpoint，旧代码在 await 追加后才推进序号；实测两个请求均发 sequence=1，原成功/失败两种用例都失败。

修复在 TurnRuntime 内用 Promise 链串行分配序号和追加确认。单项错误仍返回原调用者并设置既有 executionLeaseError；链尾仅承接排队，后续项读取该错误后拒绝，不再次发送。cleanup 等待链完成，再释放 lease、清理 session；新 Turn 重置链和序号。两个新增 Node 回归包含延迟首条、成功/失败分支和清理等待，不为此改变 Pi 工具业务并行策略。

最终 Node 整包 37 文件通过、2 文件跳过，315 测试通过、9 跳过（10.36 秒）；build 和生成合同检查通过。真实 PostgreSQL 序号冲突、尝试重置及 journal/checkpoint 锁序组合通过（2.735 秒）：同序号不同内容 Conflict，后续连续序号实际留下两行，last_checkpoint_sequence=2。本证据分别验证 Node 发送顺序与 Go 持久化合同，不声称已运行真实模型并行工具或完整跨进程 SIGKILL。主代理完整 diff 自审、docs-check 通过；未修改评价任务独占的 evals 或 eval_* 文件。


## Agent 进程终止与恢复证据

本切片由同一主代理负责，仅补充既有崩溃测试的实际验收记录，不修改运行时或评价资源。Go [SIGKILL 测试](../../go/internal/agent/sigkill_gopg_test.go) 启动当前测试二进制的独立 lease writer，经测试 HTTP 服务写入真实 PostgreSQL，在明确就绪点仅终止该子进程；父进程显式使租约过期并调用恢复函数。四个子例实际通过（2.309 秒）：

- model_start：模型开始 checkpoint 后终止，projection 为 unknown/execution_interrupted，invocation 为 interrupted，journal 连续。
- mutation：商品创建提交后终止，仅有一个对应商品，projection 为 unknown，终态事件唯一且 journal 连续。
- approval：审批请求提交后终止，测试显式设置 awaiting_confirmation；恢复保留等待状态、一个审批请求、零 graph run、零 turn/end。本例不证明 Node 自动完成审批状态投影。
- turn_end：成功终态提交后终止，恢复保留 succeeded、唯一 turn/end 和连续 journal，旧 lease 的追加请求被拒绝。

可复跑命令：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/agent -run "^TestSIGKILLLeaseHolderAgainstGoPG$" -count=1 -timeout 90s -v'`。helper 自身的环境条件用于子进程分流，不是父测试跳过或真实模型授权。

Node [进程重启测试](../../agent-service/src/process-restart.e2e.test.ts) 使用真正的 Node 服务子进程、独立临时 data root 和本地假 Provider/ProductFlow HTTP 服务。四例独立执行通过（7.62 秒）：SIGTERM 保留问题等待 requires_input；活动 Turn 优雅退出投影为 unknown；Provider 请求开始后 SIGKILL 并重启，模型请求累计仍为一次；服务端提交 journal 但本地未收到 ACK 时 SIGKILL，重启经确认恢复且不重发该批次。测试在 finally 中终止自身子进程、关闭本地服务器并删除自身临时目录。

可复跑命令：`pnpm --dir agent-service exec vitest run src/process-restart.e2e.test.ts --reporter=verbose`。当前 Vitest 默认 include 已包含此文件，本次独立 verbose 运行明确确认四例均非 skipped。Node 用例证明本地重启与 HTTP 确认策略，Go 用例证明真实数据库恢复规则；没有把二者拼接为未经运行的端到端证据。仍未运行真实 Node 与 Go/PostgreSQL 联动的完整故障时点矩阵、真实 Provider 结果未知后的对账或生产故障演练。

主代理自审本切片完整文档 diff，核对测试断言与上述描述，docs-check 通过；既有开发服务未重启，没有真实模型调用或新增费用。本轮未发现需要改动业务代码的新缺陷。


连续生图结算预留切片由本任务主代理负责，范围为 `imagesession/quota_wire.go` 与 [终态额度回归](../../go/internal/imagesession/success_hold_test.go)。原结算入口复用 finalizeQuotaIgnoreMissing，注释以直插测试夹具作为忽略 NotFound 的理由；真实 PostgreSQL 成功负例返回 nil，确认缺失任务 hold 时仍提交 succeeded。修改让成功以及已有 Provider effect 的失败结算直接透传 quota.Settle 错误，复用 finishSucceeded/finishFailed 原有事务回滚，无新增接口、状态或 schema。取消/unknown/未调用释放的缺 hold 行为和 billing sequence 选择规则不在本切片改变。

新回归用独立 pf_successhold_* 数据库，经现有创建/Generate/claim 夹具取得任务和预留，仅使该库的原 hold 键暂时不可查，验证返回 NotFound、任务仍 running、active attempt 保留、终止时间和结果 group 不写入、余额不变；恢复原键后同 attempt 完成对应终态并结算。失败场景经 ensureEffect/markEffect 创建明确失败证据。测试隔离库自动清理，不影响开发库。此测试证明缺失结算前置条件时拒绝终态，不声称已经找到生产中丢失 hold 的来源，也不声称重新执行了 Provider。

本切片最终 imagesession 整包通过（42.806 秒），两个新增真实数据库子例实际执行，标准 go vet 通过。首轮整包的既有 TestImageCheckpointTransactionBoundaries/stale_consumer 未等到重投递，记为 FAIL；定向复跑该用例及最终整包通过，未证明根因，不修改队列时序。新增失败夹具初版分别因误用异常构造器和不合法 request hash 失败，已改用现有 apperr.Validation 与 canonjson.SHA256Hex。主代理完整 diff 自审、结算调用者扫描和空白检查通过。共享 docs-check 因其他任务新增 /ops 路由尚未同步架构、README 与 PRD 而失败；当前提交的独立临时 checkout 执行相同 check_docs.py 通过，随后清理。共享失败不记为通过；其他任务的 schema、商品、账户和前端修改均未纳入提交，未使用共享开发进程或真实模型。


重试额度回退切片由本任务主代理负责，范围仍为 `imagesession/quota_wire.go`、既有终态额度回归。继续验证上个切片发现新旁路：首次 hold 已 settled，当前手动重试 hold 不可查时，activeQuotaKey 返回首次键；quota.Settle 对旧 settled 行幂等返回 nil，成功和已调用失败两种终态负例均复现提交。因此仅透传 quota 错误不足以证明结算条件成立。

查询现在额外返回是否实际命中 reserved/pending_reconciliation 预留，结算在未命中时返回 NotFound，由原终态事务回滚。unknown/release 仍消费既有键与错误，未扩大其合同。没有修改共享 quota 模块或当前由管理后台任务占用的 schema。终态重复投递仍由任务状态与 attempt 条件拒绝重复写入，不依赖退回历史 hold 完成幂等。本切片消除无活动预留的历史结算旁路；活动预留仍由任务前缀和创建时间选择，多个活动历史 hold 的精确 billing identity 绑定仍未完成。

回归扩为成功/失败 × 首次/手动重试四个独立数据库场景。重试场景先经额度服务结算首次预留、设置可重试失败夹具，再实际调用 Service.Retry 建立新预留；当前预留不可查时检查任务回滚与余额不变，恢复该预留后同 attempt 提交并结算。不把夹具设置的失败状态描述为真实 Provider 失败调用。

最终 imagesession 整包通过（48.835 秒），四个数据库子例实际执行；标准 go vet、全调用者签名扫描和主代理完整 diff 自审通过。共享 docs-check 仍因其他任务的 /ops 文档未齐而失败；以 HEAD 加本切片三个文件构造独立临时 checkout 执行 check_docs.py 通过，随后清理。没有更改其他任务的 quota/schema/Auth/Product/Web 文件、数据库或运行进程，没有真实 Provider 调用费用。


## 局部编辑 claimed 恢复边界复核

本切片由主代理负责，只补强 `localedit/quota_wire_test.go` 的既有准备失败回归并更新本记录。全调用者扫描确认 reserveEditQuota 的唯一非测试调用位于 prepareProviderCall，且与 markPhase(provider_pending) 共享事务。当前正常运行时不生成 claimed + reserved；旧取消夹具直接构造该状态，不能据此宣称当前存在历史预留泄漏的可达缺陷。保留既有恢复实现，不新增旧数据兼容或释放分支。

真实 PostgreSQL 回归继续沿同一任务执行：预留插入约束失败 → task/attempt 留在 claimed、Provider 未调用、旧 attempt 零 hold → 以确定性过期时间调用实际恢复函数 → 旧 effect failed、任务可重排队 → 去除本测试约束后 Execute → 新 attempt 调用测试 Provider、任务 succeeded、新 hold settled。恢复状态与再次执行在本轮完成验证，broker 投递和 SIGKILL 不在此新增回归的范围。测试清理约束使用 IF EXISTS，以兼容成功路径已移除与中途失败仍需清理两种情况。

初次补强要求 errors.As 获得 PostgreSQL 23514，实际只收到“创建预留失败”，该轮整包 FAIL（5.401 秒）。读取 quota.Reserve 确认 gdb.Create 错误被替换为 apperr.Internal，属于独立的原因追踪缺陷；没有通过改写错误预期来声称该缺陷已修复。恢复测试保留原非 nil 错误合同并增加上述真实数据库状态证据，最终 localedit 整包通过（4.433 秒），标准 go vet 通过。quota 当前由其他任务持有，未修改其实现或测试；后续需保留底层 cause 并检查同模块其他事务错误转换。

主代理自审完整测试与文档 diff、检查 Reserve 调用者和测试约束清理路径，当前共享工作区 docs-check 与空白检查通过。没有新增业务运行时路径，没有修改其他任务文件，没有真实模型费用或共享服务重启。


## Agent 运行资格查询的 Graph 归属

本切片由主代理负责，范围为 `graph/service.go`、`agent/workflow_requests.go` 和 [依赖数据库回归](../../go/internal/graph/runnable_dependency_test.go)。原 Agent requireRunnableWorkflow 直接配置 WithProductGuard，依次 LoadGraph、LoadAppliedGraph、SelectRunNodeIDs。未复现新的业务错误；已确认调用者依赖 Graph 的内部装配与读取步骤，属于边界改善。

新增 Graph Service.CountRunnableNodesTx 复用原查询和选择器，消费调用方的 GORM 事务并装配自身 Products，返回可运行节点数与原错误。Agent 保留空图/Validation 到审批 Conflict 的解释，删除三步读取和 context 注入。未改为 PreviewRun：该方法还加载 source 并生成 planned_action，语义与原审批资格查询不同。没有新增表、HTTP API、状态或另一套节点选择规则。全目录扫描确认 Agent 非测试代码不再调用 WithProductGuard、LoadAppliedGraph 或 SelectRunNodeIDs；全仓扫描确认两个导出读取包装没有其他调用者，删除 graph/export.go，Service 直接复用内部读取函数。其他 Graph context 调用仍存在，本切片不宣称全仓 context 依赖已删除。

真实 PostgreSQL 新回归通过（1.087 秒）：Service.DB 故意为空、只通过调用方事务传入数据库；合法商家得到正数、其他商家返回 NotFound、缺少 Products 返回 Internal、查询后 graph run 仍为零。Agent 消费者组合覆盖空图 Conflict、创建确认单和确认执行的既有测试。共享工作区初次编译被其他评价任务的未使用变量/重复 helper 阻断，记为 FAIL，未修改其文件；HEAD 加本切片的独立临时 checkout 实际通过（1.694 秒）。该组合不是 Agent 全包或 Graph 全包验收。

独立 checkout 的 go vet -stdversion=false 检查通过；标准 vet 的既有 Go 1.23 声明与 testing.Context/Chdir 使用缺口未在本切片改变，关闭该分析项不计为标准 vet 全绿。没有付费模型调用、外部副作用或共享服务重启。

删除无调用者的导出包装后，独立 checkout 的最终 Graph 数据库回归通过（0.877 秒），Agent 消费者组合通过（2.039 秒）；当前共享 docs-check 通过。主代理复核完整切片 diff、导出名称零代码残留和跨商家错误来源；临时 checkout 已清理。最终范围包含删除 graph/export.go，未修改其他任务的 Agent eval_*、后台或活文档。


## 局部编辑调用前读取失败分类

本切片由主代理负责，范围为 `localedit/execute.go`、现有 HTTP 测试服务装配和 [读取失败回归](../../go/internal/localedit/snapshot_failure_test.go)。旧 Execute 对 loadSnapshot 的全部错误调用不可重试 failed 终态。独立 PostgreSQL 测试暂时重命名该库的 product_image_assets 表，经 queue.Consume 实际触发 42P01；旧实现返回 nil，负例失败（1.944 秒）。数据库不可用不能证明输入不合法或 Provider 失败。

Execute 现在只将输入 Validation/NotFound 转为既有业务失败，其他读取错误原样返回队列；loadSnapshot 的旧 attempt 使用已有 errAttemptFenced，调用者停止而不尝试提交失败。源图/mask/参考图的 media.ReadIO 保留原错误链，继续交给同一个基础设施错误分支。文件不存在、身份核验失败等既有输入错误文案保持原合同。不新增重试状态或队列机制，仍由 claimed 过期恢复创建新 attempt。

测试服务增加显式数据库装配入口，原 newEditServer 继续复用原 testdb.Open；新故障测试使用自动清理的 pf_editsnapshot_* 独立库，避免重命名共享业务表。验证 queue.Consume 返回原 PostgreSQL 42P01、信封 pending、任务 running/claimed、Provider 未调用；恢复表后调用真实恢复函数，再次执行 succeeded 且调用测试 Provider。三种 ReadIO 映射的 cause 由定向单元回归验证，不能当作真实磁盘故障演练。当前回归没有重启 broker 或模拟 SIGKILL。

最终 localedit 整包通过（6.294 秒），新增数据库恢复与三类 I/O 原因回归实际执行；标准 go vet 通过。主代理自审完整 diff、loadSnapshot 全调用者与旧错误路径，检查故障表恢复及临时数据库清理。当前 docs-check 和空白检查通过，没有修改其他任务的 quota、商品、schema 或评价文件，没有真实模型调用。


## 局部编辑结果持久化失败与重复投递

本切片由主代理负责，范围为 `localedit/execute.go` 与 [结果写入数据库回归](../../go/internal/localedit/result_failure_test.go)。独立测试库在 Provider 返回后拒绝 local_edit 资产插入，旧执行器返回的却是 attempt phase 约束错误。当前 schema 只允许 claimed/provider_pending/provider_call/provider_result_received/succeeded/failed/unknown；该路径把任务投影用的 unknown_provider_effect 同时写入 attempt.phase，导致未知终态事务回滚。进一步拒绝 task unknown 写入时，原资产错误也被新的终态错误覆盖。两个原实现负例均失败（3.481 秒）。

结果持久化失败分支改用已有 unknown phase，并以 errors.Join 返回原始结果错误与终态错误；终态成功时仍保留结果持久化错误，避免队列/日志误认正常完成。attempt fence 已失效直接停手，不把迟到返回当持久化故障。没有放宽 schema、增加新状态或重新调用 Provider；恢复入口的 task.progress_phase=unknown_provider_effect 与 attempt.phase=unknown 分工保持原样。前端没有依赖本次改动分支的 unknown_provider_effect 专用投影。

新增两个 pf_editresult_* 独立 PostgreSQL 场景通过 queue.Consume 执行：资产插入失败必须保留原 ConstraintName；终态也失败时必须同时保留第二约束错误。第一种 task unknown，第二种 task running 并经实际过期恢复到 unknown；信封均回 pending，额度随后为 pending_reconciliation，零派生资产。模拟信封再次投递后实际 queue.Consume 成功并落 consumed，测试 Provider 累计只调用一次。测试约束仅留在自动销毁的独立数据库中，不修改共享表。没有真实模型或 broker/SIGKILL 演练。

最终 localedit 整包通过（8.566 秒），标准 go vet 通过。主代理完整 diff 自审，核对 attempt phase 约束、状态消费者及测试库清理；当前 docs-check 与空白检查通过。其他任务的 schema、quota、商品和评价文件没有纳入修改。


## 交付结果持久化错误与恢复

本切片由主代理负责，范围为 `delivery/execute.go` 和既有 [终态数据库回归](../../go/internal/delivery/terminal_persistence_test.go)。原 Execute 在 persist 失败后只返回 fail 的结果：failed 能提交时返回 nil，failed 也失败时覆盖原错误。真实 PostgreSQL 分别拒绝本测试 job 的 succeeded 和 failed 更新，两个原实现负例都失败（1.092 秒），原始 succeeded 约束原因不可达。

修改仅在结果持久化分支用 errors.Join 保留原错误及 fail 错误，仍以已有可重试 failed 和通用用户文案投影。本地渲染没有外部模型不确定性，不引入 unknown 或额度语义。原 attempt 条件更新、文件 compensation 和原资产保持规则不变。没有为这类简单错误组合提取跨业务执行框架。

最终真实数据库回归验证：只拒绝成功写入时 job failed/is_retryable，双终态写入失败时 job running；两者 queue.Consume 均返回原成功写入 ConstraintName，双失败还保留第二项原因，信封 pending，结果 ID 为空，零派生资产。去除本测试 job 的约束后，分别调用现有 Service.Retry 或 recoverDeliveryJobState，连续 Execute 两次均只得到一个派生资产与 succeeded。测试约束按 job ID 限定，成功或异常路径均清理；未修改开发数据库。

最终 delivery 整包通过（7.339 秒），标准 go vet 通过；主代理完整 diff 自审、约束清理及持久化消费者检查通过，当前 docs-check 与空白检查通过。原图读取失败的底层原因转换、persist 中直接更新商品 updated_at 的归属仍是后续调查项，本次未扩大到其他任务占用的 product 文件。没有真实模型费用或共享运行进程变更。


## 固定提交的五包组合验收

本轮由主代理固定 `6aa9dac5`，验证近期查询边界、错误传播、状态与额度修改的组合行为，不混入管理后台和评价任务尚未提交的代码。开始时无相关包测试进程运行；使用既有 testdb 按包隔离数据库，串行执行 graph、agent、imagesession、localedit、delivery，`-count=1 -p 1 -timeout 180s -json`，不重启开发服务或开启真实模型门。

| 包 | 最终包结果 | 时长 | pass 事件（含子例） | skip 事件 |
|---|---|---|---|---|
| graph | PASS | 92.035 秒 | 296 | 1 |
| agent | PASS | 83.776 秒 | 355 | 8 |
| imagesession | PASS | 46.175 秒 | 115 | 5 |
| localedit | PASS | 9.395 秒 | 41 | 0 |
| delivery | PASS | 7.534 秒 | 43 | 0 |

首轮用 git archive 构造快照，Graph/连续生图/局部编辑/交付通过；Agent FAIL（107.090 秒）。原因是 harness attribution 需要 Git 元数据，而问题回答恢复会启动真实 Node，归档目录缺少 tsx 依赖。保留该环境失败，不修改测试或业务实现。后续本地 shared clone 检出同一提交，核对 package.json/pnpm-lock.yaml 与已安装依赖一致，以只读使用的 node_modules 符号链接复用依赖，Agent 整包重跑通过。最终 clone 无 tracked 修改，仅有本轮依赖链接；两处临时 checkout 和链接均清理。原始 JSON 输出留于 `/tmp/pf-backend-integrated-tests.jsonl` 与 `/tmp/pf-backend-integrated-agent-final.jsonl`，前者的 Agent FAIL 不被后者覆盖。

默认门共记录 850 个 pass、14 个 skip 事件（包含父测试和子例，不能称为 850 个独立顶层测试）。Graph 跳过目标规模查询；Agent 跳过浏览器容量、journal 容量、SSE 容量、L2 live、外部 user-sim host、查询规模、HTTP 读取规模和独立 SIGKILL helper；imagesession 跳过活动集合规模、独立 checkpoint helper、HTTP/查询/SSE 规模。helper 在父场景中另行执行，独立 helper 的 skip 不等于父场景跳过；其余 opt-in 门没有通过声明。

本轮实际运行 [问题回答联动恢复](../../go/internal/agent/question_resume_gopg_test.go) 两个场景（共 6.590 秒）：真实 Node Agent 经真实 Go HTTP 服务与 PostgreSQL，在一个或两个问题的 waiting_input 后终止 Node；回答写入 PG 后 HTTP 返回服务暂不可用，重启同 data root 后恢复新 attempt、提交成功终态，并核对本地测试 Provider 请求里的每个 function_call_output 对应正确 call_id 与回答。该证据支持这一具体 Node/Go/PG 链路，不再将全部联动恢复统称为未运行；尚未证明完整故障时点矩阵或真实 Provider 对账。

五包 go vet -stdversion=false 通过；标准版本分析的既有缺口仍保留。本轮不改变业务代码。主代理检查测试结果、跳过项、独立 checkout 状态与清理，当前 docs-check 和文档 diff 自审通过。该组合门不是整个 Go 仓库、Node 默认整包、容量或真实模型质量门，仍未完成总目标。


## 交付商品更新时间写入归属

本切片由主代理负责，范围为 `delivery/execute.go` 与 [商品写入故障回归](../../go/internal/delivery/product_touch_test.go)。现有 product.Touch 已供图库资产事务使用，维护 products.updated_at 更新且返回数据库错误。交付 persist 在同一结果事务中重复该表更新，并以 `_ = ...Error` 忽略错误；真实 PostgreSQL 拒绝本测试商品写入后，原实现只返回 commit unexpectedly resulted in rollback（负例 FAIL，0.829 秒），原约束原因丢失。

persist 现在直接调用并返回 product.Touch，不再在执行文件中写 Products 模型。商品时间写入仍与资产、job succeeded 共用原事务；没有新增 helper/API，也没有修改其他任务持有的 product 文件。该边界让交付执行器不再了解商品更新时间列与赋值方式。交付采用版本路径仍直接写商品 current_delivery_adoption_version_id，不属于本次已收敛范围。

数据库回归用限定单个测试商品的 NOT VALID 约束拒绝后续商品更新，验证原 ConstraintName 保留、任务为可重试失败、结果 ID 为空、零派生资产且商品时间不变；移除故障并经现有 Service.Retry 再执行后，商品更新时间推进。成功/异常清理均移除测试约束，不修改开发库。最终 delivery 整包通过（7.370 秒），标准 go vet、完整 diff 自审、执行器 Products 写入零残留和 docs-check 通过；未运行真实模型或改动共享服务。


## 连续生图正常重试链的计费身份缺陷

本轮主代理沿实际 Generate → Execute(ErrRateLimit) 自动重试至耗尽 → Service.Retry → Execute 再次失败 → Service.Retry 验证活动预留，未用 SQL 改写 task 状态或制造多 hold。临时新增回归 `go/internal/imagesession/billing_lifecycle_test.go` 使用三个独立 pf_billingcycle_* 数据库，结尾分别成功、取消、unknown。成功与 unknown 场景通过；取消场景 FAIL（整组 4.635 秒）：第二次手动重试尚未 claim 或调用 Provider，取消后新 key :retry:4 的 hold 为 pending_reconciliation，预期应释放。

原因链：Retry 对上一轮 settled hold 之外创建新预留；历史 provider effects 仍存在；finalizeQuotaOnCancel 的 providerLedgerExists 仅按 task ID 计数，所以旧轮次 effect 被当作新预留的调用证据。该问题不需要手工制造多个活动 hold，也不属于不可达的历史数据假设。当前活动预留唯一并不足以证明 effect 与它属于同一计费轮次，应把计费身份缺口从待验证风险提升为已复现缺陷。

必要修改范围包含 imagesession 的 Generate/Retry、任务与 effect 持久化模型、准备调用、终态/取消/恢复及额度查询。需要在同一业务事务中固定新预留的计费身份，让实际 provider effect 记录消费的身份；取消按当前预留的 effect 判定，不能只看任务曾经调用过。不得用 progress_phase 展示值、created_at 排序或推算 worker attempts 替代持久化关联。应继续复用现有 quota 键、预留与队列事务，不建立第二套账本。最终验收须覆盖 queued 及等容量取消、调用后取消、部分候选续跑、旧 worker 返回、故障回滚与跨商家隔离。

本切片实现受明确文件占用阻挡。初查时 saas-ops-console 持有 schema；本轮复核发现该任务刚随 b251f20b 交付，但新认领的 saas-preferences-settings 已将必要 schema 及测试交给 account_backend。文件变干净不代表写入所有权已释放，本任务未写这些文件。计费生命周期回归保留为未提交的红色复现，未将预期改成 pending_reconciliation 或 skip 来取得通过；尚未交付该修复。文档单独记录新证据与依赖，不代表整个目标阻塞或完成。其他独立审计工作可继续，schema 释放后才能收口此跨层切片。


## 额度写入失败的原因与公共错误合同

本切片由主代理负责，范围为 `quota/service.go`、[额度写入数据库回归](../../go/internal/quota/persistence_cause_test.go) 和既有 localedit 准备失败回归。管理后台任务已交付，当前偏好任务仅持有 auth/preferences/schema，不占 quota；本轮先核对归属后修改。连续生图计费生命周期红色回归仍保留未提交，未纳入本切片。

首轮 11 个真实 PostgreSQL 约束场景全部复现 cause 丢失（1.050 秒），覆盖 Reserve/Settle/Release/MarkUnknown 的账户、hold、事件写入；MarkUnknown 不写账户余额。修改以 errors.Join 保留原 apperr.Internal 与实际 error，覆盖同一账户更新写法的 Adjust 及共享 appendEvent，不新增错误类型或包装框架。预留负债不一致等纯业务错误没有伪造 cause，读取/锁定/账户初始化的转换不在本切片改变。

最终回归增加 Adjust 的两个写入边界，共 13 个场景。每例按自身商家安装 NOT VALID 约束，验证 errors.As 取得 23514 与原 ConstraintName，同时仍能取得应用 500；经真实 httpx.AbortErr 输出原 Detail，不包含约束名或 SQLSTATE。余额、事件数量不变，已有 hold 保持 reserved、新预留失败不留行。约束在每例 cleanup 清除，测试使用 quota 包数据库，不改开发库。

最终 quota 整包通过（4.366 秒）；局部编辑、Graph、连续生图相关消费者组合分别通过（0.911 / 5.182 / 6.447 秒）。局部编辑准备失败现在明确要求原 SQLSTATE 与约束名，并继续验证零调用、过期恢复和新 attempt 结算。没有把该组合计为三个消费者整包；imagesession 的另一个计费取消红色复现尚未修复。quota/localedit 标准 go vet 通过，主代理自审完整任务 diff 和错误消费者，当前 docs-check 与本切片空白检查通过。共享 Web 修改中的空白问题不纳入本切片修复或提交。


## 首次并发建账的事务恢复

本切片由主代理独占 quota/service.go、新增 account_concurrency_test.go 与本历史记录。偏好任务仍持有 auth/preferences/schema，未进入其范围；共享 Agent eval 正在运行，未占用或重启其资源。现有 EnsureAccount、Adjust、Reserve 均通过 ensureAndLockAccount 维护账户初始化，没有要求各生成调用者自行防重。

回归使用真实 quota 测试数据库，在两个事务的首次缺行查询后设置测试回调屏障，确保两者都读到不存在后再插入。旧实现实测一个调用返回“锁定额度账户失败”（0.733 秒整组）：创建的唯一键错误被捕获后继续在原事务查询。PostgreSQL 唯一键错误会使该事务失效，重读不能恢复事务。修复在 INSERT 上仅对 merchant_id 使用 ON CONFLICT DO NOTHING；未插入者继续 FOR UPDATE 读取胜出账户，实际插入者才写试用事件。其他创建错误保留应用错误与原数据库原因，没有全事务自动重试或调用者兜底。

验收验证两个调用均成功，账户 available=75、reserved=0，试用种子事件严格一笔且金额为 75。测试回调只固定时序，数据库查询、冲突与提交均实际执行；结束移除回调。额度整包通过（4.430 秒），包括已有并发预留、同键幂等及余额限制回归；标准 go vet 通过。完整自审覆盖新增文件与服务 diff。未改 schema、公共接口或 Provider；连续生图计费身份的未提交红色回归仍待 schema 所有权释放，不能据额度整包通过宣称它已修复。


## 额度事件唯一键故障的错误传播

本切片主代理负责 quota/service.go、persistence_cause_test.go 与本记录。当前偏好任务仍实际修改 schema/identity.go 和 constraints.go，连续生图计费身份切片继续保护其所有权；共享 Agent eval 运行中，未修改或重启相关资源。

沿 Adjust、Reserve、Settle、Release、MarkUnknown 和建账入口核对后，正常重复请求已由账户行锁下的既有事件、hold 和终态判断处理。appendEvent 却将任意 23505 转成 nil，包含非业务幂等冲突。真实 PG 回归用限定单商家的临时唯一索引拒绝新增事件，走实际 Reserve 后复现原错误仅为 commit unexpectedly resulted in rollback（0.742 秒整组）。这是数据库故障注入证据，不证明正常重放会产生重复事件，也没有证明已发生余额损坏。

删除 appendEvent 的唯一键吞错分支及无其他使用者的 isUniqueViolation helper，让原有 errors.Join 同样覆盖唯一键错误。数据库插入 helper 只报告持久化结果，幂等判定保留在拥有账户锁和请求语义的业务入口。未使用 ON CONFLICT 忽略事件插入，避免将无法解释的账本冲突当成功。

最终 quota 整包通过（4.051 秒），标准 go vet 通过。新增回归验证原 SQLSTATE=23505 与 ConstraintName、余额 available=100/reserved=0、零 hold、仅原种子事件，证明故障仍整笔回滚且原因保留；临时索引由 cleanup 清除。已有并发与各终态重放回归随整包执行。主代理自审完整服务与测试 diff，删除 helper 后已扫描包内残留。未执行真实模型或扩大为全后端验收。
