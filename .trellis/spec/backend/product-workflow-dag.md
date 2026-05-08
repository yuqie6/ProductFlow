# Backend Product Workflow DAG Guidelines

> Executable contracts for the ProductFlow-native product workbench DAG.

## Scenario: Canvas template v1 contract

### 1. Scope / Trigger

- Trigger: any change to canvas template models, built-in ecommerce templates, template catalog endpoints, template
  application, user-saved node-group templates, or frontend palette DTOs.
- Canvas templates describe reusable ecommerce production plans. Applying a template must create normal visible
  workflow nodes and edges that can be edited, connected, and executed through the existing product workflow DAG.
- Templates may express downstream iteration by adding later nodes such as `image_generation -> reference_image`; they
  must keep the graph acyclic.

### 2. Signatures

- Backend module: `productflow_backend.application.canvas_templates`.
- Template models:
  - `CanvasTemplate`
  - `CanvasTemplateNodeSpec`
  - `CanvasTemplateEdgeSpec`
  - `CanvasTemplateScenarioMetadata`
  - `CanvasTemplateOutputSlot`
  - `CanvasTemplateReferenceInputHint`
  - `CanvasTemplateSuggestedConnection`
  - `CanvasTemplateDefaultExternalConnection`
  - `CanvasTemplateScenario`
  - `TemplateKind = Literal["full_canvas", "node_group"]`
- Catalog helpers:
  - `list_builtin_canvas_templates() -> list[CanvasTemplate]`
  - `get_builtin_canvas_template(template_key: str) -> CanvasTemplate`
  - `validate_canvas_template(template: CanvasTemplate) -> None`
- Supported node types for templates are explicitly allowlisted:
  `product_context`, `reference_image`, `copy_generation`, `image_generation`.
- Built-in template keys:
  - `ecommerce-main-image-v1`
  - `ecommerce-sku-variant-image-v1`
  - `ecommerce-model-lifestyle-image-v1`
  - `ecommerce-scene-image-v1`
  - `ecommerce-detail-material-image-v1`
  - `ecommerce-campaign-promotion-image-v1`
  - `ecommerce-white-background-image-v1`

### 3. Contracts

- `CanvasTemplate.version` must be `1`.
- `CanvasTemplate.kind` must be `full_canvas` for a complete canvas starter plan or `node_group` for a reusable group
  that can be appended into an existing workflow.
- `CanvasTemplate.nodes` is required and every node `key` must be unique within the template.
- Node specs are logical template nodes. Application code that materializes them must translate each node spec into a
  real `workflow_nodes` row and each edge spec into a real `workflow_edges` row.
- Node specs may include `config_json` with default instructions, prompt hints, image size, or tool options. Keep these as
  editable workflow-node config, not separate local UI state.
- Only `image_generation` node specs may declare `size`.
- `output_slots` document which `reference_image` nodes receive generated material and how the UI should label those
  outputs.
- `reference_inputs` document which `reference_image` nodes are expected to receive user/product/style images before
  running downstream nodes.
- `suggested_connections` may describe optional UI connection advice, but every suggestion must point to existing template
  node keys and must not connect a node to itself.
- Built-in `node_group` templates may declare `default_external_connections` from `existing_product_context` to template
  copy/image node keys. Applying the template must materialize those declarations as normal visible `workflow_edges`.
  They are not hidden suggestions or frontend-only hints.
- Built-in templates must cover real ecommerce image-production scenarios: main image, SKU/variant, model/lifestyle,
  scene, detail/material, campaign/promotion, and white-background output.
- Templates must not introduce new workflow node types by enum drift. When a new `WorkflowNodeType` is added elsewhere,
  it becomes template-supported only after `SUPPORTED_CANVAS_TEMPLATE_NODE_TYPES` and template tests are updated on
  purpose.

### 4. Validation & Error Matrix

- Template version is not `1` -> `BusinessValidationError("画布模板版本必须是 v1")`.
- Unsupported template kind -> `BusinessValidationError("画布模板类型不支持")`.
- Empty node list -> `BusinessValidationError("画布模板至少需要一个节点")`.
- Duplicate node key -> `BusinessValidationError("画布模板节点 key 不能重复")`.
- Unsupported node type -> `BusinessValidationError("画布模板包含不支持的节点类型")`.
- Non-image node declares `size` -> `BusinessValidationError("只有生图节点可以声明尺寸")`.
- Edge connects a node to itself -> `BusinessValidationError("画布模板连线不能连接到自身")`.
- Edge references a missing source or target node -> `BusinessValidationError("画布模板连线引用了不存在的节点")`.
- Template graph contains a cycle -> `BusinessValidationError` from the workflow DAG topological validator.
- Output slot references a missing node or a non-`reference_image` node ->
  `BusinessValidationError("画布模板输出槽必须引用参考图节点")`.
