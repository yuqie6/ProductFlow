# Backend Database Guidelines

## Scope

Read this guide before changing SQLAlchemy models, application transactions, runtime settings, provider profiles, migrations, gallery persistence, WorkflowDraft lineage, Agent projections, or asynchronous job state.

## Declarative Model Patterns

- Models inherit `Base` and use `Mapped[...]` / `mapped_column(...)`.
- UUID-like ids use the repository's string id helper.
- Mutable business rows normally use `TimestampMixin`.
- Immutable version/event rows carry explicit creation time and no general update path.
- Named foreign keys, unique constraints, indexes, and check constraints are preferred when they express a business invariant.
- Native PostgreSQL enums mirror current domain enums; SQLite tests must still exercise equivalent constraints.

Do not add a table when an existing versioned object, JSON contract, or relationship already owns the data.

## Current Aggregate Map

### Configuration

- `AppSetting`
- `ProviderProfile`
- `ProviderBinding`

### Product and Media

- `Product`
- `MediaObject`
- `ProductImageAsset`
- `ProductAssetFolder`

### Agent and Draft

- `AgentConversation`
- `AgentTurnProjection`
- `AgentToolMutation`
- `WorkflowDraft`
- `WorkflowDraftRevision`
- `WorkflowDraftRecipeSeed`

### Workflow

- `VisualSystem` / `VisualSystemVersion` / `VisualSystemVersionReference`
- `ProductFactSetVersion`
- `ProductWorkflow` / `WorkflowFolder` / `WorkflowNode` / `WorkflowEdge`
- `WorkflowMaterialization` / `WorkflowMaterializationKey` / `WorkflowRevealEvent`
- `WorkflowRun` / `WorkflowNodeRun`
- `ImagePromptArtifact` / `ImagePromptArtifactVersion` / `ImagePromptArtifactVersionReference`
- `VisualException`
- `WorkflowImageGenerationRecord` / `WorkflowImageGenerationReference`
- `WorkflowRecipe` / `WorkflowRecipeVersion`
- `DeliveryRenditionJob`

### Image Sessions

- `ImageSession`
- `ImageSessionAsset`
- `ImageSessionRound`
- `ImageSessionGenerationTask`
- `ImageGalleryEntry`

SQLAlchemy metadata describes only these current online concepts. Historical migration tables must not be reintroduced into runtime models.

## Session Ownership

The caller that creates a Session owns commit/rollback/close.

- FastAPI's request dependency owns request Session lifetime.
- Worker entrypoints create and close their own Session.
- Application services may flush but do not close a borrowed Session.
- A use case with one business mutation commits once at its owning boundary.
- Helper functions do not hide independent commits.
- External provider or storage work is ordered deliberately around database state and compensation.

After a database error, explicitly roll back before reusing the Session. Do not rely on a broad catch-and-continue fallback in the same failed transaction.

## Query Patterns

- Use SQLAlchemy `select(...)` and typed scalar projections.
- Scope child lookup by owning product/workflow/conversation, not only by globally unique id.
- Use eager loading when response serialization traverses known relationships.
- Keep count and item filters identical.
- Use deterministic ordering with an id tie-breaker.
- Large lists use bounded cursor pagination.
- Do not load media bytes or unbounded JSON collections during list projection.

## Product List Projection

The product list is a dedicated projection, not a Product-detail serialization.

It returns current product identity, timestamps, and automatic cover metadata:

- `cover_image_asset_id`
- `cover_image_filename`
- download, preview, and thumbnail URLs

Search, sorting, pagination, and count are database-backed. The list does not derive workflow execution state or inspect unrelated asset collections per row.

## Canonical Media Ownership

`MediaObject` is the authority for stored bytes:

- storage path;
- MIME type;
- byte size;
- width/height;
- hash;
- verification state.

`ProductImageAsset` is the authority for product-scoped identity:

- product id;
- media object id;
- origin type;
- display/original name;
- user folder;
- parent image;
- image-type classification;
- image-session source relation when applicable.

Current online origins are `upload`, `workflow_generation`, and `image_session_attach`. The migration-only `legacy_import` origin remains readable for canonical assets created from retained V1 lineage; it is not a new online generation path.

Rules:

- Every ProductImageAsset has one MediaObject.
- Every ImageSessionAsset has one MediaObject.
- Workflow nodes store ProductImageAsset ids, never storage paths.
- A MediaObject may have multiple business references only where the model explicitly allows shared ownership.
- Deletion checks cover node bindings, cover, derivations, gallery entries, and delivery jobs before bytes are removed.
- Database rollback after a new storage write removes the newly written file through compensation.

## Product Cover

Product cover is display metadata:

- creation automatically selects an initial uploaded asset;
- list projection reads only the current cover relation;
- explicit cover endpoints validate product ownership;
- clearing a cover does not delete the image;
- cover has no implicit workflow-reference behavior.

## WorkflowDraft Revisions

WorkflowDraft is a mutable lifecycle row with append-only WorkflowDraftRevision children.

