# Frontend Component Guidelines

> Component patterns currently used in ProductFlow.

---

## Overview

ProductFlow components are simple React function components with TypeScript props, Tailwind CSS classes, and named exports.
Route-level pages own data fetching and mutations; shared components stay mostly presentational.

Real examples:

- `web/src/components/TopNav.tsx`
- `web/src/components/StatusPill.tsx`
- Page-local components/helpers in `web/src/pages/SettingsPage.tsx` and `web/src/pages/ProductDetailPage.tsx`

---

## Component Structure

Use named function exports:

```tsx
interface TopNavProps {
  breadcrumbs?: string;
  onHome?: () => void;
  onLogout?: () => void;
}

export function TopNav({ breadcrumbs, onHome, onLogout }: TopNavProps) {
  return (...);
}
```

For very small props, inline typing is acceptable; `StatusPill` uses:

```tsx
export function StatusPill({ status }: { status: ProductWorkflowState }) {
  const config = CONFIG[status];
  return (...);
}
```

Use top-level constants for static display maps. `StatusPill.tsx` defines `CONFIG` as a `Record<ProductWorkflowState, ...>`
so every workflow status has a label and classes.

---

## Props Conventions

- Use `interface` for reusable component props (`TopNavProps`, `ConfigFieldProps`).
- Use explicit callback props for UI actions: `onHome`, `onLogout`, `onChange`, `onReset`.
- Keep props serializable/simple when possible; pages should pass already-derived values into shared components.
- Prefer optional props for optional UI affordances, and render `null` when absent. `TopNav` renders breadcrumbs and logout
  button only when props exist.

---

## Styling Patterns

Styling is Tailwind-first:

- Global CSS stays minimal in `web/src/index.css`.
- Components/pages use `className` utility strings directly.
- State-dependent styles are built with small maps/functions, e.g. `sourceClassName(...)` in `SettingsPage.tsx` and
  `CONFIG` in `StatusPill.tsx`.
- Icons come from `lucide-react` and are imported directly by each page/component.

Current visual language uses zinc/slate surfaces, thin borders, small rounded corners, and restrained hover/focus states.
Every new visible surface should include dark-mode variants when it uses explicit light backgrounds, borders, shadows, or
text colors. The app uses a root `dark` class from `PreferencesProvider`, so Tailwind `dark:*` utilities are the normal
path for component-level theme variants. Existing examples include `TopNav.tsx`, `ProductListPage.tsx`,
`ProductCreatePage.tsx`, and `SettingsPage.tsx`.

When adding image preview or canvas surfaces, keep images inspectable in both themes. Dark variants should change chrome
and empty/loading/error states, not tint or obscure product thumbnails.

---

## Internationalized UI Text

User-visible UI chrome should use the local i18n helpers instead of hard-coded one-off strings:

- Translation keys live in `web/src/lib/i18n.ts`; supported locales are `zh-CN`, `en-US`, `ja-JP`, and `vi-VN`.
- Components read translations through `useI18n()` / `usePreferences()` from `web/src/lib/preferences.tsx`.
- Pure helpers that format visible labels should accept an optional translate function or locale rather than importing
  React hooks. Examples include image-size labels, gallery size labels, and ProductDetail node display helpers.
- Keep product/operator/model-authored data as source text. Do not translate product names, custom node titles, user
  template titles/descriptions returned by the backend, prompts, generated copy, filenames, provider messages, or
  `ApiError.detail`.
- Backend-owned built-in canvas template catalog text is system UI chrome. Localize it in frontend helpers by stable
  built-in template key and node/output/reference identifiers, while leaving user templates and user-renamed node titles
  as source text.
- Built-in template metadata may identify a node's original system template, but it must not override a user-renamed
  title. Only translate a persisted built-in node title when the stored title still matches the source built-in label or
  an already-localized system label.
- Default system labels should be locale-aware. If a helper suppresses legacy default titles, it must recognize defaults
  from all supported locales so older records such as `参考图 2` do not leak into non-Chinese UI.

Good:

```tsx
const { t } = useI18n();
return <button type="button" aria-label={t("nav.logout")}>{t("nav.logout")}</button>;
```

Good:

```ts
export function workflowNodeDisplayTitle(node: WorkflowNode, t = defaultT): string {
  return isSystemDefaultTitle(node.title) ? t("detail.node.referenceImage") : node.title;
}
```

