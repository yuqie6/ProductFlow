# 任务：批准开发集合并采集可供 Miner 消费的冻结 L1 批次

状态：认领
类型：证据
认领者：capacity_baseline
认领于：2026-09-09T00:50:10+08:00
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

## 第六次固定开发窗口

主代理已验收并提交创建场景跨工具观察与多工作流物化 `c72047261e473ed900c237e125aac6e1b8433f98`，恢复 eval_baseline 认领。新干净 checkout `/tmp/productflow-eval-development-0908-r6`，新产物根 `storage-dev/eval-development-0908-r6`。固定该提交，不吸收共享工作树后续改动；沿原83道development/11个world分组，L1 75×3=225，regression/acceptance仍为空。模型openai/gpt-5.6-luna，推理参数未额外覆盖，并发3、180秒、12轮，冻结题目/Skill/评分/依赖/请求配置；正常失败保留，新的合同错误或不可测按原协议停止，不重采至绿。

执行者独占新checkout/证据目录与 `pf_eval_r6`、`pf_eval_r6_intake` 独立数据库前缀；记录实际创建库并只清理这些库。不得改共享DB/provider/服务、旧五批轨迹或任何被测源码。root持有文档/Git/最终审核，执行者不提交。启动前记录完整冻结身份、manifest、预期分母与资源清单，运行既有Go观察前置及12项跨语言门以验证固定checkout；门通过即可按本段授权执行完整批次，无需再等流程确认。有效完整运行导出development输入包，前后指纹一致才交付。生成代码身份或依赖安装异常则报告，不绕过。

## 第六批停止与统一观察前置

固定c7204726，run `20260908T030817Z-618fbdd2`，174/225条、58/75题，各已记录题3次；150 passed/24 failed只作诊断计数。run-diagnosis-negative-edit-node三次apply/propose为unknown工具结果，measurementEligibility=false。SIGINT停止，driver退出1；没有summary或development导出，不续接、不降分母。

root已读取 `storage-dev/eval-development-0908-r6/agent-evals/evidence/after-stop-identity-check.json`，same_identity=true，commit/题集/输入/Skill/harness/lock均未变。原始174条与转录保留；driver/CLI具体PID复查已终止，process-after-cleanup.txt记录无自有进程，前后独立DB前缀清单为空。post-stop-checks.txt早先owned_processes=1不单独作为最终进程证据。执行者交回材料后释放占用。

[按作用域统一工具观察](archive/eval-scope-tool-observation.md)承担同类缺口，须在下一整批前覆盖所有实际暴露工具及冻结注入合同，不再逐Skill付费试跑发现接线缺失。跟进者root；该前置未验收前保持阻塞，不启动r7。

统一观察已完成确定性前置验收：75输入物化/最终读取无基础设施缺口，root真实PG与runner25项回归以及未finalize新增回归通过。下一批仍须冻结最终提交、独立资源、模型与完整225分母后再恢复认领；旧六批不续接或合并。当前阻塞只剩新采证窗口冻结与资源复核。

## 第七次固定开发窗口

统一作用域观察已验收并提交`9f3d25e72519682e84da292708a0655f4ab0d235`，恢复eval_baseline认领。固定该提交于`/tmp/productflow-eval-development-0908-r7`，独立产物根`storage-dev/eval-development-0908-r7`，独立PG前缀`pf_eval_r7`。沿用83个development任务/11个world的集合分组与L1完整75×3=225，regression/acceptance为空；模型openai/gpt-5.6-luna，推理参数不额外覆盖、并发3、180秒、12轮。固定代码/题目/world/Skill/harness/lock/配置/manifest身份；不复用旧批次试验或修改评分。正常行为失败完整保留，未知观察/合同错误按既有协议停止，不为分数重采样。

当前容量执行者使用同一物理机做有界混合负载，r7先准备独立checkout/集合manifest与静态冻结身份；CPU/PG前置和模型整批启动需等root确认容量采样窗口结束，避免影响时序证据。该确认属于主代理资源协调，不新增用户审批。窗口释放后，在固定候选执行既有真实Go观察前置，成功即可按原完整批次合同运行及导出；缺输入或真实不可测则保留现场交回。执行者只写自有checkout/证据，不动共享服务/配置/源码或文档，不commit/push。

