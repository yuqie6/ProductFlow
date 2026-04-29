# Backend Product Workflow DAG Guidelines

> Executable contracts for the ProductFlow-native product workbench DAG.

## Scenario: Product workflow DAG persistence and execution

### 1. Scope / Trigger

- Trigger: any change to product workbench DAG persistence, node execution, run history, or artifact write-back.
- This is a cross-layer and database-backed feature: SQLAlchemy models, Alembic migrations, Pydantic schemas, API routes,
  frontend DTOs, and workflow tests must stay in sync.

### 2. Signatures

- Tables:
  - `product_workflows(product_id, title, active)` with one active workflow per product.
  - `workflow_nodes(workflow_id, node_type, title, position_x, position_y, config_json, status, output_json, failure_reason)`.
  - `workflow_edges(workflow_id, source_node_id, target_node_id, source_handle, target_handle)`.
  - `workflow_runs(workflow_id, status, started_at, finished_at, failure_reason)`.
  - `workflow_node_runs(workflow_run_id, node_id, status, output_json, copy_set_id, poster_variant_id, image_session_asset_id)`.
- APIs:
  - `GET /api/products/{product_id}/workflow`
  - `GET /api/products/{product_id}/workflow/status`
  - `POST /api/products/{product_id}/workflow/nodes`
  - `PATCH /api/workflow-nodes/{node_id}`
  - `PATCH /api/workflow-nodes/{node_id}/copy`
  - `POST /api/workflow-nodes/{node_id}/image`
  - `POST /api/workflow-nodes/{node_id}/image-source`
  - `POST /api/products/{product_id}/workflow/edges`
  - `DELETE /api/workflow-edges/{edge_id}`
  - `POST /api/products/{product_id}/workflow/run`
- Provider contracts:
  - `TextProvider.generate_copy(product, brief, instruction=None, reference_images=None)` receives connected
    `ReferenceImageInput` values with `path`, `mime_type`, `filename`, `role`, and `label`.

### 3. Contracts

- Supported product node types are exactly mirrored in frontend types:
  `product_context`, `reference_image`, `copy_generation`, `image_generation`.
- Legacy PostgreSQL databases may already have older enum values. Forward migrations must safely add `reference_image`
  and migrate old image-slot rows to it; fresh databases should create only the supported simplified node values.
- Node status values are `idle`, `queued`, `running`, `succeeded`, `failed`; run status values are
  `running`, `succeeded`, `failed`.
- Active workflow status polling must use `GET /api/products/{product_id}/workflow/status`, not repeated full workflow
  detail loads. The status endpoint returns workflow identity/timestamps, node status fields, latest run status fields,
  and node-run status fields only; it must not serialize edges, node `config_json`, node `output_json`, or node-run
  artifact fields. The status query should load only the ORM columns needed for those status DTOs and avoid eager-loading
  product artifacts or full DAG relationships.
- `reference_image` nodes are user-visible `参考图` slots. They can be manually uploaded into through
  `POST /api/workflow-nodes/{node_id}/image`, filled from an existing product image through
  `POST /api/workflow-nodes/{node_id}/image-source`, or filled by upstream `image_generation` nodes.
- Each `reference_image` node is a single current-image slot. Manual upload and upstream `image_generation` fill must
  replace that node's current `config_json.source_asset_ids`, `output_json.source_asset_ids`, `output_json.image_asset_ids`,
  and `output_json.images` with the new single asset. Do not delete the old `source_assets` row; it remains product history
  and can still be downloaded from artifact views.
- `POST /api/workflow-nodes/{node_id}/image-source` accepts exactly one of `source_asset_id` or `poster_variant_id`.
  SourceAsset-backed requests directly bind the existing same-product `reference_image` SourceAsset without creating a
  duplicate upload. If that SourceAsset has `source_poster_variant_id`, preserve that poster-source metadata in the filled
  reference node output. PosterVariant-backed requests first look for a same-product `reference_image` SourceAsset whose
  `source_poster_variant_id` matches the poster, then fall back to workflow output pairings from
  `generated_poster_variant_ids` / `filled_source_asset_ids`; if none exists, copy/materialize the poster file into a new
  `reference_image` SourceAsset named `poster-{poster_variant_id}.*` with `source_poster_variant_id` set, then bind it.
  The filename convention is legacy compatibility only; current de-duplication should use explicit
  `source_poster_variant_id` so a user-uploaded reference image with the same filename is not hidden or rebound as a poster
  copy.
