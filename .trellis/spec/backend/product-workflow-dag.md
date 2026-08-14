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
  - `ecommerce-taobao-main-image-v1`
  - `ecommerce-xiaohongshu-image-v1`
  - `ecommerce-multi-angle-image-v1`
  - `ecommerce-sku-variant-image-v1`
  - `ecommerce-feature-infographic-v1`
  - `ecommerce-size-spec-image-v1`
  - `ecommerce-scale-reference-image-v1`
  - `ecommerce-package-checklist-image-v1`
  - `ecommerce-usage-steps-image-v1`
  - `ecommerce-comparison-image-v1`
  - `ecommerce-model-lifestyle-image-v1`
  - `ecommerce-scene-image-v1`
  - `ecommerce-detail-material-image-v1`
  - `ecommerce-campaign-promotion-image-v1`
  - `ecommerce-short-video-cover-v1`
  - `ecommerce-white-background-image-v1`

### 3. Contracts

- `CanvasTemplate.version` must be `1`.
- `CanvasTemplate.kind` must be `full_canvas` for built-in ecommerce scenario templates. `node_group` remains valid for
  user-saved reusable groups.
- `CanvasTemplate.nodes` is required and every node `key` must be unique within the template.
- Node specs are logical template nodes. Application code that materializes them must translate each node spec into a
  real `workflow_nodes` row and each edge spec into a real `workflow_edges` row.
- Node specs may include `config_json` with default instructions, prompt hints, image size, or tool options. Keep these as
  editable workflow-node config, not separate local UI state.
- `application/product_workflow/node_config.py::normalize_workflow_node_config(...)` is the single write-time owner for
  workflow node config. Runtime create/update, reusable-template extraction after artifact sanitization, built-in copy
  config construction, and template materialization must call it directly; the `application/product_workflows.py` facade
  continues exporting the same public function name.
- Copy normalization must use `normalize_copy_node_config(...)` for known v2 fields and retain non-artifact extension
  fields from the input. Image normalization must keep the existing size, `visible_text_language_hint`, and `tool_options`
  rules. Reference-image and product-context configs are shallow-copied unchanged.
- Template materialization applies language hints and built-in `_canvas_template` metadata before the shared normalizer,
  then persists the normalized config. Built-in purpose inference remains template seed policy; copy output-mode inference
  belongs to the shared copy config contract. A template that requires a different valid mode must declare it explicitly.
- Only `image_generation` node specs may declare `size`.
- `output_slots` document which `reference_image` nodes receive generated material and how the UI should label those
  outputs.
- `reference_inputs` document which `reference_image` nodes are expected to receive user/product/style images before
  running downstream nodes.
- `suggested_connections` may describe optional UI connection advice, but every suggestion must point to existing template
  node keys and must not connect a node to itself.
- Built-in `full_canvas` templates may contain one `product_context` node. When such a template is appended inside an
  existing product workbench, application code must reuse the active workflow's existing product-context singleton instead
  of creating a second product node.
- Built-in `node_group` templates may declare `default_external_connections` from `existing_product_context` to template
  copy/image node keys. Applying the template must materialize those declarations as normal visible `workflow_edges`.
  They are not hidden suggestions or frontend-only hints.
- User-saved node-group templates are persisted in `user_canvas_templates`, not in the built-in template constant list.
  Their stable catalog key is `user:{id}`, `kind` is `node_group`, `schema_version` is `1`, and `template_json.version`
  must also be `1`.
- User-saved templates are converted back into the same `CanvasTemplate` contract with `source == "user"` and
  `user_template_id` populated. The existing catalog endpoint returns built-in and non-archived user templates together.
- Saving a user template from workflow nodes must capture only reusable intent: node type, title, relative position,
  normalized editable `config_json`, and edges whose source and target are both selected nodes. It must not read or persist
  `output_json`, workflow run rows, node run rows, artifact ids, file URLs/paths, product ids, workflow ids, or database
  node ids.
- User templates must reject empty selections, duplicate node ids, missing active workflows, nodes outside the current
  active workflow, and selections containing `product_context`. Known artifact-specific config fields such as
  `source_asset_ids`, `source_poster_variant_id`, `copy_set_id`, `poster_variant_id`,
  `generated_poster_variant_ids` and `filled_source_asset_ids` must be stripped while preserving
  reusable fields such as `role`, `label`, `instruction`, `size`, and normalized `tool_options`. Unknown config keys ending
  in `_id`, `_ids`, `_url`, or `_path` must be rejected so new artifact references do not silently enter templates.
- Applying a user template must go through the same template application path as built-in templates and must create
  ordinary persisted workflow nodes and edges. Archived user templates must not appear in the catalog and must not be
  applicable by key.
- Built-in templates must cover real ecommerce image-production scenarios: marketplace/main image, Taobao main image,
  Xiaohongshu/content cover, multi-angle gallery, SKU/variant, feature infographic, size/spec, scale reference,
  package/checklist, usage steps, comparison, model/lifestyle, scene, detail/material, campaign/promotion,
  short-video cover, and white-background output.
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
- User template save with no selected nodes -> `BusinessValidationError("请选择要保存的节点")`.
- User template save with duplicate node ids -> `BusinessValidationError("保存模板的节点不能重复")`.
- User template save before an active workflow exists -> `BusinessValidationError("需要先创建或打开画布后才能保存模板")`.
- User template save with nodes outside the current active workflow ->
  `BusinessValidationError("保存模板包含不属于当前画布的节点")`.
- User template save containing `product_context` -> `BusinessValidationError("节点组模板不能包含商品资料节点")`.
- User template save with unknown artifact-shaped config keys ->
  `BusinessValidationError("模板配置包含不可复用的产物数据")`.
- Invalid copy slots or image size at a workflow-node write boundary -> the underlying normalization `ValueError` is
  converted to `BusinessValidationError` with the existing validation message.
- Archived or missing user template key -> `BusinessValidationError("画布模板不存在")`.

### 5. Good/Base/Bad Cases

- Good: main-image template creates product context, copy generation, image generation, generated reference output, and a
  downstream iteration image/output pair. The iteration path remains a downstream DAG branch.
- Good: campaign template contains campaign prompt defaults, poster image size, and an explicit generated output slot.
- Good: built-in scenario template appends reusable ecommerce production nodes by materializing normal workflow nodes,
  reusing the active workflow product-context node for the template's `product` key, and remapping all other template keys
  to database node IDs before creating edges.
- Good: saving a selected copy/image/reference chain stores relative node positions and internal selected edges, appears in
  the template catalog with `source == "user"`, and applying `user:{id}` creates normal workflow rows with empty outputs.
- Good: saving a reference node that has been filled with an image strips `source_asset_ids` and
  `source_poster_variant_id` while preserving `role` and `label`.
- Base: a template can include suggested connections for optional palette guidance without requiring those suggestions to
  be materialized as edges. Default external connections are a separate executable contract and are materialized.
- Base: a template can include multiple output slots when one generation node is expected to fill multiple downstream
  `reference_image` nodes.
- Bad: a template stores a hidden chain in frontend state and creates only one placeholder node in the database.
- Bad: a template uses a new enum value before the explicit template allowlist and tests are updated.
- Bad: a user template stores output JSON, run results, source asset ids, poster ids, product ids, or
  workflow ids and reuses those artifacts when applied to another product.

### 6. Tests Required

- Unit test every built-in template with `validate_canvas_template` and assert all built-ins have unique keys.
- Unit test required ecommerce scenario coverage: marketplace/main image, Taobao main image, Xiaohongshu/content cover,
  multi-angle gallery, SKU/variant, feature infographic, size/spec, scale reference, package/checklist, usage steps,
  comparison, model/lifestyle, scene, detail/material, campaign/promotion, short-video cover, and white-background.
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
- User-template tests must cover create/list/rename/archive/apply, application of `user:{id}` as real workflow rows, hiding
  archived templates from the catalog, stripping known artifact config fields, rejecting unknown artifact-shaped config
  keys, preserving safe extension fields, and ignoring existing `output_json` / run outputs.
- Direct node-config tests must cover copy/image/reference normalization, invalid copy slots and image size, language-hint
  trimming, image tool-option filtering, safe extension fields, facade export compatibility, and runtime/template adapters.

### 7. Wrong vs Correct

#### Wrong

```python
template = {
    "key": "main-image",
    "steps": ["copy", "image", "iterate"],
}
```

This loses the node and edge contract, so later code cannot materialize the plan as a real workflow DAG.

#### Wrong

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
- Creation-time template selection must not implement user-saved template storage, result actions, or material lineage.
  Those are separate product-workbench capabilities.

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
  - `template_language: str | None`
- Application entrypoint:
  - `create_product(..., canvas_template_key: str | None = None, template_language: str | None = None, ...) -> Product`
- Application helper:
  - `resolve_product_creation_canvas_template(canvas_template_key: str | None) -> CanvasTemplate | None`
  - `materialize_product_workflow_from_template(session, *, product_id: str, template: CanvasTemplate, template_language: str | None = None) -> ProductWorkflow`

### 3. Contracts

- Missing, blank, and approved default aliases such as `default`, `basic`, `blank`, or `minimal` preserve the existing lazy
  default workflow behavior. Do not eagerly create a default workflow during product creation for those values.
- Any other key must resolve through the built-in template catalog. Do not accept frontend-only template payloads or
  browser-local template definitions for product creation.
- Only `CanvasTemplate.kind == "full_canvas"` is valid at product creation time.
- Built-in ecommerce templates are complete `full_canvas` templates. Product creation may materialize any built-in
  ecommerce template directly.
- `create_product` owns the SQLAlchemy transaction. Template materialization helpers may `flush` rows to resolve ids, but
  must not `commit` independently.
- Materialized `WorkflowNode` rows copy template `node_type`, `title`, `position_x`, `position_y`, and `config_json`.
- Materialized `WorkflowEdge` rows remap template node keys to persisted node ids and copy `source_handle` /
  `target_handle`.
