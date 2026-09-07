# 任务：MP-C B1 生图/图执行入口预留接线

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-wire-b1
认领于：2026-09-07T17:26:30+08:00
完成于：2026-09-07T17:40:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：Agent 入口接线；Graph 图节点；余额 HTTP；≠R5 全过

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

[MP-C B0](merchant-mp-c-quota-b0.md) 已交付账本 service，但**生图/图执行入口未调用** Reserve/Settle/Release/MarkUnknown。R5 要求收费入口归属可解释；无接线则 B0 不能约束真实消费。

## 做成什么样

1. 选定一条主收费入口（优先：图会话生图或 Graph 图节点执行——以实际会扣 provider 的同步/异步入队点为准），在**发出调用前**按商家 `merchant_id` 原子 `Reserve`；幂等键与该次操作 identity 固定。
2. 完成 → `Settle`（实际消费单位可先等于预留或按固定单价占位）；明确取消/未发出 → `Release`；已发出结果不明 → `MarkUnknown`（禁止超时当零消费 Release）。
3. 额度不足返回可解释冲突（勿静默跳过）；无账户/零余额行为与 B0 一致。
4. 自动化：入口路径在额度不足时拒绝；成功路径留下 hold/event；至少覆盖 cancel 或 unknown 其一。
5. **可不**接 Agent 会话计费、支付、HTTP 余额面。更新父章程。≠宣称 R5 通过。

## 前置与并行

- 前置：B0 已归档 `8728fa7a` 或其后含 `go/internal/quota` 的 HEAD。
- 排他：图执行/imagesession（或选定入口）相关 Go 文件、本任务、父章程；勿改 R2 e2e / 图片 live 证据目录；勿改评委。

## 只改这些文件

- 选定入口包及其测试
- `docs/audits/merchant-platform.md`
- 本文件
- 必要时最小 wiring 辅助（仍在 `go/internal`）

## 不要碰

- 支付；CreateMerchant；Skill/grader；假标 R5；web e2e R2 规格。

## 合同

- ROADMAP §8.2 / R5 子集；完成 ≠ 全入口 ≠ R5 关闭。

## 怎么验收

- 包测 + 相关入口测；`just docs-check`。
- 父章程写明「B1 已接哪条入口 / 未接哪些」。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - `2026-09-07`（执行者）：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession/ -count=1 -p 1 -timeout 180s'` → PASS（39s）。
  - 覆盖：`TestGenerateRejectsInsufficientQuota`（409 可用额度不足、不入队）；`TestGenerateSuccessSettlesQuotaHold`（Reserve→Settle + event）；`TestGenerateCancelReleasesQuotaBeforeProvider`（未发出 Release）；`TestGenerateUnknownMarksQuotaPending`（MarkUnknown，拒绝 Release）。
  - 选定入口：**图会话 `imagesession.Service.Generate`（异步入队点）**；worker `finishSucceeded`/`finishUnknown`/`Cancel`/`errCancelled`/`finishFailed` 终态收口。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/merchant-mp-c-wire-b1.md` 查询）
- 审核者 / 结论：主代理自审通过（2026-09-07）。入口为 `imagesession.Generate`；包测 PASS（含不足拒绝/Settle/Release/MarkUnknown）。≠ R5。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：**完成**（B1 单入口接线）。
  - 业务门槛：MP-C **未全过**；R5 **未通过**。
  - 剩余缺口：Graph 图节点；Agent 入口；HTTP 余额；全入口归属；真实单价/价格版本；生产零余额需 Op Adjust。
