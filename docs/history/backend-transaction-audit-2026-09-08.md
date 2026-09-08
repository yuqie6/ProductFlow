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
| 额度事件插入唯一键错误被吞没，事务提交只返回笼统回滚错误 | appendEvent 统一返回数据库原因；重放仍由原账户锁及业务状态负责 | PG 唯一约束故障保留 23505 与约束名，余额/hold/事件整体回滚；额度整包通过 | 4ba18e26 |
| Reserve 同键同金额、异价格版本仍返回旧 hold 成功 | 原账户锁内同时校验已存金额与版本，版本冲突返回 409 | PG 四种 hold 生命周期复现并修复；同版本重放及账本不变通过 | 87131c34 |
| 配方已有商品依赖，却另行查询/锁定 products 并复制归属范围 | 四个用例通过既有 Products.Lock/LoadSource 取得商品身份与事实版本 | recipe 整包商家隔离、应用和回放通过；第二连接锁竞争与回滚释放、商品配方创建消费者通过 | 88dd8664 |
| 配方继承视觉版本时将数据库错误当作可选缺失，后续写入只返回事务失效 | 仅 NotFound 可省略，原始读取失败直接返回 | 独立 PG 创建/追加原始 42P01、零部分版本、恢复后可选缺失合同通过 | 4b20a59b |
| 局部编辑已调用 Provider，但 unknown/调用后取消/恢复仍忽略缺失预留 | 三个入口共用必须找到原 hold 的 markEditQuotaUnknown；缺失只允许调用前 Release | PG 隐藏原键复现终态错误成功；修复后拒绝收口，恢复原键后待核账且零重调 | 0ea5fff4 |
| 连续生图新重试预留被旧 effect 误判为已调用，取消错误保留额度 | task/effect 持久化 billing_seq，按准确当前键与当前轮次 effect 收口 | 正常两次手动重试三种终态、迁移、回滚、整包验证通过；未运行开发库迁移 | 6444b51a |
| 连续生图 unknown/取消在准确计费键缺失时仍提交终态 | 删除缺失预留兜底，MarkUnknown/Release 错误原样回滚 | 初始/手动重试四个红色场景修复；八种终态组合、恢复原键与整包通过 | `5636f688` |
| Graph 付费调用缺预留仍提交 unknown，通用 effect 又不能直接区分非付费调用 | effect 保存可空 quota_key，准备与预留同事务；未知、取消、恢复复用准确额度身份 | 真实调用故障复现与回滚、非付费文稿/合成、迁移及 Graph 整包通过 | `1dedc721` |
| Graph/连续生图/局部编辑的商家归属读取丢失数据库及取消原因 | 各既有查询保留 apperr 文案并 Join 原错误，不增加共享查询或第二份规则 | 三个真实数据库读取故障和取消回归；Graph 终态事务回滚及恢复 | `19889e5a` |
| Graph 商品服务经 context 隐式传播，事务函数及测试 helper 隐藏装配前置 | Service/Executor 保留 Products；Graph 事务函数和商品/配方调用显式传入既有 ProductGuard，删除 context 通道 | 商品/配方、跨商家和事务/执行/恢复合同验收见本节 | `fc193c91` |
| 原图事务吞掉可选交付错误，SQL 故障污染原图、普通错误留下交付写入 | Graph 用原 tx.WithGorm 保存点隔离交付；delivery 只跳过业务校验失败，读取错误返回 | 实际交付服务的 dispatch SQL/源图读取/暂存后错误及外层结算回滚；原图结算与重排幂等 | `80808850` |
| 两个交付创建入口复制唯一冲突后回读逻辑，并发时事务失效返回 25P02 | delivery.createOrReuseJob 统一原图/规格去重与信封暂存，精确 OnConflict 只处理既有唯一键 | 主动/自动三个并发组合和首个暂存 SQL 失败后竞争者接续；一作业一信封 | `9a7c0bb8` |

| Graph 终态同步吞掉 Agent 投影写错，信封消费后确认单滞留 | Executor 返回既有 AfterRunStatus 事务错误，终态重投只补同步 | 真实 PG 23514 保留信封 pending，解除后确认单 succeeded/信封 consumed，Provider 调用与额度不变 | `8cdc5973` |

| Graph unknown 缺确认单枚举及 Task 投影，UI 以 confirmed 推导并持续轮询 | Agent 原同步事务保存 unknown；商品 Goal 与全局 Task 遵守既有终态合同，Web 消费明确状态 | 商品/全局 PG 投影、原子回滚、HTTP 确认重放、Graph 未知信封恢复及枚举升级 | `c7426d38` |

| 旧 Graph 回调覆盖较新确认单的 Task，读取还跳过待确认新请求 | Agent 持 Task 锁后按原最新确认单顺序核对投影权；读取选择相同请求集合 | 新请求待确认/运行中时旧终态不覆盖，历史请求仍同步，真实并发提交后旧回调不夺权 | `dd4618e8` |

| 同一 GraphRun 的旧 running 读取在终态同步后回写 confirmed/running | Agent 确认单行锁串行化同步，持锁后读取 Graph；保持取消的 Graph → request 顺序 | PostgreSQL 成功/未知/实际取消并发，旧读取释放后最终请求和 Task 保持终态投影 | 随本次提交 |

局部编辑的成功资产提交、普通终态、取消、过期未知和调用前准备分别有明确事务入口；这些入口调用 `quota.Service`，不直接改额度账户或账本表。外部 `Provider.Edit` 仍位于事务之外。失败事务中的媒体文件沿已有 compensation 回滚。没有新增状态、数据库列、并行账本或兼容读取路径。

## 已检查调用链与保留的边界

| 业务链 | 当前实现证据与结论 | 尚未证明的部分 |
|---|---|---|
| 商品与图创建 | `product.Service.CreateDirect → createWithGraph/createCanonical → graph.WriteTx`，商品、资产与图在调用者的同一事务组合；媒体沿 compensation 处理。保留 product 的创建 owner。 | 各种中断点的全组合故障注入尚未逐一覆盖；不从包测试推定整条真实商品交付签收。 |
| 图修改 | `graph.Service → WriteTx → graph command/project`；商品、配方和 Agent 通过 Graph 写入口组合，Graph 用 ProductGuard 访问商品。 | 商品依赖已通过 Service/Executor 字段和事务参数显式传递；未复现原 context 方式导致的越权。 |
| 图执行 | `ExecuteRun → executeClaimedNode → runClaimedNode → prepare/finishProviderCall → persist*Artifact` 已有 lease、attempt、effect、投影晋升分层。 | 取消已在持锁命令收口；过期恢复已在同一事务收口；图像调用前 prepare/Reserve 已组合事务；成功已与结算同事务，明确失败终态也已在命令内处理；付费成功结算已要求 hold，本地合成显式跳过结算；其他终态的缺失 hold 合同仍需核实，需以数据库故障和竞争证明后修改；只改文件布局没有验收价值。 |
| 连续生图 | `Execute → runGeneration → ensureEffect → Generate → saveCandidate → markEffect → finish*`；每次信封处理一个批次，已有 applied 批次跳过 Provider。 | billing sequence 绑定、部分候选已保存后的失败、effect 写失败及额度最终收口需继续审计；不能直接套用 node/attempt 的额度键规则。 |
| 局部编辑 | 本轮覆盖 HTTP 创建/提交夹具、worker、task/attempt/asset/hold/账户、恢复与 queue.Consume。 | 未进行 SIGKILL 或真实 Provider 调用；进程崩溃按可持久化边界和实际恢复函数注入验证。 |
| 交付 | 已有本地 Render、资产派生、attempt 条件写入和恢复；此次收口 worker 失败终态错误传播。 | 原图/媒体存储的各类真实 IO 故障、交付性能和全部采用流程未作本轮专项验收。 |
| Agent 控制 | Node manager 持进程内调度，TurnRuntime 持 lease/checkpoint，工具经 Go effect reconciliation；checkpoint 写失败会中止执行。保留该分工。 | 已分别验证 Go 辅助进程崩溃和 Node 替身服务重启；后续整包实际通过真实 Node → Go → PostgreSQL 的问题回答恢复（一/两个问题）。完整联动 crash 矩阵仍未验收，测试 Provider 不代表真实模型质量。 |