- The materialized workflow must be active and must use normal workflow tables. Do not store a hidden selected-template
  state that later frontend code interprets locally.
- `template_language` is optional. Blank, `auto`, and `preserve_input` values leave hard language hints unset.
- Recognized template language codes `zh-CN`, `en-US`, `ja-JP`, and `vi-VN` map to editable node config hints. Template
  copy nodes receive `copy_language_hint`, and image nodes receive `visible_text_language_hint`.
- Template language hints constrain newly generated merchant copy and newly generated poster text only. Existing product
  image/package text, brand names, model identifiers, specifications, and certification marks should be preserved from the
  source image instead of translated.
- Language hint application should fill missing hint fields on materialized node config. Existing explicit non-empty hint
  values in a template or user template remain authoritative.
- If the product creation page renders a large preview for a built-in `full_canvas` plan, that preview is a mirror of the
  backend template. Node titles, relative order, edges, and coordinates for shared template keys must be updated with the
  backend template and covered by regression tests.
- Later calls to `get_or_create_product_workflow` must return the active workflow created at product creation and must not
  overwrite it with the lazy default graph.

### 4. Validation & Error Matrix

- `canvas_template_key` missing/blank/default alias -> create product, no eager workflow row.
- Unknown non-default key -> `BusinessValidationError("画布模板不存在")` or equivalent template-missing `400`.
- Built-in key whose template kind is not `full_canvas` -> `BusinessValidationError` with a message explaining product
  creation supports only complete canvas templates.
- Product id missing during materialization -> `NotFoundError("商品不存在")`.
- Product already has an active workflow before materialization -> `BusinessValidationError("商品已有活动画布")`.

### 5. Good/Base/Bad Cases

- Good: creating a product with `ecommerce-main-image-v1` creates one active `ProductWorkflow`, persists all template nodes
  and edges, and the detail workflow endpoint returns that workflow unchanged.
- Good: creating a product with no `canvas_template_key` creates only the product/assets; opening the workflow later
  lazily creates the current default graph.
- Base: frontend can label the key as a merchant-facing output plan such as `商品主图方案`; the submitted value remains the
  backend-recognized `canvas_template_key`.
- Bad: accepting a user-saved `node_group` template in product creation and pretending it is a full-canvas starter.
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
- API test a broad built-in scenario key such as `ecommerce-sku-variant-image-v1` creates an active workflow immediately.
- API test product creation with `template_language=vi-VN` persists both `copy_language_hint` on copy nodes and
  `visible_text_language_hint` on image nodes.
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

## Scenario: Product workflow template application

### 1. Scope / Trigger

- Trigger: changes to canvas-internal template insertion APIs, built-in scenario templates, or product workflow
  mutation code that materializes template nodes.
- The workbench may append built-in `full_canvas` scenario templates to an existing active product workflow by creating
  normal persisted workflow rows and reusing the active workflow's existing product-context node.
- This scenario does not cover product-creation template selection, user-saved template authoring,
  drag-to-canvas authoring, or hidden suggested external connections.

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
  - `template_language: str | None = None`
- Application entrypoint:
  - `apply_node_group_template_to_workflow(session, product_id, template_key, position_x, position_y, template_language=None) -> ProductWorkflow`
- Shared materialization helper:
  - `materialize_canvas_template_graph(..., existing_nodes_by_template_key=None, external_source_nodes_by_template_source=None, template_language=None)`

### 3. Contracts

- The apply API resolves `template_key` through the backend template catalog, including built-in scenario templates and
  non-archived user templates.
- Catalog summary responses must expose lightweight real graph preview data from `CanvasTemplate.nodes` and
  `CanvasTemplate.edges`. Include node key, type, title, and relative coordinates plus edge source/target keys; do not
  include large prompt seeds, instruction strings, or `config_json` in summary preview data.
- Built-in `full_canvas` scenario templates and user-saved `node_group` templates are both valid for canvas-internal
  insertion.
- Applying a template appends to the product's active workflow and preserves all existing nodes and edges.
- If a template contains `product_context`, the apply path must map that template key to the active workflow's existing
  product-context node and skip creating a duplicate product node.
- Template node specs are materialized into real `workflow_nodes` rows by copying `node_type`, `title`, `config_json`, and
  relative layout.
- The smallest insertable template `position_x` / `position_y` becomes the anchor that lands at request `position_x` /
  `position_y`; other inserted template nodes keep their relative offsets.
- Template edge specs are materialized as real `workflow_edges` rows after remapping template keys to either newly created
  nodes or explicitly reused existing nodes such as the product-context singleton.
- `suggested_connections` are returned by the catalog for UI guidance and must not be silently materialized as external
  workflow edges.
- `default_external_connections` are returned by the catalog as lightweight metadata and are materialized by the apply API
  as real visible `workflow_edges` when a node-group template declares them.
- Built-in scenario templates contain a `product_context` node in the template graph. The active workflow already owns
  the product-context singleton, so insertion reuses it and still materializes the template's product-to-copy/image edges.
- `template_language` follows the same node-config contract as product creation: recognized codes fill
  `copy_language_hint` on materialized copy nodes and `visible_text_language_hint` on materialized image nodes; blank,
  `auto`, and `preserve_input` leave hard hints unset.
- Apply-time language hints affect only materialized nodes from the requested template. Existing workflow nodes keep their
  saved config.

### 4. Validation & Error Matrix

- Unknown `template_key` -> `400`, `{"detail": "画布模板不存在"}`.
- Missing product -> `404`, `{"detail": "商品不存在"}`.
- Existing product without an active workflow -> `400`; the apply API must not create a workflow implicitly.
- Active workflow without exactly one `product_context` node -> `400`; the apply API must not create a partially usable
  scenario template.
- Template self-edge, missing edge reference, or cycle -> `400` business validation error.
- Any generated edge that would make the full active workflow cyclic -> rollback and return a `400` DAG validation error.

### 5. Good/Base/Bad Cases

- Good: applying `ecommerce-sku-variant-image-v1` to the default workflow increases node and edge counts, keeps all
  previous node/edge IDs, does not create a second product-context node, and adds visible edges from the existing product
  context to the template copy/image nodes.
- Good: applying a template at `position_x=480`, `position_y=360` places the template's minimum coordinate there while
  preserving relative spacing.
- Base: the UI may render reference input hints and connection suggestions from the catalog.
- Bad: frontend creates local-only template nodes without calling the apply API.
- Bad: creating a second `product_context` node when applying a built-in scenario template inside an existing canvas.
- Bad: materializing suggested external connections as hidden edges.

### 6. Tests Required

- API test catalog response includes built-in templates with `kind`, scenario metadata, output slots, reference input
  hints, suggested connections, lightweight default external connections, and real `preview_nodes` / `preview_edges`
  matching the built-in template definitions. Catalog summaries must not expose config/prompt payloads.
- API test successful template apply preserves existing nodes and edges.
- API test persisted node count, edge count, node types, titles, config, and shifted positions match the backend template.
- API test created edges are the template edges remapped to newly created nodes plus the existing product-context node,
  and no self-edge is created.
- API test built-in full-canvas insertion reuses the existing product-context node.
- API test applying a template with `template_language=vi-VN` persists both copy and image language hints on the newly
  materialized nodes.
- API test missing product-context node returns `400` and does not create a partial template.
- API test unknown key returns `400` with template-missing detail.

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
  - `workflow_node_runs(workflow_run_id, node_id, status, output_json, copy_set_id, poster_variant_id)`.
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
  - `TextProvider.generate_copy(product, brief, config, reference_images=None)` receives `CopyNodeConfigV2` plus connected
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
  `source_poster_variant_id` matches the poster. Historical workflow output pairings from
  `generated_poster_variant_ids` / `filled_source_asset_ids` are migration input only; runtime binding does not scan or
  repair them. If no column-backed asset exists, copy/materialize the poster file into a new `reference_image` SourceAsset
  named `poster-{poster_variant_id}.*` with `source_poster_variant_id` set, then bind it.
  The filename convention is legacy compatibility only; current de-duplication should use explicit
  `source_poster_variant_id` so a user-uploaded reference image with the same filename is not hidden or rebound as a poster
  copy.
- `reference_image` nodes store image material as first-class `source_assets` rows and expose `source_asset_ids` /
  `image_asset_ids` in workflow output JSON for downstream image nodes.
- `copy_generation` nodes must collect connected upstream `reference_image` slots and pass their asset paths plus
  role/label metadata to the text provider. Text-only providers should include concise reference metadata in the prompt;
  multimodal-capable providers may also attach image payloads/paths.
- `reference_image_inputs_for_copy(...)` must collect ordered reference-node descriptors and globally unique asset ids,
  then load those assets with one `WorkflowQueryService.source_assets_by_ids(...)` call. It performs no SourceAsset query
  when no ids are present. Provider inputs are rebuilt in reference-node encounter order and each node's configured asset
  order; a shared asset keeps the first node's role/label. Missing and other-product assets remain excluded.
- A generated `copy_generation` output is editable through `PATCH /api/workflow-nodes/{node_id}/copy`. The endpoint
  updates the underlying `CopySet.structured_payload`, then rewrites the node output so downstream image nodes read the
  edited v2 copy through the existing `copy_set_id`. Structured-payload edits must not re-derive or overwrite
  removed fixed-field copy columns.
- Manually edited copy node outputs should be treated as the selected copy for downstream runs. Re-running a downstream
  image node must not silently replace that edited `CopySet` with a fresh generated copy before image generation.
- New copy-generation runs must produce `CopyPayloadV2` as the only path. Providers, templates, editors, and tests must
  treat `structured_payload` as the copy contract.
- Copy-node output JSON must include `structured_payload` and must not emit fixed copy fields for workflow runs. Upstream
  image context must use `structured_payload.summary/content/visual_guidance`.
