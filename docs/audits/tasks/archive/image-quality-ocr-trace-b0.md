# 任务：成片文字 OCR 追溯闸 B0

状态：完成
类型：实现
认领者：sub-iq/image-quality-ocr-trace-b0
认领于：2026-09-07T17:42:00+08:00
完成于：2026-09-07T17:50:30+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：采用路径消费 OCR 结果；≠R3 全过 / ≠改评委

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

IQ-CF-02 / CF-B1 已有 `text_trace` 元数据与卖点一理由检查；**成片 OCR 对照闸未建**。采用硬闸只消费声明式 `text_qualified`，不能证明画面文字与 fact 一致。卖点 live 仍保真 4，其它图种文字风险仍在。

## 做成什么样

1. 最小 OCR 对照能力：对一张成片（PNG）提取可见文字（可接已有 provider/本地库；选型写入任务证据，不在父章程指定 SDK），与该产物挂载的 `text_trace.fact_keys` / 显式覆盖对照。
2. 产出结构化结果：匹配/缺失/多余；失败→`text_qualified` 不得为 pass（或等价字段）；自动化至少覆盖「元数据有 key、成片缺字」与「成片与 fact 一致」。
3. **可不**改 live 评委/金标；**可不**全量改采用 HTTP（可留内部 API/函数供后续消费）。更新父章程 IQ-CF-02 行诚实状态。≠宣称 R3。

## 前置与并行

- 前置：CF-B1、adoption hard-gate、selling-point live 已归档。
- 排他：OCR 新包或 `go/internal/graph`/`layout` 最窄接线、本任务、父章程；勿改 R2 e2e；勿改评委/金标。

## 只改这些文件

- OCR/对照实现与测试
- 必要时产物元数据字段（窄）
- `docs/audits/image-quality.md`
- 本文件

## 不要碰

- 评委/金标/gate 分数；Skill grader；假标 R3；R2 playwright。

## 合同

- IQ-CF-02 OCR 子集；完成 ≠ R3。

## 怎么验收

- 包测；`just docs-check`；父章程更新。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：实现与验证已就绪，等待协调者审核；**未 commit / 未 push**。

## 证据

- 选型：仓库无现成 provider OCR。B0 默认引擎 = 嵌入 Liberation Sans 的**字形模板像素匹配**（`Contains`）+ 抹去已匹配模板后的**残余墨迹**作多余；`PRODUCTFLOW_OCR_LIVE=1` 可选 OpenAI 兼容视觉抽字。禁止 PNG 元数据/旁路假 OCR。
- 包：`go/internal/ocr`（`ComparePNG` / `ApplyOCRGate` / `ExpectedFromTextTrace`）；窄接线 `graph.ApplyImageOCRTrace` 写回 `text_trace.ocr_check`，失败强制 `text_qualified=false`；不抬升声明式不合格；**未**改采用 HTTP。
- 正测：成片渲染 `600ml` ↔ fact `capacity` → pass，声明 true 保持。
- 反测：元数据有 `capacity`/`600ml`、成片无该字 → missing → `text_qualified=false`；额外可见墨迹 → extra → 不得 pass。
- 命令（2026-09-07）：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/ocr/ ./internal/graph/ -count=1 -p 1 -run "OCR|ApplyImageOCR|Contains|UserOverride|Extra"'` → PASS
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/ocr/ -count=1 -p 1'` → PASS
  - `just docs-check` → FAIL 于无关看板项 `delivery-export-overlay-fix.md`（他组占用；本任务未改看板 README）；本任务与父章程链接自洽
- 父章程：IQ-CF-02 仍 `部分完成`（OCR B0 已交；采用默认接线/CJK/开放词表/R3 未宣称）。
- 未宣称：R3；采用路径默认 OCR；评委/金标分；任意生成式成片全覆盖。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/image-quality-ocr-trace-b0.md` 查询）
- 审核者 / 结论：主代理自审通过（2026-09-07）。复测 `go test ./internal/ocr/ ./internal/graph/ -run OCR|…` PASS；失败强制 `text_qualified=false`；未改评委；≠R3；采用 HTTP 未默认接线。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：**完成**（OCR B0）。
  - 业务门槛：IQ-CF-02 仍 **部分完成**；R3 **未通过**。
  - 剩余缺口：采用路径默认消费 OCR；CJK/开放词表；生成路径自动 Apply；主体提取。
