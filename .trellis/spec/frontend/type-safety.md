# Frontend Type Safety

> TypeScript and API typing conventions used by ProductFlow.

---

## Overview

The frontend uses strict TypeScript. `web/tsconfig.app.json` sets `strict: true`, `allowJs: false`,
`isolatedModules: true`, `moduleResolution: "Bundler"`, and `jsx: "react-jsx"`. The build command in `web/package.json`
runs TypeScript checks before Vite build:

```bash
pnpm --dir web build
# tsc --noEmit -p tsconfig.app.json && tsc --noEmit -p tsconfig.node.json && vite build
```

Runtime API typing is centralized in:

- `web/src/lib/types.ts`
- `web/src/lib/api.ts`

---

## API DTO Types

`web/src/lib/types.ts` mirrors backend Pydantic response/request shapes. It intentionally preserves backend field names,
including `snake_case`:

- `ProductSummary.workflow_state`
- `CopySet.creative_brief_id`
- `ImageSessionGenerationTask.failure_reason`
- `ImageSessionRound.provider_response_id`
- `SessionState.access_required`
- `RuntimeConfig.admin_access_required`
- `ConfigUpdateRequest.reset_keys`

Do not silently convert these to camelCase in frontend types unless the API layer also performs explicit mapping.

String union types mirror backend enums:

```ts
export type ProductWorkflowState = "draft" | "copy_ready" | "poster_ready" | "failed";
export type JobStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";
```

If backend enum values in `backend/src/productflow_backend/domain/enums.py` change, update these unions and all UI maps
such as `StatusPill.tsx::CONFIG`.

Workflow run DTOs mirror backend run action metadata. When the backend adds `is_retryable`, `is_cancelable`, or queue
fields (`queue_active_count`, `queue_running_count`, `queue_queued_count`, `queue_max_concurrent_tasks`,
`queued_ahead_count`, `queue_position`), update both `WorkflowRun` and `WorkflowRunStatusSummary` because full detail and
lightweight status polling merge through the same cache.

---

## API Client Typing

`web/src/lib/api.ts` exposes typed methods on the `api` object. The internal `request<T>(...)` returns a `Promise<T>` and
throws typed `ApiError` on non-2xx responses.

Examples:

```ts
getProduct(productId: string): Promise<ProductDetail> {
  return request(`/api/products/${productId}`);
}

updateConfig(payload: ConfigUpdateRequest): Promise<ConfigResponse> {
  return request("/api/settings", {
    method: "PATCH",
    body: JSON.stringify(payload),
  });
}
```

Form uploads build `FormData` in API methods such as `createProduct(...)`, `addReferenceImages(...)`, and
`addImageSessionReferenceImages(...)`. The fetch wrapper omits `Content-Type` for `FormData` so the browser can set the
multipart boundary.

### Scenario: Canonical product image DTOs

#### 1. Scope / Trigger

- Trigger: changing `/api/v2/products`, product-scoped image-library routes, `/api/v2/product-image-assets`, canonical
  cover operations, or canonical ImageSession attach.

#### 2. Signatures

- `api.createCanonicalProduct(input: CreateCanonicalProductInput): Promise<CanonicalProductCreateResponse>`.
- `api.getCanonicalProduct(productId: string): Promise<CanonicalProductDetail>`.
- `api.getProductImageLibrary(productId: string): Promise<GalleryBootstrap>`.
- `api.listGalleryAssets(productId, input): Promise<GalleryAssetPage>` owns paginated directory/search/sort reads.
- `api.getGalleryAsset(productId, assetId): Promise<GalleryAsset>` owns an explicit canonical asset detail read.
- `api.addCanonicalProductImages(productId: string, images: File[]): Promise<ProductImageAssetListResponse>`.
- `api.setProductCover(...)`, `api.clearProductCover(...)`, and `api.deleteProductImageAsset(...)` own cover/delete calls.
- `api.attachImageSessionAssetToProductCanonical(sessionId, assetId, productId): Promise<ProductImageAsset>`.

#### 3. Contracts

- `CanonicalProductDetail` is bounded product metadata and has no `image_assets` array. `cover_image_asset_id` is
  separate and nullable.
- `CanonicalProductCreateResponse` contains `{product, created_assets}`. `created_assets` is only the current upload
  batch and is bounded by the six-image creation limit.