- `image_generation` nodes collect incoming edge context, including upstream copy text and reference-image outputs. They
  are trigger/config nodes, not image-bearing artifact slots; generated images must be viewed/downloaded from linked
  downstream `reference_image` nodes or normal product artifact history, not from the `image_generation` node card.
- Workflow image generation mode is derived from the stored `poster_generation_mode` plus the current image-purpose
  provider binding. Real image bindings (`openai_responses`, `openai_images`, or `google_gemini_image`) execute through
  the image provider even when the legacy runtime value remains `template`. `mock` image bindings keep the no-external
  local development path, and `PosterRenderer` remains available for template/mock fallback.
- Generated-mode provider prompts expose visual-subject policy through the runtime-configurable
  `prompt_poster_image_reference_policy` placeholder, not hidden provider code. The default policy treats the first/source
  image as the primary visual subject when present, while upstream copy is auxiliary selling-point/layout context. This is
  important when product text is weak such as a default name `商品` and copy generation may otherwise invent an unrelated
  role, IP, brand, or ad theme.
- Image prompt mode is determined by explicit copy linkage, not by whether a workflow-local `CopySet` exists for
  persistence.
  If `image_generation.config_json.copy_set_id` or an upstream `copy_generation.output_json.copy_set_id` points to a
  same-product `CopySet`, provider input must set `PosterGenerationInput.copy_prompt_mode = "copy"` and use the poster/copy
  image template. If no explicit copy link exists, create the workflow-local draft `CopySet` as needed for
  `PosterVariant.copy_set_id`, but set `copy_prompt_mode = "image_edit"` so provider prompts use the no-copy image-edit
  template and do not require fixed copy-field semantics.
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
- `image_generation` node config may override provider size with `size`; application contracts must normalize it to the
  generation safety bounds before it reaches providers, including a 512px minimum per side and the runtime maximum
  dimension. The normalized value is carried as `PosterGenerationInput.image_size`, and providers should prefer it over
  global runtime defaults.
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
- Good: edit a copy node's structured payload; the persisted `CopySet.structured_payload` and node output update together,
  old four-field columns are not re-derived, and the downstream image node keeps referencing the same edited
  `copy_set_id`.
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
- Base: image-node reusable artifact detection must read both `poster_variant_ids` and
  `generated_poster_variant_ids` through the shared `poster_variant_ids_from_output(...)` compatibility helper, then
  validate those IDs against first-class `PosterVariant` rows for the same product before skipping an upstream image node.
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
- API/provider regression for generated-mode workflow image nodes asserts `output_json.provider_results` contains only
  compact provider summary fields such as `provider_name`, `model_name`, `provider_response_id`,
  `provider_response_status`, `actual_size`, and provider compatibility notes. Do not persist raw provider request bodies,
  full provider output JSON, prompts, API keys, or base URLs in workflow node output.
- API/provider regression asserts a real image-purpose binding overrides stale `poster_generation_mode=template`, uses the
  injected image provider dependency seam, bypasses `PosterRenderer`, and persists a `workflow:<provider>:...`
  `PosterVariant.template_name`.
- API regression uploads twice to the same `reference_image` node and asserts the node exposes only the second asset while
  both old and new `source_assets` remain on the product. Another regression fills an already populated reference node from
  an upstream `image_generation` node and asserts the same single-slot replacement behavior.
- API regression binds a reference node from an existing `source_asset_id` and asserts no duplicate SourceAsset is created;
  another regression binds from a `poster_variant_id` and asserts the poster materializes or maps to a reference SourceAsset.
- API/provider regression connects a `reference_image` node into `copy_generation` and asserts the reference label/role
  reaches generated copy/provider input.
- Query regression covers one and multiple upstream reference nodes with exactly one SourceAsset SELECT, zero ids with no
  SourceAsset SELECT, configured ordering, shared-asset de-duplication, other-product/missing filtering, role/label, and
  resolved storage paths.
- API regression edits a generated copy node through `PATCH /api/workflow-nodes/{node_id}/copy` and asserts both the
  persisted `CopySet` and node output summary fields are updated.
- API regression for selected-node runs creates successful upstream outputs, runs a downstream node, and asserts upstream
  node runs/artifacts are not duplicated when reusable outputs exist, including image-node outputs that expose
  `generated_poster_variant_ids`.
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
node.output_json = {"copy_set_id": copy_set.id, "summary": copy_set.structured_payload["summary"]}
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
  - `CopyPayloadV2(version: 2, purpose: str | None, summary: str, content: CopyContent, visual_guidance: VisualGuidance | None)`.
  - `PosterGenerationInput(..., structured_copy_context: str | None = None)`.
- Text provider methods:
  - `TextProvider.generate_brief(product: ProductInput) -> tuple[CreativeBriefPayload, str]`.
  - `TextProvider.generate_copy(product: ProductInput, brief: CreativeBriefPayload, config: CopyNodeConfigV2, reference_images: list[ReferenceImageInput] | None = None) -> tuple[CopyPayloadV2, str]`.
- Persistence/API boundary:
  - `CreativeBrief.payload` and workflow `latest_brief.payload` must expose scalar brief fields as strings.
  - `CopySet.structured_payload` and `CopySet.model_structured_payload` persist the v2 payload.

### 3. Contracts

- AI providers may occasionally return a pure text array for a scalar short-text field. The application contract boundary
  may normalize only these scalar fields by joining items with `、`:
  - `CreativeBriefPayload.positioning`
  - `CreativeBriefPayload.audience`
  - `CreativeBriefPayload.poster_style_hint`
- Fields whose contract is already a list must remain lists and must not be flattened into one scalar:
  - `CreativeBriefPayload.selling_angles`
  - `CreativeBriefPayload.taboo_phrases`
- V2 content supports `freeform`, `blocks`, and `layout_brief`. Optional fields such as block `label`, `role`,
  `visual_hint`, and visual guidance must remain optional so the model is not forced to invent fields that the task does not
  need.
- `copy_node_output(...)` exposes `structured_payload` and `summary` for copy content. It must not expose
  fixed copy-field output keys.
- Image-generation prompt context should set `PosterGenerationInput.structured_copy_context` from
  `copy_payload_context_text(validate_copy_payload(copy_set.structured_payload))`.

### 4. Validation & Error Matrix

- Scalar field is a normal string -> accepted unchanged.
- Scalar field is a non-empty list of non-empty strings -> normalized to a single string joined by `、`.
- Scalar field is an empty list -> Pydantic `ValidationError`; do not silently store an empty string.
- Scalar field list contains an empty/blank string -> Pydantic `ValidationError`.
- Scalar field list contains an object, number, boolean, or `null` -> Pydantic `ValidationError`; do not coerce with
  `str(...)`.
- `CopyPayloadV2.content.kind` is not `freeform`, `blocks`, or `layout_brief` -> Pydantic `ValidationError`.
- `CopyPayloadV2.summary` is blank -> Pydantic `ValidationError`.
- V2 block/freeform/section text is empty where required -> Pydantic `ValidationError`.
- Other list-contract fields are not lists or violate min/max length -> Pydantic `ValidationError`.

### 5. Good/Base/Bad Cases

- Good: provider returns `{"audience": ["摄影入门用户", "图文内容创作者"]}`; persisted and API-visible payload uses
  `"摄影入门用户、图文内容创作者"`.
- Good: provider returns `{"version":2,"summary":"卖点速览","content":{"kind":"blocks","blocks":[...]}}`; `CopySet.structured_payload` stores the blocks, and downstream image context reads the structured payload text.
- Good: provider returns `content.kind="layout_brief"` for information hierarchy; downstream image context receives the
  section text and visual hints instead of fixed copy fields.
- Base: provider returns scalar strings for all scalar fields; values pass through unchanged.
- Bad: provider returns `{"audience": []}` or `{"audience": [{"name": "摄影入门用户"}]}`; validation fails instead of
  inventing a display string.
- Bad: adding a new copy node template that only stores `instruction/tone/channel` without `version: 2` and
  `output_mode`; templates must seed the v2 config so old shape cannot keep accumulating.

### 6. Tests Required

- Contract regression directly validates `CreativeBriefPayload` with scalar text arrays and malformed arrays; assert good
  arrays are joined with `、` and bad arrays raise `ValidationError`.
- Contract regression validates `CopyPayloadV2` with `freeform`, `blocks`, and `layout_brief`.
- Copy-generation workflow regression monkeypatches the text provider to return scalar arrays and asserts the persisted
  `CreativeBrief.payload` fields are normalized strings while `CopySet` persists structured payloads.
- Copy-generation workflow regression asserts copy-node `output_json.structured_payload.version == 2` and does not expose
  fixed copy-field output keys.
- Canvas-template regression asserts every built-in `copy_generation` node config includes v2 `version`, `purpose`, and
  `output_mode`.
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

## Scenario: OpenAI text provider structured outputs

### 1. Scope / Trigger

- Trigger: any change to `OpenAITextProvider` response handling for `generate_brief` or `generate_copy`.
- Text provider output shape is a provider boundary contract because it feeds `CreativeBriefPayload`, `CopyPayloadV2`,
  `creative_briefs.payload`, `copy_sets.structured_payload`, workflow node `output_json`, and frontend DTOs.
- The OpenAI-backed text provider must use native Responses structured outputs, not prompt-only JSON formatting.

### 2. Signatures

- Provider methods:
  - `OpenAITextProvider.generate_brief(product: ProductInput) -> tuple[CreativeBriefPayload, str]`.
  - `OpenAITextProvider.generate_copy(product: ProductInput, brief: CreativeBriefPayload, config: CopyNodeConfigV2, reference_images: list[ReferenceImageInput] | None = None) -> tuple[CopyPayloadV2, str]`.
