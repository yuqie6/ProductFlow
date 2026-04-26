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
pnpm --dir web lint
pnpm --dir web test:run
```

`just web-build` runs `pnpm --dir web build`, which type-checks app and Vite/Vitest config before building. Frontend
changes should also run the executable quality gate added under `web/package.json`:

- `pnpm --dir web lint` runs ESLint flat config from `web/eslint.config.js` over the Vite/React/TypeScript workspace.
  The baseline intentionally keeps formatting churn low: React hooks rules are enabled, while exhaustive dependency
  cleanup is not part of the first gate.
- `pnpm --dir web test:run` runs deterministic Vitest unit tests from `web/vitest.config.ts`.
- `pnpm --dir web test` is reserved for local Vitest watch mode.

Prefer pure helper tests for page-local logic before large UI refactors. For ProductDetail workbench changes, add or
extend tests under `web/src/pages/product-detail/*.test.ts` when touching gallery, download, workflow status, or other
importable helper behavior. Do not split `ProductDetailPage.tsx` solely to satisfy tests; extract only small pure helpers
when that keeps runtime behavior unchanged.

## Scenario: Frontend executable quality gate

### 1. Scope / Trigger

- Trigger: any frontend code change under `web/src/`, frontend config change under `web/`, or ProductDetail helper
  extraction intended to support refactoring.
- Goal: keep the gate small and deterministic before larger ProductDetail UI splitting.

### 2. Signatures

- `pnpm --dir web lint`
- `pnpm --dir web test:run`
- `pnpm --dir web test` for local watch mode only.
- `just web-build` remains the build/type-check gate and delegates to `pnpm --dir web build`.

### 3. Contracts

- ESLint config lives at `web/eslint.config.js`.
- Vitest config lives at `web/vitest.config.ts`.
- Unit tests use `*.test.ts` under `web/src/`; keep them close to the pure helper they cover.
- `web/tsconfig.node.json` includes frontend tool config files that should be type-checked by `web build`.

### 4. Validation & Error Matrix

- Lint error -> fix code or narrow the rule in `web/eslint.config.js`; do not add inline suppressions unless the
  exception is intentional and documented near the code.
- Test failure -> fix the helper or update the assertion when the intended behavior changed.
- Type/build failure -> fix TypeScript/runtime import boundaries before reporting frontend work complete.
- Large pre-existing React hook dependency cleanup -> do not mix into unrelated work; keep the initial gate low-noise and
  schedule stricter rules separately.

### 5. Good/Base/Bad Cases

- Good: add or update a ProductDetail gallery/download/status helper and cover it with a colocated `*.test.ts`.
- Base: run `pnpm --dir web lint`, `pnpm --dir web test:run`, and `just web-build` before handing off frontend changes.
- Bad: split `ProductDetailPage.tsx` UI only to make tests importable.
- Bad: enable broad formatting or hook-dependency rules that require whole-frontend rewrites in an unrelated task.

### 6. Tests Required

- New pure helper -> add Vitest unit coverage for normal and edge cases.
- ProductDetail helper changes -> prefer colocated tests under `web/src/pages/product-detail/`.
- DTO/API behavior changes still require `just web-build`; frontend unit tests do not replace backend contract tests.

### 7. Wrong vs Correct

#### Wrong

```bash
pnpm --dir web test
```

Using watch mode as the handoff gate can hang automation.

#### Correct

```bash
pnpm --dir web test:run
```

Use the deterministic run mode for CI-style verification and keep `test` for local watch mode.

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
`just web-build` before finishing frontend work. When frontend code changes, also run `pnpm --dir web lint` and
`pnpm --dir web test:run`.

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
- Does `pnpm --dir web lint` pass?
- Does `pnpm --dir web test:run` pass?
- Are API methods and DTO types centralized in `web/src/lib/`?
- Are query keys and invalidations complete for every mutation?
- Are backend enum/DTO changes mirrored in `web/src/lib/types.ts`?
- Are loading/error/disabled states present for async actions?
- Does the UI match the existing Tailwind/zinc visual language?
