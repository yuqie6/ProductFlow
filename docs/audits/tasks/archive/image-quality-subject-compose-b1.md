# 任务：主体保留背景/阴影/比例合成 B1

状态：完成
类型：实现
认领者：sub-iq/image-quality-subject-compose-b1
认领于：2026-09-07T18:15:00+08:00
完成于：2026-09-07T18:22:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：复杂场景质检；≠R3 / ≠像素保真

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

[主体提取 Apply](image-quality-subject-extract-apply.md) 已自动跑蒙版/抠图，但 IQ-CF-04 仍缺**背景/阴影/位置比例**合成；当前仅闸门元数据，未形成可交付合成图。

## 做成什么样

1. 最小合成：cutout + 可控背景（纯色/简单渐变即可）+ 可选接触阴影占位 + 画布内位置/比例（安全区）；输出 PNG + lineage。
2. 与 `subject_extract` 元数据衔接；失败→未解决/`route_qualified=false`。
3. 自动化：成功产物可核验尺寸/透明主体；失败路径不合格。
4. ≠像素保真；≠R3。更新父章程 IQ-CF-04。

## 前置与并行

- 前置：subject-extract-apply 已归档。
- 排他：`subjectextract`/`graph`/`layout` 最窄、本任务、父章程；勿改余额 HTTP / §5.4 e2e。

## 只改这些文件

- 合成实现与测试
- 父章程、本文件

## 不要碰

- 评委；假标 R3。

## 合同

- IQ-CF-04；完成 ≠ R3。

## 怎么验收

- 包测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：实现与验证已就绪，等待协调者审核；**未 commit / 未 push**。

## 证据

- 实现：`subjectextract.ComposeFromExtract`（纯色/`gradient_v` 背景 + 可选接触阴影占位 + 安全区等比居中）→ PNG + `ComposeLineage`；`ApplySubjectComposeGate` 写 `produce_route.subject_compose`；提取失败跳过合格合成并强制不合格；`graph.ApplySubjectPreserveExtract` / `gateImageProduceRouteWithSubjectExtract` 在提取通过后自动合成。
- 正测：`TestComposeSuccessDimensionsAndOpaqueSubject`（尺寸 400×400、抠图透明底、合成后安全区内有色主体、角落近背景色、`subject_compose.png_sha256`）；`TestComposeGradientBackground`；graph `TestApplySubjectPreserveExtractSuccessMetadata` / `TestGateImageProduceRouteSubjectPreserveSuccessMetadata` 断言 `subject_compose.pass`。
- 反测：`TestComposeFailureWhenExtractFails`（纯色提取失败→合成失败→`route_qualified=false` + unresolved）；`TestComposeFailureInvalidSpec`；`TestComposeGateSkipsGenerative`。
- 命令 / 日期 / 结果（2026-09-07）：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/subjectextract/ ./internal/graph/ -count=1 -p 1 -run "Extract|SubjectPreserve|ApplySubject|GateImage|Compose|ProduceRoute|DeliveryQual"'` → PASS
  - `just docs-check` → Documentation contract check passed
- 交付定位：随本任务提交
- 审核者 / 结论：主代理自审通过（2026-09-07）。包测复跑 PASS；实现范围合合同；≠R3 / ≠像素保真。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：实现与包测完成，待维护者审核关闭。
  - 业务门槛：IQ-CF-04 仍 **部分完成**；R3 **未通过**。
  - 剩余缺口：复杂场景质检；合成 PNG 进入资产/交付链的持久化接线可另发。
