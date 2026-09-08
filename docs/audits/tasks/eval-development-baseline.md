# 任务：批准开发集合并采集可供 Miner 消费的冻结 L1 批次

状态：阻塞
类型：证据
认领者：—
认领于：—
业务组：Agent 自进化
父账本：agent-self-harness.md
完成后可拆：自进化组协调者按父章程发布固定实验与自动验证切片；不直接发布整个进化系统

遵循 [Issue 协议](README.md)。前置解除并获确认认领后才开展任务调查或采证。

2026-09-05 组织协调：本单是自进化组的一次性启动输入交付，承接原 P3 所需开发材料；数据用途审核由协调者完成，不能变成未来每轮人工选题或审簇。现有缺题目校正的阻塞不因改组消失。

## 问题来源

[集合隔离机制](archive/eval-collection-isolation.md) 已在 `2ab85674` 交付，旧 run 没有其冻结身份，不能回填后冒充隔离采证。[eval-contract-alignment](archive/eval-contract-alignment.md) 曾冻结 L1 `task_hash=406dc178b7908384db08a836038c7b8809f0c05b0821b76d8345bd8a9a8db7fb`，后续仍发现输入缺口。自进化组须消费有效校正版本、登记场景分组并采集带身份的新开发批次，不能将旧题集误判直接归因为行为机制。

## 做成什么样

自进化组协调者依据固定集合合同审查场景分组与暴露情况，冻结校正后的题集，运行一次完整 `k=3` 的开发用途 L1 批次，导出仅包含开发材料的输入包。记录有效失败和不确定结论，保留全部试验；通过率不达标可以完成采证，但缺项、无效运行或缺身份不能关闭。

## 前置与并行

- [独立观察刷新](archive/eval-library-observation-refresh.md) 已独立审核并提交，沿用其最新 task/world/fixture 身份；早期 [eval-contract-alignment](archive/eval-contract-alignment.md) 仅作历史依据。生产 Skill/runtime/harness 不随本单采证改变。
- 本组协调者审核清单：当前公开题与已读轨迹按 exposed development 处理，同源 scene/source/origin 不跨用途。隐藏与独立验收材料未就绪时记录空集与缺口，不另贴标签宣称独立。
- 真实 provider 凭据可用；固定候选代码 checkout、任务/world、Skill、壳、provider/model、推理参数、并发/预算与完整 trial 分母。协议已声明 dirty 工作树不能承担同 commit 比较时，使用独立固定 checkout，协调认领留在共享工作树。
- 运行使用独立 `STORAGE_ROOT`，不得修改共享 provider 设置、DB、worker 或图片池/浏览器资源。记录预期 task/trial 数后再开跑，不事后降分母。

## 修改与占用范围

- 本任务、自进化父章程的启动输入状态与本次 L1 证据、看板/归档由协调者整合。T-07 原合同留在 Agent 质量账本，只引用已冻结版本，不在本单修改其评分或集合实现。
- 原始 plan、manifest、run、transcript 与 development-inputs 只进约定 `STORAGE_ROOT/agent-evals/`，不提交 Git。现有工具实现为 `evals/collections.ts`、`live-runner.ts`、`cli.ts`。
- 不改题目、world、Skill、harness、grader、集合实现、试验预算或分数阈值；发现新合同错误时暂停，独立交回评测实现任务。

## 执行与验收

1. 批准分组依据和暴露清单，用 `just agent-evals-freeze-collection <绝对 plan 路径>` 生成 manifest，登记 hash、代码基线、各用途 task 数、预期 L1 trial 数。
2. 用 `just agent-evals-run-collection <绝对 manifest 路径> development` 跑一次 k=3。原始失败不得删掉，基础设施故障保留原批次及明确原因，不重复采样直到绿。
3. `just agent-evals-export-development <run_id> <绝对 manifest 路径>` 通过，记录导出位置与完整 task/trial 数、harness/Skill/task/collection hash、provider/model/推理配置与 pass 指标。旧 run 不回填 collection。
4. 本组协调者检查开发包没有隐藏题/逐题结果/历史摘要回流，登记分组的产品依据和审查结论。代码 hash 不证明语义等价性，证据不足则停止；不要求用户再次审批普通公开开发题的选取，不授权读取未批准数据。
5. `just docs-check` 通过；有效采证完成与质量账本 D-03/D-08、T-08、自进化 G1 分别记录。后续问题选择和归因由控制器自动执行，旧每簇人工抽读前置撤销；本单不提前签收自动诊断质量。

## 阻塞与交接

