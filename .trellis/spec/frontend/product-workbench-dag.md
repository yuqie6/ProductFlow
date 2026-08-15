# Frontend Product Workbench DAG Guidelines

## Scope

Read this guide before changing:

- `AgentProductCreatePage.tsx`
- `ProductWorkbenchPage.tsx`
- `pages/agent-workbench/`
- `pages/product-workflow-v2/`
- the retained canvas, node-card, sidebar, shortcut, and image-explorer components under `pages/product-detail/`

The product workbench is an upgrade of the established canvas interaction. New Agent behavior must compose with existing node editing, edges, layout, gallery, and sidebar controls.

## Route Contract

- `/products/new` renders the full-screen Agent creation experience.
- `/products/new/agent` redirects to `/products/new` without maintaining a second implementation.
- `/products/:productId` renders `ProductWorkbenchPage`.
- `ProductWorkbenchPage` resolves the active product id and mounts `AgentProductWorkbenchPage`.
- The workbench uses the backend-provided Agent bootstrap and active schema-v2 workflow.

Do not create a parallel workbench route for the canvas.

## Creation Flow

The creation screen has four persistent stages:

1. Product name.
2. Image-type selection with an independent quantity for each selected type.
3. One to six real product uploads.
4. Agent conversation, questions, and Draft confirmation.

Each selected image type defaults to quantity two. Quantity is product intent, not a provider advanced field.

The initial workspace request is idempotent. A page reload uses the conversation/workspace snapshot instead of creating a duplicate Product.

After confirmation:

- materialization starts once;
- reveal events append folders, nodes, and edges in sequence order;
- the canvas grows without layout jumps caused by unknown node dimensions;
- the Agent conversation transitions into the workbench sidebar;
- the URL becomes the canonical product route.

## Workbench Composition

`AgentProductWorkbenchPage` owns orchestration and composes:

- `AgentWorkbenchShell` for responsive page layout;
- `AgentConversationPanel` for message history, questions, composer, cancel, and resume;
- `ProductWorkflowV2CanvasPanel` for canvas, toolbar, inspector, runs, recipes, and library;
- `WorkflowDraftConfirmation` when the current Agent artifact needs approval.

Keep orchestration at the page boundary. Node cards and panels receive typed data and callbacks; they do not fetch unrelated product state.

## Canvas Contract

Use the established XYFlow canvas and helpers.

Required capabilities:

- add current node types;
- drag nodes;
- create and delete edges;
- zoom, pan, fit view, and automatic layout;
- click, modifier-click, and marquee multi-selection;
- keyboard delete, undo/redo where implemented, and shortcut suppression inside inputs;
- desktop and touch interaction modes;
- folder projection and translation;
- stable save state and optimistic layout persistence.

Each node card exposes one clear input handle and one clear output handle when the node type accepts those directions. Do not render a separate left-side handle for every semantic input field.

Canvas nodes have stable width/height constraints. Loading, status, image preview, and selection affordances must not resize the graph unexpectedly.

## Node Cards

`WorkflowNodeCard` is the shared visual language. A card shows:

- node type icon and localized title;
- compact purpose/status summary;
- bound reference or current output preview when relevant;
- run state and failure indicator;
- selection/folder state;
- connection handles.

Detailed forms belong in `V2NodeInspector`. Do not turn the card into a dense settings form.

Current node labels and semantics:

- product context;
- reference image;
- prompt generation;
- image generation.

## Inspector

The inspector preserves current editing depth:

- product facts and visual-system context;
- explicit ProductImageAsset binding for reference nodes;
- prompt instruction, artifact version, and prompt preview;
- aspect ratio, resolution tier, quality intent, reference fidelity, background, text policy, and text language;
- delivery specification and rendition jobs;
- node run history and retry/cancel actions.

Use node-specific draft helpers and `useV2NodeDraftAutosave`. Normalize before comparing drafts so autosave does not loop on whitespace or representation differences.

Validation errors remain near the affected control. Server conflict errors trigger a targeted refetch and visible save state.

## Sidebar and Command Surfaces