- `reference_image` nodes store image material as first-class `source_assets` rows and expose `source_asset_ids` /
  `image_asset_ids` in workflow output JSON for downstream image nodes.
- `copy_generation` nodes must collect connected upstream `reference_image` slots and pass their asset paths plus
  role/label metadata to the text provider. Text-only providers should include concise reference metadata in the prompt;
  multimodal-capable providers may also attach image payloads/paths.
- A generated `copy_generation` output is editable through `PATCH /api/workflow-nodes/{node_id}/copy`. The endpoint
  updates the underlying `CopySet` using the same validation semantics as normal copy editing, then rewrites the node
  output summary fields so downstream image nodes read the edited copy through the existing `copy_set_id`.
- Manually edited copy node outputs should be treated as the selected copy for downstream runs. Re-running a downstream
  image node must not silently replace that edited `CopySet` with a fresh generated copy before image generation.
- `image_generation` nodes collect incoming edge context, including upstream copy text and reference-image outputs. They
  are trigger/config nodes, not image-bearing artifact slots; generated images must be viewed/downloaded from linked
  downstream `reference_image` nodes or normal product artifact history, not from the `image_generation` node card.
- Generated-mode provider prompts expose visual-subject policy through the runtime-configurable
  `prompt_poster_image_reference_policy` placeholder, not hidden provider code. The default policy treats the first/source
  image as the primary visual subject when present, while upstream copy is auxiliary selling-point/layout context. This is
  important when product text is weak such as a default name `商品` and copy generation may otherwise invent an unrelated
  role, IP, brand, or ad theme.
- Image prompt mode is determined by explicit copy linkage, not by whether a fallback `CopySet` exists for persistence.
  If `image_generation.config_json.copy_set_id` or an upstream `copy_generation.output_json.copy_set_id` points to a
  same-product `CopySet`, provider input must set `PosterGenerationInput.copy_prompt_mode = "copy"` and use the poster/copy
  image template. If no explicit copy link exists, create the workflow-local draft `CopySet` as needed for
  `PosterVariant.copy_set_id`, but set `copy_prompt_mode = "image_edit"` so provider prompts use the no-copy image-edit
  template and do not require title/selling-points/headline/CTA semantics.
- A connected upstream `product_context` node contributes the product source image asset and product fields to
  `image_generation` image context. For image-generation context only, "upstream" includes direct edges and transitive
  ancestors such as `product_context -> copy_generation -> image_generation`; this preserves product context for older or
  manually rewired canvases that no longer have a direct `product_context -> image_generation` edge. Use the node output
  `source_asset_id` when available and fall back to the product's current original source asset so direct selected
  image-node runs do not require re-running product context only to get image context. Deduplicate with other reference
  assets before provider/render input construction. A totally disconnected image-generation node remains free-form and
  must not implicitly inherit product context.
- Image-generation count is driven by graph structure: an `image_generation` node connected to N downstream
  `reference_image` slots generates N images and fills those slots. If no downstream reference slot is connected, the node
  must fail with a clear user-facing message asking the user to connect at least one image/reference node before running.
- Count downstream `reference_image` slots by unique target node id, not by raw edge count. Duplicate edges from the same
  `image_generation` node to the same `reference_image` slot must not multiply generated images or overwrite the slot
  multiple times in one run.
- When N > 1, provider/render calls for the N target images should be initiated concurrently. Persist the returned
  `PosterVariant` and downstream `SourceAsset` rows in the owning SQLAlchemy session after provider calls return; do not
  share one SQLAlchemy `Session` across provider threads.
- Resolve runtime settings and construct provider/renderer dependencies before starting provider/render worker threads, so
  those threads do not open SQLAlchemy sessions just to read config while images are being generated.
- `image_generation` node config may override provider size with `size`; application contracts must carry this as
  `PosterGenerationInput.image_size` and providers should prefer it over global runtime defaults.
- `image_generation` node config may override image-generation tool parameters with `tool_options`; application contracts
  must carry this as `PosterGenerationInput.tool_options`, and generated-mode providers should pass it into their image
  client/tool builder after normalizing blank/null values.
