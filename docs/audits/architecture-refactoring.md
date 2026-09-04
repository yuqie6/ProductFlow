# 架构重构业务组

本组处理有具体调用链证据的职责分散、版本语义外泄和测试难以穿过正常 interface 的问题。目标是让商家运行中编辑的内容可靠保存，让 Agent 对话事件的发布与确认合同更容易维护和验证。文件数量、文件长度或统一分层形式不作为验收目标。

组状态：开工。当前只发布调查任务；已有画布实现任务保持原归属。执行与认领见 [Issue 看板](tasks/README.md)，本章程不授权直接改业务代码。

## 来源与复核

- 用户指定报告：`/tmp/architecture-review-20260905-033823.html`，标题为「ProductFlow 架构深化候选」，原报告基线 `9c10b99e`。仓库相对路径 `tmp/architecture-review-20260905-033823.html` 不存在。
- 原报告 SHA-256：`9acacc7e8b755c84ed7ddaf641b780eb591e4eb40c488f8b8a05681e79a556bf`。临时 HTML 只作来源定位；本章程保留候选、源码锚点和判断限制，不要求执行者依赖该临时文件。
- 2026-09-05 建组复核：从 `25653846` 工作树读取两条调用链；期间另一个文档交付提交至 `37507921`，归档了性能组的 ImageSession 详情任务。本轮保留所有并行改动，未修改业务实现。
- 下面的“已复核”表示源码结构得到确认。浏览器交错行为、Go/PostgreSQL 故障场景和重构效果没有因此通过验收。

## 候选与当前裁定

| ID | 问题 | 当前证据与判断 | 执行入口 |
|---|---|---|---|
| AR-01 | 检查器草稿保存的版本语义分散 | 已复核：autosave 用整图 revision 判断脏草稿冲突；save 的预期版本没有沿保存回调传到底；最终提交读取最新 graph revision。存在明确的版本合同缺口，优先处理 | 复用画布组 [canvas-inspector-midrun](tasks/canvas-inspector-midrun.md)，不另发重叠实现单 |
| AR-02 | Agent journal 发布与 ACK 知识留在 TurnRuntime | 已复核：batcher 管排队与 drain；在线回执、重试、ACK 和恢复前缀仍由 TurnRuntime 解释；部分测试强转访问私有方法。尚未证明必须重构，也未发现新的数据丢失证据 | 新发 [arch-journal-assessment](tasks/arch-journal-assessment.md)，类型为证据 |

### AR-01：检查器草稿保存

当前调用链：

`检查器字段 -> useNodeDraftAutosave -> GraphNodeInspector.persist/onCommit -> ProductWorkbenchSurface -> GraphCanvasPanel.commitNode -> Graph Command -> 图 revision/操作历史 -> 图 refetch -> 草稿协调`

源码锚点：

- `web/src/pages/workbench/canvas/useNodeDraftAutosave.ts`：刷新 effect 在整图版本推进且草稿 dirty 时标失败；`flush` 调用 `save(snapshot, editVersionRef.current)`。
- `web/src/pages/workbench/canvas/GraphNodeInspector.tsx`：三处 autosave 接线未传递 save 的第二个参数；`persist` 将返回的 graph revision 包装为 `edit_version`，并注册运行前 flush。
- `web/src/pages/workbench/agent/ProductWorkbenchSurface.tsx`：`onCommit` 转交 `commitNode`，未携带草稿基线。
- `web/src/pages/workbench/canvas/GraphCanvasPanel.tsx`：`commitNode` 使用发送时的 `graphRef.current.revision`；`submitRun` 在 `onBeforeRun` 失败时不提交运行。
- `go/internal/graph/stale_rebase.go`：只有请求全部为 `update_node_config`，且基线后的完整历史全部是其它节点的配置变更，才允许服务端 rebase。混合操作、同节点修改、历史缺口、未来版本仍拒绝。

验收合同：

1. AR-01-A：仅兄弟节点配置变更时，本节点脏草稿可以携带真实编辑基线请求保存，由 Graph Command 裁定是否安全；不得以发送时的最新 revision 掩盖过期编辑。
2. AR-01-B：同节点变化不能静默覆盖；409 保留草稿、显示冲突并停止自动重放。不能只比较当前内容相等就跳过服务端历史判断。
3. AR-01-C：自动保存串行化；保存失败阻止运行；保存期间的新输入不能被较早的保存响应清掉。
4. AR-01-D：沿现有 mock-provider 浏览器 gate，在 run 仍为 `running` 或 `queued` 时完成手填保存，run 完成后 live config 仍为手填内容；以 `document_origin` / `pending_candidate_artifact_id` / live config 验证 O2/O3，不以静态渲染通过代替。

画布组继续拥有 C4/O2/O3/O7 的业务验收，架构组检查草稿基线是否贯穿完整保存链，以及是否仍有重复版本解释。当前画布 issue 的条件性实现范围只有 Inspector 和 autosave；若真实修复需改 `ProductWorkbenchSurface`、`GraphCanvasPanel` 或新增直接测试，执行者须交回维护者调整原 issue 合同与独占范围。不得另开重叠 writer，也不得为守旧文件清单在错误层修补。

