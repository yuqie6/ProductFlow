# Legacy Archive Browser And Agent Rebuild Boundary

> Executable contracts for browsing/exporting immutable legacy archives and starting a separate Agent-owned rebuild.

## Ownership

- `application/legacy_archives.py` owns bounded list/detail reads, cursor validation, canonical asset projection, and
  deterministic JSON export construction.
- `application/legacy_archive_rebuilds.py` owns idempotent rebuild bootstrap, immutable seed projection, and bounded
  Agent list/inspect reads. It never translates an archived payload into a Draft revision or DAG.
- `presentation/routes/legacy_archives.py` and `presentation/schemas/legacy_archives.py` expose the administrator-only
  HTTP contract.
- The archive browser reads `LegacyWorkflowArchive`, `LegacyCanvasAgentArchive`, and `LegacyUserTemplateArchive`.
  It does not reconstruct an archive from live v1 tables during a request.
- `WorkflowDraftLegacyArchiveSeed` binds exactly one immutable archive to one new version-zero `WorkflowDraft` and its
  Agent conversation. The seed is context lineage, not an executable recipe.
- Referenced image identity remains `ProductImageAsset.id`; media-object IDs, storage paths, and legacy source IDs are
  not download identities.

## Scenario: Browse immutable legacy history

### 1. Scope / Trigger

- Trigger: changing archive list/detail/export routes, archive cursor/search behavior, archive asset presentation, or
  frontend links from a product's old workflow surface.
- This contract covers read-only history after additive archive backfill plus the command that opens a new Agent Draft.
  Backfill, cutover freezes, and eventual v1 code removal have separate implementation phases.

### 2. Signatures

- `list_legacy_archives(session, *, kind=None, product_id=None, query="", after="", limit=30) -> LegacyArchivePage`.
- `get_legacy_archive_detail(session, *, kind, archive_id) -> LegacyArchiveDetail`.
- `build_legacy_archive_export(detail) -> dict[str, Any]`.
- `legacy_archive_export_bytes(detail) -> bytes` and `legacy_archive_export_sha256(detail) -> str`.
- `create_legacy_archive_rebuild(session, *, kind, archive_id, target_product_id, idempotency_key) ->
  LegacyArchiveRebuildResult`.
- `list_agent_legacy_archives(..., limit=20) -> dict[str, Any]` and
  `inspect_agent_legacy_archive(..., section, offset, limit) -> dict[str, Any]`.
- Browser endpoints:
  - `GET /api/v2/legacy-archives` with optional `kind`, `product_id`, `q`, `after`, and `limit`;
  - `GET /api/v2/legacy-archives/{kind}/{archive_id}`;
  - `GET /api/v2/legacy-archives/{kind}/{archive_id}/export`;
  - `POST /api/v2/legacy-archives/{kind}/{archive_id}/agent-rebuilds`;
  - canonical archive images continue to use `GET /api/v2/product-image-assets/{asset_id}/download`.
- Internal Agent endpoints:
  - `GET /api/internal/v1/agent-conversations/{conversation_id}/legacy-archives`;
  - `POST /api/internal/v1/agent-conversations/{conversation_id}/legacy-archives/inspect`.

### 3. Contracts

#### Read-only ownership

- List, detail, export, and canonical image reads require the existing administrator session and perform reads only.
  The rebuild command may create a new Draft, immutable seed, and conversation, but exposes no archive mutation,
  workflow run/retry, template apply, Draft revision, or DAG mutation endpoint.
- List and detail requests query archive tables and canonical image metadata only. They must not call
  `get_or_create_product_workflow`, initialize a default DAG, enqueue a worker message, or contact a provider.
- Opening an old product/workbench surface may offer an explicit link to the archive browser. The existing v1 editor
  remains available until the scheduled cutover step; the archive browser does not mount or replace its stores.

#### Bounded list and stable pagination

- Supported kinds are `workflow`, `canvas_agent_thread`, and `user_template`.
- Pages default to 30 and accept 1 through 100 items. Each selected kind fetches at most `limit + 1` candidates before
  the bounded cross-kind merge.
- Ordering is deterministic: archive `created_at` descending, fixed kind rank (`workflow`, `canvas_agent_thread`,
  `user_template`), then archive ID descending. Equal timestamps cannot duplicate or omit an archive across pages.
- Cursors are opaque, versioned, and bound to kind, product, and normalized query through a filter hash. A cursor from a
  different filter is rejected instead of being reused.
- Search trims input, accepts at most 255 characters, escapes `%`, `_`, and the escape character, and uses literal
  case-insensitive matching against kind-specific titles, descriptions, keys, product names, and source IDs.
- A product filter first validates that the product exists. Productless user-template archives do not appear in a
  product-scoped result or count.
- `total` and the three `kind_counts` describe the complete filtered result; they are database counts, not counts of
  the currently loaded page.

#### Detail, assets, and export

- Detail returns the immutable archived payload, source profile/fingerprint, archive payload hash, retained diagnostics,
  summary counts, and canonical asset references. It does not reread mutable v1 workflow/thread/template rows.
- Only workflow archives currently carry explicit asset references. Each response exposes canonical preview/download
  URLs plus verified media metadata; it never exposes `storage_path`, bytes, base64, data URLs, or provider payloads.
- Archive image URLs reuse the canonical product-image download route, so its authorization, file-availability, MIME,
  and image-variant behavior remain authoritative.
- JSON export has schema version 1, UTF-8 encoding, stable key ordering, compact separators, and no NaN values. Its ETag
  is the SHA-256 of the exact response bytes. Re-exporting unchanged archive data yields identical bytes and ETag.

