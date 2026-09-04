# 生图质量测评验收账本

本账本管理内部淘宝完整套图对照测评：过线池、分层抽样、工作台整图 vs 一句直调 vs 金标、视觉评委闸门。它不替代工作台交互合同，也不把节点人工打分当作质量标准。

## 来源与使用规则

- 来源：2026-09-05 会话计划《生图质量测评与节点打分拆离》。原始消息没有可在仓库中复核的独立附件哈希。
- 适用范围：`go/internal/imageeval`、`go/cmd/productflow-image-evals`、`evals/image/extract-taobao.js`、opt-in `just image-evals-*`、本账本。
- 金标只进评委，不进 image provider 参考输入，也不对原 listing 做局部修改。
- 原始图只进入 `STORAGE_ROOT/image-evals/`（本地 `storage-dev/` 已 gitignore）。账本只抄 `run_id`、抽样种子、采集类目、均分、pass/fail。
- 状态变更必须引用当前代码、自动化测试或真实运行结果。没有 `run_id` 不得写「已通过」。

### 状态四值

| 状态 | 判定规则 |
|---|---|
| `完成` | 当前实现、贴近合同的自动化测试和条款要求的真实运行证据都存在。 |
| `部分完成` | 已有可执行实现或既有证据，但池规模、live 基础设施或复跑证据不全。 |
| `缺失` | 实现不存在，或当前证据不足以判断。 |
| `违背` | 当前实现明确采用冻结决策禁止的合同。 |

## 冻结决策

| ID | 决策 | 状态 | 当前证据与验收缺口 |
|---|---|---|---|
| D-01 | 金标来自已登录淘宝详情的完整套图（主图相册 + 详情模块）；不做拼多多；不用猜你喜欢当抽样框 | `部分完成` | 抽取脚本 `evals/image/extract-taobao.js`；ingest 只接受 `source=taobao`。过线池 4 个 SKU / 4 类目（3c、appliance、home、sports）。未达每层几十、合计数百。 |
| D-02 | 薄 listing 整单丢：主图相册 < 5 或详情模块 < 8，或缺 `hero`+`selling_point`+(`detail` 或 `scene`) | `完成` | `admit.go` 与 `eval_test.go` `TestAdmitRejectsVirtualAndThin` / `TestAdmitCompleteListing`。安热沙薄详情（主图 3 / 详情模块不足）未进池。 |
| D-03 | 虚拟货、买家秀不当金标；身份参考最多 6 张 SKU/白底/包装；单图不得既当参考又当金标 | `完成` | `virtual.go`、`Admit` 参考/金标拆分；UGC 过滤 `IsUGCImage`。 |
| D-04 | 画布起始走 `POST /api/v3/products` → `graph.BuildDirectCreateTemplate`；测评 k=1 每个过线图种一张 | `完成` | `harness.go` `evalTypeCounts`；`TestEvalTypeCountsK1`。不硬凑推荐 10 张，也不把套图截成两三种随机图。 |
| D-05 | 对照臂：同一 image provider；直调提示词冻结在 `naive.go`；k=1 | `完成` | `TestNaivePromptCoversGeneratingTypes`；harness `ModeChat` + `NaivePrompt`。 |
| D-06 | 评委与生图模型分开；四维 1–5：保真、适配、实用、美观 | `完成` | `judge.go` schema 与 `TestParseJudgeJSON`。live 评委优先 `/v1/responses`，失败再试 `chat.completions`。可用 `IMAGE_EVAL_JUDGE_*` 覆盖。 |
| D-07 | 闸门：有金标则工作台均分 >= 金标；必须严格赢过直调；保真不得低于金标（若有）和直调 | `完成` | `GateSlot` 与 `TestGateSlotWorkbenchMustBeatNaiveAndHoldGold`。 |
| D-08 | 工作台不提供节点人工保真检查；不把五星抽样表单当作本测评 | `完成` | 已删 `product/fidelity.go`、保真 HTTP、`web/src/pages/workbench/fidelity/`、`imageFidelityChecks.ts`。检查器测试断言不再渲染「人工保真检查」。`GenerationSpec.reference_fidelity` 与文稿 `product_fidelity` 仍是生图参数。 |
| D-09 | 分层随机抽样；种子写入 `run.json` / `report.json` | `完成` | `sample.go`、`TestSampleStratifiedReproducible`。 |
| D-10 | live 必须 `PRODUCTFLOW_RUN_IMAGE_EVALS=1`；无 run_id 不得写已通过 | `部分完成` | 环境门闩 `TestRunSampledRequiresSwitch`。第一轮有分数的 `run_id` 见 live 表；闸门未过，不得写已通过。 |

## 采集与准入

| ID | 验收要求 | 状态 | Owner / 证据 / 缺口 |
|---|---|---|---|
| C-01 | 浏览器会话负责打开页面并抽出 URL；本机下载 bytes 并准入。不另起无 cookie 爬虫，不绕滑块/验证码 | `完成` | `evals/image/extract-taobao.js` + `IngestListing`。风控出现即停。 |
| C-02 | 过线 case 落 `STORAGE_ROOT/image-evals/pool/<id>/manifest.json` + `references/` + `gold/` | `完成` | `pool.go`、`TestPoolRoundTrip`、`TestIngestListingAdmitsCompleteFixture`。 |
| C-03 | 池目标：每类目几十个完整套图，跨层合计数百；单次测评再抽样 | `部分完成` | 抽样器已接线。当前过线 4 SKU / 4 类目。未达数百。 |

类目检索词（不用猜你喜欢）：`imageeval.CategorySeeds`（3c / appliance / home / womenswear / menswear / beauty / food / baby / sports）。

## Harness

命令：

```bash
just image-evals-ingest storage-dev/image-evals/inbox
just image-evals-sample 20 1
PRODUCTFLOW_RUN_IMAGE_EVALS=1 just image-evals-run 8 1
just image-evals-report <run_id>
```

评委默认使用当前 prompt 绑定，优先 `/v1/responses`。若需要覆盖，设置 `IMAGE_EVAL_JUDGE_BASE_URL`、`IMAGE_EVAL_JUDGE_API_KEY`、`IMAGE_EVAL_JUDGE_MODEL`。

## Live 记录

| 日期 | run_id | 种子 | n | 采集类目 | 均分/闸门 | 判定 |
|---|---|---|---|---|---|---|
| 2026-09-05 | `20260904T173413Z-d6b5915c` | 1 | 1 | 3c（华为 FreeBuds 6i） | 工作台 4.5 / 金标 2.6 / 直调 4.2；scene 输直调（保真 4<5） | **未过闸门**。hero/selling_point/detail 过；scene 未严格赢过直调。不得写已通过。 |
| 2026-09-05 | `20260904T172836Z-d6b5915c` | 1 | 1 | 3c | 无槽位分数 | 评委当时打 `chat.completions` 收到 HTML。随后改为 `/v1/responses`。 |

过线池（像素在 `storage-dev/image-evals/pool/`，不进 git）：`70710f2c4bd18ab6` 3c 华为耳机、`e709588c667af802` appliance 美的空气炸锅、`b3755e27853317e4` home ymer 马克杯、`7e85f3759fa2dc5d` sports ASICS 跑鞋。生图 `openai-responses`，评委 `gpt-5.5`。
