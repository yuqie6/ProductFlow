# 任务：品牌/视觉继承（CF-B5）

状态：认领
类型：实现
认领者：sub-compete/compete-facts-brand-inherit
认领于：2026-09-07T14:28:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：无强制；OCR/采用硬闸/主体提取另发；不改 IMG 42/32

按 [Issue 协议](README.md) 认领。承接 [CF-B4](archive/compete-facts-controlled-layout.md)、体验组 [brand-visual-reuse](archive/brand-visual-reuse.md) 与父章程 IQ-CF-07 / CF-B5。

## 问题来源

四级继承（商品覆盖 > 视觉方案版本 > 品牌版本 > 产品默认）未钉解析权威；品牌/方案更新可能静默改在做任务；第二商品可能带回旧身份。体验组已交付选择/预览 UX（Brand 占位）；本组需钉解析单测、配方清身份回归与第二商品样例。

## 做成什么样

四级优先级解析单测；实例存所选版本 id；更新需显式采用新版本（不得静默改在做任务与旧交付）；配方 extract/apply 继续清身份；第二商品样例：复用方案但 facts/参考为新商品。正例：商品 overlay 色盖方案色盖品牌色盖默认；新杯复用方案但容量为新 fact。反例：事实/参考进入风格链；品牌更新静默改旧交付；配方带回 `source_product_id`。

## 前置与并行

- 前置：CF-B0 已归档；CF-B4 已归档；[brand-visual-reuse](archive/brand-visual-reuse.md) 已归档（UX/占位可复用）。
- Brand 表未建：保持显式占位合同（`brand_table_not_ready`），不伪造 Brand CRUD。
- 排他写入：继承解析权威、相关 visualsystem/recipe 回归、父章程 IQ-CF-07/CF-B5、本文件。勿与 `merchant-agent-tools` 同写 `go/internal/agent/` / `agent-service/`。
- 不改 Skill/grader/金标；无需真实 provider。

## 只改这些文件

- 认领后按现场补齐（预期：`go/internal/visualsystem/` 解析权威与单测、`go/internal/recipe/` 清身份回归、必要 product/graph 接线、父章程、本文件）。确认根因后更新本清单。
- `docs/audits/image-quality.md`：IQ-CF-07 / CF-B5 状态
- 本文件

## 不要碰

- Skill/grader/金标；IMG 42/32；新建 Brand 表冒充商家平台完成；跨商家分享；`go/internal/agent/`、`agent-service/`（并行 B7）；重做体验组已交付的 CRUD UI 除非接线必需。

## 现在代码在哪

- 合同：[image-quality.md](../image-quality.md) IQ-CF-07 / CF-B5
- 体验组预览/选择：[brand-visual-reuse](archive/brand-visual-reuse.md)；`go/internal/visualsystem/`、`product_visual_selections`
- 配方清身份：`go/internal/recipe/payload.go`
- 节点 overlay：`mergeImageVisual` / `visual_overlay`

## 合同

- IQ-CF-07 / CF-B5；Brand 占位诚实；完成 ≠ R3 / ≠ Brand 表建成 / ≠ 跨商分享。

## 怎么验收

- 解析单测：四级优先级正反夹具
- 配方 extract/apply：不带回 `source_product_id` / fact/visual 版本绑定身份
- 更新：追加品牌/方案版本不静默改已选实例（显式采用）
- 第二商品样例清单或夹具：新容量/参考，复用方案风格
- 命令：认领后钉 `go test` 包路径；`just docs-check` 若改文档链接

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
- 基线 commit / run_id / artifact（适用时）：
- 交付定位：随本任务提交
- 审核者 / 结论：
- Issue 结果 / 业务门槛结果 / 剩余缺口：