## 后续优先级

优先级按可能损害排序；修改频率与扩散范围目前只有静态调用者证据，没有生产统计。

Graph unknown 的确认单/Task 投影缺口已在后续切片修复，实际数据库证据覆盖商品与全局场景、用户拥有状态和写失败回滚。确认单枚举需迁移后启用，尚未迁移共享开发库；跨多次运行的旧 Graph 回调覆盖已在后续切片以 Task 锁后最新确认单校验修复。同一 run 的旧读取覆盖终态已在后续切片复现并以确认单锁后读源修复；旧待确认请求的取消、尚未产生新确认单的新 Agent Turn 仍待专项核实，不能宣称全部控制链路已有同一围栏。

1. **高：Graph 和连续生图的终态/额度事务分裂。** Graph 取消已归入持锁命令，Graph 过期恢复已同步额度；图像成功持久化已与结算同事务；明确失败终态额度已归入命令，付费成功结算已要求 hold；付费未知与调用后取消已依据 effect 的明确额度身份要求 hold，非付费 effect 跳过图像账本；释放入口的缺失 hold 合同仍待进一步核实；`imagesession/service.go`、`execute.go`、`quota_wire.go` 的 billing sequence 与终态组合需继续沿真实调用顺序核实。`imagesession.finishFailed` 的旧 attempt 越界已修复，成功/未知/过期恢复的额度事务已收敛，创建、取消和手工重试已改为用例内组合事务；billing sequence 已在后续切片绑定 task/effect 并删除前缀最新预留查询；unknown/release 的缺失 hold 容忍已在后续切片删除；准确预留缺失时终态回滚。当前属于已确认的代码风险，尚未全部做数据库故障复现和修复。不得宣称所有入口已原子收口。
2. **中：局部编辑未知/释放的缺失 hold 合同与额度底层错误。** 当前唯一运行时 Reserve 入口与 provider_pending 同事务，不产生 claimed + hold；新增真实数据库准备失败后恢复执行回归确认旧 attempt 零 hold、新 attempt 正常结算，不为历史组合新增恢复分支。unknown 的缺失 hold 容忍已在后续切片删除并覆盖终态/取消/恢复；调用准备前 Release 可无 hold，本轮已核对 Execute 的全部 failed 分支均位于 prepareProviderCall 成功之前，Provider 返回错误及结果持久化失败均走 unknown，未发现生产调用后明确失败 Release 路径。成功结算已拒绝缺 hold。quota 的账户/hold/事件写入原因丢失已在后续切片修复并验证 HTTP 文案保持；账户初始化、锁定及读取错误转换仍待核实。
3. **Graph context 服务依赖已迁移。** Service/Executor 继续持有 Products；Graph 的命令、读取、运行、晋升和恢复函数显式传递 ProductGuard；商品创建/事实更新、配方提取/应用/投影一并更新。WithProductGuard、context key 与读取器已删除，既有事务所有权和商家校验规则保持。此为依赖边界改善，不宣称修复已复现越权；最终验证及实际限制见本记录末节。
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


## 预留幂等请求的价格版本一致性

本切片由主代理负责 quota/service.go、新增 reserve_contract_test.go 与本记录。当前偏好任务持有 auth/schema，dashboard 任务开始调查商品列表与任务/交付读模型；未修改其文件或运行资源。

价格目录服务合同允许 Reserve 选用目录中的版本，hold 和事件保存 PriceVersionID。原重放分支只检查 AmountUnits，同键同金额但不同有效版本会返回旧版本 hold 且 err=nil。真实 PG 测试新增第二个合法目录版本，分别经实际 Reserve、Settle、Release、MarkUnknown 到达四种状态，全部复现错误接受（0.973 秒整组）。这证明服务边界合同缺口；当前五个生产调用者仍固定传 DefaultPriceVersionID，没有证据表明现有生成入口已触发异版本重放，也不将此记为已发生错扣金额。

修复在原账户锁下返回已有 hold 之前比较持久化版本与规范化后的请求版本，不一致返回已有 apperr.Conflict。金额冲突原合同保持；空字符串仍归一到默认版本。调用者不负责读取 hold 内部字段或自行校验原请求，不新增存储/API。没有改成按版本生成不同幂等键，以免将同一请求变成新的扣费操作。

最终 quota 整包通过（4.527 秒），标准 go vet 通过。四场景均验证异版本 409、原版本带首尾空格的重放成功、原 hold ID/版本保持、账户余额和事件数量不变、总 hold 严格一条。测试只使用包测试数据库的正常建账与状态 API；没有改写 hold 状态或使用真实 Provider。主代理复核五个生产 Reserve 调用者及完整切片 diff；本次局部合同证据不代表动态定价或全部入口展示/计价已交付。


## 配方商品访问归入既有 ProductGuard

主代理沿 Graph context 外部装配继续核对，确认 recipe.Service 已持有 Products，却在 getProductTarget 独立查询 products 的 id/current_fact_set_version_id、ScopeMerchant 和 FOR UPDATE。没有复现新的越权写入；这是已证实的规则重复与内部表耦合。本切片由主代理独占 recipe/service.go、store.go、新增 product_dependency_test.go 与本记录，未修改 dashboard 正在调查的 product 文件或偏好任务的 schema。

Create、Append、Preview、Apply 改为通过实例方法消费现有 Products。读路径用 LoadSource，nil 按既有跨商家 NotFound 返回；Apply 先使用 Lock，再从同一 GORM 事务读取来源。无依赖显式返回 Internal，调用者配置中的 Products 不再只服务于 Graph context。商品身份、merchant scope、行锁及当前事实字段的 SQL 留在 product.GraphGuard；recipe 只映射已有 SourceProduct 字段。没有新增接口或表。锁定路径增加一次摘要读取，这是复用当前分开的 Lock/LoadSource 合同的代价，未据此扩展新接口。

真实 PG 依赖回归通过包装并实际调用 product.GraphGuard 观察服务边界，Preview 不锁、Apply 锁定并读取。LoadSource 阶段另一连接使用 FOR UPDATE NOWAIT 得到 55P03，证明原组合事务仍持有商品锁；后续缺配方使 Apply 回滚，另一连接随即可锁。测试并不模拟锁结果。recipe 整包通过（3.025 秒），包含实际 HTTP 正常提取/预览/应用/回放及跨商家 404/零写入；标准 go vet 通过。product 的 TestRecipeCreation 组合通过（1.564 秒），覆盖配方创建、并发确认、HTTP 和提交失败文件回滚。未运行或声称 product 整包。