- Generated images should still be persisted as first-class `poster_variants` for history and as `source_assets` on the
  downstream `reference_image` slots. Keep only workflow-boundary summaries and internal generated-poster IDs in
  `image_generation.output_json`; do not expose `poster_variant_ids` there as the preview/download contract.
- `product_context` node config may override/fill `name`, `category`, `price`, and `source_note`; downstream
  `ProductInput.source_note` and `PosterGenerationInput.source_note` must use that effective node context and propagate it
  to text and image providers.
- Product context resolution must prefer the latest saved `product_context.config_json` over stale `output_json` from an
  older run. Direct selected-node runs should not require re-running the product-context node just to see saved edits.
- Copy/image node outputs may expose compact `context_summary` and `context_sources` for UI/tests. These summaries should
  name source nodes and concise upstream text/reference metadata, not full rendered provider prompts or provider payloads.

### 4. Validation & Error Matrix

- Missing product/workflow/node/edge -> `ValueError("...不存在")`, mapped to HTTP `404`.
- Existing-image fill without exactly one `source_asset_id` / `poster_variant_id` -> `400`.
- Existing-image fill on a non-`reference_image` node -> `400`.
- Existing-image fill with a source asset or poster outside the workflow product -> `404`.
- Poster fill when the backing file is missing or storage resolution fails -> `400` with `海报文件不存在`.
- Edge source/target outside the product workflow -> `400` with a user-readable validation detail.
- Self-edge -> `400`.
- Cyclic graph -> `400` and no edge persisted.
- Image generation without a connected product context/source image -> blank/free image generation remains valid when the
  node has at least one downstream `reference_image` target; only explicitly connected missing/broken image references
  should fail.
- Copy generation without a connected product context -> run against the user's node instruction/upstream context as
  free-form copy using a neutral placeholder subject; do not silently fall back to `workflow.product` fields or the
  product source image.
- Image generation without usable explicit copy link -> create a workflow-local draft `CopySet` from product context and
  image instruction for artifact linking, then generate the image with `copy_prompt_mode = "image_edit"`. Do not fail only
  because a copy node is absent, and do not route this no-copy path through the poster/copy prompt template.
- Image generation without downstream `reference_image` targets -> fail the node/run with a concise message such as
  `请先把生图节点连接到至少一个图片/参考图节点，再运行图片生成`; do not silently place output on the
  `image_generation` node.

### 5. Good/Base/Bad Cases

- Good: run default DAG `product_context -> copy_generation -> image_generation -> reference_image`; it produces one draft
  `CopySet`, one generated `PosterVariant` history row, fills the downstream reference slot with a `SourceAsset`, and writes
  run history.
- Good: delete all downstream reference nodes, then run the image node; it fails before provider generation and tells the
  user to connect at least one image/reference node.
- Good: connect an uploaded style `reference_image` into a `copy_generation` node; the generated copy reflects the
  reference label/role and the provider receives explicit `ReferenceImageInput` metadata.
- Good: edit a copy node's generated title/selling points/headline/CTA; the persisted `CopySet` and node output update
  together, and the downstream image node keeps referencing the same edited `copy_set_id`.
- Good: connect one uploaded `reference_image` into `image_generation`, then connect the image node to two downstream
  `reference_image` slots; one run creates two generated images and fills both slots.
- Base: if duplicate edges accidentally connect one image node to the same downstream `reference_image` slot, one run still
  generates one image for that unique slot, not one image per duplicate edge.
- Base: choosing an existing SourceAsset for a different reference slot reuses the same `source_asset_id` and does not add
  another `source_assets` row.
- Base: choosing a product poster not backed by a workflow-filled SourceAsset materializes one new reference SourceAsset and
  updates only the selected reference node's current slot. Reusing the same poster again should reuse that materialized
  SourceAsset via the SourceAsset's `source_poster_variant_id`, even after the original reference node has been filled with
  another image.
- Base: run from a selected node; the executor runs the selected node and only missing/invalid required dependencies.
  Previously succeeded upstream nodes with valid first-class artifacts are read as context, not re-run.
