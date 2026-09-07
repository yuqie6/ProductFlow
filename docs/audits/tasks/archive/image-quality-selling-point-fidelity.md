# 任务：卖点图保真与一理由策略对齐

状态：完成
类型：实现
认领者：sub-iq/image-quality-selling-point-fidelity
认领于：2026-09-07T17:10:00+08:00
完成于：2026-09-07T17:15:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：按回归再决定下一图种；≠R3 / ≠关闭旧 32 图位

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

[内容试跑](image-quality-content-pilot.md) 完整对照：候选相对 baseline 工作台均分 +4/平2/−2，闸门仍各 2/8。空气炸锅 `d229379fd27005e5` **selling_point** 保真 5→4、闸门是→否；视觉复核认为候选更接近「一理由」策略，与评委退步并存。家纺卖点仍多条底栏。需在**不改评委/金标**前提下，收窄生成内容策略使一理由与保真同向。

## 做成什么样

1. 定位卖点图提示词/节点拼装中导致保真退步或底栏堆砌的路径（候选 commit `61426ed6` 相对 baseline `1f0cf7b5` 的已审 prompts 差异为起点）。
2. 最窄修补：卖点图强化「一主要购买理由 + 可见结构证据」，同时保留主体/品牌/窗口等保真线索；禁止策划标签入片（已有则回归钉死）。
3. 确定性回归 + 可选小样本 live（若跑 live：费用须明示槽位上限，隔离栈，k≤1，不得改 grader/金标）。
4. 更新父章程一句；**不得**宣称 R3 或全面质量领先。

## 前置与并行

- 前置：内容试跑已归档；对照摘要 `storage-dev/image-quality-content-pilot-run/evidence/`。
- 不改评委、金标、直调提示词合同；不占用共享 `productflow` down。

## 只改这些文件

- `go/prompts/listing/image-types.md`、`go/prompts/listing/compile-image.md`
- `go/prompts/providers/prompt-generation.md`、`go/prompts/providers/creative-brief.md`
- `go/prompts/prompts_test.go`
- `go/internal/graph/listing_prompt.go`、`go/internal/graph/listing_prompt_test.go`
- `docs/audits/image-quality.md` 一句
- 本文件

## 不要碰

- 评委 / 金标 / gate 阈值；Skill grader；开放第二商；IMG 42/32 完成条件偷换。

## 合同

- 一理由策略与保真同向改善为验收目标；完成可 FAIL（live 未授权则只交确定性+设计说明）。
- ≠R3。

## 怎么验收

- 定向单元/拼装回归；若有 live：隔离跑炸锅 selling_point 至少 k=1 对照并记录保真维与闸门，诚实缺口。
- `just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已归档；交付随本任务提交。未跑 live；≠R3。

## 证据

### 协调者复验

| 检查 | 结果 |
|---|---|
| prompts/graph/providers 定向 Go | PASS |
| `CompileImageModelPrompt` selling_point 只发第一句 | 代码已审 |
| live | 未跑（合同允许） |



- 命令 / 日期 / 结果：
  - 2026-09-07：对照试跑 `comparison-summary.json` / `visual-observations.json`：炸锅卖点保真 5→4；候选更「一理由」但仍有 3 项底栏；家纺仍 4 项底栏图标带；候选 CGI 线框剖视相对 baseline 更伤本体观感。
  - 设计说明：一理由与保真冲突主因是（1）底栏被当成多利益图标带；（2）臆造线框/内部示意替代参考可见部件；（3）拼装把多条 `selling_points` 用顿号整串发给生图模型。修补：图种/compile/prompt-generation/brief 要求「一理由 + 本体可见证据 + 品牌/窗口保真同向」；底栏仅品牌/认证/质保；禁多利益底栏与线框剖视；策划标签继续禁止；`CompileImageModelPrompt` 对 `selling_point` 只发第一句「主要购买理由」。
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./prompts ./internal/graph -count=1 -p 1 -run "TestCatalogHasRequiredImageTypesAndNeedles|TestCompileImageModelPrompt|TestImageInstructions|TestLocalSupplement"'` → ok
  - `go test -C go ./internal/providers -count=1 -p 1 -run "TestBuildPrompt|TestListingPromptSchema|TestMatchJSONSchema"` → ok；`go vet` prompts/graph/providers → ok
  - `just docs-check` → passed
  - 未跑 live（无隔离栈费用授权把握）；不宣称保真维或闸门已改善。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/image-quality-selling-point-fidelity.md` 查询）
- 审核者：CTO（本会话）；结论：通过——因果修补与确定性回归可采信；**未**宣称 live 保真/闸门改善；≠R3。
- Issue 结果 / 业务门槛结果 / 剩余缺口：完成（确定性）；live k=1 未跑。
