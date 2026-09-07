# 任务：R2 常用操作核心路径验收门

状态：认领
类型：证据
认领者：sub-r2/delivery-r2-core-path-gate
认领于：2026-09-07T17:17:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：总纲 R2 关闭裁定（仅当本门与缺口齐）；≠R3

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](README.md)。

## 问题来源

方向 2 / ROADMAP **R2**：按第 5 节完成创建、改稿、采用、交付、复用；检查实际持久状态和文件，桌面/手机核心任务可达。现有成果视图、采用快照、条件默认与记忆视图已交付，但 **R2 仍标整体未验收**；浏览器整链 opt-in 多未跑。

## 做成什么样

1. 固定一商品路径证据（隔离 mock 或 skip-Agent 夹具，**不强制真实 provider**）：创建可执行工作流 → 成果视图可见产出 → 交付采用 → 导出包与采用版一致 → 重跑不覆盖已采用 → 配方/视觉复用到第二商品且清旧身份（可复用既有 API/E2E）。
2. 桌面 `1440×960` 与手机 `390×844` 至少各采一轮核心可达性（创建入口、成果切换、采用/导出入口）；截图或 Playwright trace 入库证据目录。
3. 对照 ROADMAP §5.1–5.6 列「已齐 / 缺口」；缺口诚实列出（批量无限制批跑等可标非本门）。
4. 更新 `docs/ROADMAP.md` R2 行仅当维护者后续裁定；本门可 FAIL；更新父章程。**不得**因局部 E2E 假标 R2 全过——若证据不足则保持未通过并写清缺口。

## 前置与并行

- 前置：delivery-adoption / workbench results / brand-visual-reuse / remember-view 已归档。
- 隔离 mock 栈；`PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1` 类 opt-in；禁止共享 `productflow` down。

## 只改这些文件

- 本文件、父章程、必要时 ROADMAP R2（仅诚实状态）
- `web/e2e/*` 扩展或新 opt-in 规格（最窄）
- 证据目录（gitignore 或 `docs/audits` 摘要）

## 不要碰

- 真实付费批跑；改 Skill/grader；开放第二商；假标 R2。

## 合同

- ROADMAP R2 / §5；完成可 FAIL。
- 持久状态与文件哈希/资产 ID 须可核对。

## 怎么验收

- 路径表 + 命令/exit + 视口证据；`just docs-check`。
- 父章程与 ROADMAP 叙述一致。

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
