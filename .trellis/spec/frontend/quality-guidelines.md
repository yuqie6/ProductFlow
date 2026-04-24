# Frontend Quality Guidelines

> Frontend quality standards reflected by current ProductFlow code and tooling.

---

## Tooling

Frontend tooling is defined in `web/package.json`, `web/tsconfig*.json`, `web/vite.config.ts`, and the root `justfile`:

- React 19, React DOM 19.
- Vite 7 with `@vitejs/plugin-react`.
- Tailwind CSS v4 through `@tailwindcss/vite`.
- TanStack Query 5 for server state.
- React Router DOM 7 for routing.
- TypeScript strict mode.

Common commands:

```bash
just web-install
just web-dev
just web-build
```

`just web-build` runs `pnpm --dir web build`, which type-checks app and Vite config before building. There is no frontend
ESLint or test runner configured today, so TypeScript build is the required frontend quality gate.

---

## Required Patterns

### Centralize API access

Use `web/src/lib/api.ts` for all backend calls. It handles:

- `VITE_API_BASE_URL` trimming.
- `credentials: "include"` for session-cookie auth.
- JSON vs `FormData` headers.
- API error parsing into `ApiError`.
- Typed request/response methods.

Do not add raw `fetch(...)` calls in pages/components.

### Keep server state in TanStack Query

Use `useQuery`, `useMutation`, and `useQueryClient` as shown in current pages. Mutations should update or invalidate the
query keys affected by the change. Do not introduce global stores for server records.

### Keep routes auth-gated

Add new private routes in `web/src/App.tsx` with the same authenticated/redirect pattern used by existing routes. Login is
the only public page.

### Preserve build-time type safety

Any API contract change should update `web/src/lib/types.ts`, page usage, and backend schemas/tests together. Run
`just web-build` before finishing frontend work.

### Keep UI feedback explicit

Current pages show loading, error, disabled, and success states close to the action:

- Loading spinner for initial app/session load in `App.tsx`.
- Product list load/error states in `ProductListPage.tsx`.
- Mutation errors in `ProductCreatePage.tsx`, `ProductDetailPage.tsx`, `ImageChatPage.tsx`, and `SettingsPage.tsx`.
- Disabled buttons while mutations are pending.

Follow this style for new actions.

---

## Accessibility and UX Checklist

Review new UI for:

- Non-submit buttons have `type="button"`.
- Inputs have labels or are wrapped by labels.
- Loading states use both disabled controls and visible feedback when an action can take time.
- Error text is visible near the action that failed.
- Image URLs from the backend are converted with `api.toApiUrl(...)` before being used in `src` or links.
- Destructive actions such as delete are explicit buttons and update cache/selection state after success.

---

## Build and Environment

Development and preview ports are configured through `web/vite.config.ts`:

- Dev default port: `29283`.
- Preview default port: `29281`.
- Dev API proxy target default: `http://127.0.0.1:29282`.
- Allowed hosts default to `draw.devbin.de` unless `WEB_ALLOWED_HOSTS` is provided.

Use `just web-dev` so `.env.dev` and proxy behavior match backend dev commands.

---

## Forbidden Patterns

- Raw `fetch(...)` outside `web/src/lib/api.ts`.
- Untyped API responses or `any` payloads.
- New pages not registered in `App.tsx` or not protected by session auth when private.
- New server state held only in local component state when it should be cached/invalidation-aware.
- Committing `web/dist/`, `web/node_modules/`, `*.tsbuildinfo`, or local env files.
- Adding lint/test commands to docs without actually configuring them in `web/package.json`.

---

## Review Checklist

Before accepting frontend changes, check:

- Does `just web-build` pass?
- Are API methods and DTO types centralized in `web/src/lib/`?
- Are query keys and invalidations complete for every mutation?
- Are backend enum/DTO changes mirrored in `web/src/lib/types.ts`?
- Are loading/error/disabled states present for async actions?
- Does the UI match the existing Tailwind/zinc visual language?