- OpenAI SDK call shape:
  - `client.responses.parse(model=<model>, text_format=CreativeBriefPayload, instructions=<semantic prompt>, input=<messages>)`.
  - `client.responses.parse(model=<model>, text_format=OpenAICopyPayloadStructuredOutput, instructions=<semantic prompt>, input=<messages>)`.
  - `OpenAICopyPayloadStructuredOutput.to_copy_payload(...) -> CopyPayloadV2`.
- Provider-facing copy schema:
  - `version: Literal[2]`.
  - `purpose: str` where empty string means omitted.
  - `summary: str`.
  - `content_kind: Literal["freeform", "blocks", "layout_brief"]`.
  - `freeform_text: str`, `blocks: list[...]`, and `sections: list[...]`; unused content carriers are empty string or
    empty arrays.
  - `visual_guidance` uses required string/list fields; empty string or empty arrays mean omitted.
- Prompt configuration keys remain:
  - `prompt_brief_system`
  - `prompt_copy_system`

### 3. Contracts

- Structure belongs to Pydantic/OpenAI structured outputs. Prompt text must not be the primary structure contract.
- `prompt_brief_system` and `prompt_copy_system` control semantic behavior, tone, and task guidance only.
- Provider input messages should carry task data as JSON, including `task`, `product`, `brief`, `reference_images`,
  `node_config`, `language_policy`, `fact_policy`, and `copy_planning_policy` for copy generation. This JSON is the
  semantic task payload; structural output remains enforced by `responses.parse(..., text_format=...)`.
- `copy_language_hint` belongs to copy node config and the provider-facing `language_policy`. It constrains newly
  generated copy only, while brand names, model names, unit strings, and source-image text should be preserved when they
  are known product facts.
- When product facts are sparse, the fact policy should prefer short neutral copy, known-information organization, and
  layout guidance. It should forbid unsupported discounts, time limits, lowest-price claims, best-seller claims,
  certification claims, specifications, gifts, effect promises, and other missing facts.
- Provider prompts must not depend on phrases such as `请输出 JSON`, `不要输出 markdown`, `请输出字段`, or
  `请输出 v2 JSON 外壳` for correctness.
- `OpenAITextProvider` must not silently fall back to parsing JSON text from `response.output_text`, raw strings,
  markdown fences, SSE deltas, or embedded objects.
- `CopyPayloadV2.content` is a discriminated union. Its direct Pydantic JSON Schema contains a union combinator under
  `content`, and real OpenAI-compatible providers may reject that with `invalid_json_schema`.
- For copy generation, `OpenAITextProvider` must use the flat provider-facing `OpenAICopyPayloadStructuredOutput` schema
  that has no `oneOf`, `anyOf`, or `default` schema keys, then immediately convert and validate it as `CopyPayloadV2`.
- Runtime copy payload validation must be strict. User edits, workflow context assembly, image-generation copy context,
  artifact materialization, and API serialization should call `validate_copy_payload(...)`, not provider-output shape
  repair code.
- Prompt-JSON-era payload repair such as filling missing block ids, mapping `type` to `kind`, turning string
  `visual_guidance` into an object, or deriving layout sections from arbitrary object keys must not run in normal product
  workflow paths.
- OpenAI-compatible text providers that do not support `responses.parse(..., text_format=...)` / `text.format=json_schema`
  are unsupported for the real text-provider path and should fail at the provider boundary with a stable configuration
  message.
- If historical compatibility is needed, keep it in an explicitly named legacy boundary or migration helper. Do not name it
  as the normal copy payload validator, and do not call it from user-edit or provider-generated runtime paths.
- Do not log full prompts, raw provider payloads, provider responses, API keys, or base URLs while diagnosing this path.

### 4. Validation & Error Matrix

- SDK response exposes `output_parsed` as `CreativeBriefPayload` -> return it directly.
- SDK response exposes `output_parsed` as `OpenAICopyPayloadStructuredOutput` -> convert with `to_copy_payload(...)` and
  return the resulting `CopyPayloadV2`.
- SDK response exposes `output_parsed` as a dict -> validate it through the requested Pydantic model.
- User-edited copy payload misses required `summary` / `content`, has a block missing `id`, or passes string
  `visual_guidance` -> reject with a stable business validation error instead of repairing it.
- Saved workflow/copy-set payload fails `CopyPayloadV2` validation -> treat it as invalid persisted data; do not silently
  repair shape in context/image/artifact readers.
- Direct `text_format=CopyPayloadV2` would generate a schema rejected by some providers with
  `invalid_json_schema` and message like `oneOf is not permitted`; do not use it for the real OpenAI provider path.
- `responses.parse` is missing on the client -> raise a stable provider capability error, not an AttributeError.
- Provider/model rejects structured output parameters -> raise a stable provider capability/request error; do not retry by
  prompting for JSON text.
- Parsed payload violates `CreativeBriefPayload` / `CopyPayloadV2` -> surface Pydantic validation so workflow copy retries
  can apply the existing provider-contract retry loop.

### 5. Good/Base/Bad Cases

- Good: `generate_copy(...)` calls `responses.parse(..., text_format=OpenAICopyPayloadStructuredOutput)`, converts the
  parsed object to `CopyPayloadV2`, and workflow persists `CopySet.structured_payload` from `model_dump(mode="json")`.
- Good: the system prompt says how to write merchant copy, while schema enforces `summary`, `content.kind`, blocks, and
  visual guidance shape.
- Base: mock/test text providers can still return `CopyPayloadV2` objects directly through the shared `TextProvider`
  interface.
- Bad: `responses.create(...)` plus `read_json_object_from_response(...)` is reintroduced for the OpenAI provider.
- Bad: accepting a third-party SSE/plain-string JSON response as a real-provider fallback; that keeps the old prompt-JSON
  contract alive.
- Bad: using a normalizer that accepts provider-near-miss shapes such as `blocks[].type` without `id`, `freeform.items`,
  or string `visual_guidance` in user-edit or workflow runtime paths.

### 6. Tests Required

- Provider regression asserts `OpenAITextProvider.generate_brief()` calls `responses.parse` with
  `text_format is CreativeBriefPayload` and does not call `responses.create`.
- Provider regression asserts `OpenAITextProvider.generate_copy()` calls `responses.parse` with
  `text_format is OpenAICopyPayloadStructuredOutput` and does not call `responses.create`.
- Schema regression asserts `to_strict_json_schema(OpenAICopyPayloadStructuredOutput)` has no `oneOf`, `anyOf`, or
  `default` keys.
- Payload validation regression asserts strict `validate_copy_payload(...)` accepts canonical `CopyPayloadV2` dicts and
  rejects prompt-JSON-era near-miss shapes.
- User-edit regression asserts invalid copy payloads raise typed business validation errors without shape repair.
- Prompt regression asserts provider user/system prompt text no longer contains old structure fallback phrases such as
  `请输出 JSON`, `不要输出 markdown`, `请输出字段`, `请输出 v2 JSON 外壳`, or `content.kind 必须`.
- Prompt regression parses the OpenAI text provider user message as JSON and asserts the copy language policy, node config,
  sparse-fact policy, and planning policy fields are present.
- Backend API E2E regression creates a product, runs the workflow through `/api/products/{id}/workflow/run`, waits for a
  succeeded run, and asserts the copy node and product detail expose `structured_payload`.
- Settings/help regression keeps default prompt descriptions aligned with structured-output ownership.

### 7. Wrong vs Correct

#### Wrong

```python
response = client.responses.create(...)
payload = read_json_object_from_response(response, error_label="文案 provider")
return normalize_copy_payload(payload)
```

This makes natural-language prompt wording responsible for the structural contract and keeps JSON text extraction as a
runtime dependency.

#### Wrong

```python
response = client.responses.parse(
    model=copy_model,
    text_format=CopyPayloadV2,
    instructions=semantic_copy_prompt,
    input=messages,
)
return response.output_parsed
```

This direct schema contains a union under `content` and can fail against OpenAI-compatible providers with
`invalid_json_schema`.

#### Correct

```python
parsed = client.responses.parse(
    model=copy_model,
    text_format=OpenAICopyPayloadStructuredOutput,
    instructions=semantic_copy_prompt,
    input=messages,
).output_parsed
return parsed.to_copy_payload(fallback_purpose=config.purpose)
```

Schema enforces structure without unsupported union combinators; `CopyPayloadV2` remains the application/workflow contract.

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
  Workflow-run delivery classification is derived from the run plus its node-run statuses, not Redis metadata: inactive
  runs are excluded; any running node-run classifies the run as running; otherwise any queued node-run classifies it as
  queued; a non-empty all-succeeded node-run set also classifies as queued because the scheduler still needs to finalize
  the run. Queue position is exposed only for the derived queued state, using the earliest queued node-run timestamp or
  the run timestamp for all-succeeded finalization.
- API startup must call workflow run recovery for active runs with no node currently running, so a run committed before a
  Redis send or process restart is sent again.
- `application/durable_recovery.py` owns the workflow recovery state machine. `presentation/api.py` and `workers.py` pass
  `infrastructure.queue.enqueue_workflow_run` explicitly; recovery does not import the queue adapter.
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
  not call providers, and enqueue delayed retry of the same `workflow_node_run_id`.
- Redis enqueue failure after the run has been created -> mark the run `failed`, release active node-run uniqueness slots,
  and return `503` with `任务队列暂不可用，请稍后重试`.
- Workflow image provider timeout -> mark the active run and image node run `failed`, set `finished_at`, use
  `图片生成超时，请稍后重试`, and release global generation queue capacity.
- Workflow image provider failures with recognized safe categories should not collapse into one generic message. Use
  concise user-facing reasons for rate limit/quota, content-policy refusal, connection interruption, provider request
  timeout, unsupported parameters, and provider 5xx/service failure; inspect wrapped exception causes/contexts when the
  outer provider layer uses a generic request-failure message.
- Workflow image provider failure with safe details such as unsupported dimensions -> mark failed with a concise prefixed
  reason such as `图片生成失败：image2 不支持 64x64，最小尺寸为 512x512`.