- Base: selected image-node runs may leave upstream `product_context` node status as `idle`; that node is reusable static
  context, so provider input must read its latest saved config/source image directly instead of depending on a current
  `WorkflowNodeRun` or fresh `output_json`.
- Base: selected-node execution planning is a DB-free domain rule fed by an application/query-layer reusable-edge
  decision. The domain rule decides which missing upstream node types are required; the query layer decides whether an
  existing `CopySet`, `PosterVariant`, or `SourceAsset` actually belongs to the workflow product.
- Bad: add an edge from an image node back to a copy node; the cycle validator rejects it before commit.

### 6. Tests Required

- Enum storage test includes workflow node/run enums and asserts database values equal enum `.value` strings.
- API regression creates a product with only name + image, loads the workflow, updates `product_context` node config with
  `source_note`/category/price, runs the DAG, and asserts the effective node context reaches `CopySet`, generated image
  input, node output, and run history.
- API regression rejects creating a second `product_context` node and verifies opening an active workflow normalizes duplicate
  product-context nodes down to one.
- API regression deletes downstream reference nodes and runs an image node directly, asserting a failed run/node with the
  clear connect-a-target message and no silent image output on the image node.
- API regression for node-first canvas creates/uses a `reference_image` node, uploads an image, connects it to
  `image_generation`, connects image generation to multiple downstream `reference_image` slots, runs the workflow, and
  asserts generated poster IDs, filled source asset IDs, size, and slot output are persisted.
- API regression for multi-target image generation asserts multiple downstream reference slots are filled and provider
  generation calls are initiated concurrently while database writes remain in the owning session.
- API regression uploads twice to the same `reference_image` node and asserts the node exposes only the second asset while
  both old and new `source_assets` remain on the product. Another regression fills an already populated reference node from
  an upstream `image_generation` node and asserts the same single-slot replacement behavior.
- API regression binds a reference node from an existing `source_asset_id` and asserts no duplicate SourceAsset is created;
  another regression binds from a `poster_variant_id` and asserts the poster materializes or maps to a reference SourceAsset.
- API/provider regression connects a `reference_image` node into `copy_generation` and asserts the reference label/role
  reaches generated copy/provider input.
- API regression edits a generated copy node through `PATCH /api/workflow-nodes/{node_id}/copy` and asserts both the
  persisted `CopySet` and node output summary fields are updated.
- API regression for selected-node runs first creates successful upstream outputs, then runs a downstream node and asserts
  upstream node runs/artifacts are not duplicated when reusable outputs exist.
- API regression for selected reference-slot runs connects an already successful image node to a new empty
  `reference_image` slot and asserts only the necessary image node plus target slot run; copy generation must not re-run.
- Unit regression for workflow domain rules covers selected-node planning / missing-upstream decisions without creating a
  SQLAlchemy session.
- API regression edits a previously run `product_context` node, then directly runs a downstream node and asserts the
  downstream output context summary uses the latest saved config rather than stale context output.
- API regression asserts upstream copy text and reference-image label/role metadata appear in deterministic context sources
  for image generation.
- API/provider regression asserts the default `product_context -> image_generation` edge contributes the product source
  image to image-generation context, so `context_summary.reference_image_count` does not report `0` when the product image
  is connected through the product-context node, and that copy-linked runs expose `copy_prompt_mode = "copy"`.
- API/provider regression deletes the direct `product_context -> image_generation` edge while retaining
  `product_context -> copy_generation -> image_generation`, then directly runs the image node and asserts the provider
  still receives product fields plus the product source image. A separate regression must keep the disconnected blank
  image-generation path free-form.
- API/provider regression removes the copy node or otherwise runs an image node with no explicit copy link and asserts the
  provider receives `PosterGenerationInput.copy_prompt_mode = "image_edit"` while generated artifacts still have a
  `copy_set_id` for persistence.
- Alembic head upgrade must pass on SQLite after adding workflow tables.

### 7. Wrong vs Correct

#### Wrong

```python
node.output_json = {"copy": copy_payload.model_dump()}
```

This hides the artifact in opaque JSON only; later history cannot reliably reuse it.

#### Correct

```python
session.add(copy_set)
session.flush()
node.output_json = {"copy_set_id": copy_set.id, "summary": copy_set.poster_headline}
```