- 当前实现依赖：[图观察权威](archive/eval-graph-observation-authority.md) 已交付。L1 graph-editing 的 apply/propose/discard 与写后读取走隔离 testdb 的 Go 宿主；catalog/intake 仍用 fixtures。付费 L1 需独立 `DATABASE_URL`（testdb 包建隔离库）与 `STORAGE_ROOT`，启动前通过 `TestEvalObservationFixtures`。协调者确认资源窗口并重新冻结集合前，不启动第四次付费全量采样。

- 2026-09-05T21:25:56+08:00：第三批因结构 apply 缺失 Go 观察返回 `eval_unobservable` 而停止，CLI 及父进程已结束。协调者接管本文件未提交证据并释放执行占用；[不可测消费边界](archive/eval-unobservable-trial-boundary.md) 独立处理 trial 身份和开发导出。结构观察缺口仍需等待 Go 节点合同稳定后补齐。完整开发基线仍未完成，不导出第三批，不修改冻结产物。

- 当前：读取义务校正 `8cf6e08a` 及恢复测试 `32745610` 已完成，协调者检查占用后确认重新认领。第三批固定 `8cf6e08a50f62515a77d11e9037eff2068463358`，独立 checkout `/tmp/productflow-eval-development-20260905-r3`，产物根 `storage-dev/eval-development-20260905-r3`。83 道公开 development 题中 L1 75×3，旧两批不纳入；不修改共享 provider/DB/worker 或图编辑任务文件。

- 2026-09-05T20:34:29+08:00：协调者确认第二批进程已结束，接管本文件及看板的未提交证据记录，释放执行占用以完成读取义务校正。旧两批产物与记录全部保留，不随修复单冒充基线交付。

- 原因：删节点题意已修复；第二批发现 [澄清前无用必需读取](archive/eval-clarification-read-obligations.md) 将三个安全提问判失败，按合同再次停止。
- 解除条件：上述读取义务校正及同类离线复查完成并提交，再按新身份完整采证。
- 跟进者：主代理-eval-quality-0905-1934。
- 交接：本批进程已结束、原始产物保留，未改被测代码。2026-09-05T20:05:00+08:00 协调者接管本任务及看板的未提交证据记录，释放执行占用以先完成题意校正；记录不得删除或夹带为修复任务交付。

- 输入阻塞已解除：[可见输入合同](archive/eval-observable-input-contract.md)、[生产素材读取合同](archive/agent-library-read-contract.md) 与 [独立观察刷新](archive/eval-library-observation-refresh.md) 已交付，12 条素材阻塞经独立审核移除。最新全量 taskSetHash 为 `1283d72edd0ed6a9ffb7dcd36f63c7652e14ea8eb3a8e1fc1c7e97c57041a258`；以观察归档的交付提交冻结执行代码，不使用旧合同 hash。
- 本单已批准公开开发清单并启动 k=3，发现 [批量删节点题意歧义](archive/eval-graph-clear-intent.md) 后按合同停止。修复交付后重新冻结并完整采证；不得续接旧批次或回填 collection。`measurementEligible=false` 的诊断分数不得用于候选选择；`skill-ab-20260905` 的公开诊断批次不替代本单有效开发包。
- 跟进者：自进化组协调者消费 Agent 质量组已交付的固定校正版本；输入就绪后本组自行采证。
- 认领复核：当前源码干净，图片池保留其浏览器/provider 设置占用，eval-skills 仍阻塞且无新 A/B；本单使用独立固定 checkout 与 STORAGE_ROOT，不修改共享设置或占用 PG/worker。独占本单文档及开发批次，不预研 Miner、不重用旧转录伪造完成证据。

## 本次冻结与执行窗口

- 固定干净 checkout：`/tmp/productflow-eval-development-20260905`，commit `fe32b2ada9acace3620114948875c37b94b23a40`；独立产物根 `/home/cot/ProductFlow/storage-dev/eval-development-20260905`。
- 协调者批准 11 个初始 world 场景分组，共 83 道公开 development 题；同 world、origin 与改写来源均留在 development。regression/acceptance 用途均为 0，未宣称独立隐藏验收。manifest `2fe4564a9aa35a71186002a28380f498782c57d90fdd7ffa8686845b2c75a646`。
- 本次只执行完整 L1 75 题，各 3 次，分母 225；provider `openai`、model `gpt-5.6-luna`，推理参数未显式覆盖，并发 3、单 trial 180 秒、最多 12 轮。完整身份及无明文凭据的配置记录在 `agent-evals/evidence/before.json`。
- 锁文件安装后 `pnpm test`：296 passed / 6 skipped。`just` 的嵌套 login shell 错选 pnpm 11.17.0，与锁定 10.32.1 冲突；本次用产物目录 `baseline.mjs` 直接调用同一 `evals/cli.ts` 与已安装的 tsx loader，冻结代码及依赖声明不变。

## 本批暂停证据

