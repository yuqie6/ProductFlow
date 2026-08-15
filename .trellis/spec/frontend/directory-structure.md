# Frontend Directory Structure

## Overview

The frontend is a React 19 + Vite + strict TypeScript app under `web/src/`.

- React Router owns pages and URL state.
- TanStack Query owns server state.
- XYFlow owns the workflow canvas.
- Tailwind CSS 4 owns utility styling.
- `lib/api.ts` and `lib/types.ts` form the shared HTTP/DTO boundary.

There is no global client-state store.

## Current Layout

```text
web/src/
  main.tsx
  App.tsx
  index.css
  components/
    TopNav.tsx
    ImageDropZone.tsx
    image-generation/
  lib/
    api.ts
    types.ts
    i18n.ts
    preferences.tsx
    format.ts
    imageSizes.ts
    imageToolOptions.ts
  pages/
    LoginPage.tsx
    ProductListPage.tsx
    AgentProductCreatePage.tsx
    ProductWorkbenchPage.tsx
    ImageChatPage.tsx
    GalleryPage.tsx
    SettingsPage.tsx
    HelpPage.tsx
    agent-workbench/
    product-workflow-v2/
    product-detail/
    product-list/
    image-chat/
```

Use `rg --files web/src` before editing; the live tree is authoritative.

## Routes

Routes are centralized in `App.tsx`:

- `/login` -> LoginPage
- `/products` -> ProductListPage
- `/products/new` -> AgentProductCreatePage
- `/products/new/agent` -> redirect to `/products/new`
- `/products/:productId` -> ProductWorkbenchPage
- `/image-chat` -> ImageChatPage
- `/gallery` -> GalleryPage
- `/help` -> HelpPage
- `/settings` -> SettingsPage

Auth gating also stays in AppRoutes. Do not introduce a second router or duplicate page for one product flow.

## Page Ownership

Route pages own high-level query/mutation composition, navigation, and page layout:

- AgentProductCreatePage: intake and first Agent workspace.
- ProductWorkbenchPage: canonical product-id route and workbench loading.
- ImageChatPage: sessions, candidates, branching, and save-to-product.
- GalleryPage: explicit collected image entries.
- SettingsPage: provider profiles, bindings, runtime settings, and import/export.
- HelpPage: current localized product documentation.

Complex feature code belongs in page-local directories.

## Workbench Feature Directories

`agent-workbench/` owns:

- Agent message list/composer/questions;
- Turn event reducer and SSE hooks;
- Draft confirmation;
- materialization reveal;
- responsive Agent/workbench shell.

`product-workflow-v2/` owns:

- canvas and command bar;
- add-node panel;
- node inspector;
- run panels;
- recipes;
- delivery renditions;
- graph, draft, generation-spec, viewport, and side-panel helpers.

`product-detail/` contains polished components retained and reused by the current workbench:

- canvas chrome and node card;
- inspector shell, tabs, status, text controls, shortcuts;
- product image Explorer and its hooks/helpers.

The directory name does not imply a separate page. Do not duplicate these components to make the Agent workbench look independent.

## Shared Components

Place a component in `web/src/components/` when multiple route/features actually reuse it:

- TopNav;
- image drop zone;
- dialogs and form fields;
- image-generation settings controls.

Keep tightly coupled workflow/library components in their feature directory until another feature shares their contract.

## Lib Boundary

- `api.ts` is the only raw fetch/path/credentials owner.
- `types.ts` mirrors backend DTOs and enums.
- `i18n.ts` owns locale dictionaries and keys.
- `preferences.tsx` owns global locale/theme.
- `format.ts` owns pure display formatting.
- `imageSizes.ts` and `imageToolOptions.ts` own current image control parsing/options.

Do not place React query hooks or page-specific projections in `lib/` merely to shorten a page file.

## Naming

- Components/pages: `PascalCase.tsx`.
- Hooks: `useSomething.ts`.
- Pure helpers: descriptive `camelCase.ts`.
- Test: owner filename plus `.test.ts` / `.test.tsx`.
- Backend wire fields remain `snake_case` in DTOs.
- Display translation goes through i18n; enum strings are not user-facing labels.

## Placement Decision

Before adding a file:

1. Is it a route? Put it in `pages/` and register it once in App.tsx.
2. Is it owned by Agent, V2 workflow, product library, product list, or image chat? Use that feature directory.
3. Is it visually reused with the same props in multiple features? Use `components/`.
4. Is it a wire DTO/API/format/preference helper? Use `lib/`.
5. Is it a one-off helper? Keep it beside the owner.

## Avoid

- Raw fetch outside `lib/api.ts`.
- DTO definitions inside components.
- A global store for TanStack Query data.
- A parallel Agent canvas or product detail page.
- Moving a component to global scope before real reuse.
- Browser-generated default workflows.
- Keeping deleted page names in route, query, or test conventions.