#### Rebuild bootstrap and bounded Agent inspection

- The browser supplies `target_product_id` and an idempotency key of at most 120 characters. The application locks the
  target product and binds `(kind, archive_id, target_product_id)` into a SHA-256 request hash. Reusing the same key for a
  different request is a conflict; an ambiguous retry returns the same Draft/conversation.
- Workflow and Canvas Agent archives can only rebuild into their original product. A productless user-template archive
  requires an explicit target-product selection and may rebuild into any existing product.
- Creation persists a collecting Draft with no current revision, no materialized workflow, one conversation, and one
  `WorkflowDraftLegacyArchiveSeed`. The archive payload/hash and original archive row remain unchanged.
- Agent context receives only `legacy_archive_seed` metadata. It does not receive the archive payload or all images.
  The first browser Turn uses no attached images; the Agent must call the bounded archive tools and then explicitly
  inspect at most six current-product images through the existing image tool when pixels are necessary.
- Agent list pages default to 20 and allow at most 50 metadata rows. Inspect accepts only kind-specific named sections,
  at most 10 items per call, non-negative offsets, and a maximum serialized result of 256 KiB. Asset inspection returns
  canonical metadata only, with no URL, storage path, bytes, base64, or data URL.
- The Go tool catalog uses `list_legacy_archives_v1` and `inspect_legacy_archive_v1`; both are conversation-scoped and do
  not expose product, Draft, or conversation IDs as model arguments.

### 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Invalid kind, cursor, query length, or limit | `400`/request validation; no database write |
| Cursor belongs to different filters | `400`; caller restarts from the first page |
| Product filter names a missing product | `404` |
| Archive kind/ID pair does not exist | `404` |
| Workflow archive references pending or missing canonical media | Metadata remains inspectable; canonical preview/download enforces media availability |
| Archive payload contains no image references | Empty `assets`; no inferred or fabricated asset |
| List, detail, or export request | No DML, default workflow creation, queue message, or provider call |
| Missing target product or missing archive | `404`; no Draft or conversation |
| Product-bound archive targets another product | `400`; archive and target product remain unchanged |
| Same product/idempotency key with a different archive request | `409`; original rebuild identity wins |
| Unsupported inspect section, negative offset, or limit outside 1..10 | `400`; no fallback to complete payload |
| Agent tool output exceeds 256 KiB | `409`; caller narrows section/page instead of receiving truncation |
| Populated rebuild-seed table is downgraded below migration `20260815_0040` | migration refuses; lineage is not dropped |

### 5. Good / Base / Bad Cases

- Good: traverse several kinds one item at a time when all three share a timestamp; each archive appears exactly once.
- Good: open a product-scoped old workflow, follow History, inspect its immutable snapshot, and download a referenced
  canonical image without creating a ProductWorkflow row.
- Base: a productless archived user template appears in the global history and is absent from a product-scoped view.
- Good: start a workflow rebuild, inspect only its summary/nodes/copy/assets pages, inspect one necessary canonical image,
  and let the Agent propose version 1 for explicit user confirmation.
- Base: select a target product for a user-template archive; creation returns an empty version-zero Draft and preserves
  the archive exactly.
- Bad: call the legacy workflow read use case and serialize its current mutable state as an archive detail response.
- Bad: place all legacy run history or image bytes in one list response.
- Bad: expose a storage path or use a legacy SourceAsset/PosterVariant ID as the download URL.
- Bad: copy archived nodes into a Draft revision or materialized workflow during the rebuild POST.

### 6. Tests Required

- Application/API tests cover exact timestamp ties across all kinds, complete keyset traversal, cursor/filter mismatch,
  product/kind filters, literal `%_` search, missing product/archive behavior, and the 100-item bound.
- Export tests assert byte stability, schema version, ETag/hash agreement, diagnostics, and absence of unsafe storage or
  data-URL fields.
- Read-only tests capture SQL DML during list/detail/export requests and assert no `ProductWorkflow` default DAG row is
  created.
- Asset tests prove archive URLs reuse canonical download authorization and return the archived verified media.
- Rebuild tests assert empty Draft/zero DAG, archive immutability, idempotent retry/conflict, original-product scope,
  cross-product template selection, latest-workbench selection, and no archived asset auto-attachment.
- Agent tool tests assert section allowlists, list 50/inspect 10/output 256 KiB limits, metadata-only assets, cross-product
  denial, and correct Go internal endpoint paths.
- Migration tests assert exactly-one archive FK, `CASCADE` Draft/product ownership, `RESTRICT` archive lineage, profile
  recognition at `20260815_0040`, empty downgrade success, and populated downgrade refusal.
- Run backend Ruff, focused archive tests, and the complete backend test suite.

### 7. Wrong vs Correct

Wrong:

```python
workflow = get_or_create_product_workflow(session, product_id)
return serialize_live_workflow(workflow)
```

Correct:

```python
detail = get_legacy_archive_detail(session, kind="workflow", archive_id=archive_id)
return serialize_legacy_archive_detail(detail)
```

Wrong:

```python
return {"image_url": archive_asset.media_object.storage_path}
```

Correct:

```python
urls = build_image_urls(f"/api/v2/product-image-assets/{archive_asset.product_image_asset_id}/download")
```

Wrong:

```python
draft = create_workflow_draft(payload=archive.payload_json)
```

Correct:

```python
draft = WorkflowDraft(product_id=target_product_id, status=WorkflowDraftStatus.COLLECTING)
seed = WorkflowDraftLegacyArchiveSeed(workflow_draft_id=draft.id, workflow_archive_id=archive.id, ...)
# The Agent reads bounded sections and supplies the first complete Draft revision after user confirmation.
```