- `ProductImageAsset` mirrors backend snake_case fields, including `media_object_id`, `origin_type`,
  `image_type_key`, `user_folder_id`, `parent_asset_id`, `source_image_session_asset_id`, and `verification_status`.
- `GalleryAsset` extends the canonical asset with nullable folder/type labels and bounded generation identifiers.
- Product-library pages use `GalleryAssetPage.items` plus opaque `next_cursor`; UI code must not use the legacy
  unbounded asset-list method to implement search, sorting, directories, or counts.
- `MediaVerificationStatus` is `"verified" | "legacy_pending" | "missing"`.
- `ProductImageOriginType` is `"upload" | "workflow_generation" | "image_session_attach" | "legacy_import"`.
- DTOs expose download/preview/thumbnail URLs and measured metadata. They never expose `storage_path`.
- `api.createCanonicalProduct` and `api.addCanonicalProductImages` own repeated `images` FormData fields. Page code must
  not reconstruct canonical endpoints or multipart names.
- `api.attachImageSessionAssetToProductCanonical` has only `product_id`; setting a cover is a separate API call.

#### 4. Validation & Error Matrix

- Empty or more than six multipart images -> backend validation error through `ApiError.detail`.
- Declared MIME does not match PNG/JPEG/WEBP bytes -> backend validation error through `ApiError.detail`.
- Cover asset belongs to another product or points to missing media -> backend `400` through `ApiError.detail`.
- Deleting an asset still used as cover, legacy archive target, or derivative parent -> backend `409` through
  `ApiError.detail`.
- Downloading a missing canonical media object -> backend `404`.

#### 5. Good/Base/Bad Cases

- Good: page code passes `File[]` to `api.createCanonicalProduct`; the API helper appends repeated `images` fields and
  allows the browser to set the multipart boundary.
- Base: a product with no selected cover uses `cover_image_asset_id: null`; callers do not infer a cover from gallery
  page order.
- Bad: a page builds `/api/v2/...` URLs or multipart bodies directly and drifts from the central API/type contract.
- Bad: UI treats `media_object_id` as the selectable product image identity; product-facing references use
  `ProductImageAsset.id`.

#### 6. Tests Required

- Keep `web/src/lib/types.ts` and backend Pydantic models synchronized.
- Backend API tests assert multipart field names, response ordering, shared-media attach, status codes, and absence of
  `storage_path`.
- Run `pnpm --dir web test:run`, `pnpm --dir web lint`, and `just web-build` after changes.

#### 7. Wrong vs Correct

Wrong:

```ts
const body = new FormData();
body.append("file", image);
fetch(`/api/v2/products/${productId}/image-assets`, { method: "POST", body });
```

Correct:

```ts
await api.addCanonicalProductImages(productId, images);
```

Wrong:

```ts
const cover = loadedAssets[0];
```

Correct:

```ts
const cover = product.cover_image_asset_id
  ? await api.getGalleryAsset(product.id, product.cover_image_asset_id)
  : null;
```

### Scenario: Create-product API input typing

#### 1. Scope / Trigger

- Trigger: changes to the product creation form, `api.createProduct(...)`, or backend `POST /api/products` multipart
  fields.
- Product creation is a cross-layer form-upload contract. Keep the shape centralized in `web/src/lib/types.ts` and have
  `web/src/lib/api.ts` translate it into `FormData`.

#### 2. Signatures

- Shared frontend DTO: `CreateProductInput`.
- API method: `api.createProduct(input: CreateProductInput): Promise<ProductDetail>`.
- Multipart fields currently mirrored from the backend:
  - `name: string`
  - `file: File` -> form field `image`
  - `referenceFiles?: File[]` -> repeated form field `reference_images`
  - `category?: string`
  - `price?: string`
  - `source_note?: string`
  - `canvas_template_key?: string`

#### 3. Contracts

- Keep backend field names in the DTO for optional form values such as `source_note` and `canvas_template_key`.
- `canvas_template_key` is the backend-recognized key. UI labels should be merchant-facing output plans, but the submitted
  value remains the key.
- Blank/default product-creation plans may submit an empty string or omit `canvas_template_key`; the backend owns default
  alias handling.
