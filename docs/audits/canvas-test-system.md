# 画布文稿权威测试体系验收账本

本账本管理 schema-v3 画布在 cook、Inspector 保存、候选审阅、undo 与 Agent 写入交错时的文稿权威合同。它衡量「AI 生成会不会盖掉用户已发布文稿」，不替代 [`agent-eval-system.md`](agent-eval-system.md) 的模型行为评测，也不替代 [`agent-production-readiness.md`](agent-production-readiness.md) 的 Agent 生产 Gate。

**画布组章程。不要从本文件开工。** C0–C3、C5、C6 已完成。C4 空闲改写/候选已进门禁。当前开放：[`tasks/canvas-inspector-midrun.md`](tasks/canvas-inspector-midrun.md)。下一刀 `canvas-c4-remainder` 等该任务归档再发。

## 来源与使用规则

- 来源：2026-09-05 会话中的《企业级画布测试体系》实施计划。
- 适用范围：`go/internal/graph` 文稿 cook / ChangeSet / 候选 API、有界动作搜索、运行中插入写、opt-in 浏览器 mock 文稿动作、Agent `apply_graph_change_set_v1` 与 GraphRun 交错。
- 裁判是不变量，不是模型文采。C0–C4 与 C6 使用 `MockPromptProvider` / `MockImageProvider`。真 LLM 只出现在既有 C5（`just web-e2e-live-graph`），不当覆盖判定。
- 本账本允许同时写目标合同、当前代码事实和缺口。当前能力写回 [`../ARCHITECTURE.md`](../ARCHITECTURE.md) 测试段；未接线层由 [`../ROADMAP.md`](../ROADMAP.md) 索引。

### 测试纪律

文稿权威测试按检查器和运行按钮的真实路径写，不按当前实现改断言。

- C4 必须在检查器里打字或点检查器/工具栏按钮。禁止用 ChangeSet HTTP 代替手填。
- 用户点的是整图跑就提交 `scope=graph`。禁止改成 `scope=node` 来躲开挂起或 409。
- 禁止循环重试 409。浏览器文稿保存 409 只提示 `graph.canvas.revisionConflict`，仅纯 `move_nodes` 自动重放。
- `MockPromptProvider` 瞬时返回，不构成把「运行中保存」改成「跑完再填」的理由。保存落地时 run 仍须是 `running` 或 `queued`。

### 状态四值

| 状态 | 判定规则 |
|---|---|
| `完成` | 当前实现、贴近合同的自动化测试和条款要求的运行证据都存在。 |
| `部分完成` | 已有可执行实现或既有证据，但规模、opt-in 实测或文档对齐不全。 |
| `缺失` | 实现不存在，或当前证据不足以判断。 |
| `违背` | 当前实现明确采用冻结决策禁止的合同。 |

## 冻结决策

| ID | 决策 | 状态 | 证据 |
|---|---|---|---|
| D-01 | 裁判是 live `config_json` / `document_origin` / `pending_candidate_artifact_id` 与 node-run `disposition`，不是生成稿质量 | `完成` | oracle 在 `go/internal/graph/authority_search_test.go` |
| D-02 | 搜索动作集只含文稿权威动作；`move_nodes`、分组、配方、纯布局不进搜索器 | `完成` | 动作表见 C2 |
| D-03 | 发现与钉死分离：搜索 shrink 最短轨迹；稳定违法写成 `cook_contract_test.go` 具名测试 | `完成` | C1 钉死 + C2 shrink |
| D-04 | 夹具经 HTTP ChangeSet / runs / candidate + `executeLocally`；禁止 SQL 改 `config_json` | `完成` | C3 已替换 `TestAdoptSkipsOverwriteWhenUserEditsDuringRun` |
| D-05 | C2 进 `just go-test` 但有 walk/深度预算；更深搜索 `just go-test-canvas-search` | `完成` | `PRODUCTFLOW_CANVAS_SEARCH_WALKS` |
| D-06 | 不扩大 `just web-e2e-live-graph`。rewrite 浏览器路径是独立 mock gate | `完成` | `just web-e2e-canvas-document` |

