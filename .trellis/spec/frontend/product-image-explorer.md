# Product Image Explorer

## Ownership

- `useProductImageExplorer(productId)` owns bootstrap/page queries, filters, selection, and mutations.
- `ProductImageExplorer` owns responsive composition and dialogs.
- `ImageDirectoryTree` owns directories and folder commands.
- `ImageAssetGrid` and toolbar/actions own asset presentation.
- `selectionTarget.ts` maps reference-node binding intent.
- `lib/api.ts` owns all gallery HTTP paths.

The explorer manages ProductImageAsset entries for one Product.

## Query Contract

Query keys:

- bootstrap: `["product-image-library", productId]`;
- pages: product id + directory kind/key + normalized search + sort.

Rules:

- search trims and debounces by 250 ms;
- page size is 50;
- cursors are opaque;
- directory/search/sort change clears selection;
- an exact query-identity change removes its previous infinite page chain;
- “select all” covers at most 100 currently loaded assets;
- mutations invalidate bootstrap counts and product asset-page keys.

Use URLSearchParams through `api.ts`. Components do not construct API URLs.

## Directories

The tree renders backend-provided:

- system directories;
- image-type directories;
- origin directories;
- one-level user folders.

Folder actions support create, rename, delete, and move target selection. Deleting a folder returns its assets to unorganized state; it does not delete images.

## Responsive Layout

- Observe explorer content width with ResizeObserver.
- At 440 px and above, show the fixed directory rail.
- Below 440 px, use the in-flow directory toggle/panel.
- Re-run observation after bootstrap mounts the real root.
- Thumbnails, rows, counters, and controls have stable dimensions.
- Long names truncate or wrap without changing inspector width.
- Touch actions do not depend on hover and use appropriate target sizes.

Real browser verification must include content widths 560, 440, 439, and 280 px, plus the actual mobile drawer.

## Asset Actions

- preview and download one verified asset;
- rename display name;
- move selected loaded assets;
- download selected assets as ZIP;
- open delivery rendition controls;
- bind one explicit asset to a reference node.

Missing media remains visible as metadata and cannot be previewed/downloaded.

Move/rename requests send expected current values. Conflict responses display near the current directory without optimistic overwrite.

## Reference Binding

`ImageExplorerReferenceTarget` identifies:

- workflow id;
- reference node id;
- expected workflow edit/revision value required by the API;
- expected currently bound asset id;
- optional completion callback.

The action sends one ProductImageAsset id, invalidates the active workflow/node queries, and closes selection mode only after success.

Product cover and page order have no binding semantics.

## Tests

- query identity, flattening, cursor page append, and selection cap;
- 439/440 px responsive boundary;
- owner-scoped grid/list preference;
- drag/move payload expected state;
- reference selection target;
- missing-media action disabling;
- encoded API params and archive error decoding.

Run Vitest, ESLint, build, and screenshot checks for layout changes.

## Avoid

- `window.innerWidth` for an inspector-contained explorer.
- Loading/filtering the full product library in React.
- Hover-only actions.
- Binding by filename, display name, folder, or cover.
- Hidden deletion of unselected or overwritten candidates.
- Separate reference pickers for the same ProductImageAsset model.