- Product creation large previews for backend-recognized built-in plans must mirror the backend `full_canvas` template
  layout for the same key. When changing preview node titles, edges, or coordinates, update the backend template and
  backend regression tests in the same change.
- The page component must not duplicate the full mutation object type inline when a shared DTO exists.
- The API method owns `FormData` construction. Page components should call `api.createProduct(...)` with typed values, not
  construct raw multipart bodies themselves.

#### 4. Validation & Error Matrix

- Missing `file` is handled by the page before calling the API and should produce the existing `请先上传商品图` message.
- Invalid/unknown `canvas_template_key` is backend validation and surfaces through `ApiError.detail`.
- Upload MIME/size errors are backend upload-validation errors and surface through the same `ApiError.detail` path.

#### 5. Good/Base/Bad Cases

- Good: `ProductCreatePage` stores a selected plan key in component state, displays merchant-facing labels, and passes
  `canvas_template_key` into `api.createProduct`.
- Base: a blank/basic option can use `""` while still sharing the typed DTO.
- Bad: `ProductCreatePage` creates `FormData` directly and bypasses the typed API helper.
- Bad: frontend renames `canvas_template_key` to `canvasTemplateKey` without an explicit mapping layer.

#### 6. Tests Required

- `pnpm --dir web build` must pass after any create-product DTO change.
- Add focused frontend tests for pure helper logic if plan selection or payload routing becomes non-trivial.
- Backend API tests remain the source of truth for multipart validation, template-key error status, and persisted template
  coordinates mirrored by the creation page preview.

#### 7. Wrong vs Correct

Wrong:

```ts
return api.createProduct({
  name,
  file,
  canvasTemplateKey,
});
```

Correct:

```ts
return api.createProduct({
  name,
  file,
  canvas_template_key: selectedPlanKey,
});
```

---

## Local Types

### Scenario: WorkflowDraft and schema-v2 workflow DTOs

#### 1. Scope / Trigger

- Trigger: changing WorkflowDraft requests/responses, strict visual/prompt payloads, v2 workflow/node-run projections,
  reveal-event playback, or the separation between legacy and v2 canvas node types.

#### 2. Signatures

- Legacy `WorkflowNodeType` remains the four-value v1 union and excludes `prompt_generation`.
- `WorkflowNodeTypeV2` is `product_context | reference_image | prompt_generation | image_generation`.
- Central API methods: `getActiveProductWorkflowV2`, `createWorkflowDraft`, `getWorkflowDraft`,
  `appendWorkflowDraftRevision`, `confirmWorkflowDraft`, `materializeWorkflowDraft`, and
  `workflowRevealEventsUrl`.
- Node execution methods:
  - `runWorkflowNodeV2(nodeId: string): Promise<SubmitWorkflowNodeRunV2Result>`;
  - `getWorkflowNodeRunV2(nodeRunId: string): Promise<WorkflowNodeRunV2>`.
- Strict DTOs include `WorkflowVisualSystemPayloadV1`, discriminated `WorkflowVisualFieldOverride`,
  `WorkflowImagePromptPayloadV1`, `WorkflowGenerationSpec`, `WorkflowActualMedia`, and `WorkflowNodeRunV2`.

#### 3. Contracts

- `WorkflowDraftPayloadV1.schema_version` and reveal event `schema_version` are literal `1`; materialized workflow and node
  schema versions are literal `2`.
- DTO fields retain backend `snake_case`. VisualSystem and Prompt Artifact payloads mirror the backend strict schema;
  avoid replacing them with `Record<string, JsonValue>`. Materialized node config/output remain open JSON because their
  exact shape is node-type and run-state dependent.
- The Draft response supplies `limits` for image-type, per-type, total-image, and reference-asset counts. UI code reads
  these values and does not duplicate backend numeric limits.
- Draft revisions expose nullable `visual_system_version_id`; materialized workflows expose a required fixed version ID;
  prompt nodes expose nullable `current_prompt_artifact_version_id` and image nodes use `bound_image_asset_id` as their
  current result pointer.
- `WorkflowNodeRunV2` keeps `requested_spec`, provider-specific `effective_parameters`, and decoded `actual_media`
  separate. `actual_media` has a PNG/JPEG/WEBP MIME union plus positive width, height, byte size, and SHA-256. The DTO does
  not contain storage paths or raw provider request/output objects.