完整自审及非测试扫描确认 recipe 不再引用 schema.Products、FROM products 或 current_fact_set_version_id。Graph 的 WithProductGuard 仍由 product/recipe 与自身 Service/Executor/recovery 装配；本次没有删除该隐式依赖，也没有把配方边界收敛当作 Graph context 迁移完成。后续对其整体迁移需要同时协调 product 的外部调用者。另发现 preferredVisualFromLive 将视觉版本查询的全部错误按缺失处理，仍待独立数据库故障验证，本次不顺带改变配方视觉继承语义。


## 配方视觉继承区分缺失与读取失败

本轮主代理独占 recipe/service.go、新增 visual_read_failure_test.go 与本记录。当前 auth/schema/Web 偏好与 dashboard 修改保持不动，测试使用独立 pf_recipe_visual_* 数据库；无 Provider 调用。

沿创建/追加 → preferredVisualFromLive → graph.TryLive → visualSystemVersionExists 检查，原实现将全部版本查询错误视为可选缺失。回归先通过实际商品创建与 Graph ApplyChangeSet 设置视觉版本引用，再在一次性库中暂时重命名视觉版本表。旧实现 Create 实测只返回 25P02（事务已失效），原始 42P01 丢失（1.860 秒整组）；并没有成功保存丢失继承信息的配方。故障影响是原因不可解释，不能把它描述为已证实数据损坏。

修复复用 apperr.NotFound 分类，只有确实缺失可省略，其余错误原样返回；同一 helper 覆盖 Create 和 Append，不增加重试或兜底。测试验证 Create 的原始 42P01 与零配方，恢复表后不存在的引用仍成功省略且投影 preferred_visual_system_version_id=nil；随后 Append 再遇读取故障仍保留 42P01，当前版本 ID 不变且只有原版本。临时数据库及其中故障表由 IsolatedMigrated 清理。

最终 recipe 整包通过（4.271 秒），标准 go vet 通过。新增测试完整自审确认使用真实图修改入口、两个配方用例及实际 PG 读写；本切片不改 HTTP schema、视觉版本继承策略或商品模块，也没有宣称完成 Graph context 迁移及整体后端验收。


## 额度与配方切片后的固定提交集成验证

主代理在本地 shared clone 固定到 4b20a59b8d8c39b404948b43de4a3f5841686110，保留 Git 元数据。核对 agent-service/package.json 与 pnpm-lock.yaml 一致后链接现有 node_modules；环境脚本来自当前工作区，实际被测代码全部来自固定 checkout。未复制其他任务 auth/schema/Web 未提交修改，也未纳入连续生图 billing_lifecycle_test.go 红色复现。该遗漏明确限定本表适用范围，不能据此声称工作区或计费取消缺陷通过。

首轮顺序运行 quota、recipe、graph、localedit、imagesession、delivery、agent（-p 1 -count=1 -timeout 180s -json）。Graph FAIL 92.054 秒，其余包通过。失败定位为 TestCancelGraphQuotaIsAtomicForBothEntryPoints 四个子例仍比较 err.Error()==通用文案，额度 errors.Join 变更后实际错误含原 23514 及约束名；未发现取消原子性实现的新失败。主代理仅改该测试，同时断言 apperr 500/Detail 和 pgconn 23514/ConstraintName，保留运行/节点/attempt/余额/事件/回调回滚及解除故障后重复取消断言。单文件补丁叠加到固定 checkout，Graph 重跑整包通过。

| 包 | 最终结果 | 耗时秒 | pass 测试事件 | skip 测试事件 |
|---|---|---:|---:|---:|
| quota | PASS，原固定提交 | 4.234 | 44 | 0 |
| recipe | PASS，原固定提交 | 4.277 | 19 | 0 |
| graph | PASS，固定提交加消费者断言补丁 | 98.447 | 296 | 1 |
| localedit | PASS，原固定提交 | 8.037 | 41 | 0 |
| imagesession | PASS，原固定提交，不含未提交计费复现 | 46.072 | 115 | 5 |
| delivery | PASS，原固定提交 | 7.469 | 44 | 0 |
| agent | PASS，原固定提交 | 88.063 | 362 | 8 |

合计 921 个 pass 事件、14 个 skip 事件，父测试与子测试均计入，不是 921 个独立业务场景。跳过项为 Graph 目标规模查询门，imagesession 四个规模/HTTP/SSE 门与子进程 helper，Agent 的浏览器/容量/真实 L2 eval/查询计划/HTTP 读门及两个子进程 helper。没有因缺 PG 或缺 Node 跳过关键持久化测试。Agent TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult 两场景通过（6.71 秒），使用真实 Node 进程重启与本地模拟 Provider；TestSIGKILLLeaseHolderAgainstGoPG 的 model_start/mutation/approval/turn_end 四场景通过（1.42 秒）。不代表真实模型质量或所有故障点已覆盖。

保留原始首轮日志 `/tmp/pf-backend-integration-4b20a59b.jsonl` 及 Graph 复验 `/tmp/pf-backend-integration-4b20a59b-graph-final.jsonl`，不覆盖首轮 FAIL。Graph go vet -stdversion=false 通过；已知 testing.Context/Chdir 与 go.mod 版本声明问题未在本轮修复，不能将该命令表述为标准 vet 全过。最终自审固定 checkout 的唯一 tracked 补丁为本次消费者测试，node_modules 为本轮链接；测试终止后清理临时 checkout 与链接，保留日志。该集成修复按仓库规则选择性提交，不包含其他任务文件或计费红色复现。

收尾共享 just docs-check FAIL：merchant-platform.md 已链接 tasks/archive/saas-preferences-settings.md，而归档文件尚未出现；这是其他任务正在调整的文档路径，未代为修复。将本次历史记录复制进固定 checkout 后，独立文档合同检查 PASS。两个结果分别保留，不将独立快照通过称为共享工作区通过。


## 局部编辑 unknown 必须保留对应额度负债

本轮复核偏好任务仍持有实际 schema 修改，故连续生图计费身份不越界实施。主代理独占 localedit/quota_wire.go、recovery_test.go、新增 missing_unknown_hold_test.go 与本记录，继续既有局部编辑缺 hold 审计。

真实 Execute 在 Provider 调用前已将预留和 provider boundary 原子提交，unknown 应将这一预留标记为 pending_reconciliation。原 markEditQuotaUnknown、调用后取消、恢复却通过通用 finalizeQuotaIgnoreMissing 吞掉 NotFound。新增回归经实际创建/提交/Execute 进入 Provider，使用测试 Provider 返回超时；返回时将已提交 hold 的键临时改名，使原精确键不可查，而余额和事件保持。旧实现 Execute 实测返回 nil（0.824 秒整组），证明缺待核账记录也能被当作完成。此为持久化关联故障注入，不声称正常事务会自行丢失 hold。

三个入口统一复用 markEditQuotaUnknown，原 quota.MarkUnknown 错误直接返回，终态/取消/恢复事务不提交。恢复原 hold 键后过期恢复进入 unknown、hold 为 pending_reconciliation，再次 Execute 不重调 Provider（总调用一次）。Release 的缺失容忍 helper 改名为 releaseQuotaIgnoreMissing，注释对应真实调用准备前尚无 hold 的情况，不再以测试夹具作为生产行为理由。未新增表、阶段、账本或自动重试。

