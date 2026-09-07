# 任务：subject_preserve 资格声明不得早于真实执行

状态：完成
类型：实现
认领者：主代理-cto-subject-preserve-honesty
认领于：2026-09-07T18:49:00+08:00
完成于：2026-09-07T18:52:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：合成 PNG 作为交付字节；≠R3 / ≠像素保真

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

只读审查 P1：主图等默认 `subject_preserve`，但执行仍统一调用生成式图片 provider；随后可仅凭参考存在等条件写出 `route_qualified=true`、`appearance_may_change=false`。参考图提取/合成元数据**不等于**成片已保留主体。会让交付资格表达尚未验证的保证。

## 做成什么样

1. 当 `subject_preserve` 交付字节仍来自生成式 provider 时：**不得**声明 `appearance_may_change=false`；须诚实标记外观可能变化（或等价 unresolved，使不得冒充「保留主体已执行」）。
2. 参考图上的 extract/compose 闸可继续写元数据；失败仍 `route_qualified=false`；成功**不得**单独证明「成片像素已保留主体」。
3. 自动化：生成式出图的 subject_preserve → appearance_may_change=true（或不合格声明）；既有提取失败路径保持拒绝合格。
4. 更新父章程 IQ-CF-04/05 一行；**≠宣称 R3**；**≠** 本门必须把 compose PNG 接到资产链（可列残余）。

## 前置与并行

- 排他：`go/internal/graph` produce_route / execute / subject_extract 最窄、本任务、父章程。
- 勿改：评委；OCR；quota。

## 只改这些文件

- graph 路线/执行相关 + 测试
- `docs/audits/image-quality.md`、本文件

## 不要碰

- 假标像素保真；假标 R3。

## 合同

- IQ-CF-04/05：保留主体声明须对应当前真实执行能力。

## 怎么验收

- 包测；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 实现：`ProduceRouteInput.SubjectPreserveDeliveryFromCompose`；未置位时 `subject_preserve` → `appearance_may_change=true`；仅合成交付字节时可声明外观不变。执行路径仍 `GenerateImage`，故不置该标志。
- 命令 / 日期 / 结果（2026-09-07）：`go test ./internal/graph/ -run "ProduceRoute|SubjectPreserve|GateImage|ApplySubject"` → PASS；`just docs-check` → PASS
- 交付定位：随本任务提交
- 审核者 / 结论：主代理自审通过。纠正「有参考即外观不变」的虚假保证；≠R3；合成 PNG 作交付资产仍残余。
- Issue 结果 / 业务门槛结果 / 剩余缺口：Issue 完成；IQ-CF-04 仍部分；残余：compose 字节入资产链。
