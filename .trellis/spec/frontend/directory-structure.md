# Frontend Directory Structure

> Actual React/Vite organization for ProductFlow.

---

## Overview

The frontend is a React 19 + Vite + TypeScript app under `web/src/`. It uses React Router for pages, TanStack Query for
server state, Tailwind CSS v4 utility classes for styling, and a small central API/type layer under `web/src/lib/`.

Key files:

- `web/src/main.tsx` mounts the app with `React.StrictMode`.
- `web/src/App.tsx` creates the `QueryClient`, wraps `BrowserRouter`, and declares routes.
- `web/src/index.css` imports Tailwind and defines minimal global theme/base styles.
- `web/src/pages/` contains route-level pages.
- `web/src/components/` contains shared presentational components.
- `web/src/lib/` contains API calls, shared TypeScript DTOs, and formatting helpers.

---

## Directory Layout

```text
web/
├── package.json                     # scripts: dev, build, lint, test, test:run, preview
├── tsconfig.json
├── tsconfig.app.json                # strict TypeScript for src/
├── tsconfig.node.json               # Vite config typing
├── vite.config.ts                   # React/Tailwind plugins, API proxy, ports/hosts
└── src/
    ├── main.tsx                     # ReactDOM entrypoint
    ├── App.tsx                      # QueryClientProvider, BrowserRouter, auth-gated routes
    ├── index.css                    # Tailwind import and global base CSS
    ├── components/
    │   ├── StatusPill.tsx           # shared status badge
    │   └── TopNav.tsx               # shared top navigation
    ├── lib/
    │   ├── api.ts                   # fetch wrapper, ApiError, typed API methods
    │   ├── format.ts                # date/price/job formatting helpers
    │   └── types.ts                 # frontend DTOs mirroring backend responses
    └── pages/
        ├── LoginPage.tsx
        ├── ProductListPage.tsx
        ├── ProductCreatePage.tsx
        ├── ProductDetailPage.tsx
        ├── product-detail/              # page-local product workflow constants/types/utils/components
        ├── ImageChatPage.tsx
        └── SettingsPage.tsx
```

There is no `hooks/` directory and no global state store today. Stateful logic currently lives in pages unless it is a
shared API/type/format helper.

---

## Route Organization

Routes are centralized in `web/src/App.tsx` inside `AppRoutes()`:

- `/login` -> `LoginPage`
- `/products` -> `ProductListPage`
- `/products/new` -> `ProductCreatePage`
- `/products/:productId` -> `ProductDetailPage`
- `/image-chat` -> standalone `ImageChatPage`
- `/products/:productId/image-chat` -> product-scoped `ImageChatPage`
- `/settings` -> `SettingsPage`

Auth gating is also in `AppRoutes()`: it loads `api.getSessionState` with query key `['session']` and redirects
unauthenticated users to `/login`.

---

## Page vs Component Placement

Use `web/src/pages/` for route-level modules that own data fetching, navigation, mutations, and complex local UI state.
Current examples:

- `ProductListPage.tsx` owns product list fetching, logout mutation, and navigation to settings/image chat/new product.
- `ProductDetailPage.tsx` owns product detail/history queries, workflow status polling, copy editing state, and
  workbench actions.
- `ImageChatPage.tsx` owns session selection, auto-create behavior, config-derived image size options, and generation.
- `SettingsPage.tsx` owns config fetching, grouped drafts, secret touched state, save/reset mutations.

Use `web/src/components/` for reusable presentational components with small props and no route ownership:

- `TopNav.tsx`
- `StatusPill.tsx`

If a component is only used inside one page and tightly coupled to that page's state, keep it either in the page file or
in a page-local directory. `ProductDetailPage.tsx` uses `web/src/pages/product-detail/` for workflow canvas constants,
draft/config utilities, image mapping helpers, and page-local components; do not move those to global `components/`
until another page actually reuses them.

---

## Lib Organization

- `web/src/lib/api.ts` is the only place that should know fetch details, credentials, `VITE_API_BASE_URL`, and API paths.
- `web/src/lib/types.ts` contains DTO interfaces and string union types mirroring backend Pydantic responses and enums.
- `web/src/lib/format.ts` contains pure formatting helpers such as `formatDateTime`, `formatShortDate`, and
  `formatPrice`.
- `web/src/lib/image-downloads.ts` contains reusable image URL, filename sanitization, timestamp suffix, and extension
  helpers. Page-specific mapping from product/poster records to downloadable images should stay page-local.

Do not scatter raw `fetch(...)` calls or duplicate DTO interfaces inside pages.

---

## Naming Conventions

- Page and component files use `PascalCase.tsx`: `ProductListPage.tsx`, `TopNav.tsx`.
- Exported React components use named exports: `export function ProductListPage() { ... }`.
- Utility files use lower camel-ish names: `api.ts`, `format.ts`, `types.ts`.
- Helper functions use `camelCase`, for example `getWorkingCopy`, `getSourceImageUrl`, `draftsFromConfig`.
- API DTO fields intentionally preserve backend `snake_case` names, for example `workflow_state`, `copy_set_id`,
  and `reset_keys` in `web/src/lib/types.ts`; image size presets live in `web/src/lib/imageSizes.ts`, not runtime config.

---

## Avoid

- Adding route declarations outside `App.tsx` without a deliberate router refactor.
- Creating a global state store for server data that already lives in TanStack Query.
- Duplicating API URL construction outside `api.toApiUrl(...)`.
- Moving page-specific subcomponents into `components/` before they are reused.
- Renaming backend DTO fields to camelCase in frontend types unless the backend response changes too.
