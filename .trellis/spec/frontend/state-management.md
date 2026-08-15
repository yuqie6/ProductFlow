# Frontend State Management

## State Categories

ProductFlow uses:

1. TanStack Query for server state.
2. React state/reducers for local interaction and editable drafts.
3. React Router for page and product identity.
4. Narrow localStorage records for global preferences and canvas ergonomics.

There is no Redux/Zustand/Jotai store or custom global event bus.

## Server State

`web/src/lib/api.ts` is the server boundary. The QueryClient is created once in `App.tsx` with focus refetch disabled.

Feature query keys are owned near their hooks/pages. Current scopes include:

- `["session"]`
- product list and product detail
- Agent workspace/bootstrap/conversation/Turn pages
- active workflow and node detail
- workflow/node runs
- WorkflowDraft/materialization
- WorkflowRecipe list/detail
- product image-library bootstrap/pages
- image sessions and session status
- Gallery
- settings lock/config/runtime/provider profiles/bindings

Use ids in keys for product/workflow/node/conversation/session scoping. Do not combine unrelated products under one mutable cache entry.

## Mutation Invalidation

Invalidate or update every projection that is visibly affected, using the narrowest authoritative scope.

Examples:

- save image-session result to product -> target product detail, product library pages, product list cover if needed;
- bind reference node -> active workflow and node detail;
- edit node -> active workflow/node detail and save status;
- apply recipe -> active workflow, recipe list if version metadata changes;
- provider binding change -> settings and current runtime provider projection;
- settings access toggle -> session.

Avoid `queryClient.clear()` or whole-app invalidation after a narrow mutation.

## Agent State

ProductFlow's persisted AgentTurn projection is server state. The active stream also has local reducer state for immediate deltas.

`useAgentConversation` / `useAgentTurnEvents` own:

- bounded older-page loading;
- active Turn;
- SSE sequence and reconnect cursor;
- text/refusal deltas;
- questions;
- cancel/resume state;
- WorkflowDraft artifact linkage.

After reconnect or terminal state, refetched projection is authoritative. Deduplicate events by sequence.

Creation workspace identity is durable server state. Reloading the page retrieves the existing workspace snapshot using the conversation/idempotency contract.

## Workflow State

The active ProductWorkflow, node details, runs, recipes, and image library are server state.

Local state includes:

- selection;
- open folder;
- viewport;
- command/dialog state;
- inspector tabs and editable drafts;
- pending edge or drag interaction;
- transient reveal animation state.

Graph mutations return canonical server projections. Reconcile optimistic visuals against them.

`useV2NodeDraftAutosave` owns node-edit draft normalization, debounce, mutation state, and conflict feedback. Do not duplicate autosave in each inspector section.

## Creation Form State

AgentProductCreatePage keeps short-lived values locally:

- product name;
- selected image types;
- per-type quantities;
- selected upload files;
- local validation messages.

After workspace creation, Product/Draft/Conversation are server state. Do not persist an independent onboarding model in localStorage.

## Image Chat State

Server state:

- session list/detail/status;
- assets, rounds, generation tasks;
- product list used for save-to-product.

Local state:

- selected session/result;
- prompt;
- size and candidate count;
- branch base and selected context assets;
- advanced tool options;
- target product;
- drawers/sheets and transient feedback.

Do not copy full session detail into local state. Derive selected round/result by id.

## Settings State

SettingsPage stores editable drafts locally after loading server definitions/profiles/bindings.

- The unlock token exists only for the unlock submission.
- Secret fields track whether the user touched them.
- Saved secrets never enter query data.
- Reset/save completion refetches authoritative settings.

## URL State

React Router owns:

- current page;
- `productId`;
- settings section/search params where supported.

Do not use localStorage or a global store for the current product/page.

## Global Preferences

`PreferencesProvider` owns:

- `productflow.locale`
- `productflow.theme`

It updates document language, resolved dark class, and theme attributes. Locale/theme are browser preferences, not backend settings.

## Canvas Persistence

The current canvas record uses:

```text
productflow.workflowV2.canvasState.v1:{workflow_id}
```

It stores:

- schema version;
- open folder id;
- global and per-folder viewport;
- required surface width/height used for compatibility checks.

The parser accepts only finite bounded coordinates/zoom and the exact current record shape. Invalid or mismatched state is discarded. Deleted folder state is removed during reconciliation.

Other workbench preference keys must have one clear owner, parser, bounds, and focused tests. Avoid accumulating compatibility parsing for retired browser records.

## Derived State

Prefer pure derivation:

- ProductSummary cover display from current cover fields.
- graph nodes/edges from ProductWorkflowV2.
- selected node detail from id.
- image-library directory selection and page filters.
- WorkflowDraft confirmation sections.
- provider usage from current purpose bindings.
- image-session branch/context count.

Use `useMemo` for expensive or referentially important projections, not for every trivial value.

## Error State

`ApiError` is the common HTTP failure. Keep action-specific user feedback near the affected page/panel.

- Auth error in Login.
- Intake/upload/Agent error in creation.
- Graph/edit/run error in the workbench surface.
- Session generation/save error in ImageChat.
- Provider/config validation error in Settings.

Persisted failed/unknown business status is server state; a toast alone is insufficient.

## Tests

Add focused tests for:

- query invalidation after mutations;
- Agent reducer sequence/reconnect;
- autosave normalization/conflict;
- creation default/custom quantities;
- canvas localStorage parsing/reconciliation;
- image-chat branch/context derivation;
- settings secret/unlock state.

## Avoid

- Global store for cached API data.
- Local copies of whole server records.
- Broad invalidation for narrow mutations.
- Page identity outside the URL.
- Admin/settings/provider secrets in localStorage or Query cache.
- Unversioned persistent browser records.
- Hidden fallback parsing for a removed workflow/page.
