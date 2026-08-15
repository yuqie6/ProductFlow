# ProductFlow Context

## Product

ProductFlow is a single-administrator, single-merchant visual production workspace. A user uploads real product references, selects intended image types and quantities, and works with an Agent to produce a reviewable `WorkflowDraft`. Only an explicit user confirmation may materialize that draft into the online schema-v2 workflow.

The current repository targets a personal live demo and self-hosted deployments. Multi-tenancy, billing, team roles, publication integrations, and long-term SaaS compatibility are outside the current contract. Existing deployed data is still migrated through explicit, auditable operations; upgrade procedures must not depend on resetting the database or storage.

## Online Flow

1. `/products/new` is the only product-creation entry.
2. Image types start unselected. Selecting a type initializes its quantity to 2.
3. Each selected type has a quantity from 1 to 6; the total planned images cannot exceed 30.
4. The user uploads 1 to 6 verified references and is expected to include at least one image that identifies the real product or an authoritative product rendering. The backend deterministically validates count, ownership, bytes, and media format; semantic adequacy remains an Agent/user review responsibility.
5. ProductFlow persists the draft Product, uploaded `ProductImageAsset` records, one `WorkflowDraft`, and one `AgentConversation` before the first Agent Turn.
6. The Agent asks for missing facts and proposes a versioned, structured Draft. It may suggest plan changes but cannot silently change confirmed user choices or facts.
7. The user confirms an explicit Draft revision. A single application transaction materializes the complete workflow and reveal events.
8. Reveal events control presentation only. Disconnecting or cancelling animation cannot leave a partial business graph.
9. The same Agent conversation continues in the product workbench beside the editable workflow.

## Authorities

- PostgreSQL is authoritative for products, facts, assets, Draft revisions, workflows, recipes, provider configuration, and business job state.
- The Go Agent service journal is authoritative for durable Agent Turns, transcript, questions, tool calls/results, token deltas, and event cursors.
- ProductFlow stores a web projection of Agent state but does not reconstruct a second model transcript.
- `MediaObject` identifies immutable media bytes. `ProductImageAsset` identifies one image inside a product namespace.
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
- The Agent lists bounded metadata and inspects only selected images. It never receives the entire product library in one Turn.
- Product cover is display metadata. Changing it does not change product facts or workflow reference bindings.

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
