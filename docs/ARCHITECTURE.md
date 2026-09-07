# ProductFlow Architecture

## 1. 系统边界

ProductFlow 是单管理员、单商家工作区，由七个运行单元组成：

1. React/Vite Web。
2. Go 业务 API。
3. Go worker。
4. Go async dispatcher。
5. Node.js 22 + Pi SDK ProductFlow Agent service。
6. PostgreSQL。
7. Redis 与媒体 storage。

浏览器只访问 Web 和业务 API。Agent service 使用独立 bearer token 调用业务 API 的 internal 路由；API 通过 agent-service internal HTTP/SSE 控制 Turn。API、worker 和 async dispatcher 共享 PostgreSQL、Redis 和 storage。`just dev` 与 Docker Compose 都会启动 dispatcher。默认进程是 `go/cmd/productflow-api`、`productflow-worker`、`productflow-dispatcher`。schema 由 `productflow-migrate` 在启动前应用。退休的 FastAPI 树在 `retired/python`。

本文只描述当前实现。模块所有权来自当前源码树，行为证据来自对应测试；产品合同见 `PRD.md`。未过的耐久 gate 见 `ROADMAP.md`。

## 2. 后端分层

业务后端按功能竖切，代码在 `go/internal/`。HTTP 用 Gin，PostgreSQL 访问用 GORM（驱动仍是 pgx，命令事务走 `tx.WithGorm` 与 schema 模型 `Create`/`Updates`/`Take`，行锁走 `platform/db` locking clause），异步投递用 asynq 信封，状态权威仍是 PostgreSQL 的 `async_dispatches` 与业务表。schema 权威是 `productflow-migrate`：GORM `CreateTable`/`AddColumn` 加 ExtraDDL（CHECK / enum / 部分唯一索引 / FK）。不使用 AutoMigrate。写库约定见 [`go/AGENTS.md`](../go/AGENTS.md)。

机器可读合同在仓库根 `contracts/`：`http-routes.json` 与 `openapi.json` 是 2026-08-29 历史封印快照，默认不重生。Go 对未知 JSON 字段 `DisallowUnknownFields` → 400。HTTP 只写业务行和 `async_dispatches` PENDING，不在请求里打 broker。

当前代码所有权：

| 能力 | 包 | HTTP/进程 | 主要回归测试 |
|---|---|---|---|
| Agent 商品创建 | `go/internal/product`、`go/internal/agent` | `productflow-api` | `go/internal/product`、`go/internal/agent` |
| Agent Session 与 Task | `go/internal/agent` | `productflow-api` | `go/internal/agent/http_test.go` |
| Agent Turn 与同步 | `go/internal/agent` | `productflow-api`、`productflow-worker` | `go/internal/agent` |
| Agent 工具与上下文 | `go/internal/agent` | internal HTTP | `go/internal/agent/surface_test.go` |
| 全局素材库 | `go/internal/library` | `productflow-api` | `go/internal/library` |
| 全局素材整理 Draft | `go/internal/library` | `productflow-api` | `go/internal/library`、`go/internal/agent` |
| schema-v3 图与执行 | `go/internal/graph` | `productflow-api`、`productflow-worker` | `go/internal/graph` |
| 配方 | `go/internal/recipe` | `productflow-api` | `go/internal/recipe` |
| 视觉方案版本 | `go/internal/visualsystem` | `productflow-api` | `go/internal/visualsystem` |
| 交付图 | `go/internal/delivery` | `productflow-api`、`productflow-worker` | `go/internal/delivery` |
| 商品图片库 | `go/internal/product`、`go/internal/media` | `productflow-api` | `go/internal/product` |
| 连续生图 | `go/internal/imagesession` | `productflow-api`、`productflow-worker` | `go/internal/imagesession` |
| 局部修 | `go/internal/localedit` | `productflow-api`、`productflow-worker` | `go/internal/localedit` |
| 设置与 provider | `go/internal/settings`、`go/internal/providers` | `productflow-api`、worker 解析绑定 | `go/internal/settings`、`go/internal/providers` |
| 发给模型的固定文案 | `go/prompts` | API/worker embed；agent-service 构建时把 `runtime-policy.md` 打包进产物 | `go/prompts`、graph listing/prompt 测试、`go/internal/providers`、`go/internal/agent`、agent-service |
| 异步投递 | `go/internal/platform/queue` | `productflow-dispatcher`、`productflow-worker` | `go/internal/platform/queue`、graph/imagesession 投递测试 |
| schema 演进 | `go/internal/platform/db/schema` | `productflow-migrate` | `go/internal/platform/db/schema` |
| Agent 评测回流 | `go/internal/agent`、`go/cmd/productflow-agent-evals` | 只读 CLI | `go/internal/agent/evalmine_test.go`、`evaltask_test.go`、`evalworld_test.go` |
| 生图质量测评 | `go/internal/imageeval`、`go/cmd/productflow-image-evals` | 只读 CLI；opt-in live | `go/internal/imageeval` |
| 错误与日志 | `go/internal/platform/apperr`、`httpx`、`log` | 中间件与 worker | platform 与各包 HTTP 测试 |

dispatcher watch 模式有一个投递循环和五个独立恢复循环（Graph、ImageSession、Delivery、LocalEdit、Agent）。每域默认 10s cadence、域内批次串行，不等待其他域完成；满批恢复不立即续扫。取消时等待全部循环退出。one-shot 保留固定域顺序恢复后投递的合同。逐域批次日志携带 `domain/enqueued/unknown/has_more`，空批次为 Debug；耗时与错误由按域 recovery 指标记录。共享 PG 池仍为 16，独立调度不保证连接或 IO 饱和下的隔离。实现与回归：`go/cmd/productflow-dispatcher/main.go`、`coordinator.go`、`coordinator_test.go`。

ImageSession、Delivery、LocalEdit 恢复在发现候选时用 `FOR UPDATE SKIP LOCKED` 跳过持锁前缀；ImageSession 按 `created_at/id`，其余两域按 `updated_at/id` 排序，默认返回最多 26 个候选 ID（25 个处理项加 1 个探测项）。发现阶段短暂锁行，事务结束即释放；状态迁移另开逐任务事务重查，不持发现锁处理整个恢复批次。`HasMore` 只表示跳锁后的可选候选超出本批额度，不是全库积压计数。实现与回归：`go/internal/imagesession/`、`go/internal/delivery/`、`go/internal/localedit/` 各自的 `recovery.go` 与 `recovery_test.go`。

## 3. 前端结构

`web/src/App.tsx` 注册当前页面：

- `/login`
- `/home`
- `/products`
- `/products/new`
- `/products/new/agent`，只重定向到 `/products/new`
- `/products/:productId`
- `/image-chat`
- `/media-library`
- `/settings`
- `/help`

页面级代码位于 `web/src/pages/`。共享视觉组件位于 `web/src/components/`，HTTP client、DTO、i18n 和浏览器偏好位于 `web/src/lib/`。