Persist the first-class artifact, then keep only workflow-boundary references and summaries in JSON.

#### Wrong

```python
posters = [poster for poster in workflow.product.poster_variants if poster.id in poster_ids]
```

During one DAG run, relationship collections can be stale after an upstream node has just created new artifacts.

#### Correct

```python
posters = session.scalars(select(PosterVariant).where(PosterVariant.id.in_(poster_ids))).all()
```

For downstream nodes, query first-class artifacts by ID so same-run outputs are visible.

#### Wrong

```python
execution_nodes = ancestors(start_node) | {start_node}
```

This makes every selected-node run regenerate upstream copy/images even when the user only wants to refresh one downstream
node, wasting provider calls and replacing previously accepted artifacts.

#### Correct

```python
execution_nodes = missing_required_dependencies(start_node) | {start_node}
```

For selected-node runs, treat successful upstream nodes with valid `CopySet`, `PosterVariant`, or `SourceAsset` records as
read-only context. Re-run an upstream dependency only when the target cannot be satisfied from existing first-class
artifacts, such as a newly connected empty `reference_image` slot that needs its upstream image node to fill it.

## Scenario: AI provider scalar payload normalization

### 1. Scope / Trigger

- Trigger: any change to AI text provider payload parsing for creative briefs, generated copy, or workflow copy-node
  execution.
- This is a cross-layer contract because provider JSON is parsed into application contracts, persisted into
  `creative_briefs` / `copy_sets`, emitted through workflow node `output_json`, and consumed by frontend typed DTOs.

### 2. Signatures

- Application contracts:
  - `CreativeBriefPayload(positioning: str, audience: str, selling_angles: list[str], taboo_phrases: list[str], poster_style_hint: str)`.
  - `CopyPayload(title: str, selling_points: list[str], poster_headline: str, cta: str)`.
- Text provider methods:
  - `TextProvider.generate_brief(product: ProductInput) -> tuple[CreativeBriefPayload, str]`.
  - `TextProvider.generate_copy(product: ProductInput, brief: CreativeBriefPayload, instruction: str | None = None, reference_images: list[ReferenceImageInput] | None = None) -> tuple[CopyPayload, str]`.
- Persistence/API boundary:
  - `CreativeBrief.payload` and workflow `latest_brief.payload` must expose scalar brief fields as strings.
  - `CopySet.title`, `CopySet.poster_headline`, `CopySet.cta`, and copy-node output summaries must expose scalar copy
    fields as strings.

### 3. Contracts

- AI providers may occasionally return a pure text array for a scalar short-text field. The application contract boundary
  may normalize only these scalar fields by joining items with `、`:
  - `CreativeBriefPayload.positioning`
  - `CreativeBriefPayload.audience`
  - `CreativeBriefPayload.poster_style_hint`
  - `CopyPayload.title`
  - `CopyPayload.poster_headline`
  - `CopyPayload.cta`
- Fields whose contract is already a list must remain lists and must not be flattened:
  - `CreativeBriefPayload.selling_angles`
  - `CreativeBriefPayload.taboo_phrases`
  - `CopyPayload.selling_points`
- Normalization belongs in the application contract layer, before persistence and workflow output construction. Do not
  make frontend DTOs accept `string | string[]` for these fields.

### 4. Validation & Error Matrix

- Scalar field is a normal string -> accepted unchanged.
- Scalar field is a non-empty list of non-empty strings -> normalized to a single string joined by `、`.
- Scalar field is an empty list -> Pydantic `ValidationError`; do not silently store an empty string.
- Scalar field list contains an empty/blank string -> Pydantic `ValidationError`.
- Scalar field list contains an object, number, boolean, or `null` -> Pydantic `ValidationError`; do not coerce with
  `str(...)`.
- List-contract field is not a list or violates min/max length -> Pydantic `ValidationError`.

### 5. Good/Base/Bad Cases

- Good: provider returns `{"audience": ["摄影入门用户", "图文内容创作者"]}`; persisted and API-visible payload uses
  `"摄影入门用户、图文内容创作者"`.
- Good: provider returns `{"title": ["轻巧入门", "随拍即出片"]}`; `CopySet.title` and copy-node output use
  `"轻巧入门、随拍即出片"`.
