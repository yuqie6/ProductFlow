# 画布文稿权威测试体系验收账本

本账本管理 schema-v3 画布在 cook、Inspector 保存、候选审阅、undo 与 Agent 写入交错时的文稿权威合同。它衡量「AI 生成会不会盖掉用户已发布文稿」，不替代 [`agent-eval-system.md`](agent-eval-system.md) 的模型行为评测，也不替代 [`agent-production-readiness.md`](agent-production-readiness.md) 的 Agent 生产 Gate。

## 来源与使用规则

- 来源：2026-09-05 会话中的《企业级画布测试体系》实施计划。
- 适用范围：`go/internal/graph` 文稿 cook / ChangeSet / 候选 API、有界动作搜索、运行中插入写、opt-in 浏览器 mock 文稿动作、Agent `apply_graph_change_set_v1` 与 GraphRun 交错。
- 裁判是不变量，不是模型文采。C0–C4 与 C6 使用 `MockPromptProvider` / `MockImageProvider`。真 LLM 只出现在既有 C5（`just web-e2e-live-graph`），不当覆盖判定。
- 本账本允许同时写目标合同、当前代码事实和缺口。当前能力写回 [`../ARCHITECTURE.md`](../ARCHITECTURE.md) 测试段；未接线层由 [`../ROADMAP.md`](../ROADMAP.md) 索引。

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
| C-03 | C3 回调插入写 | Mock provider 回调里发 HTTP ChangeSet（改本节点 / 改兄弟 / undo）。整图跑中途插入写；cook 协程不得 `t.Fatal`；并行 adopt 顶 revision 时按 409 重试 | `完成` | `authority_inject_test.go`；O2 钉在 `TestAdoptSkipsOverwriteWhenUserEditsDuringRun`（`scope=graph`） |
| C-04 | C4 浏览器 + mock 供应商 | Playwright 点改写/补全/替换与按 section 应用；prompt/image 必须为 mock | `完成` | `web/e2e/canvas-document-mock.spec.ts`、`just web-e2e-canvas-document`；2026-09-05 本机 Chromium 2 passed（prompt/image 临时 mock，跑完已恢复 openai） |
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
| O7 | ChangeSet 过期 `base_graph_revision` 必须 409 | C2 `stale`；`TestChangeSetStaleRevisionConflict` |

## 命令

```bash
just go-test
just go-test-canvas-search
just web-e2e-canvas-document
just web-e2e-live-graph
just docs-check
```

C2 加深：`PRODUCTFLOW_CANVAS_SEARCH_WALKS` 默认 8；`just go-test-canvas-search` 设为 80。

## 明确不做

- 无界 12-op DFS
- 用真模型当搜索步进
- 把视觉矩阵、性能 e2e、Agent L1 桩世界并进本文稿权威门