整包初跑暴露旧 markStaleLocalEdit 夹具把 phase 直接置为 provider_call 却不建预留；补齐跨过 Provider 边界的夹具预留，claimed 夹具仍不建账。失败运行在持久测试库留下两条缺预留任务，引发后续批量恢复扫描失败。按本轮输出核对后仅删除这两个测试 task ID 及其数据库级联依赖，不重置数据库或修改共享服务。初次清理连接方式不适配环境 URL，未执行删除；改用 libpq 的独立连接环境字段后确认 DELETE 2。

最终 localedit 整包 PASS 8.162 秒，标准 go vet PASS；之前失败不隐藏。原成功/失败/取消/恢复、跨商家、旧 attempt 隔离与准备失败回归随整包执行。完整自审确认只改所列文件，非测试代码仅保留一个 quota.MarkUnknown 调用，无旧通用 helper 残留。调用后明确失败 Release 的缺失合同、额度读取错误原因和整体 Graph context 迁移仍不在本切片宣称完成。


## 连续生图持久化计费身份与历史 effect 隔离

偏好任务已随归档释放 schema；当前 dashboard 明确只占 product 读模型/Web，不改 schema 或连续生图后端。主代理独占 imagesession 的 Service/Executor/额度查询及相关测试、schema/models.go 中两种图片任务模型和本记录。此前保留的 billing_lifecycle_test.go 红色复现本轮纳入交付，不再作为未提交缺陷悬置。

schema 增加 task 与 provider effect 的 billing_seq（integer/not null/default 0）。初始 Generate 使用 0，对应既有初始 key；Retry 沿原键格式选择序号，并在 Reserve、任务 queued、dispatch 的同一事务保存序号。后续收口直接读取已保存序号，不根据当前 attempts、展示阶段、时间戳或历史 hold 排序推算。ensureEffect 在已有 task 锁和 attempt fence 下，将实际准备的 effect 绑定 task.BillingSeq；failed 批次再次准备时更新计费归属，已 applied 的批次仍复用，不引入第二份 effect 或重复调用机制。

activeQuotaKey 删除 LIKE 前缀与 created_at DESC，查询准确 generationQuotaKey(taskID, BillingSeq)。providerLedgerExists 只统计当前 task 序号的 effects，旧轮次调用不再成为新预留已消费的证据。所有最终额度操作继续复用 quota.Service，公开 DTO/HTTP/Node 合同未增加计费内部字段，没有新账本或兼容读取路径。

实际 Generate → 限流自动重试耗尽 → 手动 Retry 再失败 → 第二次 Retry 的三场景通过：尚未 claim 就取消时新 hold released，成功时 settled，超时时 pending_reconciliation；task 保存的序号与预留一致，只有实际调用的当前轮次有 effect。新增迁移回归用真实任务与 effect 删除两列后 schema.Apply，验证初始序号 0，再存 7 后重复 Apply 不覆盖；已有重试 dispatch 故障回归强化为序号与状态一起回滚，成功提交才保存序号 1。缺 hold 表回归原来只有随机 task ID，现改为真实 Generate 创建身份，仍要求原 42P01。

共享工作区首次整包编译受 dashboard 正在修改的 operator_records.go 暂缺 gorm 导入阻挡，未修改该文件。随后使用固定 0ea5fff4 本地 shared clone，加本切片全部补丁验收。首轮仅旧随机 task ID 夹具失败（48.622 秒），修正后第二轮 TestImageCheckpointTransactionBoundaries/stale_consumer 定时重投断言失败（50.891 秒，Sent=0）；没有证据把它归因于计费修改，原失败日志保留。该交接测试、计费生命周期/迁移与创建重试事务组合针对性复验通过（10.912 秒），标准 imagesession go vet 通过；最终整包通过（51.275 秒），包含原批次恢复、旧 worker/取消竞争、跨商家及实际请求构造测试。未将一次重投时序失败改成 skip 或放宽断言。

原日志 `/tmp/pf-billing-identity-suite.log`、`/tmp/pf-billing-identity-suite-final.log` 与最终 `/tmp/pf-billing-identity-suite-accepted.log` 保留。最终审查独立 checkout 只有本任务代码/测试补丁；清理 checkout 后选择性提交。没有启动 Provider 真实费用或重启共享服务。

迁移边界：本次未执行开发/生产数据库迁移。对当前环境 DATABASE_URL 所指开发库的只读查询确认 billing_seq 尚不存在，queued/running 且有 reserved/pending_reconciliation 重试预留的任务数为 0。该观察只代表查询当时该库。默认 0 不会重建旧代码已受理的活动重试身份；在其他环境应用迁移前，仍须核实并收口这类活动执行，不能把新列默认值当作旧计费轮次迁移。没有新增旧数据修复命令。当前 unknown/release 的缺失 hold 容忍与整体 Graph context 迁移仍是独立剩余项。


## 连续生图终态不得忽略缺失预留

本轮由主代理独占 imagesession/quota_wire.go、success_hold_test.go 与本记录；dashboard 继续持有 product/Web，未触碰其修改。Generate 已在任务入队前原子预留，Retry 也在排队前绑定新预留；与局部编辑调用准备前可能尚无预留的合同不同，连续生图的有效排队任务已有预留身份。原 finalizeQuotaIgnoreMissing 以直插测试夹具为理由吞掉 unknown/Release 的 NotFound，实际会把无法关联额度的终态当成功。

扩展既有独立 PG 预留故障回归：初始及手动 Retry 两类计费轮次，各测 succeeded/failed/unknown/cancelled。原实现新增四场景全部复现 nil 错误（9.912 秒整组）。测试仅临时改名本轮 hold 键，保持余额与历史预留，验证不能退回初始已结算 hold 或其他轮次。删除通用吞错 helper，两个 finalizer 直接返回 quota.MarkUnknown/Release 错误；准确键和事务入口继续复用上一切片。已有 hold 的幂等仍由 quota.Service 处理。

最终八个组合验证缺失时 task 仍 running、原 active attempt 不变、无 finished/result 字段、余额不变；恢复同一 hold 键后，四类终态分别落到 settled/settled/pending_reconciliation/released。当前 checkout 的 imagesession 整包 PASS 56.508 秒，包含既有恢复、取消/重投、旧 worker 和计费生命周期回归；标准 go vet 与 just docs-check PASS。没有为旧直插夹具放宽合同，也未新增表、状态或第二套恢复入口。完整自审确认 finalizeQuotaIgnoreMissing 在 imagesession 无残留，原始日志保留 `/tmp/pf-image-missinghold-suite.log`。

本次没有复现正常事务自行丢失 hold，证据是持久化关联故障时终态必须拒绝提交。schema 迁移应用边界继续沿上一切片说明；Graph context 依赖及更广的执行职责审计仍未完成，整包通过不等于整体后端交付签收。


## Graph effect 明确记录是否预留图像额度

主代理独占 Graph 调用准备、effect、额度收口和对应回归，以及 schema/models.go 的 WorkflowGraphProviderEffects 单一字段与本记录。dashboard 与 eval 的文件及运行资源保持原归属。真实调用链显示文稿、付费生图和本地 subject_compose 共用 effect 机制；原 markImageQuotaUnknown 对所有入口计算图像键并吞掉 NotFound。直接要求所有 effect 有图像预留会破坏合法的非付费调用。