Bad:

```tsx
return <button type="button">退出登录</button>;
```

Bad:

```tsx
return locale === "en-US" ? translateProductName(product.name) : product.name;
```

---

## Accessibility and Forms

Follow the patterns already present:

- Buttons include `type="button"` unless they submit a form. See `TopNav.tsx`, `ProductListPage.tsx`, and `SettingsPage.tsx`.
- Form submit handlers call `event.preventDefault()` and trigger a mutation, e.g. `ProductCreatePage.tsx` and
  `LoginPage.tsx`.
- Inputs in settings use `label htmlFor={item.key}` and matching `id={item.key}` in `ConfigField`.
- Image upload drop zones use the shared `ImageDropZone` component. Pages own the upload mutation and pass an `onFiles`
  callback; the shared component only handles click, keyboard, drag/drop, `accept`, `multiple`, and disabled/focus states.
  Use the default single-file mode for product/workflow images and `multiple` for session reference images.
- Loading states use `Loader2` with `animate-spin`; disabled buttons use `disabled` and reduced opacity.
- Errors are rendered near the relevant action as text or red alert blocks.

When adding new forms, keep keyboard/focus behavior at least as strong as these examples.

---

## Data Fetching Boundary

Shared components should not call the API directly today. API calls live in pages through TanStack Query and the central
`api` object:

- `ProductListPage.tsx` calls `useQuery({ queryKey: ['products'], queryFn: api.listProducts })`.
- `SettingsPage.tsx` calls `api.getConfig` / `api.updateConfig` from page-level mutations.
- `TopNav.tsx` receives `onLogout` instead of knowing about sessions or `api.destroySession`.

If a component starts needing API calls, consider whether it is actually a route/page-level component.

## Feature Page Extraction Boundary

Large route pages should keep query/mutation ownership, URL parameters, selection reconciliation, and submit handlers in
the route component. Move repeated or bulky display surfaces into page-local feature components under the route's feature
folder, for example `web/src/pages/image-chat/`.

Good extraction targets:

- Main preview/canvas surfaces that receive already-derived rounds, task placeholders, and callback props.
- History strips, session lists, reference panels, and other repeated UI regions that can stay presentational.
- Pure display helpers for labels, status classes, and sizing text.

Keep extracted components API-free. Pass action callbacks such as `onSelectRound`, `onDeleteSession`, `onRetry`, and
`onCancel` from the page. If extraction starts requiring TanStack Query hooks or direct `api.*` mutations inside the
component, promote the design to a dedicated controller/hook refactor with focused regression tests around selection and
submission behavior.

When optimizing image-heavy pages, preserve the resource contract while extracting UI: visible preview surfaces should use
preview-sized assets, explicit download actions should use download URLs, and route-level lazy loading should stay in
`App.tsx` so unrelated pages do not inflate the initial route load.

---

## Scenario: Shared image size picker contract

### 1. Scope / Trigger

- Trigger: editing continuous image chat size controls, workflow `image_generation` inspector controls, runtime
  built-in preset display behavior, or frontend helpers that parse `WIDTHxHEIGHT`.
- Goal: keep the visual size picker, custom dimensions, and backend image-size contract aligned across every image
  generation surface.

### 2. Signatures

- Shared component: `ImageSizePicker({ value, onChange, presets, disabled?, maxDimension? })`.
- Shared helpers live under `web/src/lib/imageSizes.ts`.
- Runtime max dimension comes from `api.getRuntimeConfig()` and is passed into `buildImageSizeOptions(maxDimension)` and
  `ImageSizePicker({ maxDimension })`.
- Page/API boundary values remain normalized `WIDTHxHEIGHT` strings, for example `1024x1024` or `3840x2160`.
- Custom dimensions are calibrated to the nearest provider-safe 16-pixel multiple before being emitted, for example
  `1500x800` becomes `1504x800`.

### 3. Contracts

- Continuous image chat and workflow image-generation inspector must use the same shared picker instead of duplicating
  separate button/input implementations.
- Continuous image chat and workflow image-generation inspector must use the same shared `ImageToolControls` component for
  provider image-tool parameters. Keep compaction/normalization in shared helpers under `web/src/lib/`, not inside one
  page, so the workbench node and image chat submit the same payload shape.