- Reference input hint references a missing node or a non-`reference_image` node ->
  `BusinessValidationError("画布模板参考输入必须引用参考图节点")`.
- Suggested connection connects a node to itself -> `BusinessValidationError("画布模板连接建议不能连接到自身")`.
- Suggested connection references a missing node -> `BusinessValidationError("画布模板连接建议引用了不存在的节点")`.
- Default external connection on a non-`node_group` template ->
  `BusinessValidationError("只有节点组模板可以声明默认外部连接")`.
- Default external connection references a missing template node ->
  `BusinessValidationError("画布模板默认外部连接引用了不存在的节点")`.
- Default external connection targets a node that is not `copy_generation` or `image_generation` ->
  `BusinessValidationError("画布模板默认外部连接只能接入文案或生图节点")`.
- Unknown built-in template key -> `ValueError("画布模板不存在")`.

### 5. Good/Base/Bad Cases

- Good: main-image template creates product context, copy generation, image generation, generated reference output, and a
  downstream iteration image/output pair. The iteration path remains a downstream DAG branch.
- Good: campaign template contains campaign prompt defaults, poster image size, and an explicit generated output slot.
- Good: node-group template appends a reusable group by materializing normal workflow nodes and remapping template keys to
  database node IDs before creating edges.
- Base: a template can include suggested connections for optional palette guidance without requiring those suggestions to
  be materialized as edges. Default external connections are a separate executable contract and are materialized.
- Base: a template can include multiple output slots when one generation node is expected to fill multiple downstream
  `reference_image` nodes.
- Bad: a template stores a hidden chain in frontend state and creates only one placeholder node in the database.
- Bad: a template uses a new enum value before the explicit template allowlist and tests are updated.

### 6. Tests Required

- Unit test every built-in template with `validate_canvas_template` and assert all built-ins have unique keys.
- Unit test required ecommerce scenario coverage: main image, SKU/variant, model/lifestyle, scene, detail/material,
  campaign/promotion, and white-background.
- Unit test downstream iteration remains acyclic and includes only downstream template edges.
- Unit test validation rejects missing edge references, self-edges, cycles, duplicate node keys, unsupported node types,
  invalid template kind, invalid output slot references, invalid suggested connections, and invalid default external
  connections.
- Unit test direct Pydantic model construction still runs contract validation so bypassing catalog helpers cannot create an
  invalid template instance.
- Regression test `SUPPORTED_CANVAS_TEMPLATE_NODE_TYPES` as an explicit allowlist so future `WorkflowNodeType` additions do
  not silently become template-supported.
- When a template application API is added, integration tests must assert that applying a template persists real
  `workflow_nodes` and `workflow_edges`, preserves DAG validation, and keeps prompt/size defaults editable through normal
  node update endpoints.

### 7. Wrong vs Correct

#### Wrong

```python
template = {
    "key": "main-image",
    "steps": ["copy", "image", "iterate"],
}
```

This loses the node and edge contract, so later code cannot materialize the plan as a real workflow DAG.

#### Correct

```python
CanvasTemplate(
    key="ecommerce-main-image-v1",
    version=1,
    kind="full_canvas",
    nodes=[
        CanvasTemplateNodeSpec(key="copy", node_type=WorkflowNodeType.COPY_GENERATION, title="商品卖点文案"),
        CanvasTemplateNodeSpec(key="image", node_type=WorkflowNodeType.IMAGE_GENERATION, title="生成主图"),
        CanvasTemplateNodeSpec(key="output", node_type=WorkflowNodeType.REFERENCE_IMAGE, title="主图结果"),
    ],
    edges=[
        CanvasTemplateEdgeSpec(source_node_key="copy", target_node_key="image"),
        CanvasTemplateEdgeSpec(source_node_key="image", target_node_key="output"),
    ],
)
```

Keep the template as node and edge specs so application code can persist visible, editable, runnable workflow objects.

## Scenario: Product creation canvas template selection

### 1. Scope / Trigger

- Trigger: changes to `POST /api/products`, product creation use cases, or creation-time canvas template application.
- Product creation may initialize a complete ecommerce output plan, but only by materializing a built-in `full_canvas`
  template into normal persisted workflow rows.
