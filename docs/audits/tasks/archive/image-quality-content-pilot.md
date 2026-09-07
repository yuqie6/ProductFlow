# 任务：相同商品输入下验证生成内容策略候选

状态：完成
类型：证据
认领者：sub-iq/image-quality-content-pilot
认领于：2026-09-07T16:14:58+08:00
完成于：2026-09-07T17:08:25+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：按真实差距决定下一项生成改进

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

用户委托本会话负责整个图片质量组。[生成内容候选](image-quality-content-candidate.md) 已针对实际诊断实现改动，但需要新生成图片证明效果。此前固定 8 SKU 的费用与执行合同不包含自动追加批次，新的真实调用仍须遵守用户 AGENTS.md 的费用授权边界。

2026-09-07 CTO goal 明确授权可使用真实供应商；本批次按下方已写明的调用上限解除费用阻塞，运行前仍须隔离栈与配置 hash，不改共享 provider。

## 做成什么样

固定空气炸锅 `d229379fd27005e5` 与家纺 `6d69aa54c5a7794d`，每个 hero/selling_point/scene/detail 各一张。原版与候选分别运行工作台、一句直调、商家原图三方对照；原版和候选复用同一份审核后的 AI 表单输入。按原四维、原 gate 记录，同时逐图及整套比较真实内容差异。

## 前置与并行

- 原版：生成内容候选交付的父提交；候选：该交付提交。冻结 checkout 的生产代码差异只允许已审核的生成内容策略。
- source_note：每个商品调用现有生成接口一次，经事实核查后冻结同一文件，两个工作台共用。不得把金标专有功效或内部结构补进商品事实。生成失败不自动重试。
- 参考与金标：`storage-dev/image-quality-content-pilot-0906/` 下 baseline/candidate 两臂，来自上轮校正池的原始字节，不更换图片。准备合同 hash `9a923d536accdf8405a93849b451ed936ae24ab4cd47e50623f168ca112b9e76`。
- 运行前另行登记独立 PostgreSQL/Redis/API/worker/dispatcher 地址及配置 hash；不使用或停止共享服务，不改共享 provider。
- 新批次上限：16 张工作台图、16 张直调图，共 32 张生成图；16 次三方评审；2 次商品 AI 表单、24 次创作要求/风格/画面方案调用。k=1，unknown 占用调用额度，失败不自动补跑。实际供应商计费须按当时绑定与用量核实。
- 与讨论草稿的 24 张预算不同：当前 harness 每臂独立生成直调，共计 16 张直调，未引入跨臂复用直调输出机制。两臂直调抽样噪声必须披露；原版与候选的改进判读须直接比较其工作台输出，不能仅比较各自对直调胜率。

## 只改这些文件

- 本任务、看板及组内证据摘要；本地运行脚本、冻结输入、输出和对照页留在隔离产物目录。
- 不改候选代码、评委、直调提示词、金标或 gate。

## 合同与验证

- 两臂全部 16 个工作台图位以及各自对应直调完成，并有 16 次原合同评分，才形成完整试验。缺失单列，不把运行失败计为质量零分。
- 首先检查商品身份、配件数量、可见结构和文字事实。保真退步单列，不能被美观均分抵消。
- 检查卖点是否有一个清晰理由和可见证据，策划标签是否进入成片；场景/细节是否提供主图没有的信息。匿名化 A/B 版面后进行主代理逐图视觉复核，标明非独立人工标注。
- 保留输入文本、源文件与输出 SHA、代码/配置身份、完整四维评分、图组对照、费用用量和 unknown；不把两商品 k=1 写成泛化结论或超过行业优秀图片。
- 本任务不关闭原 32 图位诊断、不证明耳机数量问题修复、不验证多系列图的绑定与单图任务数组设计。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核；执行者已停隔离 API/worker/dispatcher/Redis，保留 PG 容器 `productflow-iq-content-pilot-pg-0907` 与产物目录待验收后处置。
- 交接：已归档；产物留在 `storage-dev/image-quality-content-pilot-run/`（gitignore）；PG 容器 `productflow-iq-content-pilot-pg-0907` 可由协调者择机停。跟进见 [image-quality-selling-point-fidelity](../image-quality-selling-point-fidelity.md)。