- Workflow image provider failure with raw provider details -> mark failed with a generic safe reason such as
  `图片生成失败，请稍后重试`; never expose secrets, base URLs, prompt payloads, request bodies, file paths, or tracebacks
  through `failure_reason`.
- Workflow image provider success may persist a compact `output_json.provider_results` summary for UI logs, but this is
  not a live progress channel. Real-time provider progress requires durable workflow node-run progress fields; do not fake
  ImageChat-style `provider_response_status` polling from stale terminal output.
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

## Scenario: Workflow node-group duplicate

### 1. Scope / Trigger
- Trigger: changes to canvas copy/paste, node-group duplicate routes, reusable node config sanitization, or undo restore of
  deleted workflow nodes.
- Node-group duplication creates ordinary workflow rows for repeated workbench modules without reusing generated artifacts
  or run state.

### 2. Signatures
- API: `POST /api/products/{product_id}/workflow/node-groups/duplicate -> ProductWorkflowResponse`.
- Request fields:
  - `node_ids: list[str]`
  - `position_x: int | None`
  - `position_y: int | None`
  - `offset_x: int`
  - `offset_y: int`
- Application use case returns the refreshed active `ProductWorkflow`.

### 3. Contracts
- Duplicate operates only on nodes in the product's active workflow.
- `product_context` nodes are never duplicated. If the selected group contains only product context nodes, the request is
  invalid.
- Duplicated nodes keep node type, title, relative position, and normalized editable `config_json`.
- Duplicated nodes start with idle/default run state and empty outputs. Do not copy `output_json`, failure reason,
  `last_run_at`, workflow-node-run rows, workflow-run rows, `CopySet`, `PosterVariant`, `SourceAsset`, image-session
  assets, or gallery artifacts.
- Internal edges are recreated only when both source and target are duplicated. External edges to unselected nodes are not
  recreated.
- Reusable config sanitization must match user-template boundaries: strip known artifact fields and reject unknown
  artifact-shaped config keys ending in `_id`, `_ids`, `_url`, or `_path`.
- The insertion position may be anchored to a requested point or use a deterministic offset from the selected group.

### 4. Validation & Error Matrix
- Empty `node_ids` -> `400` with a concise selection-required message.
- Duplicate node ids in request -> `400` with duplicate selection message.
- Unknown product or no active workflow -> existing product/workflow not-found behavior.
- Node outside active workflow -> `400` with current-canvas ownership message.
- Selected group contains no duplicable node after excluding `product_context` -> `400`.
- Sanitization finds artifact-shaped config -> `400` with reusable-config validation message.

### 5. Good/Base/Bad Cases
- Good: duplicate a copy -> image -> reference chain and get three new nodes plus two internal edges.
- Good: duplicate a filled reference node and preserve reusable `role` / `label` while dropping asset ids and output JSON.
- Base: duplicate a group selected with the product context plus copy/image nodes; only copy/image nodes are recreated.
- Bad: duplicate a node with `output_json.copy_set_id` or `source_asset_ids` and make the new node appear already
  generated.
- Bad: recreate edges from the copied group back to original unselected nodes, which changes the user's graph topology.

### 6. Tests Required
- API regression for duplicating selected nodes with internal edges.
- Regression that `product_context` is skipped and a product-context-only selection is rejected.
- Regression that output JSON, run state, run rows, and artifact-shaped config are not copied.
- Regression that duplicated nodes are selected/usable through normal frontend workflow payloads.

### 7. Wrong vs Correct

Wrong:

```python
new_node = WorkflowNode(**old_node.__dict__)
```

This copies database identity, output/run fields, and artifact references.

Correct:

```python
new_node = WorkflowNode(
    workflow_id=workflow.id,
    node_type=old_node.node_type,
    title=old_node.title,
    config_json=_extract_reusable_config(old_node),
)
```

Create a fresh workflow node from reusable intent only, then recreate selected internal edges.

---

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

---

## Scenario: WorkflowDraft v1 and atomic schema-v2 materialization

### 1. Scope / Trigger

- Trigger: changes to WorkflowDraft artifact fields, product fact confirmation, schema-v2 workflow persistence,
  Draft materialization, reveal events, or `/api/v2/.../workflow-drafts` endpoints.
- This contract creates a complete persisted v2 DAG from one confirmed Draft revision. Prompt compilation, image
  execution, Agent transport, and React reveal animation have separate owners.
- Existing schema-v1 workflows keep their historical rows and old execution path until the legacy retirement task.

### 2. Signatures

- Artifact: `WorkflowDraftPayloadV1` in `application/workflow_drafts/contracts.py`, with `schema_version: Literal[1]`.
- Lifecycle commands:
  - `create_workflow_draft(session, *, product_id, payload, ready_for_confirmation, source_turn_id=None,
    source_artifact_step_id=None) -> WorkflowDraft`
  - `append_workflow_draft_revision(session, *, product_id, draft_id, expected_draft_version, payload,
    ready_for_confirmation, source_turn_id=None, source_artifact_step_id=None) -> WorkflowDraft`
  - `confirm_workflow_draft_revision(session, *, product_id, draft_id, expected_draft_version) -> WorkflowDraft`
- Materialization command:
  - `materialize_workflow_draft(session, *, product_id, draft_id, expected_draft_version,
    expected_workflow_revision, idempotency_key) -> WorkflowMaterializationResult`
- Read commands:
  - `get_active_v2_workflow_snapshot(session, *, product_id) -> ActiveV2WorkflowSnapshot`
  - `list_workflow_reveal_events(session, *, materialization_id, after) -> list[WorkflowRevealEvent]`
- HTTP boundaries:
  - `GET /api/v2/products/{product_id}/workflow`
  - `POST /api/v2/products/{product_id}/workflow-drafts`
  - `GET /api/v2/products/{product_id}/workflow-drafts/{draft_id}`
  - `POST /api/v2/products/{product_id}/workflow-drafts/{draft_id}/revisions|confirm|materialize`
  - `GET /api/v2/workflow-materializations/{materialization_id}/reveal-events`

### 3. Contracts

- Pydantic models are frozen and use `extra="forbid"`. Business keys use lowercase ASCII letters, digits, `_`, and `-`;
  Draft revisions persist the full canonical JSON snapshot and SHA-256 hash.
- A Draft has at least one image type and one canonical reference binding. It accepts at most six reference assets, one to
  six planned images per type, and no more than thirty planned images in total.
- Every image type owns exactly one prompt plan and one `prompt_generation` node. Every planned image owns exactly one
  `image_generation` node. Every reference binding owns exactly one `reference_image` node. The graph owns exactly one
  `product_context` node.
- Typed edges allow only the declared product/reference/prompt/image data flows. Every prompt receives a direct product
  context edge, every planned image receives its type prompt edge, duplicate pairs/self-edges are rejected, and the graph
  must remain acyclic.
- Revisions are append-only. Confirming a revision creates one immutable `ProductFactSetVersion`, marks facts confirmed,
  and moves `Product.current_fact_set_version_id`; later information creates another revision and fact version.
- Materialization locks the product and Draft, validates both expected versions, re-parses the stored artifact, verifies
  its hash and all `ProductImageAsset` ownership, then writes workflow/folders/nodes/edges/materialization/key mappings/
  reveal events and active-workflow changes in one transaction.
- One Draft revision maps to one workflow. Every successful product-scoped idempotency key is persisted in
  `WorkflowMaterializationKey`; a repeated key must carry the same normalized request hash. A new key for an already
  materialized revision is bound to that existing result.
- An active schema-v1 workflow blocks v2 materialization with `409` and remains unchanged. Old query/edit/run entrypoints
  reject schema-v2 workflows and nodes with `409`.
- `GET /api/v2/products/{product_id}/workflow` is read-only. Missing v2 state returns
  `{ "latest_revision": 0, "workflow": null }` and never calls the v1 get-or-create helper.
- Reveal rows are committed before SSE starts. SSE uses sequence as `id`, kind as `event`, supports `Last-Event-ID` and
  `after` by taking the larger cursor, and performs no writes while replaying.

### 4. Validation & Error Matrix

- Unknown request/artifact field or invalid Pydantic shape -> HTTP `422` at the API boundary.
- Stored or application-provided artifact cannot be parsed as Draft v1 -> `BusinessValidationError` / HTTP `400`.
- Missing required fact, unresolved conflict, missing asset, or cross-product asset -> `BusinessValidationError` /
  HTTP `400`; no workflow rows are committed.
- Stale Draft/workflow revision, unconfirmed Draft, fact lineage drift, active v1 workflow, or reused key with another
  request hash -> `ConflictError` / HTTP `409`.
- Missing product, Draft, or materialization -> `NotFoundError` / HTTP `404`.
- Folder/node/edge/materialization/key/event flush or commit failure -> rollback the complete transaction; the previous
  active workflow and confirmed Draft remain retryable.
- Invalid or negative SSE cursor -> HTTP `400`/`422` according to whether it came from `Last-Event-ID` or query parsing.

### 5. Good/Base/Bad Cases

- Good: confirm revision 2, materialize with expected Draft 2 and workflow revision 1, then atomically receive active v2
  revision 2 plus deterministic reveal events.
- Good: retry a successful request with the same key and request hash, or use a new key for the same Draft revision; both
  return the same workflow without adding DAG rows.
- Base: query a product with only a v1 workflow through the v2 endpoint and receive an empty v2 projection without
  modifying the v1 workflow.
- Bad: update a confirmed revision's JSON/hash in place after the user has confirmed it.
- Bad: deactivate the current workflow, commit, and create the replacement DAG in later transactions.
- Bad: emit transient reveal deltas before the full workflow transaction commits.

### 6. Tests Required

- Artifact unit tests cover unknown fields, quantity boundaries, unique keys/orders, plan/node cardinality, typed edges,
  required product/prompt edges, references, and DAG cycles.
