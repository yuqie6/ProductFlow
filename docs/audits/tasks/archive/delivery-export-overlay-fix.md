# 任务：成果页导出按钮叠层可达修复

状态：完成
类型：实现
认领者：sub-r2/delivery-export-overlay-fix
认领于：2026-09-07T17:48:30+08:00
审核材料提交于：2026-09-07T18:01:30+08:00
完成于：2026-09-07T18:10:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：R2 关闭裁定（仍须 §5.4 等缺口齐）；≠假标 R2

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

[R2 核心路径门](delivery-r2-core-path-gate.md) 本门 PASS，但成果「导出已采用交付」按钮可被画布右上控件拦截；E2E 只能断言可见后走 API 导出。方向 2 要求桌面/手机核心任务可达——导出入口被叠层挡住是诚实缺口。

## 做成什么样

1. 修复成果视图下导出采用交付控件的点击可达（z-index/布局/pointer-events 等最窄改动）；桌面 1440 与手机 390 均可点到并触发导出（或打开导出流程）。
2. 扩展或复用 `web/e2e/delivery-r2-core-path.spec.ts`：断言真正 UI 点击导出成功（不只 API）。
3. 更新父章程该缺口行；**不得**因本修宣称 R2 全过。

## 前置与并行

- 前置：delivery-r2-core-path-gate 已归档。
- 排他：相关 workbench/results UI 文件、e2e、本任务、父章程；勿改 Graph 额度/OCR 并行包。

## 只改这些文件

- `web/src/pages/workbench/`（最窄）
- `web/e2e/delivery-r2-core-path.spec.ts`（或窄扩展）
- 父章程、本文件

## 不要碰

- 假标 R2；Skill/grader；额度/OCR 包。

## 合同

- ROADMAP §5.5 可达；完成 ≠ R2 关闭。

## 怎么验收

- Playwright UI 点击导出；`just docs-check`。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：审核材料已提交；**未提交 git**；状态保持认领，等维护者归档。并行 agent 改动导致 `go/internal/agent` 当前无法编译 dispatcher，本门用既有 dispatcher 二进制 + Redis DB2 隔离栈采证。

## 证据

### 根因与修复

- 根因：成果层 `z-10`，画布右上 `data-graph-canvas-toolbar` 为 `z-30 absolute`，导出按钮在成果头右侧被工具条命中拦截。
- 修复：成果态不渲染浮动工具条；将视图切换与画布最大化收纳进成果头 `headerActions`，与「导出已采用交付」同排可点。流程态工具条不变。

### 命令 / 日期 / 结果

| 命令 | 日期 | 结果 |
|---|---|---|
| `pnpm --dir web exec vitest run …GraphResultsView.test.tsx …ProductWorkbenchSurface.graph.test.ts` | 2026-09-07 | 2 files / 14 passed |
| 隔离栈：`APP_PORT=29392` + Vite `29393` + worker/dispatcher `REDIS_URL=…/2`（不 down 共享 productflow） | 2026-09-07 | healthz 200；采证前 SQL 种子商家额度（并行额度接线后账本为空会「可用额度不足」） |
| `PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 WEB_BASE_URL=http://127.0.0.1:29393 pnpm --dir web exec playwright test e2e/delivery-r2-core-path.spec.ts` | 2026-09-07T18:01+08:00 | **2 passed / 25.7s**；桌面/手机均真实 `click` 导出（无 `force`）；成果态 `data-graph-canvas-toolbar` count=0 |
| `just docs-check` | 2026-09-07 | 见交付回合 |

证据目录：`storage-dev/audits/delivery-export-overlay-fix/`（gitignore；含 `adoption-export.zip`、`mobile-390x844-export-reachable.png`、`playwright3.log`）。

### 自审

- **未宣称 R2 全过**；仅闭合导出 UI 叠层缺口。
- §5.4 / Brand / 批跑仍为父章程缺口。
- 未改 `go/internal/graph/*`、`go/internal/ocr/*`、quota 包源码。

- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/delivery-export-overlay-fix.md` 查询）
- 审核者 / 结论：主代理自审通过（2026-09-07）。Vitest 14 PASS；证据目录有 390 导出可达截图；成果态 `showFlowView` 下才渲染 toolbar。≠R2 全过。
- Issue 结果 / 业务门槛结果 / 剩余缺口：
  - Issue：**完成**（导出叠层可达）。
  - 业务门槛：总纲 **R2 未通过**（§5.4/Brand/批跑仍缺）。