- `current_revision_id` points to the latest accepted revision.
- revision numbers are monotonic within one Draft.
- each revision stores a validated versioned payload and content hash;
- confirmation records an explicit revision;
- materialization records the Draft/revision source;
- repeated idempotency keys return the existing result only for the same request hash.

Draft, revision, confirmation state, materialization rows, graph rows, and reveal events are committed in the owning application transaction.

## Immutable Workflow Lineage

Version tables and execution records must preserve what a prior generation consumed:

- ProductFactSetVersion
- VisualSystemVersion and references
- ImagePromptArtifactVersion and references
- WorkflowImageGenerationRecord and references
- WorkflowRun and WorkflowNodeRun
- WorkflowRecipeVersion

Editing current configuration creates a new version or later run. It does not rewrite previous lineage.

## V2 Workflow Persistence

- ProductWorkflow and WorkflowNode schema version are fixed at 2.
- One product may have revisions according to the current uniqueness constraints.
- Node keys are unique where the materialization contract requires them.
- Edges reference nodes in the same workflow.
- Folder membership is workflow-scoped.
- layout mutations advance the workflow edit version.
- reference bindings point to ProductImageAsset.

Keep graph integrity checks in the application/domain layer and reinforce stable invariants with database constraints.

## Agent Projection

ProductFlow stores a projection of durable Agent state:

- AgentConversation is product and Draft scoped.
- AgentTurnProjection stores idempotency key, request hash, harness ids, input asset ids, projected status, output, questions, sequence, and artifact linkage.
- AgentToolMutation stores prepared payload, request hash, status, result, and reconciliation fields.

The Go service is authoritative for durable Turn execution. PostgreSQL is authoritative for ProductFlow business objects. Synchronization must preserve `unknown` when a side effect or Turn result cannot be proven.

Agent mutation rows are idempotent by tool name and idempotency key. A repeated key with different prepared content is a conflict.

## Recipes

WorkflowRecipe is the user-owned reusable identity. WorkflowRecipeVersion is immutable.

- kind is full workflow or fragment;
- source is workflow, folder, or selection;
- payload stores reusable structure only;
- apply operations map source ids to new workflow ids in one transaction;
- archive hides a recipe without mutating workflows already created from it.

## Image Sessions

An ImageSession owns:

- reference and generated ImageSessionAsset rows;
- ordered ImageSessionRound rows;
- ImageSessionGenerationTask rows.

Each generation task stores:

- prompt and requested size;
- generation_count;
- selected base/reference ids;
- filtered tool options;
- queued/running/terminal state;
- safe provider metadata and errors.

One task may create multiple rounds/candidates. Candidate ordering and count are explicit. Branching references a completed asset. The request context is bounded to six images including the branch base.

## Gallery

ImageGalleryEntry is an explicit user collection of an ImageSessionAsset. It retains source session/product metadata and download access. It does not duplicate image bytes.

Saving an image-session result to a Product creates a ProductImageAsset with origin `image_session_attach`. Gallery collection and product attachment are separate user actions.

## Delivery Renditions

DeliveryRenditionJob references one ProductImageAsset and persists a validated DeliverySpec, status, output MediaObject/path metadata, and failure details.

- equivalent completed requests may reuse a deterministic result;
- running/failed/retried jobs remain observable;
- source image is immutable;
- output creation uses storage compensation on transaction failure.

## Runtime Settings

`config.py` defines the registry, defaults, parsing, and validation. `application/runtime_settings.py` composes environment defaults with database overrides. `infrastructure/runtime_config_store.py` owns the database read.

Environment-only settings:

- database and Redis URLs;
- storage root;
- session/admin/settings secrets;
- Agent service internal token and bootstrap address.

Database-overridable settings include upload limits, image-tool fields, maximum generation dimension, concurrency, security switches, and prompt customization.

Avoid importing database/session code from `config.py`.

## Provider Profiles and Bindings

ProviderProfile stores provider type, endpoint, encrypted/private API key field, capabilities, default models, config, and enabled/archive state.

ProviderBinding purpose is one of:

- `prompt`
- `agent`
- `image`

Rules:

- one active binding per purpose;
- selected profile must be enabled and advertise the required capability;
- interface/model settings are validated before commit;
- public settings responses redact secrets;
- Agent runtime secret is exposed only through the internal-token-protected endpoint;
- missing binding fails clearly and does not fall back to unrelated settings.

## Image Tool Fields

Allowed advanced fields are defined once by `IMAGE_TOOL_FIELD_KEYS`. The database stores a normalized ordered subset.

Current fields:

- model
- quality
- output_format
- output_compression
- background
- moderation
- action
- input_fidelity
- partial_images

Unknown fields fail settings validation. Candidate count is stored in the business request's quantity/generation_count and mapped internally by provider adapters.

## Settings Import and Export

Import/export describes only the current versioned settings contract.

- strict schema version and compatibility marker;
- provider profiles with redacted/missing secret handling;
- prompt/agent/image bindings;
- current runtime settings;
- preview before apply;
- one transaction for apply.

