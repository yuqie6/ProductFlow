# ProductFlow Context

## Product

ProductFlow is a visual production workspace. The current development bootstrap still starts with one administrator and one development merchant. Public email registration is implemented and has passed real-browser and real SMTP/IMAP mail-receipt verification; a verified signup creates an ordinary User with that user's own Merchant and trial quota. Each ordinary account owns one merchant; multi-organization membership and workspace switching are outside the product scope. Platform administrators have explicit merchant-targeted product read/edit/delete APIs that preserve ownership; merchant-scoped administration pages and current-operation records are implemented. Accounts use direct users.merchant_id ownership; no team membership model remains. Interface language/theme belong to the User; merchant naming belongs to the directly owned Merchant. Suspending merchant business writes does not disable personal profile, password or session management. A user uploads real product references, selects intended image types and quantities, and works on a live schema-v3 graph. Agent-first create writes that graph immediately; the Agent applies or proposes ChangeSets instead of submitting a WorkflowDraft.

The current implementation is an unreleased, rapid-development baseline with no commercial merchant users. Breaking changes are expected on the pre-stable mainline: there is no general compatibility window or dual runtime, and following this baseline may still require recreating the database and storage. From the first stable release onward, supported **operating-data** N→N+1 upgrades follow the contract in [`release/README.md`](release/README.md) (precheck → backup → pin swap → `schema.Apply` migrate → smoke; migrate failure stops the app tier and rolls back via the pre-upgrade backup). That contract does not cover retired V1/v2 or experimental data. Complete account and administrator workflows and real payments remain unimplemented; expanded team roles are outside current scope.

The intended product is a self-hostable, multi-merchant SaaS, also deployed by the project owner as an operated service. The current development boundary does not define the final product scope. After deployer bootstrap, the registration flow uses Operator-configured SMTP and a six-digit email code to create an ordinary User, that user's own Merchant and trial quota; the registration slice has API, database, Web and real SMTP/IMAP evidence. The [release roadmap](docs/ROADMAP.md) owns the remaining identity, isolation, commercial, experience and stable-release requirements.

## Online Flow

1. `/products/new` is the only product-creation entry.
2. Image types start unselected. Selecting a type initializes its quantity to 2. 「应用推荐套图」补齐封面 2、卖点 4、规格 1、SKU 1、场景 1、细节 1，不覆盖已选类型的数量。
3. Each selected type has a quantity from 1 to 6; the total planned images cannot exceed 30.
4. The user uploads 1 to 6 verified references and is expected to include at least one image that identifies the real product or an authoritative product rendering. The backend deterministically validates count, ownership, bytes, and media format; semantic adequacy remains an Agent/user review responsibility.
5. Agent-first create can start from a product name.
   - Persist the Product, a live schema-v3 graph, one product-owned `AgentSession`, and one product-scoped `AgentConversation`. Name-only graphs contain a `product_source` node; form-complete graphs use the same template as direct create.
   - Do not create a `WorkflowDraft`, an onboarding `AgentTask`, or an automatic Turn. Product intake (image types, quantities, reference asset ids) lives on the Product.
   - The user uploads 1 to 6 verified references in that conversation and names the image types in text. The Agent persists them with `finalize_product_intake_v1`. That write expands a name-only birth graph from the same photography/infographic template as form-complete create (one group + prompt + N image nodes per generating type). Later graph edits use Graph Command operation names from the Agent tool schema and `go/internal/graph` `ops_parse` (`create_node`, `connect_nodes`, and the rest of the closed op table).
   - The create form can still collect types and files first as a shortcut, and remains the required path for direct create. Direct create persists the product, references, and schema-v3 graph without a conversation. The create page may call the prompt provider on uploaded references to draft `source_note`; returned `fields` become an editable spec table, and values the photos do not show stay empty for the user.
   - Opening the canvas Agent sidebar with none present attaches a product-owned session. Missing image types or references do not block Agent Turns.
   - `AgentSession` is the longer-lived conversation container. Canvas sessions belong to one product; global Dock sessions do not. The current Turn runtime still uses the conversation projection.
