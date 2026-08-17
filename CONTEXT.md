# ProductFlow Context

## Product

ProductFlow is a single-administrator, single-merchant visual production workspace. A user uploads real product references, selects intended image types and quantities, and works with an Agent to produce a reviewable `WorkflowDraft`. Only an explicit user confirmation may materialize that draft into the online schema-v2 workflow.

The current repository targets a personal live demo and self-hosted deployments. Multi-tenancy, billing, team roles, publication integrations, and long-term SaaS compatibility are outside the current contract. Existing deployed data is still migrated through explicit, auditable operations; upgrade procedures must not depend on resetting the database or storage.

## Online Flow

1. `/products/new` is the only product-creation entry.
2. Image types start unselected. Selecting a type initializes its quantity to 2.
3. Each selected type has a quantity from 1 to 6; the total planned images cannot exceed 30.
4. The user uploads 1 to 6 verified references and is expected to include at least one image that identifies the real product or an authoritative product rendering. The backend deterministically validates count, ownership, bytes, and media format; semantic adequacy remains an Agent/user review responsibility.
5. ProductFlow persists the draft Product, uploaded `ProductImageAsset` records, one `WorkflowDraft`, one product-scoped `AgentConversation`, and its associated `AgentSession` before the first Agent Turn. `AgentSession` is the longer-lived conversation container; the current Turn runtime still uses the product-scoped conversation projection.
6. The Agent asks for missing facts and proposes a versioned, structured Draft. It may suggest plan changes but cannot silently change confirmed user choices or facts.
7. The user confirms an explicit Draft revision. A single application transaction materializes the complete workflow and reveal events.
8. Reveal events control presentation only. Disconnecting or cancelling animation cannot leave a partial business graph.
9. The same Agent conversation continues in the product workbench beside the editable workflow.

## Authorities

- PostgreSQL is authoritative for products, facts, assets, Draft revisions, workflows, recipes, provider configuration, and business job state.
- The Go Agent service journal is authoritative for durable Agent Turns, transcript, questions, tool calls/results, token deltas, and event cursors.
- ProductFlow stores a web projection of Agent state but does not reconstruct a second model transcript.
- `AgentTask` stores one business goal and one task-specific harness run under an `AgentSession`; `AgentTurnProjection` may point to a task and a bounded `AgentPageContextSnapshot`. A route change updates ambient context for later turns and does not rewrite the task goal.
- `MediaObject` identifies immutable media bytes. `MediaLibraryAsset` identifies one global library asset and its provenance. `ProductImageAsset` identifies one image inside a product namespace. `WorkflowMediaLibraryAsset` records a workflow usage association without owning another media copy.
- Workflow nodes and covers reference `ProductImageAsset` ids, never storage paths or parallel-array positions.
- Historical V1 source rows and immutable archives are migration evidence. They are not an online editor or executor.

## Workflow Invariants

- The only online workflow schema is version 2.
- Node types are `product_context`, `reference_image`, `prompt_generation`, and `image_generation`.
- A reference node binds exactly one product image asset.
- One planned output image is represented by one runnable image node. A rerun updates its current asset while previous results remain in the library and run history.
- Product facts, visual systems, prompts, recipes, and execution inputs preserve immutable versions used by prior runs.
- `GenerationSpec` describes model-generation intent. Provider-effective values and measured output remain separately observable.
- `DeliverySpec` describes deterministic rendition work. Changing delivery dimensions or format does not invoke the image model or replace the generated source.
- Canvas folders are one-level visual groups. They have no execution status, ports, nesting, run, cancel, or retry behavior.
- Recipes are created only by an explicit user save. Applying one to another product produces a reviewable Draft before materialization.

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
- 全局图库是跨会话、可归档、可跨工作流复用的长期图片集合。`/media-library` 是当前全局入口；旧 `/gallery` 只负责兼容重定向，在线 `/api/gallery` 已退休。旧 `ImageGalleryEntry` 物理表仍由迁移 reader 有界读取，直到部署级回填、引用审计、观察窗和独立素材库清理闸门完成；清理只允许删除旧 Gallery 行和表，不得删除全局素材、共享媒体或工作流引用。
- 配方库保存可复用的工作流结构和配置，不等同于收藏画廊，也不保存商品图片或生成结果。
- 工作流生成后仍然是用户可以直接编辑和执行的生产工具。Agent 可以辅助配置、检查、批量安排和解释执行结果，但不能取代工作流画布、运行按钮、节点重试和人工选择。
- `WorkflowRun` 是独立的业务执行记录。用户从工作流页面点击执行可以直接创建它，不需要先创建 Agent Session 或 Agent Task；Agent 代为请求执行时也必须复用同一套工作流业务约束。
- Agent Session、Agent Task、WorkflowRun 和图片生成会话分别表达长期交流、业务目标、工作流执行和连续生图，不能通过重命名一个现有对象来合并这些职责。
- Agent Session 和 Agent Task 保存有界 operational summary 供 Dock、列表和恢复索引使用；完整 Agent transcript、tool effect 和 compaction 事实仍由 agent-harness durable journal 保存。Task 可以在首轮 Turn 或等待回答/确认时暂停，运行中的模型 Turn 和 WorkflowRun 继续通过现有取消链处理。
- 收藏画廊条目的旧生命周期跟随连续生图会话资产；删除来源会话的目标行为是移除旧收藏。SQLite 默认关闭外键时不能仅依赖数据库级联，应用删除路径必须显式处理这类旧条目。

## Legacy Cutover State

The online V1 editor, executor, mutation routes, template catalog, and default-DAG creation path have been removed. The repository retains additive archive/backfill tools, a read-only history UI, an Agent rebuild seed, V1 source tables needed for audit, and a durable cutover evidence gate.

This code state does not prove that a production cutover occurred. Production-source audit, canonical mapping reconciliation, archive reconciliation, backup/storage restore rehearsal, active/unknown-run drain, and gate approval are operational evidence that must be produced for each deployment. See `docs/rollout/workflow-v2-cutover.md` and `docs/operations/legacy-v1-cutover.md`.

## Documentation Map

- `docs/README.md`: documentation ownership and minimum reading paths.
- `docs/PRD.md`: current user-facing product contract.
- `docs/ARCHITECTURE.md`: current implementation structure and data flow.
- `docs/adr/`: decisions whose rationale should survive implementation changes.
- `docs/rollout/workflow-v2-cutover.md`: current migration checkpoint and remaining evidence.
- `docs/operations/legacy-v1-cutover.md`: operator commands, stop conditions, and recovery procedure.
- `AGENTS.md`, `backend/AGENTS.md`, `web/AGENTS.md`: executable engineering guidance for coding agents.

Code, tests, migrations, and runtime behavior remain the final source of current implementation truth. Documentation must be corrected when they disagree.