r7低负载准备已完成，root复核checkout干净且HEAD为9f3d25e7。集合语义hash仍为5e1e31c6c3dcbab5651cba62cfea17f827a7fa82b844652a1245e269295dd126；manifest文件字节SHA256为b43f61accf111359bfb631ee8ee9d55e812d6cec0b3a224be6ce856dc1e4e38b，两者含义不同。before.json保留本候选独立身份，107输入JSON/83任务/11world与旧组划分相同，L1完整分母225。尚无模型/Go host/PG执行，容量采样资源窗口未释放。


容量栈已清理，root释放物理机计时窗口。原eval_baseline句柄恢复因代理线程上限失败，现由已交回容量执行的capacity_baseline承接同一r7任务，冻结代码、集合、模型、分母和产物路径不变。完成实际Go观察前置后执行完整225及导出；只占用pf_eval_r7前缀数据库与本任务产物，不改共享服务或评分。root保留文档/Git及最终审核。

## 第七批停止与持久化操作评分前置

r7固定9f3d25e7，run 20260908T125629Z-d80c35af，18/225 trial、6/75题后按合同停止。Go fixture前置和go-world 24项通过，但持久化评分缺discard分支，apply分支仅支持首节点rename/update，合法删除/断边/移动被错误计为意外变更。18份原始轨迹及stop-summary保留，无summary/development导出；专用Go host和pf_eval_r7库已清理。不能把所有persisted mismatch都提升为unknown，需分清业务不满足与观察器不支持。[持久化操作观察](archive/eval-persisted-operation-observation.md)修复完整操作语义并完成无模型正反覆盖前，禁止启动r8。旧七批不续接、不合并分母。

持久化观察与共用 global seed 修复已验收，见上述归档任务；原题/world/Expect 未改，当前观察 fixture 增加原题指定图的元数据。r8 尚未启动，须固定新交付版本与观察身份，并等待 e190 容量正式计时结束，不能复用 r7 批次。

## r8 准备窗口

capacity_baseline 已重新认领。固定代码 `f9c01008`，独立 checkout `/tmp/productflow-eval-development-0909-r8`，产物 `storage-dev/eval-development-0909-r8`，专用 PG 前缀 `pf_eval_r8_`。当前仅准许轻量只读核对、建立固定 checkout 与冻结清单；e190 容量正式计时期间禁止安装、构建、Go/Node 测试、模型或数据库负载，等待 root 释放窗口。

公开 development 集合仍为 83 题/11 world，L1 75×3=225，regression/acceptance 为 0；模型 openai/gpt-5.6-luna、并发 3、每题最多 12 轮及 180 秒，不额外覆盖推理参数。重新记录观察 fixture/代码/Skill/任务/world/集合身份，不能沿用 r7 的观察指纹；旧七批材料不合并或续跑。准备完成后等待运行分配。

## r8 实际结果与审核缺口

e190 窗口释放后完成固定 f9 来源首批 `20260908T174928Z-ad09433d` 的 225 份轨迹，原评分 161/225。原 run 的 Git provenance 为 unknown/dirty：archive checkout 缺 .git，运行时未记录有效 Git 身份；事后补 Git 不能改写原始记录。首批保留作开发诊断，尚未验收为正式固定基线。

root 抽查并由执行者全量归因：34 条存在 document_action 的 null/空串等价字段误判，另 1 条同时存在 scope 差异；恢复归档素材的 3 条在模型前加载所选素材时报“全局素材不存在或已归档”，不能当模型行为失败。纯字段差异尚需按真实 wire 语义做受限离线重评分，不能改题目/Expect 或宽松消除 scope/操作顺序错误。

执行者曾误启动第二批 `20260908T182733Z-222ed23e`，2 条落盘后停止；独立保留，不填入首批、不继续补抽。临时修改 staging provenance 导出的包未获 root 接受，须移入 evidence/rejected-derived-export 并标明原因，不能进入正常 development-inputs 消费。首批 225 的失败分析可继续；当前只准许调查最小离线归一化与重评分方案，不改冻结源码/答案，不再调用模型。r8 专用进程和数据库已由执行者清理，共享 dev 服务未动。

