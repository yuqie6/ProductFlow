# Product Image Explorer

> Executable frontend contracts for the product-scoped image-library explorer and its workbench integration.

## Scenario: Browse and organize canonical product images

### 1. Scope / Trigger

- Trigger: changing `ProductImageExplorer`, its hook/state helpers, gallery DTOs/API methods, the Images inspector panel,
  responsive behavior, or schema-v2 "use as reference" integration.
- The explorer manages canonical `ProductImageAsset` entries for one product. It does not replace the global `/gallery`
  page and does not provide canonical asset deletion.

### 2. Signatures

- `useProductImageExplorer(productId)` owns bootstrap/page queries, search, directory/sort/view state, selection, and
  gallery mutations.
- `ProductImageExplorer({ productId, productName, onPreviewImage, referenceTarget? })` owns responsive composition and
  dialogs.
- `ImageExplorerReferenceTarget` contains `workflowId`, `nodeId`, `expectedWorkflowRevision`,
  `expectedBoundAssetId`, and optional `onBound`.
- Query keys:
  - bootstrap: `['product-image-library', productId]`;
  - pages: `['product-image-library-assets', productId, directoryKind, directoryKey, query, sort]`.
- Local preference key: `productflow.product.{productId}.imageExplorer.view` with value `grid` or `list`.
- Central API methods live in `web/src/lib/api.ts`: `getProductImageLibrary`, `listGalleryAssets`, `getGalleryAsset`,
  folder mutations, asset rename/move, archive download, and `bindWorkflowReferenceAsset`.

### 3. Contracts

#### Server and local state

- Bootstrap, pages, and mutation results are React Query server state. Directory, debounced search input, sort,
  selection, dialogs, and narrow-directory visibility are component/hook state.
- Only grid/list preference is stored in owner-scoped `localStorage`. Directory, search, cursor, selection, and dialog
  state are not persisted there.
- Search is trimmed and debounced by 250 ms. Directory, effective search, or sort changes clear selection.
- Infinite queries request 50 rows at a time and render only fetched pages. "Select all" means up to 100 currently loaded
  assets; it does not issue a hidden whole-library query.
- A query-identity change removes the previous exact infinite-query cache entry. Returning to an earlier
  directory/search/sort starts from its first page instead of restoring a large stale page chain.
- Gallery mutations invalidate both the bootstrap counts and all product asset-page keys. A successful move or folder
  delete also clears selection.
- API query strings use `URLSearchParams`. Components never construct `/api/v2/...` URLs directly.

#### Responsive layout and interaction

- Responsiveness is based on the explorer component's observed content width, not the browser viewport or configured
  sidebar width.
- At 440 px and wider, the explorer renders a 148 px directory rail beside the asset area. Below 440 px, the rail is
  replaced by a directory toggle and an in-flow directory panel.
- `ProductImageExplorer` returns a loading/error state before its root exists. The `ResizeObserver` effect must run again
  after bootstrap data mounts the root; an empty dependency list leaves the first real render permanently narrow.
- Grid/list rows, thumbnails, counters, and controls have stable dimensions. Long names truncate or wrap within their
  own bounds and do not resize the inspector.
- Mobile actions are available without hover. Folder action buttons remain visible at narrow widths, and interactive
  targets in the mobile drawer are at least 44 by 44 px where the compact desktop action is expanded for touch.
- Preview and download are disabled for media whose verification status is not `verified`; metadata remains visible.
- Drag/drop and move dialogs call the same move mutation with each loaded asset's expected current folder.

#### Workbench compatibility and references

- `ImagesPanel` mounts the canonical explorer as the default view.
- When a schema-v1 reference node is selected, a sibling legacy tab keeps the existing SourceAsset/PosterVariant picker.
  The explorer does not expose canonical re-reference in this v1 path and does not write canonical-to-legacy mappings.
- A schema-v2 owner may pass `referenceTarget`. The action sends the stable canonical asset ID and both expected values,
  then invalidates the active v2 workflow query and forwards the binding result to `onBound`.
- The explorer never infers a product cover from page order. Cover identity comes from bootstrap/product
  `cover_image_asset_id`.

### 4. Validation & Error Matrix

| Condition | UI behavior |
|---|---|
| Bootstrap loading/failure | Stable loading state or retry action; asset surface is not mounted with partial metadata |
| Asset page loading/failure | Loading state or page-local retry; folder shell remains usable after bootstrap |
| Directory/search/sort changes | Selection cleared, prior exact page chain removed, new first page requested |
| Mutation conflict or validation error | `ApiError.detail` shown beside the current directory; no optimistic overwrite |
| Missing or pending media | Metadata card/list row remains; preview and archive selection are unavailable |
| Narrow explorer | Directory toggle/panel; no permanent desktop rail or hover-only folder actions |
| v1 reference selected | Explorer and "legacy workflow reference" tabs shown; legacy fill callbacks remain unchanged |
| v2 bind conflict | Error remains in the explorer; active workflow cache is not treated as successfully rebound |

### 5. Good / Base / Bad Cases

- Good: load 50 assets, load 50 more, enter a search, then clear it; the all-assets query returns to one 50-row page.
- Good: at 408 px component width inside a 1440 px viewport, the explorer uses its narrow layout.
- Base: a product with no assets shows a bounded empty state and still permits upload/folder creation.
- Bad: use `window.innerWidth` to decide whether the directory rail fits inside the inspector.
- Bad: cache every visited infinite-query page chain for a 1,000-asset product.
- Bad: hide rename/move/folder controls behind hover on touch devices.
- Bad: expose the v2 canonical bind action while the current canvas still owns a schema-v1 reference node.

### 6. Tests Required

- Unit tests cover query-key identity, page flattening, selection cap, drag payload validation, byte/pixel formatting,
  owner-scoped view preference, and the 439/440 px boundary.
- API tests assert encoded list parameters, expected-before mutation bodies, archive error decoding, and credentials.
- Component/workbench tests preserve the existing v1 SourceAsset/PosterVariant bind path and assert that the default tab
  is the canonical explorer.
- Run `pnpm --dir web test:run`, `pnpm --dir web lint`, and `just web-build`.
- For layout changes, verify actual `innerWidth/clientWidth`, component bounding boxes, console/network failures, and
  screenshots at inspector widths 560, 440, and 280 px; a 1024 px desktop viewport; and a 390 px mobile drawer.

### 7. Wrong vs Correct

Wrong:

```tsx
const wide = window.innerWidth >= 1024;
return wide ? <DirectoryRail /> : <DirectoryMenu />;
```

Correct:

```tsx
useEffect(() => {
  const element = rootRef.current;
  if (!element || typeof ResizeObserver === "undefined") return;
  const observer = new ResizeObserver(([entry]) => {
    setWide(isWideImageExplorer(entry.contentRect.width));
  });
  observer.observe(element);
  return () => observer.disconnect();
}, [bootstrap]);
```

Wrong:

```ts
const assets = await api.listProductImageAssets(productId);
const visible = assets.items.filter((asset) => asset.display_name.includes(query));
```

Correct:

```ts
await api.listGalleryAssets(productId, {
  directory_kind: directory.kind,
  directory_key: directory.key,
  q: query,
  sort,
  after: cursor,
  limit: 50,
});
```