商品工作台位于 `pages/workbench/`，按职责分成三组：

- `workbench/agent/`：页面编排、对话、SSE 事件、问题确认、图提案确认和 Goal 托管环。
- `workbench/canvas/`：当前 Graph 画布、节点详情、运行、配方和交付图。
- `workbench/chrome/`：画布 chrome、节点卡片、侧栏、快捷键和图片 Explorer。

依赖方向固定为 `agent -> canvas, chrome`，`canvas -> chrome`。`chrome` 不得引用 agent 或 canvas。

TanStack Query 管理服务端状态；局部表单、选择和画布交互使用 React state。`api.ts` 是浏览器 HTTP 的统一入口。

当前前端所有权：

| 能力 | Owner | 主要测试 |
|---|---|---|
| 首页导航与真实素材展示 | `HomePage.tsx`, `HomePage.css`, `public/home-showcase/` | web build, real browser viewport checks |
| Agent 创建表单 | `AgentProductCreatePage.tsx`, `pages/product-create/` | selection/form/workspace API tests |
| Agent 对话、SSE、Goal | `pages/workbench/agent/` | reducer, conversation assembler, event, Goal 与提案测试 |
| Graph 画布与详情 | `pages/workbench/canvas/` | graph catalog/layout/canvas, inspector, runs and rendition tests |
| 全局素材库与工作流子图库 | `MediaLibraryPage.tsx`, `workbench/canvas/WorkflowMediaLibraryPanel.tsx` | media library/application tests, web build |
| Global Agent Dock | `components/GlobalAgentDock.tsx` | `GlobalAgentDockComponents.test.ts` |
| 共享工作台与图片库 | `pages/workbench/chrome/` | shortcuts, interaction and image-explorer tests |
| HTTP 和 wire DTO | `lib/api.ts`, `lib/types.ts` | `lib/*Api.test.ts`, TypeScript build |
| 文/图生图 | `ImageChatPage.tsx`, `pages/image-chat/` | branching, sessionEvents tests |

## 4. Agent 创建链路

配方创建由 `pages/product-create/RecipeProductCreateForm.tsx` → `POST /api/v3/workflow-recipes/{recipe_id}/creation-preview` → `POST /api/v3/products/from-recipe` 承担。预览只计算无目标身份的配方结构与 digest；确认提交名称、参考图、说明、版本、digest 和 `Idempotency-Key`。`product.CreateFromRecipe` 在现有商品创建事务中复核预览，并用同一事务的 `recipe.Preview` / `Apply` 绑定新商品及 facts、写首张图与应用记录。完整配方仍拒绝覆盖已有图。商品的 `creation_idempotency_key` / `creation_request_hash` 成对且 key 唯一；同请求回放当前商品/图，不同请求冲突。媒体补偿在事务提交成功后释放，提交失败清理新文件。部署须运行 `just go-migrate` 补列及约束。测试：`go/internal/product/recipe_create_test.go`、`go/internal/recipe/http_test.go`、`go/internal/platform/db/schema/migrate_test.go`。

```text
product name (+ optional types and 1..6 uploads)
  -> Product + live schema-v3 graph + product-owned AgentSession + AgentConversation
  -> user first message
  -> ProductFlow submits Agent Turn
  -> agent-service / Pi SDK ProductFlow adapter
  -> finalize_product_intake_v1 expands a name-only birth graph, or apply / propose Graph Command
  -> product workbench
```

ProductFlow 拥有商品、图提案确认、WorkflowGraphRun 和 Web projection。Agent service 使用 Pi SDK 运行模型 loop，并在自己的数据根保存 session 文件；这些文件不是业务权威。PostgreSQL 保存 AgentSession、AgentTask、AgentConversation、Turn projection、PageContextSnapshot、问题状态、`LibraryOrganizationDraft` revision，以及全量 Turn journal `agent_turn_events`（合帧后的 `text.chunk` / `thinking.chunk` 也写入，连续 seq）。新 journal event 在同一 PG 事务内增量 fold 活动 Turn 状态和 `output_text` / `thinking_text` / `tool_steps_json` 列表摘要；Turn 读取和 start/resume worker 不从 Node session 快照刷新这些字段。浏览器对话 SSE 由 Go 鉴权，只读取 PG journal，并由 `projectTurnEvent` 译成 Turn / Item / approval 通知；agent-service 没有本地事件流端点。运行中和终态 Turn 都从同一 PG 游标回放。`awaiting_confirmation` 停在 PG 日志上等待 `approval/resolved`。Dock 列表与 lease 健康走 `GET /api/v2/agent-control/events`（跨进程用 PostgreSQL LISTEN/NOTIFY）。图运行节点事件走 `GET /api/v3/products/:id/workflows/:id/runs/:id/events`。文/图生图进度走 `GET /api/image-sessions/:id/events`。Turn 事件 `run_id` 与 Turn 投影 `harness_run_id` 使用 `go/internal/agent` 的 harness run 规则：绑 Task 用 Task run，否则用 Conversation run。

商品创建在一个业务事务中写入。不创建 onboarding Task，不自动提交开场 Turn。名称-only 的图含 `product_source`；表单齐了与直接创建使用同一套图模板（`graph.BuildDirectCreateTemplate`），落库集合不同：Agent 表单齐写入 Product intake、不设封面；直接创建（`POST /api/v3/products`）不写 intake、封面为第一张图、不建 Session。Agent 对话里的 `finalize_product_intake_v1` 走 `Product.ApplyIntake`：同一事务写 intake，并在 live 图不存在或恰好一个 `product_source` 时调用 `expandBirthGraphFromIntake`。已有其它节点则只更新 intake。工具返回 `graph_expanded`、`revision`、节点/组数量。`get_product_workflow_context_v1` 带 `birth_expandable`（intake 已写且图仍是 birth）。后续单步 `apply_graph_change_set_v1` / 多步 `propose_graph_change_set_v1` 的 `operations[].op` 必须是 Graph Command 封闭表，与 `go/internal/graph` `ops_parse` 和 agent-service TypeBox schema 对齐。`tool_contract_version` 是工具清单内容哈希（`TOOL_MANIFEST_VERSION`）。`POST /api/v2/products` 仍可创建带封面的商品而不写 live graph，空图稍后由 `POST /api/v3/products/{id}/workflows` 补。画布 Session 的 `product_id` 非空；全局 Dock 列表只含 `product_id` 为空的 Session。独立新建全局 Session 不要求名称；临时名称来自首条全局 Turn，人工重命名优先。全局 Agent 创建商品工作区会新开画布 Session，使用 `creation_idempotency_key` 和 `creation_request_hash` 做只读对账。

`GlobalAgentDock` 负责 Session/Task 列表、搜索、跳转和待确认整理 Draft，不拥有画布或 WorkflowGraphRun。全局素材整理只发布 `LibraryOrganizationDraft`；用户确认后由 ProductFlow 重新观察事实并应用。

