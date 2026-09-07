# 任务：受控二维排版（CF-B4）

状态：认领
类型：实现
认领者：sub-compete/compete-facts-controlled-layout
认领于：2026-09-07T14:17:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：CF-B5 品牌/视觉继承；不改 IMG 42/32

按 [Issue 协议](README.md) 认领。承接 [CF-B3](archive/compete-facts-produce-route.md) 与父章程 IQ-CF-06 / CF-B4。

## 问题来源

规格/营销字仍依赖生图重写；无图内层/字体/安全区结构，改字号会重生成杯身。DeliverySpec 只做缩放裁切，不是排版。

## 做成什么样

先完成 IQ-CF-06 选型比较备忘（能力/一致性/许可/维护/归属/非目标），再落地最小可测结构：图片层、文字层、必要形状、字体、字号、颜色、对齐、安全区；浏览器预览与服务端导出同输入一致；接现有资产 lineage。正例：规格字改字号不重生成杯身；同夹具预览与导出一致。反例：排版服务调度 Graph 节点；未经验证 SDK 进主依赖；缺字静默空白当合格。

## 前置与并行

- 前置：CF-B3 已归档（`produce_route` 可用）；维度表见父章程 IQ-CF-06。
- 排他写入：排版 schema/服务、预览/导出一致性夹具、选型备忘、父章程 CF-B4/IQ-CF-06、本文件。勿改 Skill/grader/金标、IMG 42/32、delivery 采用快照核心除非接线 lineage 必需。
- 可与 `merchant-agent-tools` 并行：勿写 `go/internal/agent/`。
- 不调用真实 provider 完成本批。

## 只改这些文件

- 选型备忘与实现路径在认领后按选型结果补齐（预期：新建排版包或明确归属模块、必要 web 预览、父章程、本文件）。根因/选型未定时先只写调查与备忘，确认后更新本清单。
- `docs/audits/image-quality.md`：IQ-CF-06 / CF-B4 状态
- 本文件

## 不要碰

- Skill/grader/金标；IMG live 42/32；第二工作流编辑器；任意图片 PSD 级分层；用 Graph DAG 调度排版服务；未经验证的主依赖 SDK。

## 现在代码在哪

- 合同与维度：[image-quality.md](../image-quality.md) IQ-CF-06 / CF-B4
- 交付缩放裁切：`go/internal/delivery/renderer.go`（**不是**排版）
- 路线标签：`go/internal/graph/produce_route.go`（CF-B3）
- 资产 lineage：现有 media/asset 链；排版状态须落入可追溯产物，不另开第二仓库

## 合同

- IQ-CF-06 / CF-B4；完成 ≠ R3 / ≠ CF-B5 / ≠ 主体提取链。

## 怎么验收

- 选型备忘按六维可回答，并记录选定方案与否决理由
- 正测：规格字改字号→主体位图资产不变、仅排版重出；固定夹具预览 vs 导出差异在备忘上限内
- 反测：缺字不得标合格；禁止把未验证 SDK 写入主依赖
- 命令：选型后补齐包测 / Vitest / 夹具路径；`just docs-check` 若改文档链接

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