- Base: provider returns scalar strings for all scalar fields; values pass through unchanged.
- Bad: provider returns `{"audience": []}` or `{"audience": [{"name": "摄影入门用户"}]}`; validation fails instead of
  inventing a display string.

### 6. Tests Required

- Contract regression directly validates `CreativeBriefPayload` and `CopyPayload` with scalar text arrays and malformed
  arrays; assert good arrays are joined with `、` and bad arrays raise `ValidationError`.
- Copy-generation workflow regression monkeypatches the text provider to return scalar arrays and asserts the persisted
  `CreativeBrief.payload` and `CopySet` fields are strings.
- Product workflow DAG regression runs `POST /api/products/{product_id}/workflow/run` with provider scalar arrays and
  asserts copy-node `output_json` and product `latest_brief.payload` expose normalized strings.

### 7. Wrong vs Correct

#### Wrong

```python
payload = response_json
if isinstance(payload["audience"], list):
    payload["audience"] = str(payload["audience"])
```

This leaks Python/JSON list formatting into persisted copy and hides malformed provider output.

#### Correct

```python
CreativeBriefPayload.model_validate(response_json)
```

Keep normalization and malformed-shape rejection inside the application contract validators so all provider entrypoints
and workflow runs share the same behavior.

## Scenario: Async workflow runs and deletion safety

### 1. Scope / Trigger

- Trigger: any change to workflow run kickoff/execution, active-run locking, workflow node deletion, or product deletion
  while workflow/job state may still be active.
- This is a cross-layer and database-backed contract because it spans API responses, background execution, run/node status
  persistence, database uniqueness, frontend polling, and storage cleanup.

### 2. Signatures

- APIs:
  - `POST /api/products/{product_id}/workflow/run` returns `ProductWorkflowResponse` after creating or reusing an active
    `workflow_runs` row; it must not wait for provider execution to finish.
  - `DELETE /api/workflow-nodes/{node_id}` returns `ProductWorkflowResponse` after deleting the node and connected edges.
  - `DELETE /api/products/{product_id}` returns `204 No Content` after deleting the product and related persisted data.
- Application entrypoints:
  - `start_product_workflow_run(session, product_id, start_node_id=None) -> WorkflowRunKickoff`.
  - `execute_product_workflow_run(run_id) -> None`.
  - `delete_workflow_node(session, node_id) -> ProductWorkflow`.
  - `delete_product(session, product_id) -> str`.
- Database:
  - `workflow_runs` must enforce at most one `status = 'running'` row per `workflow_id`, using a partial unique index
    such as `uq_workflow_runs_one_running_per_workflow`.

### 3. Contracts

- Run kickoff is a durable two-step contract:
  1. create/reuse a persisted `running` run plus `queued` node runs and immediately return the refreshed workflow;
  2. enqueue that `workflow_run_id` through Dramatiq/Redis with `enqueue_workflow_run(...)`;
  3. the `run_product_workflow_run` actor executes the selected nodes in a background execution boundary that opens its
     own database session.
- `workflow_runs` is the authoritative state for workflow execution. Redis/Dramatiq messages are recoverable delivery
  attempts; do not use in-process executors or Web-process memory as the source of truth.
- API startup must call workflow run recovery for active runs with no node currently running, so a run committed before a
  Redis send or process restart is sent again.
- Worker startup may reset stale `workflow_node_runs.status = 'running'` rows back to `queued` before re-enqueueing their
  parent run. Do not reset recent running nodes on API startup because another worker may still be executing them.
- Duplicate kickoff for the same active workflow must return the existing active workflow/run state or be caught by the
  database uniqueness guard and converted back into `created=False`; it must not silently create duplicate provider calls.
- Duplicate Redis messages must be idempotent:
  - terminal workflow runs (`succeeded` / `failed`) are no-ops;
  - runs that already have a non-stale `running` node run are no-ops;
  - claiming a queued node run must be an atomic conditional update so two workers cannot execute the same provider call.
- Background execution must persist every decisive transition: node run `queued -> running -> succeeded/failed`, node
  status, workflow run `succeeded/failed`, output JSON, artifact IDs, `failure_reason`, and `finished_at`.
- Any exception inside or around the background execution boundary must mark the run `failed`; do not leave a stale
  `running` row that causes indefinite frontend polling.
