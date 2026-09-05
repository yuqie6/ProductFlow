# Frontend Engineering Guidelines

## Task Context

Inspect the changed component or helper, its relevant callers, tests and diff. For navigation changes, trace the route; for API changes, inspect the API method, shared DTO and backend contract. Consult the relevant section of `docs/ARCHITECTURE.md` when ownership is unclear. A copy or layout fix does not require tracing unrelated API and persistence paths. Current code and browser evidence establish actual behavior; compare them with the intended contract before deciding what to fix.

For visible copy and controls, load `.cursor/rules/ui-language.mdc`. New or substantially restyled surfaces use `.cursor/skills/productflow-frontend/SKILL.md`, which routes to the relevant design guidance. Ordinary event wiring, query fixes and copy corrections do not require a design plan or a full UI audit. Workbench behavior uses `.cursor/rules/workbench.mdc`; read affected `docs/USER_GUIDE.md` sections when the user workflow changes. Reuse unchanged rules already loaded in this session.

## Ownership

- Page and feature components live in `web/src/pages/`; reusable visual controls live in `web/src/components/`.
- All HTTP calls use `web/src/lib/api.ts`; shared wire types use `web/src/lib/types.ts`.
- TanStack Query owns server state. React state owns local form, selection, and canvas interaction state.
- Do not restore a parallel product workbench, V1 editor, browser-generated default workflow, or fallback request to retired routes. Do not add compatibility redirects or dual DTOs for retired pages.

Current feature owners:

- Agent creation: `AgentProductCreatePage.tsx` and `pages/product-create/`.
- Product workbench: `pages/workbench/`. Keep `agent -> canvas, chrome` and `canvas -> chrome`; `chrome` must not import the other two.
- Agent conversation/replay/Draft transition and workbench orchestration: `pages/workbench/agent/`.
- Online canvas, inspector, and runs: `pages/workbench/canvas/Graph*.tsx` plus `graphCatalog.ts` / `graphLayout.ts`.
- Shared workbench controls and canonical image explorer: `pages/workbench/chrome/`.
- Global media library: `MediaLibraryPage.tsx` and `workbench/canvas/WorkflowMediaLibraryPanel.tsx`.
- Global Agent Dock: `components/GlobalAgentDock.tsx`.

## API And Types

- API methods have typed inputs and outputs. Build query strings with `URLSearchParams` and encode path ids.
- TypeScript annotations do not validate external or localStorage JSON; use existing parsers for runtime boundaries.
- Keep discriminated unions synchronized with backend enums. Do not use `any`, unchecked double casts, or optional fields to hide backend drift.
- Keep candidate/image quantity outside advanced provider tool options.
- Never add secret-bearing response types or persist unlock tokens in localStorage/query keys.
- Keep opaque ids and cursors opaque. Optional fields mean the wire may omit them; use explicit `null` for JSON null.
- Runtime-parse persisted JSON, localStorage, route/search params, generation specs, recipe sources, and reveal events. Invalid browser-local state may be discarded; required server data must surface a recoverable error.
- Remove retired API methods, DTOs, label maps, tests, and fallback callers together.

## Interaction State

- Render explicit loading, empty, error, pending, success, failed, cancelled, and unknown states where the contract exposes them.
- Event streams reconnect from the runtime cursor while a Turn is live, and fold the Turn snapshot after it settles, without duplicating text or messages.
- Draft confirmation renders structured data, not parsed assistant prose.
- Creation-to-workbench transition retains the same Agent conversation context.
- Visible actions use semantic controls, visible focus, labels/tooltips, keyboard access, touch targets, and reduced-motion handling.
- Query keys include every identity/filter read by the query. Mutations invalidate the narrow authoritative projections and visible dependent summaries, not the whole QueryClient.
- Do not keep whole server records in component state. Local state may hold drafts, selections, panel state, viewport, and optimistic token deltas; it must reconcile to persisted server state.
- Autosave normalizes before comparison, serializes writes, exposes save/conflict state, and refetches the affected projection after conflicts.

## Canvas And Image Workflows

- Preserve drag, pan, zoom, selection, edge editing, folder behavior, inspector access, run history, and image library access when changing the workbench.
- Fixed-format nodes, toolbars, counters, and controls have stable responsive dimensions.
- Verify actual `innerWidth`, `clientWidth`, and element bounds before accepting screenshots.
- Upload count/format and per-type quantity are validated before submit; a selected type defaults to two and total planned images remains bounded by the backend contract.
- Product Image Explorer uses bounded pages and preview/thumbnail URLs rather than loading full originals or full libraries.
- Product Image Explorer query identity includes product, directory, search, and sort; identity changes clear selection and stale infinite pages. Reference selection commits one explicit ProductImageAsset id.
- Node cards stay compact and dimensionally stable; detailed node-specific editing belongs in the inspector. Folders remain one-level visual groups and preserve cross-folder edges.
- Manual add/edit/connect/run/history/recipe/library controls remain available even when the Agent has a related tool.

## Components And Performance

- Reuse existing workbench shell, canvas, node card, inspector, dialogs, toolbar, drawer, and image controls before adding variants.
- Page components orchestrate; leaf components receive typed data and callbacks and do not fetch unrelated aggregates.
- Routes stay lazy. Do not pull workbench-heavy modules into login/list entry chunks.
- Critical actions work without hover. Long names cannot overlap adjacent controls. Status is not color-only.
- Component-contained responsive behavior uses its measured content width when page viewport width is not the real boundary.

## Verification

Select checks using the root `AGENTS.md` risk matrix:

- Local logic or interaction: run the affected Vitest tests and lint the changed TS/TSX files; verify the actual interaction in a browser when the defect depends on DOM events or rendering.
- Copy or styling: check changed translations and affected rendering; choose the locales, themes and viewport boundaries that can expose the change. A prose-only edit to these instructions needs documentation checks, not a frontend build.
- Shared API/types, query ownership, routing, shared controls, global styling, dependencies or build configuration: run the full frontend gate below and relevant consumer tests. Use the same full gate for a frontend release.

Full frontend gate:

```bash
pnpm --dir web test:run
pnpm --dir web lint
just web-build
```

`just web-build` includes type-checking, the production build and bundle budgets. Direct `pnpm --dir web build` omits the budget check; do not report it as the same gate. For local TS/TSX changes where a targeted test does not establish type correctness, run the relevant TypeScript project check or `just web-build`.

Skip-Agent, real-provider full-graph browser coverage is opt-in unless required by the task's acceptance contract. It needs a suitable running dev stack, real prompt/image bindings and authorized provider usage:

```bash
just web-e2e-live-graph
```

Inspector rewrite / candidate apply against mock prompt/image providers is a separate opt-in gate. It temporarily changes provider bindings; use an isolated stack or coordinate exclusive use and restoration:

```bash
just web-e2e-canvas-document
```

For layout and canvas changes, verify the affected flow at desktop, narrow desktop and mobile boundaries, checking overlap, clipping and console/network errors. Theme changes need light/dark checks; translated or resized text needs relevant locales and long content; changed animation needs reduced motion; changed controls need relevant keyboard and touch behavior. Combine the full matrix for a shared shell redesign or release, rather than repeating every dimension for each local change.

Match tests to the changed owner: API encoding/body, parser/reducer, hook/query invalidation, component interaction, canvas adapters, inspector autosave, image selection, or route transition. For canvas and layout evidence, verify actual `innerWidth`, `clientWidth` and element bounds; check nonblank pixels for affected raster or 3D canvases. Report required checks that could not run and optional checks outside the local claim. Reuse results while their relevant inputs remain unchanged.
