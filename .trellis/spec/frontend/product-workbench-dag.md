# Frontend Product Workbench DAG Guidelines

> Frontend contracts for the product detail node workbench.

## Scenario: Product detail DAG workbench UI

### 1. Scope / Trigger

- Trigger: any ProductDetail page change that renders, edits, runs, or consumes product workflow DAG data.
- This feature spans API DTOs, TanStack Query cache keys, local selected-node state, and artifact previews.

### 2. Signatures

- API methods live only in `web/src/lib/api.ts`:
  - `getProductWorkflow(productId)`
  - `createWorkflowNode(productId, input)`
  - `updateWorkflowNode(nodeId, input)`
  - `updateWorkflowNodeCopy(nodeId, input)`
  - `uploadWorkflowNodeImage(nodeId, input)`
  - `createWorkflowEdge(productId, input)`
  - `deleteWorkflowEdge(edgeId)`
  - `runProductWorkflow(productId, input?)`
- DTOs live only in `web/src/lib/types.ts`: `ProductWorkflow`, `WorkflowNode`, `WorkflowEdge`, `WorkflowRun`,
  `WorkflowNodeRun`.
- Query key: `['product-workflow', productId]`.

### 3. Contracts

- Frontend keeps backend `snake_case` fields (`node_type`, `config_json`, `output_json`, `start_node_id`).
- Supported user-facing node types are `product_context`, `reference_image`, `copy_generation`, and `image_generation`.
- Product detail/workbench is canvas-first: product context, reference slots, copy, and image generation are graph nodes,
  not permanent fixed columns.
- Canvas interaction is pointer-first: nodes move by dragging the node body/header and persist via
  `updateWorkflowNode(...)` on pointer release; pointermove should use `transform`/`translate3d` plus RAF-throttled local
  drag state or an equivalent no-layout-thrash approach.
- Pointer release must not flash the node back to its stale server position. Keep the final drag coordinates in an
  optimistic position layer and update the `['product-workflow', productId]` cache before/while the PATCH is in flight;
  clear the optimistic entry after the server response becomes the authority, or restore the previous cache on error.
- Edges are created by dragging an output handle to a target handle/node and showing a temporary SVG connection while
  dragging.
- Edge deletion is a canvas action and must call `deleteWorkflowEdge(edgeId)` before refreshing
  `['product-workflow', productId]`; do not leave stale local-only edge state.
- Node deletion is a persisted canvas action and must call `deleteWorkflowNode(nodeId)` before refreshing
  `['product-workflow', productId]`; deleting a node must not be represented by local-only filtering because connected
  edges and run history cleanup are backend responsibilities.
- Workflow execution is asynchronous from the frontend perspective: `runProductWorkflow(productId, input?)` returns the
  persisted kickoff state, then the page polls `['product-workflow', productId]` while any run is `running` or any node is
  `queued` / `running`.
- Do not use the run mutation pending state as a global canvas lock. Split interaction busy state so `runBusy` prevents
  duplicate run clicks, structural mutations can be disabled during active runs, and node dragging remains available unless
  a layout/position mutation is already pending.
- When an active run transitions to inactive, refresh artifact-bearing queries: `['product', productId]`,
  `['product-history', productId]`, and `['products']`.
- Product creation is intentionally minimal: only product name and preview/main image are required; category, price,
  description/context, reference images, copy, and image directions are configured later through canvas nodes.
- Product list deletion must use `api.deleteProduct(productId)`, ask for explicit confirmation, and refresh `['products']`
  after success. Show `ApiError.detail` when active jobs/runs block deletion.
- `reference_image` nodes use `uploadWorkflowNodeImage(...)` for manual uploads and can also be filled by upstream
  `image_generation` nodes.
- `image_generation` output count is represented by downstream graph slots: one generated image per connected downstream
  `reference_image` node. Do not expose candidate count as the primary UX.
- Any node with an image asset/output should render a compact preview directly on the node card.
- Any user-visible product/workbench image preview should provide an explicit `下载` action. Do not rely on browser
  right-click as the only way to retrieve product images.
- Type-specific inspector forms are required for product context, reference image, copy generation, and image generation;
  avoid generic JSON editors for normal user flows.
- A selected `copy_generation` node with a generated `copy_set_id` must show concise editable fields for title, selling
  points, poster headline, and CTA. Saving calls `updateWorkflowNodeCopy(...)`, refreshes workflow/product artifacts, and
  does not expose the raw `copy_set_id`.
- Node output previews should be productized summaries only. Do not render raw `output_json` keys, artifact IDs, or prompt /
  instruction text directly in the inspector; show concise chips such as generated copy, image count, filled reference slot
  count, and size.
- Image download links should use `download_url` when available and fall back to preview URLs only when needed. Always pass
  backend URLs through `api.toApiUrl(...)`, use short visible copy such as `下载`, stop propagation inside node cards, and
  sanitize generated filenames so product names cannot introduce path separators or control characters.
- User-visible copy should be short utility labels such as `商品`, `参考图`, `文案`, `生图`, `运行`, `连接`, `删除`.
- Mutations that create artifacts must refresh `['product', productId]`, `['product-history', productId]`, and
  `['products']` when outputs can affect copy, posters, or list status.

