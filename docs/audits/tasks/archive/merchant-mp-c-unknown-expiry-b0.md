# 任务：额度 unknown 到期运营策略 B0

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-unknown-expiry-b0
认领于：2026-09-07T19:25:00+08:00
完成于：2026-09-07T19:25:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：客服裁定 UI；≠R5 全过 / ≠支付

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

R5 / MP-C 残余：`MarkUnknown` 后 hold 无到期运营策略，超时不能自动当零消费 Release。需最小可执行合同：何时可 Settle/Release/保持 unknown、谁可操作、自动化边界。

## 做成什么样

1. 书面合同：`pending_reconciliation` / unknown hold 的到期与人工裁定规则（时长可配置或文档固定）。
2. 最小实现：到期扫描或 Op 明示裁定入口之一（Prefer 复用 Adjust/Settle/Release 语义，不造第二账本）。
3. 自动化：unknown →（到期或 Op）→ 终态且不重复结算。
4. 更新父章程；**≠宣称 R5 通过**。

## 前置与并行

- 前置：B0–B4、bootstrap 已归档。
- 排他：`go/internal/quota` 最窄、本任务、父章程。
- 勿改：compose、评委、开放 CreateMerchant。

## 只改这些文件

- quota 包 + 测试；必要时 worker/dispatcher 最窄钩子
- 父章程；本文件；必要时 USER_GUIDE 一句

## 不要碰

- 假标 R5；真实支付。

## 合同

- ROADMAP R5 / MP-05；完成 ≠ R5 关闭。

### 运营合同（本任务钉死）

| 规则 | 裁定 |
|---|---|
| MarkUnknown 后 | hold=`pending_reconciliation`；保留 reserved 负债 |
| Release | **禁止**（含超时）；unknown 不得当零消费释放 |
| Op 明示裁定 | `ResolveUnknown` → Settle(`actual_units`∈[0,reserved])；须 `reason`；actor=Operator 会话；0=核查后确认零消费 |
| 到期自动化 | `QUOTA_UNKNOWN_HOLD_TTL`（默认 **72h**，自 `updated_at`=MarkUnknown 起）；`ExpireUnknownHolds` **按预留全额 Settle**；原因固定 `unknown hold TTL expiry; charged reserved amount` |
| 幂等 / 不双结 | Settle 幂等键=hold 键；Op 与到期竞态：已终态跳过；额度不一致 → 409 |
| 谁可操作 | 仅站点 Operator（HTTP）；到期扫描=Sys dispatcher `quota_unknown` |
| 非目标 | 客服 UI；假标 R5；真实支付/退款保证 |

## 怎么验收

- 包测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已审核归档；**≠R5 / 支付**。

## 证据

- 命令 / 日期 / 结果：
  - `go test ./internal/quota/ -count=1` — **PASS**（维护者复跑 PASS）
  - routes contract / dispatcher build — 执行者 PASS
  - `just docs-check` — PASS
- 交付定位：随本任务提交
- 审核者 / 结论：维护者通过（2026-09-07）；TTL 全额 Settle + Op resolve；禁止超时 Release；≠R5
- Issue 结果 / 业务门槛结果 / 剩余缺口：实现完成；**R5 仍未通过**；残余：客服裁定 UI、其它入口单价展示、≠支付。