## 文档对齐缺口

[ADR 0015](../adr/0015-canvas-ports-run-queue.md) 曾写：live 相对快照分叉时「生成 artifact 仍保留并成为 `current_artifact_id`」。live 执行器 [`execute_node.go`](../../go/internal/graph/execute_node.go) `persistContentArtifact` 把文稿建议写入 `pending_candidate_artifact_id`，并把 `current_artifact_id` 清掉；`current_artifact_id` 留给效果节点。本账本 oracle 跟代码。ADR 正文冻结，不在此改写成仪表盘。

## 层与验收

| ID | 层 | 验收 | 状态 | Owner / 证据 |
|---|---|---|---|---|
| C-00 | C0 单步合同 | origin、merge、section apply、何时 cook | `完成` | `document_test.go`、`document_candidate_test.go`、`select_test.go`、`ops_parse_test.go` |
| C-01 | C1 mock HTTP cook | `graphServer` + `Executor`；O4 在 apply 前 live 不变；O2 mid-run 不覆盖 | `完成` | `cook_contract_test.go`；`TestForceRewritePromptDoesNotChangeLiveUntilApply` |
| C-02 | C2 有界搜索 | 默认 8 条随机 walk（深度 ≤ 3）+ 6 类动作深度 2 穷举；失败 shrink；缺节点动作跳过 | `完成` | `authority_search_test.go`；`just go-test`；加深 `just go-test-canvas-search` |
| C-03 | C3 回调插入写 | Mock provider 回调里发一次 HTTP ChangeSet（开跑时的 `base_graph_revision`，不重试 409）。整图跑中途改本节点 / 兄弟文稿 / 视觉 overlay / undo / 保存后取消。整图跑期间点改写必须排队且不得盖 live。同节点已采用后一次过期保存 409。`to_node` 与 `selection` 在手填提示词后不得盖 live。cook 协程不得 `t.Fatal` | `完成` | `authority_inject_test.go`；`TestAdoptSkipsOverwriteWhenUserEditsDuringRun`；`TestAdoptSkipsOverwriteWhenVisualEditedDuringGraphRun`；`TestCancelGraphRunKeepsMidRunInspectorSave`；`TestRewriteQueuedDuringGraphRunKeepsAuthoredLive`；`TestInspectorSaveAfterSameNodeAdoptConflicts`；`TestToNodeAndSelectionAfterAuthoredPromptKeepLive` |
| C-04 | C4 浏览器 + mock 供应商 | 空闲时在检查器手填提示词，再点改写/补全/替换与按 section 应用。整图或节点 cook **正在跑**时检查器打字保存，adopt 不得盖 live（O2） | `部分完成` | 空闲路径：`web/e2e/canvas-document-mock.spec.ts`、`just web-e2e-canvas-document`；2026-09-05 Chromium 2 passed（门禁临时切 mock 后恢复）。运行中打字未进 Playwright，C3 HTTP 插入写不代替本层；指导 [`tasks/canvas-inspector-midrun.md`](tasks/canvas-inspector-midrun.md) |
| C-05 | C5 真 provider 整图 | skip-Agent 出一张真图；不覆盖 rewrite/候选 | `完成` | `just web-e2e-live-graph` → `direct-create-full-graph.spec.ts` |
| C-06 | C6 Agent × 画布 | `ApplyAgentChangeSet` / `apply_graph_change_set_v1` 与整图 GraphRun 交错仍守 O2/O3 | `完成` | `authority_agent_interleave_test.go`；`go/internal/agent/canvas_authority_interleave_test.go` |

## 不变量

