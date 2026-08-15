# Backend Product Workflow DAG Guidelines

## Scope

Read this guide before changing:

- `application/workflow_drafts/`
- `application/product_workflow/`
- `presentation/routes/workflow_drafts.py`
- workflow, prompt-artifact, generation, recipe, folder, or delivery models
- Dramatiq workflow execution and recovery

The online workflow contract is schema version 2. Historical Alembic revisions are migration inputs and must not shape runtime branching.

## Ownership

- `domain/workflow_rules.py` owns database-free DAG ordering and readiness rules.
- `application/workflow_drafts/` owns Draft revisions, confirmation, materialization, and reveal events.
- `application/product_workflow/v2_*.py` owns current graph mutation, node editing, execution, runs, folders, references, and recipes.
- `application/product_workflow/execution.py` is the shared current execution facade used by workers.
- `presentation/routes/workflow_drafts.py` maps typed request/response DTOs and business failures.
- `infrastructure/db/models.py` owns persistence shape only.

Do not put graph validation, provider construction, or multi-step transactions in route handlers.

## Schema Contract

`ProductWorkflow.schema_version` and `WorkflowNode.schema_version` are exactly `2`.

Current node types:

| Type | Responsibility |
|---|---|
| `product_context` | Product facts and shared visual-system context |
| `reference_image` | One explicit ProductImageAsset binding |
| `prompt_generation` | Editable, versioned per-image prompt generation |
| `image_generation` | GenerationSpec, execution, and current output binding |

`WorkflowFolder` is a visual grouping boundary. Folder membership and translation affect canvas organization, not graph execution semantics.

## Graph Rules

- Every node, edge, folder, and run is scoped to one ProductWorkflow.
- Every referenced ProductImageAsset is scoped to the workflow's Product.
- An edge endpoint must exist in the same workflow.
- Self-edges and cycles are rejected.
- Graph ordering is deterministic. Position may break ties only after dependency rules.
- A product context node is the workflow context authority.
- Node deletion must reject or intentionally cascade dependent edges according to the application contract.
- Layout edits use the workflow edit-version conflict boundary.

Use `topological_node_ids` and `ready_workflow_node_ids` instead of local graph algorithms.

## Draft Contract

WorkflowDraft is append-only at the revision boundary:

1. Create a Draft for one Product.
2. Append validated revision payloads.
3. Mark the latest intended revision ready for confirmation.
4. Confirm an explicit revision.
5. Materialize exactly once for an idempotency key.
6. Persist reveal events for folders, nodes, edges, and completion.

A revision may contain product facts, visual-system payload, image-type plans, per-image prompts, reference bindings, GenerationSpec, DeliverySpec, folders, nodes, edges, and an optional recipe seed.

Confirmation and materialization must detect:

- stale revision id;
- unresolved required fact conflict;
- missing or cross-product reference;
- invalid visual-system reference;
- duplicate node key;
- invalid edge or cycle;
- quantity or graph limits;
- repeated idempotency key with a different request.

Materialization is one owning transaction. Do not commit half a workflow and repair it from the route.

## Visual-System and Prompt Lineage

- `VisualSystem` identifies the product/workflow visual system.
- `VisualSystemVersion` is immutable after publication.
- `ImagePromptArtifact` identifies one prompt lineage.
- `ImagePromptArtifactVersion` records the exact prompt payload and references used for a run.
- `VisualException` records explicit per-image departures from shared visual rules.

Prompt generation reads confirmed product facts, the active visual-system version, the node's image goal, upstream references, and explicit exceptions. It does not infer missing product assets from unrelated library entries.

An image-generation run stores the exact prompt-artifact version and reference asset ids it consumed. Re-running after edits creates new lineage; it does not mutate the record of an earlier run.

## Reference Binding

Reference nodes bind one `ProductImageAsset.id` through `bound_image_asset_id`.

- Binding validates product ownership.
- Clearing a binding leaves the library asset intact.
- Deleting a bound asset follows the canonical gallery deletion contract and must reject active references.
- Product cover has no implicit reference meaning.
- Agent-created and manually-created reference nodes use the same binding API.

Do not copy media bytes or storage paths into node config.

## Node Editing

Node edit endpoints use node-specific typed payloads:

