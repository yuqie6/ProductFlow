# 任务：图库绑定商家隔离（B4）

状态：认领
类型：实现
认领者：sub-merchant/merchant-library-binding
认领于：2026-09-07T13:50:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B6 或与 B5 并行（写集不冲突时）；完整隔离门未过不开放第二商家

按 [Issue 协议](README.md) 认领。承接 [Graph/配方 B3](archive/merchant-graph-recipe.md) 与 [会话生图 B5](archive/merchant-image-session.md) 与覆盖矩阵 **B4**。

## 问题来源

矩阵 E\*：from-product/session、workflow sync 等图库绑定仍可能跨商绑定半写入。

## 做成什么样

同商绑定成功；**跨商绑定**拒绝且无半写入（对外 404）。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B3 已归档；与 B5 的依赖按父章程——认领前读批次依赖行。
- 排他写入：library/product 绑定路径及相关测试、父章程 B4、本文件。勿与 CF-B2 同写事实预览冲突文件。

## 只改这些文件

认领后补齐。

## 合同

- 矩阵 E\*；跨商 404；完成 ≠ MP-B。

## 阻塞与交接

- 原因：无（若 B5 硬依赖未满足则改阻塞并写明）。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
