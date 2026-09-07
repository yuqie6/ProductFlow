# 任务：事实变更影响预览（CF-B2）

状态：开放
类型：实现
认领者：—
认领于：—
业务组：图片质量
父账本：image-quality.md
完成后可拆：CF-B3 双路线（可并行若写集不冲突）；不改 IMG 42/32

按 [Issue 协议](README.md) 认领。承接 [CF-B1](archive/compete-facts-text-trace.md) 与父章程 IQ-CF 变更影响合同。

## 问题来源

改商品事实后缺「哪些图位受影响」预览；用户无法多选更新范围，易导致全图重跑或静默跳过。

## 做成什么样

事实保存前预览依赖图位；用户多选更新；未选中已完成节点保持 artifact；解释旧 `fact_set_version_id`。正例：改 500→600ml 列出规格/卖点图；场景图无容量字默认不入更新集。反例：改容量后全图自动重跑；预览注入未连边节点资料。

## 前置与并行

- 前置：CF-B1 已归档。
- 排他写入：事实影响预览 API/UI、相关 graph digest/`skipUnchanged` 回归、父章程 CF-B2、本文件。勿与 `merchant-graph-recipe` 同写冲突的 graph HTTP/SSE 商家过滤文件——认领前核占用。
- 不改 Skill/grader/金标；无需真实 provider。

## 只改这些文件

认领后补齐。

## 合同

- 父章程 CF-B2；完成 ≠ R3 / ≠ OCR 闸。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