- Application tests cover append-only confirmation, fact lineage, artifact-origin retry, product-scoped idempotency aliases,
  stale versions, cross-product assets, active-v1 rejection, replacement rollback, and product deletion cascades.
- Failure-injection tests raise during folder, node, edge, and reveal-event flushes and assert zero partial rows plus the
  unchanged previous active workflow.
- API tests cover the empty read, full lifecycle/projection, strict request rejection, v1 guards, SSE cursor replay, and
  replay read-only behavior.
- Migration tests run upgrade/downgrade/re-upgrade on SQLite and an isolated PostgreSQL database containing a real v1
  workflow. Run full backend tests and Ruff after contract changes.

### 7. Wrong vs Correct

Wrong:

```python
legacy = get_or_create_product_workflow(session, product_id)
legacy.active = False
session.commit()
create_v2_nodes_in_separate_transactions(session, draft.payload_json)
```

Correct:

```python
result = materialize_workflow_draft(
    session,
    product_id=product_id,
    draft_id=draft_id,
    expected_draft_version=draft_version,
    expected_workflow_revision=workflow_revision,
    idempotency_key=request_id,
)
```

The command validates lineage and owns one commit/rollback boundary for the full replacement.

---

## Scenario: VisualSystem, Prompt Artifact, and schema-v2 DAG execution

### 1. Scope / Trigger

- Trigger: changes to strict visual or prompt payloads, VisualSystem confirmation/reuse, prompt/image node execution,
  provider-neutral generation settings, v2 node/workflow-run APIs, DAG scheduling, or canonical generation history.
- This scenario applies only to materialized workflow/node `schema_version = 2`. Legacy `copy_generation`, batched
  `image_generation`, downstream result slots, and legacy artifact writes remain isolated in the v1 executor.

### 2. Signatures

- Strict contracts in `application/workflow_drafts/contracts.py`:
  - `VisualSystemDraftPayload`, `VisualExceptionPlan`, `ImagePromptPayloadV1`, and `GenerationSpec`;
  - every model is frozen and uses `extra="forbid"` through `StrictArtifactModel`.
- Confirmation/materialization:
  - `confirm_workflow_draft_revision(...)` fixes `WorkflowDraftRevision.visual_system_version_id`;
  - `materialize_workflow_draft(...)` fixes `ProductWorkflow.visual_system_version_id`, creates one
    `ImagePromptArtifact` per image type, and binds each prompt node to an initial immutable version.
- Runtime:
  - `submit_v2_workflow_node_run(session, *, node_id, enqueue=None) -> V2WorkflowNodeRunSubmission`;
  - `submit_v2_workflow_run(session, *, product_id, workflow_id, enqueue=None) -> V2WorkflowRunSubmission`;
  - `get_v2_workflow_run(...)`, `list_v2_workflow_runs(...)`, `cancel_v2_workflow_run(...)`, and
    `retry_v2_workflow_run(...)` own workflow-level inspection and control;
  - `get_v2_workflow_node_run(session, *, node_run_id) -> WorkflowNodeRun`;
  - `list_v2_workflow_node_runs(session, *, node_id, limit=20) -> tuple[WorkflowNodeRun, ...]`;
  - `cancel_v2_workflow_node_run(session, *, node_run_id) -> WorkflowNodeRun`;
  - `get_v2_workflow_node_detail(...) -> V2WorkflowNodeDetail`;
  - typed `update_v2_reference_node`, `update_v2_prompt_node`, and `update_v2_image_node` commands;
  - `execute_v2_workflow_node_run(session, *, node_run_id, dependencies=None, storage=None) -> bool` persists one node
    outcome and tells the worker whether to enqueue the shared workflow scheduler;
  - `_execute_product_workflow_run(...)` schedules both schema-v1 and schema-v2 runs from persisted DAG state;
  - `PromptGenerationProvider.generate_prompt(PromptGenerationRequest) -> PromptGenerationResult`;
  - `ImageProvider.generate_workflow_image(WorkflowImageRequest) -> WorkflowImageResult`.
- HTTP:
  - `POST /api/v2/products/{product_id}/workflows/{workflow_id}/runs -> 202 SubmitWorkflowRunV2Response`;
  - `GET /api/v2/products/{product_id}/workflows/{workflow_id}/runs[/{run_id}]`;
  - `POST /api/v2/products/{product_id}/workflows/{workflow_id}/runs/{run_id}/cancel|retry`;
  - `POST /api/v2/workflow-nodes/{node_id}/run -> 202 SubmitWorkflowNodeRunV2Response`;
  - `GET /api/v2/workflow-node-runs/{node_run_id} -> WorkflowNodeRunV2Response`;
  - `GET /api/v2/workflow-nodes/{node_id}/runs` and `POST /api/v2/workflow-node-runs/{node_run_id}/cancel`;
  - `GET|PATCH /api/v2/products/{product_id}/workflows/{workflow_id}/nodes/{node_id}` for typed detail/edit.

### 3. Contracts

- No VisualSystem rows are built in or seeded. Draft mode creates a user-owned stable identity plus immutable version;
  `confirmed_version` resolves the exact supplied version ID and never follows a latest-version pointer.
- `locked_fields` and discriminated override value schemas define the only fields a confirmed `VisualException` may
  replace. Scope is exactly workflow, image type, or image plan, with key requirements enforced by the scope type.
- A saved VisualSystemVersion may be consumed by another product. Its version-owned reference assets are authorized
  across products for prompt-generation input; ordinary Draft references and Prompt evidence still require current-product
  ownership. Draft references plus saved-version references are de-duplicated and capped at six during confirmation.
- Each image type owns one `ImagePromptArtifact`. Its immutable payload contains shared rules and the complete ordered
  per-image plan list. A prompt run appends one version and atomically moves only the prompt node's current pointer.
- OpenAI prompt generation sends actual `input_image` content parts next to stable asset metadata. Data URLs exist only
  while constructing the provider request and are never persisted in versions, node outputs, logs, or run DTOs.
- A workflow-scoped v2 submission creates one `WorkflowRun` with one `WorkflowNodeRun` for every runnable
  `prompt_generation` and `image_generation` node. Product-context and reference nodes remain persisted context and never
  receive synthetic execution rows. A node-scoped submission keeps one node run inside one workflow run.
- `progress_metadata.run_scope` distinguishes `workflow` and `node`. Repeating an identical active submission returns the
  existing run. Any active run that overlaps only part of the requested node set returns `409`, including the window where
  an overlapping node run has already succeeded but its owning workflow run is still active.
- The shared scheduler reads real edges. In-run dependencies wait for success, independent ready branches dispatch in the
  same wave, and dependencies outside a partial/retry run are treated as reusable persisted context. A failed upstream
  marks only its queued descendants as `上游节点失败`; independent branches continue.
- Prompt/image executors finish only their own node run. They enqueue the workflow scheduler after success or failure; the
  scheduler alone sets the workflow run terminal state. Duplicate node delivery remains safe through the atomic queued to
  running claim.
- Manual retry requires a failed retryable run and creates a new workflow-scoped run containing failed and blocked nodes
  only. Successful branch rows and artifacts remain history. Retry metadata preserves `source_run_id`, `manual_retry`, and
  failure classification across later failures.
- Each image node run calls the adapter once, requires exactly one returned image, stages one MediaObject plus one
  ProductImageAsset, appends one generation record, and moves the node's `bound_image_asset_id`. A rerun leaves the prior
  asset, node run, generation record, compiled prompt, and reference rows intact.
- Generation evidence has three independent sources:
  - `requested_spec_json`: the exact provider-neutral `GenerationSpec` read before provider invocation;
  - `effective_parameters_json`: parameters the adapter actually sent after mapping/fallback plus explicit notes;
  - `actual_media_json`: MIME, width, height, byte size, and SHA-256 decoded from returned bytes.
- Provider request/output metadata is recursively JSON-checked and removes inline image values before persistence. The v2
  node-run API exposes evidence fields and provider identifiers, but not raw provider request/output or storage paths.
- Prompt/image success never creates `CreativeBrief`, `CopySet`, `SourceAsset`, or `PosterVariant`. Worker and scheduler
  dispatch by workflow schema, and each executor rejects the other schema again at its own entry.
- Every v2 edit locks the active workflow and target node, compares `expected_edit_version`, and rejects any actual change
  while the target node has a queued/running node run. Reference semantic changes also fence reachable prompt/image runs.
- Prompt edits compare `expected_prompt_artifact_version_id`, preserve image-plan keys/order, validate fact, visual variant,
  and product-owned evidence references, append one immutable artifact version, and reset dependent image nodes to idle.
  Image edits retain materialization lineage/config keys; generation changes reset the node while delivery-only changes keep
  the successful source asset and status. One successful command increments `workflow.edit_version` exactly once; no-op
  submissions keep it unchanged.
- Workflow and node history listings are bounded to 1..50 records and validate schema-v2 ownership. Cancellation uses the
  existing workflow-run transition, preserves node-run history, and writes the stable cancelled reason. Startup recovery
  re-enqueues active runs with queued nodes and active runs whose node rows are all terminal but whose workflow row still
  needs finalization.
- Direct canonical asset deletion returns `409` while any node binding, VisualSystem reference, Prompt evidence,
  generation result, or generation reference exists. Product deletion removes owned unused VisualSystem versions; an
  external product consumer that needs a source-product visual reference blocks deletion before any partial mutation.

### 4. Validation & Error Matrix

- Unknown visual/prompt field, invalid override value, duplicate business key, or prompt/image-plan mismatch -> strict
  Draft validation error / HTTP `422` or application `400` according to the entrypoint.
- Missing VisualSystemVersion, payload/hash drift, unknown variant, unlocked exception field, or combined references over
  six -> confirmation failure; revision remains awaiting confirmation with no fact/visual binding.
- Missing/cross-product ordinary asset, unreadable media, unsupported MIME, or image node without an explicit upstream
  reference asset -> non-retryable input conflict; no provider result is persisted.
