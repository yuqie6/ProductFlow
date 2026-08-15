# Frontend Engineering Guidelines

## Read First

Use `docs/ARCHITECTURE.md` for the current code map and inspect the route, API method, shared DTO, feature owner, and closest tests before editing. Backend and current browser behavior define the wire contract; old screenshots and retired pages do not.

## Ownership

- Page and feature components live in `web/src/pages/`; reusable visual controls live in `web/src/components/`.
- All HTTP calls use `web/src/lib/api.ts`; shared wire types use `web/src/lib/types.ts`.
- TanStack Query owns server state. React state owns local form, selection, and canvas interaction state.
- Do not restore a parallel product workbench, V1 editor, browser-generated default workflow, or fallback request to retired routes.

Current feature owners:

- Agent creation: `AgentProductCreatePage.tsx` and `pages/product-create/`.
- Agent conversation/replay/Draft transition: `pages/agent-workbench/`.
- V2 canvas, inspector, runs, recipes, and renditions: `pages/product-workflow-v2/`.
- Shared workbench controls and canonical image explorer: `pages/product-detail/`.
- Read-only legacy history: `LegacyHistoryPage.tsx` and `pages/legacy-history/`.

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
- Event streams reconnect from persisted cursors and converge to server state without duplicating text or messages.
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

Run focused tests while editing, then:

```bash
pnpm --dir web test:run
pnpm --dir web lint
pnpm --dir web build
```

For visible workflow changes, also verify desktop, narrow desktop, and mobile in a real browser, including light/dark mode, supported locales, console/network errors, overlap, clipping, and reduced motion.

Match tests to the changed owner: API encoding/body, parser/reducer, hook/query invalidation, component interaction, canvas adapters, inspector autosave, image selection, or route transition. For canvas and layout changes, verify actual `innerWidth`, `clientWidth`, element bounds, and nonblank canvas pixels.