- Creation-time template selection must not implement canvas-internal node-group insertion, user-saved template storage,
  result actions, or material lineage. Those are separate product-workbench capabilities.

### 2. Signatures

- API: `POST /api/products` multipart form.
- Existing required fields remain:
  - `name: str`
  - `image: UploadFile`
- Existing optional fields remain:
  - `reference_images: list[UploadFile] | None`
  - `category: str | None`
  - `price: str | None`
  - `source_note: str | None`
- Creation-time template field:
  - `canvas_template_key: str | None`
- Application entrypoint:
  - `create_product(..., canvas_template_key: str | None = None, ...) -> Product`
- Application helper:
  - `resolve_product_creation_canvas_template(canvas_template_key: str | None) -> CanvasTemplate | None`
  - `materialize_product_workflow_from_template(session, *, product_id: str, template: CanvasTemplate) -> ProductWorkflow`

### 3. Contracts

- Missing, blank, and approved default aliases such as `default`, `basic`, `blank`, or `minimal` preserve the existing lazy
  default workflow behavior. Do not eagerly create a default workflow during product creation for those values.
- Any other key must resolve through the built-in template catalog. Do not accept frontend-only template payloads or
  browser-local template definitions for product creation.
- Only `CanvasTemplate.kind == "full_canvas"` is valid at product creation time.
- Built-in `node_group` templates are appended to an existing canvas by later palette features, not by `POST /api/products`.
- `create_product` owns the SQLAlchemy transaction. Template materialization helpers may `flush` rows to resolve ids, but
  must not `commit` independently.
- Materialized `WorkflowNode` rows copy template `node_type`, `title`, `position_x`, `position_y`, and `config_json`.
- Materialized `WorkflowEdge` rows remap template node keys to persisted node ids and copy `source_handle` /
  `target_handle`.
- The materialized workflow must be active and must use normal workflow tables. Do not store a hidden selected-template
  state that later frontend code interprets locally.
- If the product creation page renders a large preview for a built-in `full_canvas` plan, that preview is a mirror of the
  backend template. Node titles, relative order, edges, and coordinates for shared template keys must be updated with the
  backend template and covered by regression tests.
- Later calls to `get_or_create_product_workflow` must return the active workflow created at product creation and must not
  overwrite it with the lazy default graph.

### 4. Validation & Error Matrix

- `canvas_template_key` missing/blank/default alias -> create product, no eager workflow row.
- Unknown non-default key -> `BusinessValidationError("画布模板不存在")` or equivalent template-missing `400`.
- Built-in key whose template kind is `node_group` -> `BusinessValidationError` with a message explaining product creation
  supports only complete canvas templates.
- Product id missing during materialization -> `NotFoundError("商品不存在")`.
- Product already has an active workflow before materialization -> `BusinessValidationError("商品已有活动画布")`.

### 5. Good/Base/Bad Cases

- Good: creating a product with `ecommerce-main-image-v1` creates one active `ProductWorkflow`, persists all template nodes
  and edges, and the detail workflow endpoint returns that workflow unchanged.
- Good: creating a product with no `canvas_template_key` creates only the product/assets; opening the workflow later
  lazily creates the current default graph.
- Base: frontend can label the key as a merchant-facing output plan such as `商品主图方案`; the submitted value remains the
  backend-recognized `canvas_template_key`.
- Bad: accepting a `node_group` template in product creation and pretending it is a full-canvas starter.
- Bad: creating a default workflow eagerly for blank/default key and changing current lazy behavior without an explicit
  product decision.
- Bad: persisting only `canvas_template_key` on the product and letting the frontend draw non-persisted template nodes.

### 6. Tests Required

- API test default product creation without `canvas_template_key` succeeds and does not eagerly create a workflow.
- API test explicit default alias preserves the same lazy behavior.
- API test valid `full_canvas` key creates an active workflow immediately.
- API test persisted node and edge counts, node types, titles, positions, and config match the selected template.
- Regression test layout-sensitive built-in templates, including the main-image output and downstream iteration node
  coordinates that the creation page preview mirrors.
- API test unknown key returns `400` with template-missing detail.
- API test built-in `node_group` key returns `400` with complete-canvas-only detail.
- Regression test fetching the workflow after template-backed creation returns the existing active workflow id.

### 7. Wrong vs Correct

#### Wrong

```python
product = create_product(...)
product.canvas_template_key = payload.canvas_template_key
session.commit()
```

