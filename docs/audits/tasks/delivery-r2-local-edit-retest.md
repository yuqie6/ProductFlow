# 任务：§5.4 局部修图核心路径复验

状态：认领
类型：证据
认领者：sub-r2/delivery-r2-local-edit-retest
认领于：2026-09-07T18:11:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：R2 关闭裁定；≠假标 R2

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

[R2 核心路径门](archive/delivery-r2-core-path-gate.md) 本门 PASS，但 **§5.4 修图与候选** 标为缺口（依赖旧归档未重跑）。导出叠层已修。关闭 R2 前须用当前栈复验局部编辑→候选→采用可达。

## 做成什么样

1. 隔离 mock：对一商品成果图做局部编辑（或既有 local-edit e2e），确认可进候选/结果并可选采用；桌面 1440 与手机 390 至少各一轮可达截图。
2. 对照 §5.4 列已齐/缺口；诚实 FAIL 可接受。
3. 更新父章程；**不得**因本门宣称 R2 全过。

## 前置与并行

- 前置：delivery-r2-core-path-gate、delivery-export-overlay-fix 已归档。
- 隔离 mock；勿 down 共享 productflow。

## 只改这些文件

- 本文件、父章程、必要时窄 e2e 扩展
- 证据目录（gitignore）

## 不要碰

- 假标 R2；额度/OCR/主体提取包。

## 合同

- ROADMAP §5.4；完成可 FAIL。

## 怎么验收

- 命令 + 截图；`just docs-check`。

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
