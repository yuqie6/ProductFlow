# 任务：交付与局部编辑商家隔离（B6）

状态：认领
类型：实现
认领者：sub-merchant/merchant-delivery-localedit
认领于：2026-09-07T13:56:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B7 Agent 工具链；完整隔离门未过不开放第二商家

按 [Issue 协议](README.md) 认领。承接 [图库绑定 B4](archive/merchant-library-binding.md) 与覆盖矩阵 **B6**。

## 问题来源

矩阵 G\*：delivery job/export/adopt 与 localedit task/source 仍可能跨商读写。

## 做成什么样

本商 job/export/adopt 与局部编辑成功；跨商 job/task/source 拒绝（404）。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B4 已归档。
- 排他写入：`go/internal/delivery/`、`go/internal/localedit/` 及相关测试、父章程 B6、本文件。勿与 CF-B2 同写 facts 预览冲突文件。
- 不改 Skill/grader、发行脚本。

## 只改这些文件

认领后补齐。

## 合同

- 矩阵 G\*；跨商 404；完成 ≠ MP-B。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