- Generated-mode workflow image provider calls must be bounded by
  `workflow_image_generation_provider_timeout_seconds`; timeout or provider failure must fail the run/node with a stable
  safe user-facing reason and must not persist provider keys, base URLs, raw prompts, request bodies, or tracebacks in
  `failure_reason`.
- The `run_product_workflow_run` Dramatiq actor must keep `max_retries=0` and an internal worker failsafe `time_limit`;
  the application execution boundary remains responsible for durable failure state.
- Node deletion must remove connected incoming/outgoing edges and existing `workflow_node_runs` for that node before
  returning the refreshed workflow.
- Product deletion must refuse active workflow runs, then rely on ORM/database cascade for related rows and
  perform best-effort storage tree cleanup after the database delete commits.

### 4. Validation & Error Matrix

- Missing product/workflow/node -> `404`.
- Starting a run while one is already active -> return existing active workflow state; do not create a second active run.
- Concurrent duplicate active-run insert hits the partial unique index -> rollback, reload existing active run, return it.
- Redis enqueue failure after the run has been created -> mark the run `failed`, release the active-run uniqueness guard,
  and return `503` with `任务队列暂不可用，请稍后重试`.
- Workflow image provider timeout -> mark the active run and image node run `failed`, set `finished_at`, use
  `图片生成超时，请稍后重试`, and release global generation queue capacity.
- Workflow image provider failure with raw provider details -> mark failed with a generic safe reason such as
  `图片生成失败，请稍后重试`; never expose secrets or prompt payloads through `failure_reason`.
- Duplicate Redis message for terminal run -> no-op and do not call providers.
- Duplicate Redis message while another worker owns a non-stale running node -> no-op and do not call providers.
- Delete a node while its workflow has an active run, or while the node is `queued` / `running` -> `400` with
  `运行中，稍后删除`.
- Delete a product while any related job is `queued` / `running` -> `400` with `商品任务运行中，稍后删除`.
- Delete a product while any related workflow run is `running` -> `400` with `商品工作流运行中，稍后删除`.
- Missing storage files during product deletion -> ignore for storage cleanup; the database deletion remains authoritative.

### 5. Good/Base/Bad Cases

- Good: `POST /workflow/run` returns quickly with `runs[0].status == "running"` and queued node statuses; polling later
  observes success/failure written by `execute_product_workflow_run`.
- Good: two concurrent run requests for the same workflow result in one active run and one provider execution path.
- Good: deleting a workflow node removes that node plus connected edges, and a refreshed workflow response contains no
  broken edge references.
- Base: deleting a product with completed workflow history cascades workflow rows and then best-effort removes
  `storage/products/{product_id}`.
- Bad: leaving run execution inside the request handler blocks the frontend and hides intermediate committed status.
- Bad: checking active runs only in application code without a database uniqueness guard allows races in concurrent or
  multi-process deployments.

### 6. Tests Required

- API regression for run kickoff asserts the initial response is `running` / `queued`, then waits/polls until the
  background execution writes terminal status and artifacts.
- Duplicate active-run regression asserts a second kickoff returns/reuses the same active run and that direct duplicate
  database insertion violates the unique active-run guard.
- Failure-path regression should force execution failure and assert stale `running` runs are marked `failed`.
- Workflow image-generation regressions should cover provider timeout cleanup, safe provider-failure reason sanitization,
  and the `run_product_workflow_run` actor failsafe `time_limit`.
- Durable delivery regressions should assert kickoff sends a Dramatiq workflow message, enqueue failure returns `503` and
  leaves no stranded active run, startup recovery requeues queued workflow runs, stale running node runs are reset only on
  worker recovery, and duplicate messages no-op for terminal/currently-running runs.
- Node deletion regression asserts connected edges and node runs are removed and active-run deletion is rejected.
- Product deletion regression asserts completed products are deleted, direct detail fetch returns `404`, and active
  workflow runs block deletion with the expected concise error.
- Alembic upgrade must create the active-run unique index and first close historical duplicate running rows if present.

### 7. Wrong vs Correct

#### Wrong

```python
workflow = run_product_workflow(session, product_id=product_id)
return serialize_product_workflow(workflow)
```