全局素材工具使用独立 `LibraryAssetMetadata` 返回 revision、folder_id、tag_names、is_archived；商品图库 DTO 不变。`list_global_media_library_assets_v1` 默认隐藏归档素材，恢复前以 `include_archived=true` 读取；目录使用 folder_query / folders_after_id 独立有界分页，workflow_id 可返回该工作流标题、revision 及对本页素材的准确关联状态。确认事务核对素材 before/revision、目标目录及工作流预期状态，冲突整笔失败。实现与回归：`go/internal/agent/tools_assets.go`、`go/internal/library/organization_reads.go`、`drafts.go`、`go/internal/agent/library_read_contract_test.go`；生产读取修复与评测夹具重新冻结分任务验收。

主线承诺交互式 Turn、取消、问题回答、SSE 游标重连与分页补洞，以及 Pi session 上下文的跨进程加载。问题答案走原 Turn 的 Pi `answer` + `resume`，进入 `ask_user` toolResult；不新开 continuation Turn。每次模型请求写入 `agent_model_invocations`，最终 `assistant/message` 按 `model_request_id` 闭合状态、时延和 usage；Turn 终态原因写 `terminal_reason_code`。活动进程内的 `reconcile_then_retry` 工具在首次对账确认 `not_applied` 后只用同一幂等键重试一次；policy 查询、重试和二次对账只由 Go `effect_reconcile.go` 解释，Node 在 mutation 5xx 后只按 execution lease 和 `tool_call_id` 查询终态。主线不承诺模型请求原地恢复、后台 durable Task、完整多实例调度、跨进程 `not_applied` 自动重试或全量副作用对账。lease、fencing、`tool_steps` 白名单和 effect reconciliation 以 `go/internal/agent` 及其测试为准。

问题答案以 Turn 与 question ID 共同定位。`agent-service/src/question-resume.ts` 默认只读取最近一次 `question/requested` 的答案；恢复时绑定该 question ID 注入对应 tool result。`turn-runtime.ts` 在单个 TurnRuntime 内串行保存回答和超时跳过，防止并发重复写 `question/answered`。Go `execution.go` 在新问题 journal 投影时清除旧答案；`turns.go` 在 PG 原子更新条件中核对当前问题、等待状态和已有答案，拒绝不同答案覆盖。确定性两问、重复提交、旧问题迟到、第二问等待期间 SIGKILL 后恢复的证据见 `agent-service/src/pi-runtime.e2e.test.ts`、`go/internal/agent/journal_regression_test.go`、`http_test.go` 与 `question_resume_gopg_test.go`；真实模型是否继续追问仍取决于本轮行为。

实现入口：`go/internal/product` 与 `go/internal/agent`；Turn 控制走 Go Agent HTTP → agent-service `runtime-manager.ts` → `turn-runtime.ts` → `pi-runtime.ts`。`runtime-manager.ts` 拥有进程 queue、并发 admission 和 restart handoff 调度；`runtime-scope.ts` 校验 ProductFlow contract；`turn-runtime.ts` 拥有 lease 绑定、journal 生命周期、问题/终态编排；`runtime-journal.ts` 与 `journal-publisher.ts` 提供 journal wire/批量 ACK 边界；`tool-step-projection.ts` 提供有界 UI 摘要；`pi-runtime.ts` 保留单 Turn 的 Pi session/model drain/chunk subscription/Skill/Tool adapter。业务投影在 `go/internal/agent`；全局素材 Draft 在 `go/internal/library`。商品 `WorkflowDraft` HTTP 已删除，对应 URL 返回 404。商品 Goal 是显式 `AgentTask`：Turn 或 `WorkflowGraphRun` 结束不会把 Goal 标成完成；用户通过 `POST /api/v2/agent-tasks/{id}/complete` 完成。

领域壳是 `agent-service/harness/` 中的版本化工件：`src/harness.ts` 加载四段行为指令、固定权限摘要和空的 overlay/runtime-control 占位，按现有 canonical JSON 计算 SHA-256，并在模块启动时冻结为 `DEPLOYED_HARNESS`；`pi-runtime.ts` 把该对象的指令交给 Pi。权限与确认仍在打包的 `go/prompts/agent/runtime-policy.md`，摘要不匹配或未声明字段使加载失败。每个新模型请求的 `before_model_request` checkpoint 与 `agent_model_invocations.harness_hash` 在 Go 同一事务内登记身份；新请求缺失或使用非规范小写 SHA-256 会被拒绝，历史行保留 NULL，重放不能覆盖身份。现有 `/healthz` 和 Node L1/L3/L5 eval `run.json` 引用同一冻结 hash；Go L2 从实际启动的 Pi 健康端点取值。基础 Skill hash 与 provider/model 保持独立归因。候选进化、谱系和热切尚未实现。工件与装配测试见 `harness/harness.test.ts`、`src/pi-runtime-harness.test.ts`；归因回归见 `src/pi-runtime.e2e.test.ts`、`evals/harness-attribution.test.ts`、`go/internal/agent/harness_attribution_test.go`。部署须迁移新增列，并协调更新 Go 与 Node，旧 Node 的无 hash 请求不受兼容。

演化诊断由 `agent-service/src/evolution-traces.ts` 提供，`AGENT_EVOLUTION_TRACES=1` 显式开启，默认关闭。已认领的执行 attempt 从现有 Pi 事件观察模型/工具边界，记录冻结壳/Skill 身份、模型标识、白名单操作结构、usage 和确认终态，不记录用户/模型原文、工具正文、媒体或推理。独立异步队列不进入 lease/journal promise chain；单条 4 KiB、单 attempt 64 KiB、待写 128 条，目录最多 256 个自有文件并预留总计不超过 16 MiB，满额淘汰非活动旧记录或拒绝新轨迹。输出位于绝对 `STORAGE_ROOT/agent-evolution-traces/`，Compose 使用独立 trace 卷；每目录只支持一个 Agent writer。`/healthz.evolution_traces` 暴露丢弃、I/O 错误和淘汰计数。缺尾记录或 `complete=false` 不得作为完整证据；`complete` 不等于业务成功。没有自动 Miner/playbook 消费或线上学习结论。配置、限额、脱敏和执行结果不受影响的回归见 `src/config.test.ts`、`src/evolution-traces.test.ts`、`src/pi-runtime.e2e.test.ts`。

`before_model_request` 还记录已加载 `skill_catalog_hash` 与 `model_configuration`：后者由 `PiSessionAdapter.requestConfiguration` 从已解析的 SDK model、thinking level 和会话请求选项读取，endpoint 仅存 hash，不含 API key。这是请求前 SDK 配置身份，不代表上游供应商返回了相同的最终执行参数。Go L2 的 `eval_provenance_test.go` 保存选中任务及其 world 的 canonical 内容快照、代码/工作树内容身份，并从真实 checkpoint 与 invocation 关联读取逐次请求配置。新 `run.json` 使用 `provenance_version=l2-content-v1`，启动为 incomplete、模型为 unobserved；完整 trial 身份、终态、输入快照、运行配置和 checkout 收尾校验后才为 complete，否则为 invalid。complete 允许业务 FAIL，不表示能力过门。历史批次不回填；校验不能替代运行全程的独立固定 checkout。回归见 `eval_provenance_regression_test.go`、`harness_attribution_test.go` 与 Pi harness/E2E 测试。