- Provider changes image-plan order, facts, evidence, or visual variant outside supplied inputs -> non-retryable
  `provider_contract` failure; prompt current version remains unchanged.
- Provider returns zero/multiple images, non-image bytes, or non-JSON metadata -> failed run; no canonical asset or
  generation record remains. A staged file is removed by `StorageWriteCompensation` when commit fails.
- Initial workflow queue delivery failure -> the run and its queued node rows become failed with the stable
  queue-unavailable message. Node dispatch failure fails the owning run and prevents already dispatched work from
  persisting after the run becomes terminal.
- Active overlap -> idempotent response only when scope and complete node set match; partial overlap -> HTTP `409` without
  adding run rows.
- Prompt/image failure -> that node row fails; queued descendants become blocked, independent branches remain runnable,
  and finalization copies the strongest retryability metadata to the workflow run.
- Cancelled, succeeded, non-retryable, or otherwise non-failed source passed to retry -> HTTP `400`/`409`; no retry run is
  created.
- v1 node submitted to the v2 API/executor, or v2 node sent through the legacy runtime -> `ConflictError` / HTTP `409`.
- Stale workflow edit version, stale Prompt Artifact version, inactive workflow, type/request mismatch, or queued/running
  target node -> `409`; the complete edit transaction rolls back.
- Prompt image-plan drift, unknown fact/visual variant, or cross-product evidence asset -> `400`/`422`; no artifact version
  or partial node update remains.

### 5. Good/Base/Bad Cases

- Good: one image type requests two images; materialization creates one prompt node/artifact and two independently
  runnable image nodes.
- Good: product B reuses product A's fixed VisualSystemVersion; prompt input includes B's product reference and A's saved
  visual reference, and a subsequent prompt version may retain that visual asset as evidence.
- Good: rerun one image node with another provider; the current asset changes while both generation records retain their
  provider metadata and the same immutable prompt/visual version IDs.
- Good: submit a prompt with two image descendants; the scheduler dispatches the prompt once, then dispatches both images
  in parallel inside the same workflow run.
- Good: one branch fails while another succeeds; retry creates a new run for the failed branch and its blocked descendants
  without rerunning the successful branch.
- Base: an adapter cannot send a requested field; omit it from effective fields and append a mapping/fallback note while
  actual dimensions continue to come only from decoded bytes.
- Bad: loop over runnable nodes in the browser and call the node-run endpoint to simulate one complete workflow run.
- Bad: copy VisualSystem or prompt payload JSON into every node config and let later edits silently diverge.
- Bad: set effective/actual dimensions from `GenerationSpec`, keep only the newest asset, or truncate a multi-image
  provider response to the first image.

### 6. Tests Required

- Contract tests cover strict unknown-field rejection, locked override types, visual/prompt reference ownership and limit,
  per-type artifact cardinality, and image-plan equality.
- Prompt tests inspect the actual OpenAI content-part array, exercise cross-product saved visual references across two
  consecutive prompt runs, and assert no data URL/base64 or legacy artifact rows are persisted.
- Image tests cover every adapter mapping, exact-one-result rejection, actual byte inspection, metadata sanitization,
  rerun history, provider switching, commit compensation, and zero legacy writes.
- Queue/API tests cover full-run and node-run idempotency, partial overlap, schema dispatch/rejection, queue failure,
  workflow-level get/list/cancel/retry, strict evidence DTOs, and absence of raw provider/storage fields.
- Scheduler tests cover prompt-to-image waits, image-to-image order, parallel ready branches, blocked descendants,
  successful-branch exclusion on retry, duplicate delivery, all-terminal recovery, and terminal workflow finalization.
- Node-edit tests cover immutable prompt append, no-op behavior, stale workflow/artifact versions, prompt-plan drift,
  generation-versus-delivery state changes, reference staleness, title-only active-run conflicts, and rollback.
- Node-run API tests cover bounded ordered history and cancellation through the persisted run state machine.
- Migration verification runs SQLite constraints plus isolated PostgreSQL 16
  `20260811_0032 -> 20260812_0033 -> 20260811_0032 -> 20260812_0033`, with real v1 sentinel rows, real v2 writes, and an
  unchanged storage-file hash across downgrade.
- Run `just backend-test`, `uv run --directory backend ruff check .`, `pnpm --dir web test:run`, `pnpm --dir web lint`,
  and `just web-build` before commit.

### 7. Wrong vs Correct

Wrong:

```python
for node in runnable_nodes:
    submit_v2_workflow_node_run(session, node_id=node.id)
```

Correct:

```python
submission = submit_v2_workflow_run(
    session,
    product_id=product_id,
    workflow_id=workflow_id,
)
```

The workflow-level submission persists one run before queue delivery. The shared scheduler owns dependency waves and final
state; each image node still persists exactly one canonical ProductImageAsset plus immutable generation lineage.

## Scenario: Schema-v2 canvas folders and user workflow recipes

### 1. Scope / Trigger

- Trigger: changing schema-v2 folder membership/layout, typed node/edge structure commands,
  `ProductWorkflow.edit_version`, recipe extraction/versioning, recipe application, or the version-zero WorkflowDraft
  state.
- These contracts apply to materialized schema-v2 workflows. Schema-v1 workflow mutation and `UserCanvasTemplate`
  remain separate compatibility paths.

### 2. Signatures

- Database authority:
  - `ProductWorkflow.edit_version >= 0` covers canvas layout, folder title, and membership changes;
  - `WorkflowFolder` stores `workflow_id`, stable `folder_key`, title, and sort order; it has no geometry, config, status,
    ports, or execution rows;
  - `WorkflowRecipe` is a stable identity and `WorkflowRecipeVersion` is an append-only schema-v1 snapshot;
  - `WorkflowDraftRecipeSeed` binds one version-zero Draft to one immutable recipe version and optional fragment base
    workflow revision.
- Canvas APIs under `/api/v2/products/{product_id}/workflows/{workflow_id}`:
  - `POST /folders`, `PATCH /folders/{folder_id}`, `PUT /folders/{folder_id}/members`,
    `DELETE /folders/{folder_id}`, `POST /folders/{folder_id}/translate`, and `PATCH /layout`;
  - `POST /reference-nodes`, `POST /nodes/{node_id}/duplicate`, `DELETE /nodes/{node_id}`,
    `POST /edges`, and `DELETE /edges/{edge_id}`;
  - every request carries `expected_edit_version`; every response returns `changed`, latest `edit_version`, sorted
    `dissolved_folder_ids`, and the complete latest workflow.
- Recipe APIs:
  - list/detail current recipes, create from `workflow|folder|selection`, append a version, archive with expected version,
    and apply to a product with an idempotency key.

### 3. Contracts

- Node `position_x/position_y` is the only persisted canvas geometry. Folder bounds are derived from members in the
  frontend. Translating a folder adds one delta to every member in one locked transaction.
- A folder must contain at least one schema-v2 node. Exact-set member replacement may move nodes across folders; any
  emptied source folder is deleted in the same transaction and its nodes remain. Folder IDs are never valid members, so
  nested folders cannot be represented by the backend contract.
- Successful canvas changes increment `edit_version` once. No-op rename, zero translation, or unchanged layout/member
  sets keep the version unchanged. `revision` remains the complete Draft materialization sequence.
- Typed structure commands lock and validate the complete current graph in one transaction. They reject inactive/schema-v1
  workflows, stale edit versions, duplicate edges, unsupported node-type pairs, cycles, missing lineage, and any topology
  change while a `WorkflowRun` for the workflow remains `running`, including the all-node-runs-terminal finalization
  window.
- Reference creation is the only standalone node-create contract. Duplicate preserves a reference binding, creates one
  image variant with a matching Prompt Artifact image plan, or copies one prompt node plus its complete image group and
  internal lineage. Product context remains a singleton and cannot be copied or deleted. Reference nodes remain capped at
  six; image nodes remain capped at six per type and thirty per workflow.
- Server-owned handles are `facts -> facts`, `asset -> reference`, `prompt -> prompt`, and `image -> reference` for the
  supported type pairs. Product-context-to-prompt and matching-prompt-to-image lineage edges cannot be deleted. Optional
  edge changes reset reachable runnable nodes to idle and clear their failure state.
- Deleting one image node appends a Prompt Artifact version without that image plan. Deleting the final image deletes its
  prompt group; deleting a prompt deletes the complete owned image group. Connected edges and node-run rows are removed,
  and empty folders dissolve in the same transaction. A bound `ProductImageAsset` remains in the product gallery after
  its node is deleted.
- Alembic `20260813_0036` removes persisted folder geometry after deleting empty schema-v2 folders. Empty schema-v1
  folders remain. Downgrade fails while any recipe/recipe-version/recipe-seed data exists; operators must export or remove
  that data explicitly before retrying.
- `RecipePayloadV1` is built through a whitelist from the current runtime nodes, folders, edges, current Prompt Artifact
  versions, and current Generation/DeliverySpec values. Source Draft and VisualSystem hashes remain lineage-integrity
  gates; they do not replace edited runtime graph authority. Recipe-local keys may represent graph shape, relative
  positions, image types/counts, generation/delivery specs, prompt field shape, reference roles, boundary requirements,
  and visual requirements. Product facts, asset/entity IDs, prompt prose, outputs, cover state, provider data, and run
  history are rejected recursively. Limits are 128 nodes, 256 edges, 32 folders, and 512 KiB canonical JSON.
- Recipe kind is immutable. A `workflow_recipe` version must be extracted from a complete workflow; a `recipe_fragment`
  version must come from a folder or non-empty selection.
- Applying a recipe creates a collecting Draft with no revision, an immutable seed, and one Agent conversation. It does
  not create workflow/folder/node/edge rows. The first strict artifact appends Draft version 1 from expected version 0;
  confirmation and materialization still require a positive revision.