新增回归经实际 callImageProvider 准备与 Reserve，在测试 Provider 返回超时前临时改名准确 hold 键。旧实现接受 unknown（针对性首轮 FAIL 0.964 秒）。effect 新增可空 quota_key：付费入口在原准备/Reserve 事务写入准确键，文稿与本地合成明确写 nil；failed effect 再准备时同步更新归属。未知收口按 node/attempt 查询 intent，付费预留缺失和读取失败原样返回，非付费跳过图像账本。调用后取消复用同一收口，恢复继续消费原 markNodeUnknown。不按 Provider 名称或请求 JSON 猜测收费，不增加第二份账本。

故障回归确认缺 hold 时 run/node 保持 running、effect 保持 pending；恢复准确键后原未知事务成功，Provider 仅调用一次。取消/恢复/三种 unknown 原子性回归补齐真实已跨调用边界的 effect 夹具，保留原数据库约束故障和回滚断言。首次 Graph 整包 FAIL 94.020 秒源于旧夹具只写阶段或缺额度身份；没有通过生产兜底继续兼容这些夹具。四条本轮失败留下的无意图运行按准确 ID 删除，未重置共享测试库。

独立 PostgreSQL 包数据库的最终 Graph 整包 PASS 95.368 秒；新增回滚断言及迁移、非付费合成、恢复组合随后 PASS 4.896 秒。迁移回归在一次性真实数据库删除新列、schema.Apply 创建空列，再写准确键并重复 Apply 验证不覆盖；它只证明 schema 创建与幂等，不证明旧付费数据身份重建。go vet -stdversion=false PASS；标准 vet 的既有 Go 版本声明问题仍未解决。首轮及最终日志保留 /tmp/pf-graph-quota-identity.log 与 /tmp/pf-graph-quota-identity-final.log。完整 diff 与新增迁移文件已自审，旧通用 finalizer 名称无残留，保留的 releaseQuotaIgnoreMissing 只服务 Release 的调用准备前缺预留情形；未宣称全部 Release 入口审计完成。

两个本轮独立验证数据库已删除。共享开发库只读确认 quota_key 尚不存在，查询当时带 reserved/pending_reconciliation 图像预留的 queued/running Graph 节点为 0；没有执行开发或生产迁移。历史 effect 新列为 NULL 不能证明其原调用不收费，应用迁移前必须核实并收口旧活动付费执行。本切片没有历史回填或兼容读取路径，没有真实模型费用。Graph context 依赖与更广的节点职责收敛仍是后续工作。


## 商家归属读取保留数据库与取消原因

本轮主代理独占 graph/localedit/imagesession 的 quota_wire.go、各自新增 merchant_lookup_error_test.go 与本记录。其他任务仍占 product 聚合读模型、Web 与 Agent eval，共享 API/worker/dispatcher/Agent 服务保持运行；本轮没有使用其运行数据。

重新追踪 Graph Release 的全部生产调用：failClaimedNode 在 provider boundary 前释放；failGraphRun 在边界后转入 markNodeUnknown；恢复在 nodeSafeToRequeue 分支释放；取消有 effect 时走未知收口。没有证据要求删除调用前缺预留的容忍，保留原机制。继续沿额度归属读取发现 Graph merchantIDForGraphRun、局部编辑 merchantIDForProduct、连续生图 sessionMerchantID 将任意查询错误替换为固定 Internal，原始原因不可追踪。

三个一次性真实 PostgreSQL 数据库分别重命名被读表，原代码三个回归都丢失 42P01；恢复表后使用已取消 context，也都丢失 context.Canceled。首轮 Graph/localedit/imagesession 分别 FAIL 6.025/2.015/1.950 秒。修复只在三处保留原 apperr 并 errors.Join 原因；没有改变商家身份、HTTP 状态及文案、数据归属策略或事务入口。现有 httpx.AbortErr 使用 errors.As 读取公开错误字段，不把 Joined 原因写入响应。

补充 Graph 实际 failClaimedNode：节点更新后读取商家失败，原 42P01 可提取，运行和节点仍 running、active attempt 保持；恢复表后再次失败收口进入 failed。故障表只存在于一次性测试库，清理由 IsolatedMigrated 负责。针对性三个包 PASS 3.036/1.783/1.983 秒；所有新增测试都实际访问数据库，没有跳过。

当前 checkout 三包顺序整包最终全部通过：Graph 103.697 秒、localedit 9.681 秒、imagesession 61.649 秒，日志 /tmp/pf-merchant-read-cause-suite.log。Graph go vet -stdversion=false、另两包标准 go vet 与 just docs-check 通过；没有宣称修复 Graph 的既有标准 vet 版本声明问题。自审三处生产修改与三个新增测试文件，未修改平台错误/HTTP 公共实现或其他任务文件；本次没有迁移、真实 Provider 调用或新增重试。Graph context 的显式依赖迁移、额度内部读取/锁错误与其余节点职责审计仍未完成。


## Graph 商品依赖从 context 迁为显式参数

本轮由主代理唯一负责 Graph 调用链、商品 app/workspace/fact_impact/intake/recipe_create 的调用装配、recipe/service.go 和本记录；当前 dashboard 只持有 product 聚合读模型/HTTP/DTO 与 Web，eval 持有其独立文件，修改路径不相交。所有权及运行进程已刷新；未重启共享服务。

静态调用图从 requireProductGuard 向上追踪到命令、投影、读图、运行终态晋升及恢复，证明隐式服务依赖贯穿多条事务链。保留已有 ProductGuard 接口及商品侧实现，Service.Products / Executor.Products 是外部装配入口，已有事务函数显式接收 ProductGuard；恢复沿原 products 参数继续传递。商品和配方在原 GORM 事务内提供依赖，不新增 Service 容器、事务框架或兼容重载。删除 WithProductGuard、productGuardCtxKey、productGuardFrom 和 guardCtx；context 继续携带请求取消、商家与执行 lease 等原有运行信息。

本次收敛的是依赖装配合同：事务调用者直接声明所需服务，Graph 内部函数不再假定调用前某个上层已修改 context；产品/配方入口不再需要安排注入的时机或维持带服务的 context 传播。原锁商品、归属统一 404、批量读取及写事务规则仍由既有 owner 实现。没有证据把旧方式定性为已发生的跨商家越权，也不以参数变化或文件行数作为业务收益。

验收固定到 19889e5a 的本地 shared clone，仅叠加本切片，Node 测试依赖链接已有 node_modules；不纳入其他任务未提交业务改动。全 Go 包编译检查通过（-run ^$，仅编译证据）；Graph/Agent go vet -stdversion=false 与 product/recipe 标准 go vet 通过。初次 Graph 整包 FAIL 97.608 秒，原因是 beginCommandTx 原经 context 隐式返回测试守卫，而辅助命令仍传 nil；已将这些调用显式绑定原 cmdTestProducts，未恢复任何生产兜底。商品整包 PASS 4.027 秒，配方整包 PASS 4.772 秒。