Agent Turn 列表先按游标选择本页 ID，再用单条关联查询批量读取投影、conversation 作用域及 Task/Conversation harness 身份，按所选 ID 顺序组装响应；不逐条回读 Turn。单条与批量读取共用关联字段和作用域校验，canvas focus 保持原批量投影。Session 列表最多返回每会话 20 条 conversation 摘要，同时返回完整 conversation_count；没有独立的 Session GET 详情路由。实现与回归：`go/internal/agent/turns.go`、`serialize.go`、`turn_batch_test.go`、`read_load_test.go`。

Agent queued Task 补首轮恢复在候选发现及逐条处理时跳过锁定 conversation；发现事务只锁 conversation 并立即释放，处理事务重新取得同一锁后调用原 `reserveTurn`，保持既有幂等键、作用域和 Task 校验。候选 `HasMore` 只反映跳锁后可选任务，锁定候选存在时也可为 false；下一轮仍按 cadence 扫描。取消返回 context 错误，不继续把取消当成单条业务失败忽略。该机制不跳过 Task/Session 行锁。实现与回归：`go/internal/agent/recovery.go`、`recovery_queued_lock_test.go`。

Agent pending Turn 补投递与 worker 的 `turnNeedsSync` 对齐：仅未绑定 harness 的 queued/running/cancel_requested，或已有答案的 requires_input，且 resume_required=false，才需要同步。已绑定的活动 Turn 由 PG journal 推进，不周期性补无效信封；SQL 发现与逐条读取后复核均检查绑定状态。该复核不是跨读取与 outbox 写入的原子快照。实现与回归：`go/internal/agent/recovery.go`、`helpers.go`、`recovery_sync_eligibility_test.go`。

迟到的 Agent sync 信封在 worker 入口按同一 `turnNeedsSync` 判断，已终态、停等或 resume_required 的未绑定 Turn 不调用 StartTurn；`bindGatewayTurn` 重读后也复核资格，覆盖首次 worker 读取后状态改变及提交路径的直接绑定。已可证明无需同步的信封正常消费，不通过 ErrLater 重排队。最后一次读取与远端请求之间不持数据库锁，终态执行权仍由 `ClaimExecution` 的 projection 锁和状态校验约束。回归：`go/internal/agent/sync_start_guard_test.go`。

## 5. 商品 intake 与已移除的 WorkflowDraft 拓扑

商品图种、数量和参考图 ID 存在 Product 的 intake 上。创建路径不插入 `WorkflowDraft`。商品 Conversation 只要求 `product_id`。产品路径上的 `propose_workflow_draft` / 确认 / persist 不存在；对应 HTTP 返回 404。`workflow_drafts` 表已删除。Agent intake 落库会按模板展开 birth 图；已经展开的图改拓扑只走 Graph Command，不再交第二套完整 DAG。

全局库整理仍使用 `LibraryOrganizationDraft`。多节点改图确认走 `WorkflowGraphProposal`。

## 6. 在线 schema-v3 图

在线工作流保存在 `workflow_graphs`，schema 固定为 3。为什么是 live graph 而不是第二份 Draft 拓扑，见 `adr/0008-free-canvas-agent-graph-authority.md`。

图上只有三类权威对象：Node（配置、效果输出引用、文稿候选引用）、Edge（类型、角色、顺序、依赖）、Artifact（一次生成的不可变产物）。节点 Catalog 把节点标为 `source`、`document` 或 `effect`。正式文稿只存在 `config_json`；文稿候选通过 `pending_candidate_artifact_id` 引用；`current_artifact_id` 只表示效果输出。用户、Agent 和配方都通过 `apply_graph_change_set` 写入正式配置。ChangeSet 操作：`create_node`、`update_node_config`、`rename_node`、`delete_node`、`connect_nodes`、`disconnect_edge`、`reorder_edges`、`move_nodes`、`create_group`、`move_nodes_to_group`、`rename_group`、`dissolve_group`。不完整 DAG 可以保存；运行前再查完整性。文稿候选的读取、按 section 应用与放弃由专用 API 管理。运行时上下文只读目标节点的 incoming edges，见 `go/internal/graph` 的 catalog 与 rules，以及 [`adr/0015-canvas-ports-run-queue.md`](adr/0015-canvas-ports-run-queue.md)。

节点类型为：

- `product_source`：商品事实入口。
- `image_asset`：一对一绑定 ProductImageAsset。
- `creative_brief`：运行时根据商品资料和参考图生成创作要求，结果写入节点并可以再编辑。
- `visual_system`：运行时根据商品资料和参考图生成风格与背景约束，结果写入节点并可以再编辑。
- `image_prompt`：运行时根据上游上下文生成提示词，结果写入节点并可以再编辑。
- `image_generation`：根据正式提示词文稿和 GenerationSpec 生成图片。运行此节点（`scope=node`）只入队目标；需要连带仍需生成的上游时使用“运行到此节点”。跑文稿节点不会自动跑下游生图。文稿、候选、`document_origin` 与生成范围见 [`adr/0014-canvas-document-cook.md`](adr/0014-canvas-document-cook.md)。端口、`selection` 运行、运行队列与 `skipped` 见 [`adr/0015-canvas-ports-run-queue.md`](adr/0015-canvas-ports-run-queue.md)。

画布分组是一层视觉分组，可进入局部视图并分记视口，不改变 DAG 执行语义。跨组边在全图可见。分组没有端口、运行、取消或重试。边由 Node Catalog 决定 data_type 与 role。节点详情表单按同一份 `config_fields` 渲染，保存走 `update_node_config`。检查器或 Agent 一次 `update_node_config` 若 `base_graph_revision` 落后于并行 cook 采用，只要中间历史没改过该节点，服务端 rebase 到当前 revision；同节点丢失更新、拓扑和过期 `create_node` 仍 409。浏览器只对纯 `move_nodes` 自动重放，文稿保存失败会提示版本冲突。检查器草稿把开始编辑时的图 revision 作为 `base_graph_revision` 沿 autosave、Inspector、Surface 和 `commitNode` 传到 Graph Command；兄弟节点配置变更触发 refetch 时，当前节点 `config_json` 未变则保留该基线并继续保存。同节点冲突保留草稿、提示失败、不自动重放。证据：`web/src/pages/workbench/canvas/useNodeDraftAutosave.ts`、`just web-e2e-canvas-document`。

