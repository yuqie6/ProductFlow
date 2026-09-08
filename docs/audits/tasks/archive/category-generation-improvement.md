# 任务：从稳定图像缺陷落实生成改进候选

状态：完成
类型：实现
认领者：account_backend
认领于：2026-09-08T18:25:01+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：固定候选的同输入真实生成对照

遵循 [任务协议](../README.md)。

## 目标与边界

沿用户已批准的分类真实电商图参照与多模态标注方向，从现有重复标注中选可核实的具体缺陷，追踪真实生成输入与提示词所有者，形成有界改进并验证；不能停在报告工具增加或波动胜负排名。复用现有商品事实、图位目的、生成配置及provider调用链，不新增孤岛功能/自动采纳控制器。不同图种的展示要点应按该图目的适用，不把类别所有信息硬塞进每张图。

第一阶段由account_backend只读现有`storage-dev/image-annotation-basis-0908`及其固定源报告、实际对应图片和当前生成链，交回最多2项跨重复证据支持的问题、实际图像核实、代码因果点、独占修改文件建议、最小候选和对照验收。root负责选择与冻结实现范围；该内部协调无需用户重复审批。未获具体写域前不改生产源码。初始研究产物独占`storage-dev/category-generation-improvement-0908/`，root持有文档/Git。

## 不变量与验收

质量对照和身份参考不混用；不把不同目的的图比较胜负，不将缺少本图非必需信息记成缺陷。事实未确认不得补为真实性能、功能、品牌或结构。候选不得包含SKU/商品名/测试集ID特判。原v2报告、旧42/32图位和blocked主体保留live smoke不改；本单不解除其它任务阻塞。所有旧原始失败保留，不重采至绿。

实现阶段需沿真实提示词/生成调用链修改已有所有者，确定性回归覆盖适用和不适用场景；真实生成和多模态比较使用新固定候选、同输入/预算/模型与完整分母，明确局部结果和未改善项。模型评价不等于人工真值，不因一例通过宣称所有类别达标。当前先只读取证，不运行付费模型、生成、批量测试或改共享provider；真实运行窗口由root与容量/Agent采证协调后登记。

## root候选裁定与实施范围

root已查看旧3c主图及重复证据：轮廓整体可辨，部分内腔与深色部件的局部暗部对比不足；这只支持有条件的摄影改进候选，不构成现版本失败或要求改成浅底。选候选A进行有界实现，候选B暂不实施（现有scene规则已变化，旧图不能证明当前缺陷）。

account_backend独占`go/prompts/providers/prompt-generation.md`、`go/prompts/listing/compile-image.md`及现有`go/prompts/prompts_test.go`、必要`go/internal/graph/listing_prompt_test.go`中的本规则回归。复用摄影/hero的现有指令，尽量替换收紧相邻句而非叠加负面清单。根据主体和环境的实际明度/材质选择背景或补光/轮廓光，使缩略图中主要轮廓与关键可见结构可读；保持真实商品颜色、材质和用户明确构图/背景，不把暗色高级感本身视为缺陷，不套到文字规格/详情信息要求。禁止新增图像后验硬阈值、自动改图或SKU特判。

固定基线使用当前`9f3d25e72519682e84da292708a0655f4ab0d235`；候选确定性验证和root审核通过后允许一次候选提交供同输入实图对照，任务仍保持认领，只有真实对照证据与结论归档后完成。真实运行前冻结一个暗色摄影样本和一个非暗色控制样本、各版本各3次完整分母、事实/身份参考/图位/用户约束、实际prompt及图片provider/model。固定v2评委保留原报告与失败，不混用旧图作为本轮baseline。只验证局部候选，若效果无改善/有退化，报告并撤销本候选生产指令而不放宽验收。当前容量采样期间仅实现与轻量确定性检查，真实模型运行窗口仍由root协调。

## 候选代码审核与固定对照合同

root已审4文件完整diff与当前调用链：CompileImageModelPrompt的TypeLine与FamilyLine互斥选用，故hero实际行和photography后备行分别保存条件指令；不会双重发送。新增文字要求保留用户已确认背景/整体光照与真实颜色材质，通过局部补光或轮廓光维持暗部可读，不改变scene规则或规格信息图职责。root执行prompts合同与真实graph编译正反回归通过（.003s/.018s），证据`storage-dev/category-generation-improvement-0908/root-contract-tests.log`；执行者完整prompts/graph与provider prompt组装回归通过，未运行真实模型或PG。

