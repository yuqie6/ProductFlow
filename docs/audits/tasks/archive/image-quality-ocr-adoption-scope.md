# 任务：收窄采用路径 OCR 硬闸（残余墨迹不得拦商品图）

状态：完成
类型：实现
认领者：主代理-cto-ocr-gate-scope
认领于：2026-09-07T18:36:00+08:00
完成于：2026-09-07T18:45:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：subject_preserve 资格诚实；≠R3

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

只读审查：采用路径用 Liberation Sans 模板 OCR，并用残余深色像素判「多余文字」。商品本体/阴影会留下墨迹，**正确文案的商品图也可能被拒绝**。硬闸已进 `CreateAdoption`，不能仅用「≠R3」解释。

## 做成什么样

1. 采用路径 OCR：以「期望文字是否出现」为硬条件；**不得**因未声明残余墨迹拒绝正常商品成片。
2. 字形包测可保留残余墨迹失败（合成白底场景）；采用接线改用不把 residual 当硬失败的模式。
3. 成片解码尽量接受常见光栅格式（至少 JPEG+PNG），避免非 PNG 直接不可核。
4. 自动化：含主体色块+期望字的成片可采用；缺期望字仍拒绝；`just docs-check`。
5. 更新父章程；**≠宣称 R3**。

## 前置与并行

- 排他：`go/internal/ocr`、`go/internal/graph` OCR 接线、`go/internal/delivery` 采用 OCR、本任务、父章程。
- 勿改：并行 Brand/价格目录任务文件范围；评委。

## 只改这些文件

- ocr / graph ocr_trace / delivery adoption OCR + 测试
- `docs/audits/image-quality.md`、本文件

## 不要碰

- 假标 R3；重写 live OCR 供应商。

## 合同

- IQ-CF-02/08：无依据文字不得合格采用；但识别器能力外的「残余墨迹」不得冒充文字审核。

## 怎么验收

- ocr + delivery 包测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果（2026-09-07）：
  - `go test ./internal/ocr/ ./internal/delivery/ -run "TestOCRExpectedOnly|TestOCRExtraInk|TestDeliveryAdoptionOCR"` → PASS
  - 采用：`ApplyImageOCRTraceForAdoption` / `ComparePNGExpectedOnly`（`RejectUnmatchedInk=false`）；缺期望字仍拒绝；合成严格模式保留 `TestOCRExtraInkFailsPass`
  - 解码：`decodeInkMask` 经 `image.Decode` 接受 JPEG/PNG
  - `just docs-check` → PASS
- 交付定位：随本任务提交
- 审核者 / 结论：主代理自审通过（2026-09-07）。纠正采用硬闸超出识别器能力的断点；≠R3 / ≠全文 OCR。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：完成。业务门槛：IQ-CF-02 仍部分；R3 未通过。
  - 剩余：CJK/live OCR；subject_preserve 资格诚实；生成路径 ApplyOCR。