摄影和信息图每种图片类型落成一层 Group：1 个 `image_prompt` 加 N 个 `image_generation`（N 为该镜头张数）。证据类型（资质、工厂）是未绑定的 `image_asset`，`role=evidence`。创建上传的参考图 `role=product_identity`，接到视觉规范、创作要求和会生图镜头，不接到证据占位。添加面板「添加场景」一次 ChangeSet 创建组 + prompt + 1 张生图。实现：`web/src/pages/workbench/canvas/shotChangeSet.ts`，模板 `go/internal/graph`。

`WorkflowGraphRun` 和 `WorkflowGraphNodeRun` 保存运行状态。执行读 run snapshot，不再读 live graph。图片结果写入 ProductImageAsset 和 `WorkflowGraphArtifact`。同一 run 由一个 worker 持有；跨实例执行权由 GraphRun 行上的 token/expiry lease 提供，默认 35 分钟、每 5 分钟续租，过期后以 CAS 接管；同进程 mutex 只作快速门禁，不钉住长生命周期 SQL connection。互不依赖的处理节点可同时打 provider，上限为 PostgreSQL 权威的 `generation_max_concurrent_tasks` admission。一个节点失败或 unknown 不中止同层独立节点；上游失败的下游标失败。运行时的 Graph claim 先取全局 generation capacity advisory，再按 `run -> node -> effect` 的顺序取执行行；修改 live graph 的自动采用路径按 `run -> graph -> node`。GraphRun 列表返回状态/时间/节点进度摘要；snapshot、input trace 和 output 由单 run 详情读取。证据：`go/internal/graph` 执行、lease、列表投影与耐久测试。

工作流运行由 ProductFlow 业务接口直接创建和校验。工作流页面可以提交整图、运行到某节点、单节点，或对镜头/失败子集提交一次 `selection`。已有 `running` run 时新请求进入 FIFO 排队，出队时再快照。用户不需要先创建 Agent Conversation。Agent 通过 `go/internal/agent` 创建待确认请求；用户确认后走同一套 `go/internal/graph` 约束。商品路径 Agent Turn 不能提交 Draft artifact。单次可逆改图走 Graph Command（Agent 立即写入也只接受一条 operation）；多节点重构写入未应用的 `WorkflowGraphProposal`，画布幽灵预览，确认和取消只在画布完成。

WorkflowRecipe 保存用户主动创建的完整工作流或局部片段。配方库只列出用户从 live graph 保存的配方，不预置画布模板。保存从 live schema-v3 graph 提取，payload 是节点/边/分组片段，不含商品身份、绑定素材、生成结果或媒体字节。完整配方只在目标商品还没有 live graph 时创建；已有图时返回冲突。片段配方合并进已有 schema-v3 工作流，无法合并时返回明确冲突，不会写成 Draft 或退休模型。HTTP 保存入口是 `POST /api/v3/products/{product_id}/workflows/{workflow_id}/recipes`；预览/应用是 `POST /api/v3/products/{product_id}/workflow-recipes/{recipe_id}/preview` 与 `.../apply`。

无 live graph 时，`POST /api/v3/products/{product_id}/workflows` 写入一张空的 schema-v3 图（revision 1，无节点/边）；已有 active graph 时返回冲突。空图出生不是空 ChangeSet（operations 至少一条）。节点、边、分组写入仍走 `apply_graph_change_set`。

图规则由 `go/internal/graph` 负责。目录同时给出端口合同和可编辑配置字段；ChangeSet 写入会拒绝未登记的 `config` 键。结构写入走 Graph Command；运行走同一包的 runs / execute / durability。HTTP 入口是该包的 HTTP 层。

参考输入按入边顺序携带资产 ID、用途 `role` 与说明 `label`；节点 `config.label` 非空时优先于绑定资产名称。三项共同进入消费节点的输入摘要，用途或说明改变会使相关结果需要更新，断开参考边后不再影响该目标。生图 adapter 发出的参考像素顺序与最终提示词中的编号一致。此摘要算法变更可能使带参考图的既有结果显示过期，不删除历史结果，也不自动提交运行。实现与回归：`go/internal/graph/compiler.go`、`go/internal/graph/reference_contract_test.go`、`go/internal/providers/adapt/adapt_test.go`。

画面文字由 `image_prompt.config.text_settings` 拥有（`policy: none|required`、`language`）；`generation_spec` 不再保存文字参数。图片生成的 `prompt_overrides` 仅允许构图、内容与氛围的叶字段，显式空字符串/列表保留，缺省叶字段继承；`text_override` 是本图完整文字设置。`graph/image_document.go` 统一解析执行、摘要和检查器输入，摘要使用生效内容，不因空覆盖或被覆盖的上游字段变化而过期。连接到方案的创作要求将必须包含和禁用要求传给生图，不能通过局部覆盖键删除。此合同不提供自然语言冲突的确定性检测。

创作要求字段为 `goal`、`key_messages`、`required_elements`、`prohibitions`、`fact_gaps`；系列风格仅有 `style` 和 `colors`。文稿候选不拥有方案文字设置，采用时保留用户配置。本次字段变更不提供旧配置转换或双读路径。生成与交付仍分离，交付参数不进入模型输入摘要。

## 7. 图片模型

`MediaObject` 保存 storage 路径、MIME、字节数、尺寸、哈希和核验状态。`ProductImageAsset` 保存商品作用域内的显示名、来源、文件夹、父图和图片类型。

当前图片来源：

- `upload`
- `workflow_generation`
- `image_session_attach`
- `local_edit`

商品图片库、节点参考绑定、封面和交付图都使用 ProductImageAsset id。图片会话的资产也必须关联 MediaObject；保存到商品时创建 ProductImageAsset。图片会话列表按 `updated_at DESC, id DESC` 使用版本化游标分页，默认 20 条、最多 100 条；列表只返回最新资产和轮次摘要，详情再读取任务与轮次明细。连续生图 worker claim 与 Graph 共用全库 `generation_max_concurrent_tasks` advisory，顺序为 capacity → task；HTTP `Generate` 只写 queued 与 PENDING，不持该锁、不增加 denied。

DeliveryRenditionJob 从 ProductImageAsset 读取原始媒体，按裁切、缩放和格式规范异步生成交付文件。交付文件不替换源图。内置 DeliverySpec 模板由 `go/internal/delivery` 提供，只读 API 顺序为淘宝/天猫首屏 3:4、京东主图 1:1、Amazon 主图 1:1、详情竖图 3:4、场景横图 4:3。模板是便捷默认值，不构成平台审核或合规保证；用户可以覆盖宽高、格式和体积。来源标记为 `docs/ARCHITECTURE.md §7`。

交付采用快照由 `delivery_adoption_versions` / `delivery_adoption_slots` 持久化：引用源资产 ID、图位用途、排序、DeliverySpec 与可选事实/视觉版本，不复制图片字节。显式采用创建新版本并把 `products.current_delivery_adoption_version_id` 指向它；历史版本行不可变，节点重跑不改已采用快照。`quality_status=fail` 或文字溢出的图位不得进入合格采用集（IQ-CF-08）。导出与预览共用同一文件计划，复用既有 DeliveryRenditionJob / ZIP，不另建执行器。HTTP：`/api/v3/products/{id}/delivery-adoptions` 及 `current` / `{version_id}` / `preview` / `renditions` / `export`。文稿候选采用（O1–O7）与交付采用分开。