初轮 Agent 整包 FAIL 88.753 秒：TestAgentSSETimeToFirstEventP95 在 POST /api/v2/agent-sessions 创建通用会话时收到 500，尚未进入 Graph；检查 CreateSession 路径只写会话及投影，无 Graph 调用。该用例原代码未改，单独复验 PASS 1.347 秒；最终 Agent 整包 PASS 83.995 秒。没有捕获初轮 500 的底层原因，故保留为未解释的测试风险，不能声称通过本重构解决。没有放宽断言或改成 skip。

补齐命令夹具显式依赖后，最终 Graph 整包 PASS 107.733 秒。商品/配方首轮整包结果继续有效：相关代码、依赖及环境未变，后续只修改 Graph 内部测试的守卫参数。原事务不提交测试、revision/rebase 冲突、批量商品/fact 查询、跨商家读写、提案和运行消费者、Provider 请求证据、额度回滚及取消/恢复回归随包执行。全 Go 编译证据不能代替这些行为回归；本轮不提供真实模型质量或生产容量结论。

最终逐文件比对 34 个交付代码/测试文件与固定验收 checkout 一致，完整 diff 自审确认仅修改依赖参数、装配及必要测试，未改变 SQL、取锁次序、状态判断或 Provider 请求。代码扫描无旧 context 服务通道残留；历史段落保留当时事实。共享 just docs-check 曾因 dashboard 归档文件尚未出现失败，未修改该任务文件；归档完成后共享检查 PASS。日志保留 /tmp/pf-explicit-graph-suite.log、/tmp/pf-explicit-graph-final.log、/tmp/pf-explicit-agent-focus.log、/tmp/pf-explicit-agent-final.log 与 /tmp/pf-explicit-graph-compile.log。测试进程终止后清理本轮临时 checkout、依赖链接和转换脚本；未迁移数据库或启动真实 Provider。节点执行职责和其余生成入口的重复机制仍按实际因果继续审计。


## 原图成功事务隔离可选交付排队

本切片由主代理负责 Graph execute_node.go/providers.go、新增 delivery_queue_atomicity_test.go、delivery service.go/service_queue_test.go 与本记录。当前并行 eval 仅占自己的评测文件，共享业务服务未重启。沿节点执行 → 图片资产/artifact → current 晋升 → 交付排队 → 节点终态 → 额度结算追踪，Graph 已明确约定可选派生失败不影响原图，却在原图 GORM 事务内直接吞掉 QueueAfterImageSuccess 错误。原测试只有普通 Go 错误，无法证明真实数据库故障隔离。

实际图创建/运行使用本地测试 Provider、真实 product 写入与 delivery.Service，全部持久化发生在独立 PostgreSQL 测试库。给 dispatch 设置拒绝交付信封的约束，旧实现原图最终 unknown；交付服务成功写入作业与信封后再返回普通错误，旧实现保留 1 job + 1 dispatch。两场景红色复现整组 3.469 秒。测试编写时先修正 JSON 字段显式转 jsonb、artifact 经 node_run 关联运行的查询，及终态清除 active attempt 后须从 effect.quota_key 查询额度的断言；这些夹具错误不作为业务缺陷证据。

Graph 复用 tx.WithGorm 的嵌套保存点，仅包围可选交付调用：其写入出错则回滚保存点，原图事务继续；交付暂存成功仍受外层原图/额度事务最终提交约束。没有在事务外另开根连接、提交交付或增加独立队列机制。进一步沿 callee 核实 validateSource 的查询错误被统一返回 nil；源图 artifact 表读取故障实测仍将原图推为 unknown（FAIL 2.449 秒）。delivery 现在只跳过明确的 400 业务校验，原数据库读取错误返回给保存点入口，日志继续使用既有原错误。

针对性最终 PASS 8.325 秒：dispatch SQL 失败、暂存作业/信封后普通错误、源图读取失败均保留 succeeded 原图和准确 settled 预留，零交付作业/信封；解除故障后，对保留原图调用原交付入口两次只得到一组 job/dispatch。外层额度结算故障则同时回滚原图 artifact、交付作业与信封，运行保留 unknown，不允许保存点变成独立提交。保留既有 failDelivery 回归，并扩展交付测试证明可读取但非生成原图仍跳过排队。

这保证可选派生失败的隔离和既有入口的幂等重排，不提供自动补排调度，也不将 Graph succeeded 当作交付件已经完成。日志可解释排队故障，但本切片没有新增持久化失败投影或 UI 提示。没有真实模型费用、生产操作或迁移。

最终当前 checkout 的 Graph/交付整包分别 PASS 106.167/7.384 秒，包含源图业务校验跳过的回归；Graph go vet -stdversion=false、交付标准 go vet、just docs-check 与 diff 空白检查通过。保存点初版整包 103.145/7.619 秒仅代表加入源图错误传播前的代码，最终结果以上述复验为准。日志保留 /tmp/pf-optional-delivery-suite.log 和 /tmp/pf-optional-delivery-final.log。完整自审包含新增测试文件、所有 QueueAfterImageSuccess 生产调用者及实际数据库读取分支；故障约束和重命名仅作用于测试库，无运行中的本轮测试进程。按本切片文件选择性提交，不包含评测任务修改。


## 交付创建统一并发幂等与信封写入

本轮主代理独占 delivery/service.go、新增 submit_concurrency_test.go 与本记录。并行 eval 路径及共享 API/worker/dispatcher 进程保持不动。沿主动 Submit 与自动 QueueAfterImageSuccess 两个入口读取原图、校验规格、创建作业及 StageForActor，确认两处复制了先查缺失、Create、捕获任意 23505 再回读的实现。PostgreSQL 唯一冲突已使事务失效，原回读无法兑现幂等合同。

真实 PostgreSQL 回归在 GORM 查询后的观测 callback 设置并发屏障，只控制调度、不替代数据库读写。两个事务均读到作业不存在后才继续；submit/submit、submit/auto、auto/auto 三组合全部复现 25P02（首轮 FAIL 0.941 秒）。这证明影响为并发请求报错，未观察到重复作业或重复信封。

两个入口现在复用 delivery 内部 createOrReuseJob，继续使用既有 NormalizedSpec、jobFromModel、loadBySourceHash、模型和队列合同。在调用方原事务内，精确以 (source_asset_id,spec_hash) 的既有唯一约束执行 OnConflict DoNothing；只有插入者 StageForActor，冲突方回读胜出作业。其他约束错误直接返回，删除两处 23505 后回读路径。源图资格/商家校验仍由各入口在调用 helper 前完成，Graph 保存点与主动 Submit 外层事务保持原边界。

三个并发组合均得到一作业、一信封，主动重复请求返回同一 ID，只有一个 Created=true。增加首个插入者在信封暂存时触发真实 SELECT 1/0 的场景，错误保留 22012；其作业回滚，等待者接续插入并提交，一作业一信封。最终四组合针对性 PASS 1.050 秒。SubmitResult.Queued 原本在复用 queued/running 作业时也返回 true，本轮仅更正其注释，未更改返回行为。

该共享 owner 消除实际重复的并发机制；修改交付作业创建或信封暂存规则不再同步维护两个实现。没有增加新的幂等键、状态、schema、重试调度或模型调用。可选交付的自动补排与持久化故障提示仍不在本切片交付范围。

