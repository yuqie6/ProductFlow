# 任务：卖点图保真策略 live k=1 复验

状态：完成
类型：证据
认领者：sub-iq/image-quality-selling-point-live
认领于：2026-09-07T17:17:00+08:00
完成于：2026-09-07T17:36:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：按分数决定下一图种修补；≠R3 / ≠关闭旧 32 图位

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

[卖点保真对齐](image-quality-selling-point-fidelity.md) 已收窄 prompts/拼装（确定性通过），但**未跑 live**。[内容试跑](image-quality-content-pilot.md) 中炸锅 `d229379fd27005e5` selling_point 保真 5→4、闸门是→否。需在当前 HEAD 上对同商品同图种做 k=1 工作台+直调+三方分，核对保真维与闸门是否同向改善。

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
- 跟进者：维护者审核归档
- 交接：证据在 `storage-dev/image-quality-selling-point-live-run/`（gitignore）；已停 Redis/API/worker/dispatcher；PG 容器 `productflow-iq-content-pilot-pg-0907`（含库 `productflow_iq_sp_live_0907`）留待处置。不自行 commit。

## 证据

- 命令 / 日期 / 结果：
  - 2026-09-07：隔离栈（复用 PG 容器 `productflow-iq-content-pilot-pg-0907:15449` 新建库 `productflow_iq_sp_live_0907`；Redis `16550`；API `29450`）。共享 `productflow` PG/Redis/API 未停。
  - HEAD `6b9916d70149cba610f262ac052fd9e7edf27004`（含保真修补）；冻结输入 SHA `7810025622ee2ca99cf7b6bdccd840723fe22ad2bdda59d880fba88d80d35f76`；配置 hash `65e18f0bc2e86fa3d6d67fde26b4de535c338cb1090a364830aa960ee1e9ec20`（与试跑一致）。
  - 池仅炸锅、`image_types` 收窄为 `selling_point`×1；`go run ./cmd/productflow-image-evals run --n 1 --seed 1 --product-inputs …`。
  - run_id `20260907T092416Z-d6b5915c`；槽位：工作台 1 + 直调 1 + 三方 1（≤预算）。闸门 **是**；保真 **4**；均分 4.5（fit/utility 5，aesthetics 4）；vs_naive win、vs_gold tie。
  - 对照试跑 candidate（保真 4、闸门否、均分 4.0）：闸门 **改善**、保真 **平**、均分 +0.5。相对 baseline 保真仍低于 5。
  - 视觉（主代理非盲评）：无多利益底栏、无线框剖视；主文案聚焦「烹饪进度看得见/可视化窗口」；角标有 6.2L 次要信息。产物 `storage-dev/image-quality-selling-point-live-run/evidence/`（gitignore）。
  - 已停 Redis/API/worker/dispatcher；**留** PG 容器与库待维护者处置。
  - 诚实结论：相对试跑 candidate **闸门改善、保真持平**；≠R3；≠全面改善；k=1 不泛化。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/image-quality-selling-point-live.md` 查询）；大图留 `storage-dev/image-quality-selling-point-live-run/`（gitignore）。
- 审核者 / 结论：主代理自审通过（2026-09-07）。对照 comparison-summary + harness report + 工作台 PNG：闸门是、保真 4、无一多利益底栏/线框剖视；诚实「闸门改善、保真持平」成立。≠R3。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：**完成**（采证完整）。
  - 业务门槛：≠R3；卖点保真未回到 baseline 5。
  - 剩余缺口：保真维仍 4；其它图种未复验；旧 32/42 图位合同仍阻塞。