## 证据

### 协调者复验

| 检查 | 结果 |
|---|---|
| `comparison-summary.json` run_id / 16 slots / settings_unchanged | 一致 |
| 闸门 2/8 + 炸锅 selling_point 保真 5→4 | 与任务叙述一致 |
| 产物在 `storage-dev/`（不入库） | 确认 |



- 试验完整：两臂各 8 槽（共 16 工作台 + 16 直调 + 16 三方评分）均有四维分；**无缺失槽、无 unknown 占位当分**。不把闸门未过写成质量零分。
- run_id：baseline `20260907T082558Z-673aeeb0`（commit `1f0cf7b5ecd1964cfdf7dd3b3c4c9366a46cbb49`）；candidate `20260907T084359Z-673aeeb0`（commit `61426ed6a81e2dd4b19971088132b6229d4997f4`）。
- 准备合同 hash：`9a923d536accdf8405a93849b451ed936ae24ab4cd47e50623f168ca112b9e76`（未改）。
- 隔离栈：PG `productflow-iq-content-pilot-pg-0907` `127.0.0.1:15449` / DB `productflow_iq_content_pilot_0907`；Redis 曾用 `127.0.0.1:16549`（已停）；API 曾用 `http://127.0.0.1:29449`（已停）；`STORAGE_ROOT=storage-dev/image-quality-content-pilot-run/shared-pool`。
- 配置 hash 前后均为 `65e18f0bc2e86fa3d6d67fde26b4de535c338cb1090a364830aa960ee1e9ec20`（prompt=gpt-5.5 / image=openai_responses+gpt-5.6-luna / host=sub.devbin.de / has_key=true）；只读复制入隔离库，未改共享 provider。
- 冻结输入 SHA-256 `7810025622ee2ca99cf7b6bdccd840723fe22ad2bdda59d880fba88d80d35f76`（两臂共用；事实核查见 `evidence/product-inputs-review.json`）。
- 闸门：baseline 2/8、candidate 2/8（槽位不同）。工作台均分相对 baseline：+4 / 平 2 / −2；保真维：+2 / 平 5 / −1（炸锅卖点 5→4 单列，不被美观均分抵消）。
- 逐槽工作台均分（baseline→candidate）：炸锅 hero 4.25→5.0（闸门否→是）、selling_point 5.0→4.0（是→否）、scene 5.0→5.0（是→否，平直调）、detail 3.25→4.25（否→否）；家纺 hero 4.5→4.5、selling_point 4.0→4.5、scene 4.5→4.0、detail 4.25→5.0（否→是）。
- 直调披露：两臂各自独立采样直调（各 8 张）；改进判读以工作台 A/B 为主，不单用对直调胜率。完整对照 `storage-dev/image-quality-content-pilot-run/evidence/comparison-summary.json`。
- 主代理视觉复核（非独立人工标注）：炸锅候选卖点更聚焦「360°热风+海星底盘」并有结构示意，未再见「卖点1/卖点2」策划标签入片；与评委均分退步并存，记为非盲评委与内容策略观感分歧。家纺卖点仍多条底栏，仅部分聚焦。详见 `evidence/visual-observations.json`。
- 费用用量（harness 槽位，非网关账单明细）：工作台图 16、直调图 16、三方评审 16、AI 表单 2；创作要求/风格/画面方案调用按工作台图位发生（合同上限 24）。实际计费须按绑定与供应商用量另行核对。
- 产物根：`storage-dev/image-quality-content-pilot-run/`（scripts、product-inputs、evidence/{baseline,candidate,visual,comparison-summary.json}）。
- 非结论：两商品 k=1 不泛化；不关闭原 32 图位诊断；不证明耳机数量修复；不验证多系列作用域。
- 审核者：CTO（本会话）；结论：证据任务**完成**（完整 16/16）；业务门槛 **未证明全面改善**（闸门仍 2/8；卖点保真退步单列）。≠R3；不关闭旧 32 图位合同。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/image-quality-content-pilot.md` 查询）
- Issue 结果 / 业务门槛结果 / 剩余缺口：完成采证；候选相对原版有升有降；下一步针对炸锅卖点保真退步与一理由策略冲突。
