# 任务：浏览器整图跑中途撤销与文稿保存 409 停止

状态：完成
类型：实现
认领者：主代理-0905-1436
认领于：2026-09-05T14:36:17+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，业务组结论见[父账本](../../canvas-test-system.md)。

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 取得已确认的认领后，才开始读取当前实现、追踪调用链、调查根因或设计方案；修改前核对适用仓库规则、测试和 diff。

## 问题来源

[canvas-inspector-midrun](canvas-inspector-midrun.md) 关闭时声明：整图跑中途撤销与浏览器 409 交互未纳入该单 Playwright。父账本用法矩阵对应两行仍为未单独覆盖 / 部分完成。维护者 2026-09-05 核对现场：

- C4 运行中检查器打字已有 `mid-run inspector typing keeps live authored copy after graph adopt`，不重复派。
- AR-01 版本语义已交付，不重复派基线传递修复。
- C3 HTTP 同节点过期保存 409 已有 `TestInspectorSaveAfterSameNodeAdoptConflicts`；浏览器层没有看到 `graph.canvas.revisionConflict` 后停止重放的用例。
- 整图跑中途撤销的 HTTP 具名钉死已由 [canvas-graph-run-undo.md](canvas-graph-run-undo.md) 交付；本单补用户点击工具栏「撤销」的浏览器证据。两单可分别验收。关闭本单时不得把 C3 HTTP 缺口写成未覆盖。

## 做成什么样

`just web-e2e-canvas-document` 新增（或扩展现有 spec 的）两条 mock 浏览器用例，且现有三条必须仍过：

1. **整图跑中途撤销**：用户点 `data-graph-run-all`（body `scope=graph`），在该次 run 仍为 `running` 或 `queued` 时点画布「撤销」（`graph.canvas.undo`）。跑完后 live 文稿是撤销后的内容，mock 生成不得盖 live（O2/O3）。保存/撤销当时须断言 run 仍 in-flight。Mock 太快时沿用现有 `withPausedGraphWorker`，不得改成 `waitForGraphRunSucceeded` 之后再撤销。
2. **文稿保存 409 后停止**：检查器手填使草稿变脏；用 `page.request` 对**同一节点**发一次会冲突的 ChangeSet（同节点变更，服务端不得 rebase）；随后自动保存须 409。页面提示 `graph.canvas.revisionConflict`（`data-graph-canvas-notice`）。统计文稿 `update_node_config` 请求：409 之后不得再自动重放该次保存。仅纯 `move_nodes` 允许自动重放，本用例禁止用位移操作冒充文稿保存。

裁判是 live 文稿、origin、pending 候选与 409 次数，不是文采。prompt/image 必须 mock。

## 前置与并行

- 前置：已起的 `just dev`。不依赖 canvas-graph-run-undo 关闭。
- 冻结输入：测试期间固定画布代码与 mock 配置；不得改 AR-01 已交付的草稿基线语义来让 409 消失。
- 运行资源：本测试临时切换 provider，可能暂停 `productflow-worker`。共享 dev 必须独占该窗口；**不得与 [image-eval-pool.md](../image-eval-pool.md) 的 `image-evals-run` 或任何真实生图/Agent live 同时切 mock、暂停 worker**。image-eval-pool 当前只做淘宝采集、live 未跑时，仍须在开跑本门前与其执行者确认未进入 live，并在结束后恢复 provider 与 worker。不得覆盖 `STORAGE_ROOT/image-evals/`。

## 只改这些文件

- `web/e2e/canvas-document-mock.spec.ts`
- 本文件

现有源码若浏览器路径确实违背合同，允许修复下列路径；没有因果需要的文件不动，认领时对本清单统一检查独占：

- `web/src/pages/workbench/canvas/GraphCanvasPanel.tsx` 的 undo / 文稿 409 提示与重放分支
- `web/src/pages/workbench/canvas/GraphNodeInspector.tsx`、`useNodeDraftAutosave.ts` 仅当 409 提示或停止重放的真实缺口在此
- 上述模块相邻的直接回归测试

不要改 Go 文稿 cook / 搜索器，不要重做 AR-01 基线传递。

## 不要碰

