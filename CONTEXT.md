# ProductFlow Context

## Product

ProductFlow is a single-administrator, single-merchant visual production workspace. A user uploads real product references, selects intended image types and quantities, and works with an Agent to produce a reviewable `WorkflowDraft`. Only an explicit user confirmation may persist that draft as the online schema-v3 graph.

The current repository targets a personal live demo and self-hosted deployments. Multi-tenancy, billing, team roles, publication integrations, and long-term SaaS compatibility are outside the current contract. Existing deployed data is still migrated through explicit, auditable operations; upgrade procedures must not depend on resetting the database or storage.

## Online Flow

1. `/products/new` is the only product-creation entry.
2. Image types start unselected. Selecting a type initializes its quantity to 2.
3. Each selected type has a quantity from 1 to 6; the total planned images cannot exceed 30.
4. The user uploads 1 to 6 verified references and is expected to include at least one image that identifies the real product or an authoritative product rendering. The backend deterministically validates count, ownership, bytes, and media format; semantic adequacy remains an Agent/user review responsibility.
5. Agent-first create can start from a product name. It persists the draft Product, one collecting `WorkflowDraft`, one product-scoped `AgentConversation`, and its associated `AgentSession`. The user uploads 1 to 6 verified references in that conversation and names the image types in text; the Agent persists them as immutable intake. The create form can still collect types and files first as a shortcut, and remains the required path for direct create. Direct create persists the product, references, and schema-v3 graph without a conversation; the workbench later attaches one. Missing image types or references do not block Agent Turns. `AgentSession` is the longer-lived conversation container; the current Turn runtime still uses the product-scoped conversation projection.
6. The Agent asks for missing facts and proposes a versioned, structured Draft. It may suggest plan changes but cannot silently change confirmed user choices or facts.
7. The user confirms an explicit Draft revision. Agent confirmation persists that revision as a schema-v3 graph; the product-create form can also create the same graph directly without a Draft.
8. Draft confirmation and direct create persist a complete schema-v3 graph in one transaction. The workbench reads that graph after persist; it does not assemble the graph incrementally in the browser.
9. The same Agent conversation continues in the product workbench beside the editable workflow. After a live schema-v3 graph exists, the Agent inspects, explains, and may request runs; it does not submit a WorkflowDraft that would replace that graph.

## Authorities

- PostgreSQL is authoritative for products, facts, assets, Draft revisions, workflows, recipes, provider configuration, and business job state.
- ProductFlow PostgreSQL remains authoritative for business state. The main Node.js/Pi adapter owns its session and event files for interactive Agent Turn execution; those files are not a durable business authority and do not prove background recovery, effect reconciliation, or multi-instance claims.
- ProductFlow stores a web projection of Agent state but does not reconstruct a second model transcript.
- `AgentTask` stores one business goal and one task-specific run under an `AgentSession` (the persisted compatibility column is still named `harness_run_id`); `AgentTurnProjection` may point to a task and a bounded `AgentPageContextSnapshot`. A route change updates ambient context for later turns and does not rewrite the task goal.
- `MediaObject` identifies immutable media bytes. `MediaLibraryAsset` identifies one global library asset and its provenance. `ProductImageAsset` identifies one image inside a product namespace. `WorkflowMediaLibraryAsset` records a workflow usage association without owning another media copy.
- Workflow nodes and covers reference `ProductImageAsset` ids, never storage paths or parallel-array positions.
- Historical V1 source rows and immutable archives are migration evidence. They are not an online editor or executor.

## Workflow Invariants

