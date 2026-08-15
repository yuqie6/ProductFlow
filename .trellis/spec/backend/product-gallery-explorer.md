# Product Gallery Explorer

## Ownership

- `application/gallery_queries.py` owns directory validation, database filters, keyset pagination, counts, and detail reads.
- `application/gallery_mutations.py` owns folder, rename, and move commands.
- `application/gallery_archives.py` owns bounded ZIP construction and cleanup.
- `application/product_gallery.py` exposes the product-gallery application boundary used by routes.
- `application/product_workflow/v2_reference_bindings.py` owns reference-node rebinding and downstream staleness.
- `presentation/routes/products.py` and `presentation/schemas/products.py` own browser wire contracts.

ProductImageAsset.id is the product-facing image identity.

## Read Contract

Endpoints:

- `GET /api/v2/products/{product_id}/image-library`
- `GET /api/v2/products/{product_id}/image-assets`
- `GET /api/v2/products/{product_id}/image-assets/{asset_id}`

Asset list filters:

- directory kind/key;
- query;
- sort;
- after cursor;
- bounded limit.

Rules:

- Product detail never embeds an unbounded image array.
- Bootstrap uses database counts/grouping.
- Page default is 50 and maximum is 100.
- Fetch limit + 1 and emit next_cursor only when another row exists.
- Cursor is versioned, opaque, and bound to product/filter/sort.
- Keyset ordering uses ProductImageAsset.id as deterministic tie-breaker.
- Search is trimmed, bounded, escaped, and database-backed.
- Metadata excludes storage path, provider payload, base64, and bytes.

## Classification

Every upload, workflow result, and explicit image-session attachment is one ProductImageAsset.

- `image_type_key` is an optional business classification.
- `origin_type` is upload, workflow_generation, or image_session_attach.
- `user_folder_id` is optional one-level organization.
- System directories are virtual filters.
- User-folder membership does not remove an asset from its type/origin directory.

Folder names are trimmed, bounded, and unique per Product.

## Mutations

Endpoints cover:

- create/rename/delete folder;
- rename asset display name;
- move 1..100 assets atomically;
- download archive;
- delete asset under the canonical dependency checks.

Rules:

- lock Product, sorted folders, then sorted assets where concurrent mutation matters;
- requests include expected current name/folder;
- stale expected state is a conflict;
- one cross-product member rejects the entire batch;
- rename does not change original_filename;
- folder deletion clears member folder ids and preserves assets/media/lineage;
- moving changes organization only.

## Archive Downloads

- request contains 1..100 distinct current-product asset ids;
- every asset must have verified present PNG/JPEG/WEBP media;
- declared total is at most 512 MiB;
- write to a temporary file and stream with cleanup;
- derive extension from verified MIME;
- sanitize names and add stable id suffixes for collisions.

Do not build large archives as one in-memory byte string.

## Reference Rebinding

Binding accepts:

- Product, workflow, and reference-node scope;
- stable ProductImageAsset id;
- expected workflow concurrency value;
- expected currently bound asset id.

It validates same-product ownership, updates the reference node, and marks reachable prompt/image nodes stale/idle according to current rules. Historical prompt/generation records and generated assets remain.

Rebinding conflicts while an affected node has queued/running work. A stale prompt must succeed again before dependent image execution.

## Agent Tools

The Agent:

1. lists bounded asset metadata;
2. chooses explicit ids;
3. inspects only required images;
4. uses idempotent durable tools for folder creation/rename, asset rename, and moves.

The list endpoint never returns the whole library's image bytes.

## Tests

- every directory/filter/sort;
- cursor mismatch and complete large traversal;
- special-character search;
- expected-before conflict;
- atomic cross-product move rejection;
- folder deletion preservation;
- archive count/bytes/MIME/name/cleanup;
- reference rebind ownership, active-run conflict, stale recovery, and lineage preservation;
- Agent bounded list/inspect and mutation reconciliation.

## Avoid

- Python-side or browser-side full-library filtering.
- Reference lookup by filename/display name/media id.
- Deleting folder members with the asset-delete command.
- Clearing historical results during rebinding.
- Copying media bytes into a second gallery entity.
- Returning storage paths to the browser.