### 4. Validation & Error Matrix

- API `ApiError.detail` is shown near the workflow action.
- Missing workflow while loading -> loading state, not an empty destructive reset.
- Active workflow polling stops when no run is `running` and no node is `queued` / `running`.
- Deleting a node during an active workflow run -> show backend `运行中，稍后删除`; do not locally remove it.
- Deleting a product during active jobs/runs -> show backend detail; do not locally remove it until the API succeeds.
- Unsupported node config fields stay in `config_json` and are not force-cast to narrower frontend-only types.
- Image URLs from workflow-created source assets and poster artifacts still go through `api.toApiUrl(...)`.
- If the backend returns `连接参考图节点`, show that concise error instead of a long explanation.

### 5. Good/Base/Bad Cases

- Good: selecting a node updates the inspector without navigating away from the product detail page.
- Good: an image-generation node connected to two downstream reference slots visibly fills both slot nodes after run.
- Base: adding a copy/image/reference branch creates a node, then connects it with an edge through API helpers.
- Base: after a copy node run succeeds, editing the generated copy updates the inspector draft from product `copy_sets`
  plus node output, and the normal output preview stays a short summary/chip list.
- Base: uploading an image in a `reference_image` inspector refreshes the workflow query and keeps the node output visible
  after a page reload.
- Base: after dragging a node and releasing the pointer, the rendered node stays at the dropped position while the
  position mutation is pending; it must not briefly render the old `position_x` / `position_y`.
- Base: while a workflow run is active, users can still drag nodes to reorganize the canvas, but cannot start duplicate
  runs or make unsafe structural changes.
- Base: deleting a node removes it and its connected edges after the backend response, and a page refresh does not restore
  the node.
- Base: deleting a product from the product list removes it after API success and a direct detail load returns not found.
- Base: visible product images, reference-slot images, generated-image node previews, and image-history thumbnails each
  expose a concise `下载` action that does not select/drag the node or open the preview modal as a side effect.
- Bad: keeping workflow nodes in local-only state; refresh would lose the DAG and break run history.
- Bad: treating `runProductWorkflow` pending as `busy` for all canvas interactions; long provider calls would make drag
  and layout feel frozen.

### 6. Tests Required

- `just web-build` must pass after any DTO or page change.
- Backend API tests should cover workflow payload shapes; the frontend relies on these typed shapes at build time.
- If a separate frontend test runner is added later, cover selected-node inspector, run-all mutation, edge drag/delete, and
  cache invalidation.
- If a separate frontend test runner is added later, cover workflow active-run polling, active-to-inactive artifact query
  refresh, node deletion, and product list deletion error/success states.
- Drag-position regressions should cover the render priority: active drag position, then optimistic dropped position, then
  server workflow position.
- Download-link regressions should cover URL construction through `api.toApiUrl(...)`, filename sanitization, and event
  propagation isolation inside node cards.

### 7. Wrong vs Correct

#### Wrong

```ts
const [nodes, setNodes] = useState(defaultNodes);
```

Local-only nodes do not satisfy the persisted ProductFlow workflow contract.

#### Correct

```ts
const workflowQuery = useQuery({
  queryKey: ["product-workflow", productId],
  queryFn: () => api.getProductWorkflow(productId),
});
```

Load the persisted workflow and keep only transient selection/edit drafts in local state.

#### Wrong

```tsx
Object.entries(node.output_json).map(([key, value]) => <div>{key}: {String(value)}</div>);
```

This leaks internal artifact IDs and prompt-like implementation detail into the product UI.

#### Correct

```tsx
const facts = [`图片 ${posterCount}`, `参考图 ${filledCount}`, size].filter(Boolean);
```

Render concise, user-facing facts and keep raw workflow JSON as an API/debug boundary, not normal UI copy.

#### Wrong

```tsx
setNodeDrag(null);
updateWorkflowNode(node.id, { position_x: x, position_y: y });
```

If the render path falls back to the still-stale query data after `setNodeDrag(null)`, the node flashes back to the old
position until the mutation/refetch completes.

#### Correct

```tsx
setOptimisticNodePositions((positions) => ({ ...positions, [node.id]: { x, y } }));
queryClient.setQueryData(["product-workflow", productId], moveNodeInCache(node.id, x, y));
updateWorkflowNode(node.id, { position_x: x, position_y: y });
```

Keep a short-lived optimistic coordinate and cache update during the mutation, then replace it with the server-returned
workflow on success or restore the previous cache on error.

#### Wrong

```tsx
const busy = runWorkflowMutation.isPending || updateNodePositionMutation.isPending;
if (busy) return;
```

This makes a long async workflow run feel like a frozen canvas even though persisted run/node status is available through
polling.

#### Correct

```tsx
const workflowActive = hasActiveWorkflow(workflow);
const runBusy = runWorkflowMutation.isPending || workflowActive;
const dragBusy = updateNodePositionMutation.isPending;
```

Use persisted workflow activity to control duplicate runs and polling, while keeping layout dragging independent from
provider execution.