当前 checkout 交付整包 PASS 7.920 秒，Graph 可选交付错误/外层回滚消费者回归 PASS 8.291 秒；交付标准 go vet、just docs-check 与完整 diff 空白检查通过。生产修改仅在 delivery/service.go；新增测试的数据库读写与屏障、真实 SQL 故障、自身 callback 清理均已自审。Graph 本轮未重跑整包，范围由受影响的交付调用点决定，不将针对性通过表述为 Graph 全量验收。日志保留 /tmp/pf-delivery-concurrency-suite.log 和 /tmp/pf-delivery-concurrency-graph.log。测试进程已结束，无共享服务或其他任务文件变更，按独占文件提交。


## Graph 终态投影写错保留队列重投

本切片由主代理唯一负责 graph/execute.go、新增 status_projection_recovery_test.go 与本记录。并行 eval 文件保持原样，共享业务进程未重启。实际调用链为 queue.Consume → Executor.ExecuteRun → notifyRunStatus → agent.SyncGraphRunToTasks → 确认单/Task 投影。Agent 同步函数会返回数据库错误，但 notifyRunStatus 忽略整个事务结果，worker 返回 nil，队列将信封消费，确认单可能长期停在 confirmed。

只改变该错误传播边界：notifyRunStatus 返回既有 tx.WithGorm 的错误，ExecuteRun 的成功、明确失败、未知及已终态重投分支直接返回它。Graph 已提交终态保持权威；队列沿既有重投机制再次同步，不重新执行终态图节点。原 lease、attempt、额度、取消组合事务和队列重试上限均未更改，没有第二套同步调度。调用者不再需要另行发现并修补被消费信封对应的投影事务失败。

新回归使用实际 Graph HTTP 创建/执行、agent.SyncGraphRunToTasks、queue.Consume 和独立 PostgreSQL。确认单约束拒绝从 confirmed 变更；旧实现成功运行场景复现 run succeeded、确认单 confirmed、信封 consumed 且错误为 nil。修复后原 SQLSTATE 23514 和约束名透传，图仍 succeeded，确认单 confirmed，信封 pending。故障未解除时再次投递仍返回数据库错误；移除约束再投递，确认单 succeeded、信封 consumed，图像 Provider 调用计数及账户 available/reserved 不变。测试使用确定性 Provider，没有真实模型费用。

探索 unknown 场景另发现 Agent 两处状态 switch 缺少 unknown 分支；实际 run unknown 时没有投影写入，故不能用同一约束证明吞错。首轮包含两个场景 FAIL 4.383 秒；错误传播修复后中间运行仅 unknown 场景仍失败（4.762 秒）。最终本回归限定已证明的成功终态故障，并把 unknown 单独列入高优先级风险；没有宣称它已解决。新夹具未绑定 Agent Task，验证的是实际确认单持久化恢复；Task 的既有 Goal/user-owned 语义由消费者测试另行覆盖。

针对性恢复和缺失 Graph 消费回归 PASS 3.284 秒。最终当前 checkout Graph 整包 PASS 103.886 秒，受影响 Agent 确认/Goal 保持/用户控制状态/取消消费者回归 PASS 1.307 秒。Graph go vet -stdversion=false 通过；标准 vet 的既有 Go 版本声明问题仍未解决。日志保留 /tmp/pf-graph-projection-suite.log 与 /tmp/pf-graph-projection-agent.log。重投测试把既有信封置 sent 后调用真实 Consume，没有运行真实 dispatcher 或 SIGKILL；不据此宣称整套进程崩溃矩阵完成。

完整 diff 自审确认四处调用均消费同步错误，旧吞错实现已删除；测试故障约束只存在于一次性数据库，测试进程已结束。相关文档与空白检查通过后按三个独占文件提交，不包含 eval 修改。


## Graph 未知结果贯穿 Agent 确认单与 Task

本切片由主代理独占 Agent task_graph.go、新增 graph_unknown_projection_test.go、Graph 既有投影恢复测试、schema 枚举及新增升级测试、Web 确认单类型/卡片/消费者测试、ARCHITECTURE 和本记录。已刷新并行 eval 任务范围及实际共享服务进程，未触碰 eval 宿主与评测文件，未重启共享服务。

从 Graph 终态向上查 Agent 同步函数、确认单 GET/Confirm/Cancel、重试来源检查、数据库枚举、Node 工具请求及 Web 类型/卡片/轮询。原枚举只有 awaiting_confirmation/confirmed/succeeded/failed/cancelled；两处同步 switch 缺 unknown。Web 卡片以 confirmed + workflow_run_status unknown 推导警示，但 statusKey 仍为 confirmed，轮询条件也仍成立。Node 工具创建的是待确认请求，执行确认后由 Go 同步运行结果，没有另一份需修改的确认单终态枚举。

原代码真实 PostgreSQL 三场景 FAIL 4.077 秒：商品与全局确认单均停在 confirmed，无原因与 finished_at；Task 写故障未触发，因为原函数没有未知分支。现有 SyncGraphRunToTasks 仍是唯一同步 owner：确认单保存 unknown、Graph 原因及结束时间；商品 Goal 保持 waiting_user/goal_loop、无失败终态时间；全局 Task 使用原有 unknown 状态、原因及结束时间。用户 succeeded/canceled/paused 继续受原保护。没有把未知当 failed，没有改 Graph、Task 或 Provider 重试策略。Web 删除 confirmed 组合推导，直接读请求 unknown，复用现有四语文案和警示呈现，终态轮询自然停止。

数据库验证两种 Task 归属与三种用户拥有状态；确认单/Task 共用原投影事务，真实约束拒绝 Task waiting_user 时确认单更新回滚，解除故障后一起恢复。真实 HTTP GET 和重复 Confirm 返回原 run ID、两个 unknown 状态，信封数量不增加。Graph 执行级回归扩展到真实测试 Provider 返回未知：确认单投影故障保留 pending，解除后 consumed，未知图不重调 Provider、可用/预留额度不变。

schema.Apply 通过 EnumDDL 支持新库，并用 ExtraDDL 的 ALTER TYPE ADD VALUE 更新已有类型；没有修改迁移事务框架。一次性 PostgreSQL 测试还原缺少 unknown 的原枚举，在既有类型保存 confirmed 行后重复 Apply，旧值保持、新值提交后可写入。该升级只作用于测试库；共享开发库及生产没有运行迁移。新版 API/worker 上线前必须完成迁移，不能把本地代码验收当运行环境已切换。

首轮针对性 Agent/迁移/Graph 分别 PASS 4.278/2.846/4.045 秒。补 HTTP 确认重放后，Agent 新回归与失败用例单独组合 PASS 5.015 秒；schema 整包 PASS 27.148 秒。首轮 Agent 整包 FAIL 87.927 秒，TestConcurrentExpiredRecoverySkipLockedDoesNotDoubleTerminate 只将 3/4 个过期 Turn 收口；该链调用 recoverExpiredExecutions，不经过 Graph 投影。单独复验通过，未更改其代码或断言。扫描采用 projection SKIP LOCKED 后逐条锁 execution，首轮具体少一条的运行时原因未捕获，保留为未解释风险。

Web 完整门通过：113 文件、775 测试；lint/设计 token 检查、just web-build 的类型检查/构建/bundle 预算通过。卡片静态渲染验证结果未知与原因、无确认/取消/运行 spinner；轮询消费者验证 unknown 返回 false。复用已有文案和布局，没有做浏览器截图或真实模型运行。Graph/Agent go vet -stdversion=false、schema 标准 vet 通过；前两包既有标准 vet 版本声明问题未解决。

