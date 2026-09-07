# 任务：双路线声明（CF-B3）

状态：认领
类型：实现
认领者：sub-compete/compete-facts-produce-route
认领于：2026-09-07T14:05:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：CF-B4 受控排版；不改 IMG 42/32

按 [Issue 协议](README.md) 认领。承接 [CF-B2](archive/compete-facts-impact-preview.md) 与父章程 IQ-CF-05 / CF-B3。

## 问题来源

主图保留主体与场景生成式未在图位/运行记录上声明；生成式提示词可能宣称像素保真；保留主体失败仍可能标合格。

## 做成什么样

图位/运行记录 `produce_route=subject_preserve|generative`；生成式禁像素保真文案；保留主体路线失败→未解决项。正例：主图 `subject_preserve`；场景 `generative` 且 UI 显示「可能改变外观」。反例：生成式含「像素级一致」；保留主体失败仍标已交付合格。

## 前置与并行

- 前置：CF-B2 已归档（合同允许与 CF-B1 并行，现串行于 B2 后）。
- 排他写入：路线枚举持久化、文案审计测试、相关 UI、父章程 CF-B3、本文件。勿与 `merchant-delivery-localedit` 同写 delivery 采用合格集除非必要。
- 不改 Skill/grader/金标；无需真实 provider。

## 只改这些文件

认领后补齐。

## 合同

- IQ-CF-05 / CF-B3；完成 ≠ R3 / ≠ CF-B4。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
