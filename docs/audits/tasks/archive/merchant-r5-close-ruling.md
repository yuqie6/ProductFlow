# 任务：总纲 R5 额度与执行关闭裁定

状态：完成
类型：证据
认领者：sub-mpc/merchant-r5-close-ruling
认领于：2026-09-07T18:17:00+08:00
完成于：2026-09-07T18:22:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：缺口项实现；≠支付产品化

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

ROADMAP **R5**：并发预留、重复投递、超时、取消、恢复、未知结果与核对；全部收费入口归属可解释。MP-C B0–B4 已交付账本、图会话/Graph/Agent 主入口与余额 HTTP，但 R5 仍标未通过。需对照条文诚实裁定。

## 做成什么样

1. 逐条对照 R5 / §8 与 B0–B4 归档证据：已齐 / 缺口。
2. 若仅缺可补自动化且合同允许：最窄补测后复跑。
3. 证据齐则更新 ROADMAP R5 为**通过**并写残余；否则保持**未通过**并列阻塞。**不得假标通过**。
4. 更新父章程。

## 前置与并行

- 前置：B0–B4 已归档。
- 只读为主；补测用测试 DB。

## 只改这些文件

- 本文件、父章程、ROADMAP R5、必要时最窄测试

## 不要碰

- 假标 R5；支付；开放第二商。

## 合同

- ROADMAP R5；完成可 FAIL。

## 怎么验收

- 对照表 + 命令；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：证据与活文档已写；**未**假标通过；待维护者审核后归档提交（本执行者不 commit/push）。

## 证据

### 对照表（ROADMAP R5 / §8 ↔ B0–B4）

| 条文 | 证据锚点 | 结论 |
|---|---|---|
| 并发预留 | B0：[merchant-mp-c-quota-b0](merchant-mp-c-quota-b0.md)；`TestConcurrentDoubleReserveCannotExceedBalance` | **已齐**（账本层） |
| 重复投递 / 幂等 | B0：`TestReserveSettleReleaseUnknown` 同键重放；`TestConcurrentIdempotentReserveSameKey`；入口键：imagesession `generationQuotaKey`、graph `imageNodeQuotaKey`、agent model_request | **已齐**（账本 + 已接入口） |
| 超时不自动当零消费 | `quota` 包合同 + MarkUnknown 拒 Release；B1 `TestGenerateUnknownMarksQuotaPending`；B2 `TestImageNodeUnknownMarksQuotaPending`；B3 `TestModelInvocationUnknownMarksQuotaPending` | **部分齐**（已接入口）；未接入口无此保证 |
| 取消 → 释放预留 | B0 Release；B1 `TestGenerateCancelReleasesQuotaBeforeProvider`（未发出） | **部分齐**（账本 + 图会话）；Graph/Agent 取消路径依赖 effect/中断收口，全入口未统一枚举 |
| 恢复 | Agent lease 过期 → MarkUnknown（B3）；imagesession/graph recovery 与 provider 边界 unknown 合同 | **部分齐**（已接入口）；localedit 有 provider unknown 恢复但**无**额度接线 |
| 未知结果与核对 | B0 `pending_reconciliation` + 可从待核对 Settle；拒绝 Release | **账本齐**；**缺口**：无固定到期时限/客服裁定运营策略（§8.2） |
| 全部收费入口归属可解释 | `rg`：`Reserve` 仅 `imagesession/quota_wire.go`、`graph/quota_wire.go`、`agent/quota_wire.go` | **缺口** |
| §8.1 三账分离 | 商业额度 `go/internal/quota`；调用事实仍 `agent_model_invocations` 等；占位单价 1 iu | **骨架齐**；成本估计/真实费率未产品化 |
| §8.2 价格版本展示与检查 | `DefaultPriceVersionID = "pv-placeholder-v0"`；无真实价格目录 | **缺口** |
| §8.2 邀请试用 Op 调账 | B4 Op `Adjust` HTTP + 幂等测试 | **已齐**（≠真实支付） |
| 真实支付 / webhook | 明确不在 B0–B4 / R5 本门必过范围 | **残余非宣称**（不单独构成假标动机） |

### 已接线收费入口（仅此三处）

| 入口 | 归档 | 关键测试 |
|---|---|---|
| 图会话 `imagesession.Service.Generate` | [B1](merchant-mp-c-wire-b1.md) | 不足拒绝 / Settle / Cancel Release / MarkUnknown |
| Graph `Executor.callImageProvider`（`NodeImageGeneration`） | [B2](merchant-mp-c-wire-b2-graph.md) | 不足拒绝 / Settle / MarkUnknown |
| Agent `before_model_request` → `recordModelInvocationStart` | [B3](merchant-mp-c-wire-b3-agent.md) | 不足拒绝 / Settle / MarkUnknown |
| 商家/Op 余额 HTTP | [B4](merchant-mp-c-balance-http-b4.md) | 跨商拒绝 / Adjust 幂等 |

### 阻塞缺口（阻止 R5 通过）

1. **全收费入口未齐**：`localedit`（provider 出图/编辑）、`product` source-note 生成（`POST /api/v2/product-source-notes/generate`）等会调模型的路径 **无** `quota.Reserve`；§8.1 明确要求追踪局部编辑与 source-note，不能只计主生图。
2. **真实价格版本缺失**：仍为 `pv-placeholder-v0` 与固定占位单位；生成前无可核验的展示单价目录。
3. **unknown 到期运营策略未固定**：账本可挂 `pending_reconciliation`，但无运营试用前约定的处理时限与客服裁定流程证据（§8.2）。

本窗**无需补测实现**：缺口为接线/产品合同，非缺一条可补自动化即可翻盘；未改代码。

### 复跑命令（2026-09-07T18:17:10+08:00）

| 命令 | 结果 |
|---|---|
| `bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./internal/quota/ ./internal/imagesession/ ./internal/graph/ ./internal/agent/ -run "Quota\|Concurrent\|Idempotent\|Unknown\|Reserve\|Insufficient" -count=1 -p 1 -timeout 20m'` | **PASS**（4 包 ok） |
| `just docs-check` | **PASS**（Documentation contract check passed） |

### 总纲裁定

**总纲 R5：未通过。**

依据：R5 要求「全部收费入口归属可解释」与 §8.2 价格版本/unknown 到期策略；B0–B4 仅覆盖账本 + 三主入口 + 余额面，**不足以**关闭本门。不得假标通过。

### 残余非宣称

1. **≠真实支付**（邀请试用可用 Op 调账验证消耗）。
2. **≠** 开放第二互不信任商产品上线（`CreateMerchant` 仍 409）。
3. **≠ MP-D** 完整运营产品化。
4. 不宣称满载公平调度 / 混合经营 SLA。

### 审核

- 审核者：主代理自审（2026-09-07）；`rg` 确认生产 `Reserve` 仅三入口；对照表与复跑可采信；**总纲 R5 未通过**。
- 交付定位：随本任务提交（维护者归档时用 `git log --follow` 查询）
- Issue 结果 / 业务门槛结果 / 剩余缺口：证据齐可关 issue；**业务门槛 R5 = FAIL**；剩余缺口见上三条阻塞项。
