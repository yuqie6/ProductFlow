# Frontend Hook Guidelines

> How React hooks and TanStack Query are currently used in ProductFlow.

---

## Overview

There are no custom hook modules in `web/src/` today. Hooks are used directly inside page components and `AppRoutes()`.
Server state uses TanStack Query; local UI/form state uses React's built-in hooks.

Real hook-heavy files:

- `web/src/App.tsx`
- `web/src/pages/ProductListPage.tsx`
- `web/src/pages/ProductDetailPage.tsx`
- `web/src/pages/ImageChatPage.tsx`
- `web/src/pages/SettingsPage.tsx`

---

## Server State Hooks

Use `useQuery` for reads and `useMutation` for writes. Query keys are small arrays of stable values:

```tsx
const productQuery = useQuery({
  queryKey: ["product", productId],
  queryFn: () => api.getProduct(productId),
  enabled: Boolean(productId),
});
```

Examples:

- `App.tsx` uses `['session']` for `api.getSessionState` with `retry: false`.
- `ProductListPage.tsx` uses `['products']` for `api.listProducts`.
- `ProductDetailPage.tsx` uses `['product', productId]`, `['product-history', productId]`,
  `['product-workflow', productId]`, `['product-workflow-status', productId]`, and `['job', jobId]`.
- `ImageChatPage.tsx` uses `['image-sessions', productId ?? 'standalone']`, `['image-session', selectedSessionId]`,
  `['image-session-status', selectedSessionId]`, `['config']`, and product queries.
- `SettingsPage.tsx` uses `['config']` for runtime settings.

Use `enabled` when an ID is required. Do not call an API with an empty ID just because a route param has not loaded.

---

## Mutations and Cache Updates

Use `useMutation` for writes and update/invalidate TanStack Query caches in `onSuccess`:

- Logout mutations invalidate `['session']` and navigate to `/login`.
- Product/detail mutations invalidate `['product', productId]`, `['product-history', productId]`, and/or `['products']`.
- Image session mutations often call `queryClient.setQueryData(['image-session', id], updated)` and invalidate the session
  list.
- Settings save/reset mutations update `['config']` with `queryClient.setQueryData(...)`.

Keep cache keys consistent with the page that reads them. If a mutation changes list and detail data, invalidate both.

---

## Polling Pattern

Long-running product copy/poster jobs are polled from `ProductDetailPage.tsx` with `refetchInterval` that stops when the
job is no longer `queued` or `running`:

```tsx
refetchInterval: (query) => {
  const data = query.state.data as JobRun | undefined;
  if (!data || jobIsRunning(data.status)) {
    return 1000;
  }
  return false;
}
```

Use the shared `jobIsRunning(...)` helper from `web/src/lib/format.ts` rather than duplicating status checks.

## Scenario: ImageChat active-task lightweight status polling

### 1. Scope / Trigger

- Trigger: changing `ImageChatPage` generation polling, image-session API DTOs, or continuous image task visibility.
- Goal: active task status updates should be lightweight while full generated history remains loaded through the detail
  query.

### 2. Signatures

- Full detail query key: `['image-session', selectedSessionId]` -> `api.getImageSession(sessionId)`.
- Lightweight status query key: `['image-session-status', selectedSessionId]` ->
  `api.getImageSessionStatus(sessionId)`.
- Backend status fields used by the page: `rounds_count`, `latest_round_id`, `has_active_generation_task`,
  `generation_tasks`, `updated_at`, and `title`.

### 3. Contracts

- Do not put `refetchInterval` on the full `['image-session', selectedSessionId]` query for active generation.
- Enable the status query only when the cached full detail has an active queued/running generation task.
- Each status response should merge `title`, `updated_at`, and `generation_tasks` into the cached full detail so the task
  card, queue position, failure reason, provider notes, and duplicate-submit disabled state stay current.
- When status shows a new round count/latest round or a task changes from active to terminal, invalidate/refetch the full
  detail query once so generated candidates/history appear.
- Keep write mutations authoritative: create/update/upload/delete/generate handlers may still set full detail cache from
  mutation responses and invalidate the session list.

### 4. Validation & Error Matrix

- No selected session -> status query disabled.
- Full detail has no active generation task -> status query disabled; do not poll.
- Status still active -> merge task status only, no full detail refetch.
- Status terminal or new latest round -> invalidate `['image-session', selectedSessionId]` and the session list key.
- Status API error -> normal React Query error state; do not clear existing full detail cache only because a status poll
  failed.

### 5. Good/Base/Bad Cases

- Good: active task updates the visible queue position every 1500ms without refetching every historical round and asset.
- Base: a task failure appears in the task card, then full detail is refetched once.
- Bad: status polling replaces the detail cache with a partial object missing `assets` or `rounds`.
- Bad: broadening this ImageChat status query to ProductDetail workflow polling without a separate workflow DTO.

### 6. Tests Required

- Pure helper tests for active-task detection.
- Pure helper tests for merging status into cached detail without replacing `assets` or `rounds`.
- Pure helper tests for deciding when status requires a full detail refresh.
- Run `pnpm --dir web lint`, `pnpm --dir web test:run`, and `just web-build`.

### 7. Wrong vs Correct

Wrong:

```tsx
useQuery({ queryKey: ["image-session", id], refetchInterval: 1500 });
```

Correct:

```tsx
useQuery({ queryKey: ["image-session-status", id], refetchInterval: 1500 });
```

## Scenario: ProductDetail active-workflow lightweight status polling

### 1. Scope / Trigger

- Trigger: changing `ProductDetailPage` workflow polling, product-workflow API DTOs, or active workflow status visibility.
- Goal: active workflow polling should be lightweight while full DAG structure and artifacts remain loaded through the
  workflow detail query.

