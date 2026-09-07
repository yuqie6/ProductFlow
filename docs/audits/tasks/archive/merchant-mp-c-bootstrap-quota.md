# 任务：新商家额度引导与试用量种子

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-bootstrap-quota
认领于：2026-09-07T18:55:00+08:00
完成于：2026-09-07T19:20:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：unknown 到期策略；≠R5 全过 / ≠支付

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

审查 P1：额度硬限制已接主入口，账户默认 **0**；正常运营补额度依赖 Op Adjust，但新商家/验收常卡在「可用额度不足」。方向 1 要求额度贯通可用，不能只剩 SQL 种额度。

## 做成什么样

1. 明确产品合同：新商家首次可用额度来源（邀请试用种子 **或** 强制 Op Adjust 且有可达运营入口/文档）。
2. 实现最小可用路径之一：
   - A：创建/激活商家账户时种子可配置试用量（env/设置，默认可为正试用单位）；或
   - B：若坚持零余额，则保证 Op Adjust 在本地/自托管文档与 `just`/`USER_GUIDE` 可达步骤，并给自动化证明「零余额拒绝 → Adjust → 可生成」。
3. 自动化覆盖上述路径；更新父章程；**≠宣称 R5 通过**；≠真实支付。

## 前置与并行

- 前置：B4 Adjust HTTP、价格目录 B0 已归档。
- 排他：`go/internal/quota` / auth merchant 创建最窄、本任务、父章程；必要时 `docs/USER_GUIDE.md` 一节。
- 勿改：compose 交付字节任务、评委。

## 只改这些文件

- quota/auth/merchant 相关最窄 + 测试
- 父章程；必要时用户指南
- 本文件

## 不要碰

- 假标 R5；开放 CreateMerchant 对外注册。

## 合同

- ROADMAP §8 / R5 可用子集；完成 ≠ R5 关闭。

## 怎么验收

- 包测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已审核归档；**≠R5 / 支付**。

## 证据

- **选择 A**：`QUOTA_TRIAL_UNITS`（config 默认 100；可显式 0）在首次建账种子 available；bootstrap / CreateMerchant / activate → `EnsureAccount`；lazy `ensureAndLockAccount` 同合同；写 `adjust` 事件（幂等键 `quota-trial-seed`）。未开放 CreateMerchant 对外注册；≠R5；≠支付。
- 命令 / 日期 / 结果（2026-09-07）：
  - `go test ./internal/quota ./internal/auth ./internal/platform/config -count=1` → PASS（维护者复跑 PASS）
  - `just docs-check` → PASS（compose 并行阻塞已解除）
- 交付定位：随本任务提交
- 审核者 / 结论：维护者通过（2026-09-07）；选型 A 合理；≠宣称 R5 通过
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - 新商家默认可用试用额度路径已落地；Op Adjust 仍可用作补额。
  - **残余仍开**：unknown 到期运营/客服裁定策略；入口展示单价产品面；≠R5 关闭。
