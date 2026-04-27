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

Current visual language uses white/zinc surfaces, thin borders, small rounded corners, and restrained hover/focus states.
Existing examples include `ProductListPage.tsx`, `ProductCreatePage.tsx`, and `SettingsPage.tsx`.

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

### 3. Contracts

- Continuous image chat and workflow image-generation inspector must use the same shared picker instead of duplicating
  separate button/input implementations.
- Continuous image chat and workflow image-generation inspector must use the same shared `ImageToolControls` component for
  provider image-tool parameters. Keep compaction/normalization in shared helpers under `web/src/lib/`, not inside one
  page, so the workbench node and image chat submit the same payload shape.
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
- Backend rejection still remains authoritative; frontend validation only improves UX.

### 5. Good/Base/Bad Cases

- Good: `3840x2160` from workflow node config opens the inspector with custom dimensions `3840` and `2160`, then submits
  `3840x2160` unchanged.
- Base: `1024x1024`, `2048x2048`, and `3840x3840` appear as preset buttons when present in the derived presets.
- Bad: `ImageChatPage` accepts custom dimensions while `InspectorPanel` still exposes a raw text field.
- Bad: `ImageChatPage` supports provider quality/format/fidelity fields while `InspectorPanel` has a separate partial
  implementation or sends raw unnormalized `tool_options`.
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
  `商品/工作台`, `连续生图`, and `配置`.
- The entries link to `/products`, `/image-chat`, and `/settings`; keep route declarations centralized in `web/src/App.tsx`.
- `TopNav` also exposes the persistent guided-onboarding action through `OnboardingNavButton`. Keep it visually secondary
  to the centered main nav but large enough to be discoverable.
- Page components may still pass `breadcrumbs`, `onHome`, and `onLogout`, but should not duplicate these global nav links
  in a separate header unless that page needs an additional hero call-to-action.
- `TopNav` may use React Router primitives such as `NavLink` / `useLocation`, but must not fetch session or settings data
  directly. Session logout remains a page-owned mutation passed in through `onLogout`.

Wrong:

```tsx
<TopNav breadcrumbs="配置" />
<button onClick={() => onboarding.start()}>开始引导</button>
```

Correct:

```tsx
<TopNav breadcrumbs="配置" onHome={() => navigate("/products")} onLogout={() => logoutMutation.mutate()} />
```

The shared nav itself exposes the settings/image-chat/product links and the guided-onboarding action; pages only add page-specific actions.

---

## Guided Onboarding Components

`web/src/components/OnboardingGuide.tsx` owns the product-native tutorial UI:

- Store the lightweight progress state in browser localStorage through `web/src/lib/onboarding.ts`; do not persist this
  user-assistance state in the backend.
- Do not add a standalone onboarding or documentation route for normal product help. Guided onboarding starts or continues
  from `OnboardingNavButton`; repo documentation such as `docs/USER_GUIDE.md` remains the reference surface.
- Show large `OnboardingGuideCard` / progress panels only on the homepage/product list. Operational pages such as
  product creation, product workbench canvas, and continuous image chat must not render tutorial cards that occupy working
  space. The nav button may still start/continue/reset onboarding and navigate to the relevant route.
- The card should always show current step/progress, a next action, and explicit complete/skip/reset controls.
- Onboarding copy should remain low-jargon and action-oriented: "click this, fill that, expect this result" instead of DAG
  or provider internals.

---

## Common Mistakes to Avoid

- Putting server mutations inside shared presentational components.
- Creating untyped props or using `any` for component inputs.
- Omitting `type="button"` on non-submit buttons inside forms.
- Hardcoding API URLs in components; use `api.toApiUrl(...)` for backend-provided relative image URLs.
- Duplicating status label/style maps instead of reusing `StatusPill` or a local typed `Record`.