- `ImageToolControls` visibility and `compactImageToolOptions(...)` submission filtering must both use
  `runtime-config.image_tool_allowed_fields`; do not show or submit provider fields that the active provider profile has
  not enabled.
- Pages pass built-in preset options into the component; `ImageSizePicker` must not call the API.
- Runtime config filters built-in size preset buttons by maximum single edge. It must not provide an arbitrary backend
  allowlist; a custom value may be valid even when it is not present in the preset list.
- The picker should preserve and round-trip unknown valid values by switching to custom width/height mode instead of
  resetting to the first preset.
- Preset labels should include the human tier/aspect and the exact pixel string so users know what will be submitted.

### 4. Validation & Error Matrix

- Invalid local text such as missing width/height -> keep the custom inputs visible and avoid emitting a malformed size.
- Existing value not found in presets -> show it as custom dimensions when parseable.
- Custom inputs with uppercase separators or oversized values -> normalize/calibrate in the shared helper before emitting.
- Custom inputs with either side not divisible by 16 -> normalize/calibrate in the shared helper before emitting.
- Backend rejection still remains authoritative; frontend validation only improves UX.

### 5. Good/Base/Bad Cases

- Good: `3840x2160` from workflow node config opens the inspector with custom dimensions `3840` and `2160`, then submits
  `3840x2160` unchanged.
- Base: `1024x1024`, `2048x2048`, and `3840x3840` appear as preset buttons when present in the derived presets.
- Bad: `ImageChatPage` accepts custom dimensions while `InspectorPanel` still exposes a raw text field.
- Bad: `ImageChatPage` supports provider quality/format/fidelity fields while `InspectorPanel` has a separate partial
  implementation or sends raw unnormalized `tool_options`.
- Bad: product workflow inspector and image-session generation rebuild separate size/tool/count panels instead of sharing
  `ImageGenerationSettingsPanel` where the behavior is the same.
- Bad: one image generation entry uses a combined settings page while another uses `生成设置 / 高级`; product workflow
  image nodes and image-session generation should both use `ImageGenerationSettingsTabs` to keep common
  size/count/prompt controls separate from advanced provider tool options.
- Bad: a custom value is auto-reset because it is not one of the built-in preset buttons.

### 6. Tests Required

- Shared helper tests should cover default presets, custom labels, calibration, and invalid strings.
- When picker state behavior changes, add or update component-level tests before relying on manual visual review.
- `just web-build`, `pnpm --dir web lint`, and `pnpm --dir web test:run` remain required for frontend changes.

### 7. Wrong vs Correct

#### Wrong

```tsx
<input value={draft.size} onChange={(event) => onDraftChange({ ...draft, size: event.target.value })} />
```

This creates a second workflow-only size UI and bypasses the shared custom/preset behavior.

#### Correct

```tsx
<ImageSizePicker
  value={draft.size}
  onChange={(size) => onDraftChange({ ...draft, size })}
  presets={imageSizePresets}
/>
```

Pages provide data and mutations; the shared picker owns only presentational size selection state.

---

## TopNav Global Navigation Contract

`web/src/components/TopNav.tsx` is the shared authenticated product navigation bar, not just a page title strip.

- Every primary authenticated page should render `TopNav` so the same frequent entries are always available:
  `商品/工作台`, `文/图生图`, `画廊`, `帮助`, and `配置`.
- The entries link to `/products`, `/image-chat`, `/gallery`, `/help`, and `/settings`; keep route declarations centralized in
  `web/src/App.tsx`.
- Page components may still pass `breadcrumbs`, `onHome`, and `onLogout`, but should not duplicate these global nav links
  in a separate header unless that page needs an additional hero call-to-action.
- `TopNav` may use React Router primitives such as `NavLink` / `useLocation`, but must not fetch session or settings data
  directly. Session logout remains a page-owned mutation passed in through `onLogout`.
- `TopNav` owns the compact global locale and theme controls. Do not add separate per-page language/theme toggles unless a
  page-specific workflow requires an additional local affordance.

Wrong:

```tsx
<TopNav breadcrumbs="配置" />
<button onClick={() => navigate("/settings")}>配置</button>
```

Correct:

```tsx
<TopNav breadcrumbs="配置" onHome={() => navigate("/products")} onLogout={() => logoutMutation.mutate()} />
```

The shared nav itself exposes the settings/image-chat/product/gallery links; pages only add page-specific actions.

