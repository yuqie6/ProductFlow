# 任务：运行中检查器保存与编辑基线贯通（C4 / AR-01）

状态：完成
类型：实现
认领者：主代理-0905-0438
认领于：2026-09-05T04:39:28+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：canvas-c4-remainder.md（维护者扣除本单已覆盖项后核对整图跑中途撤销与浏览器 409 剩余缺口，不重复发版本语义修复）

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，业务组结论见[父账本](../../canvas-test-system.md)。

## 前置与并行

- 前置：已起的 `just dev` 与对应浏览器测试环境。
- 冻结输入：测试期间固定画布代码与 mock 配置。
- 运行资源：本测试临时切换 provider，可能暂停 worker。共享 dev 时必须独占该窗口；不得与生图或 Agent live、dispatcher 压测同时运行，结束后恢复原 provider 与 worker 状态并登记。

## 做成什么样

`just web-e2e-canvas-document` 覆盖：用户点工具栏整图运行（`data-graph-run-all`，`scope=graph`）或检查器「运行该节点」之后，**该次 run 仍为 `running` 或 `queued`** 时，在检查器里打字并保存。保存后的 live 文稿在 adopt 时不得被生成结果盖掉（O2）。

空闲手填再点改写/补全/替换已经有了。本任务补**运行中**打字，并接收原 AR-01 的草稿编辑基线合同，由工作流体验组统一交付与验收。

裁判是 `document_origin` / `pending_candidate_artifact_id` / live `config_json`，不是文稿文采。prompt 与 image 必须是 mock。

## 只改这些文件

- `web/e2e/canvas-document-mock.spec.ts`
- 本文件

现有源码已显示基线传递缺口。按真实保存链允许修复下列路径；没有因果需要的文件不动，认领时对本清单统一检查独占：

- `web/src/pages/workbench/canvas/GraphNodeInspector.tsx`
- `web/src/pages/workbench/canvas/useNodeDraftAutosave.ts`
- `web/src/pages/workbench/agent/ProductWorkbenchSurface.tsx` 的检查器保存转交
- `web/src/pages/workbench/canvas/GraphCanvasPanel.tsx` 的 `commitNode` 输入、版本传递与运行前保存
- 上述模块相邻的直接回归测试；允许为实际缺口新增相邻测试，不扩大到其它交互重构

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

保存链为 `useNodeDraftAutosave.save(snapshot, editVersion) -> GraphNodeInspector.persist/onCommit -> ProductWorkbenchSurface -> GraphCanvasPanel.commitNode -> Graph Command`。Inspector 的三处 save 接线只接 draft，基线未传到底；`commitNode` 使用发送时 `graphRef.current.revision`。`go/internal/graph/stale_rebase.go` 仅允许完整历史全部为其它节点配置变更的纯 `update_node_config` 请求 rebase；混合操作、同节点变更和历史缺口仍冲突。

`MockPromptProvider` 立即返回。真实 LLM 下打字窗口以秒计。用例必须证明保存发生时 `GET .../runs` 的 `items[0].status` 仍是 `running` 或 `queued`。mock 太快看不到 in-flight 时，夹具可以在点运行之后、保存完成之前让 worker 先等着（例如暂停 `productflow-worker`，保存后再继续）；不得改断言。

C3 `authority_inject_test.go` 在 cook 回调里发一次 HTTP ChangeSet，不代替本层 Playwright。

## 合同

- 用户点整图就 `scope=graph`；`complete|rewrite|replace` 必须 `scope=node`+`force`。
- live 相对 run snapshot 已分叉 → 生成进 `pending_candidate`，不改 live config（O2）。origin 已是 authored 时无 `force` 的 graph 跑也不得把 provider 结果写进 live（O3）。两种结果都算守合同，live 必须仍是用户刚填的字。
- 同节点已被采用后的过期保存仍 409（O7）；仅兄弟 `update_node_config` 不得挡住这次检查器保存。
- AR-01-A：保存请求携带真实草稿基线，沿完整保存链传递；不能换成发送时最新 revision，也不能以内容相等代替服务端历史裁定。
- AR-01-B：同节点冲突保留用户草稿、提示失败、停止自动重放；服务端 Graph Command 继续拥有 rebase 决策。
- AR-01-C：自动保存串行；运行前 flush 失败不得创建 run；较早保存响应不能清掉保存期间的新输入。补贴近 hook 与调用边界的确定性回归。
- AR-01-D：浏览器保存当时 run 仍 in-flight，完成后 live 仍为用户稿，满足 O2/O3。四项与 C4 分别列证据，不以一条浏览器用例代替完整版本合同。
- 供应商绑定必须是 mock。需要已起的 `just dev`。

## 怎么验收

```bash
just web-e2e-canvas-document
```

新用例须在保存当时断言 run 仍为 `running` 或 `queued`，跑完后再断言 live 文稿与 `document_origin`。现有空闲改写/补全/替换两条必须仍过。

源码变动还须通过 `web/AGENTS.md` 的测试、lint、build，至少明确覆盖真实基线传递、兄弟配置更新可保存、同节点 409 保留草稿、保存中继续输入、失败阻止运行。Go rebase 合同只读复核，若真实根因越出本单范围交维护者裁定。

## 证据

```text
spec 用例名：
  rewrite stages a candidate and section apply keeps unselected fields
  complete and replace stage candidates without writing live config
  mid-run inspector typing keeps live authored copy after graph adopt
保存链实际修改路径 / 对应回归：
  useNodeDraftAutosave.ts：createNodeDraftSession / applyServerToNodeDraft / flushNodeDraft
  GraphNodeInspector persist 传递 expectedEditVersion；三处 editor.save 第二参
  ProductWorkbenchSurface inspectorSaveToCommitNode
  GraphCanvasPanel commitNode → buildNodeCommitChangeSet(baseGraphRevision)
  graphRunLock.submitAfterSuccessfulFlush
  回归：useNodeDraftAutosave.test.ts、graphChangeSetQueue.test.ts、graphRunLock.test.ts、
  GraphCanvasPanel.test.ts、GraphNodeInspector.test.ts、ProductWorkbenchSurface.test.ts
AR-01-A：inspectorSaveToCommitNode / buildNodeCommitChangeSet 使用草稿基线；兄弟 refetch 不改 editVersion
AR-01-B：409 保留草稿与原基线；commitNode 走 mutateAsync 不走 executeApply 重放
AR-01-C：flush 串行且保存中继续输入会再发后一次快照；flush 失败不 submit run
AR-01-D：mid-run 用例在 SIGSTOP productflow-worker 后 POST scope=graph，保存当时断言 run 为 running|queued，恢复 worker 后 live 仍为手填稿且 document_origin=authored
日期 / 结果：2026-09-05 自审通过。just web-e2e-canvas-document 3 passed（29.5s，临时 mock 后恢复）。pnpm --dir web test:run / lint / build 通过。测试期间暂停 worker 二进制 /tmp/go-build*/exe/productflow-worker，finally SIGCONT；采证后该进程仍在。
保存时 run status：用例断言 running|queued（worker 暂停后为 queued/running，未改断言）
审核者 / 结论：主代理-0905-0438 自审通过，非独立审核。
Issue 结果：完成。C4 空闲改写/补全/替换与运行中打字已进 mock 浏览器门。整图跑中途撤销与浏览器 409 交互未纳入本单 Playwright，留给维护者核对是否发 canvas-c4-remainder。
业务门槛结果：工作流体验 C-04 与 AR-01 本单合同已验收；不代表其它组门槛。
```