This stores a hidden selector but does not create editable, runnable workflow rows.

#### Correct

```python
template = resolve_product_creation_canvas_template(canvas_template_key)
product = Product(...)
session.add(product)
session.flush()
if template is not None:
    materialize_product_workflow_from_template(session, product_id=product.id, template=template)
session.commit()
```

Creation-time template application must persist the visible workflow graph in the same product creation transaction.

## Scenario: Product workflow node-group template application

### 1. Scope / Trigger

- Trigger: changes to canvas-internal template insertion APIs, built-in `node_group` templates, or product workflow
  mutation code that materializes template nodes.
- The workbench may append built-in `node_group` templates to an existing active product workflow by creating normal
  persisted workflow rows.
- This scenario does not cover product-creation `full_canvas` selection, user-saved templates, drag-to-canvas authoring,
  or hidden suggested external connections.

### 2. Signatures

- Catalog API: `GET /api/workflow/canvas-templates -> CanvasTemplateListResponse`.
- Catalog summary preview fields:
  - `preview_nodes: list[{key, node_type, title, position_x, position_y}]`
  - `preview_edges: list[{source_node_key, target_node_key}]`
  - `default_external_connections: list[{source, target_node_key, label}]`
- Apply API: `POST /api/products/{product_id}/workflow/template-groups -> ProductWorkflowResponse`.
- Request schema:
  - `template_key: str`
  - `position_x: int`
  - `position_y: int`
- Application entrypoint:
  - `apply_node_group_template_to_workflow(session, product_id, template_key, position_x, position_y) -> ProductWorkflow`
- Shared materialization helper:
  - `materialize_canvas_template_graph(session, workflow, template, position_x_offset=0, position_y_offset=0)`

### 3. Contracts

- The apply API resolves `template_key` through the built-in backend catalog only.
- Catalog summary responses must expose lightweight real graph preview data from `CanvasTemplate.nodes` and
  `CanvasTemplate.edges`. Include node key, type, title, and relative coordinates plus edge source/target keys; do not
  include large prompt seeds, instruction strings, or `config_json` in summary preview data.
- Only `CanvasTemplate.kind == "node_group"` is valid for canvas-internal insertion.
- `full_canvas` templates remain valid only for product creation.
- Applying a node group appends to the product's active workflow and preserves all existing nodes and edges.
- Template node specs are materialized into real `workflow_nodes` rows by copying `node_type`, `title`, `config_json`, and
  relative layout.
- The smallest template `position_x` / `position_y` becomes the anchor that lands at request `position_x` /
  `position_y`; other template nodes keep their relative offsets.
- Template edge specs are materialized as real `workflow_edges` rows only between nodes created in the same template
  application.
- `suggested_connections` are returned by the catalog for UI guidance and must not be silently materialized as external
  workflow edges.
- `default_external_connections` are returned by the catalog as lightweight metadata and are materialized by the apply API
  as real visible `workflow_edges`. For built-in SKU/detail/white-background node groups, the existing active workflow
  `product_context` node connects to the newly created `copy_generation` and `image_generation` nodes so the group can
  inherit product fields and the product source image.
- Built-in `node_group` templates should not contain a `product_context` node, because the active workflow already owns
  the product-context singleton.

### 4. Validation & Error Matrix

- Unknown `template_key` -> `400`, `{"detail": "画布模板不存在"}`.
- `template_key` resolves to `full_canvas` -> `400`, detail explains that canvas insertion accepts node-group templates.
- Missing product -> `404`, `{"detail": "商品不存在"}`.
- Existing product without an active workflow -> `400`; the apply API must not create a workflow implicitly.
- Active workflow without exactly one `product_context` node -> `400`; the apply API must not create a partially usable
  node group.
- Template self-edge, missing edge reference, or cycle -> `400` business validation error.
- Any generated edge that would make the full active workflow cyclic -> rollback and return a `400` DAG validation error.

### 5. Good/Base/Bad Cases

- Good: applying `ecommerce-sku-variant-image-v1` to the default workflow increases node and edge counts, keeps all
  previous node/edge IDs, adds template-internal edges, and adds visible external edges from the existing product context
  to the new copy/image nodes.
- Good: applying a template at `position_x=480`, `position_y=360` places the template's minimum coordinate there while
  preserving relative spacing.
- Base: the UI may render reference input hints and connection suggestions from the catalog.
- Bad: frontend creates local-only template nodes without calling the apply API.
- Bad: applying a `full_canvas` template inside an existing canvas.
- Bad: materializing suggested external connections as hidden edges.

