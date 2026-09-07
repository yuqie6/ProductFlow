# 任务：受控二维排版（CF-B4）

状态：完成
类型：实现
认领者：sub-compete/compete-facts-controlled-layout
认领于：2026-09-07T14:17:00+08:00
完成于：2026-09-07T14:28:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：CF-B5 品牌/视觉继承；不改 IMG 42/32

按 [Issue 协议](../README.md) 认领。承接 [CF-B3](compete-facts-produce-route.md) 与父章程 IQ-CF-06 / CF-B4。协调者审核：2026-09-07 CTO 通过——layout 包测、Vitest、docs-check 复测；选型 A 自研最小组合器、未新增主依赖；未宣称 HTTP/工作台编辑器/采用硬闸/R3/CF-B5。

## 问题来源

规格/营销字仍依赖生图重写；无图内层/字体/安全区结构，改字号会重生成杯身。DeliverySpec 只做缩放裁切，不是排版。

## 做成什么样

先完成 IQ-CF-06 选型比较备忘（能力/一致性/许可/维护/归属/非目标），再落地最小可测结构：图片层、文字层、必要形状、字体、字号、颜色、对齐、安全区；浏览器预览与服务端导出同输入一致；接现有资产 lineage。正例：规格字改字号不重生成杯身；同夹具预览与导出一致。反例：排版服务调度 Graph 节点；未经验证 SDK 进主依赖；缺字静默空白当合格。

## 前置与并行

- 前置：CF-B3 已归档（`produce_route` 可用）；维度表见父章程 IQ-CF-06。
- 排他写入：排版 schema/服务、预览/导出一致性夹具、选型备忘、父章程 CF-B4/IQ-CF-06、本文件。勿改 Skill/grader/金标、IMG 42/32、delivery 采用快照核心除非接线 lineage 必需。
- 可与 `merchant-agent-tools` 并行：勿写 `go/internal/agent/`。
- 不调用真实 provider 完成本批。

## 只改这些文件

- [iq-cf-06-controlled-layout-selection.md](../../iq-cf-06-controlled-layout-selection.md)：IQ-CF-06 六维选型备忘（选定 A）
- `go/internal/layout/`：Document/Compose/LayoutPlan/lineage/预览框比较；testdata Liberation Sans（OFL）
- `web/src/lib/layout/preview.ts` + `preview.test.ts`：同 schema 预览框与夹具
- `docs/audits/image-quality.md`：IQ-CF-06 / CF-B4 状态
- `docs/ARCHITECTURE.md` / `docs/ARCHITECTURE.en.md`：layout 所有权行
- `go/AGENTS.md`：包列表含 `layout`
- 本文件

## 不要碰

- Skill/grader/金标；IMG live 42/32；第二工作流编辑器；任意图片 PSD 级分层；用 Graph DAG 调度排版服务；未经验证的主依赖 SDK。
- `go/internal/agent/`、`agent-service/`（并行 B7）

## 现在代码在哪

- 选型备忘：[iq-cf-06-controlled-layout-selection.md](../../iq-cf-06-controlled-layout-selection.md)
- 排版权威：`go/internal/layout`（`Compose` / `ValidateDocument` / `PreviewFrames`）
- 浏览器预览：`web/src/lib/layout`
- 交付缩放裁切：`go/internal/delivery/renderer.go`（**不是**排版）
- 路线标签：`go/internal/graph/produce_route.go`（CF-B3）

## 合同

- IQ-CF-06 / CF-B4；完成 ≠ R3 / ≠ CF-B5 / ≠ 主体提取链。

## 怎么验收

- 选型备忘按六维可回答，并记录选定方案与否决理由 → [selection](../../iq-cf-06-controlled-layout-selection.md)
- 正测：规格字改字号→主体位图资产不变、仅排版重出；固定夹具预览 vs 导出层框 ≤1px
- 反测：缺字不得标合格；包内不 import graph；未验证 SDK 未进主依赖
- 命令：

```bash
bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./internal/layout/ -count=1 -p 1'
pnpm --dir web exec vitest run src/lib/layout/preview.test.ts
just docs-check
```

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已归档；交付随本任务提交。

## 证据

- 选型：选定 **A 自研最小组合器**（已有 `golang.org/x/image`）；否决 Fabric/Konva、Skia/CanvasKit、HTML+headless、扩展 `delivery.Render`；未新增主依赖 SDK
- 结构：`Document` 含 image/text/shape、字体/字号/颜色/对齐/安全区；`Compose`→PNG+`LayoutPlan`；`Lineage.parent_asset_id=subject_asset_id`
- 正夹具：`TestFontSizeChangeKeepsSubjectBitmap`；`TestPreviewPlanMatchesExportFrames`；Vitest `matches export plan frames within 1px` / `keeps subject layer when only font_size changes`
- 反夹具：`TestMissingGlyphNotQualified`；`TestPackageDoesNotImportGraph`；`TestOutsideSafeNotQualified`
- 验证（协调者复测 2026-09-07）：
  - `bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./internal/layout/ -count=1 -p 1'` → ok
  - `pnpm --dir web exec vitest run src/lib/layout/preview.test.ts` → 3 passed
  - `just docs-check` → Documentation contract check passed
- 自审：未改 Skill/grader/金标、agent/agent-service、delivery 采用核心、IMG 42/32、看板 README；未 commit
- 未宣称：HTTP API / 工作台编辑器 / 采用硬闸消费 `layout_qualified` / 主体提取链 / R3 / CF-B5 / 字形级浏览器≈Go 像素（框级 ≤1px）
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/compete-facts-controlled-layout.md` 查询）

- 审核者：CTO（本会话）；结论：通过。可拆 CF-B5。
- Issue 结果：完成。业务门槛：IQ-CF-06 部分完成（库级组合器就绪；HTTP/编辑器/采用硬闸另发）。剩余缺口同上未宣称项。