商家内视觉方案版本由 `visual_systems` / `visual_system_versions` 持久化，商品显式选择落在 `product_visual_selections`（钉住不可变版本 id）。追加新版本不静默改选择、在做图任务或已采用交付。继承解析（IQ-CF-07）优先级为本商品覆盖 > 选定方案版本 > 品牌版本占位（`brand_table_not_ready`）> 产品默认；Brand 表未就绪时不伪造多品牌实体。HTTP：`/api/v3/visual-systems` 及版本/impact，以及 `/api/v3/products/{id}/visual-selection` 与 `visual-inheritance`。配方创建预览附带 `reuse_preview`（继承/待填）。

GenerationSpec 保存模型生成意图；provider effective values 和解码后的 actual output 保存在运行/生成记录中。DeliverySpec 是独立确定性合同，不能触发图片模型调用。

全局素材库读取、文件夹/标签/归档、来源保存和工作流关联由 `go/internal/library`、`MediaLibraryPage.tsx` 和 `WorkflowMediaLibraryPanel.tsx` 负责，列表使用有界 cursor page 和 preview/thumbnail URL。商品工作台中的 `workbench/chrome/image-explorer/` 继续负责商品作用域的人工选图和绑定。

商品图库批量下载通过 `POST /api/v2/products/:product_id/image-assets/download-archive`，最多 100 张且总元数据字节不超过 512MiB。事务只冻结条目身份，事务外逐文件 `ReadVerified` 核验、写入临时 ZIP；完成 ZIP 后 HTTP 才发送附件，返回或传输中断后删除临时文件。内存不持整包，但首响应要等待打包完成，并需要整包临时存储。`just go-test-zip-http-rss` 覆盖鉴权、10/100 张有效 PNG、条目哈希、传输中断清理与数量/总字节拒绝；该单客户端下载门不代表并发磁盘或内存容量。

商品图库打包的取消点由请求 context 和 `Writer.Add` 入口检查连接：临时 ZIP 已创建、首个媒体元数据读取后取消时，当前文件核验可以完成，但不会继续读取后续文件，失败路径删除临时包。HTTP 内存门用真实请求取消与 handler 退出屏障验证该边界；不在文件核验或压缩内部新增取消线程。`ReadVerified` 核对大小、SHA 和图片配置，不执行完整像素解码。

## 8. Provider 架构

`ProviderProfile` 保存 endpoint、secret、能力、默认模型和 provider 级配置。`ProviderBinding` 把一个 profile 绑定到用途：

- `prompt`：视觉规范、创作要求和提示词节点；创建页看图起草 `source_note` 也走同一用途。用途枚举仍是 `prompt`。
- `agent`：workflow Agent。
- `image`：工作流和图片会话生图。

Go 业务 API 解析 prompt/image 绑定；Agent service 通过受内部 token 保护的 endpoint 获取 agent 绑定。缺少绑定、profile 被禁用或模型为空时返回明确配置错误。

运行时图片工具设置经过允许字段校验，再由具体 provider adapter 映射。候选数量来自图片类型数量或图片会话 generation_count，不属于高级 tool option。

## 9. 异步与恢复

连续生图每次消费最多发起一次 provider 调用。确认批次后仍有候选时，任务在 `candidate_saved` 检查点回 queued、释放 attempt 围栏，并在同一事务通过 Requeue 释放信封 lease、置 PENDING、设置原有 1s 续投延迟；然后返回 `queue.ErrLater`。旧 consumer 的 token CAS 不能修改新一轮 lease。检查点后进程退出不再等待旧 35 分钟消费 lease，信封写入失败则整个检查点事务回滚。下次 claim 创建新围栏，保留完成数、结果组、业务尝试次数和原 started_at；失败重试清空 started_at，仍受原尝试次数上限约束。30 分钟 handler deadline 限制每次消费，不再累计顺序批次。每次消费重新读取输入媒体，批次之间可能等待投递和容量 admission。回归：`go/internal/imagesession/batch_yield_test.go`、`checkpoint_exit_test.go`、`checkpoint_transaction_test.go`。

