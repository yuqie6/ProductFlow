# 图片质量组

本组负责合法样本、固定图片对照、质量归因、必要生成链修复与复验的完整结果。按 [业务组协调规则](README.md#职责裁定) 在组内串行交付，不依赖 Agent 行为分数或自进化控制器。

当前图片组由主代理-image-quality-0905-2245 接管。用户于 2026-09-05 确认原会话停止并移交组内开发与现有成果。接管复核发现 5 个参考/金标内容重叠样本；[内容隔离修复](tasks/archive/image-pool-disjoint.md) 后读取端接受 195 SKU，数量已获用户接受。逐图复核另发现参考身份和图种错标，不能把 195 个文件有效样本写成已验证的优质金标。[image-eval-pool](tasks/image-eval-pool.md) 保留原 42 图位合同并阻塞；[核心商品图诊断](tasks/image-quality-comparison.md) 使用同一批 8 SKU 校正后的 32 图位，独立记录实测结果。

## 组内交付

1. 按当前任务取得合法样本并记录完整抽样输入；登录、人工许可或真实 provider 缺失时如实阻塞，不绕验证码、不以合成金标冒充。
2. 固定 API/worker、模型、评委、抽样集合和预算运行对照；池不足与闸门失败分别记录，不因 Agent 其它阶段未完成而暂停可执行采证。
3. 有真实质量差距后按根因发布生成提示、参考输入、provider 适配或必要业务实现修复；相关回归与真实图片对照由同组执行。不得只因涉及工作流代码转出本组的完整修复。
4. 候选不得同时改评委或金标；评分合同缺陷另立先行任务，审核冻结后再比较。正常调用固定评测不需要其它组逐轮批准。

用户已授权组内开发；当前先交付采证任务，有证据的生成链修复按组内序列确认任务范围和占用后推进。共用 provider/DB/浏览器资源仍需排他预约。工作流操作正确性与执行可靠性的固定合同继续适用，不复制 Graph 或图片执行器。

<a id="image-quality"></a>

## 图片质量验收

以下 IMG 合同和历史证据从原评测账本迁入，数值门槛不变。图片四维评分独立于 Agent pass^k。池规模是任务记录，最新数量须按任务约定核验实际 manifest。

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
