# 任务：淘宝过线池与生图闸门 live

状态：开放
类型：证据
认领者：—
认领于：—
业务组：图片质量
父账本：image-quality.md
完成后可拆：合计仍 <200 或每类目不足则发 image-eval-pool-grow.md（合同同本任务）。闸门未过不另发刷分任务

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](README.md)。执行前读取适用仓库规则、当前实现、测试和 diff。ingest/admit/harness 不在本次修改范围。

图片门槛与验收结论在父章程的 [图片质量验收](../image-quality.md#image-quality) 节，维护者关闭时更新 IMG-D-01/IMG-D-10/IMG-C-03 和 live 记录；不把 Agent pass^k 套用于图片闸门。

2026-09-05 组织协调：改归图片质量组，原认领者、任务 ID、采集/验收合同及资源占用不变。原图片账本的未提交增量迁至 `../image-quality.md`，后续只更新该父账本；本次组织提交不代为验收采集结果。

## 前置与并行

- 前置：已登录浏览器会话、真实 provider 凭据；运行前从 pool manifest 清点基线，不沿用旧规模数字。
- 冻结输入：抽样 live 期间固定抽样 manifest、`go/internal/imageeval/`、生图/评委模型和 provider 设置；采集脚本有用户 diff 时先确认归属。
- 运行资源：采集独占对应浏览器会话与 inbox/pool 写入；live 独占共享 provider 设置，不能与画布 mock 切换或共用库的破坏性测试并行。

## 做成什么样

1. 过线池向每类目≥20、合计≥200 个完整套图扩充。当前规模以认领时的 pool manifest 清点为准，并记录本轮增量。
2. 登记一次抽样 n 足够、四维均分齐全的 live `run_id`。闸门过不过都如实写；没有 `run_id` 不得写已通过。

金标来自已登录淘宝详情（主图相册 + 详情模块）。不做拼多多。不用猜你喜欢当抽样框。

## 只改这些文件

- `evals/image/` 抽取脚本（仅当抽取坏了；默认不要改）
- 本文件的池规模与 live 表
- 像素只进 `STORAGE_ROOT/image-evals/`（gitignore，不提交）

## 不要碰

- `go/internal/imageeval` 的准入阈值、闸门公式、naive 提示词（已有测试钉死）
- 工作台检查器、已删除的人工保真表单
- Agent 行为评测代码、题库与标签（Agent 质量组的任务仍各自独占）

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

本轮开始前由维护者与执行者在本 issue 写定采集批次（类目与目标增量）和 live 抽样 n，不能事后降低目标凑完成。本轮约定增量与有效 live 报告齐全即可关闭采证 issue；总池规模或质量门未达标分别留在父章程。缺登录、风控中止或未完成约定采证时转阻塞。若修抽取脚本，必须补充对应提取样本验证证据。

## 证据

池：类目计数、合计 SKU、日期。

```text
YYYY-MM-DD | run_id= | 种子= | n= | 类目= | 工作台/金标/直调 | 闸门=过|未过
```