局部修复满足合同且未留下实际重复知识时，AR-01 可据画布交付证据关闭，不强制增加 module。画布 issue 完成不自动证明 AR-01-A 至 AR-01-C 均已覆盖；维护者逐项核对后再裁定剩余切片。

### AR-02：journal 发布与确认

当前调用链：

`TurnRuntime -> TurnStore 本地 WAL -> JournalEventBatcher -> TurnRuntime.appendPublishedBatch -> ProductFlowClient -> Go AppendEvents -> PostgreSQL journal -> 回执校验 -> TurnStore ACK`

恢复链另从 `confirmPublishedPrefix` / `confirmMatchingUnpublishedPrefix` 进入 `ProductFlowClient.confirmTurnEvents`，对照 PostgreSQL 的已提交前缀后再推进本地 ACK；它与在线 append 的权限不同。

源码锚点为 `agent-service/src/turn-runtime.ts`、`journal-publisher.ts`、`runtime-journal.ts`、`store.ts`、`productflow.ts`，以及 `go/internal/agent/execution.go`、`event_confirm.go`、`http_internal.go`。已有 `pi-runtime.test.ts` 的错误回执与 5xx 用例创建 manager、强转取得 runtime、注入 lease 并调用私有 `appendPublishedBatch`，是可观察的测试维护成本。

必须保持的合同：

1. AR-02-A：PostgreSQL 是业务 journal 权威；本地 WAL 只确认可证明前缀。错误或不匹配回执不得推进 ACK；重试保留原 sequence 和事件内容。
2. AR-02-B：在线 append 需要有效 execution/owner/lease；confirmation 不产生 claim、不续租、不写业务终态。确认已落库与取得追加权限必须分别处理。
3. AR-02-C：保留分叉、fencing、barrier drain、响应丢失及部分前缀确认语义。Node 重启不重放丢失的模型执行；丢失执行的业务终态继续由 Go lease-expiry scanner 裁定。
4. AR-02-D：TurnRuntime 保留 lease 生命周期、等待输入/确认和终态编排。候选 module 只有确实吸收发布协议、减少调用方必知状态和测试私有侵入才值得新增或扩大；只搬 helper、增加转发层不满足门槛。

本阶段只评估；允许结论为保留现状。不得将可维护性候选写成已证实的持久化故障，也不得借本组重开已经关闭的运行时所有权计划。

## 与其它组的边界

- 画布组：复用现有运行中编辑 issue 和 mock-provider gate；源码写入范围需要扩展时由维护者在原任务上协调。
- 性能组：ImageSession 详情已有 [归档交付](tasks/archive/perf-imagesession-detail.md)，dispatcher 时延、容量指标和 admission 仍归 [性能章程](performance-governance.md)。本组不重复发单，不顺带修改查询、锁序或并发上限。
- 运行时所有权组与生产可靠性组：引用 [所有权验收](agent-runtime-ownership.md) 和 [可靠性合同](agent-production-readiness.md)，不更换 lease、journal 或终态权威。
- 评测与壳进化组：本组不修改 Skill、grader、工具能力或进化策略；未来 journal 实现若涉及共同 runtime 文件，认领前检查所有活跃 issue 的冻结输入和资源。

保留当前 `ConversationRuntime` 的重连、连续游标、补洞与多消费者共享职责；保留 Graph Command 的事务所有权。没有新的因果证据，不推广跨业务 recovery 框架，不统一所有状态机，不新增兼容路径、双 serializer 或旧数据迁移。

## 后续发布条件与组验收

- AR-01：由画布任务交付驱动复核；若其范围不足，维护者更新原任务。若交付后仍有独立、可举证的职责重复，才发布额外重构任务。
- AR-02：调查必须给出调用方知识清单、保留现状与收拢方案的比较、在线/恢复权限矩阵、可执行测试迁移方案和收益裁定。维护者接受收益与范围后才发实现 issue；调查完成允许不发实现单。
- 新候选必须附具体触发链、用户影响或维护成本、已有 owner、可删除的重复知识、测试入口和跨组排重结果。只凭热点/行数不发布。
- 实现任务按真实因果独占文件，逐片验证。前端变动通过 `web/AGENTS.md` 的 test/lint/build 和相关浏览器门；journal 变动通过 Agent 确定性与进程重启回归，跨 Go wire/权威时追加 Go/PostgreSQL 验证。
- 本组验收要求每个候选有经审核的“已交付 / 保留现状 / 明确未完成”结论及证据；建组、发布任务、测试基线绿灯均不算重构交付。实现 owner 变化后同步稳定架构文档，本阶段不把提案写成当前实现。

## 建组验证记录

2026-09-05 在上述工作树重跑原报告的两条确定性命令：

```bash
pnpm --dir web exec vitest run src/pages/workbench/canvas/GraphNodeInspector.test.ts src/pages/workbench/agent/conversation/runtime.test.ts
pnpm --dir agent-service exec vitest run src/journal-publisher.test.ts src/pi-runtime.test.ts -t 'mismatched event receipt|retries a 5xx journal batch|merges multiple events|failed transactional batch'
```

结果：Web 2 文件、36 条通过；Agent 2 文件、4 条通过、32 条因筛选跳过。未运行 Go/PostgreSQL、真实供应商、运行中检查器浏览器和进程重启验证。以上仅为建组复核基线，未认领或完成任何候选实施任务。