本次为合同允许的固定候选提交，任务保持认领。live必须两版本各2样本×3完整生成（12次GeneratePrompt+12次image provider），覆盖实际GeneratePrompt→持久化prompt artifact→CompileImageModelPrompt→真实image provider，不直接复用旧编译字符串；只hero，不额外生成历史naive/gold。保留实际出站请求、模型/响应ID、事实与图片输入hash、run/node-run、费用、失败/unknown。两臂资源隔离，共享服务与配置不改。固定v2评委比较同商品同目的，结合原始图/缩略图核实；任何局部无改善或退化如实记录，当前不宣称候选质量提升。

候选已固定提交5355a3bb631389eade0daabda9d1b48bb998771e。两个独立checkout、live-inputs.json、live-driver.mjs和provider-trace-proxy.mjs已准备，未调用模型。root分配image_live_review只读审核driver与实际API/provider合同（不改代码或运行资源）；account_backend保持本任务实现/执行所有权，root并行复核样本与冻结事实。

root逐图检查7张参考发现颜色混用，现执行输入切换live-inputs-v2.json（SHA256 5f233b62856e594c8825a7f973fcb519642aa588e6798ec1089190c63c9cd804）：灰色耳机与白色T恤各仅选references/0.webp。去除页眉Logo本体化和标题营销功效，保留所选本体可辨事实；旧准备版不覆盖。两个checkout的v2 dry-run通过，分母仍12 prompt+12 image；尚无真实调用。可见性核查图为root-reference-review.jpg。


执行准备已冻结为v3：live-inputs-v3.json的SHA256为2e1a55fcabfd96f1cc010a30d4180eaab73e32de0a4150622f70422a54ecdb67，live-input-freeze-v3.json固定输入与分母；中性商品名跨臂/重复保持相同。新增live-stack-wrapper.mjs记录实际进程/二进制与DB身份，export-effective-provider-config.mjs导出非密钥有效配置，driver关联实际调用证据并拒绝输入/配置漂移。offline-driver-contract-gate-v3.json、offline-stack-wrapper-contract-v3.json和offline-negative-gate-v3.json记录假服务的正反路径；尚未运行真实ProductFlow栈或模型。image_live_review复审固定脚本，account_backend仅继续只读核实实际provider与隔离运行准备，不修改冻结脚本。v1/v2输入和失败均保留；真实质量结论仍待原定两臂对照。


v4修复已通过两臂完整离线driver流程和root差异审核：可比配置排除本地DB/profile身份但保留provenance，并强制两臂不同DB；图操作使用稳定节点角色；合法缺response ID时保留response/effect/output归因；wrapper纳入dispatcher。当前live-inputs-v4.json SHA256为377859ecceaa11b4b3bdf6ab9cf310b84fac32366fb316a5e668f0dde0d81c35，driver SHA256为60a572935eaee23ab04e5e9e27c2e0e48eb0051d2598e2fcc030d2ad6c30b721，wrapper SHA256为09b6ebf28218c998c6aa78c375a597b9ae09aa43f9a634c5dc1978c45fbe7c32。旧v3保留。

容量独占窗口已释放，account_backend获准建立两臂专用PG数据库、独立Redis实例与API/worker/dispatcher，执行无模型preflight。实际开发绑定经只读核实为prompt openai/gpt-5.5、image openai_responses/gpt-5.6-luna且responses_background_enabled=false，保持不变。真实调用前必须比较两臂语义配置与upstream、验证实际进程和标准PostgreSQL DSN，清空PF_LIVE_PROVIDER_EFFECTS_FILE及禁止--provider-effects，真实effect从PG读取；dispatcher必须以--watch启动。root核对真实preflight后才放行原12 prompt+12 image，当前尚无真实出图。两臂仍为9f3d25e7/5355a3bb，不混入后续独立worker信号修复907101a4。

真实非模型 preflight 已通过，证据位于 `storage-dev/category-generation-improvement-0908/preflight-v4/`。root 已放行原定调用额度。两臂 live 使用独立数据库 `pf_cat_v4_live_0908_b` / `pf_cat_v4_live_0908_c`，调用前 PostgreSQL 配置与实际 upstream 比较通过。

首个 baseline prompt 返回 HTTP 200，graph run `f76b9a4a-006a-4a7b-a26b-fd6a5d9af6ea` 成功，provider effect `1bcd0576-f498-4812-a199-27c016997e48` 为 applied。driver 在候选应用后未读到预期 current_artifact_id 而停止；已发生 1 次 prompt 调用、0 次 image 调用，candidate 尚未调用。失败记录 `/tmp/productflow-category-v4-live-0908/runs/baseline-v4/failure.json` 保留，不能计为完成的图像 trial。account_backend 正在利用已有产物定位驱动与真实应用契约，不重复模型调用、不修改冻结生产版本。