- `go/internal/graph/` 权威测试与 cook 实现（C0–C3、C6；整图 HTTP undo 归 canvas-graph-run-undo）
- `just web-e2e-live-graph`
- Agent 评测、Skill、图片池像素与 manifest
- 把用例改成跑完再 undo / 再填
- 把 `scope=graph` 缩成 `scope=node`
- 循环重试 409；把文稿保存改走 `executeApply` 的 `move_nodes` 重放

## 现在代码在哪

- 空闲改写/补全/替换与运行中打字：`web/e2e/canvas-document-mock.spec.ts`（3 条）。
- 撤销按钮：`GraphCanvasPanel` `runHistoryMutation("undo")`；`disabled={structureBusy || !graph.can_undo}`。`structureBusy` 是结构保存/undo/redo/提案，**不是** GraphRun，运行中按钮按理可点。
- 文稿 `commitNode` 走 `applyMutation.mutateAsync`，不走 `executeApply`，409 不重放（`GraphCanvasPanel.test.ts` 源码钉死）。
- `executeApply`：非 `move_nodes` 的 409 只 `showNotice(t("graph.canvas.revisionConflict"))`。
- 草稿 409 保留草稿：`useNodeDraftAutosave.test.ts`。这些单测不代替本单 Playwright。
- 同节点 HTTP 409：C3 `TestInspectorSaveAfterSameNodeAdoptConflicts`。

## 合同

- 用户点整图就 `scope=graph`。
- O2/O3：撤销后的 live 不得被本次 graph cook 的 provider 结果盖掉。
- O7：同节点丢失更新必须 409；浏览器文稿保存 409 只提示、不自动重放。
- 供应商绑定必须是 mock。需要已起的 `just dev`。

## 怎么验收

```bash
just web-e2e-canvas-document
```

新用例须在撤销或冲突保存当时断言 run 仍 in-flight（撤销条）或统计到单次 409 且无重放（409 条）。现有三条继续通过。源码若有改动，另跑 `web/AGENTS.md` 的 test / lint / build 中与改动边界对应的部分。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：主代理-0905-1436
- 交接：已关闭。开跑时 image-eval-pool live 未跑、仅淘宝采集；本单占用 mock/provider 窗口与暂停 worker 的窗口已结束。未改 dispatcher / Skill / 图片池文件。

## 证据

```text
测试：mid-run undo keeps reverted live copy after graph adopt；document save 409 shows revision conflict and does not replay
夹具：withPausedGraphWorker + data-graph-run-all（scope=graph）后工具栏撤销；检查器手填后 page.request 同节点 update_node_config，自动保存 409。
断言：撤销当时 run 仍为 running|queued；adopt 后 live 回到 seed，不是手填稿。409 后 data-graph-canvas-notice 为 graph.canvas.revisionConflict 文案；其后无 update_node_config 重放。
实现：commitNode 在 ApiError 409 时 showNotice(revisionConflict) 再抛出，不走 executeApply。selectNode 先打开详情侧栏再点节点，避免合成 click 未选中。
命令：just web-e2e-canvas-document
日期 / 结果：2026-09-05 Chromium 5 passed (44.5s)
单测：pnpm --dir web exec vitest run src/pages/workbench/canvas/GraphCanvasPanel.test.ts → 3 passed
eslint：GraphCanvasPanel.tsx / GraphCanvasPanel.test.ts / canvas-document-mock.spec.ts 通过
just docs-check：pass
审核者 / 结论：主代理-0905-1436 自审通过，非独立审核。
Issue 结果：完成。C4 浏览器整图跑中途撤销与文稿 409 停止已进 just web-e2e-canvas-document。
业务门槛结果：工作流体验用法矩阵「整图跑中途撤销」「浏览器看到文稿 409 后停止」两行完成。
```

- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/canvas-c4-remainder.md` 查询）；已有独立候选 commit 时填写其 hash，不为回填本次 hash 另提 commit。
- 审核者 / 结论（自审须注明）：主代理-0905-1436 自审通过，非独立审核。
- Issue 结果 / 业务门槛结果 / 剩余缺口：完成。本组章程已明确的 C4 剩余缺口已落地；无本单拆出后续。
