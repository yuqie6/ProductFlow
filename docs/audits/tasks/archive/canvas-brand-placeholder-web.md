# 任务：前端 Brand 占位 fallback 对齐 B0

状态：完成
类型：实现
认领者：sub-r2/canvas-brand-placeholder-web
认领于：2026-09-07T18:35:00+08:00
完成于：2026-09-07T18:43:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：多品牌 UI；≠R2 全过

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

商家平台 [Brand B0](merchant-brand-entity-b0.md) 已将继承占位改为 `brand_not_selected` / `brand_exists_no_style_merge`，但 `web/src/pages/workbench/canvas/visualReuse.ts`（及测试）仍硬编码 `brand_table_not_ready`，前端会显示过时语义。

## 做成什么样

1. 对齐 API/契约：本地 fallback 与 Vitest 使用新 reason；若 UI 有用户可见文案，改为简体中文诚实说明（未选定品牌 / 品牌层尚未合并风格等）。
2. 不宣称 Brand 色已合并、不建完整多品牌管理页（除非最小展示必需）。
3. 更新父章程一行；**≠宣称 R2 通过**。

## 前置与并行

- 前置：Brand B0 已归档。
- 排他：`web/src/pages/workbench/canvas/visualReuse.ts`、相关测试、本任务、父章程。
- 勿改：Go Brand/product schema（并行 B1）、quota。

## 只改这些文件

- `web/src/.../visualReuse.ts` 及相关测试
- `docs/audits/canvas-test-system.md`
- 本文件

## 不要碰

- 假标 R2；重做视觉方案 CRUD。

## 合同

- 与 Brand B0 / IQ-CF-07 占位语义一致。

## 怎么验收

- 相关 Vitest；`just docs-check`（若改 web 包规则另加包内检查）。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
  - `2026-09-07T18:36:37+08:00`：`pnpm --dir web exec vitest run src/pages/workbench/canvas/visualReuse.test.ts` → **PASS**（6 tests）
  - 同日：`just docs-check` → Documentation contract check passed
- 交付定位：随本任务提交（执行者未 commit / 未 push；状态保持认领待维护者审核）
- 审核者 / 结论：主代理自审通过（2026-09-07）。Vitest 6 PASS；占位语义对齐 Brand B0；≠R2。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - **已交付**：`emptyReusePreview` / 测试对齐 B0 `brand_not_selected`（及 `brand_exists_no_style_merge` 文案键）；UI 回退文案改为诚实「未选定 / 未合并风格」；父章程一行更新。
  - **残余**：商品选定 Brand、品牌色合并、多品牌 UI、批跑；≠R2 全过。
  - **≠** 宣称 R2 通过。