## Scenario: Global gallery display page

### 1. Scope / Trigger

- Trigger: editing `GalleryPage`, gallery route registration, gallery API DTO consumption, or continuous image-chat save
  to gallery affordances.
- The gallery is a visual browsing surface for generated images, not a management dashboard.

### 2. Signatures

- Route: `/gallery` in `web/src/App.tsx`.
- API client:
  - `api.listGalleryEntries()`.
  - `api.saveGalleryEntry(imageSessionAssetId)`.
- Query key: `['gallery']`.
- DTO: `GalleryEntry` in `web/src/lib/types.ts`.

### 3. Contracts

- `GalleryPage` lists global gallery entries and uses `api.toApiUrl(...)` for `image.thumbnail_url`, `image.preview_url`,
  and `image.download_url`.
- Continuous image chat saves only the selected generated candidate to the gallery; existing save-to-product behavior must
  remain separate.
- Successful save invalidates `['gallery']` so the global page refreshes without a hard reload.
- The page should emphasize image-led browsing: a strong selected/hero image, a responsive visual grid, and compact prompt
  and metadata context. Do not turn it into product filters, bulk tools, or a table-first admin page.
- Gallery feed cards should preserve the full generated image instead of cropping it. Derive card aspect from
  `actual_size` first and `size` second, clamp extreme ratios, and use a stable id/index-based score for featured cards
  so the layout feels varied without changing on every render.
- If the feed uses CSS Grid masonry behavior with `auto-rows-*` and `gridRowEnd: span N`, the span calculation must include
  both the row unit and the grid gap. A span that ignores `gap-*` will produce oversized dark bars because CSS Grid adds
  every inter-row gap inside the spanned area.
- Desktop masonry row spans must be calculated from the measured grid width, not a fixed container width. Account for
  column gaps when deriving tile width: subtract `gap * (columns - 1)` before dividing into columns, then add the gaps
  inside the tile span back. Keep `auto-rows-*` and `gridRowEnd` scoped to the desktop grid; mobile and tablet layouts
  should use natural `aspect-ratio` sizing.

### 4. Validation & Error Matrix

- Empty gallery -> styled empty state, no broken image placeholders.
- API load failure -> visible page-local error state.
- Missing selected ID after refresh/list change -> fall back to the newest available entry.
- Save-to-gallery API error from image chat -> show page-local mutation error near existing image-chat feedback.

### 5. Good/Base/Bad Cases

- Good: the selected generated candidate appears in the gallery after saving and refreshes via `['gallery']`.
- Bad: raw `fetch('/api/gallery')` from a page.
- Bad: adding gallery grouping/filtering/bulk controls under this display-only contract.

### 6. Tests Required

- Pure helper tests for selected-entry fallback, size/actual-size labels, aspect-ratio parsing/clamping, stable featured
  tile placement, masonry row-span behavior, gap-aware tile width, and measured grid width changes.
- Frontend build must type-check `GalleryEntry` DTOs and API methods.
- When save behavior changes, run image-chat related helper tests and `pnpm --dir web test:run`.

### 7. Wrong vs Correct

#### Wrong

```tsx
fetch('/api/gallery')
```

#### Correct

```tsx
useQuery({ queryKey: ['gallery'], queryFn: api.listGalleryEntries })
```

#### Wrong

```tsx
const rowSpan = Math.ceil(tileHeight / 8)
```

This ignores the `gap-4` space that CSS Grid adds between every spanned row.

#### Correct

```tsx
const rowSpan = Math.ceil((tileHeight + gridGapPx) / (rowUnitPx + gridGapPx))
```

#### Wrong

```tsx
const tileWidth = (1280 * columnSpan) / 12
```

This ignores the 11 grid gaps in a 12-column desktop grid.

#### Correct

```tsx
const columnWidth = (gridWidth - gridGapPx * (columns - 1)) / columns
const tileWidth = columnWidth * columnSpan + gridGapPx * (columnSpan - 1)
```

---

## Common Mistakes to Avoid

- Putting server mutations inside shared presentational components.
- Creating untyped props or using `any` for component inputs.
- Omitting `type="button"` on non-submit buttons inside forms.
- Hardcoding API URLs in components; use `api.toApiUrl(...)` for backend-provided relative image URLs.
- Duplicating status label/style maps instead of reusing `StatusPill` or a local typed `Record`.