6. The Agent asks for missing facts and applies or proposes graph ChangeSets. It may suggest plan changes but cannot silently change confirmed user choices or facts. Product-workflow Agent cannot propose, confirm, or persist a WorkflowDraft.
7. Direct create and Agent-first create both persist a live schema-v3 graph in the create transaction. The workbench reads that graph after persist; it does not assemble the graph incrementally in the browser.
8. Closing or never opening the Agent conversation leaves add, connect, inspect, run, undo, and recipes available. Turn `running` / `unknown` / `failed` does not lock the canvas.
9. The same product-owned Agent conversation continues in the product workbench beside the editable workflow. The Agent inspects, explains, applies one reversible ChangeSet or proposes a multi-node ChangeSet, and may request runs. A product Goal is an explicit `AgentTask` the user starts on a runnable graph. A finished Turn or `WorkflowGraphRun` does not complete that Goal; the user marks it complete.

## Authorities

- PostgreSQL is authoritative for products, facts, assets, library-organization Draft revisions, workflows, recipes, provider configuration, and business job state.
- ProductFlow PostgreSQL remains authoritative for business state and for the Agent Turn event journal (`agent_turn_events`). The Node.js/Pi adapter owns its session files for the model loop; those files are not a durable business authority and do not prove background recovery, effect reconciliation, or multi-instance claims. Node restart recovery only confirms or drains a provable WAL prefix. The Go lease-expiry scanner is the sole terminal author for a lost in-flight Agent execution; parked input and confirmation states remain parked.
- ProductFlow stores a web projection of Agent state. PostgreSQL journal writes incrementally fold active Turn status plus the `output_text` / `thinking_text` / `tool_steps` list summaries; Turn reads and the start/resume worker do not refresh those fields from Node session snapshots. Conversation rendering replays the Turn journal, and PostgreSQL does not reconstruct a second model transcript. In agent-service, `runtime-manager.ts` owns process scheduling, `runtime-scope.ts` owns contract validation, `turn-runtime.ts` owns lease-bound execution and question/terminal orchestration, `runtime-journal.ts` and `journal-publisher.ts` provide the journal wire/ACK boundary, `tool-step-projection.ts` provides bounded UI summaries, and `pi-runtime.ts` owns the per-Turn Pi session/model/chunk/Skill/Tool adapter.
- `AgentTask` stores one business goal and one task-specific run under an `AgentSession`. The persisted run-identity column is `harness_run_id`. On a product-workflow conversation, Turn or graph-run success, failure, cancel, or unknown leaves the task `waiting_user` with `waiting_reason=goal_loop`; only the user complete/cancel endpoints finish the Goal. Read-path graph-run sync must not overwrite those user-owned statuses. `AgentTurnProjection` may point to a task and a bounded `AgentPageContextSnapshot`. A route change updates ambient context for later turns and does not rewrite the task goal.
- `MediaObject` identifies immutable media bytes. `MediaLibraryAsset` identifies one global library asset and its provenance. `ProductImageAsset` identifies one image inside a product namespace. `WorkflowMediaLibraryAsset` records a workflow usage association without owning another media copy.
- Workflow nodes and covers reference `ProductImageAsset` ids, never storage paths or parallel-array positions.
- Retired V1/v2 source rows, archives, and cutover gates are not product authorities. Do not add readers, rebuilds, or upgrade paths for them.

## Workflow Invariants

