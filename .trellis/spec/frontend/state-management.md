# Frontend State Management

> Actual state management choices in ProductFlow.

---

## Overview

ProductFlow uses three state categories:

1. Server state: TanStack Query in pages and `AppRoutes()`.
2. Local UI/form state: React `useState`, `useMemo`, and `useEffect` inside page components.
3. URL state: React Router params and navigation.

There is no Redux, Zustand, Jotai, app-wide React context (besides `QueryClientProvider`), or custom event bus.
The guided onboarding flow is the one current durable browser-local exception: it stores tutorial progress in
`localStorage` because it is user-assistance state, not server data.

---

## Server State

Server state is loaded through `web/src/lib/api.ts` and cached by TanStack Query. The `QueryClient` is created once in
`web/src/App.tsx` with `refetchOnWindowFocus: false`.

Current query key patterns:

- Session: `['session']` in `App.tsx`.
- Product list: `['products']` in `ProductListPage.tsx` and `ImageChatPage.tsx`.
- Product detail/history: `['product', productId]` and `['product-history', productId]` in `ProductDetailPage.tsx`.
- Jobs: `['job', activeCopyJobId]` and `['job', activePosterJobId]` in `ProductDetailPage.tsx`.
- Image sessions: `['image-sessions', productId ?? 'standalone']` and `['image-session', selectedSessionId]` in
  `ImageChatPage.tsx`.
- Runtime config: `['config']` in `ImageChatPage.tsx` and `SettingsPage.tsx`.

When writing mutations, update/invalidate every key that can show stale data.

---

## Local UI and Form State

Keep short-lived UI state local to the page that owns the interaction:

- `ProductCreatePage.tsx` stores form fields, selected files, and a local error string.
- `ProductDetailPage.tsx` stores editing mode, editable copy draft, active job IDs, and job error strings.
- `ImageChatPage.tsx` stores selected session/generated asset, prompt draft, image size, rename mode, target product,
  and transient success/error messages.
- `SettingsPage.tsx` stores config drafts, secret touched flags, reset progress, and save/error messages.

Local state should not duplicate server records unless the user is editing a draft. For example, `SettingsPage.tsx` creates
`drafts` from fetched config so the user can edit before saving; product details themselves remain in TanStack Query.

### Guided onboarding localStorage

`web/src/lib/onboarding.ts` defines the guided-onboarding steps and storage key. `web/src/components/OnboardingGuide.tsx`
provides the hook, `OnboardingNavButton` entry point, and the product-list-only progress card.

- Use localStorage only for small, non-sensitive, cross-page UI assistance state such as onboarding progress.
- Do not store admin keys, provider keys, product records, prompts, image URLs, or API responses in localStorage.
- Dispatch a small browser event after onboarding writes so multiple mounted nav/page cards stay in sync.
- Only the homepage/product list should render the large onboarding progress card. Operational pages may use the hook for
  lightweight progress transitions, but must not show onboarding panels that consume product-create, workbench canvas, or
  image-chat space.
- Do not persist or derive onboarding route state from a standalone help page. Start/continue/reset affordances live in
  `OnboardingNavButton` and the product list guide/progress panel.

---

## URL and Navigation State

React Router owns route selection and route params:

- `useNavigate()` is used after login/logout, product creation, and page buttons.
- `useParams()` supplies `productId` for `ProductDetailPage.tsx` and product-scoped `ImageChatPage.tsx`.
- Auth redirects are centralized in `App.tsx` route elements and `LoginPage.tsx` redirects authenticated users away from
  `/login`.

Do not introduce a global store just to track current page or product ID; use the URL.

---

## Derived State

Prefer derived values over additional state:

- `ProductDetailPage.tsx` derives source image URL, reference images, working copy, and poster variants from
  `ProductDetail`.
- `ImageChatPage.tsx` derives built-in image-size picker presets from `web/src/lib/imageSizes.ts`, selected round from
  the selected asset ID, and product source/reference images from product detail.
- `SettingsPage.tsx` derives grouped config items from the fetched config response.

Use `useMemo` where the derivation is non-trivial or passed deeply; otherwise a local helper function is fine.

---

## API Error State

The central API wrapper throws `ApiError(status, detail)` from `web/src/lib/api.ts`. Pages convert it into local user-facing
strings:

- `LoginPage.tsx` displays invalid key errors.
- `ProductCreatePage.tsx` displays create/upload validation errors.
- `ProductDetailPage.tsx` displays copy/poster/reference image mutation errors.
- `ImageChatPage.tsx` displays generation/session/attach errors.
- `SettingsPage.tsx` displays config validation errors.

Keep error display local unless multiple pages need a shared notification system.

---

## Avoid

- Adding a global store for server data already cached by TanStack Query.
- Keeping a separate local copy of fetched records unless the user is editing a draft.
- Invalidating broad caches unnecessarily when a precise `setQueryData` is already used and safe.
- Hiding route state in local storage or globals instead of using React Router params.
- Storing API keys or admin keys in frontend local storage. Authentication is session-cookie based.
