# 任务：检查器运行该节点、运行到此与运行中取消的浏览器证据

状态：完成
类型：实现
认领者：主代理-0905-1453
认领于：2026-09-05T14:53:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无（运行此场景 / 失败重试另核，不并进本单）

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，业务组结论见[父账本](../../canvas-test-system.md)。

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。

## 问题来源

父账本 [真实用法矩阵](../../canvas-test-system.md#真实用法矩阵) 按钮以 USER_GUIDE §3.4 为准，列出检查器「运行该节点」「运行到这里」和「取消」。维护者 2026-09-05 核对现场：

- C4 已覆盖空闲改写/补全/替换、运行中打字、整图跑中途撤销、文稿 409。不重复派。
- 「运行该节点」只有 live/视觉用例点过按钮（缺绑定资产报错），没有 mock 文稿权威断言 `scope=node` 且 authored live 不被盖。
- 「运行到这里」HTTP 具名钉死见 C3 `TestToNodeAndSelectionAfterAuthoredPromptKeepLive`；C4 未点 `data-graph-inspector-run-to-node`。
- 「保存后取消整图跑」C3 HTTP 见 `TestCancelGraphRunKeepsMidRunInspectorSave`；C4 未点取消。检查器取消仅在该节点有 `running` node-run 时出现；`withPausedGraphWorker` 下 run 常为 `queued`，工具栏「取消排队」是同一条「运行尚未结束时取消」合同的稳定路径。
- 不派运行此场景、失败重试、交付图。

## 做成什么样

`just web-e2e-canvas-document` 新增三条 mock 浏览器用例，且现有五条必须仍过：

1. **运行该节点**：检查器手填提示词后点 `data-graph-inspector-run-node`。POST body 为 `scope=node` 且带该节点 `node_id`，禁止改成 `graph` 或加 `force`。跑完后 live 仍是手填稿（O3：无 force 不得把 provider 结果写进 authored live）。
2. **运行到这里**：手填后选中下游生图节点，点 `data-graph-inspector-run-to-node`。POST body `scope=to_node` 且 `node_id` 为该生图节点。跑完后提示词 live 仍是手填稿，prompt provider 不得盖掉。
3. **运行中取消**：手填后点 `data-graph-run-all`（`scope=graph`）。在该次 run 仍为 `running` 或 `queued` 时取消：queued 点工具栏 `data-graph-run-queue` 的「取消排队」；running 且检查器对该节点给出取消时点检查器「取消」。取消当时 run 仍 in-flight 或刚被取消请求命中。最终 live 仍是手填稿，mock 生成不得盖 live。Mock 太快时用 `withPausedGraphWorker`，不得改成跑完再取消。

裁判是 live 文稿、origin 与 run scope，不是文采。prompt/image 必须 mock。

## 前置与并行

- 前置：已起的 `just dev`。
- 冻结输入：不改 AR-01 草稿基线、不改 C0–C3 cook/adopt 合同来让跳过或取消消失。
- 运行资源：临时切 mock，取消条可能暂停 `productflow-worker`。不得与 [image-eval-pool.md](../image-eval-pool.md) 的 `image-evals-run` 或真实生图/Agent live 同时切 mock、暂停 worker。image-eval-pool 当前 live 未跑；eval-skills 阻塞于题库校正、L1 用独立 checkout。不得覆盖 `STORAGE_ROOT/image-evals/`。

## 只改这些文件

- `web/e2e/canvas-document-mock.spec.ts`
- 本文件

现有源码若浏览器路径确实违背合同，允许修复下列路径；没有因果需要的文件不动：

- `web/src/pages/workbench/canvas/GraphNodeInspector.tsx` 的运行/取消按钮（含稳定 data 属性）
- `web/src/pages/workbench/canvas/GraphCanvasPanel.tsx` 仅当排队取消无法点到
- 上述模块相邻的直接回归测试

## 不要碰

- `go/internal/graph/` 权威测试与 cook 实现
- `just web-e2e-live-graph`
- Agent 评测、Skill、图片池
- 把 `scope=node` / `to_node` 改成 `graph` 来躲开断言
- 把取消改成跑完再点
- 运行此场景、失败重试、交付图

## 现在代码在哪

- 检查器：`submitInspectorRun({ scope: "node" | "to_node", node_id })`；`data-graph-inspector-run-node` / `data-graph-inspector-run-to-node`。
- 检查器取消：`activeRun` 要求整图 run 为 `running` 且该节点 node-run 为 live 状态；`data-graph-inspector-cancel`；文案 `detail.cancel`。
- 排队取消：`GraphCanvasPanel` 对 `queuedRuns` 渲染 `graph.runs.cancelQueued`。
- C3：`TestToNodeAndSelectionAfterAuthoredPromptKeepLive`、`TestCancelGraphRunKeepsMidRunInspectorSave`。
- C4：`web/e2e/canvas-document-mock.spec.ts`（8 条）。

## 合同

- 点运行该节点就 `scope=node`；点运行到这里就 `scope=to_node`；点整图就 `scope=graph`。
- O2/O3：手填 live 不得被本次 cook 的 provider 结果盖掉。
- 运行尚未结束时取消必须真正发出 cancel，最终 live 保持手填稿。
- 供应商绑定必须是 mock。

## 怎么验收

```bash
just web-e2e-canvas-document
```

新用例须断言 POST scope 与 node_id（前两条）或取消当时 run 仍为 queued/running（第三条）。现有五条继续通过。源码若有改动，另跑与改动边界对应的 Vitest / eslint。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：主代理-0905-1453
- 交接：已关闭。mock 窗口与暂停 worker 已结束。未改 dispatcher / Skill / 图片池。

## 证据

```text
测试：inspector run-this-node keeps authored live copy；inspector run-to-here keeps authored prompt live copy；mid-run cancel keeps authored live copy
夹具：手填后点 data-graph-inspector-run-node / run-to-node；withPausedGraphWorker 后 data-graph-run-all，queued 点取消排队或 running 点检查器取消。
断言：POST scope=node|to_node 且带 node_id、无 force；取消后 status=cancelled；live 仍为手填稿 origin=authored。
实现：检查器取消补 data-graph-inspector-cancel。未改 cook/adopt。
命令：just web-e2e-canvas-document
日期 / 结果：2026-09-05 Chromium 8 passed (1.1m)
eslint：GraphNodeInspector.tsx / canvas-document-mock.spec.ts 通过
just docs-check：pass
审核者 / 结论：主代理-0905-1453 自审通过，非独立审核。
Issue 结果：完成。C4 检查器运行该节点、运行到这里与运行中取消已进 just web-e2e-canvas-document。
业务门槛结果：用法矩阵对应三行完成。运行此场景、失败重试、交付图未纳入本单。
```

- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/canvas-c4-run-controls.md` 查询）。
- 审核者 / 结论（自审须注明）：主代理-0905-1453 自审通过，非独立审核。
- Issue 结果 / 业务门槛结果 / 剩余缺口：完成。USER_GUIDE §3.4 点名的检查器运行/到此/取消已有 C4 证据。不拆运行此场景或失败重试。