This keeps provider execution inside the HTTP request; the frontend sees a long pending mutation and cannot observe
intermediate node status until the request finishes.

#### Correct

```python
kickoff = start_product_workflow_run(session, product_id=product_id)
if kickoff.created:
    enqueue_workflow_run(kickoff.run_id)
return serialize_product_workflow(kickoff.workflow)
```

Persist the run state first, return quickly, and let the frontend poll the persisted workflow state.

#### Wrong

```python
if _active_workflow_run(workflow):
    return workflow
session.add(WorkflowRun(workflow_id=workflow.id, status=WorkflowRunStatus.RUNNING))
session.commit()
```

The application-level check can race with another request before commit.

#### Correct

```python
Index(
    "uq_workflow_runs_one_running_per_workflow",
    "workflow_id",
    unique=True,
    postgresql_where=text("status = 'running'"),
    sqlite_where=text("status = 'running'"),
)
```

Keep the application check for normal control flow, but enforce the invariant in the database and handle `IntegrityError`
by reloading the existing active run.

## Scenario: Product context singleton and direct image generation

### 1. Scope / Trigger
- Trigger: product workflow DAG changes that affect product-context nodes, image-node run prerequisites, or default graph
  shape.

### 2. Signatures
- `POST /api/products/{product_id}/workflow/nodes` rejects `node_type = product_context` when the active workflow already
  has one.
- `POST /api/products/{product_id}/workflow/run` with `start_node_id` pointing at an `image_generation` node requires at
  least one connected downstream `reference_image` target.
- Image-node output with downstream targets contains `generated_poster_variant_ids`, `copy_set_id`, `target_count`,
  `filled_source_asset_ids`, and `filled_reference_node_ids`; it does not expose `poster_variant_ids` as a node-level
  image carrier.

### 3. Contracts
- Each active workflow has exactly one `product_context` node. Runtime opening may normalize older duplicate rows by keeping
  the earliest context node and deleting duplicate context nodes plus their connected edges/node-run rows.
- Default workflows include one product context, one copy node, one image node, and one downstream reference slot. The
  default edge set is `product_context -> copy_generation`, `product_context -> image_generation`,
  `copy_generation -> image_generation`, and `image_generation -> reference_image`.
- Image nodes prefer connected/manual/confirmed copy when present. If absent, the backend creates a draft `CopySet` with
  `provider_name = workflow_context` from product context and the image instruction so `PosterVariant.copy_set_id` remains a
  first-class artifact link.
- Downstream reference slots are required outputs. One image is generated per unique slot and each slot is filled with a
  `SourceAsset`; when absent, no provider call is made and the image node fails with the connect-a-target message.

### 4. Validation & Error Matrix
- Duplicate product-context creation -> `400` with `商品资料节点已存在`.
- Missing/unconnected product source image -> image nodes may still run as blank/free generation when they have a
  downstream reference target and prompt/context; do not fail solely because no product image is connected.
- Missing copy node or missing upstream copy -> create a workflow-context draft `CopySet` if a downstream target exists;
  do not reuse `product.confirmed_copy_set` unless a copy node/config explicitly links it into the image node context.
- Missing downstream reference slot -> fail before generation with the connect-a-target message.
- Duplicate downstream edges to the same reference slot -> one generated image for that unique slot.

### 5. Good/Base/Bad Cases
- Good: selected image-node run after editing product context and image instruction uses the latest saved draft and fills a
  connected downstream reference slot.
- Base: optional copy/reference nodes can be connected and will enrich input/fill slots when present.
- Bad: rendering generated image preview/download on the `image_generation` node itself instead of on filled
  `reference_image` slots.

### 6. Tests Required
- API regression for duplicate product-context rejection.
- API regression for direct image-node run with copy/reference nodes removed.
- Product list regression for source-image thumbnail URL because direct image output is discoverable from list/detail UI.

### 7. Wrong vs Correct
#### Wrong

```python
if copy_set is None:
    raise ValueError("图片生成节点缺少可用文案")
if not downstream_reference_nodes:
    raise ValueError("请先把生图节点连接到至少一个图片/参考图节点，再运行图片生成")
```

#### Correct

```python
if copy_set is None:
    copy_set = _create_context_copy_set(session, product=product, product_context=context, node=node)
targets = downstream_reference_nodes
```