- Go worker 负责工作流节点、生图会话候选、交付图和局部修任务。
- Async dispatcher 扫描 PostgreSQL 中的 durable dispatch/recovery 状态并向 Redis 投递；`just dev` 与 Compose 都启动该进程。watch 模式默认每秒运行 dispatch，默认每 10 秒运行一次 domain recovery；`--interval` 与 `--recovery-interval` 分开控制。claim 满 `limit` 时 dispatch 立即续跑，空闲才等 interval 或 NOTIFY；同一轮已 claim 行有界并发 SENT+enqueue，每条仍先 SENT 再 enqueue。Agent、Graph、ImageSession、Delivery、LocalEdit recovery 默认每阶段最多处理 25 条候选，业务域使用稳定排序与 `SKIP LOCKED`；dispatcher 结构化日志记录每个 owner 的 `has_more`、recovery duration 和错误；配置 metrics token 后，API `/metrics` 提供 queued/stale-running backlog 与当前 PostgreSQL 锁等待，dispatcher `/metrics` 另提供各域 recovery duration histogram 和候选锁查询耗时。
- 连续生图 running 是否闲置看最后一次 progress heartbeat（没有则 started_at）。默认 90 分钟后 dispatcher 重排队或标 `unknown`；心跳未过期不恢复。asynq `TaskTimeout` 30 分钟取消 handler context 时，worker 仍把不可证明的结果写成 `unknown`，不等待闲置阈值。已 `applied` 的 candidate 不因恢复再写副作用；晚到 attempt 不能覆盖 `unknown`。本链没有 parked question/approval。实现：`go/internal/imagesession/recovery.go`、`execute.go`；回归：`recovery_test.go`、`TestExecuteCanceledContextMarksUnknown`。
- Redis 只承担 asynq broker 和投递唤醒；业务状态和生成容量 admission 由 PostgreSQL 负责。asynq worker 默认并发为 4，业务失败不依赖 broker retry。
- PostgreSQL 保存 queued/running/terminal 状态、attempt 和错误摘要。
- 连续生图状态的 `has_active_generation_task` 由同次返回的完整活动任务列表推导，不做独立 COUNT，避免查询间入队/完成导致活动标志与列表矛盾。此保证仅覆盖活动标志与任务列表，不表示轮次、effects 和队列统计共用一个全局快照。Status/SSE 返回该会话全部 queued/running，但不重复下发 `prompt`；提示词以详情和提交响应为准，前端把状态进度叠到已缓存任务上。实现：`go/internal/imagesession/serialize.go`、`web/src/pages/image-chat/branching.ts`；回归：`status_projection_test.go`、`active_status_load_test.go`、`branching.test.ts`。
- 连续生图 Status 和详情仅在本次返回任务时读取队列概览；空任务列表不查询全局容量配置及队列 COUNT。详情返回终态任务时仍附带现行队列字段。实现与回归：`go/internal/imagesession/serialize.go`、`status_projection_test.go`、`http_load_test.go`。
- 展示用队列总览在单条 SQL 中汇总 ImageSession 与 Graph 的 running/queued，避免状态切换期间重复计数或漏计；Graph 按 run 计数，有 running 节点时优先归 running。容量配置仍单独读取，worker 的 admission 节点计数与准入锁不变。实现与回归：`go/internal/platform/generation/snapshot.go`、`snapshot_test.go`。
- 连续生图 SSE 每个连接发送首份 `session.status`，之后仅在完整 JSON 快照变化时发送；比较涵盖任务进度与队列位置，不只看会话更新时间。PG 回读与 15 秒注释心跳保持独立，终态发送后关流，重连重新发送首份快照。实现：`go/internal/imagesession/sse.go`；回归：`sse_test.go`、`sse_load_test.go`。
- worker 启动恢复可安全重投的未完成任务。
- Agent service 使用 Pi session 文件做模型 loop 恢复；本地 JSONL 只作私有 WAL，持有当前 lease/fencing 的 runtime 显式读取未确认连续前缀、批量写入 PostgreSQL `agent_turn_events`，并只在 receipt 匹配后推进 ACK。浏览器 live SSE 只读取 PG journal 并投影为 UI 协议，运行中与终态使用同一游标。`GET /api/v2/agent-control/events` 推送 Session/Task/lease 变更。图运行 SSE 与文/图生图会话 SSE 由 worker 写入后经 `pg_notify` 唤醒 API；同一 API 进程内的 Agent、Graph、ImageSession SSE 通过 `go/internal/platform/notify` 按 pool 共用一条 listener 连接，通知丢失时按 PostgreSQL 游标或状态回读。浏览器断开不取消 Agent。Go worker 只绑定未启动 Turn 的 harness ID、重试 start，或恢复已持久化答案；已绑定 Turn 的活动状态和摘要由 journal 写入推进。启动只重放尚未开始的 queued Turn；Node 重启只确认或提交可证明的 WAL 连续前缀，不为丢失的 in-flight execution 写终态。Go lease 过期扫描是该类执行的唯一终态作者，`requires_input` 与 `awaiting_confirmation` parked Turn 不会被误标 unknown。无法证明的结果保持 `unknown`。后台 durable Task 与全量对账见 `ROADMAP.md`。BFF 边界的历史决策见已被取代的 [`adr/0013-agent-live-journal-bff.md`](adr/0013-agent-live-journal-bff.md)；当前全量 journal 与 UI 协议见 [`adr/0017-agent-full-journal-ui-protocol.md`](adr/0017-agent-full-journal-ui-protocol.md)。
- ProductFlow 的 Turn 命令响应只信任符合 Agent service wire contract 的状态；活动投影只信任 PostgreSQL journal，无法证明的外部结果继续保留 `unknown` 语义。

## 10. 配置与安全

环境变量只保存启动前必需的基础设施和 secret：

- database、Redis、storage
- admin/session/settings token
- Agent service 地址和内部 token
- 可选的 `METRICS_BEARER_TOKEN`；未配置时 API 与 dispatcher 都不注册 `/metrics`；dispatcher 可用 `DISPATCHER_METRICS_ADDR` 暴露独立抓取端口
- 上传限制、日志和 worker 基础参数

Provider profile、purpose binding 和业务运行时设置由 `/settings` 写入数据库。设置页需要管理员 session 和独立 `SETTINGS_ACCESS_TOKEN`。

上传在持久化前校验 MIME、真实图片格式、字节数、像素数和数量。下载接口按数据库资产定位 storage，不接受任意文件路径。

API / worker / dispatcher 终端默认打可读行（时间、级别、进程、消息、`key=value`）；滚动 JSON 文件落在 `STORAGE_ROOT/logs/`（默认 `storage-dev/logs/` 或 Compose 的 `/app/storage/logs`）：`productflow-api.log`、`productflow-worker.log`、`productflow-dispatcher.log`，含 caller 与 error stack。`LOG_DIR` 覆盖目录，`LOG_FORMAT=json` 让 stderr 也输出 JSON。空闲 dispatcher 周期、`/healthz` 和 Agent heartbeat 只写文件。配置 metrics token 后，`/metrics` 以 bearer 鉴权输出低基数的 Turn、Graph run、dispatch、模型调用、副作用对账和 SSE 连接指标。实现与测试：`go/internal/platform/log`、`go/internal/platform/metrics`。

## 11. Schema 演进

空库和已有库都跑 `productflow-migrate`：GORM `CreateTable`/`AddColumn` 建/补表和列，随后 ExtraDDL 幂等补上 CHECK、PostgreSQL enum、部分唯一索引和 FK。不使用 AutoMigrate（它会改写已有库的 unique 索引名）。该命令不删除已退休表或列；退休表用显式 SQL 删除。主仓库不写旧数据回填、冻结或 cutover gate。跟上主仓库可以重建数据库和 storage。

## 12. 质量门

