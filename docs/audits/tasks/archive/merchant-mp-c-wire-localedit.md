# 任务：MP-C 局部编辑入口额度接线

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-wire-localedit
认领于：2026-09-07T18:23:00+08:00
完成于：2026-09-07T18:30:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：source-note 接线；≠R5 全过 / ≠支付

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

[R5 关闭裁定](merchant-r5-close-ruling.md)：**未通过**。阻塞项之一是 localedit 调 provider 出图/编辑路径 **无** `quota.Reserve`。§8.1 要求局部编辑可追踪费用来源。

## 做成什么样

1. 在 localedit **发出 provider 调用前**按商家 `merchant_id` 原子 `Reserve`；幂等键与该次编辑作业 identity 固定。
2. 完成 → `Settle`；明确取消/未发出 → `Release`；已发出结果不明 → `MarkUnknown`（禁止超时当零消费 Release）。
3. 额度不足返回可解释冲突（勿静默跳过）；行为与 B0/B1 一致（占位单价/价格版本可先沿用 `DefaultPriceVersionID`）。
4. 自动化：不足拒绝；成功留下 hold/event；至少覆盖 cancel 或 unknown 其一。
5. 更新父章程写明「localedit 已接」；**≠宣称 R5 通过**。

## 前置与并行

- 前置：B0–B4、R5 裁定已归档。
- 排他：`go/internal/localedit/**`、必要时最小 quota 接线辅助、本任务、父章程。
- 勿改：Brand 表、product source-note、R2 e2e、评委、支付。

## 只改这些文件

- `go/internal/localedit/`（及最小接线测试）
- `docs/audits/merchant-platform.md`
- 本文件

## 不要碰

- 假标 R5；真实价格目录；CreateMerchant；Skill/grader。

## 现在代码在哪

- 账本：`go/internal/quota`；既有接线范式：`imagesession/quota_wire.go`、`graph/quota_wire.go`、`agent/quota_wire.go`。
- 局部编辑：`go/internal/localedit/execute.go` / `service.go`（provider 调用点）。

## 合同

- ROADMAP §8.1 / R5 子集；完成 ≠ 全入口 ≠ R5 关闭。

## 怎么验收

- 包测覆盖不足拒绝 + Settle +（Release 或 MarkUnknown）；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - `2026-09-07`（执行者 `sub-mpc/merchant-mp-c-wire-localedit`）：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/localedit/ -count=1 -p 1 -timeout 600s'` → **PASS**。
  - 覆盖：`TestLocalEditRejectsInsufficientQuota`（可用额度不足 → failed 可重试、无 hold）；`TestLocalEditSuccessSettlesQuotaHold`（Reserve→Settle）；`TestLocalEditUnknownMarksQuotaPending`（MarkUnknown，拒绝 Release）；`TestLocalEditCancelReleasesQuotaBeforeProvider`（claimed 阶段 Cancel → Release）。
  - `just docs-check` → **PASS**。
  - 选定入口：**`Executor.Execute`**（capability 通过后、`provider.Edit` 前 `Reserve`）；成功 `Settle`；未发出 `Release`；已发出不明 / 恢复 unknown → `MarkUnknown`。幂等键 `local-edit:{taskID}:{attemptID}`；占位单价 `DefaultPriceVersionID`。
- 交付定位：随本任务提交（维护者归档；本执行者**未** commit/push）
- 审核者 / 结论：主代理自审通过（2026-09-07）。包测复跑 PASS；Reserve 在 Edit 前；≠R5 通过。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：实现完成，状态仍 **认领**（待维护者归档）。
  - 业务门槛：MP-C localedit 接线齐；**总纲 R5 仍未通过**（source-note 等入口、真实价格目录、unknown 运营策略）。
  - 剩余缺口：source-note 接线；真实单价；unknown 到期裁定；≠支付。
