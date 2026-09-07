# 任务：subject_preserve 真实 provider 冒烟 k=1

状态：认领
类型：证据
认领者：sub-iq/image-quality-subject-preserve-live-smoke
认领于：2026-09-07T19:35:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：扩大样本；≠R3 全过 / ≠像素保真

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。按 [所有权前置规则](README.md#认领与并行) 取得已确认的认领后，才开始调查或设计。

## 问题来源

compose 交付与声明诚实已归档，但 R2/R3 仍缺**真实 provider** 路径证据。需 k=1 冒烟：`subject_preserve` 在有 identity 参考时优先合成交付（或诚实降级），记录实际字节来源与资格字段。

## 做成什么样

1. 冻结 1 个合法商品/参考输入；真实 provider（声明预算与模型）。
2. 跑通一次图生成；记录 `SubjectPreserveDeliveryFromCompose` / `appearance_may_change` / 资产 SHA / 是否跳过 GenerateImage。
3. 失败也诚实记 FAIL 原因；**不得**假标 R3。
4. 更新父章程；`just docs-check`。

## 前置与并行

- 前置：compose-deliver、honesty 已归档。
- 运行资源：真实 provider 凭据与预算；独占或隔离 DB/worker。
- 排他：本任务、父章程、证据目录；勿改评委/金标。

## 只改这些文件

- 本文件；证据路径；父章程/必要时 ROADMAP R3 一句
- 必要时最窄脚本；勿大改生产路径

## 不要碰

- 假标 R3；改 42/32 图位合同。

## 合同

- IQ-CF-04 真实路径子集；完成 ≠ R3。

## 怎么验收

- 证据表 + 产物；`just docs-check`。

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