- Backend：Go `go test ./...`、`productflow-migrate`，以及 opt-in PostgreSQL/Redis live tests。
- Frontend：Vitest、ESLint、TypeScript 和 Vite production build。跳过 Agent、真实 prompt/image provider 跑完整图的浏览器 gate 是 opt-in：`just web-e2e-live-graph`。画布文稿权威的有界动作搜索随 `just go-test`（`go/internal/graph/authority_search_test.go`）；加深搜索 `just go-test-canvas-search`。mock 供应商下的 Inspector 空闲改写/候选、运行中打字、整图跑中途撤销、文稿 409 停止、检查器运行该节点/运行到这里与运行中取消浏览器门是 opt-in：`just web-e2e-canvas-document`。验收口径见 [`audits/canvas-test-system.md`](audits/canvas-test-system.md)。
- Agent service：`pnpm --dir agent-service test`、`pnpm --dir agent-service build`。`agent-service/evals/` 保存 JSON 任务集、评分器、L1/L3/L5 runner 与报告。`just agent-evals-live`（需要 `AGENT_PROVIDER_API_KEY`，默认 k=3）对 L1 任务跑真实模型并落盘 `STORAGE_ROOT/agent-evals/`。`graph-editing` 的 L1 trial 会为每个试验拉起隔离 testdb 上的 Go 宿主（`PRODUCTFLOW_EVAL_HOST_LAYER=l1`），需要 `DATABASE_URL`；宿主由 testdb 包建隔离库，不写共享 `productflow_dev`。启动前跑 `TestEvalObservationFixtures`，Catalog/intake 漂移则失败。默认 Vitest 不启动该宿主。`just agent-evals-state` 是 opt-in L2（Go httptest + PostgreSQL + 真实 Pi）。`just agent-evals-sim`、`just agent-evals-adversarial`、`just agent-evals-judge`、`just agent-evals-nightly` 是对应层的 opt-in 入口。生产回流：`just agent-evals-mine` / `go run ./cmd/productflow-agent-evals`。验收口径见 [`audits/agent-eval-system.md`](audits/agent-eval-system.md)。
- 生图质量：`just image-evals-ingest` 写入过线池，`just image-evals-run` 需要 `PRODUCTFLOW_RUN_IMAGE_EVALS=1`、已起的 API/worker 与真实 prompt/image 绑定。像素只落 `STORAGE_ROOT/image-evals/`。验收口径见 [`audits/image-quality.md#image-quality`](audits/image-quality.md#image-quality)。
- 跨层变更补真实浏览器、真实数据库或真实 provider 验证，验证强度由变更风险决定。

### 冻结集合与开发输入

`evals/report.ts:isUnobservableTrial` 统一判定 trial 可测性：显式 `unobservable`、记录状态或运行终态为 `unknown`、任一工具结果为 `unknown` 均只能作为诊断。L1 落盘、能力比较和开发导出共用此判定；工具成功不能证明结果未定的整轮已完成。

L3 的 `evals/user-sim.ts` 使用独立 Pi SDK `complete()` 选择规范事实回答并生成后续话语，模型可由 `AGENT_EVAL_USER_SIM_MODEL` 覆盖，默认与被测模型同名但另发请求。隐藏目标、事实与策略仅供用户侧模型使用；被测对象仍是生产 `PiRuntimeManager`。问题回答按 `answerQuestion -> resume` 恢复同一 turn。授权绑定回答后的调用区间和具体写入目标，新回答撤销旧的未来授权；后来同意不追认先前写入。确认/丢弃由脚本策略选择，经 `evals/go-world.ts` 调用隔离 PostgreSQL 中的 Go 业务服务与确认路由，并复读图、草案或运行请求。该测试 host 的 lease/journal 仍为本地夹具，不证明生产持久化恢复链。回归见 `evals/user-sim.test.ts`、`evals/go-world.test.ts` 与 `go/internal/agent/eval_user_sim_host_test.go`。

工具观察必须记录 `succeeded`、`failed` 或 `unknown`；required 写入只接受成功结果，附加错误写入仍失败。L1 的 catalog、intake 展开及素材元数据观察来自 Go 生成的 `evals/fixtures/`；缺失 intake/library 快照仍记 `eval_unobservable`。`graph-editing` 的 apply/propose/discard 与写后读取由 `evals/go-world.ts` 的 graph overlay 交给隔离 testdb 上的 `TestEvalUserSimHost`；业务 `apperr` 保留原 HTTP 状态，基础设施错误为 500 `eval_host`。409 注入仍在 Node 侧生效，重试读取 Go 返回的当前 revision。未绑定宿主的图写入返回 500 `eval_host`，不能伪造成功或业务拒绝。素材桩使用 `library-observations.json`，保留默认隐藏归档、显式恢复读取、独立目录分页及按本页限定的关联真值；L3 素材草案从实际读取取得 before/revision，确认后复读。测试 owner：`eval_library_observation_test.go`、`eval_graph_authority_test.go`、`evals/graph-authority.test.ts`、`evals/library-observation.test.ts`、`evals/go-world.test.ts`。缺失快照或未知结果仍为不可测，不能据此做能力比较。L5 按实际暴露后的目标操作判攻击成功，无暴露或结果未知不计安全通过；素材派生注入按显式 base origin 消费 Go 快照并保留污染字段。历史无 outcome 的原始记录保留，新报告不兼容读取；旧 ASR 与 L3 通过数须按新合同复验，见 Agent 质量账本。

`agent-service/evals/collections.ts` 拥有场景集合清单与开发材料投影。评测维护者提供 JSON plan：`schema_version=1` 和 `groups`，每组含 `scene_id`、`source_ids`、`purpose`（`development` / `regression` / `acceptance`）、`exposed`、分组依据 `evidence`、`task_ids`。清单须覆盖完整题集，同场景、同 source 或同 task origin 不得跨用途；已暴露组只能是 development。全部释义随任务保留。声明的来源与未暴露性仍需维护者审查，代码不能自动证明语义独立。

`just agent-evals-freeze-collection <绝对 plan 路径>` 钉住当前 loader 读出的 task/world 内容与分组 hash，写 `STORAGE_ROOT/agent-evals/collections/<hash>.json`，不覆盖旧清单。改题、world 或分组须重新冻结。当前公开题库和已读旧转录只能作为已暴露开发材料，旧 `held_in/held_out` 是历史报告标签，不具备集合访问授权。没有真实隐藏/验收材料时必须保持缺失，不把空集合当验收通过。

`just agent-evals-run-collection <绝对 manifest 路径> <用途>` 沿现有 L1 runner 执行该用途的完整 L1 任务，默认 k=3；禁止叠加 filter、suite、替换 tasks 或其它 layer。`run.json.collection` 记录实际 manifest hash、用途及任务 ID；普通 run 不补造此身份。需要与现有 live 相同的真实模型凭据，命令本身不证明一次运行已完成。

`just agent-evals-export-development <run_id> <绝对 manifest 路径>` 只接收已结束且身份完整匹配的开发批次。验证集合与 run 后才读 trial，所有 trial 的数量、身份、表述和路径均通过后才读转录；混合用途、旧 run、缺失/重复试验和符号链接被拒绝。导出按原始 trial 重算 `measurementEligible`，含 `unobservable` 状态或 `unknown` 工具结果时整批拒绝，不删除坏样本后缩小分母。L1 将未知工具结果显式记录为不可测 trial，保留原始终态及诊断轨迹；真实已观察的能力失败仍可导出。输出 `agent-evals/development-inputs/` 的 0600 文件，包含开发 task/world、成功与失败 trial、转录的回答/工具/错误字段及版本身份，不含转录顶层 thinking 字段；不读取全局 history 或 summary 正文，不能从这些入口把隐藏摘要带入提案。该输出仍是敏感开发材料，不是匿名化产物。

这是可信评测进程中的应用边界，不隔离同一 OS 用户；维护者须固定输入和已结束 run，避免并发篡改。未来提案器只能接导出的开发包，不得获得评测文件系统或命令工具。P3/P4、隐藏集采证、独立验收和候选比较尚未实现。测试见 `evals/collections.test.ts`、`evals/cli.test.ts`。

代码与文档同步规则：

- 路由变化同时核对 `App.tsx`、`lib/api.ts`、PRD 页面表和用户指南。
- 用户操作变化同时核对 `USER_GUIDE.md` 和 `web/src/pages/HelpPage.tsx`。
- enum/DTO 变化同时核对 Go DTO、`lib/types.ts`、label map 和 parser tests。
- transaction/queue/recovery 变化沿 application entrypoint、durable row、broker call、worker claim 和恢复测试验证。
- 模块移动同时更新本页所有权表和 package `AGENTS.md`，删除旧路径引用。
