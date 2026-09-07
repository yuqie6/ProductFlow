# 任务：会话生图商家隔离（B5）

状态：认领
类型：实现
认领者：sub-merchant/merchant-image-session
认领于：2026-09-07T13:45:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：与 B4 并行收束后进 B6；完整隔离门未过不开放第二商家

按 [Issue 协议](README.md) 认领。承接 [Graph/配方 B3](archive/merchant-graph-recipe.md) 与覆盖矩阵 **B5**。

## 问题来源

矩阵 F\*：image session generate、SSE、attach 仍可能跨商 attach/download/retry。

## 做成什么样

本商 generate/SSE/attach 成功；跨商 attach/download/retry 拒绝（404）。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B3 已归档。
- 排他写入：`go/internal/imagesession/` 及相关测试、父章程 B5、本文件。可与 `merchant-library-binding` 并行若写集不交。

## 只改这些文件

认领后补齐。

## 合同

- 矩阵 F\*；跨商 404；完成 ≠ MP-B。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
