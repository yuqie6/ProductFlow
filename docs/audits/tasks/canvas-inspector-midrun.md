# 任务：运行中检查器打字（画布 C4 尾项）

状态：开放
认领者：—
认领于：—
业务组：画布
父账本：canvas-test-system.md
完成后可拆：canvas-c4-remainder.md（整图跑中途撤销 + 浏览器文稿 409 即停；仍只动 canvas-document-mock.spec.ts）

读完本文件就可以改代码。认领前不要改「只改这些文件」。认领步骤见 [README.md](README.md)。

## 做成什么样

`just web-e2e-canvas-document` 覆盖：用户点工具栏整图运行（`data-graph-run-all`，`scope=graph`）或检查器「运行该节点」之后，**该次 run 仍为 `running` 或 `queued`** 时，在检查器里打字并保存。保存后的 live 文稿在 adopt 时不得被生成结果盖掉（O2）。

空闲手填再点改写/补全/替换已经有了。本任务只补**运行中**打字。

裁判是 `document_origin` / `pending_candidate_artifact_id` / live `config_json`，不是文稿文采。prompt 与 image 必须是 mock。

## 只改这些文件

- `web/e2e/canvas-document-mock.spec.ts`
- 本文件

检查器字段已有 label，优先只改 spec。若运行中输入被禁用，或打字后保存发不出去 / live 被生成盖掉，才允许改：

- `web/src/pages/workbench/canvas/GraphNodeInspector.tsx`
- `web/src/pages/workbench/canvas/useNodeDraftAutosave.ts`

不要改 Go 文稿 cook / 搜索器。

## 不要碰

- `go/internal/graph/` 权威测试与 cook 实现（C0–C3、C6 已完成）
- `just web-e2e-live-graph`（真模型整图，不覆盖 rewrite）
- Agent 评测、Skill
- 把用例改成 `waitForGraphRunSucceeded` 之后再 `fill`（那是空闲保存，不是本任务）
- 把 `scope=graph` 缩成 `scope=node` 来躲开挂起或 409
- 循环重试 409（浏览器文稿保存 409 不自动重放）

## 现在代码在哪

`web/e2e/canvas-document-mock.spec.ts`：`authorPrompt` 在空闲画布用 `getByLabel` 填「设计目标」「商品占比（%）」「布局」，再点改写/补全/替换。`withMockDocumentProviders` 临时切 mock 后恢复。没有在 run 仍为 in-flight 时打字。

`ProductWorkbenchSurface` 把 `canvasBusy` 设为结构保存中（`onBusyChange(structureBusy)`），不是 GraphRun。检查器字段 `disabled={busy}`，整图跑期间输入框按理可编辑。运行按钮在 `runMutation.isPending` 时禁用。

`useNodeDraftAutosave`：debounce 700ms；`serverEditVersion` 传入整图 `revision`。兄弟节点采用触发图 refetch 时，当前节点 `config_json` 没变也可能把脏草稿标冲突，保存发不出去。服务端 O7 已能 rebase 一次落后的 `update_node_config`；缺口在检查器草稿。`GraphCanvasPanel.executeApply`：文稿 ChangeSet 409 只提示 `graph.canvas.revisionConflict`，不自动重放。

`MockPromptProvider` 立即返回。真实 LLM 下打字窗口以秒计。用例必须证明保存发生时 `GET .../runs` 的 `items[0].status` 仍是 `running` 或 `queued`。mock 太快看不到 in-flight 时，夹具可以在点运行之后、保存完成之前让 worker 先等着（例如暂停 `productflow-worker`，保存后再继续）；不得改断言。

C3 `authority_inject_test.go` 在 cook 回调里发一次 HTTP ChangeSet，不代替本层 Playwright。

## 合同

- 用户点整图就 `scope=graph`；`complete|rewrite|replace` 必须 `scope=node`+`force`。
- live 相对 run snapshot 已分叉 → 生成进 `pending_candidate`，不改 live config（O2）。origin 已是 authored 时无 `force` 的 graph 跑也不得把 provider 结果写进 live（O3）。两种结果都算守合同，live 必须仍是用户刚填的字。
- 同节点已被采用后的过期保存仍 409（O7）；仅兄弟 `update_node_config` 不得挡住这次检查器保存。
- 供应商绑定必须是 mock。需要已起的 `just dev`。

## 怎么验收

```bash
just web-e2e-canvas-document
```

新用例须在保存当时断言 run 仍为 `running` 或 `queued`，跑完后再断言 live 文稿与 `document_origin`。现有空闲改写/补全/替换两条必须仍过。

## 证据

```text
spec 用例名：
是否改了 GraphNodeInspector / useNodeDraftAutosave：
日期 / 结果：
保存时 run status：
```