- run `20260905T114437Z-a8d89635`，2026-09-05 19:44:33 至 19:51:05 +08:00。发现同题改写范围歧义后向本单 CLI 发 SIGTERM，父进程已退出，未动共享服务。`after.json` 的 `exit_code=null` 表示信号终止，不代表成功。
- 54/225 条完整 trial，覆盖 18 题，其中 49 条 passed、5 条 failed；未形成完整 summary，不报告正式通过率、不执行 development export。已落盘 token 合计 2,444,195；在途调用可能有未取得用量，总成本未知。
- 固定 checkout 结束仍干净，107 个 JSON 输入 hash 与开始一致。原始转录、plan、manifest、before/after 和 run.log 全部保留在独立产物根。
- 跟进者：主代理-eval-quality-0905-1934；待新题意校正任务交付后重新核对资源和身份。当前保留协调记录所有权；无运行进程、无 PG/worker/provider 设置占用。证据任务未完成，不归档或提交为成功交付。

## 第二次冻结窗口

- 2026-09-05T20:11:48+08:00：协调主代理检查现有占用与已提交修复后确认认领。固定提交 `051e33386e56ef8d817a7f18556c54fe08a58231`，新 checkout `/tmp/productflow-eval-development-20260905-r2`，新产物根 `storage-dev/eval-development-20260905-r2`。不改变共享 provider 设置、数据库或其他任务冻结资源。
- 沿用 11 个 world 场景及已暴露 development 用途，83 题中 75 个 L1 各跑 3 次。新全量 task hash `988c2d2895c09fa2b8e8167f060ad27c320c471a5c3bf8fa8cce1802bd4c5e6a`；旧批次仅保留历史，不参与本批分母或导出。

- 第二批 manifest `74ec701d58297da14326739814bb189f45589af05cf1f2f4ca5a8742705f6f2c`；run `20260905T121323Z-d6899746`，2026-09-05 20:13:22 至 20:23:38 +08:00，openai/gpt-5.6-luna，推理参数未覆盖，并发 3、每 trial 180 秒、最多 12 轮。记录见 r2 产物根的 `agent-evals/evidence/before.json`、`after.json`、`run.log`。
- 因上述三道素材澄清题的唯一失败为多余列表义务，向本单 CLI 发送 SIGTERM。90/225 条完整记录、30 题，其中 83 passed / 7 failed；这是诊断计数，不能形成正式通过率。已落盘 tokens 3,546,614；中断时在途调用用量可能缺失，总费用未知。没有 summary、没有 development export。
- 结束固定 checkout 仍干净，输入/Skill/harness/依赖 hash 未变，父进程及 CLI 已结束。已修复删节点题在该批 3/3 通过，但不能代表完整开发基线。原始两批均保留，未改写分数；本单仍未完成。
- 跟进者：主代理-eval-quality-0905-2011。当前保留证据记录所有权，无运行资源占用；修复需独立认领和提交。

## 第三批暂停证据

- 固定 `8cf6e08a50f62515a77d11e9037eff2068463358`，checkout `/tmp/productflow-eval-development-20260905-r3`；产物根 `storage-dev/eval-development-20260905-r3`。manifest `5e1e31c6c3dcbab5651cba62cfea17f827a7fa82b844652a1245e269295dd126`，全量 task hash `acb773ce1b47457cb94d11544cddddb886dfcee8bb7febdb7267c64ab6c719f2`，L1 75×3，公开 development 83 题，regression/acceptance 均为 0。
- run `20260905T131829Z-35b08c45`，2026-09-05 21:18:28 至 21:25:31 +08:00；openai/gpt-5.6-luna，并发 3、每 trial 180 秒、最多 12 轮，推理配置未覆盖。59/225 条已落盘，覆盖 20 题，54 passed / 5 failed 仅为诊断计数；其中解散重排、批量禁止直接 apply 两题的 trial 1 含 `unknown` 工具结果。已落盘 tokens 2,594,413，在途用量可能缺失，总费用未知。
- 缺失结构 Go 观察使 stub 返回 `eval_unobservable`，不能据此判定真实后端会拒绝同一操作。已向本批 CLI 发送 SIGTERM 并确认父进程退出；`after.json` exit_code 为 null，固定 checkout 干净、107 个 JSON 与 Skill/harness/lock 身份前后不变。无 summary、无开发导出，不续跑或回填原批次。
- 下一步：独立完成不可测消费边界修复；结构观察需在 `node-detail-redesign.md` 所持有的 Go 节点合同稳定后采集并独立核对。未取得有效完整开发包前，不启动依赖该包的自进化实验。

## 最初发布依据