最终 Agent 整包 PASS 81.570 秒，包含新增 HTTP 重放回归；未放宽并发扫描断言。完整切片自审完成，Graph 测试只增加 unknown 组合，不改生产执行链。相关日志为 /tmp/pf-unknown-{focused,recheck,go-suite,agent-final,web-tests,web-lint,web-build,vet}.log；首轮红色证据 /tmp/pf-agent-unknown-red.log。just docs-check 与完整 diff 检查通过。测试进程已退出，只读 PostgreSQL 确认本切片的 pf_unknown_projection_/pf_unknown_rollback_/pf_request_enum_ 一次性库已清理；没有删除历史数据库。按独占文件选择性提交，其他任务修改保留。


## Task 图运行投影以最新确认单为准

本轮由主代理独占 agent/task_graph.go、tasks.go、受影响 task_graph_test.go、新增 graph_request_authority_test.go、ARCHITECTURE 与本记录。已刷新当前 checkout、eval 认领与共享服务进程，其他任务代码和测试数据库保持不动。

实际链路 SyncGraphRunToTasks 按 run 查所有关联请求，更新请求后不带请求身份写 Task；loadTaskAfterGraphRunSync 则按时间选择最近一条已提交 GraphRun 的请求，排除了更晚的待确认请求。loadTask/GetWorkflowRunRequest 原合同已经以 created_at DESC, id DESC 选择最新请求。复用这个顺序，无须新建执行序号、状态或 Task 指针。

独立 PostgreSQL 场景先关联旧运行，再创建待确认或运行中的新请求。旧 succeeded/failed/unknown/cancelled 原代码均把新 Task 改成 waiting_user；旧 running 还会覆盖待确认新请求，GetTask 重读也保留错误状态。首轮 FAIL 4.307 秒；其中旧 running 与新 running 同图并存另触发既有 uq_workflow_graph_runs_one_active_per_graph，属于不合法夹具，未作为缺陷。删除该组合，继续保留新待确认/旧 running 以及四种旧终态组合。

applyGraphRunStatusToTask 现在接收原确认单 ID，在原 lockTask 成功后读取最新请求；身份不符仅跳过 Task 更新，旧确认单仍如实保存自己的 Graph 结果。数据库读取错误直接返回既有同步事务。读取路径去掉 graph_run_id IS NOT NULL 过滤，最新请求未确认时直接返回当前 Task，不主动补旧运行。用户拥有状态和商品 Goal 规则仍由原函数维护，未增加接口或绕开事务。

最终针对性 PASS 7.928 秒：旧四种终态与合法运行中组合不覆盖新 Task，旧确认单照常更新；新运行成功仍进入商品 goal_loop。并发测试的第二事务持有真实 Task 行锁；旧回调开始并到达锁查询后，该事务写入新确认单、调用原 markRequestWaiting 并提交，旧回调取得锁后保留新的待确认状态。GORM callback 只观察查询到达时刻，未替代 SQL、锁或持久化。未知投影回滚、用户完成保护、Get/List 消费者同批通过。

该切片收敛的是不同确认单关联 Graph 运行的 Task 投影权。取消旧待确认请求仍走 parkTaskAfterCancelledRunRequest；其 conversation/Turn/Task 组合需要单独追踪。Task 已进入新 Agent Turn 但尚未创建新确认单、以及同一 GraphRun 读取快照过期的情况也未被本次新旧请求测试覆盖，继续作为待验证风险。未新增自动重试、Provider 调用、计费身份或数据库迁移。

最终 Agent 整包 PASS 88.222 秒，Graph 投影重投的成功/未知消费者 PASS 5.059 秒；Graph 命令的测试筛选未匹配 TestCancelGraphQuotaIsAtomicForBothEntryPoints，故不将其列为本轮额外验收。Agent go vet -stdversion=false 与 just docs-check 通过，既有标准 vet 版本声明限制保持。日志 /tmp/pf-request-authority-{red,focused,focused-final,suite,vet}.log；focused 的 6.940 秒失败仅余不合法双活动运行夹具，最终已修正。完整六文件 diff 自审确认没有 gofmt 带出的无关修改或新状态/查询通道；旧 apply 函数全部调用者已携带请求身份。测试进程已退出，PostgreSQL 只读确认 pf_request_authority_/pf_request_order_ 临时库均已清理；共享栈、历史数据库和其他任务文件保持不动，选择性提交本切片。


## 同一图运行的同步先锁确认单再读取源状态

本轮主代理唯一负责 agent/task_graph.go、新增 graph_projection_snapshot_test.go、ARCHITECTURE 与本记录。当前 eval-development-baseline 已进入新的认领，只读取其资源边界；其看板/评测文档、共享业务服务及数据库不变。

沿 GetTask/GetWorkflowRunRequest/Confirm 与 Graph worker/取消回调确认：原 SyncGraphRunToTasks 先读取 run.status，再更新确认单/Task。旧读取得 running 后暂停，另一事务提交成功、未知或取消并同步，旧事务恢复仍把确认单改为 confirmed、Task 改为 running。三个真实 PostgreSQL 场景全部复现，首轮 FAIL 4.536 秒。取消场景使用实际 Graph.Service.CancelRunTx 及其 AfterRunStatus，未以直接 UPDATE 替代取消用例。

同步入口现在按请求 ID 顺序发现关联确认单；每条请求取得原 pfdb.ForUpdate 行锁后重新确认其 Graph 关联，再读取源状态，最后沿原 request/Task 事务更新。删除锁前缓存的 run 快照。只锁 Agent 自己的请求，不新增 Graph 行锁，保持取消已有 run → request 的取锁顺序。请求在等待期间删除或不再关联该运行则跳过；数据库错误返回，用户 Task 状态和最新确认单投影权沿用原规则。

两个并发事务使用独立 PostgreSQL 连接：旧同步真实读取 running 后由查询观测 callback 暂停；新事务更新终态并调用同步，测试从 pg_blocking_pids 观察其阻塞，或在旧代码下观察其先完成，然后释放旧读取。最终成功/未知/取消确认单均保留结束时间，商品 Task 保持 waiting_user/goal_loop。callback 只控制读后的调度，数据库查询、写入和锁均真实执行。取消组合没有死锁；单次测试 context 有 10 秒界限。

针对性 PASS 11.141 秒，同时覆盖不同请求的新旧投影权、未知投影错误回滚、用户拥有状态与取消消费者。该变化为每个关联请求增加显式锁查询并在锁后读取 Graph，不据此声称性能改善；原请求身份、事务框架与状态枚举均复用。没有 Provider 调用、迁移或共享服务重启。旧待确认取消与新 Agent Turn 的权威关系仍按原后续风险继续调查。

最终 Agent 整包 PASS 91.356 秒，Graph 投影重投/两取消额度入口/缺失运行消费组合 PASS 5.484 秒；Agent go vet -stdversion=false、just docs-check 和 diff 空白检查通过，标准 vet 的既有版本声明问题未解决。日志 /tmp/pf-projection-snapshot-{red,focused,suite,vet}.log。完整四文件自审确认锁顺序、删除旧快照、请求关联再核验及真实 SQL 观测；无 schema、Provider 或 Node 代码变更。测试进程已退出，只读查询确认 pf_projection_snapshot_ 一次性库已清理。未删除历史资源，按独占文件提交，保留其他任务的看板与评测文档。
