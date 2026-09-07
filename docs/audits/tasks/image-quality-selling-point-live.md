# 任务：卖点图保真策略 live k=1 复验

状态：认领
类型：证据
认领者：sub-iq/image-quality-selling-point-live
认领于：2026-09-07T17:17:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：按分数决定下一图种修补；≠R3 / ≠关闭旧 32 图位

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

[卖点保真对齐](archive/image-quality-selling-point-fidelity.md) 已收窄 prompts/拼装（确定性通过），但**未跑 live**。[内容试跑](archive/image-quality-content-pilot.md) 中炸锅 `d229379fd27005e5` selling_point 保真 5→4、闸门是→否。需在当前 HEAD 上对同商品同图种做 k=1 工作台+直调+三方分，核对保真维与闸门是否同向改善。

## 做成什么样

1. 隔离栈；冻结输入复用试跑 `product-inputs` SHA（若仍有效）或重新事实核查同一商品；配置 hash 登记。
2. 仅跑空气炸锅 **selling_point**（工作台 1 + 直调 1 + 三方评审 1）；费用上限写死：≤3 张图级调用 + 1 次评审相关（按 harness 实际计槽）。
3. 对比试跑 candidate 分数：记录保真、均分、闸门、视觉是否仍多利益底栏/线框剖视。
4. 诚实结论：改善 / 平 / 退步；**不得**写成 R3 或全面改善。更新父章程一句。

## 前置与并行

- 前置：selling-point-fidelity 已归档入库。
- 隔离 PG/Redis/API；禁止共享 `productflow` down；不改评委/金标。

## 只改这些文件

- 本文件、父章程一句
- 隔离产物目录（storage-dev，不入库大图）

## 不要碰

- 评委/金标/gate；候选 prompts（本窗只采证）；Skill grader。

## 合同

- 证据完整可 FAIL；k=1 不泛化。

## 怎么验收

- run_id、配置 hash、分数表、与试跑对照；`just docs-check`。

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
