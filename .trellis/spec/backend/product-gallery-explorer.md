# Product Gallery Explorer

> Executable contracts for the product-scoped canonical image library, organization commands, archive downloads, and
> schema-v2 reference rebinding.

## Ownership

- `application/gallery_assets.py` owns directory validation, database filtering, keyset pagination, bootstrap counts,
  and gallery detail reads.
- `application/gallery_mutations.py` owns top-level folder creation/rename/delete, asset display-name changes, and
  atomic moves.
- `application/gallery_archives.py` owns bounded ZIP construction and temporary-file cleanup.
- `presentation/routes/products.py` and `presentation/schemas/products.py` expose the browser contract.
- `application/product_workflow/v2_reference_bindings.py` owns canonical reference rebinding and downstream
  invalidation. `v2_staleness.py` owns the image-run freshness guard.
- `ProductImageAsset.id` is the product-facing image identity. `MediaObject.id`, storage paths, filenames, folder IDs,
  and generation-record IDs are not substitutes for it.

## Scenario: Browse and organize a product image library

### 1. Scope / Trigger

- Trigger: changing `ProductImageAsset`, product image folders, image-library routes, image search/sort/pagination,
  batch download, Agent gallery tools, or schema-v2 reference rebinding.
- This contract applies to the product-scoped explorer under the product workbench. The global display-only
  `/api/gallery` and `/gallery` surface has a separate contract.

### 2. Signatures

- `get_gallery_bootstrap(session, *, product_id, current_time=None) -> GalleryBootstrap`.
- `list_gallery_assets(session, *, product_id, directory_kind, directory_key, query, sort, after, limit,
  current_time=None) -> GalleryAssetPage`.
- `get_gallery_asset_detail(session, *, product_id, asset_id) -> GalleryAssetRecord`.
- `create_gallery_folder(...)`, `rename_gallery_folder(...)`, `delete_gallery_folder(...)`.
- `rename_gallery_asset(...)` and `move_gallery_assets(...)`.
- Browser endpoints:
  - `GET /api/v2/products/{product_id}/image-library`.
  - `GET /api/v2/products/{product_id}/image-assets` with `directory_kind`, optional `directory_key`, `q`, `sort`,
    `after`, and `limit`.
  - `GET|PATCH /api/v2/products/{product_id}/image-assets/{asset_id}`.
  - `POST|PATCH|DELETE /api/v2/products/{product_id}/image-folders...`.
  - `POST /api/v2/products/{product_id}/image-assets/move`.
  - `POST /api/v2/products/{product_id}/image-assets/download-archive`.
  - `PATCH /api/v2/products/{product_id}/workflows/{workflow_id}/reference-nodes/{node_id}`.

### 3. Contracts

#### Canonical identity and classification

- Uploads, workflow-generation results, explicit image-session attachments, and legacy imports are represented by one
  `ProductImageAsset` row each. The gallery queries canonical rows; it does not copy media bytes or manufacture a
  second gallery entity.
- `ProductImageAsset.image_type_key` is a nullable classification snapshot. Full prompt, visual-system, node-run, and
  workflow lineage remains on `WorkflowImageGenerationRecord` and related artifacts.
- `ProductImageAsset.user_folder_id` is an optional organizational pointer. Moving an asset changes only this pointer
  and `updated_at`; the asset ID, media object, origin, original filename, cover relation, reference bindings, and
  generation lineage remain unchanged.
- System directories are virtual database filters: `all`, `recent_generated`, `uploads`, `generated`, `image_type`,
  `source`, `unorganized`, and `user_folder`. Membership in a user folder is orthogonal to system-directory membership.
- User folders are product-scoped and one level deep. `ProductAssetFolder` has no parent field. Folder names are
  trimmed, non-empty, at most 120 characters, and unique per product.

#### Bounded reads and stable pagination

- `CanonicalProductDetailResponse` never embeds an unbounded image array. Canonical product creation returns
  `{product, created_assets}` where `created_assets` is the current upload batch of at most six images.
- Bootstrap uses database `COUNT`/`GROUP BY` queries. It does not load every `ProductImageAsset` to derive directory
  counts.
- Asset pages default to 50 and accept at most 100 rows. The query fetches `limit + 1`; `next_cursor` is present only
  when another page exists.
- Cursors are versioned, opaque, and bind the product, directory, search text, and sort through a filter hash. A cursor
  from another query is rejected.
- All four sorts use keyset seek with `ProductImageAsset.id` as the deterministic tie-breaker. Offset pagination is not
  used.
- `recent_generated` captures an `as_of` timestamp in the first-page cursor and reuses it on later pages. Other
  directories reject a cursor carrying that anchor.
- Search trims input, limits it to 255 characters, and uses escaped case-insensitive matching against display name and
  original filename.
- Gallery metadata may include canonical URLs and bounded generation identifiers. It never exposes `storage_path`,
  provider payloads, base64, or image bytes.

#### Organization commands

- All writes validate the product scope and lock in the order Product, sorted folders, then sorted assets.
- Rename and delete commands carry the expected current name. Move commands carry each asset's expected current folder.
  A mismatch is a conflict; the server does not overwrite a concurrent change.