| ID | 合同 | 钉死测试 |
|---|---|---|
| O1 | 无 `document_action`、snapshot origin=`seed`、live 未分叉 → cook 后 origin=`generated`，可 adopt | `TestGeneratedContentNodeRemainsReadyAfterAdopt` |
| O2 | live 相对 run snapshot 已分叉 → 不改 live config；生成进 `pending_candidate`；`disposition=candidate` | `TestAdoptSkipsOverwriteWhenUserEditsDuringRun` |
| O3 | origin 为 `authored\|generated\|collaborative` 时，无 `force` 的 graph 跑不得把 provider 结果写进 live config | `TestGraphRunAfterAuthoredLayoutEditSkipsPromptProvider`；C2 `run_graph` |
| O4 | `complete\|rewrite\|replace` 必须 `scope=node`+`force`；成功后 pending 非空，apply 前 live 不变；`graph`+`force` 4xx | `TestForceRewritePromptDoesNotChangeLiveUntilApply`；`TestSubmitGraphRunRejectsForce` |
| O5 | apply 前 live 哈希或 revision 变了 → apply 拒绝 | C2 `stale` / 先 `author` 再 `apply`；`ApplyDocumentCandidate` 409 |
| O6 | section apply 只改选中业务 section | `TestApplyDocumentSectionsOnlyReplacesSelectedBusinessSection`；C2 `apply_objective`；C4 |
| O7 | 同节点丢失更新必须 409；仅兄弟节点 `update_node_config` 时检查器/Agent 一次保存可 rebase | C2 `stale`（先写同节点再过期写）；`TestChangeSetStaleRevisionConflict`；`TestWriteTxRebasesStaleNodeConfigWhenSiblingChanged`；`TestWriteTxStaleNodeConfigConflictsWhenSameNodeChanged` |

## 命令

```bash
just go-test
just go-test-canvas-search
just web-e2e-canvas-document
just web-e2e-live-graph
just docs-check
```

C2 加深：`PRODUCTFLOW_CANVAS_SEARCH_WALKS` 默认 8；`just go-test-canvas-search` 设为 80。

## 真实用法矩阵

按钮以 USER_GUIDE §3.4 / 检查器为准：工具栏 `data-graph-run-all`（整图）、检查器「运行该节点」「运行到这里」、补全/改写/重新生成、字段手填、撤销、取消。裁判仍是 O1–O7。

| 组合 | 层 | 状态 |
|---|---|---|
| 空闲检查器手填 → 改写/补全/替换 → 按 section 应用 | C4 | 完成（`canvas-document-mock.spec.ts`） |
| 手填提示词后 `to_node` / `selection` | C3 | 完成（`TestToNodeAndSelectionAfterAuthoredPromptKeepLive`） |
| 整图跑中途 HTTP 改本节点 / 兄弟文稿 / 视觉 overlay | C3 | 完成（`authority_inject_test.go`，一次 PATCH，开跑时的 `base_graph_revision`） |
| 整图跑中途点改写 | C3 | 完成（排队且不盖 live） |
| 同节点已采用后一次过期保存 | C3 | 完成（409，不重试） |
| 保存后取消整图跑 | C3 | 完成 |
| Agent 整图跑中途写入 | C6 | 完成 |
| 节点 rewrite 中途 undo | C3 | 完成 |
| 整图或节点 cook 进行中，检查器打字保存 | C4 | 缺失 |
| 整图跑中途撤销 | — | 未单独覆盖（现有 undo 在节点 rewrite） |
| 浏览器看到文稿 409 后停止 | C3 有 409 断言；C4 无 | 部分 |

当前实现事实（C4 可能踩到）：`ProductWorkbenchSurface` 传给检查器的 `busy` 是结构保存，不是 GraphRun，运行中输入框按理可编辑。`useNodeDraftAutosave` 的 `serverEditVersion` 是整图 `revision`；兄弟采用导致 refetch 时，当前节点 config 没变也可能挡住保存。Mock 瞬时返回，自动保存 debounce 700ms。

## 明确不做

- 无界 12-op DFS
- 用真模型当搜索步进
- 把视觉矩阵、性能 e2e、Agent L1 桩世界并进本文稿权威门
