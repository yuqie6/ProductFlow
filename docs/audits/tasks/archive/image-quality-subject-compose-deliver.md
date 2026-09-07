# 任务：subject_preserve 合成 PNG 作为交付字节

状态：完成
类型：实现
认领者：sub-iq/image-quality-subject-compose-deliver
认领于：2026-09-07T18:55:00+08:00
完成于：2026-09-07T19:10:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：复杂场景质检；≠R3 / ≠像素保真

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

[声明诚实](image-quality-subject-preserve-honesty.md) 已禁止在生成式出图时宣称 `appearance_may_change=false`，但交付资产仍是 `GenerateImage` 字节；compose 仅写元数据。要让保留主体路线名副其实，须在 extract+compose **通过**时把合成 PNG 作为持久化交付字节，并置 `SubjectPreserveDeliveryFromCompose`。

## 做成什么样

1. `subject_preserve` 且参考 extract+compose **Pass**：`persistImageArtifact`（或紧邻）写入 **compose PNG**（非生成式成片）；produce_route 置 `SubjectPreserveDeliveryFromCompose` 语义（`appearance_may_change=false` 仅在此路径允许）。
2. compose/extract **失败**：保持现有 `route_qualified=false`；交付字节策略写清（可仍存生成式图但不得合格采用，或拒绝持久化——选型写证据，不得静默合格）。
3. lineage：compose 与 cutout/source SHA 可追溯（复用已有 ComposeLineage）。
4. 自动化：成功路径产物 MIME/尺寸/SHA 对得上 compose；失败不得声明外观不变；`just docs-check`。
5. 更新父章程；**≠宣称 R3 / 像素保真**。

## 前置与并行

- 前置：compose B1、honesty 已归档。
- 排他：`go/internal/graph` execute/subject_extract/persist 最窄、`subjectextract` 若需、本任务、父章程。
- 勿改：OCR、quota、评委、R2 e2e。

## 只改这些文件

- graph 执行/持久化/测试；必要时 subjectextract
- `docs/audits/image-quality.md`、本文件

## 不要碰

- 假标 R3；假标像素保真。

## 合同

- IQ-CF-04；完成 ≠ R3。

## 怎么验收

- 包测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已审核归档；**≠R3 / 像素保真**。

## 证据

- 设计选型（compose-first）：`resolveSubjectPreserveImageDelivery` 在 `GenerateImage` 前跑 extract+compose；**Pass** 时 `callLocalSubjectCompose` 走 provider 围栏但不 Reserve 额度、不调 `GenerateImage`，`persistImageArtifact` 写入 compose PNG，并重建 produce_route 置 `SubjectPreserveDeliveryFromCompose=true`（`appearance_may_change=false`）。**失败**策略：仍调用 `GenerateImage` 持久化生成式字节（不合格采用），不置交付标志，`appearance_may_change` 保持 true。lineage 仍在 `subject_compose`（compose/cutout/source SHA）。
- 命令 / 日期 / 结果（2026-09-07）：
  - `go test ./internal/graph/ -run "ProduceRoute|SubjectPreserve|GateImage|ApplySubject|ResolveSubject"` → PASS（维护者复跑 PASS）
  - `go test ./internal/graph/ ./internal/subjectextract/ -count=1` → PASS（执行者）
  - `just docs-check` → PASS
- 交付定位：随本任务提交
- 审核者 / 结论：维护者通过（2026-09-07）；compose-first 诚实且跳过额度；≠R3
- Issue 结果 / 业务门槛结果 / 剩余缺口：实现完成；IQ-CF-04 仍部分完成（复杂场景质检未建）；≠R3 / ≠像素保真。