Do not add in-request conversion of prior configuration shapes. Deployment-time database cleanup belongs in Alembic.

## Migrations

- Every schema change has an Alembic revision and focused regression test.
- Fresh-database `upgrade head` must pass.
- SQLite migration tests cover schema and data transforms where practical.
- PostgreSQL-specific enum or transaction behavior needs a live validation.
- Destructive cleanup names the removed objects and has no misleading downgrade.
- The 0042 legacy cutover revision is a non-destructive evidence boundary. Any later cleanup must require the persisted gate's source/archive/canonical hashes, backup-restore timestamp, and zero active or unknown legacy runs.
- Historical Alembic files stay unchanged unless the task explicitly repairs a broken historical revision.
- Runtime models and migrations at head must agree on nullability, enums, indexes, and constraints.

## Scenario: Legacy Cutover Evidence Gate

### 1. Scope / Trigger

This contract applies when a deployment retains V1 source tables while the online runtime has moved to Agent/WorkflowDraft/schema-v2. Revision `20260816_0042` installs the evidence table and must not perform semantic migration or deletion.

### 2. Signatures

- `approve_legacy_cutover_gate(connection, *, source_profile, source_report_sha256, archive_report_sha256, canonical_report_sha256, backup_restore_verified_at)` updates the singleton gate inside the caller-owned transaction.
- `assert_legacy_cutover_cleanup_ready(connection)` is mandatory at the start of any future destructive cleanup transaction.
- `mark_legacy_cutover_cleaned(connection)` only records completion after the separate cleanup transaction succeeds.
- `legacy_cutover_gates` stores `phase`, three report hashes, `backup_restore_verified_at`, and `active_run_count`.

### 3. Contracts

- Hashes are lowercase 64-character SHA-256 values; `source_profile` is the recognized schema profile.
- Approval requires all three report hashes, a timezone-aware backup/restore verification timestamp, and zero active or unknown V1 workflow/node/Canvas Agent execution rows.
- Phases are `pending`, `ready_for_cleanup`, and `cleaned`; cleanup may proceed only in `ready_for_cleanup` with complete evidence and `active_run_count = 0`.
- The command boundary is `manage_legacy_cutover_gate status|approve|assert-cleanup-ready`; report payloads, storage paths, media bytes, and secrets are never stored in the gate.

### 4. Validation & Error Matrix

- Missing gate row or unsupported evidence -> `BusinessValidationError` and no update.
- Invalid hash, blank profile, or timezone-less backup timestamp -> `BusinessValidationError`.
- Active/unknown retained execution -> `ConflictError`; gate remains `pending`.
- Pending, incomplete, or cleaned gate at cleanup -> `ConflictError`; no destructive statement is allowed.
- Re-approving a `cleaned` gate -> `ConflictError`; evidence is retained.

### 5. Good/Base/Bad Cases

- Good: audit/archive/canonical/restore evidence is complete and the active-run query returns zero; one transaction moves the singleton to `ready_for_cleanup`.
- Base: a fresh 0042 database has a `pending` singleton and all retained source/archive rows remain readable.
- Bad: a report hash is missing, an execution status is unknown, or a backup was not restored; approval and cleanup stop without deleting data.

### 6. Tests Required

- Migration test: fresh and upgraded databases retain source/archive tables and create a pending singleton.
- Gate unit/integration tests: pending rejection, active/unknown run rejection, evidence persistence, hash/timezone validation, cleanup completion, and no reopen after `cleaned`.
- Audit/backfill tests: stable report hashes, page hash binding, idempotent archive creation, and drift blocking.

### 7. Wrong vs Correct

#### Wrong

```python
op.drop_table("source_assets")
```

#### Correct

```python
assert_legacy_cutover_cleanup_ready(connection)
# A separately reviewed cleanup transaction may delete only its approved objects.
```

## Storage Compensation

When a use case writes bytes and database rows:

1. Validate input.
2. Write to a new deterministic/unique storage path.
3. Add/flush database rows.
4. Commit at the owning boundary.
5. On database failure, remove only bytes created by this attempt.

Never delete a pre-existing shared file as generic rollback cleanup.

## Tests Required

Depending on scope:

- model constraint and relationship tests;
- use-case transaction/rollback tests;
- migration fresh-upgrade and data-cleanup tests;
- cursor pagination and projection query tests;
- media ownership and storage compensation tests;
- Draft/version/idempotency tests;
- Agent projection/reconciliation tests;
- provider settings redaction and binding tests;
- image-session candidate/branching tests;
- PostgreSQL live tests for dialect-sensitive behavior.

## Forbidden Patterns

- Runtime branches that read retired table shapes.
- Hidden commit inside a borrowed-session helper.
- Querying a child object without parent scope.
- Persisting API keys, data URLs, or media bytes in JSON history.
- Reusing product cover as an implicit node reference.
- Updating immutable lineage rows.
- Separate provider settings outside ProviderProfile/ProviderBinding.
- A second workflow-reuse table beside WorkflowRecipe.
- Silent truncation of media or Agent multimodal results.
