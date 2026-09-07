# 任务：商品链商家隔离（B2）

状态：开放
类型：实现
认领者：—
认领于：—
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B3 Graph/配方或 B4 图库绑定（写集不冲突时由协调者并行）；完整隔离门未过不开放第二商家

按 [Issue 协议](README.md) 认领。承接 [根归属 B1](archive/merchant-root-ownership.md) 与覆盖矩阵 **B2**。

## 问题来源

B1 已写根表 `merchant_id` 并过滤根查询。商品子链（事实、图库绑定、封面、直链下载/ZIP、workspace intake、from-recipe）仍可能按裸 UUID 跨商读到或混装导出。

## 做成什么样

矩阵 B\* 路径：本商读写/下载成功；跨商 product/fact/folder/asset id、ZIP 混装拒绝（对外码继续 **404**，与 B1 一致）。仍不开放第二商注册；不宣称 MP-B。

## 前置与并行

- 前置：B1 已归档。
- 排他写入：`go/internal/product/` 商品链（facts/gallery/cover/download/ZIP/intake/from-recipe）及相关测试、父章程 B2、本文件。勿与 CF-B1 同时改同一产物元数据文件——认领前核占用。
- 不改 Skill/grader；不改 delivery 采用快照合同；不改发行脚本。

## 只改这些文件

认领后补齐。

## 合同

- 矩阵 B\*；跨商 404；完成 ≠ MP-B / ≠ B10。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
