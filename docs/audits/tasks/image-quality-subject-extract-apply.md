# 任务：主体提取接入生成路径 Apply

状态：认领
类型：实现
认领者：sub-iq/image-quality-subject-extract-apply
认领于：2026-09-07T18:11:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：背景/阴影/比例；≠R3

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

[主体提取 B0](archive/image-quality-subject-extract-b0.md) 已交付 `ApplySubjectPreserveExtract`，但**生成路径未自动 Apply**；`subject_preserve` 图位仍可不跑提取却带声明合格。

## 做成什么样

1. 在 Graph `image_generation`（或紧邻 persist）当 `produce_route=subject_preserve` 时调用 `ApplySubjectPreserveExtract`（参考图来源按现有入边/绑定）。
2. 失败写入 `route_qualified=false` / 未解决项，不得静默合格。
3. 自动化：preserve 成功留下 subject_extract 元数据；失败 unqualified；generative 不强制提取。
4. ≠像素保真；≠R3。更新父章程。

## 前置与并行

- 前置：subject-extract B0 已归档。
- 排他：`go/internal/graph` 最窄接线、本任务、父章程；勿改余额 HTTP / §5.4 e2e。

## 只改这些文件

- graph 执行/persist 最窄 + 测试
- 父章程、本文件

## 不要碰

- 评委；假标 R3；支付。

## 合同

- IQ-CF-04；完成 ≠ R3。

## 怎么验收

- 包测；`just docs-check`。

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