- The only online workflow schema is version 3, stored on `workflow_graphs`.
- Node Catalog owns connection rules and editable config keys. ChangeSet `config` cannot introduce unregistered keys or retired plan keys.
- Node types are `product_source`, `image_asset`, `creative_brief`, `visual_system`, `prompt_generation`, and `image_generation`.
- `creative_brief`, `visual_system`, and `prompt_generation` call the prompt provider when run and write the result into that node's config. `image_generation` calls the image provider. Running a content node does not run downstream image nodes.
- An `image_asset` node binds exactly one product image asset. Binding is not the same as a downstream `reference` edge.
- One planned output image is represented by one runnable image node. A rerun updates its current asset while previous results remain in the library and run history.
- Product facts, visual systems, prompts, recipes, and execution inputs preserve immutable versions used by prior runs.
- `GenerationSpec` describes model-generation intent. Provider-effective values and measured output remain separately observable.
- `DeliverySpec` describes deterministic rendition work. Changing delivery dimensions or format does not invoke the image model or replace the generated source.
- Canvas folders are one-level visual groups. They have no execution status, ports, nesting, run, cancel, or retry behavior. Generating image types persist as one group with one prompt node and N image nodes; evidence types persist as unbound `image_asset` placeholders.
- Graph compiler runtime inputs include only facts, references, briefs, and visual guidance that arrive on the target node's incoming edges. Disconnecting an edge removes that input; the compiler does not scan the rest of the graph.
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
- 全局图库是跨会话、可归档、可跨工作流复用的长期图片集合。`/media-library` 是当前全局入口；旧 `/gallery` 只负责兼容重定向，在线 `/api/gallery` 已退休。当前 schema 的旧 `ImageGalleryEntry` 物理表仍由迁移 reader 有界读取，直到部署级回填、引用审计、观察窗和独立素材库清理闸门完成；旧 `legacy_canvas_agent_20260518_0032` source 通过 Gallery-only manifest bridge 迁移到新图库，Agent archive 不属于该 bridge。清理只允许删除旧 Gallery 行和表，不得删除全局素材、共享媒体或工作流引用。
- 配方库保存可复用的工作流结构和配置，不等同于收藏画廊，也不保存商品图片或生成结果。
- 工作流生成后仍然是用户可以直接编辑和执行的生产工具。Agent 可以辅助配置、检查、批量安排和解释执行结果，但不能取代工作流画布、运行按钮、节点重试和人工选择。
- `WorkflowGraphRun` 是独立的业务执行记录。用户从工作流页面点击执行可以直接创建它，不需要先创建 Agent Session 或 Agent Task；Agent 代为请求执行时也必须复用同一套工作流业务约束。
- Agent Session、Agent Task、WorkflowGraphRun 和图片生成会话分别表达长期交流、业务目标、工作流执行和连续生图，不能通过重命名一个现有对象来合并这些职责。
- Agent Session 和 Agent Task 保存有界 operational summary 供 Dock、列表和恢复索引使用；main 的 Pi session/event files 保存交互式 transcript、tool events 和 compaction 所需 runtime state。后台 durable Task、tool effect reconciliation 和多实例 claim 仍属于 `exp` 实验方向。Task 可以在首轮 Turn 或等待回答/确认时暂停，运行中的模型 Turn 和 WorkflowRun 继续通过现有取消链处理。
- 收藏画廊条目的旧生命周期跟随连续生图会话资产；删除来源会话的目标行为是移除旧收藏。SQLite 默认关闭外键时不能仅依赖数据库级联，应用删除路径必须显式处理这类旧条目。

## Legacy Cutover State

The online V1 editor, executor, mutation routes, template catalog, and default-DAG creation path have been removed. The repository retains additive archive/backfill tools, a read-only history UI, an Agent rebuild seed, V1 source tables needed for audit, and a durable cutover evidence gate.

This code state does not prove that a production cutover occurred. Production-source audit, canonical mapping reconciliation, archive reconciliation, backup/storage restore rehearsal, active/unknown-run drain, and gate approval are operational evidence that must be produced for each deployment. See `docs/rollout/legacy-v1-retirement.md` and `docs/operations/legacy-v1-cutover.md`.

## Documentation Map

- `docs/README.md`: documentation ownership.
- `docs/PRD.md`: current user-facing product contract.
- `docs/USER_GUIDE.md`: page operations; `web/src/pages/HelpPage.tsx` is the in-product projection and must change in the same commit.
- `docs/ARCHITECTURE.md`: current implementation structure and data flow.
- `docs/ROADMAP.md`: directions that are not yet product fact.
- `docs/adr/`: frozen decisions.
- `docs/rollout/` and `docs/operations/`: deployment evidence and operator commands.
- `AGENTS.md`, `backend/AGENTS.md`, `web/AGENTS.md`: how to change this repository.

Code, tests, migrations, and runtime behavior remain the final source of current implementation truth. Documentation must be corrected when they disagree.
