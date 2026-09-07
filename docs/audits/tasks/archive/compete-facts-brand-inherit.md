# 任务：品牌/视觉继承（CF-B5）

状态：完成
类型：实现
认领者：sub-compete/compete-facts-brand-inherit
认领于：2026-09-07T14:28:00+08:00
审核材料提交于：2026-09-07T14:45:00+08:00
完成于：2026-09-07T14:34:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：无强制；OCR/采用硬闸/主体提取另发；不改 IMG 42/32

按 [Issue 协议](../README.md) 认领。承接 [CF-B4](compete-facts-controlled-layout.md)、体验组 [brand-visual-reuse](brand-visual-reuse.md) 与父章程 IQ-CF-07 / CF-B5。协调者审核：2026-09-07 CTO 通过——visualsystem/recipe 与 Vitest 复测；风格链只保留 style/colors；Brand 占位诚实；未宣称 R3 / Brand 表 / 跨商分享。

## 问题来源

四级继承（商品覆盖 > 视觉方案版本 > 品牌版本 > 产品默认）未钉解析权威；品牌/方案更新可能静默改在做任务；第二商品可能带回旧身份。体验组已交付选择/预览 UX（Brand 占位）；本组需钉解析单测、配方清身份回归与第二商品样例。

## 做成什么样

四级优先级解析单测；实例存所选版本 id；更新需显式采用新版本（不得静默改在做任务与旧交付）；配方 extract/apply 继续清身份；第二商品样例：复用方案但 facts/参考为新商品。正例：商品 overlay 色盖方案色盖品牌色盖默认；新杯复用方案但容量为新 fact。反例：事实/参考进入风格链；品牌更新静默改旧交付；配方带回 `source_product_id`。

## 前置与并行

- 前置：CF-B0 已归档；CF-B4 已归档；[brand-visual-reuse](brand-visual-reuse.md) 已归档（UX/占位可复用）。
- Brand 表未建：保持显式占位合同（`brand_table_not_ready`），不伪造 Brand CRUD。
- 排他写入：继承解析权威、相关 visualsystem/recipe 回归、父章程 IQ-CF-07/CF-B5、本文件。勿与 `merchant-agent-tools` 同写 `go/internal/agent/` / `agent-service/`。
- 不改 Skill/grader/金标；无需真实 provider。

## 只改这些文件

实际写入：

- `go/internal/visualsystem/inheritance.go`：风格链只保留 `style`/`colors`；剥离子身份/事实键；Brand 载荷忽略
- `go/internal/visualsystem/inheritance_test.go`：四级色优先级、拒事实/身份、第二商品 ReusePreview 夹具
- `go/internal/recipe/brand_inherit_test.go`：extract 清身份；apply 绑新商品/新 fact；ReusePreview 待填
- `web/src/pages/workbench/canvas/visualReuse.ts(+test)`：overlay 载荷只取 style/colors
- `docs/audits/image-quality.md`：IQ-CF-07 / CF-B5 → 完成
- 本文件

复用未改（体验组已交付）：`service.go` / HTTP / `product_visual_selections` / Append 不静默更新 / Impact / 显式 Select。

## 不要碰

- Skill/grader/金标；IMG 42/32；新建 Brand 表冒充商家平台完成；跨商家分享；`go/internal/agent/`、`agent-service/`（并行 B7）；重做体验组已交付的 CRUD UI 除非接线必需。

## 现在代码在哪

- 合同：[image-quality.md](../../image-quality.md) IQ-CF-07 / CF-B5
- 解析权威：`visualsystem.ResolveInheritance` + `filterStyleChain`
- 体验组预览/选择：[brand-visual-reuse](brand-visual-reuse.md)；`product_visual_selections`
- 配方清身份：`recipe/payload.go` / `extract.go`；CF-B5 夹具 `brand_inherit_test.go`
- 节点 overlay：`mergeImageVisual` / `visual_overlay`

## 合同

- IQ-CF-07 / CF-B5；Brand 占位诚实；完成 ≠ R3 / ≠ Brand 表建成 / ≠ 跨商分享。

## 怎么验收

- 解析单测：四级优先级正反夹具
- 配方 extract/apply：不带回 `source_product_id` / fact/visual 版本绑定身份
- 更新：追加品牌/方案版本不静默改已选实例（显式采用）
- 第二商品样例清单或夹具：新容量/参考，复用方案风格
- 命令：见证据

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已归档；交付随本任务提交。

## 证据

### 实际修改范围

见上文「只改这些文件」。未改 `go/internal/agent/`、`agent-service/`、Skill/grader、Brand 表、看板 README。

### 验证命令与结果

| 命令 | 结果 |
|---|---|
| `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/visualsystem/ ./internal/recipe/ -count=1 -p 1'` | pass（协调者复测 2026-09-07） |
| `pnpm --dir web exec vitest run src/pages/workbench/canvas/visualReuse.test.ts` | 1 file / 4 passed |
| `just docs-check` | Documentation contract check passed（归档后） |

### 自审与剩余差距

- Brand 表仍为显式占位，不宣称多品牌实体或品牌色合并生效。
- 未宣称 R3 / 跨商分享 / 真实 provider。
- 体验组 HTTP 静默更新防护复用既有 `TestVisualSystemSaveSelectAppendDoesNotSilentUpdate`（包测覆盖）。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/compete-facts-brand-inherit.md` 查询）

- 审核者：CTO（本会话）；结论：通过。CF 实施批次 B0–B5 合同项本组已交付；OCR/采用硬闸/主体提取/R3 另发。
- Issue 结果：完成。业务门槛：IQ-CF-07 完成（Brand 占位）。剩余缺口：Brand 表、跨商分享、R3。
