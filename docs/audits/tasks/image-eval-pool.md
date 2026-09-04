# 任务：淘宝过线池与生图闸门 live

状态：开放
认领者：—
认领于：—
业务组：生图测评
父账本：image-quality-eval.md
完成后可拆：合计仍 <200 或每类目不足则发 image-eval-pool-grow.md（合同同本任务）。闸门未过不另发刷分任务

读完本文件就可以做。认领前不要改「只改这些文件」。认领步骤见 [README.md](README.md)。不要重写 ingest/admit/harness。

## 做成什么样

1. 过线池逼近每类目数十、跨类目合计 ≥200 个完整套图。账本登记过线 23 SKU / 9 类目，未达每类目≥20、合计≥200。
2. 登记一次抽样 n 足够、四维均分齐全的 live `run_id`。闸门过不过都如实写；没有 `run_id` 不得写已通过。

金标来自已登录淘宝详情（主图相册 + 详情模块）。不做拼多多。不用猜你喜欢当抽样框。

## 只改这些文件

- `evals/image/` 抽取脚本（仅当抽取坏了；默认不要改）
- 本文件的池规模与 live 表
- 像素只进 `STORAGE_ROOT/image-evals/`（gitignore，不提交）

## 不要碰

- `go/internal/imageeval` 的准入阈值、闸门公式、naive 提示词（已有测试钉死）
- 工作台检查器、已删除的人工保真表单
- Agent 评测

## 合同（抄齐）

- 薄 listing 整单丢：主图相册 < 5 或详情模块 < 8，或缺 `hero`+`selling_point`+(`detail` 或 `scene`)
- 虚拟货、买家秀不当金标；身份参考最多 6 张；单图不得既当参考又当金标
- 工作台走 `POST /api/v3/products` → `BuildDirectCreateTemplate`；k=1，每个过线图种一张
- 对照臂同一 image provider；直调提示词在 `naive.go`
- 评委四维 1–5：保真、适配、实用、美观。评委与生图模型分开
- 闸门：有金标则工作台均分 >= 金标；必须严格赢过直调；保真不得低于金标（若有）和直调
- live 必须 `PRODUCTFLOW_RUN_IMAGE_EVALS=1`

类目检索词在 `imageeval.CategorySeeds`：3c / appliance / home / womenswear / menswear / beauty / food / baby / sports。

```bash
just image-evals-ingest storage-dev/image-evals/inbox
just image-evals-sample 20 1
PRODUCTFLOW_RUN_IMAGE_EVALS=1 just image-evals-run 8 1
just image-evals-report <run_id>
```

浏览器会话负责打开页面抽 URL。风控/滑块出现即停，不要写爬虫绕过。

## 证据

池：类目计数、合计 SKU、日期。

```text
YYYY-MM-DD | run_id= | 种子= | n= | 类目= | 工作台/金标/直调 | 闸门=过|未过
```
