# 任务：工作台生图确认展示单价

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-price-display-workbench
认领于：2026-09-07T19:35:00+08:00
完成于：2026-09-07T19:40:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：其它入口；≠R5 全过 / ≠支付

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

[图会话单价 B0](merchant-mp-c-price-display-b0.md) 已接 ImageChat Generate；R5 残余仍含**工作台生图确认**未展示目录单价。

## 做成什么样

1. 工作台触发 Graph/生图确认（或等价主收费确认面）展示当前价格版本下对应 entry 预估扣减（复用 `quotaPrice` 解析，不硬编码）。
2. 缺失/无效单价：禁用确认并明示，不静默 0。
3. 测覆盖；更新父章程；**≠R5 通过 / ≠支付**。

## 前置与并行

- 前置：price-display-b0 已归档。
- 排他：`web` 工作台确认最窄；可读价格 API；勿改账本。
- 勿与检查器事实修复争写无关模块。

## 只改这些文件

- web 工作台确认面 + 测试；父章程；本文件

## 不要碰

- 假标 R5；法币。

## 合同

- R5 入口可解释费用子集；完成 ≠ R5 关闭。

## 怎么验收

- 包测/组件测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已审核归档；**≠R5 / 支付**。

## 证据

- 命令 / 日期 / 结果：
  - `pnpm --dir web exec vitest run src/lib/quotaPrice.test.ts src/pages/workbench/agent/AgentWorkflowRunRequestCard.test.tsx` — 10 tests passed（维护者复跑 PASS）
  - `tsc --noEmit` / `just docs-check` — PASS
- 交付定位：随本任务提交
- 审核者 / 结论：维护者通过（2026-09-07）；Agent 工作流确认面展示 `graph.image_generation`；≠R5
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - 入口：`AgentWorkflowRunRequestCard`
  - **≠ R5 关闭**；画布直接跑图 / localedit / source-note / Agent model 等仍开；≠支付
