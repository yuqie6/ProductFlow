# 任务：总纲 R2/R5 状态对照刷新

状态：完成
类型：证据
认领者：sub-cto/roadmap-r2-r5-refresh
认领于：2026-09-07T18:55:00+08:00
完成于：2026-09-07T19:05:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：R2/R5 关闭裁定；≠假标通过

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

`docs/ROADMAP.md` R2/R5 行仍写「修图未重跑 / Brand 占位 / localedit·source-note 未接线」等**已过时**表述，而后续归档已交付 §5.4 复验、Brand B0/B1、localedit/source-note 额度、价格目录骨架。活文档漂移会误导关闭裁定。

## 做成什么样

1. 只读对照当前归档：逐条更新 ROADMAP **R2**、**R5** 行（及必要时 merchant-platform / canvas 交叉一句）为**当前事实**。
2. 诚实列出仍阻塞关闭的缺口（批跑、确定性文字层、真实 provider 修图、unknown 到期策略、入口展示单价等）。
3. **不得**因文档刷新宣称 R2/R5 通过。
4. `just docs-check`。

## 前置与并行

- 只读+活文档；勿改业务代码。
- 可与 compose-deliver / bootstrap-quota 并行（共享 ROADMAP 时由维护者整合）。

## 只改这些文件

- `docs/ROADMAP.md`（R2/R5 行）
- 必要时 `docs/audits/merchant-platform.md` / `canvas-test-system.md` 一行
- 本文件

## 不要碰

- 假标 R2/R5；改 Go/Web 业务。

## 合同

- 活文档与归档一致；完成可仍为未通过。

## 怎么验收

- 对照表；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已审核归档；**未**假标 R2/R5 通过。

## 证据

### 对照表（归档 → ROADMAP 当前格）

| 门 | 归档锚点 | 写入「当前」的事实 | 仍阻塞关闭 |
|---|---|---|---|
| R2 | [delivery-r2-core-path-gate](delivery-r2-core-path-gate.md)；[delivery-export-overlay-fix](delivery-export-overlay-fix.md)；[delivery-r2-local-edit-retest](delivery-r2-local-edit-retest.md)（§5.4 本门 PASS，检查器 mock）；[merchant-brand-entity-b0](merchant-brand-entity-b0.md)；[merchant-brand-product-select-b1](merchant-brand-product-select-b1.md) | 核心路径 PASS；导出叠层已修；§5.4 mock 复验 PASS；Brand B0/B1 已交付 | 批跑；确定性文字层；真实 provider 修图质量；≠完整多品牌 UI |
| R5 | [merchant-r5-close-ruling](merchant-r5-close-ruling.md)（基线 FAIL）；后继 [merchant-mp-c-wire-localedit](merchant-mp-c-wire-localedit.md)；[merchant-mp-c-wire-source-note](merchant-mp-c-wire-source-note.md)；[merchant-mp-c-price-catalog-b0](merchant-mp-c-price-catalog-b0.md) | B0–B4 + localedit/source-note 接线 + 价格目录骨架已交付 | 入口展示单价产品面；unknown 到期运营策略；新商家引导额度（进行中）；≠真实支付 |

### 命令 / 日期 / 结果

- 只读归档对照：`docs/audits/tasks/archive/` 上列任务（2026-09-07）
- `just docs-check`：2026-09-07 → Documentation contract check passed
- **总纲 R2 / R5：仍均为未通过**（本任务仅刷新事实单元格，不关闭门）

### 交付定位

- 随本任务提交
- `docs/ROADMAP.md` §10.1 R2、R5「当前状态」格

### 审核者 / 结论

- 维护者审核通过（2026-09-07）：活文档与归档一致；R2/R5 仍诚实未通过；≠假标通过

### Issue 结果 / 业务门槛结果 / 剩余缺口

- Issue：完成（证据刷新）
- 业务门槛：**R2 = 未通过**；**R5 = 未通过**
- 剩余缺口：见上对照表「仍阻塞关闭」列