- A move accepts 1 through 100 distinct assets and is atomic. Cross-product assets or folders reject the entire command.
- Folder creation appends a stable `sort_order`. Manual folder reordering and nested folders are not supported.
- Deleting a folder locks its members, sets their `user_folder_id` to null, updates those assets, and deletes the folder.
  It does not delete assets or media, clear a cover, alter workflow nodes, or rewrite generation history.
- Asset rename changes `display_name` only. `original_filename` remains immutable through gallery commands.

#### Archive downloads

- A ZIP request accepts 1 through 100 distinct, current-product asset IDs.
- Every selected media object must be `verified`, present on disk, and use PNG, JPEG, or WEBP. Pending or missing media
  rejects the whole request.
- The sum of declared verified byte sizes must not exceed 512 MiB. The archive is written to a temporary disk file,
  streamed with `FileResponse`, and removed by a response background task. It is not assembled as one in-memory blob.
- Entry names use the display name, sanitize platform-invalid characters, select the extension from verified MIME, and
  add a stable asset-ID suffix when names collide.

#### Schema-v2 reference rebinding

- The binding endpoint accepts a stable canonical asset ID plus `expected_workflow_revision` and
  `expected_bound_asset_id`. The workflow must be active schema-v2 and the target must be a schema-v2 reference node in
  the same product.
- Rebinding does not increment the materialized workflow revision. It updates the reference node and marks reachable
  prompt/image nodes idle while retaining historical runs, output pointers, generated assets, and generation records.
- Each affected prompt output receives `references_stale=true` and accumulates the replaced asset ID in
  `superseded_reference_asset_ids`. A successful prompt rerun replaces the output and clears the stale marker.
- Submit and prepare boundaries for the associated image node reject execution while its prompt output is stale.
- Rebinding rejects when a reachable affected prompt/image node has a queued or running run. This prevents partial
  invalidation of an active execution.

#### Migration and downgrade

- Migration `20260812_0035` creates folders, adds the asset classification/organization fields and indexes, makes
  generation result assets unique, and generalizes `AgentToolMutation` with `prepared_json`.
- Existing workflow-generation assets are backfilled from generation/prompt lineage when that lineage is available;
  missing lineage remains unclassified.
- Downgrade preserves canonical assets and media. It refuses to discard active user-folder organization or v2-only
  Agent ledger data that the old schema cannot represent.

### 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Missing product, folder, or asset in the requested product scope | `404` |
| Directory kind/key mismatch, invalid cursor, duplicate IDs, or size/count violation | `400` |
| Expected folder/name/revision/binding differs from current state | `409` |
| Same folder name in one product | `409`; another product may use the same name |
| Any move member is stale or cross-product | Entire move rejected; no member changes |
| ZIP contains pending/missing media or exceeds declared 512 MiB | Entire archive request rejected |
| v2 rebinding reaches an active prompt/image run | `409`; binding and downstream state remain unchanged |
| Image run observes `references_stale=true` | `409` until the prompt node succeeds again |

### 5. Good / Base / Bad Cases

- Good: an uploaded image moved into `Campaign A` still appears under Uploads, Source/Upload, All, and that user folder.
- Good: the Agent lists a bounded metadata page, chooses explicit IDs, then inspects at most six selected images.
- Base: an unclassified canonical image appears under All, its source directory, Unorganized, and the unclassified image
  type directory.
- Bad: loading every product image and filtering it in Python, React, or an Agent prompt.
- Bad: resolving a reference by display name, filename, media-object ID, or folder path.
- Bad: deleting a folder by calling the canonical asset delete command for its members.
- Bad: clearing historical image outputs when a reference changes; history remains inspectable and the current path is
  made stale explicitly.

### 6. Tests Required

- Query tests cover every directory, special-character search, all four sorts, cursor/query mismatch, recent `as_of`,
  and a complete 1,000-asset traversal without duplicates or omissions.
- Mutation tests cover expected-before conflicts, cross-product rejection, stable IDs, atomic moves, same-name folders,
  and folder deletion returning members to Unorganized without changing cover/node/lineage data.
- Archive tests cover count, verification, declared bytes, MIME-based names, duplicate-name disambiguation, and cleanup.
- v2 workflow tests cover inactive/v1/non-reference/cross-product rejection, active-run conflicts, stale prompt/image
  behavior, prompt rerun recovery, and preservation of historical results.
- Migration tests cover SQLite contracts and an opt-in PostgreSQL 16 `0034 -> 0035 -> 0034 -> 0035` round trip with
  sentinel rows.

### 7. Wrong vs Correct

Wrong:

```python
assets = list(session.scalars(select(ProductImageAsset).where(ProductImageAsset.product_id == product_id)))
visible = [asset for asset in assets if query.lower() in asset.display_name.lower()]
```

Correct:

```python
page = list_gallery_assets(
    session,
    product_id=product_id,
    directory_kind=GalleryDirectoryKind.ALL,
    query=query,
    sort=GalleryAssetSort.CREATED_DESC,
    after=cursor,
    limit=50,
)
```

Wrong:

```python
reference_node.bound_image_asset_id = find_asset_by_filename(filename).id
```

Correct:

```python
bind_v2_reference_node_asset(
    session,
    product_id=product_id,
    workflow_id=workflow_id,
    node_id=node_id,
    asset_id=asset_id,
    expected_workflow_revision=revision,
    expected_bound_asset_id=current_asset_id,
)
```