- V1 components continue accepting `WorkflowNodeType`; v2 components explicitly accept `WorkflowNodeTypeV2` or the full
  v2 DTO. Do not widen the legacy union to make prompt nodes compile in old rendering/execution paths.
- `workflowRevealEventsUrl(materializationId, after?)` owns the SSE path and replay cursor. Consumers first load the complete
  v2 workflow; reveal events control presentation order only.

#### 4. Validation & Error Matrix

- Backend `422` for a strict Draft shape -> surface `ApiError.detail`; do not coerce unknown fields locally.
- Backend `409` for stale revisions, active v1, or idempotency drift -> refresh the corresponding Draft/workflow state
  before a deliberate retry.
- Backend `409` for a v1 node sent to v2 run APIs, an inactive workflow, or a non-runnable v2 context/reference node ->
  surface `ApiError.detail`; do not fall back to the legacy run endpoint.
- A repeated run submit for the same queued/running node may return `created: false` with the existing `node_run`; callers
  poll that stable ID instead of submitting again.
- Empty v2 query -> handle `workflow: null` and `latest_revision`; do not call a legacy endpoint to fill it.
- SSE disconnect -> reconnect with EventSource `Last-Event-ID` behavior or rebuild the URL with `after`; the committed
  workflow remains the recovery source.

#### 5. Good/Base/Bad Cases

- Good: use response `limits.max_images_per_type` to configure the count control and submit the chosen quantity in the
  complete Draft payload.
- Good: display requested settings, adapter-effective settings, and decoded output metadata from their three dedicated
  fields without inferring one from another.
- Base: render no v2 canvas when `workflow` is null while retaining `latest_revision` for the materialization command.
- Base: a queued/running node query has null generation evidence; successful prompt runs may expose only a Prompt Artifact
  version while successful image runs expose the full generation evidence.
- Bad: add `prompt_generation` to legacy `WorkflowNodeType` and let old switch/maps silently accept an unsupported node.
- Bad: treat reveal SSE as the only source of workflow entities and lose the canvas after a reload.
- Bad: derive actual dimensions from `requested_spec.aspect_ratio` or expose provider raw JSON so UI code depends on one
  provider's response shape.

#### 6. Tests Required

- API helper tests assert exact materialization/run paths, methods, JSON fields, and reveal URL cursor construction.
- TypeScript build must verify distinct v1/v2 node unions, literal schema versions, strict visual/prompt fields, and typed
  requested/actual generation evidence.
- Run `pnpm --dir web test:run`, `pnpm --dir web lint`, and `just web-build` after DTO changes.
- UI integration work must test reconnect/reload against the complete workflow query when reveal animation is implemented.

#### 7. Wrong vs Correct

Wrong:

```ts
export type WorkflowNodeType = "product_context" | "reference_image" | "copy_generation" |
  "prompt_generation" | "image_generation";
const maxPerType = 6;
```

Correct:

```ts
export type WorkflowNodeTypeV2 = "product_context" | "reference_image" |
  "prompt_generation" | "image_generation";
const maxPerType = draft.limits.max_images_per_type;
const run = await api.runWorkflowNodeV2(nodeId);
const evidence = await api.getWorkflowNodeRunV2(run.node_run.id);
```

### Scenario: Settings migration API typing

#### 1. Scope / Trigger
- Trigger: changes to settings export/import API methods, SettingsPage import/export UI, or backend
  `SettingsExportDocument` / `SettingsImportPreviewResponse` / `SettingsImportCommitResponse` schemas.
- Settings migration is a cross-layer DTO contract. Keep TypeScript types aligned with backend Pydantic schemas and keep
  backend `snake_case` field names.

#### 2. Signatures
- API methods:
  - `api.exportSettings(): Promise<SettingsExportDocument>`
  - `api.previewSettingsImport(payload: SettingsExportDocument): Promise<SettingsImportPreview>`
  - `api.importSettings(payload: SettingsExportDocument): Promise<SettingsImportCommitResponse>`
- Frontend DTOs live in `web/src/lib/types.ts` and mirror backend field names:
  - `SettingsExportDocument`
  - `SettingsExportMetadata`
  - `SettingsProviderProfileExport`
  - `SettingsProviderBindingExport`
  - `SettingsImportPreview`
  - `SettingsImportCommitResponse`

