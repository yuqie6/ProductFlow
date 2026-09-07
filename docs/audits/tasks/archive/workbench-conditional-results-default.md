# 任务：有产出时默认进入成果视图

状态：完成
类型：实现
认领者：sub-delivery/workbench-conditional-results-default
认领于：2026-09-07T14:00:00+08:00
审核材料提交于：2026-09-07T14:15:00+08:00
完成于：2026-09-07T14:08:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：记忆上次视图（可选）；不改生成链

按 [Issue 协议](../README.md) 认领。承接 [默认入口走查](workbench-default-entry-walkthrough.md)。协调者审核：2026-09-07 CTO 通过——Vitest 17；条件默认成立；未宣称浏览器整链/跨会话记忆。

## 问题来源

走查建议：**暂不全局**默认成果视图；复访已出图时成果更省力，首次制作仍需流程。可选「有产出时默认成果」。

## 做成什么样

当商品已有生图/成果时，工作台打开默认「图片成果」；无产出或空图时仍默认「流程编辑」。不记忆跨会话除非另开。须在挂载 delivery-adoption / visual-systems 的栈上验证采用入口。完成 ≠ 全局一律成果。

## 前置与并行

- 前置：走查已归档。
- 排他写入：工作台默认视图选择及相关测试/文案、父章程、本文件。勿改 graph 执行器。
- 并行：`compete-facts-impact-preview` 占用 factImpactPreview / GraphNodeInspector 事实预览——本任务未改 Inspector；`merchant-delivery-localedit` 占用 Go delivery/localedit——未改。

## 只改这些文件

- `web/src/pages/workbench/canvas/resultProjection.ts`（`graphHasUsableResultImages` / `defaultWorkbenchMainView`）
- `web/src/pages/workbench/canvas/resultProjection.test.ts`
- `web/src/pages/workbench/agent/ProductWorkbenchSurface.tsx`
- `web/src/pages/workbench/agent/ProductWorkbenchSurface.graph.test.ts`
- `web/src/pages/workbench/canvas/GraphCanvasPanel.tsx`（非受控默认）
- `docs/USER_GUIDE.md`、`docs/PRD.md`、`web/src/pages/HelpPage.tsx`
- `docs/audits/canvas-test-system.md`、本任务文件

未改 `GraphNodeInspector`、Go delivery/localedit、看板 README；未 commit。

## 合同

- 走查结论；完成 ≠ 全局默认成果。
- 判据：生图 `preview_asset_id` 或证据 `bound_asset_id`/`preview_asset_id` 至少一枚非空 → 打开默认 `results`，否则 `flow`。
- 仅打开时初始化；会话内用户切换与跨会话均不记忆（除非另开任务）。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核。
- 交接：保持认领等审核；**未 git commit**。

## 证据

### 实现

- `defaultWorkbenchMainView(graph)`：有可用当前图 → `"results"`，否则 `"flow"`。
- `ProductWorkbenchSurface` / 非受控 `GraphCanvasPanel` 用该函数作 `useState` 初值；Surface 受控传给 Canvas。
- 活文档与帮助页同步条件默认说明。

### 验证

| 项 | 结果 |
|---|---|
| `pnpm --dir web exec vitest run src/pages/workbench/canvas/resultProjection.test.ts src/pages/workbench/agent/ProductWorkbenchSurface.graph.test.ts` | **2 files / 17 passed**（含有产出→results、无产出→flow） |
| 浏览器复访已出图商品 / 空图商品 | **未跑**（并行共享栈；采用/视觉 API 仍可能为旧进程缺口）。诚实缺口：未宣称浏览器门通过 |
| 未改他人 CF-B2 WIP；未 commit | 是 |

### 未宣称

- 全局一律默认成果
- 跨会话记忆上次视图
- 采用/导出/视觉整链浏览器验收
- R2 / 真实 provider

- 审核者：CTO（本会话）；结论：通过。可选拆记忆上次视图；≠全局一律成果。
