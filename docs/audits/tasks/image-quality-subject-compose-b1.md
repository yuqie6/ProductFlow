# 任务：主体保留背景/阴影/比例合成 B1

状态：认领
类型：实现
认领者：sub-iq/image-quality-subject-compose-b1
认领于：2026-09-07T18:15:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：复杂场景质检；≠R3 / ≠像素保真

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

[主体提取 Apply](archive/image-quality-subject-extract-apply.md) 已自动跑蒙版/抠图，但 IQ-CF-04 仍缺**背景/阴影/位置比例**合成；当前仅闸门元数据，未形成可交付合成图。

## 做成什么样

1. 最小合成：cutout + 可控背景（纯色/简单渐变即可）+ 可选接触阴影占位 + 画布内位置/比例（安全区）；输出 PNG + lineage。
2. 与 `subject_extract` 元数据衔接；失败→未解决/`route_qualified=false`。
3. 自动化：成功产物可核验尺寸/透明主体；失败路径不合格。
4. ≠像素保真；≠R3。更新父章程 IQ-CF-04。

## 前置与并行

- 前置：subject-extract-apply 已归档。
- 排他：`subjectextract`/`graph`/`layout` 最窄、本任务、父章程；勿改余额 HTTP / §5.4 e2e。

## 只改这些文件

- 合成实现与测试
- 父章程、本文件

## 不要碰

- 评委；假标 R3。

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
