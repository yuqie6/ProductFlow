# 任务：记忆工作台上次视图

状态：完成
类型：实现
认领者：sub-delivery/workbench-remember-last-view
认领于：2026-09-07T15:15:00+08:00
审核材料提交于：2026-09-07T15:30:00+08:00
完成于：2026-09-07T15:20:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无

按 [Issue 协议](../README.md) 认领。承接 [有产出时默认成果](workbench-conditional-results-default.md)。协调者审核：2026-09-07 CTO 通过——Vitest 30 与 docs-check 复测；商品级显式偏好；未宣称全局默认成果/浏览器整链。

## 问题来源

条件默认已交付；用户希望跨会话记住上次「流程/成果」选择时，当前仍每次按有无产出重算。

## 做成什么样

在显式用户偏好下记忆上次主视图（**商品级**，钉死）；无偏好时仍用条件默认。完成 ≠ 强制全局默认成果。

## 前置与并行

- 前置：条件默认已归档。
- 排他写入：前端偏好存储与相关测试、父章程、本文件；活文档/帮助页同步行为说明。

## 只改这些文件

- `web/src/pages/workbench/chrome/workbenchUiState.ts`（`mainView` 偏好 / `clearWorkbenchMainViewPreference`）
- `web/src/pages/workbench/chrome/workbenchUiState.test.ts`
- `web/src/pages/workbench/canvas/resultProjection.ts`（`resolveWorkbenchMainView`）
- `web/src/pages/workbench/canvas/resultProjection.test.ts`
- `web/src/pages/workbench/agent/ProductWorkbenchSurface.tsx`
- `web/src/pages/workbench/agent/ProductWorkbenchSurface.graph.test.ts`
- `web/src/pages/workbench/canvas/GraphCanvasPanel.tsx`（非受控路径）
- `docs/USER_GUIDE.md`、`docs/PRD.md`、`web/src/pages/HelpPage.tsx`
- `docs/audits/canvas-test-system.md`、本任务文件

未改 Skill/grader、看板 README、商家平台、Go delivery。

## 合同

- 可选增强；须可关闭记忆回退条件默认。
- 钉死：商品级 localStorage（`workbenchUiState.mainView`）。
- 仅用户显式切换（含从成果定位回流程）写入偏好；打开时不得把条件默认写成偏好。
- `clearWorkbenchMainViewPreference(productId)` 清除后回退 `defaultWorkbenchMainView`。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：无。
- 交接：已归档；交付随本任务提交。

## 证据

### 实现

- `resolveWorkbenchMainView(graph, preferred)`：合法 `flow`/`results` 偏好优先，否则条件默认。
- `ProductWorkbenchSurface` / 非受控 `GraphCanvasPanel` 打开时读商品偏好；切换时 `patchWorkbenchUiState({ mainView })`。
- `clearWorkbenchMainViewPreference` 删除字段且保留邻域 UI 态。

### 验证（协调者复测 2026-09-07）

| 项 | 结果 |
|---|---|
| `pnpm --dir web exec vitest run src/pages/workbench/canvas/resultProjection.test.ts src/pages/workbench/chrome/workbenchUiState.test.ts src/pages/workbench/agent/ProductWorkbenchSurface.graph.test.ts` | **3 files / 30 passed** |
| `just docs-check` | **passed** |
| 浏览器复访 | **未跑** |

### 未宣称

- 全局一律默认成果
- 用户级（跨商品）偏好
- 浏览器整链验收
- R2 / 真实 provider

- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/workbench-remember-last-view.md` 查询）
- 审核者：CTO（本会话）；结论：通过。
