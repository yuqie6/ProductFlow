# 任务：收费入口展示单价 B0

状态：完成
类型：实现
认领者：sub-mpc/merchant-mp-c-price-display-b0
认领于：2026-09-07T19:25:00+08:00
完成于：2026-09-07T19:15:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：法币/支付；其它入口展示；≠R5 全过

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

价格目录 B0 已有 `quota_price_*` 与 Op/商家只读价格 HTTP；R5 仍缺**入口展示单价**产品面——商家在生图/修图前看不到将扣多少内部单位。

## 做成什么样

1. 至少一个主收费入口（优先图会话 Generate 或工作台生图确认）在动作前展示当前价格版本下的单价/预估扣减（读价格目录，不硬编码）。
2. 价格缺失或版本无效时显式失败/禁用，不静默 0。
3. 自动化或组件测覆盖展示数据源；更新父章程；**≠R5 通过 / ≠支付**。

## 前置与并行

- 前置：price-catalog-b0 已归档。
- 排他：`web` 相关最窄 + 必要时价格 GET 投影；勿与 unknown-expiry 争写 `quota` 核心账本（只读价格 API 可）。
- 勿改：账本 Settle/Reserve 语义。

## 只改这些文件

- web 入口最窄；必要时 `quota` 只读 DTO
- 父章程；本文件

## 不要碰

- 假标 R5；法币收银台。

## 合同

- ROADMAP R5 入口可解释费用；完成 ≠ R5 关闭。

## 怎么验收

- 包测或组件测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已审核归档；**≠R5 / 支付**。

## 证据

- 命令 / 日期 / 结果：
  - `pnpm --dir web exec vitest run src/lib/quotaPrice.test.ts src/lib/imageSessionApi.test.ts`（2026-09-07）— 2 files / 7 tests passed（维护者复跑 PASS）
  - `pnpm --dir web exec tsc -p tsconfig.json --noEmit`（2026-09-07）— exit 0
  - `just docs-check`（2026-09-07）— Documentation contract check passed
- 交付定位：随本任务提交
- 审核者 / 结论：维护者通过（2026-09-07）；图会话 Generate 前读目录展示预估；缺失禁用；≠R5
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - 入口：文/图生图 `ImageChatPage` Generate（桌面底栏 + 移动设置抽屉）
  - 数据源：`GET /api/merchants/:merchant_id/quota/price` → `image_session.generate`
  - **≠ R5 关闭**；工作台 / 其它收费入口展示仍开；≠支付