root 复核固定 baseline 的 ApplyDocumentCandidate：应用通过 UpdateNodeConfigOp 写入正式文稿配置并清除 pending 指针，不设置 current_artifact_id。该中止为 driver 契约错误。相关真实 Go 假 provider 回归通过（4.998s）。已批准保存 v4、新修订 driver 验证实际配置与 revision/pending/origin，并显式记录来源后复用已应用的 prompt 接续；累计调用分母仍为 12 prompt + 12 image，原失败不得覆盖。无生产代码修改。

旧配置下两侧首个暗色样本均停于 image unknown：各 1 次 prompt 成功、1 次 image 应用调用；每次 image 内部由 400 参数降级再发一次 HTTP，继而 502，总计 6 次实际 provider HTTP。candidate 的安全错误摘要明确为 `invalid_input_fidelity_model`，上游图片模型不支持 `input_fidelity`；随后错误为上游暂不可用。没有 response ID 或图片 artifact，未自动重试。失败与原分母保留，不能用于图片质量胜负。

root 批准新隔离配置修订：从两侧当前有效 `image_tool_allowed_fields` 中仅去掉 `input_fidelity`，保留所有其他原有字段；实际有效列表原本不含 background，不将 catalog 默认列表当作运行配置。工作流会按参考图要求补入 input_fidelity，故只清空 runtime 默认值不足以移除；复用现有白名单，不新增模型特判、配置开关或生产源码改动。调用前重新核对两侧配置与修订身份；新批次上限 12 prompt + 12 image 应用调用，实际 HTTP 另计。先两侧各一暗色样本，若继续同类 502 或契约失败则暂停批量；成功后继续原样本矩阵。旧配置的两个 unknown 不回填、不重抽为成功。

新配置两臂真实生成已完成：每臂 2 样本×3 次，合计 12 次 prompt、12 次 image 应用调用，24 次实际 HTTP 全为 200。两臂语义配置 SHA 为 a40c5dc9fe4b69a796585ad8a5449e4405fd02c272d692b4feb2b0d8907c4b9a；图片请求均不含 input_fidelity，旧 unknown 保留。比较与请求证据位于 `/tmp/productflow-category-v4-live-0908/live-arm-comparison-v6.json` 及 `new-batch-v2/`。root 逐一重算 12 个图片文件 SHA，与 manifest 所录 DB hash 一致，并查看两组原图缩略对照。

当前视觉观察：暗色组整体接近，尚不能确认稳定改善；白 T 恤候选 trial 2 出现模特穿着，其余五张为单品展示，root 回读冻结输入确认 layout 允许“商品/穿着主体居中”，因此模特出现本身不违反构图要求；呈现差异仍需在比较中说明。account_backend 继续既定 v2 重复多模态标注，保留可比性与身份判断、完整失败分母，不因单图更吸睛宣布质量提升；本单仍认领。

固定 v2 多模态评委已完成 6 对×3 次，共 18/18，unknown/failed 均为 0；15 次可比、3 次不同展示目的。root 按真实图片 SHA 独立还原盲化方向：暗色 stronger/equal/weaker=2/6/1，白 T 可比部分=1/3/2；白 T trial 2 三次均 different_purpose。证据 `storage-dev/category-generation-improvement-0908/comparison-summary-v6.json` 与 `root-judge-replay-v6.json`，包含原报告 hash。暗色保真/适用/效用均分差为 0，美观 +0.111，重复胜负不稳定。

root 判定本候选没有稳定质量收益，按原验收撤销 5355a3bb 中本候选生产提示词及专属回归，不撤销其他作者或后续正交改动。account_backend 正在原四文件范围执行删除及确定性验证；不重新生成或放宽胜负口径。两套生成旧失败、新批次和 18 次标注均保留，当前不宣称各类别达到竞品水平。

## 交付结论

root 已审核四文件完整反向差异，并逐字比对 5355a3bb 父版本一致。删除后的 prompts 与 graph 全包通过（加载 .env.dev，真实 PostgreSQL，分别 .002s/108.266s）；该次证据来自执行终端，没有另存日志，不虚构文件路径。任务以“局部候选未证实改善，移除候选”完成，随本任务提交；不增加替代指令。

两臂专用 API/worker/dispatcher、trace proxy、Redis 已停，两个专用数据库确认无连接后删除，专用端口释放；共享 29282/29283/29284 服务保留。所有生成图、失败与标注产物保留。root 独立重放 18 次比较方向和样本配对，结论与执行者汇总一致。