The existing workbench tool surfaces remain available:

- add-node command;
- workflow/single-node run;
- node details;
- run history;
- product image library;
- user recipe library;
- Agent conversation;
- canvas chrome and layout controls.

On desktop, use the established right-side rail/panel behavior. On narrow viewports, use the existing drawer/bottom-sheet behavior and preserve the canvas as the primary surface.

Do not remove a tool because the Agent can perform a related action. Manual editing remains a first-class workflow.

## Folders

Folders are local visual groups:

- project backend folder membership into canvas bounds;
- keep folder and member selection coherent;
- translate members with the folder;
- show a compact folder header and name;
- allow recipe save from a folder;
- preserve edges crossing folder boundaries.

Folders do not introduce nested canvases or a separate navigation stack.

## Recipes

`RecipeLibraryPanel` lists only user-saved WorkflowRecipe records.

Save sources:

- full workflow;
- selected folder;
- current multi-selection.

The UI collects name/description, displays source scope, submits the current workflow edit version, and invalidates recipe/workflow queries after success. Applying a recipe reveals the inserted graph and retains current selection semantics.

Do not synthesize a recipe catalog in the browser.

## Product Image Library

`ProductImageExplorer` is the canonical product-image selector and manager.

The workbench uses it for:

- general library browsing;
- binding a reference node;
- preview/download;
- folders, rename, move, and multi-select;
- delivery renditions.

Selection mode must state the target node and commit one explicit asset id. Product cover is visual list metadata and must not appear as an automatic reference choice.

Every generated output remains visible in the library even after a node points to a newer result.

## Agent Conversation

`useAgentConversation` and `useAgentTurnEvents` own:

- bounded turn-page loading;
- one active Turn projection;
- SSE sequence deduplication and reconnect;
- token-level text delta reduction;
- questions and answers;
- cancel/resume;
- Draft artifact attachment;
- transition from creation to workbench.

Render persisted projections as authority after reconnect. Optimistic local delta may fill the current response but must converge to the server event sequence.

The composer accepts text and selected product assets according to the current contract. It must not send the complete product library by default.

## Query Keys and Invalidation

Use feature-owned query keys. Common scopes include:

- product list and product detail;
- agent workspace/bootstrap/conversation/turns;
- active workflow, node detail, workflow runs, node runs;
- product image library and gallery asset pages;
- workflow recipes;
- provider/runtime settings.

After a mutation, invalidate the narrow authoritative query and any projection that visibly depends on it. Avoid clearing the entire QueryClient.

## Responsive Requirements

- Verify real `innerWidth` and `clientWidth` before drawing mobile conclusions.
- Fixed canvas controls, node cards, toolbar buttons, and sidebar rails need stable dimensions.
- Long product, folder, recipe, and file names truncate or wrap without overlapping controls.
- Touch panning, node dragging, edge creation, and bottom-sheet gestures must not compete.
- The Agent composer remains reachable when the software keyboard is open.

## Tests Required

Change-specific tests should cover:

- creation selection and default/custom quantity;
- workspace idempotency and route transition;
- SSE reducer, reconnect, questions, and cancellation;
- Draft confirmation and materialization reveal ordering;
- graph adapters, edge validation, layout, selection, and shortcuts;
- node inspector parsing and autosave;
- folder projection and recipe source mapping;
- reference-node selection through the image library;
- desktop/mobile panel state.

Run:

```bash
pnpm --dir web test:run
pnpm --dir web lint
pnpm --dir web build
```

For interaction or layout changes, inspect Playwright screenshots at desktop and mobile widths and verify there is no overlap or blank canvas.

## Forbidden Patterns

- Rebuilding the canvas in the Agent feature.
- Removing manual add, edge, inspector, run, recipe, or library controls.
- One handle per semantic field on the left side of a node.
- Fetching full image bytes for every library item.
- Treating product cover as a workflow reference.
- Maintaining two creation screens or two product-workbench pages.
- Browser-generated default workflows.
- Hidden fallback to a different workflow schema.
- Whole-app query invalidation after a narrow mutation.