### 2. Signatures

- Full workflow query key: `['product-workflow', productId]` -> `api.getProductWorkflow(productId)`.
- Lightweight status query key: `['product-workflow-status', productId]` ->
  `api.getProductWorkflowStatus(productId)`.
- Backend status fields used by the page: `has_active_workflow`, node `status` / `failure_reason` / `last_run_at`,
  run `status` / `failure_reason` / `finished_at`, node-run status fields, and workflow `updated_at`.

### 3. Contracts

- Do not put active-run `refetchInterval` on the full `['product-workflow', productId]` query.
- Enable the status query only when the cached full workflow has a running run or queued/running node.
- Each status response may merge only workflow/node/run status metadata into the cached full workflow. It must not replace
  `edges`, node `config_json`, node `output_json`, or node-run artifact fields such as `output_json`, `copy_set_id`,
  `poster_variant_id`, and `image_session_asset_id`.
- When status shows an active workflow becoming terminal, invalidate/refetch the full workflow once and let the existing
  active-to-inactive path refresh `['product', productId]`, `['product-history', productId]`, and `['products']`.
- Keep write mutations authoritative: node/edge/update/run handlers may still set the full workflow cache from mutation
  responses and refresh product artifact queries.

### 4. Validation & Error Matrix

- No product id -> status query disabled.
- Full workflow has no active run/node -> status query disabled; do not poll.
- Status still active -> merge status metadata only, no full workflow refetch.
- Status terminal -> merge terminal status, invalidate `['product-workflow', productId]`, and refresh artifact-bearing
  product queries through the workflow active-to-inactive transition.
- Status API error -> normal React Query error state; do not clear existing full workflow cache only because a status poll
  failed.

### 5. Good/Base/Bad Cases

- Good: active workflow updates node/run status every 1200ms without refetching all edges, node config/output JSON, and
  artifact-bearing run payloads.
- Base: a failed node shows the failure reason promptly, then full workflow and product artifacts refetch once.
- Bad: status polling replaces the detail cache with a partial object missing `edges` or node `config_json`.
- Bad: workflow terminal status refreshes only `['product-workflow', productId]` and leaves product detail/history/list
  artifact surfaces stale.

### 6. Tests Required

- Pure helper tests for status active detection.
- Pure helper tests for merging status into cached workflow without replacing structure or artifact fields.
- Pure helper tests for deciding when status requires a full workflow refresh.
- Run `pnpm --dir web lint`, `pnpm --dir web test:run`, and `just web-build`.

### 7. Wrong vs Correct

Wrong:

```tsx
useQuery({ queryKey: ["product-workflow", productId], refetchInterval: 1200 });
```

Correct:

```tsx
useQuery({ queryKey: ["product-workflow-status", productId], refetchInterval: 1200 });
```

---

## Local State and Derived State

Use `useState` for local form/UI state:

- `ProductCreatePage.tsx`: product form fields, selected file(s), error text.
- `ProductDetailPage.tsx`: copy editing state, active job IDs, error text.
- `ImageChatPage.tsx`: selected session/asset IDs, draft prompt, size, rename mode, target product, messages.
- `SettingsPage.tsx`: draft config values, touched secret keys, resetting key, saved/error messages.

Use `useMemo` for derived values that depend on fetched data or local state:

- `ProductDetailPage.tsx` derives `workingCopy`.
- `ImageChatPage.tsx` derives allowed size options, selected round, source image, and reference images.
- `SettingsPage.tsx` groups config items by category.

Use `useEffect` for synchronization side effects, not for deriving values that can be calculated during render. Current
examples include auth redirects in `LoginPage.tsx`, job completion invalidation in `ProductDetailPage.tsx`, and draft reset
from fetched config in `SettingsPage.tsx`.

---

## Custom Hooks

Custom hooks should stay rare and intentional. For cross-page/shared behavior, extract a hook only when at least two
pages/components need the same behavior. Follow React naming rules (`useSomething`) and keep API calls typed through
`web/src/lib/api.ts`.

### Page-local controller hooks

An oversized route page may extract a page-local controller hook under that page's local directory even before there is
cross-page reuse, when the hook isolates a cohesive browser interaction boundary and materially reduces route complexity.
For ProductDetail-style workbench interactions, keep the hook page-local (for example
`web/src/pages/product-detail/useWorkflowCanvas.ts`) and pass API/cache work in as callbacks instead of hiding TanStack
Query mutations inside the controller.

Correct:

```tsx
const workflowCanvas = useWorkflowCanvas({
  workflow,
  onNodePositionCommit: (input) => updateNodePositionMutation.mutate(input),
  onConnectionCreate: (input) => createEdgeMutation.mutate(input),
});
```

Wrong:

```tsx
function useWorkflowCanvas(productId: string) {
  return useMutation({ mutationFn: () => api.createWorkflowEdge(productId, input) });
}
```

Likely future extraction candidates, if duplication grows:

- session/logout behavior shared by `ProductListPage.tsx`, `ImageChatPage.tsx`, and `SettingsPage.tsx`.
- job polling behavior from `ProductDetailPage.tsx`.
- config draft handling from `SettingsPage.tsx`.

Do not create a `hooks/` directory for one-off logic that is still page-specific.

---

## Avoid

- Calling hooks conditionally or after early returns. Keep hooks at the top of component functions.
- Using `useEffect` to mirror fetched data into local state unless the user can edit that local draft (`SettingsPage.tsx`
  is an example where mirroring is intentional).
- Forgetting `enabled` for queries that require route params or selected IDs.
- Invalidating only detail cache when a mutation also affects list summaries.
- Adding custom hooks that hide query keys or API behavior before there is real reuse.
