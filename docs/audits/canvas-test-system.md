# 工作流体验组与画布文稿权威账本

本账本管理 schema-v3 画布在 cook、Inspector 保存、候选审阅、undo 与 Agent 写入交错时的文稿权威合同。它衡量「AI 生成会不会盖掉用户已发布文稿」，不替代 [`agent-eval-system.md`](agent-eval-system.md) 的模型行为评测，也不替代 [`performance-governance.md#production-gates`](performance-governance.md#production-gates) 的 Agent 生产 Gate。

**工作流体验组章程。执行以已发布 issue 为界。** 接收原画布职责与架构 AR-01，当前交付聚焦编辑保存链；产品创建、图操作、生成结果与资产使用问题按实际合同发布，不因改组自动扩大实现范围。C0–C6 已完成。当前任务与认领见 [Issue 看板](tasks/README.md)。C3 整图跑中途撤销具名钉死已归档为 [canvas-graph-run-undo](tasks/archive/canvas-graph-run-undo.md)；C4 浏览器整图撤销与文稿 409 停止已归档为 [canvas-c4-remainder](tasks/archive/canvas-c4-remainder.md)；检查器运行该节点、运行到这里与运行中取消已归档为 [canvas-c4-run-controls](tasks/archive/canvas-c4-run-controls.md)，不重复派版本语义修复。

## 组职责与交接

- 负责商家从编辑、运行到使用商品素材的业务行为，保留关闭 Agent 后仍可独立操作的合同。组内按“操作复现 → 必要前后端修复 → 浏览器与业务状态验收”串行交付；使用固定 Graph/provider 合同，不依赖模型涨分。
- 图片质量差距及相应生成链优化由图片质量组完整交付；本组的保存/运行/资产操作缺陷由本组完整修复。两个目标遇到同一根因时由协调者指定一张主任务，不按代码目录重复接单或中途转交。
- AR-01 与 C4 空闲改写/运行中打字由 [canvas-inspector-midrun](tasks/archive/canvas-inspector-midrun.md) 交付。2026-09-05 检查器草稿基线沿 autosave → Inspector → Surface → `commitNode` 传到 Graph Command；兄弟节点配置变更不再把未改节点的脏草稿标冲突。mock 浏览器门另覆盖整图跑中途撤销与文稿 409 停止，见 [canvas-c4-remainder](tasks/archive/canvas-c4-remainder.md)。
- AR-01-A：真实编辑基线贯穿 autosave、Inspector、Surface、Canvas 到 Graph Command；仅兄弟节点配置变更可由服务端安全 rebase，不以最新 revision 遮蔽过期编辑。
- AR-01-B：同节点变更仍拒绝过期保存；409 保留草稿、提示冲突并停止自动重放，不以当前值相等绕过历史裁定。
- AR-01-C：保存串行，失败阻止运行，较早响应不能清掉保存期间的新输入。AR-01-D：实际 run 仍 in-flight 时手填保存，运行完成后 live 保持用户稿，按 O2/O3 验证。
- 以上四条与 C4 分别记录覆盖证据，由本组完成验收；局部修复足够时不强制新模块。固定 journal/lease 合同继续适用，普通 Agent 行为由 Agent 质量组负责；本组必要的跨层修改和测试纳入同一交付序列，不等待其它组逐层实现。

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
| C-03 | C3 回调插入写 | Mock provider 回调里发一次 HTTP ChangeSet（开跑时的 `base_graph_revision`，不重试 409）。整图跑中途改本节点 / 兄弟文稿 / 视觉 overlay / undo / 保存后取消。整图跑期间点改写必须排队且不得盖 live。同节点已采用后一次过期保存 409。`to_node` 与 `selection` 在手填提示词后不得盖 live。cook 协程不得 `t.Fatal` | `完成` | `authority_inject_test.go`；`TestAdoptSkipsOverwriteWhenUserEditsDuringRun`；`TestAdoptSkipsOverwriteWhenVisualEditedDuringGraphRun`；`TestAdoptSkipsOverwriteWhenUndoDuringBriefCook`；`TestAdoptSkipsOverwriteWhenUndoDuringGraphRun`；`TestCancelGraphRunKeepsMidRunInspectorSave`；`TestRewriteQueuedDuringGraphRunKeepsAuthoredLive`；`TestInspectorSaveAfterSameNodeAdoptConflicts`；`TestToNodeAndSelectionAfterAuthoredPromptKeepLive` |
| C-04 | C4 浏览器 + mock 供应商 | 空闲时在检查器手填提示词，再点改写/补全/替换与按 section 应用。整图或节点 cook **正在跑**时检查器打字或工具栏撤销，adopt 不得盖 live（O2）。文稿保存 409 提示 `graph.canvas.revisionConflict` 且不重放。检查器运行该节点 / 运行到这里提交对应 scope，运行尚未结束时取消保持手填稿 | `完成` | `web/e2e/canvas-document-mock.spec.ts`、`just web-e2e-canvas-document`；2026-09-05 Chromium 8 passed（门禁临时切 mock 后恢复）。指导 [canvas-inspector-midrun](tasks/archive/canvas-inspector-midrun.md)、[canvas-c4-remainder](tasks/archive/canvas-c4-remainder.md)、[canvas-c4-run-controls](tasks/archive/canvas-c4-run-controls.md) |
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
| 整图或节点 cook 进行中，检查器打字保存 | C4 | 完成（`mid-run inspector typing keeps live authored copy after graph adopt`） |
| 整图跑中途撤销 | C3 HTTP 完成（`TestAdoptSkipsOverwriteWhenUndoDuringGraphRun`）；C4 浏览器完成（`mid-run undo keeps reverted live copy after graph adopt`） | 完成 |
| 浏览器看到文稿 409 后停止 | C3 有 409 断言；C4 Playwright 完成（`document save 409 shows revision conflict and does not replay`） | 完成 |
| 检查器运行该节点 | C4 完成（`inspector run-this-node keeps authored live copy`） | 完成 |
| 检查器运行到这里 | C3 HTTP 完成（`TestToNodeAndSelectionAfterAuthoredPromptKeepLive`）；C4 浏览器完成（`inspector run-to-here keeps authored prompt live copy`） | 完成 |
| 运行中取消 | C3 HTTP 完成（`TestCancelGraphRunKeepsMidRunInspectorSave`）；C4 浏览器完成（`mid-run cancel keeps authored live copy`） | 完成 |

当前实现事实：`ProductWorkbenchSurface` 传给检查器的 `busy` 是结构保存，不是 GraphRun，运行中输入框与撤销按钮按理可操作。检查器草稿基线沿 autosave → Inspector → Surface → `commitNode` 传递；兄弟节点配置变更不再把未改节点的脏草稿标冲突。文稿 `commitNode` 遇 409 提示 `graph.canvas.revisionConflict` 且不重放；`executeApply` 仅纯 `move_nodes` 自动重放。Mock 瞬时返回，自动保存 debounce 700ms。

## 完整操作链验收

2026-09-05 用户授权本组负责完整落地。以下核查基线为 `1dccfb59a09802eab194ee0d568079c9c4c8b8e5`，目标 Web/Graph/Delivery/Product/Recipe 文件无在途 diff；Agent、图片池和 imagesession 的其它会话改动不纳入本组。核查记录见 [canvas-workflow-coverage](tasks/archive/canvas-workflow-coverage.md)。

文稿权威 C0-C6 保持已交付结论。下表的「部分完成」表示已有实现或分层测试，尚未收齐该完整操作链的浏览器与业务状态证据，不直接判定产品有 bug。既有浏览器文件本轮只核对代码，未重跑。

| 用户操作 | 当前入口与业务效果 | 当前证据及边界 | 状态 |
|---|---|---|---|
| 只建画布并上传参考 | 创建表单 → `POST /api/v3/products` → 商品、资产与 schema-v3 图事务创建 | `product/http_test.go`；`direct-create-full-graph.spec.ts` 浏览器创建、整图成功、图库图片和真实 bytes；本轮未重跑真实 provider | 完成（既有 C5） |
| 添加、连线、复制、分组、撤销重做 | `GraphCanvasPanel` → Graph ChangeSet → 持久图与操作组 | `workbench-v3-actions.spec.ts` 检查添加六类节点、场景、删边、复制内部边、分组与 undo/redo 的持久结果；现有合同不重建 | 部分完成（待完整操作链复跑） |
| 检查器保存与运行交错 | autosave → Inspector → Surface → `commitNode` → Graph Command | AR-01、C0-C4、C6 的已归档证据 | 完成 |
| 运行此场景 | `GraphShotFilmstrip` / 画布分组 → `shotRunRequest` → 一次 `selection`，只含该组生图节点；保存 flush 后提交 | `shotChangeSet.test.ts` 验 node_ids；`select_test.go` 验 selection；Filmstrip 测试仅静态渲染，e2e 没有点击运行此场景后的业务断言 | 部分完成 |
| 失败后修正并重试、仅重试失败节点 | Inspector / RunsPanel → `/runs/:id/retry` 或 `selection` → 按当前图重新提交；原 run 保留 | `runs.go:retryGraphRun` 复用原 scope/target/force/document action；`graphRunPreview.test.ts` 验失败目标集合；`workbench-v3-actions.spec.ts` 的 can retry 用例只验按钮可见，没有点击 | 部分完成 |
| 结果预览与原图下载 | GraphRunsPanel / ProductImageExplorer → 明确资产预览和 download URL | C5 已取真实生成 bytes；图库 HTTP 覆盖 ZIP 与归属；未找到浏览器点击下载并核文件的用例 | 部分完成 |
| 交付规格、生成交付图和交付包 | `DeliveryRenditionPanel` → rendition job → 确定性渲染；Explorer → delivery export | `delivery/http_test.go` 验提交、幂等与结果 lineage；`export_archive_test.go` 验 manifest、SHA256 与完整/部分导出；Web 仅规格/选择/按钮测试，无该链浏览器用例 | 部分完成 |
| 绑定、拖入参考与固定当前结果 | Explorer / Canvas → 明确 asset id 的 ChangeSet；固定结果创建独立 image_asset，不自动连边 | `graphAssetDrop.test.ts` 验操作计划；actions 浏览器验绑定但不连线显示 unused；固定结果、拖入端口后的持久身份未有完整浏览器证据 | 部分完成 |
| 保存配方、预览并确认应用 | `recipeSave` / `RecipeLibraryPanel` → `/recipes` → Graph Command | `recipe/http_test.go` 验 fragment 保存/预览/应用及 full 冲突；现有 proof/actions 浏览器到确认预览或取消为止，未确认后核新图/合并图 | 部分完成 |

本轮确定性回归：`pnpm --dir web test:run src/pages/product-create src/pages/workbench/canvas src/pages/workbench/chrome/image-explorer`，39 files / 274 tests passed（2026-09-05 16:48:49，1.36s）。本轮未执行 Go PG 回归、浏览器或真实 provider；不以单元测试通过替代这些门槛。

### 后续交付次序

1. [场景运行与失败恢复](tasks/canvas-run-recovery-proof.md)：真实点击、一次正确 scope/target、修正后成功、非目标与手填文稿不变。优先级 P1，覆盖用户继续生产的关键动作。
2. [结果交付](tasks/canvas-delivery-proof.md)：选规格、产物尺寸/格式/源图身份、下载文件、ZIP manifest 与资产 lineage。优先级 P1，验收商家能拿到可使用的文件。
3. [资产复用与配方确认](tasks/canvas-asset-recipe-proof.md)：绑定、固定当前结果、片段确认合并及完整配方冲突；复跑既有图编辑动作。优先级 P2，不重复实现编辑器。

以上为测量与交付缺口，根因未证实前不预设需要改生产代码。mock/live 必须使用隔离环境或已确认的共享 provider/worker 窗口；不能覆盖图片组资源。路线图中的新交互设计不自动成为本批次实现要求。

## 明确不做

- 无界 12-op DFS
- 用真模型当搜索步进
- 把视觉矩阵、性能 e2e、Agent L1 桩世界并进本文稿权威门
