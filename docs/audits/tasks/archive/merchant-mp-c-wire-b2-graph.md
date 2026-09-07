# 任务：MP-C B2 Graph 图节点额度接线

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-wire-b2-graph
认领于：2026-09-07T17:42:00+08:00
完成于：2026-09-07T17:54:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：Agent 入口；余额 HTTP；≠R5 全过

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

[B1](merchant-mp-c-wire-b1.md) 已接图会话 `Generate`；**Graph 图节点执行仍未预留**。工作台主路径大量走 Graph，仅接会话不足以覆盖主线收费入口。

## 做成什么样

1. 在 Graph 图节点实际会触发 provider 出图的入队/执行前 `Reserve`（商家 `merchant_id` + 稳定幂等键）。
2. 成功 `Settle`；明确未发出 `Release`；已发出不明 `MarkUnknown`（禁止超时当免费 Release）。
3. 额度不足显式冲突；自动化覆盖不足拒绝 + 成功 hold + cancel 或 unknown 其一。
4. 复用 `go/internal/quota`；单价可继续占位 1 iu。≠接 Agent、支付、HTTP 余额。≠R5 通过。更新父章程。

## 前置与并行

- 前置：B1 已归档 `a0183e53`+。
- 排他：`go/internal/graph`（及相关执行器）最窄文件、本任务、父章程；勿改 imagesession 已交付接线除非必要 bugfix；勿改 R2 e2e。

## 只改这些文件

- Graph 入口与测试
- `docs/audits/merchant-platform.md`
- 本文件

## 不要碰

- 支付；CreateMerchant；Skill/grader；假标 R5。

## 合同

- ROADMAP §8.2 / R5 子集；完成 ≠ 全入口。

## 怎么验收

- 包测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - `2026-09-07`（执行者）：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph/ -count=1 -p 1 -timeout 600s'` → PASS。
  - 覆盖：`TestImageNodeRejectsInsufficientQuota`（可用额度不足 → 图节点失败、无 hold）；`TestImageNodeSuccessSettlesQuotaHold`（Reserve→Settle）；`TestImageNodeUnknownMarksQuotaPending`（MarkUnknown，拒绝 Release）。
  - 选定入口：**Graph `Executor.callImageProvider`（`NodeImageGeneration` 出图前）**；成功 `persistImageArtifact` Settle；取消/恢复按 effect 有无 Release 或 MarkUnknown。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/merchant-mp-c-wire-b2-graph.md` 查询）
- 审核者 / 结论：主代理自审通过（2026-09-07）。复测 `./internal/graph` ImageNode/Quota 与全包 PASS；不足拒绝/Settle/MarkUnknown 齐。≠R5。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：**完成**（B2 Graph 图节点接线）。
  - 业务门槛：MP-C **未全过**；R5 **未通过**。
  - 剩余缺口：Agent 入口；HTTP 余额；全入口归属；真实单价；生产零余额需 Op Adjust。
