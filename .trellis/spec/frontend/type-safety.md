# Frontend Type Safety

## Baseline

The Web app uses strict TypeScript. The production build is the type gate:

```bash
pnpm --dir web build
```

This runs application and Node tsconfig checks before Vite.

Do not use `any`, unchecked casts, or duplicate local DTOs to bypass a backend contract mismatch.

## DTO Ownership

Shared HTTP DTOs live in `web/src/lib/types.ts`. HTTP methods live in `web/src/lib/api.ts`.

Use:

- `interface` for object DTOs;
- string unions for finite backend enums;
- explicit `null` when the backend can return JSON null;
- optional properties only when the wire field may be omitted;
- opaque strings for ids and cursors.

Page-local UI types belong near the page when they are projections or interaction state, not API contracts.

## Current Product DTOs

`ProductSummary` mirrors the V2 list projection:

- id, name, category, price, timestamps;
- automatic cover asset id, filename, and URLs.

It does not carry workflow state, prompt output, or an embedded image list.

Canonical image DTOs:

- `ProductImageAsset`
- `GalleryAsset`
- `GalleryAssetPage`
- `GalleryBootstrap`
- folder and directory mutation responses

Current unions:

```ts
type MediaVerificationStatus = "verified" | "legacy_pending" | "missing";
type ProductImageOriginType =
  | "upload"
  | "workflow_generation"
  | "image_session_attach"
  | "legacy_import";
```

Keep `next_cursor` opaque. Do not parse cursor JSON in the browser. `legacy_pending` and `legacy_import` are migration/archive values; they do not add an online V1 generation path.

Legacy archive DTOs use bounded pages and read-only detail/export/rebuild actions. Archive payloads are opaque historical evidence and must not be converted in the browser into V2 nodes or editable workflow state.

The shared client contract is:

```ts
api.listLegacyArchives({ kind?, product_id?, q?, after?, limit? }): Promise<LegacyArchivePage>
api.getLegacyArchive(kind, archiveId): Promise<LegacyArchiveDetail>
api.downloadLegacyArchive(kind, archiveId): Promise<Blob>
api.createLegacyArchiveAgentRebuild(kind, archiveId, { target_product_id, idempotency_key }): Promise<LegacyArchiveAgentRebuildResult>
```

`after` remains an opaque bounded cursor. Rebuild is the only archive mutation and returns an ordinary `WorkflowDraft`/`AgentConversation`; the browser never applies a legacy payload as a workflow graph.

## Agent Creation DTOs

The versioned intake contracts include:

- image-type option key/label and limits;
- selected image types with explicit quantity/order;
- one-to-six uploaded assets;
- workspace Product, WorkflowDraft, and AgentConversation snapshot.

The `V1` suffix on selected payload interfaces denotes the current wire schema version. It does not authorize a second product-workflow runtime.

Use discriminated/finite unions for image-type keys and Turn states. Preserve unknown Turn status only where the backend union includes `unknown`.

## WorkflowDraft DTOs

WorkflowDraft payload types mirror:

- product facts, source, status, and conflicts;
- reference bindings;
- visual-system payload and explicit overrides;
- image-type and per-image prompt plans;
- GenerationSpec and DeliverySpec;
- folder, node, and edge plans;
- recipe seed;
- revision and lifecycle state.

Keep the payload's `schema_version` field explicit. Validate JSON-like nested data before rendering controls; do not assume server-persisted JSON is shape-safe merely because TypeScript compiled.

## Workflow V2 DTOs

Current node union:

```ts
type WorkflowNodeTypeV2 =
  | "product_context"
  | "reference_image"
  | "prompt_generation"
  | "image_generation";
```

Use the existing interfaces for:

- ProductWorkflowV2;
- WorkflowFolderV2;
- WorkflowNodeV2 / WorkflowNodeDetailV2;
- WorkflowEdgeV2;
- prompt artifact versions;
- workflow/node runs;
- reveal events;
- recipe source/application results.

Do not define a generic node config index signature when a node-specific editor can parse a typed shape.

## GenerationSpec

`WorkflowGenerationSpec` is parsed by `generationSpec.ts`.

Required invariants:

- known aspect ratio and resolution tier;
- known quality/reference/background intent;
- known text policy;
- required text has a nonblank language;
- no-text policy has no language;
- numeric/dimension values stay in current bounds.

UI drafts may temporarily be incomplete. Normalize and validate before mutation.

## Provider Types

Current provider purpose:

```ts
type ProviderPurpose = "prompt" | "image" | "agent";
```

Provider capability describes interface ability, not purpose. `text_responses` remains a valid provider capability for prompt/Agent models.

Settings DTOs cover:

- redacted ProviderProfile;
- ProviderBinding and update requests;
- runtime config definitions/values;
- current versioned import/export/preview contract.

Never add secret response fields to frontend types. A blank secret update means “keep existing” only when the backend endpoint documents it.

## Image Session Types

`ImageToolOptions` contains current advanced options only:

- model;
- quality;
- output format/compression;
- background;
- moderation;
- action;
- input fidelity;
- partial images.

`generation_count` is a separate request/task field. Do not add a second count field inside ImageToolOptions.

ImageSession rounds and tasks distinguish requested size, actual size, candidate index/count, provider notes, status, and safe error.

## Runtime Parsing

TypeScript annotations do not validate external JSON. Use existing parsers for:

- generation specs;
- canvas viewport/local-storage state;
- route/search params;
- recipe sources;
- workflow reveal events;
- settings import preview;
- API error details.

A parser should return a typed value or a clear invalid result. Avoid silently fabricating required fields.

Browser-local persistence can be discarded when its schema is invalid. Do not carry compatibility branches for obsolete local records unless the product explicitly promises them.

## API Client Rules

- Every API method has a typed input and output.
- Build query strings with `URLSearchParams`.
- Encode path ids with `encodeURIComponent`.
- Multipart methods keep structured JSON in a named form field and images as File parts.
- Mutation idempotency keys are explicit arguments.
- The shared request helper parses `ApiError` consistently.
- Do not fetch retired routes as fallback after a 404.

## UI Projections

Derived UI models should be pure functions:

- product list row/card;
- graph nodes/edges;
- inspector draft;
- library selection target;
- run timeline;
- Agent event reducer state.

Keep source DTO identity available so a projection can issue a correctly scoped mutation.

## Unknown and Nullable Data

- Render an explicit empty state for valid empty collections.
- Render a recoverable error for invalid required payloads.
- Preserve `unknown` Agent state and show refresh/recovery controls.
- Do not use `||` when zero/empty string is a valid value; use nullish checks.
- Narrow `unknown` error values with `instanceof ApiError` or shared helpers.

## Tests Required

- API route/method/body assertions after DTO changes.
- Parser tests for valid, boundary, and invalid data.
- Settings purpose/capability and import/export tests.
- Workflow graph and inspector draft tests.
- Agent event reducer and reconnect tests.
- Image-session tool option/count tests.
- Product-list and image-library projection tests.

## Avoid

- `any` or double casts.
- Duplicate DTOs in components.
- Optional fields used to hide backend drift.
- Browser conversion between separate workflow models.
- Enum values that do not exist in `domain/enums.py`.
- Secret-bearing response types.
- Parsing opaque cursor or id values.
- Keeping an unused API type after its method/page is deleted.
