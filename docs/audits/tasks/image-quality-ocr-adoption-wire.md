# 任务：采用路径默认消费 OCR 对照结果

状态：认领
类型：实现
认领者：sub-iq/image-quality-ocr-adoption-wire
认领于：2026-09-07T17:51:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：生成路径自动 ApplyOCR；≠R3

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

[OCR B0](archive/image-quality-ocr-trace-b0.md) 已交付 `ApplyImageOCRTrace` / `ApplyOCRGate`，但**采用路径未默认调用**；`CreateAdoption` 仍只看声明式 `text_qualified`。IQ-CF-02/08 要求无依据文字不得进入已采用合格集——缺默认 OCR 消费则声明可绕过成片核对。

## 做成什么样

1. 在 `CreateAdoption`（或紧邻的资格判定）对拟采用资产：若有 `text_trace` 且政策要求文字，则对成片调用 OCR 对照；失败则不得 `pass`/`qualified=true`。
2. 无成片字节/无可对照期望时行为明确（拒绝合格或跳过并记录——选型写证据，不得静默当 pass）。
3. 自动化：声明合格但 OCR missing → 采用拒绝或 unqualified；OCR pass + 声明合格 → 可采用。
4. 不改评委/金标；≠宣称 R3。更新父章程 IQ-CF-02/08。

## 前置与并行

- 前置：OCR B0 `c0b17383`+。
- 排他：`go/internal/delivery` 采用路径、必要时 `graph` 读产物最窄、本任务、父章程；勿改 Graph 额度接线 WIP；勿改导出叠层 UI。

## 只改这些文件

- delivery 采用相关 + 测试
- 父章程、本文件

## 不要碰

- 评委；假标 R3；R2 e2e（除非必要读）。

## 合同

- IQ-CF-02/08；完成 ≠ R3。

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
