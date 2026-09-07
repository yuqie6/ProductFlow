# 任务：Graph 与配方商家隔离（B3）

状态：开放
类型：实现
认领者：—
认领于：—
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B4 图库绑定或 B5 会话生图（写集不冲突时并行）；完整隔离门未过不开放第二商家

按 [Issue 协议](README.md) 认领。承接 [商品链 B2](archive/merchant-product-chain.md) 与覆盖矩阵 **B3**。

## 问题来源

B2 已隔离商品子链下载/ZIP/workspace。Graph changeset/run/SSE 与 recipe apply 仍可能按裸 workflow/recipe/run id 跨商读写或重放游标。

## 做成什么样

矩阵 C\*、D\*：本商 run/SSE/apply 成功；交叉 workflow/recipe/run 与 SSE 游标拒绝（对外 **404**，与 B1/B2 一致）。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B2 已归档。
- 排他写入：`go/internal/graph/`、`go/internal/recipe/`（apply/SSE/run 路径）及相关测试、父章程 B3、本文件。勿与 CF-B1（graph 产物元数据）同写冲突文件——认领前核占用。
- 不改 Skill/grader、delivery 采用快照、发行脚本。

## 只改这些文件

认领后补齐。

## 合同

- 矩阵 C\*/D\*；跨商 404；完成 ≠ MP-B。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
