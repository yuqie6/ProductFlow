# 任务：MP-C B3 Agent 入口额度接线

状态：认领
类型：实现
认领者：sub-mpc/merchant-mp-c-wire-b3-agent
认领于：2026-09-07T17:54:30+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：HTTP 余额面；≠R5 全过

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

[B1](archive/merchant-mp-c-wire-b1.md)/[B2](archive/merchant-mp-c-wire-b2-graph.md) 已接图会话与 Graph 出图；**Agent 会话/模型调用入口仍未预留**。Agent 亦消耗 provider，R5 要求收费入口可解释。

## 做成什么样

1. 选定 Agent 实际会触发计费模型调用的入队/执行点（Pi/agent-service 经 Go 侧或 Go agent 包——以现网调用链为准），在发出前 `Reserve`（`merchant_id` + 稳定幂等键）。
2. 成功 `Settle`；未发出 `Release`；已发出不明 `MarkUnknown`。
3. 额度不足显式冲突；自动化覆盖不足拒绝 + 成功 hold + cancel 或 unknown 其一。
4. 复用 `go/internal/quota`；≠支付、≠HTTP 余额面、≠宣称 R5。更新父章程。

## 前置与并行

- 前置：B2 已归档。
- 排他：`go/internal/agent`（及必要 adapter）最窄、本任务、父章程；勿改 delivery OCR 采用接线、导出叠层 UI。

## 只改这些文件

- Agent 入口与测试
- 父章程、本文件

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
- 交付定位：随本任务提交
- 审核者 / 结论：
- Issue 结果 / 业务门槛结果 / 剩余缺口：