2026-09-05：主代理-agent-0905-0458 核对现有开放任务。L2 state、L3 user-sim、L6 production mine 与图片池均不承担本单的冻结 L1 开发输入，无重复采证单。仅发布为阻塞，不记为采证完成。

## 第四次固定开发窗口

主代理根据完整目标授权恢复本单。既有图观察交付已消除实现前置，当前阻塞解除仅限本次独立开发采证；不追认旧暂停批次，不更改其数据。认领者eval_baseline，冻结提交 a12e10ee41fc21388f80e230db0754c923938446，checkout /tmp/productflow-eval-development-0908-r4；STORAGE_ROOT=/home/cot/ProductFlow/storage-dev/eval-development-0908-r4。Go/Agent与题集/Skill只读；schema/测试库使用独立pf_eval_development_0908前缀，先跑TestEvalObservationFixtures及必需输入合同，确认当前图操作实际可观察后再执行完整L1三次。公开题保持development用途，按现有集合/导出合同生成真实完整开发包；不创建虚假隐藏集或人工标签。

用户已授权必要真实模型开发验证，模型gpt-5.6-luna，沿当前L1冻结请求/并发/时限合同，配置只读注入，不打印凭据。不改共享DB/provider/服务，独立安装依赖与Go观察宿主；当前主工作树其他auth/graph/web修改不进入该固定checkout。执行者报告开工门与最终完整包，失败/不可测照实保留，不能擅自修改grader或被测Skill补成通过。运行后释放自己明确创建的临时数据库与进程；证据原始目录保留。主代理持有活文档/Git/最终审核，执行者不push/reset/revert/提交。

## 第四批暂停证据

固定a12e10ee与全部输入/Skill/harness/lock前后未变；run `20260907T215536Z-e24d4e7f` 完成39/75任务、117/225 trial，其中7个unobservable。`finalize_product_intake_v1` 对真实模型选择返回 `eval_unobservable: no Go observation fixture for this intake selection`，按合同停止。96条passed、21条failed仅为诊断计数，measurementEligible=false，未输出summary/history/latest或development包。原始117份转录与assessment保留于 `storage-dev/eval-development-0908-r4/agent-evals/`；主代理核对报告与子代理清理结果，宿主进程与pf_eval_development_0908前缀DB已释放。等待 [创建确认观察修复](archive/eval-intake-observation.md)；不得续接本run或把117作为新分母。

## 第五次固定开发窗口

创建确认观察修复已交付于 `326bf59ae070687c45d0426b9ebd1f53cb798480`，协调者复核占用并确认 eval_baseline 认领。独立干净 checkout `/tmp/productflow-eval-development-0908-r5`，产物根 `storage-dev/eval-development-0908-r5`；DB 前缀 `pf_eval_r5`，intake 宿主设置 `PRODUCTFLOW_EVAL_HOST_DB_PREFIX=pf_eval_r5_intake`。其它 Go 宿主只清理本次记录的具体数据库，不碰历史库。Go 观察前置串行执行，避免同库初始化竞争。

冻结该提交与依赖、题集、world、Skill、模型及请求配置；沿用公开 development 83 题中的 L1 75×3=225，11 个 world，regression/acceptance 均为 0。openai/gpt-5.6-luna、推理参数不额外覆盖、并发 3、单 trial 180 秒、最多 12 轮。旧四批只留历史，不续接、不合并分母。新合同错误或不可测按既有协议停止，正常行为失败完整保留，不为分数重采样。执行者仅写独立证据，主代理持有共享文档与 Git；不改共享服务、provider 设置或被测代码。

## 第五批暂停与后续观察缺口

run `20260908T003528Z-e48b5db4` 固定 326bf59a，记录 133/225 trial、45/75 题；111 passed / 22 failed 仅为诊断计数，其中 product-intake-negative-delete-images 的 trial 2/3 调用 propose_graph_change_set_v1 后无真实观察，2 条 unobservable，measurementEligible=false。按合同 SIGINT 停止（130），不续接或降分母，无 summary/development 导出。原始轨迹、stop 与清理记录保留于 `storage-dev/eval-development-0908-r5/agent-evals/evidence/`；新建五库已清理，与启动前清单一致，历史三库未动。执行者报告固定 checkout 干净；协调者已读取停止后指纹报告，all_match=true，固定提交、输入/Skill/harness/lock及133条原始记录均未变。

[创建场景跨工具观察](archive/eval-intake-graph-observation.md) 承担新缺口；其检查需覆盖创建场景可调用的写工具，不再只按上一条成功路径补宿主。题目、评分、Skill 与原始五批保持冻结。完整开发包仍未交付。

创建场景跨工具观察与多工作流种子已通过最终确定性回归；本单仍待新提交冻结与资源复核后恢复采证，旧五批不能用于完整开发包。