- The only online workflow schema is version 3, stored on `workflow_graphs`.
- Node Catalog owns connection rules and editable config keys. ChangeSet `config` cannot introduce unregistered keys or retired plan keys. Catalog fields declare `affects_digest` and `required`. Image digest omits `delivery_spec`. Topology verbs (`create_node`, `connect_nodes`, and the other Graph Command ops) are owned by the Agent tool JSON Schema and `ops_parse`; they are not restated as a third matrix in Skill or Context prose.
- Node types are `product_source`, `image_asset`, `creative_brief`, `visual_system`, `image_prompt`, and `image_generation`.
- Processing nodes expose one named input port per Catalog `accepts` role; the React Flow handle id equals the persisted edge `role`. Connection checks type, cardinality, and cycles only; readiness is a run-time check.
- `creative_brief`, `visual_system`, and `image_prompt` keep a published document in `config_json`. Content-node `document_origin` (`seed` | `generated` | `authored`) is a column on `workflow_graph_nodes`. Template create is seed. Inspector visible-field saves become authored. Successful generate adopt writes through a ChangeSet as generated. Fill cook calls the prompt provider only for seed documents. `image_generation` consumes the live `config.prompt` document; artifacts are run lineage, not a compile gate. Running a content node does not run downstream image nodes. Inspector and Agent `update_node_config` with a stale `base_graph_revision` still land when intervening history only changed other nodes; same-node lost updates stay 409.
- An `image_asset` node binds exactly one product image asset. Binding is not the same as a downstream `reference` edge.
- One planned output image is represented by one runnable image node. A rerun updates its current asset while previous results remain in the library and run history.
- Product facts, visual systems, prompts, recipes, and execution inputs preserve immutable versions used by prior runs.
- `GenerationSpec` describes model-generation intent. Provider-effective values and measured output remain separately observable.
- `DeliverySpec` describes deterministic rendition work. Changing delivery dimensions or format does not invoke the image model or replace the generated source.
- Canvas folders are one-level visual groups. They have no execution status, ports, nesting, run, cancel, or retry behavior. Generating image types persist as one group with one prompt node and N image nodes; evidence types persist as unbound `image_asset` placeholders.
- Graph compiler runtime inputs include only facts, references, briefs, and visual guidance that arrive on the target node's incoming edges. Disconnecting an edge removes that input; the compiler does not scan the rest of the graph.
- Run scope is `graph`, `node`, `to_node`, or `selection`. `force` is valid only for `node|to_node|selection` with explicit targets. Shot "run this scene" and "retry failed nodes" each submit one `selection` run.
- A graph may have one `running` WorkflowGraphRun; further submits become FIFO `queued` runs snapshotted at dequeue. Duplicate same-scope same-target requests merge.
- Node run statuses are `queued|running|succeeded|failed|unknown|skipped|cancelled`. Unchanged digest or frozen nodes finish as `skipped` and count as ready for downstream. Cancel writes `cancelled`.
- One `WorkflowGraphRun` worker may call providers on independent processing nodes at the same time, limited by `generation_max_concurrent_tasks`. A failed or unknown node does not fail siblings with no dependency path from it. Downstream of a failed or unknown processing node is marked failed. Cancelling the run still stops remaining queued and running nodes.
- Create-time uploads bind as `image_asset` nodes with role `product_identity`. That role string is the value sent to prompt and image providers.
- Recipes are created only by an explicit user save from a live schema-v3 graph (full graph, group, or selection). Applying one to another product first previews the nodes and edges that will appear, then one confirm writes that product's live graph through Graph Command. A full recipe cannot merge into a product that already has a live graph. Fragment recipes merge into an existing v3 graph or return an explicit conflict. Recipes do not store product identity, bound assets, generated results, or media bytes.

## Product Image Invariants

- Uploads, workflow results, explicit image-session attachments, and delivery renditions use the canonical media model.
- The product library contains every current and historical product image result; the product does not assign automatic reject/draft status to successful images.
- System directories are query projections. User folders are one level deep and do not replace system classification.
- Deleting a user folder removes organization only. It does not delete assets or break node, cover, lineage, or rendition references.
- The current product-scoped Agent lists bounded product-library metadata and inspects only selected images. The target global Agent must keep the same bound for global and workflow sub-libraries; no Agent Turn receives an entire library.
- Product cover is display metadata. Changing it does not change product facts or workflow reference bindings.

