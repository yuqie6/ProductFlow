# 图片质量组

本组负责合法样本、固定图片对照、质量归因、必要生成链修复与复验的完整结果。按 [业务组协调规则](README.md#职责裁定) 在组内串行交付，不依赖 Agent 行为分数或自进化控制器。

当前图片组由主代理-image-quality-0905-2245 接管。用户于 2026-09-05 确认原会话停止并移交组内开发与现有成果。接管复核发现 5 个参考/金标内容重叠样本；[内容隔离修复](tasks/archive/image-pool-disjoint.md) 后有效池为 195 SKU。[image-eval-pool](tasks/image-eval-pool.md) 保留原采集与 n=8 live 合同，正式 live 尚未完成。

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
| IMG-D-01 | 金标来自已登录淘宝详情的完整套图（主图相册 + 详情模块）；不做拼多多；不用猜你喜欢当抽样框 | `部分完成` | 已接收原采集证据；200 个 manifest 均标记 source=taobao，内容隔离复核后有效 195 个。来源采集过程见 [image-eval-pool](tasks/image-eval-pool.md#证据) 原记录，本次只复核落盘文件；剩余规模缺口见 IMG-C-03。 |
| IMG-D-02 | 薄 listing 整单丢：主图相册 < 5 或详情模块 < 8，或缺 `hero`+`selling_point`+(`detail` 或 `scene`) | `完成` | `admit.go` 与 `eval_test.go` `TestAdmitRejectsVirtualAndThin` / `TestAdmitCompleteListing`。安热沙薄详情（主图 3 / 详情模块不足）未进池。 |
| IMG-D-03 | 虚拟货、买家秀不当金标；身份参考最多 6 张 SKU/白底/包装；单图不得既当参考又当金标 | `完成` | `virtual.go`、`IsUGCImage`；准入按下载字节 SHA-256 隔离参考/金标，池读取排除污染 manifest。接管查出的 5 个污染样本不进入 live，原始文件保留。见 [内容隔离修复](tasks/archive/image-pool-disjoint.md)。 |
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
| IMG-C-03 | 池目标：每类目几十个完整套图，跨层合计数百；单次测评再抽样 | `部分完成` | 原始池 200 SKU，排除 5 个参考/金标污染样本后有效 195 SKU / 9 类目；3c 与 womenswear 各 19，未达合计 200、每类 20。有效类目数量和新抽样见 [采证任务](tasks/image-eval-pool.md#证据)。 |

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

### Live 记录

| 日期 | run_id | 种子 | n | 采集类目 | 均分/闸门 | 判定 |
|---|---|---|---|---|---|---|
| 2026-09-05 | `20260904T173413Z-d6b5915c` | 1 | 1 | 3c（华为 FreeBuds 6i） | 工作台 4.5 / 金标 2.6 / 直调 4.2；scene 输直调（保真 4<5） | **未过闸门**。hero/selling_point/detail 过；scene 未严格赢过直调。不得写已通过。 |
| 2026-09-05 | `20260904T172836Z-d6b5915c` | 1 | 1 | 3c | 无槽位分数 | 评委当时打 `chat.completions` 收到 HTML。随后改为 `/v1/responses`。 |

以下为接管前的历史采集笔记，规模和模型配置不代表当前有效池或当前运行设置；当前复核结论见 IMG-C-03。

过线池（像素在 `storage-dev/image-evals/pool/`，不进 git）：28 SKU / 9 类目。3c 华为耳机/惠普机械键盘、appliance 美的空气炸锅、home ymer 马克杯、sports ASICS、womenswear 伊芙丽/诗凡黎/MUJI/红袖、menswear 棉的美学/迪卡侬/网易严选/森马/特步、beauty 芙丽芳丝/瑷尔博士/敷尔佳、food 良品铺子/三只松鼠、baby 丸丫T6max/T9max/逸乐途F2/洛可适/小虎子T3/十月结晶/乔治熊/贝因美/百亿补贴纱布浴巾。CeraVe、丸丫T2 主图 uniq<5 未进池；amorhome、田客纱布浴巾验证码拦截未进。均搜索点进，`xxc=taobaoSearch`。生图 `openai-responses`，评委 `gpt-5.5`。
