# 图片质量组

本组负责合法样本、固定图片对照、质量归因、必要生成链修复与复验的完整结果。按 [业务组协调规则](README.md#职责裁定) 在组内串行交付，不依赖 Agent 行为分数或自进化控制器。

2026-09-05 用户确认原会话停止，主代理-image-quality-0905-2245 接管组内开发与现有成果；这是历史交接标识，当前执行者和占用以任务文件为准。接管复核发现 5 个参考/金标内容重叠样本；[内容隔离修复](tasks/archive/image-pool-disjoint.md) 后读取端接受 195 SKU，数量已获用户接受。逐图复核另发现参考身份和图种错标，不能把 195 个文件有效样本写成已验证的优质金标。[image-eval-pool](tasks/image-eval-pool.md) 保留原 42 图位合同并阻塞；[核心商品图诊断](tasks/image-quality-comparison.md) 使用同一批 8 SKU 校正后的 32 图位，独立记录实测结果。

## 正式版内容责任

按 [总纲第 6 节](../ROADMAP.md#6-核心竞争力的建设与验证)，本组承担事实依据、单图目的、商品保真、准确文字与视觉复用的质量合同。比较纯生成与保留主体/确定性组合路线时，分别记录适用输入、实际变化和失败，不把提示词约束当保真保证。文字/布局应检查内容正确、可读和导出一致性；品牌复用检查第二商品不继承来源身份。

当前优先补足原固定实验的有效输入与候选复验，`61426ed6` 单图卖点候选的改善仍待 [内容试跑](tasks/image-quality-content-pilot.md)。新的端到端竞品比较须独立冻结输入、操作预算和判读，不能覆盖旧 42/32 图位合同，不继续扩池凑数量。已授权研发不等于新增付费实验授权。

## 组内交付

1. [compete-facts-layout-contract](tasks/archive/compete-facts-layout-contract.md)：已冻结 IQ-CF-01…08 与 CF-B0…B5。[事实分层闸 CF-B0](tasks/archive/compete-facts-layer-gate.md) 已交付；下一项 [CF-B1 图位文字追溯](tasks/compete-facts-text-trace.md)。
2. [image-quality-content-pilot](tasks/image-quality-content-pilot.md)：固定两商品内容策略候选真实对照（费用上限内已授权）。
3. 有真实差距后按合同发布生成链/排版实现切片；不与旧 42/32 图位完成条件混写。
4. 候选不得同时改评委或金标；评分合同缺陷另立先行任务。

用户已授权组内开发；当前先交付采证任务，有证据的生成链修复按组内序列确认任务范围和占用后推进。共用 provider/DB/浏览器资源仍需排他预约。工作流操作正确性与执行可靠性的固定合同继续适用，不复制 Graph 或图片执行器。

<a id="compete-facts-layout"></a>

## 事实约束与排版分工合同

依据 [总纲 §6.1–§6.3](../ROADMAP.md#61-商品事实约束生产) 与 [compete-facts-layout-contract](tasks/archive/compete-facts-layout-contract.md)。本节冻结实施合同与批次；**不实现业务代码、不选定第三方 SDK、不改下方 IMG 旧 42/32 图位完成条件**。完成合同≠图片质量门 R3 通过。

冻结基线：认领时 HEAD `d6709c4aacb2e26bb30ab70a99d08b1dca05f487`（2026-09-07）。下列「现场枚举」对应该提交的只读结论。

### 现场枚举（对照源码，非复述总纲）

| 区域 | 已有 | 缺口（相对 §6.1–§6.3） |
|---|---|---|
| 商品事实 | `product/facts.go`：不可变 `product_fact_set_versions`；`source_type` 闭集 `user\|image_observation\|agent_inference`；`status` 闭集 `observed\|user_declared\|confirmed\|conflicted`；`layer` 闭集 `performance\|marketing`；`requires_confirmation` / `evidence_asset_ids` / `conflicts`；写入闸拒绝未确认推断升 `confirmed`、拒绝营销口吻入性能层 | 无事实变更→依赖图位的影响预览 API/UI（CF-B2） |
| 编译入边与 digest | `graph/compiler.go`：`incomingSorted` 只扫入边；`incomingFactSetVersions` 写入 digest；`execute_node.go` `skipUnchanged` 同 digest 跳过 | 变更后自动跳过≠用户可见的影响预览与「选择更新范围」；未连边资料不进运行输入（已正确，须保持） |
| 图位文字 | `image_prompt` Catalog 有 `fact_keys`；`listing_prompt.go` 组装「图片内文字」；`text_settings` policy `none\|required` | 无持久化「成片文字 → fact key」追溯；卖点「单图一购买理由」仍靠内容策略候选，非法证据未硬闸 |
| 配方清身份 | `recipe/payload.go` 剥 `source_product_id` / `fact_set_version_id` / `visual_system_version_id` / `visual_overrides` / `fact_keys` 等 | 结构复用已有；品牌/视觉方案版本继承与「第二商品不带旧身份」缺产品级继承链 |
| 主体/局部 | `localedit`：供应商 `masked_edit`（`remove\|replace_text\|inpaint`），非确定性抠图保真链 | **无**主体提取+背景/阴影+比例的保留主体产图路线；不能把 localedit 标成像素保真 |
| 生成式摄影 | `image_generation` + providers；提示词可含「保持一致」类文案 | 无路线标签区分「保留主体 vs 生成式」；无禁止用提示词宣称像素保真的合同闸 |
| 交付规格 | `delivery/spec.go` + `renderer.go`：确定性缩放/裁切/编码（png/jpeg/webp），`Render` 不调模型 | **不是**图内二维排版；无层/字体/安全区组合器 |
| 视觉方案 | 表 `visual_systems` / `visual_system_versions`；节点 `visual_overlay`；`mergeImageVisual` 本地 overlay 盖系统；配方 `preferred_visual_system_version_id` | **无** Brand 实体与品牌版本；总纲四级继承未落地；品牌更新静默改在做任务的防护未建 |
| 采用快照 | 体验组 [delivery-adoption-snapshot](tasks/archive/delivery-adoption-snapshot.md)（已交付） | 本组定义身份/文字**合格判据**；采用/导出 UX 与快照持久化归体验组 |

### 可执行合同条目

| ID | 合同 | 验证层 | 状态 |
|---|---|---|---|
| IQ-CF-01 | **事实来源分层**：每条事实保留 `source_type` 与 `status`；`agent_inference` / 未确认 `observed` 不得静默升为 `confirmed` 性能断言；`conflicted` 与 `requires_confirmation=true` 必须对用户可见且可裁定。品牌营销口吻（卖点文案）不得写入与规格/材质同级的「已确认性能事实」。 | 写入校验 + UI 展示 + 正反夹具；复用 `normalizeFactPayload` 闭集，扩展规则不得开第二事实仓库 | `完成`（`layer` 闭集 + 确认门/营销闸；资料面板分栏；`TestFactLayerGate*`） |
| IQ-CF-02 | **图位文字追溯**：信息图成稿中的可核验文字（规格、容量、材质、卖点短句）须能追到本商品 fact key 或显式「用户本图覆盖」标记；卖点图默认一图一主要购买理由。无依据文字不得进入交付采用合格集。 | 节点/产物元数据或导出旁路索引；对照 `fact_keys` 与成片 OCR/人工检；内容策略候选可作输入不得替代本闸 | `缺失` |
| IQ-CF-03 | **规格/事实变更影响预览**：确认事实新版本前，列出依赖该 fact（经 RoleFacts 入边 → prompt/generation 图位）的文案与图位；**已完成且 digest 不受影响的图不得自动重做**；用户显式选择更新范围。旧运行仍可通过当时 `fact_set_version_id` + `input_digest` 解释。 | 预览 API/用例 + `skipUnchanged` 回归；禁止全图扫描注入未连接资料 | `缺失`（digest/skip 已有，预览与范围选择无） |
| IQ-CF-04 | **主体保留路线**：有可靠主体图且需外观保真时，走主体提取→背景/阴影/位置比例→（可选）确定性排版；输出进入现有资产与交付链。透明/反光/遮挡边缘须质量检查，**不得宣称绝对像素保真**。 | 路线标签 + 质检失败留未解决项；与 localedit 供应商修补区分记账 | `缺失` |
| IQ-CF-05 | **生成式摄影路线**：新场景/创意摄影使用生成模型；记录身份参考与预期可变项；对照实果。提示词或 UI **不得**用「保持像素一致 / 像素级还原」等表述把本路线标成保留主体。 | 路线枚举 + 提示词/文案审计测试；失败留未解决项，不用均分掩盖身份错误 | `缺失`（生成链已有，路线合同与禁令未钉） |
| IQ-CF-06 | **受控二维排版边界**：营销字、规格、Logo 使用图内组合：图片层、文字层、必要形状、字体、字号、颜色、对齐、安全区。归属图片产出与 media lineage；**不**承担 DAG 调度，**不**第二工作流编辑器。只编辑本系统持有的结构；任意图片分层后置。浏览器预览与服务端导出同输入一致性（中文换行、缺字、长标题、像素尺寸）可测。技术选型在实现任务中按下方比较维度验证后选定，**本文件不指定 SDK**。 | 结构 schema + 预览/导出一致性测试；选型备忘只记比较结果 | `缺失`（DeliverySpec≠排版） |
| IQ-CF-07 | **品牌/视觉继承优先级**：本商品显式覆盖 > 选定视觉方案版本 > 品牌版本 > 产品默认。事实与身份参考来自目标商品，**不参与**风格继承链。实例保存所选版本 id；品牌/方案更新先列受影响商品，须显式采用新版本，不得静默改在做任务与旧交付。配方继续清除来源商品身份与 fact/visual 版本绑定。 | 继承解析单测 + 配方 extract/apply 回归 + 第二商品无旧身份样例 | `缺失`（visual_system / overlay 局部存在；Brand 与四级链未建） |
| IQ-CF-08 | **与采用快照交接**：本组判定「身份合格 / 文字合格 / 路线声明正确」；体验组 [delivery-adoption-snapshot](tasks/archive/delivery-adoption-snapshot.md) 负责采用动作、快照持久化、导出 UX。不合格图可生成但**不得**进入「已采用交付」合格集（由体验组消费本判据）。O1–O7 文稿采用 ≠ 交付采用。 | 跨组合同引用；本组提供判据表，不写采用表模型 | `部分完成`（体验组快照已交付；本组判据闸仍待 CF 批次） |

#### IQ-CF-06 技术选型比较维度（不选定实现）

实现切片启动前，对候选渲染/编辑组件（含自研最小集）逐项记分，**未经验证不得写入依赖**：

| 维度 | 必须可回答 |
|---|---|
| 能力 | 多图层、文本框、字体子集、对齐、安全区、导出 PNG/JPEG；中文换行与缺字行为 |
| 一致性 | 浏览器预览像素与服务端导出在固定夹具下的差异上限 |
| 许可 | 字体与引擎许可证是否允许自托管商用 |
| 维护 | 上游活跃度、安全响应、打包体积对 `web`/`go` 边界的影响 |
| 归属 | 排版状态存哪张表/哪个 artifact；失败是否进入现有 unknown 合同 |
| 非目标 | 不替代 Graph DAG；不做任意图片 PSD 级通用设计器 |

#### 采用合格判据（交体验组）

| 判据 | 合格 | 不合格（可生成，不可采用为交付） |
|---|---|---|
| 身份 | 可见主体与参考 SKU 一致；数量/部件与确认事实一致（例：两耳塞不得成三） | 身份漂移、未解决质检项、路线声称像素保真但实为生成式 |
| 文字 | 成片可核验字可追到 fact 或本图覆盖；无未确认性能参数上图 | 包装臆造参数、无依据功效、卖点多理由堆砌且无法追溯 |
| 规格变更后 | 用户未选入更新范围的已完成图保持原 artifact | 静默重做未受影响图位 |
| 品牌复用 | 第二商品仅继承风格链；facts/参考为新商品 | 带入来源商品文案、fact 版本或身份参考 |

### 实施批次

总约束：每批可独立验收；不改 IMG-D/C 与 42/32 图位门槛；不调用真实 provider 完成本合同任务；实现批须另发看板 issue 并确认占用。依赖：CF-B0→CF-B1→CF-B2；CF-B3 可与 CF-B1 并行；CF-B4 依赖 CF-B3 路线标签；CF-B5 依赖 CF-B0 且与商家平台 Brand 骨架衔接（Brand 表未建时先落视觉方案版本+商品覆盖，品牌层用显式占位合同）。

| 批次 | 名称 | 精确范围 | 正测样例 | 反测样例 | 验证层 |
|---|---|---|---|---|---|
| **CF-B0** | 事实来源分层闸 | 扩展/收紧 facts 写入与展示：确认门、冲突可见、营销文案与性能事实分栏；不新建事实仓库 | 用户确认容量 `600ml`→`confirmed`+`user`；图观材质→`image_observation` 待确认 | `agent_inference`「保温 24h」未确认即当 `confirmed` 性能；口吻「明星同款」写入规格事实 | `完成`（`product/facts*` + 资料面板分栏；证据见 [compete-facts-layer-gate](tasks/archive/compete-facts-layer-gate.md)） |
| **CF-B1** | 图位文字追溯 | prompt/generation 产物记录文字所用 `fact_keys` 或本图覆盖；卖点一图一理由检查器（可先非 live） | 规格图「600ml」← fact `capacity`；保温杯卖点只强调「轻量杯身」且有依据 | 成片「24h 保温」无 fact；卖点同时堆三句无关口号且无 key | 产物元数据单测；抽检清单；内容试点可对照但非本批完成条件 |
| **CF-B2** | 变更影响预览 | 事实保存前预览依赖图位；用户多选更新；未选中已完成节点保持 artifact；解释旧 `fact_set_version_id` | 改 500→600ml：列出规格/卖点图；场景图无容量字则默认不入更新集且不重跑 | 改容量后全图自动重跑；预览注入未连边节点资料 | 预览 API + `skipUnchanged`/`incomingFactSetVersions` 回归 |
| **CF-B3** | 双路线声明 | 图位/运行记录 `produce_route=subject_preserve\|generative`；生成式禁像素保真文案；保留主体路线失败→未解决项 | 主图 `subject_preserve`；场景 `generative` 且 UI 显示「可能改变外观」 | 生成式路线提示词含「像素级一致」；保留主体失败仍标已交付合格 | 枚举/文案审计测试；路线字段持久化 |
| **CF-B4** | 受控二维排版 | 先完成选型比较备忘（上表维度），再最小结构：层、字体、安全区、预览/导出一致性；接现有资产 lineage | 规格字排版改字号不重生成杯身；预览与导出同夹具一致 | 排版服务调度 Graph 节点；未经验证 SDK 直接进主依赖；缺字静默空白当合格 | 选型备忘审阅 + 一致性夹具；**本证据任务只冻结维度，不选型** |
| **CF-B5** | 品牌/视觉继承 | 四级优先级解析；实例存版本 id；更新需显式采用；配方清身份保持；第二商品样例 | 商品 overlay 色盖方案色盖品牌色盖默认；新杯复用方案但容量为新 fact | 事实/参考进入风格链；品牌更新静默改旧交付；配方带回 `source_product_id` | 解析单测 + recipe payload 回归 + 手工第二商品清单 |

#### 保温杯贯通流程 → 批次（可自动化部分）

| 总纲 §6.6 步骤 | 可自动化断言 | 批次 |
|---|---|---|
| 上传参考、已知容量/材质；不可确认保温时长留空 | 无证据 key 不得 `confirmed`；留空不造值 | CF-B0 |
| 删无依据性能宣传；改清单 | 冲突/未确认可见；保存产新 fact 版本 | CF-B0 |
| 主图保留主体、场景生成式、规格受控排版；费用/预期变化运行前可见 | 路线标签与预期变化字段 | CF-B3、CF-B4 |
| 改标题/字位不重生成杯身 | 排版重出、主体资产不变 | CF-B4 |
| 采用与规格文件；重跑不改已交付 | 合格判据 IQ-CF-08；快照归体验组 | CF-B1 判据 + delivery-adoption-snapshot |
| 第二商品复用布局/视觉；新容量与参考 | 继承链与清身份 | CF-B5 |

### 未知项与非本任务范围

- Brand 实体与商户多品牌 UI 归属商家平台；本组只钉继承优先级与质量判据，表结构实现可与商家批次衔接。
- 主体提取算法/模型未选型；CF-B3 先钉路线与禁令，CF-B4/实现另发。
- 成片文字自动 OCR 追溯是否首版必备未裁定；CF-B1 允许「元数据声明 + 抽检」先行，OCR 闸另发。
- 旧 42 图位池与 32 图位诊断合同保持原状态；本竞争力合同样本独立冻结，不得回写 IMG live 表凑数。

<a id="image-quality"></a>

## 图片质量验收

以下 IMG 合同和历史证据从原评测账本迁入，数值门槛不变。图片四维评分独立于 Agent pass^k。池规模是任务记录，最新数量须按任务约定核验实际 manifest。**本节完成条件与上方 IQ-CF / CF-B\* 相互独立。**

### 来源与使用规则

- 来源：2026-09-05 会话计划《生图质量测评与节点打分拆离》。原始消息没有可在仓库中复核的独立附件哈希。
- 适用范围：`go/internal/imageeval`、`go/cmd/productflow-image-evals`、`evals/image/extract-taobao.js`、`evals/image/extract-search.js`、`evals/image/load-detail.js`、opt-in `just image-evals-*`、本账本。
- 金标只进评委，不进 image provider 参考输入，也不对原 listing 做局部修改。
- 原始图只进入 `STORAGE_ROOT/image-evals/`（本地 `storage-dev/` 已 gitignore）。账本只抄 `run_id`、抽样种子、采集类目、均分、pass/fail。
- 状态变更必须引用当前代码、自动化测试或真实运行结果。没有 `run_id` 不得写「已通过」。

#### 状态四值

| 状态 | 判定规则 |
|---|---|
| `完成` | 当前实现、贴近合同的自动化测试和条款要求的真实运行证据都存在。 |
| `部分完成` | 已有可执行实现或既有证据，但池规模、live 基础设施或复跑证据不全。 |
| `缺失` | 实现不存在，或当前证据不足以判断。 |
| `违背` | 当前实现明确采用冻结决策禁止的合同。 |

### 冻结决策

| ID | 决策 | 状态 | 当前证据与验收缺口 |
|---|---|---|---|
| IMG-D-01 | 金标来自已登录淘宝详情的完整套图（主图相册 + 详情模块）；不做拼多多；不用猜你喜欢当抽样框 | `部分完成` | 200 个 manifest 均标记 source=taobao，内容隔离后读取端接受 195 个。来源记录见 [image-eval-pool](tasks/image-eval-pool.md#证据)。数量已接受，但来源平台不能证明设计优秀；语义分类与参考身份仍有缺口。 |
| IMG-D-02 | 薄 listing 整单丢：主图相册 < 5 或详情模块 < 8，或缺 `hero`+`selling_point`+(`detail` 或 `scene`) | `部分完成` | 数量准入与薄 listing 拒绝测试存在；2026-09-05 视觉复核发现卖点槽含跨商品推荐、场景槽含参数切片。标签数量过线不能证明具备对应图种。原 42 图位测评中止。 |
| IMG-D-03 | 虚拟货、买家秀不当金标；身份参考最多 6 张 SKU/白底/包装；单图不得既当参考又当金标 | `部分完成` | 下载字节 SHA-256 隔离已修复，5 个重叠样本排除，见 [内容隔离修复](tasks/archive/image-pool-disjoint.md)。但珀莱雅原参考为赠品合集、BENNS 原参考为海盐原料海报；原池身份参考语义未全面验证。本次 8 SKU 单独校正并冻结。 |
| IMG-D-04 | 画布起始走 `POST /api/v3/products` → `graph.BuildDirectCreateTemplate`；测评 k=1 每个过线图种一张 | `完成` | `harness.go` `evalTypeCounts`；`TestEvalTypeCountsK1`。不硬凑推荐 10 张，也不把套图截成两三种随机图。 |
| IMG-D-05 | 对照臂：同一 image provider；直调提示词冻结在 `naive.go`；k=1 | `完成` | `TestNaivePromptCoversGeneratingTypes`；harness `ModeChat` + `NaivePrompt`。 |
| IMG-D-06 | 评委与生图模型分开；四维 1–5：保真、适配、实用、美观 | `完成` | `judge.go` schema 与 `TestParseJudgeJSON`。live 评委优先 `/v1/responses`，失败再试 `chat.completions`。可用 `IMAGE_EVAL_JUDGE_*` 覆盖。 |
| IMG-D-07 | 闸门：有金标则工作台均分 >= 金标；必须严格赢过直调；保真不得低于金标（若有）和直调 | `完成` | `GateSlot` 与 `TestGateSlotWorkbenchMustBeatNaiveAndHoldGold`。 |
| IMG-D-08 | 工作台不提供节点人工保真检查；不把五星抽样表单当作本测评 | `完成` | 已删 `product/fidelity.go`、保真 HTTP、`web/src/pages/workbench/fidelity/`、`imageFidelityChecks.ts`。检查器测试断言不再渲染「人工保真检查」。`GenerationSpec.reference_fidelity` 与文稿 `product_fidelity` 仍是生图参数。 |
| IMG-D-09 | 分层随机抽样；种子写入 `run.json` / `report.json` | `完成` | `sample.go`、`TestSampleStratifiedReproducible`。 |
| IMG-D-10 | live 必须 `PRODUCTFLOW_RUN_IMAGE_EVALS=1`；无 run_id 不得写已通过 | `部分完成` | 环境门闩 `TestRunSampledRequiresSwitch`。第一轮有分数的 `run_id` 见 live 表；闸门未过，不得写已通过。 |

### 采集与准入

| ID | 验收要求 | 状态 | Owner / 证据 / 缺口 |
|---|---|---|---|
| IMG-C-01 | 浏览器会话负责打开页面并抽出 URL；本机下载 bytes 并准入。不另起无 cookie 爬虫，不绕滑块/验证码 | `完成` | `evals/image/extract-search.js`、`load-detail.js`（懒加载图文详情，遇验证码只回报）、`extract-taobao.js` + `IngestListing`。风控出现即停。 |
| IMG-C-02 | 过线 case 落 `STORAGE_ROOT/image-evals/pool/<id>/manifest.json` + `references/` + `gold/` | `完成` | `pool.go`、`TestPoolRoundTrip`、`TestIngestListingAdmitsCompleteFixture`。 |
| IMG-C-03 | 本轮池规模：用户接受现有有效池；单次测评固定抽样 | `完成` | 2026-09-05 用户确认 195 SKU / 9 类目数量足够，取消本轮合计 200、每类 20 的补采前置。5 个参考/金标污染样本继续排除，准入和质量评分门槛不变；该裁定发生在本轮正式 live 前。数量与新抽样见 [采证任务](tasks/image-eval-pool.md#证据)。 |

类目检索词（不用猜你喜欢）：`imageeval.CategorySeeds`（3c / appliance / home / womenswear / menswear / beauty / food / baby / sports）。

### Harness

命令：

```bash
just image-evals-ingest storage-dev/image-evals/inbox
just image-evals-sample 20 1
PRODUCTFLOW_RUN_IMAGE_EVALS=1 just image-evals-run 8 1
just image-evals-report <run_id>
```

评委默认使用当前 prompt 绑定，优先 `/v1/responses`。若需要覆盖，设置 `IMAGE_EVAL_JUDGE_BASE_URL`、`IMAGE_EVAL_JUDGE_API_KEY`、`IMAGE_EVAL_JUDGE_MODEL`。

完整商品输入对照支持先调用现有商品 AI 表单接口，再固定结果供多个候选复用。准备命令每个抽样商品调用一次，只上传身份参考；未知规格不进入 source_note。输入文件记录商品身份、参考字节摘要、AI 原始表单和最终 source_note；最终文本可在冻结前审核编辑。代码不能证明 AI 提取内容正确，人工复核仍需记录在对应实测合同中。

```bash
PRODUCTFLOW_RUN_IMAGE_EVALS=1 just image-evals-prepare-inputs /absolute/path/product-inputs.json 2 1
PRODUCTFLOW_RUN_IMAGE_EVALS=1 just image-evals-run 2 1 /absolute/path/product-inputs.json
```

准备命令拒绝覆盖已有文件；调用失败保留部分结果并停止，不自动重试。运行前验证整个抽样集的输入与实际参考字节，输入缺失或身份变化时不创建商品。报告在生图前写入输入，并记录 `input_mode`、最终文本和冻结文件 SHA-256；省略输入文件则明确记作 `title_props` 基线。A/B 必须复用同一冻结文件，不能每轮重新生成商品说明。命令需要隔离的 API/worker、现有模型绑定及该批费用授权；新增入口本身不授予费用权限。实现与确定性证据见 [商品输入冻结](tasks/archive/image-eval-product-input.md)，尚未运行新的真实模型批次，不构成质量改善结论。

生成内容策略已交付一个[待实图验证的候选](tasks/archive/image-quality-content-candidate.md)：统一单张卖点图一个购买理由，按图种选择相应信息，区分策划标签与成稿，场景交代部件状态，细节使用可见结构证据。该候选复用已有节点和字段，不代表多系列作用域重构已完成。确定性回归通过，空气炸锅与家纺的[成对试验](tasks/image-quality-content-pilot.md)已准备原始材料，等待新批次费用授权；没有新的质量改善分数。

### Live 记录

| 日期 | run_id | 种子 | n | 采集类目 | 均分/闸门 | 判定 |
|---|---|---|---|---|---|---|
| 2026-09-05 | `20260904T173413Z-d6b5915c` | 1 | 1 | 3c（华为 FreeBuds 6i） | 工作台 4.5 / 金标 2.6 / 直调 4.2；scene 输直调（保真 4<5） | **未过闸门**。hero/selling_point/detail 过；scene 未严格赢过直调。不得写已通过。 |
| 2026-09-05 | `20260904T172836Z-d6b5915c` | 1 | 1 | 3c | 无槽位分数 | 评委当时打 `chat.completions` 收到 HTML。随后改为 `/v1/responses`。 |

### 2026-09-06 核心图诊断

固定候选 `36ade164`，8 个商品、每个主图/卖点/场景/细节各一张。运行 `20260905T152704Z-546201ca` 与续测 `20260905T155629Z-62325dfc` 合计取得 24/32 图位三方评分；浴巾和跑鞋因供应商结果 unknown 缺少 8 个图位，不能关闭完整采证合同。原 42 图位合同也未完成。材料和异常细节见 [核心图诊断任务](tasks/image-quality-comparison.md)。

| 图种 | 有效图位 | 工作台均分 | 直调均分 | 商家原图均分 | 工作台对直调胜/平/负 |
|---|---|---|---|---|---|
| 主图 | 6 | 4.58 | 4.29 | 3.42 | 4/1/1 |
| 卖点 | 6 | 4.33 | 4.50 | 3.67 | 2/0/4 |
| 场景 | 6 | 4.54 | 4.50 | 4.29 | 2/3/1 |
| 细节 | 6 | 4.38 | 4.08 | 4.08 | 3/2/1 |
| 合计 | 24 | 4.46 | 4.34 | 3.86 | 11/6/7 |

10/24 图位满足既有闸门。工作台优势集中在部分主图和细节图，卖点图对直调偏弱。逐图复核发现：耳机场景有佩戴1只、手持1只、仓内1只，共3只，直调正常2只；评委未指出该错误。空气炸锅的“卖点1/卖点2”在生成文稿正文中已出现，并进入成片。洁面图和空气炸锅多个图位偏向重复摆拍；巧克力较好的设计大量沿用参考广告的现有构图。

本轮不能用商家原图均分较低证明超过优秀商业设计。评委非盲、每图只评一次，其单图职责偏好会处罚商家多模块海报；部分金标功效/结构证据也未提供给生成链。此轮主要评原始生成图，不证明平台交付规格或真实转化率。

创建页已有 AI 看图填写商品说明和规格，并将编辑后的表单提交为 source_note。本轮 harness 没有调用该步骤，仅使用标题和空 props，因此属性缺失属于本次评测输入限制，不能归为产品没有卖点提取功能。下一次完整流程比较需要固定该表单输出。系列/单图约束设计仍处于讨论，当前没有将视觉趋同全部归因于全局风格：空气炸锅创作要求本已要求不同背景、光线与景别，最终仍有重复。

以下为接管前的历史采集笔记，规模和模型配置不代表当前有效池或当前运行设置；当前复核结论见 IMG-C-03。

过线池（像素在 `storage-dev/image-evals/pool/`，不进 git）：150 SKU / 9 类目。3c 华为耳机/漫步者/飞利浦 TAT2569/倍思 m4s/绿联 T6s/SoundPEATS/QCY/惠普机械键盘/达尔优 dk100/联想 M130/雷柏 M350G/前行者 DK63、appliance 美的空气炸锅 + 米家/苏泊尔/海尔/飞利浦/追觅吸尘器与美的/九阳/苏泊尔复古/小熊/沁园电热水壶 + 九阳/苏泊尔/飞利浦空气炸锅、home ymer 马克杯 + 方迪/飞旺藤达/蔓斯菲尔/赛杉实木餐椅 + 无印良品家纺宿舍三件套 + 海澜之家长绒棉四件套 + 苏萱家纺全棉四件套 + 朵罗塔条纹/黄条纹/小猫/紫樱桃四件套 + 洁丽雅/雅鹿/网易严选/南极人/恩兴/水漾/莱美秋/华锦添四件套、sports ASICS + 迪卡侬/安踏/361/特步/维动/Keep/鸿星尔克 + 安踏凌风2/匹克态极/特步羽逸/安踏心率SE + 哈宇百合/Umay/品健瑜伽垫 + 回力/李宁吾适6跑步鞋、womenswear 伊芙丽/诗凡黎/MUJI/红袖 + 米施尔/魅序/BLANCWING/朵菲卡/帛梵希真丝衬衫 + 迪赛尼斯/UR/库恩玛维/orange desire/逸芬梦纺/香影/三彩 ibudu 羊毛大衣 + 真维斯/FOREVER21/名创优品/自然涩牛仔裤、menswear 棉的美学/迪卡侬/网易严选/森马/特步 + HOMEPANDA/班尼路/凡客/元宿纯棉 T 恤 + 啄木鸟/马克华菲西裤 + 七匹狼牛津纺衬衫、beauty 芙丽芳丝/瑷尔博士/敷尔佳/欧莱雅/摇滚动物园/Mistine/玉泽/高姿/笙木之源/馥珮/曼秀雷敦/吾诺/米云/尊界、food 良品铺子/三只松鼠森林礼/八马/西湖牌龙井/德芙醇黑/法布朗/周和利/senz 炫彩礼盒/鼎总正山小种/爱普诗/百利芙/萝西/BENNS + 式尚大红袍/唐翼铁观音/随方就圆组合/石草池六大名茶/墨峰金骏眉/半春茗碧螺春、baby 丸丫T6max/T9max/逸乐途F2/洛可适/小虎子T3/十月结晶/乔治熊/贝因美/百亿补贴纱布浴巾 + 凯利 102pro/贝丽可推车 + Ckbebe/梦呵奶瓶 + 呢哺/金号/田客/爱尔思/友信/水星纱布浴巾。CeraVe、丸丫T2 主图 uniq<5 未进池；HEYREAL `784337215490`、迷语 `684463536592`、塔莉尔 `1062680522391`、科巢 `611702287228`、senz 16 粒 `600453204144`、川岛屋 `751945754248`、后海 `18938025979`、莫等闲 `884729816334`、三月理 `672733948281`、黑爵 ak992 `697664601559` 图文详情 captchadrag 未进；梦巢家居 `868905501928`、古缇思 `655980424944`、梁丰麦丽素 `738433052134`、隐狼世家 `676384717625`、风荷牛仔裤 `666797760560`、啄木鸟牛津纺衬衫 `828610142567`、美的居家电器电热水壶 `1064515543918`、臻羞洁面 `708083064010` 整页验证码拦截未进；SANA `624874877334` 海外无商品页未进；梁丰金莎系列 `587847485935` 详情模块 7、绘春 `1028374267505` 有效详情不足 8、惜玥洁面膏 `675252866938` 主图 4、乐初妍洁面 `843032916657` 详情 5、pepebear 瑜伽垫 `822044643467` 详情 7、品芗毛尖 `668485566735` 主图 4 未进；华硕 MW107 `1063492718814`、NIKE JOURNEY RUN `1056420869860` 图文详情 punish 未进；安踏男士健身垫 `924689126972`、真维斯西裤 `1014247563209`、TugTug 婴儿浴巾 `944964386595` 整页验证码拦截未进。均搜索点进，`xxc=taobaoSearch` 或广告卡 `xxc=ad_ztc`。生图 `openai-responses`，评委 `gpt-5.5`。
