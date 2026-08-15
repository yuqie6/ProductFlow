# Legacy History Explorer

> Executable frontend contracts for the read-only, Explorer-style legacy archive surface and old-workflow links.

## Scenario: Inspect archived work without mounting an editor

### 1. Scope / Trigger

- Trigger: changing `LegacyHistoryPage`, `pages/legacy-history/model.ts`, archive DTO/API helpers, History navigation, or
  the archive links shown by existing product workflow pages.
- This page is an inspection and export surface. The existing v1 editor and the schema-v2 workbench remain separate
  product surfaces until their planned cutover steps.

### 2. Signatures

- Routes: `/history` and `/history/:archiveKind/:archiveId`.
- URL query state: optional `kind`, `product_id`, and `q`.
- Query keys:
  - list pages: `['legacy-archives', kind, productId, query]`;
  - detail: `['legacy-archive', kind, archiveId]`.
- Central API methods in `web/src/lib/api.ts`: `listLegacyArchives`, `getLegacyArchive`, and
  `downloadLegacyArchive`.
- Model helpers: `isLegacyArchiveKind`, `flattenLegacyArchivePages`, `legacyArchiveDetailPath`,
  `legacyArchiveExportFilename`, and bounded payload projection helpers.

### 3. Contracts

#### State and data access

- Kind, product filter, committed search, and selected archive are represented by the URL. Search input is local while
  typing and commits its trimmed value after 300 ms.
- List data uses `useInfiniteQuery` with 30-item pages and the server cursor. The client flattens fetched pages only; it
  does not request an unbounded history or filter archived payloads in memory.
- Detail loading is enabled only for a valid kind plus archive ID. An invalid detail kind redirects to the canonical
  list URL while preserving list filters.
- Components consume typed methods from `lib/api.ts`; they do not construct archive API or storage URLs inline.
- Export downloads the server's stable JSON bytes. Image preview/download uses canonical URLs supplied by the detail
  response and the shared image preview component.

#### Read-only interaction

- The page exposes search, kind/product filtering, paging, detail navigation, JSON export, image preview, and canonical
  image download.
- It contains no node editor, free-connect canvas, prompt/image configuration form, workflow run/retry control,
  template apply action, archive mutation, or rebuild shortcut.
- Workflow, Canvas Agent, and user-template payloads use kind-specific semantic summaries. Generic JSON previews are
  bounded in the UI; complete retained data remains available through export.
- The old `ProductDetailPage` keeps its established toolbar, node details, ratio/quality controls, and connection
  behavior. Its History action is an additional product-filtered route, not an editor replacement.
- `ProductWorkbenchPage` may show the same product-filtered History action when no active schema-v2 workflow exists.
  Opening either link must not request a default DAG.
- No runtime setting or provider binding controls this archive browser; adding it does not require a Settings page
  field.

#### Responsive composition

- At `xl`, the page uses a directory rail, compact archive list, and detail pane.
- From `lg` to below `xl`, it uses a directory rail plus either list or detail; selecting a detail replaces the list.
- Below `lg`, kind filters are in the header and list/detail replace one another above the existing mobile navigation.
- Fixed rows, icon buttons, counters, and pane tracks keep stable dimensions. Long titles/product names truncate within
  their panes, and no pane creates horizontal page overflow.
- Mobile detail removes the list header from the active detail view so archive content starts near the top and remains
  visible above bottom navigation.
- Light and dark themes preserve readable borders, selected states, verification metadata, and action contrast.

### 4. Validation & Error Matrix

| Condition | UI behavior |
|---|---|
| List loading/failure | Stable loading state or retry action; no partial fake results |
| Detail loading/failure | Detail-local loading state or retry/back action; list filters remain in URL |
| Invalid route kind | Replace-navigate to filtered `/history` list |
| Search/kind/product changes | A new first-page query identity; prior selected detail is removed from the URL |
| No matches | Search-aware or general empty state; no template/create prompt |
| Export failure | Inline API detail near the export control; detail remains inspectable |
| Referenced media is not verified | Metadata remains visible; preview/download action is unavailable |
| Narrow viewport | List/detail replacement and mobile navigation with no horizontal overflow |

### 5. Good / Base / Bad Cases

- Good: follow a product's History link, search the product's workflow archives, inspect a snapshot, export JSON, and
  preview one verified canonical image.
- Good: copy a detail URL, reload it, then return to the same kind/search/product-filtered list.
- Base: global history with no selected item shows the three classifications and a quiet detail placeholder.
- Bad: import and mount `ProductDetailPage`, a DAG store, or node mutation hooks inside the history route.
- Bad: label an archive snapshot as editable or add a Run/Rebuild action before the Agent rebuild contract exists.
- Bad: fetch every archive or render an arbitrary unbounded payload tree in the browser.

### 6. Tests Required

- Model tests cover valid kinds, page flattening, URL preservation, safe export filenames, and bounded JSON previews.
- API tests assert encoded list filters/cursors, typed detail routes, export error decoding, and session credentials.
- Existing product/workbench tests retain their old controls while archive links use the product filter.
- Run frontend unit tests, ESLint, and the production TypeScript/Vite build.
- For layout changes, verify real `innerWidth` and `clientWidth`, console/network failures, horizontal overflow, and
  screenshots at 1440x900, 1024x768, and 390x844 in light mode plus a representative dark detail/preview state.

### 7. Wrong vs Correct

Wrong:

```tsx
return <ProductDetailPage readOnly workflowId={archiveId} />;
```

Correct:

```tsx
const detail = useQuery({
  queryKey: ["legacy-archive", kind, archiveId],
  queryFn: () => api.getLegacyArchive(kind, archiveId),
});
return <ArchiveDetailPanel detail={detail.data ?? null} />;
```

Wrong:

```ts
const archives = await api.getAllLegacyArchives();
return archives.filter((item) => item.title.includes(query));
```

Correct:

```ts
return api.listLegacyArchives({ kind, product_id: productId, q: query, after: cursor, limit: 30 });
```