## Gallery And Library Terms

- 收藏画廊是连续生图结果的收藏视图。它保存一条图片结果引用，并展示该结果所属轮次的提示词、尺寸、模型、供应商和候选信息；它不是独立的提示词参数库。
- 工作流子图库是全局图库的工作流作用域关联和使用集合。`WorkflowMediaLibraryAsset` 只保存关联；工作流节点、封面、参考绑定和交付 lineage 使用稳定的工作流侧图片身份，并可追溯到全局素材身份，不复制媒体 bytes。
- 全局图库限同一商家，是跨会话、可归档、可跨工作流复用的长期图片集合。`/media-library` 是全局入口。旧 `/gallery` 路由和 `ImageGalleryEntry` 不是当前产品；残留重定向、物理表和 bridge 删除，不写回填。
- 配方库保存可复用的工作流结构和配置，不等同于收藏画廊，也不保存商品图片或生成结果。
- 工作流生成后仍然是用户可以直接编辑和执行的生产工具。关闭或从未打开 Agent 对话时，添加节点、连线、检查器、绑定、运行、取消、重试、撤销和配方必须保持可用。Turn 的 running / unknown / failed 不得锁整张画布。Agent 写入与人写入走同一套 Graph Command；人可以立刻继续改刚被 Agent 改过的节点。Agent 可以辅助配置、检查、批量安排和解释执行结果，但不能取代工作流画布、运行按钮、节点重试和人工选择。
- `WorkflowGraphRun` 是独立的业务执行记录。用户从工作流页面点击执行可以直接创建它，不需要先创建 Agent Session 或 Agent Task；Agent 代为请求执行时也必须复用同一套工作流业务约束。
- Agent Session、Agent Task、WorkflowGraphRun 和图片生成会话分别表达长期交流、业务目标、工作流执行和连续生图，不能通过重命名一个现有对象来合并这些职责。
- Agent Session 和 Agent Task 保存有界 operational summary 供 Dock、列表和恢复索引使用；main 的 Pi session files 保存模型 loop 与 compaction 所需 runtime state。合帧后的 journal（含 `text.chunk`）先写入 PostgreSQL `agent_turn_events`，浏览器对话 SSE 由 Go 鉴权并只读取该表；agent-service 不提供本地事件流端点。终态和断线重连都从同一 PG 游标回放。控制面走 `GET /api/v2/agent-control/events`。后台 durable Task、跨进程 claim 和全量 effect reconciliation 仍属于 `exp` 实验方向。Task 可以在首轮 Turn 或等待回答/确认时暂停，运行中的模型 Turn 和 WorkflowRun 继续通过现有取消链处理。

## Mainline Scope

The online product is schema-v3 graphs, Agent-first create, the workbench, media library, and current provider settings. Retired V1/v2 editors, executors, WorkflowDraft writes, archive/cutover gates, Gallery backfill, and old-JSON readers are not product requirements. Do not add compatibility shims, dual serializers, or migration commands **for those retired paradigms**—delete leftover paths instead of wrapping them. This ban does **not** forbid the official stable-release operating-data upgrade path (`productflow-migrate` / `schema.Apply`, documented under [`release/README.md`](release/README.md) for N→N+1).

## Documentation Map

- `docs/README.md`: documentation ownership.
- `docs/PRD.md`: current user-facing product contract.
- `docs/USER_GUIDE.md`: page operations; `web/src/pages/HelpPage.tsx` is the in-product projection and must change in the same commit.
- `docs/ARCHITECTURE.md`: current implementation structure and data flow.
- `docs/ROADMAP.md`: directions that are not yet product fact.
- `docs/adr/`: historical archive only. Do not read it for current design. Current runtime shape is `docs/ARCHITECTURE.md`.
- `AGENTS.md`, `go/AGENTS.md`, `web/AGENTS.md`: how to change this repository.

Code, tests, migrations, and runtime behavior remain the final source of current implementation truth. Documentation must be corrected when they disagree.
