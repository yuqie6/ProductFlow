# 任务：Agent 工具链商家隔离（B7）

状态：认领
类型：实现
认领者：sub-merchant/merchant-agent-tools
认领于：2026-09-07T14:10:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B8 贯穿或 B9/B10；完整隔离门未过不开放第二商家

按 [Issue 协议](README.md) 认领。承接 [交付/局部编辑 B6](archive/merchant-delivery-localedit.md) 与覆盖矩阵 **B7**。

## 问题来源

矩阵 H\*、I\*：浏览器+internal+Pi scope 商家字段与工具越权仍可能跨商读写或伪造 scope。

## 做成什么样

本商 turn/tool 成功；伪造 internal scope、跨商 content、确认在撤销后拒绝（404/统一策略）。25 工具越权 harness 覆盖关键路径。仍不开放第二商；不宣称 MP-B。不改 grader 边界。

## 前置与并行

- 前置：B6 已归档。
- 排他写入：`go/internal/agent/` 及相关测试、父章程 B7、本文件。勿改 Skill/grader 评测题边界。
- 可与 CF-B3 并行若写集不交。

## 只改这些文件

认领后补齐。

## 合同

- 矩阵 H\*/I\*；跨商 404；完成 ≠ MP-B / ≠ B10。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