### 6. Tests Required

- API test catalog response includes built-in templates with `kind`, scenario metadata, output slots, reference input
  hints, suggested connections, lightweight default external connections, and real `preview_nodes` / `preview_edges`
  matching the built-in template definitions. Catalog summaries must not expose config/prompt payloads.
- API test successful node-group apply preserves existing nodes and edges.
- API test persisted node count, edge count, node types, titles, config, and shifted positions match the backend template.
- API test created edges connect only newly created template nodes and no self-edge is created.
- API test default external connections persist visible edges from the existing product context node to the new
  copy/image nodes.
- API test missing product-context node returns `400` and does not create a partial node group.
- API test unknown key returns `400` with template-missing detail.
- API test `full_canvas` key returns `400` with node-group-only detail.

### 7. Wrong vs Correct

#### Wrong

```python
workflow.nodes.extend(local_template_nodes)
```

This leaves the template in local memory and loses the persisted DAG contract.

#### Correct

```python
workflow = apply_node_group_template_to_workflow(
    session,
    product_id=product_id,
    template_key="ecommerce-sku-variant-image-v1",
    position_x=480,
    position_y=360,
)
```

The application use case resolves the built-in template, materializes visible workflow rows, validates the full DAG, and
returns the normal `ProductWorkflow`.

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
  `running`, `succeeded`, `failed`, `cancelled`. Any run-status enum expansion must include an Alembic revision that
  adds the PostgreSQL enum value while remaining a no-op for SQLite test databases.
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
  - `POST /api/products/{product_id}/workflow/runs/{run_id}/cancel` returns `ProductWorkflowResponse` after durably
    marking an active run `cancelled`.
  - `POST /api/products/{product_id}/workflow/runs/{run_id}/retry` returns `202 Accepted` after creating/enqueueing a new
    run from a failed run.
  - `DELETE /api/workflow-nodes/{node_id}` returns `ProductWorkflowResponse` after deleting the node and connected edges.
  - `DELETE /api/products/{product_id}` returns `204 No Content` after deleting the product and related persisted data.
- Application entrypoints:
  - `start_product_workflow_run(session, product_id, start_node_id=None) -> WorkflowRunKickoff`.
  - `execute_product_workflow_run(run_id) -> None`.
  - `delete_workflow_node(session, node_id) -> ProductWorkflow`.
  - `delete_product(session, product_id) -> str`.
- Database:
  - `workflow_node_runs` must enforce at most one active row per `node_id` where `status IN ('queued', 'running')`,
    using a partial unique index such as `uq_workflow_node_runs_one_active_per_node`.
  - `workflow_runs` may contain multiple `status = 'running'` rows for the same `workflow_id` when their active
    node-run sets are disjoint.

### 3. Contracts

- Run kickoff is a durable two-step contract:
  1. create/reuse a persisted `running` run plus `queued` node runs and immediately return the refreshed workflow;
  2. enqueue that `workflow_run_id` through Dramatiq/Redis with `enqueue_workflow_run(...)`;
  3. the `run_product_workflow_run` actor executes the selected nodes in a background execution boundary that opens its
     own database session.
- `workflow_runs` is the authoritative state for workflow execution. Redis/Dramatiq messages are recoverable delivery
  attempts; do not use in-process executors or Web-process memory as the source of truth.
- Manual cancel is a durable run-level transition to `cancelled` with `failure_reason = "已取消"`. Queued node runs are
  released back to idle node state; a running node run is marked failed with the same cancel reason because node statuses
  intentionally keep the existing five-value contract.
- Failed workflow runs are retryable through a new run. Retry must not create a duplicate run while any active run already
  owns a queued/running node run in the retry plan.
- Run responses and lightweight status responses expose `is_retryable`, `is_cancelable`, `queue_active_count`,
  `queue_running_count`, `queue_queued_count`, `queue_max_concurrent_tasks`, `queued_ahead_count`, and `queue_position`.
  Queue position for workflow runs is derived from queued node-run state, not Redis delivery metadata.
- API startup must call workflow run recovery for active runs with no node currently running, so a run committed before a
  Redis send or process restart is sent again.
- Worker startup may reset stale `workflow_node_runs.status = 'running'` rows back to `queued` before re-enqueueing their
  parent run. Do not reset recent running nodes on API startup because another worker may still be executing them.