root 已确认 runtime wire 的 document_action 空值语义，准许 capacity_baseline 在当前开发树的 `agent-service/evals/graders/writes.ts` 与 `graders.test.ts` 修复观察边界；固定 f9 checkout 和首批记录不可改。仅在两个 workflow run 请求中将 null 视作未指定 action，非空动作、scope/force/node/source 与商品身份严格比较。验证 omitted/null/空串及非空动作、其它字段的正反例。

复用已有 grader 可在本任务 storage 内编写一次性只读重评分脚本，若原始轨迹足以重建完整评分输入，则生成绑定原始 hash 和本次 grader diff/hash 的独立诊断报告；不得编辑原 trials/transcript/provenance 或伪造 clean 身份，不新增 CLI/导出绕过，不作正式能力基线。缺失评分输入则明确列不可重评，不按删错误字符串推算通过率。无模型调用。root 持文档/Git。

空 action 观察修复已完成 root 自审：仅两个 run 工具实际 null 归一化为空串，其它比较不变；13 项 grader 回归、TypeScript 检查及生成契约检查通过。离线 v2 直接导入固定 f9 旧 grader 和当前 grader，对同一 225 条持久化调用执行，write 失败 54→20，34 条差异均为该空值边界；原 161/225 不覆盖，不外推新能力通过率。v2 报告 `storage-dev/eval-development-0909-r8/agent-evals/evidence/r8-offline-grader-diagnostic-v2.json` 绑定 raw tree hash 50a8fd58d7534ecc432ff23d9d5309dc24468c7d5186e0cc2879c277c4c1177f 与两版 grader hash。哨兵模拟 v1 只保留作历史，不作为审核依据。

本次提交冻结必要的观察器修复；本单仍未取得正式完整开发包，不立即开启下一批模型采样。归档选择故障已定位为评测把页面管理选择误装成附件，已由[输入装配修复](archive/agent-archived-asset-selection.md)处理，生产权限不放宽。后续固定运行须在启动前核对真实 Git provenance 和完整初始化合同。

## 待处理提案 fixture 缺口

r8 discard 三次都实际调用工具且返回失败；两次最终文字为无待处理，一次无依据声称已丢弃。root 与执行者核对发现 world.PendingProposalID 从未进入真实 GoHost seed，实际图没有 pending proposal；已有持久化覆盖测试在测试体中额外创建提案，掩盖了实际 seed 缺口。不能把该三条解释为模型未调用，也不能用 terminal succeeded 覆盖失败。第三次虚假成功表述另保留为行为观察。

capacity_baseline 获准修改当前开发树 `go/internal/agent/evalworld_test.go`、`eval_persisted_writes_test.go` 及必要的 `eval_observation_fixture_test.go` / `eval_user_sim_host_test.go` 回归。根据已有 world 字段物化真实 pending proposal，绑定实际 graph/product/conversation/revision；把覆盖测试里的额外造提案移入共用 seed，删除重复准备，验证真实 GoHost 路由直接 discard 成功并读回 discarded 状态。无 pending 的 world 仍保持无 pending，错误 graph/conversation 不被接受。固定 f9、原题/world/Expect、原225、生产工具/Skill都不改；无模型调用。测试用独立 pf_eval_seed_0909 前缀，避免其它Agent的Go agent测试库。

共享 pending proposal seed 修复已通过主代理逐文件审核：读取既有 PendingProposalID，经现有 ProposeGraphTool 物化真实提案，维护 fixture/实际 ID 双向映射；删除 discard 覆盖测试里的额外造提案。新增真实 GoHost 跨语言回归位于 agent-service/evals/go-world.test.ts，属于本次初始化合同的必要验证。真实 PG 覆盖 pending/无 pending、错误 conversation 拒绝、正确 discard 后状态与 revision；Go fixture/持久化焦点回归、Node GoHost 1 项、TypeScript 与 diff 检查通过。证据为执行者终端结果，未创建持久化日志。

本次为评测前置修复候选提交，完整开发基线仍未验收。capacity_baseline 继续只读检查其余 world 初始化字段及现有 provenance 启动检查，无模型采样；不能事后改写 r8 或重算完整能力结论。
