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
- `ProductDetailPage.tsx` uses `['product', productId]`, `['product-history', productId]`, and `['job', jobId]`.
- `ImageChatPage.tsx` uses `['image-sessions', productId ?? 'standalone']`, `['image-session', selectedSessionId]`,
  `['config']`, and product queries.
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
