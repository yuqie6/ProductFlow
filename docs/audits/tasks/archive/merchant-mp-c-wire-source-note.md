# 任务：MP-C source-note 生成入口额度接线

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-wire-source-note
认领于：2026-09-07T18:23:00+08:00
完成于：2026-09-07T18:32:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：价格版本；≠R5 全过 / ≠支付

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

[R5 关闭裁定](merchant-r5-close-ruling.md)：**未通过**。§8.1 要求追踪 source-note 生成；`POST /api/v2/product-source-notes/generate` 调模型路径 **无** `quota.Reserve`。

## 做成什么样

1. 在 source-note **发出 prompt provider 调用前**按商家 `merchant_id` 原子 `Reserve`；幂等键与该次请求 identity 固定。
2. 完成 → `Settle`；明确失败/未发出 → `Release`；结果不明 → `MarkUnknown`（禁止超时当零消费 Release）。
3. 额度不足返回可解释冲突；占位单价可沿用 `DefaultPriceVersionID`。
4. 自动化：不足拒绝；成功留下 hold/event；至少覆盖失败 Release 或 unknown 其一。
5. 更新父章程；**≠宣称 R5 通过**。

## 前置与并行

- 前置：B0–B4、R5 裁定已归档。
- 排他：`go/internal/product/` 中 source-note 生成路径及其测试、本任务、父章程。
- 勿改：localedit、Brand 表、R2 e2e、评委、支付。

## 只改这些文件

- `go/internal/product/`（source-note 生成及相关测试；最小 wiring）
- `docs/audits/merchant-platform.md`
- 本文件

## 不要碰

- 假标 R5；CreateMerchant；Skill/grader。

## 现在代码在哪

- HTTP：`product/http.go` `generateSourceNote`；provider：`graph.GenerateSourceNote` / LivePrompt。
- 接线范式：`imagesession/quota_wire.go` 等。

## 合同

- ROADMAP §8.1 / R5 子集；完成 ≠ 全入口 ≠ R5 关闭。

## 怎么验收

- 包测或路由测：不足拒绝 + Settle +（Release 或 MarkUnknown）；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - 2026-09-07T18:29:49+08:00 `bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./internal/product/ -count=1 -timeout 15m'` → **PASS**
  - 覆盖：`TestGenerateSourceNoteRejectsInsufficientQuota`（409 可用额度不足、不调 provider、不新建 hold）；`TestGenerateSourceNoteSuccessSettlesQuotaHold`（Settle + reserve/settle events）；`TestGenerateSourceNoteProviderErrorMarksQuotaUnknown`（pending_reconciliation；禁止再 Release）；`TestSourceNoteQuotaReleaseRestoresBalance`（未发出 Release 接线）
  - `just docs-check` → Documentation contract check passed
- 交付定位：随本任务提交（未 commit / 未 push）
  - `go/internal/product/quota_wire.go`：Reserve/Settle/Release/MarkUnknown；幂等键 `product-source-note:{requestID}`；单价 `DefaultPriceVersionID` / 1 unit
  - `go/internal/product/source_note.go`：`GenerateSourceNoteDraft` 在 provider 前 Reserve；成功 Settle；ctx 已取消未发出 Release；provider 错 MarkUnknown
  - `go/internal/product/quota_wire_test.go` + `source_note_http_test.go`（成功路径补 seed）
  - `docs/audits/merchant-platform.md`：矩阵 B7 / 验收缺口注明 source-note 已接；**≠宣称 R5 通过**
- 审核者 / 结论：主代理自审通过（2026-09-07）。包测复跑 PASS；provider 前 Reserve；≠R5 通过。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - 本入口已接线；**≠ R5 全过**（价格版本、unknown 到期策略等仍开）
  - 缺口：未跑 `just go-test` 全量；Release 的 HTTP 端到端路径仅 helper 测（provider 错误走 MarkUnknown）