- Duplicate kickoff for the same active node set must return the existing active workflow/run state or be caught by the
  node-run database uniqueness guard and converted back into `created=False`; it must not silently create duplicate
  provider calls for the same node.
- Kickoff for a selected node set that is disjoint from every active run's queued/running node runs may create a separate
  `running` workflow run for the same workflow. A full-workflow kickoff overlaps every node and therefore still reuses an
  existing active run when any node is active.
- Duplicate Redis messages must be idempotent:
  - terminal workflow runs (`succeeded` / `failed` / `cancelled`) are no-ops;
  - runs that already have a non-stale `running` node run are no-ops;
  - claiming a queued node run must be an atomic conditional update so two workers cannot execute the same provider call.
- Background execution must persist every decisive transition: node run `queued -> running -> succeeded/failed`, node
  status, workflow run `succeeded/failed`, output JSON, artifact IDs, `failure_reason`, and `finished_at`.
- Any exception inside or around the background execution boundary must mark the run `failed`; do not leave a stale
  `running` row that causes indefinite frontend polling.
- If all global generation running slots are occupied when a workflow worker tries to claim the next queued node run, the
  worker must leave that node run `queued`, avoid provider calls, and schedule delayed delivery retry. Starting the run
  itself should still succeed and show queued metadata instead of returning a submit-time busy error.
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
- Starting a run whose planned node set overlaps an active run's queued/running node runs -> return existing active
  workflow state; do not create duplicate active node runs.
- Starting a selected-node run whose planned node set is disjoint from active node runs -> create/enqueue a separate
  `running` workflow run for the same workflow.
- Cancelling an active run -> persist `status = 'cancelled'`, set `finished_at`, release queued nodes from queued/running
  UI state, and make duplicate worker delivery a no-op.
- Cancelling a terminal succeeded/failed run -> `400`, `已结束的工作流运行不能取消`.
- Retrying a failed run while no active run exists -> create/enqueue a new run and keep the failed run retryable in
  history.
- Retrying while another active run owns any retry-plan node -> `400`, `相关节点运行中，不能重试`.
- Concurrent duplicate active node-run insert hits the partial unique index -> rollback, reload existing overlapping
  active run, return it.
- Global running capacity full during worker claim -> keep the workflow run `running`, keep the next node run `queued`, do
  not call providers, and enqueue delayed retry of the same `workflow_run_id`.
- Redis enqueue failure after the run has been created -> mark the run `failed`, release active node-run uniqueness slots,
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
- Good: two duplicate run requests for the same selected node result in one active run and one provider execution path.
- Good: two selected-node run requests for graph-disjoint nodes can create two active workflow runs, while the database
  still prevents the same node from being queued/running twice.
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
- Duplicate active-node regression asserts a second kickoff for the same planned node set returns/reuses the same active
  run and that direct duplicate node-run insertion violates the unique active-node guard.
- Disjoint active-node regression asserts two selected-node kickoffs with no shared required nodes create distinct running
  workflow runs.
- Failure-path regression should force execution failure and assert stale `running` runs are marked `failed`.
- Workflow image-generation regressions should cover provider timeout cleanup, safe provider-failure reason sanitization,
  and the `run_product_workflow_run` actor failsafe `time_limit`.
- Durable delivery regressions should assert kickoff sends a Dramatiq workflow message, enqueue failure returns `503` and
  leaves no stranded active run, startup recovery requeues queued workflow runs, stale running node runs are reset only on
  worker recovery, and duplicate messages no-op for terminal/currently-running runs.
- Node deletion regression asserts connected edges and node runs are removed and active-run deletion is rejected.
- Product deletion regression asserts completed products are deleted, direct detail fetch returns `404`, and active
  workflow runs block deletion with the expected concise error.
- Alembic upgrade must replace the workflow-level active-run unique index with the active node-run unique index and first
  close historical duplicate queued/running node-run rows if present. Downgrade must close duplicate running workflow runs
  before restoring the workflow-level unique index.

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

The workflow-level check blocks disjoint node runs and can still race with another request before commit.

#### Correct

```python
Index(
    "uq_workflow_node_runs_one_active_per_node",
    "node_id",
    unique=True,
    postgresql_where=text("status IN ('queued', 'running')"),
    sqlite_where=text("status IN ('queued', 'running')"),
)
```

Plan the node IDs first, reuse only overlapping active runs for normal control flow, enforce the same-node active invariant
in the database, and handle `IntegrityError` by reloading the existing overlapping active run.

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
