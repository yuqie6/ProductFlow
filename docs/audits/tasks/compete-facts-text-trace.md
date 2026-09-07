# 任务：图位文字追溯到商品事实（CF-B1）

状态：认领
类型：实现
认领者：sub-compete/compete-facts-text-trace
认领于：2026-09-07T13:22:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：CF-B2 变更影响预览；不改 IMG 42/32

按 [Issue 协议](README.md) 认领。承接 [CF-B0](archive/compete-facts-layer-gate.md) 与父章程 IQ-CF-02。

## 问题来源

信息图成稿文字须能追到本商品 fact key 或显式本图覆盖；卖点图默认一图一主要购买理由。

## 做成什么样

prompt/generation 产物记录所用 `fact_keys` 或本图覆盖标记；卖点一图一理由检查器（可先非 live）。正例：规格「600ml」← `capacity`；卖点只强调有依据的一句。反例：成片「24h 保温」无 fact；卖点堆多句无关口号。

## 前置与并行

- 前置：CF-B0 已归档。
- 排他写入：graph/product 产物元数据与相关测试；勿与 `merchant-graph-recipe` / `brand-visual-reuse` 同写 `recipe/`、`visualsystem/`、共享 schema；本批优先 graph 产物元数据与检查器，product store 仅必要最小改动。
- 不改 Skill/grader/金标；无需真实 provider 完成本批。

## 只改这些文件

认领后补齐；预期 graph compile/产物、product 相关、测试、父章程 CF-B1、本文件。

## 合同

- IQ-CF-02；OCR 闸非本批必备（元数据+抽检可先行）。
- 完成 ≠ R3。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