#### 3. Contracts
- `runtime_config` is a map of config key to JSON scalar/list values from the backend export.
- `provider_profiles` may include `api_key`; SettingsPage must treat exported files as sensitive and show confirmation
  copy before download.
- `provider_bindings` references imported provider profile ids for non-mock bindings.
- Import preview response fields are flat DTO fields such as `runtime_config_count`,
  `provider_profile_count`, `provider_binding_count`, `includes_api_keys`, and
  `provider_profiles_with_api_key_count`; do not invent a nested `metadata.summary` layer unless the backend schema
  changes in the same commit.
- Import commit returns refreshed settings/provider config data or enough data for SettingsPage to invalidate and refetch
  `['config']`, `['provider-config']`, `['runtime-config']`, and `['session']`.

#### 4. Validation & Error Matrix
- Invalid JSON file -> SettingsPage shows a local invalid-file error before calling the API.
- API 400 from preview/commit -> show `ApiError.detail`.
- User cancels export/import confirmation -> do not call the API.
- Successful import -> invalidate settings/runtime/session queries so UI reflects the imported values.

#### 5. Good/Base/Bad Cases
- Good: export downloads exactly the typed backend payload, then importing that JSON previews the same counts.
- Good: preview with `includes_api_keys=true` shows sensitive-file warning before commit.
- Base: import file contains `mock` provider bindings and no provider API keys.
- Bad: frontend reads `preview.metadata.summary` when backend returns flat preview fields.
- Bad: converting DTO fields to camelCase in `types.ts` without an explicit API mapping layer.

#### 6. Tests Required
- SettingsPage tests for export confirmation and generated JSON download path.
- SettingsPage tests for import preview summary, API-key warning, commit confirmation, and query invalidation.
- `pnpm --dir web build` after any settings migration DTO change.

#### 7. Wrong vs Correct

Wrong:

```ts
const keyCount = preview.metadata.summary.providerProfilesWithApiKeyCount;
```

Correct:

```ts
const keyCount = preview.provider_profiles_with_api_key_count;
```

Keep frontend reads aligned with the backend response shape.

---

Use local `type` aliases for page-only structures:

- `EditableCopy` in `ProductDetailPage.tsx`.
- `DraftValue` in `SettingsPage.tsx`.

Use `interface` for component props and DTO object shapes:

- `TopNavProps` in `TopNav.tsx`.
- `ConfigFieldProps` in `SettingsPage.tsx`.
- API DTOs in `web/src/lib/types.ts`.

Static option arrays can use `as const`, as in `ImageChatPage.tsx::DEFAULT_SIZE_OPTIONS`.

---

## Runtime Validation Reality

The frontend currently relies on backend validation for API payloads and on TypeScript for compile-time checks. There is no
Zod/Yup/io-ts runtime validation layer in `web/src/`.

Existing frontend-side validation is lightweight and UI-oriented:

- Required form fields and file accept attributes in `ProductCreatePage.tsx`.
- Config input types/min/max from backend-provided `ConfigItem` metadata in `SettingsPage.tsx`.
- Allowed image size options derived from `/api/settings` in `ImageChatPage.tsx`.

Do not add a validation library unless a feature truly needs client-side runtime parsing beyond backend errors.

---

## Handling Unknown Data

Use `unknown`, not `any`, for flexible payloads. `CreativeBriefSummary.payload` in `web/src/lib/types.ts` allows known
optional fields and `[key: string]: unknown` for provider-specific additions.

When narrowing errors, follow current patterns:

```ts
if (mutationError instanceof ApiError) {
  setError(mutationError.detail);
  return;
}
setError(mutationError instanceof Error ? mutationError.message : "创建商品失败");
```

---

## Avoid

- `any` in API types, component props, or mutation payloads.
- Duplicating DTO interfaces inside pages instead of importing from `web/src/lib/types.ts`.
- Renaming API fields to camelCase only on the frontend.
- Type assertions that hide missing null checks; prefer `enabled: Boolean(id)` for queries and explicit null rendering.
- Adding new backend response fields without updating `web/src/lib/types.ts` and the relevant UI.