- Product context updates allowed product-facing metadata only.
- Reference editing changes the explicit asset binding.
- Prompt editing updates prompt instruction and artifact state.
- Image generation editing validates GenerationSpec and DeliverySpec.

GenerationSpec invariants include:

- valid aspect-ratio and resolution tier;
- explicit quality and reference-fidelity intent;
- background intent supported by the contract;
- `text_policy=required` requires a nonblank language;
- `text_policy=none` has no text language;
- dimensions stay within runtime maximums.

Preserve optimistic concurrency fields and return the canonical updated projection after a mutation.

## Execution

Whole-workflow execution:

1. Validate the graph.
2. Create WorkflowRun and target WorkflowNodeRun rows.
3. Queue nodes whose in-run dependencies are already satisfied.
4. Execute prompt and image nodes through resolved provider bindings.
5. Persist outputs and mark newly ready downstream nodes.
6. Finish the WorkflowRun when every target is terminal.

Single-node execution computes the required current scope and uses the same execution path. It must not create a second implementation of provider or lineage logic.

Cancellation:

- marks queued work terminal without provider calls;
- records cancellation intent for running work;
- never rewrites a succeeded node to failed;
- converges the parent run after node state changes.

Retry creates a new attempt/run boundary and preserves earlier records.

## Image Generation

Image generation:

- resolves the current `image` provider binding;
- compiles prompt and references from persisted lineage;
- enforces reference and byte limits before provider invocation;
- persists one ProductImageAsset per returned candidate;
- stores provider/model/request metadata without secrets or data URLs;
- updates the image node's current binding according to the run contract;
- leaves every generated ProductImageAsset available in the product library.

Candidate quantity comes from the Draft image plan or explicit image-session request. It is not accepted as an arbitrary advanced tool field.

## Folders and Recipes

Folder operations:

- create, rename, replace members, translate, and delete;
- validate workflow ownership and non-overlapping constraints implemented by the current service;
- update member positions atomically with translation;
- preserve graph edges.

Recipe sources:

- complete workflow;
- one folder;
- current node selection.

Recipes contain reusable nodes, edges, folder structure, and configuration. They exclude product identity, media bytes, current output ids, run ids, and Agent conversation ids.

Applying a recipe validates source version, maps ids deterministically, reuses the current product context where required, and commits the new graph atomically.

## Delivery Renditions

DeliveryRenditionJob is sourced from a ProductImageAsset and a validated DeliverySpec.

- Equivalent completed requests may reuse a deterministic result.
- Running requests expose queued/running state.
- Output format, crop, dimensions, and source lineage are persisted.
- A rendition never replaces or mutates the source ProductImageAsset.
- Retry preserves the earlier failed job.

## HTTP Boundary

Current endpoints are under `/api/v2`. Important groups:

- product active workflow and node detail;
- node, edge, folder, layout, and reference mutations;
- workflow and node runs;
- WorkflowDraft revisions, confirmation, materialization, and reveal events;
- WorkflowRecipe list, create, apply, rename, and archive;
- ProductImageAsset delivery renditions.

Use domain/application exceptions and the shared presentation error mapping. Avoid raw `ValueError` as a business contract.

## Tests Required

Match coverage to the change:

- graph rule unit tests for cycle, missing endpoint, ordering, and readiness;
- route/application tests for ownership and optimistic concurrency;
- Draft contract and atomic materialization tests;
- prompt/visual/reference lineage tests;
- worker execution, cancellation, retry, and recovery tests;
- folder and recipe round-trip tests;
- provider payload tests for prompt/reference/spec projection;
- delivery rendition idempotency and failure tests;
- migration tests when persistence changes.

Run at minimum:

```bash
uv run --directory backend ruff check src tests
uv run --directory backend pytest
```

Use opt-in PostgreSQL/Redis tests for transaction, queue-recovery, or dialect-sensitive changes.

## Forbidden Patterns

- Creating a default graph during read.
- Branching current runtime behavior by workflow schema version.
- Storing image bytes, base64, data URLs, or secrets in node JSON.
- Treating a folder as an execution subgraph.
- Using product cover as an implicit reference.
- Mutating prior prompt, generation, or run lineage.
- Calling provider clients from routes.
- Committing between rows of one graph mutation.
- Adding a second recipe/template model for the same reuse behavior.
