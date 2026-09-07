# 任务：成果页导出按钮叠层可达修复

状态：认领
类型：实现
认领者：sub-r2/delivery-export-overlay-fix
认领于：2026-09-07T17:48:30+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：R2 关闭裁定（仍须 §5.4 等缺口齐）；≠假标 R2

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

[R2 核心路径门](archive/delivery-r2-core-path-gate.md) 本门 PASS，但成果「导出已采用交付」按钮可被画布右上控件拦截；E2E 只能断言可见后走 API 导出。方向 2 要求桌面/手机核心任务可达——导出入口被叠层挡住是诚实缺口。

## 做成什么样

1. 修复成果视图下导出采用交付控件的点击可达（z-index/布局/pointer-events 等最窄改动）；桌面 1440 与手机 390 均可点到并触发导出（或打开导出流程）。
2. 扩展或复用 `web/e2e/delivery-r2-core-path.spec.ts`：断言真正 UI 点击导出成功（不只 API）。
3. 更新父章程该缺口行；**不得**因本修宣称 R2 全过。

## 前置与并行

- 前置：delivery-r2-core-path-gate 已归档。
- 排他：相关 workbench/results UI 文件、e2e、本任务、父章程；勿改 Graph 额度/OCR 并行包。

## 只改这些文件

- `web/src/pages/workbench/`（最窄）
- `web/e2e/delivery-r2-core-path.spec.ts`（或窄扩展）
- 父章程、本文件

## 不要碰

- 假标 R2；Skill/grader；额度/OCR 包。

## 合同

- ROADMAP §5.5 可达；完成 ≠ R2 关闭。

## 怎么验收

- Playwright UI 点击导出；`just docs-check`。

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