- `preferred_visual_system_version_id` is a suggestion retained by the recipe version. Its `RESTRICT` reference means a
  source product cannot be deleted while a saved recipe still depends on that product-owned visual version.

### 4. Validation & Error Matrix

- Missing/inactive/schema-v1 workflow, cross-workflow node/folder, or stale `expected_edit_version` -> `404` or `409`;
  the mutation rolls back.
- Empty create set, duplicate node IDs, invalid title, nested/non-v2 member, or empty layout batch -> validation failure.
- Any running workflow run during node/edge create, duplicate, or delete -> `409`; no graph, Prompt Artifact, status, or
  edit-version change is committed.
- Unsupported node pair, duplicate edge, cycle, protected-lineage deletion, orphaned prompt/image group, or reference/image
  capacity overflow -> `400`/`409`; the complete structure mutation rolls back.
- Recipe source kind differs from immutable recipe kind, recipe version is stale/archived, or source Draft/visual hash
  drifts -> `400`/`409`; no version is appended.
- Forbidden key or any current entity ID appears anywhere in the extracted payload -> fail-closed validation and full
  transaction rollback.
- Same product/idempotency key with the same recipe request -> return the existing Draft/conversation; the same key with
  different recipe/version input -> `409`.
- Downgrade with saved recipe data -> explicit runtime failure before destructive table drops.

### 5. Good/Base/Bad Cases

- Good: move the last member from folder A into folder B; one response reports A as dissolved, keeps every node, and
  increments `edit_version` once.
- Good: duplicate an image node; one new image plan is appended to a new immutable Prompt Artifact version, the new node
  receives canonical lineage edges, and the source image asset/output history is not copied.
- Good: delete an image node whose generated asset is in the gallery; the node and its graph lineage disappear while the
  canonical gallery asset remains available.
- Good: edit runtime generation specs and the current Prompt Artifact, duplicate a prompt group, and save a recipe; the
  saved version reflects the current graph rather than the original Draft topology.
- Good: save a full recipe from product A, apply it to product B, let the Agent bind B's facts and references, then confirm
  and materialize version 1.
- Base: archive a recipe identity; existing immutable versions and Draft seeds remain readable while default listing no
  longer returns the recipe.
- Bad: persist a second folder rectangle, treat a folder as an executable node, or increment workflow `revision` for a
  drag operation.
- Bad: copy `config_json`, asset IDs, completed prompts, or image outputs into a recipe and attempt to sanitize them with
  string replacement.

### 6. Tests Required

- Folder application/API tests cover CRUD, cross-folder moves, automatic dissolution, no-ops, translation, batch layout,
  stale edit version, duplicate/unknown/cross-workflow/v1 inputs, and complete response serialization.
- Graph-command application/API tests cover standalone reference creation, all supported duplicate/delete paths, exact
  edge pairs, cycle/duplicate/lineage rejection, capacity limits, active workflow-run locking, rollback, edit-version
  increments, folder dissolution, and gallery-asset retention after node deletion.
- Recipe tests cover full/folder/selection extraction, current runtime Prompt Artifact and generation-spec changes,
  duplicated groups, local key allocation, boundary summaries, recursive leak rejection, append-only versions, archive,
  idempotent application, version-zero artifact sync, and cross-product materialization.
- Migration tests retain an empty schema-v1 folder, remove an empty schema-v2 folder, preserve member folders, perform a
  safe round trip, reject unsafe downgrade, and upgrade SQLite to head.
- Run the isolated PostgreSQL canvas/recipe gate because row locks, partial unique indexes, enum storage, and transaction
  rollback cannot be established by SQLite alone.

### 7. Wrong vs Correct

Wrong:

```python
folder.position_x += delta_x
workflow.revision += 1
```

Correct:

```python
for node in locked_members:
    node.position_x += delta_x
    node.position_y += delta_y
workflow.edit_version += 1
```

The backend persists one coordinate system and one canvas edit sequence; folder cards remain a derived projection.

Wrong:

```python
recipe_payload = extract_recipe_payload(source_draft.payload_json)
```

Correct:

```python
recipe_payload = extract_recipe_payload(
    workflow=current_runtime_workflow,
    visual_system_payload=visual_system_payload,
    source_type=source_type,
    folder_id=folder_id,
    node_ids=node_ids,
    forbidden_entity_ids=forbidden_entity_ids,
)
```

Source Draft and visual hashes verify lineage. The saved recipe structure and generation intent come from the current
runtime workflow so manual canvas edits are preserved.

## Scenario: Deterministic delivery renditions

### 1. Scope / Trigger

- Trigger: changing `DeliverySpec`, schema-v2 image persistence, delivery rendition jobs, Pillow rendering, rendition
  recovery/retry, canonical asset deletion, gallery lineage, or delivery-rendition API routes.
- The generated source remains the image node's current asset. A delivery rendition is a deterministic child asset with
  its own durable task state and cannot call an image provider.

### 2. Signatures

- Contract: `DeliverySpec(width, height, format, max_byte_size?, fit, background_color?, crop_anchor?)`, schema version 1,
  with `width * height <= 67_108_864`.
- Application: `create_delivery_rendition_job(...)`, `submit_delivery_rendition_job(...)`,
  `claim_delivery_rendition_job(...)`, `execute_delivery_rendition_job(...)`, and
  `retry_delivery_rendition_job(...)`.
- Durable actor: `run_delivery_rendition_job(job_id)`, `max_retries=0`; the database row is authoritative.
- API:
  - `POST|GET /api/v2/product-image-assets/{source_asset_id}/renditions`;
  - `GET /api/v2/delivery-rendition-jobs/{job_id}`;
  - `POST /api/v2/delivery-rendition-jobs/{job_id}/retry`.
- Database: migration `20260814_0037`; idempotency key `(source_asset_id, spec_hash)`; public states are exactly
  `queued`, `running`, `succeeded`, and `failed`.

### 3. Contracts

- Canonicalize a validated DeliverySpec with sorted compact JSON and SHA-256. Repeated source/spec submissions reuse one
  active task or successful result. A failed task changes state only through the explicit retry command.
- Only verified, top-level `ProductImageAsset` rows produced by a schema-v2 `WorkflowImageGenerationRecord` are valid
  sources. Uploaded assets and existing child renditions are rejected.
- Image-node success persists the generated source, generation record, node/run success, and optional queued rendition
  job atomically. Queue submission occurs after commit. Queue or rendition failure updates only the rendition job.
- Claim is a conditional `queued -> running` update that assigns a persisted attempt ID. Completion and failure writes
  require the same active attempt ID; recovery clears stale attempts before requeueing, so late workers cannot overwrite
  the current attempt.
- Rendering fixes EXIF orientation, preserves aspect ratio, applies deterministic contain/pad or cover/crop behavior,
  emits the requested PNG/JPEG/WEBP format, and inspects the encoded bytes again. Exact width, height, MIME, byte count,
  and SHA-256 come from the emitted bytes.
- A successful result is a new canonical `ProductImageAsset` whose `parent_asset_id` points to the generated source.
  The rendition does not replace the node's current source, increment the planned generation count, or enter provider
  generation lineage as a new candidate.
- Source and result assets are protected by application checks and `RESTRICT` foreign keys while a rendition job exists.
  Product aggregate deletion removes rendition jobs before canonical assets through the existing cascade path.
- API responses expose the validated spec, bounded failure reason, attempts, retryability, timestamps, and optional
  canonical result serializer. They do not expose the spec hash, active attempt, storage path, or image bytes.

### 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Missing source/job | `404` |
| Invalid DeliverySpec, upload source, or recursive child source | `400`/`422`; no job or asset is written |
| Retry requested for active, successful, or non-retryable failed job | `409`; current row is unchanged |
| Queue unavailable after a new submission | `503`; the job remains visible as retryable `failed` |
| Exact size/format/max-byte contract cannot be met | Non-retryable `failed`; source and workflow success remain unchanged |
| Duplicate queue message | At most one claim succeeds; later messages are no-ops |
| Stale worker writes after recovery | Attempt mismatch rejects both completion and failure writes |
| Asset delete while referenced by a rendition job | `409`, with the rendition reference reported before generic child use |

### 5. Good / Base / Bad Cases

- Good: one generated PNG source yields exact JPEG and WEBP child assets for two DeliverySpecs without another provider
  call; both children remain traceable to the same source.
- Base: an image plan without DeliverySpec persists only its generated source and generation record.
- Bad: overwrite the generated source, silently switch output format, stretch pixels, or satisfy a byte limit by changing
  dimensions.
- Bad: model rendition status as a workflow node, mutate `WorkflowNodeRun.output_json` after node success, or rely on a
  Dramatiq message as the task authority.

### 6. Tests Required

- Pure renderer tests cover contain transparency/JPEG background, all cover anchors, PNG/JPEG/WEBP, EXIF, deterministic
  byte-limit ladders, unsupported encoders, and post-encode measured metadata.
- Application/API tests cover source eligibility, normalized idempotency, no-spec behavior, queue failure, workflow
  success isolation, deletion conflicts, strict response fields, explicit retry, duplicate claim, and attempt fencing.
- Migration tests cover checks, indexes, `RESTRICT`/`CASCADE`, enum values, and refusal to downgrade populated jobs.
- Run the opt-in PostgreSQL/Redis gate `just backend-test-live-delivery-renditions`; it must use a temporary PostgreSQL
  database, real Redis messages, and real PNG/JPEG/WEBP files.

### 7. Wrong vs Correct

Wrong:

```python
result = image_provider.generate(prompt=f"resize to {spec.width}x{spec.height}")
source_asset.media_object = save(result)
```

Correct:

```python
claim = claim_delivery_rendition_job(session, job_id=job_id)
if claim is not None:
    execute_delivery_rendition_job(job_id=job_id, attempt_id=claim.attempt_id)
```

The deterministic worker emits a child asset and leaves the successful generation record and node current asset intact.
