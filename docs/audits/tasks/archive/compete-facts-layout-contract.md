# 任务：冻结事实约束与排版分工实施合同

状态：完成
类型：证据
认领者：sub-image/compete-facts-layout-contract
认领于：2026-09-07T12:12:00+08:00
完成于：2026-09-07T12:16:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：按合同发布事实影响预览、主体/生成双路线与受控二维排版实现切片

任务合同以本文件为准，按 [Issue 协议](../README.md) 认领并确认所有权后调查。协调者审核：2026-09-07 CTO 通过——IQ-CF-01…08 与 CF-B0…B5 可执行，交接点 IQ-CF-08 清楚；未改 IMG 42/32。

## 问题来源

[总纲 §6.1–§6.3](../../../ROADMAP.md#61-商品事实约束生产) 要求竞争力投入「准确、可改、可复用」：事实可追溯；摄影生成与精确文字排版分工；改稿影响范围可预览；品牌视觉可复用且清除旧商品身份。当前有 facts/version、incoming-edge、recipe 清除身份，但缺逐入口实施合同与验收样例绑定。

## 做成什么样

在父章程写入可执行合同与批次：事实来源分层、图位文字追溯、规格变更影响预览、主体保留路线 vs 生成式摄影、受控二维排版（层/字体/安全区）的边界与技术选型比较维度、品牌/视觉方案版本继承优先级。每批有正反验收样例（含总纲保温杯流程中可自动化部分）。不在本任务实现代码或选定未经验证的第三方 SDK。

## 前置与并行

- 前置：总纲 §6；现有 `go/internal` facts、recipe payload、providers 边界可只读。
- 冻结输入：认领时 HEAD；不改 IMG 旧 42/32 图位完成条件。
- 运行资源：只读源码与文档；无需 DB/provider。可与商家矩阵、发行差距并行（写入路径不重叠）。
- 排他写入：`docs/audits/image-quality.md` 与本文件；跨组交接条款由协调者同步工作流体验/商家平台。

## 只改这些文件

- `docs/audits/image-quality.md`：合同、批次、验收样例与未知项。
- 本文件。

## 不要碰

业务源码、Skill、grader、金标池、真实 provider 调用、`delivery-adoption-snapshot` 实现。

## 现在代码在哪

商品事实与 version、Graph compile/digest、recipe `payload.go`/`extract.go`、localedit、delivery 规格转换、visual_system 相关路径。现场枚举，禁止只复述总纲。

## 合同

- 事实变更列出依赖图位；已完成且不受影响的图不自动重做。
- 生成式路线不得用提示词宣称像素保真；确定性排版不承担 DAG 调度。
- 品牌继承：商品覆盖 > 视觉方案版本 > 品牌版本 > 默认；事实与身份参考不参与风格链。
- 与工作流「采用快照」交接：质量合同定义何为身份/文字合格；采用/导出 UX 归体验组。

## 怎么验收

`rg`/调用链枚举对照合同条目；每条样例落到批次与验证层。`just docs-check`、`git diff --check`。完成≠图片质量门 R3 通过。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：sub-image/compete-facts-layout-contract（CTO 2026-09-07 派出）。
- 交接：合同与批次已写入父章程；待协调者审核归档。执行者停止，不自行开实现切片、不 push。

## 证据

- HEAD（认领冻结）：`d6709c4aacb2e26bb30ab70a99d08b1dca05f487`
- 枚举：父章程「现场枚举」表（facts 闭集、incoming digest/`skipUnchanged`、recipe 清身份、localedit≠主体保留、DeliverySpec≠二维排版、visual_system 无 Brand 四级链）
- 合同条目：IQ-CF-01 … IQ-CF-08（8 条）；选型比较维度表挂 IQ-CF-06；采用合格判据挂 IQ-CF-08
- 批次：CF-B0 … CF-B5（6 批），各含正反样例；§6.6 保温杯可自动化映射表
- 验证：源码锚点对照通过（facts 闭集、`incomingFactSetVersions`/`skipUnchanged`、recipe 清身份、localedit `masked_edit`、DeliverySpec/`Render`、visual_systems/`mergeImageVisual`）；`just docs-check` → Documentation contract check passed；`git diff --check` 干净
- 自审：未改业务源码/Skill/grader/金标/IMG 42/32；未选定 SDK；未调用真实 provider；写入范围仅两文件；未碰看板 README
- 未知项：Brand 表归属商家平台；主体提取未选型；OCR 是否首版必备；见父章程
- 交付定位：随本任务提交（协调者归档时）

- 审核者：CTO（本会话）；结论：通过，可拆 CF-B0。
