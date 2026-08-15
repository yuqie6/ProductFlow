# Frontend Quality Guidelines

## Required Gates

```bash
pnpm --dir web test:run
pnpm --dir web lint
pnpm --dir web build
```

`build` includes both TypeScript projects and the Vite production bundle.

Run focused tests while editing, then the complete gate before finishing.

## API Centralization

- All HTTP calls use `web/src/lib/api.ts`.
- DTOs use `web/src/lib/types.ts`.
- Paths, credentials, JSON/multipart parsing, and ApiError stay centralized.
- Tests assert method, encoded path/query, headers, and body for changed methods.
- Deleted routes/types/methods are removed together.

## Server State

- TanStack Query owns remote records.
- Query keys include every identity/filter that affects queryFn.
- Mutations update/invalidate precise projections.
- Poll only lightweight status.
- Event streams reconnect and converge to persisted state.

## Type Safety

- No `any` or unchecked double casts.
- Parse untrusted/localStorage JSON.
- Keep backend unions synchronized.
- Optionality must reflect wire behavior.
- Remove unused DTOs after feature retirement.

## Visual and Interaction Quality

For every affected viewport:

- no overlap;
- no clipped controls/text;
- stable fixed controls and canvas nodes;
- clear selected, pending, success, failure, empty, and disabled states;
- light and dark mode;
- all supported locales;
- keyboard and touch access where applicable.

Operational workbenches remain dense and predictable. Do not replace established toolbars, sidebar controls, or inspector depth with a simplified shell.

## Canvas Review

Canvas changes require:

- nonblank initial render;
- node/edge visibility;
- one clear input/output handle;
- drag, pan, zoom, selection, edge creation/deletion;
- folder behavior;
- inspector/command access;
- desktop and mobile screenshot review;
- canvas pixel check when automation is used.

Verify actual browser `innerWidth` / `clientWidth` and element bounds.

## Agent UI Review

- token deltas do not reflow the whole page excessively;
- reconnect does not duplicate text/messages;
- questions remain actionable;
- cancel/resume has explicit pending state;
- Draft confirmation is readable and complete;
- materialization reveal preserves stable canvas coordinates;
- creation-to-workbench transition retains conversation context;
- reduced-motion users receive an immediate stable transition.

## Image UI Review

- uploads validate count/format before submit;
- per-type quantity defaults to two and remains editable;
- candidate count stays separate from advanced fields;
- missing media disables preview/download;
- save-to-product updates the target library;
- Product Image Explorer uses bounded pages and component-width responsiveness.

## Settings Review

- provider purposes are prompt, Agent, and image;
- secret never appears in response/UI after save;
- capability/model incompatibility is visible;
- unlock token is not persisted;
- import preview and apply use the current schema;
- locale/theme coverage includes compact and desktop layouts.

## Accessibility

- semantic controls;
- visible focus;
- icon button labels/tooltips;
- status not color-only;
- dialog/drawer focus behavior;
- touch targets;
- reduced motion;
- form labels and error association.

## Bundle and Performance

- Route-level pages remain lazy.
- Avoid importing workbench-heavy modules into list/login entry chunks.
- Large libraries use cursor pages, not full arrays.
- Images use preview/thumbnail URLs where provided.
- Use React memoization only for measured or obvious expensive projections.
- Investigate new chunk warnings when a change materially increases a route bundle.

## Testing Strategy

Prefer the smallest useful test:

- pure parser/reducer/helper;
- component interaction;
- API contract;
- feature integration;
- real browser flow.

Risk expands verification:

- DTO/API changes -> API tests + build;
- shared hook/state changes -> related feature tests;
- canvas/interaction -> browser screenshots;
- provider/config flow -> live backend/browser where available.

## Environment

- Dev Web defaults to 29283 and proxies API to 29282.
- Docker Web defaults to 29281 and proxies API to backend 29280.
- `VITE_API_BASE_URL` stays empty for same-origin proxy operation.
- Do not hard-code a localhost backend URL in components.

## Review Checklist

- Read the live component and caller before editing.
- Reuse current components and helpers.
- Check route/query/API/type/i18n together.
- Inspect loading/empty/error/pending/terminal states.
- Run tests, lint, and build.
- Use browser evidence for visible interaction changes.
- Run `git diff --check` and residual scans after deletion work.

## Forbidden Patterns

- Raw fetch outside api.ts.
- Global store for server state.
- Browser-generated workflow defaults.
- Parallel product workbench/canvas.
- Hidden fallback to a removed route or localStorage shape.
- Secrets in localStorage or query keys.
- Hover-only critical action.
- Layout controlled only by screenshot width assumptions.
- Feature-complete controls removed because the Agent has a related tool.
