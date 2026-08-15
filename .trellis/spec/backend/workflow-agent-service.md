# Workflow Agent Service

> Executable contracts for the scoped Go agent service, FastAPI projection layer, internal tools, and durable Turn sync.

## Ownership

- `agent-service/` owns the Go process, the fixed agent-harness source snapshot, conversation-scoped harness services,
  SQLite journals, native multimodal provider requests, and the internal Turn control plane.
- `application/agent_conversations.py` owns PostgreSQL conversation and Turn projection state.
- `application/agent_control.py` owns start, answer, cancel, resume, and harness-state projection orchestration.
- `application/agent_sync.py` owns browser-independent polling, restart recovery, and required-artifact attachment.
- `application/agent_tools.py` owns scope-bound product context, gallery metadata/content reads, and reconcilable
  folder/asset organization changes.
- `application/legacy_archive_rebuilds.py` owns immutable archive-seed context plus bounded archive list/inspect reads.
- `infrastructure/agent_service.py` owns the FastAPI-to-Go HTTP/SSE client.
- `presentation/routes/agent_conversations.py` exposes session-authenticated browser APIs.
- `presentation/routes/agent_internal.py` exposes bearer-authenticated ProductFlow tool APIs to the Go service.
- `presentation/routes/agent_runtime.py` exposes the complete Agent provider configuration only to the authenticated Go
  service; browser settings APIs continue to return redacted provider profiles.

The service is single-instance and single-merchant. Multi-instance journal coordination, workflow materialization, and
the frontend Agent conversation are separate tasks.

## Service And Scope Contract

Each `AgentConversation` owns one isolated harness service and disk directory:

```text
${AGENT_DATA_ROOT}/conversations/{conversation_uuid}/
  scope.json
  agent.db
  workspace/
```

- The outer Go route accepts a ProductFlow conversation UUID. It obtains product, Draft, and harness run IDs from the
  internal ProductFlow contract and persists them atomically in `scope.json`.
- Reopening an existing journal requires an exact scope match. Product, Draft, conversation, or run drift is a conflict.
- The model never receives product, conversation, Draft, or run IDs as writable tool parameters.
- Built-in file tools can only access the empty conversation workspace.
- `agent-service/third_party/agent-harness/SOURCE.md` records the immutable upstream commit. Release builds use the
  repository snapshot and must not reference a sibling checkout.

## HTTP Boundaries

Browser routes use the existing admin session dependency:

- `POST /api/v2/products/{product_id}/agent-conversations`
- `GET /api/v2/products/{product_id}/agent-conversations/{conversation_id}`
- `GET|POST /api/v2/products/{product_id}/agent-conversations/{conversation_id}/turns...`
- `GET .../turns/{projection_id}/events`

The browser never contacts the Go service. FastAPI validates `(product_id, conversation_id, projection_id)` before
calling its internal client.

Go and ProductFlow internal routes use `AGENT_SERVICE_INTERNAL_TOKEN` with constant-time bearer comparison. The token is
separate from browser sessions, admin credentials, and provider credentials. The Go service has no host port in Compose.

The Agent provider is a first-class `agent` purpose binding over an enabled OpenAI-compatible profile with
`text_responses` capability and a non-empty API key. The binding owns the model plus optional reasoning effort, reasoning
summary, text verbosity, and service tier. Existing databases add the missing binding by copying the text binding and its
brief-model fallback; no second credential store is created. Settings export schema v2 includes this binding, while v1
imports derive it from the required text binding.

When opening an uncached conversation, Go validates the ProductFlow conversation contract, fetches
`GET /api/internal/v1/agent-runtime/provider-config`, and passes that immutable snapshot to `agenttask.OpenService`.
Repeated requests for the same conversation reuse its existing service and provider snapshot. A newly opened conversation
fetches current settings. Missing, disabled, incomplete, or capability-incompatible Agent configuration fails before any
harness journal or workspace is created. `AGENT_PROVIDER_*` variables are reserved for the opt-in provider test and are
not runtime service configuration.

SSE preserves harness event bytes and sequence IDs. FastAPI forwards the greater valid cursor from `after` and
`Last-Event-ID`, disables proxy buffering, and closes its upstream stream when the browser disconnects. PostgreSQL does
not store token deltas; reconnect replay comes from the harness SQLite journal.

## PostgreSQL Projection

`AgentConversation` binds one product, one WorkflowDraft, and one harness run. A Draft has at most one conversation.

`AgentTurnProjection` stores bounded UI and recovery state:

- input text and ordered asset IDs;
- caller idempotency key and canonical request hash;
- harness Turn ID and exact harness status;
- bounded final output, safe error, Question snapshot, and `resume_required`;
- required artifact name/step and the attached `WorkflowDraftRevision` ID;
- sync error and timestamps.

It does not store the harness transcript, reasoning items, image bytes, image URLs, data URLs, or the artifact body.
`AgentToolMutation` stores the idempotency ledger for reconcilable ProductFlow mutations. Its generic `prepared_json`
binds operation, scope, expected-before state, and target state; rename compatibility columns remain nullable for old
rows and current rename diagnostics.

Migration changes must preserve native PostgreSQL enums, SQLite test compatibility, product/Draft cascade behavior,
artifact revision `SET NULL`, and complete enum removal on downgrade.

## Turn Lifecycle

- FastAPI persists or reuses an `AgentTurnProjection` before calling Go. The projection ID is the Go start
  idempotency key, so an ambiguous HTTP result can be retried without creating another harness Turn.
- A Turn accepts trimmed text and at most six distinct, verified current-product asset IDs.
- Go fetches selected images through the scoped ProductFlow binary endpoint and creates native `input_image` parts with
  embedded checkpoint mode. ToolResult and TurnInput limits from agent-harness remain authoritative.
- The harness automatically inherits the latest trustworthy transcript in the same run. ProductFlow does not rebuild or
  append transcript history.
- An answered Question returns to `queued` but requires explicit `resume`. ProductFlow sets `resume_required=true`, does
  not poll that Turn, and clears the flag after cancel or resume.
- `unknown` is projected unchanged. ProductFlow must not infer success or automatically replay an ambiguous effect.
- API and worker startup both enqueue recoverable projection rows. Duplicate sync messages converge through projection,
  harness start, artifact-origin, and tool-mutation idempotency contracts.
- Reading an active `queued`, `running`, or `cancel_requested` projection without a persisted sync error refreshes it from
  the scoped Agent service before returning. An `awaiting_confirmation` projection without a revision ID is also refreshed.
  This closes the SSE
  terminal-event race so the terminal response includes its attached revision instead of leaving the browser on an
  incomplete local projection.

## Required Workflow Artifact

The first completed workflow-design Turn and every Turn that changes the proposed draft must call strict
`propose_workflow_draft` with the current `WorkflowDraftPayloadV1` JSON Schema. A prose-only completion before any
accepted artifact fails the harness gate. Once the trusted run transcript contains an accepted artifact, a follow-up that
only explains or answers questions about that draft may complete without creating a duplicate revision. The conversation
remains `awaiting_confirmation` while its current WorkflowDraft is still unconfirmed. If a follow-up submits an artifact,
that artifact still passes the complete local and application validation path below.

`WorkflowDraftPayloadV1.model_json_schema()` is the domain/validation schema and is not sent to a strict provider
unchanged. `workflow_draft_tool_schema()` derives the provider boundary schema by making every object property required,
setting `additionalProperties=false`, representing optional values through nullable unions, converting `oneOf` to
supported `anyOf`, and removing unsupported `default`, `deprecated`, and `discriminator` annotations. The unconstrained
Pydantic `JsonValue` definition is narrowed to recursive scalar/list/null facts because a strict open object is not a
portable tool parameter. This conversion must not change Pydantic defaults, persisted Draft payloads, or confirmation
semantics.

Provider JSON Schema acceptance is necessary but cannot express all Draft invariants. The harness `RequiredArtifact`
runs an optional, repeatable, side-effect-free application validator after local schema validation. ProductFlow binds it
to `POST /api/internal/v1/agent-conversations/{conversation_id}/workflow-draft/validate`, which parses the complete
payload, checks model validators, missing/conflicted facts, current-product reference ownership, and verified media.
Application rejection becomes the native tool error returned to the model, so the same Turn can submit a corrected
artifact. Rejected values do not produce `awaiting_confirmation` and do not append a Draft revision.

When a Turn reaches `awaiting_confirmation`, the sync path calls `append_workflow_draft_revision(...)` with harness Turn
ID and artifact step ID. The existing `(draft_id, source_turn_id, source_artifact_step_id)` uniqueness contract makes
replay idempotent. Only the resulting revision ID is stored on the Turn projection. Confirming the Draft marks an
associated Agent conversation `completed`. Artifact attachment parses and validates the value again before persistence;
this closes callback bypass, stale-reference, and projection-race paths without adding a second acceptance policy.

## ProductFlow Tool Gateway

Read tools are bound to the conversation closure and expose no scope IDs in their schemas:

- `get_product_workflow_context_v1`
- `list_product_image_assets_v2`
- `inspect_product_image_assets_v1`
- `list_legacy_archives_v1`
- `inspect_legacy_archive_v1`

Asset listing accepts the product-gallery directory, search, sort, and cursor contract and returns at most 100 metadata
rows without URLs, storage paths, or bytes. Inspect accepts explicit IDs only, returns at most six images, and revalidates
file byte count, MIME type, dimensions, and SHA-256 against verified media metadata.

The current reconcilable durable tools are:

- `create_product_image_folder_v1`;
- `rename_product_image_folder_v1`;
- `rename_product_image_asset_v1`;
- `move_product_image_assets_v1`.

Prepare captures stable object IDs plus expected-before and target state. Execute uses the harness invocation idempotency
key. Reconcile distinguishes `applied`, `not_applied`, `conflict`, and `unknown` from the generic ProductFlow mutation
ledger and current object state. An applied ledger result remains replayable after a later rename, move, or folder delete.

The archive tools return metadata/selected JSON sections only. They cannot return image bytes or URLs; pixels remain
behind the existing explicit six-image inspect tool. The service has no tools for deleting assets, changing covers,
creating missing brand material, writing individual DAG nodes, or materializing a Draft. It cannot change original
filenames, media bytes, origin, cover relations, reference bindings, or generation lineage through gallery organization
tools.

## Tool Catalog V3 Cutover

- The ProductFlow contract wire remains schema version 1 and declares `tool_contract_version=3`. The Go manager checks
  both values before opening a conversation service. There is no catalog-version switch inside one process.
- Existing completed Turns remain durable transcript history. A new Turn opens against the current v2 catalog. A pending
  old Turn whose execution envelope contains the old catalog fails with an explicit durable contract-drift conflict.
- Release freezes browser/backend/worker ingress before the cutover check, then cross-checks every nonterminal projected
  Turn with the old Agent service. `queued`, `running`, `requires_input`, `cancel_requested`, and `unknown` block release.
- `awaiting_confirmation` is safe only when its WorkflowDraft artifact revision is already attached. A missing harness
  Turn ID, an unreachable Agent service, or an unrecognized harness status blocks release.
- The cutover gate runs from the candidate backend image while the old PostgreSQL and Agent service remain available.
  Failure restores frozen ingress; success deploys backend and Agent service from the same repository version.

### Scenario: Strict tools with no arguments

#### 1. Scope / Trigger

- Applies when a ProductFlow or harness tool is strict and accepts no arguments, such as
  `get_product_workflow_context_v1`.

#### 2. Signatures

- The JSON Schema `parameters` object is
  `{"type":"object","properties":{},"required":[],"additionalProperties":false}`.

#### 3. Contracts

- `properties` and `required` remain explicit empty containers; omitting either is not an equivalent wire contract for
  all OpenAI-compatible Responses providers.
- The handler continues to receive and validate an empty JSON object. This schema requirement does not add optional or
  placeholder model arguments.

#### 4. Validation & Error Matrix

- Complete strict empty-object schema -> provider accepts the tool declaration.
- Missing explicit `properties` or `required` -> a compatible gateway may reject the complete Responses request before
  model execution; some gateways surface that upstream validation failure as HTTP 502.
- Non-empty tool arguments -> ProductFlow strict decoding rejects the call.

#### 5. Good/Base/Bad Cases

- Good: a no-argument read tool declares both empty containers and sets `additionalProperties=false`.
- Base: a tool with real arguments declares its normal properties and required field list.
- Bad: a strict no-argument tool declares only `type=object` and `additionalProperties=false`.

#### 6. Tests Required

- Unit-test the registered tool schema, including strict mode and all four empty-object fields.
- Run the opt-in live two-Turn provider gate when changing tool schemas or provider wiring; it must use the full tool set,
  not a reduced probe set.

#### 7. Wrong vs Correct

Wrong:

```go
map[string]any{"type": "object", "additionalProperties": false}
```

Correct:

```go
map[string]any{
    "type": "object", "properties": map[string]any{},
    "required": []string{}, "additionalProperties": false,
}
```

## Scenario: Recipe-seeded version-zero Agent Drafts

### 1. Scope / Trigger

- Trigger: reading Agent context for a Draft created by workflow-recipe application or attaching the first required
  `propose_workflow_draft` artifact to a Draft with no current revision.

### 2. Signatures

- `get_agent_contract(...)` returns `current_draft_version=0` when `WorkflowDraft.current_revision_id` is null.
- `get_product_workflow_context_v1` returns `workflow_draft.payload=null` plus `workflow_recipe_seed` containing the fixed
  recipe version, strict payload, preferred visual version ID, and optional base workflow structure.
- `append_workflow_draft_revision(..., expected_draft_version=0)` and artifact synchronization create version 1 only while
  the Draft still has no current revision.

### 3. Contracts

- A recipe is structural guidance. The Agent must re-evaluate target-product facts, inspect explicitly selected reference
  assets as needed, write target-specific prompt content, and select or create an appropriate VisualSystem before
  proposing a complete Draft.
- Recipe application itself persists no placeholder revision and no partial DAG. Confirmation and materialization remain
  unavailable until a strict version-1 artifact exists.
- Fragment context includes the exact materialized base workflow ID/revision recorded in the seed and a bounded graph
  summary. If that lineage is missing, belongs to another product, or is not schema-v2, context loading fails explicitly.
- Product facts, Draft payload, recipe seed, and optional base graph share the ProductFlow
  `AGENT_CONTEXT_MAX_BYTES=512 KiB` output budget. The tool returns an explicit conflict instead of truncating JSON.

### 4. Validation & Error Matrix

- Recipe payload schema/hash drift -> context tool conflict; the Agent never receives unverified seed data.
- First artifact uses expected version other than 0, or another writer already created version 1 -> optimistic conflict.
- Seed base workflow is missing/cross-product/schema-v1 -> conflict requiring application state repair or a fresh apply.
- Combined context exceeds 512 KiB -> explicit bounded-output conflict; no silent field removal.

### 5. Good/Base/Bad Cases

- Good: apply a full recipe to another product, read version-zero context, ask for missing facts, and attach one complete
  target-specific version-1 artifact.
- Base: apply a fragment to a product with an active schema-v2 workflow; the Agent receives the fixed base revision and
  decides how the fragment integrates.
- Bad: submit the recipe payload itself as a WorkflowDraft or fabricate asset IDs for missing Logo/certification inputs.

### 6. Tests Required

- Assert the contract and context expose version 0, null Draft payload, verified recipe seed, and optional base workflow.
- Assert artifact replay is idempotent and a second expected-version-zero write conflicts after version 1 exists.
- Complete the cross-product path through confirmation and materialization, proving apply alone created no workflow.

### 7. Wrong vs Correct

Wrong:

```python
draft = create_workflow_draft(payload=recipe_version.payload_json)
```

Correct:

```python
draft = WorkflowDraft(product_id=target_product_id, status=WorkflowDraftStatus.COLLECTING)
seed = WorkflowDraftRecipeSeed(workflow_draft_id=draft.id, recipe_version_id=recipe_version.id, ...)
```

The Agent supplies the first complete product-specific artifact after reading the immutable seed.

## Scenario: Legacy-archive-seeded version-zero Agent Drafts

### 1. Scope / Trigger

- Trigger: creating a WorkflowDraft from an immutable workflow, Canvas Agent, or user-template archive; exposing archive
  lineage in Agent context; or changing the archive list/inspect tools.

### 2. Signatures

- `POST /api/v2/legacy-archives/{kind}/{archive_id}/agent-rebuilds` accepts
  `{target_product_id, idempotency_key}` and returns `{created, archive_kind, archive_id, target_product_id, draft,
  conversation}`.
- `get_product_workflow_context_v1` returns `legacy_archive_seed` metadata when the Draft has an archive seed.
- `list_legacy_archives_v1` accepts `{kind, query, after, limit}` with limit `1..50`.
- `inspect_legacy_archive_v1` accepts `{kind, archive_id, section, offset, limit}` with limit `1..10`.
- Migration `20260815_0040` creates `workflow_draft_legacy_archive_seeds`.

### 3. Contracts

- A rebuild creates a collecting Draft with version zero, one conversation, and one seed that points to exactly one
  immutable archive. It creates no Draft revision, workflow, node, edge, run, recipe version, or copied archive asset.
- Workflow and Canvas Agent archive reads are restricted to the conversation product. User-template archives are global,
  while the browser must select the target product before creating the scoped conversation.
- Context exposes the archive kind/ID/title/status, source profile, payload hash, counts, and timestamps. Complete archive
  payloads are available only through named inspect sections with explicit pagination.
- List output contains at most 50 metadata rows. Inspect output contains at most 10 section items and 256 KiB of encoded
  JSON. The application returns a conflict when the budget is exceeded; it never silently truncates a section item.
- The initial archive-rebuild Turn has `asset_ids=[]`. If visual evidence is needed, the Agent reads archive asset
  metadata, chooses specific current-product asset IDs, and calls `inspect_product_image_assets_v1` within its six-image
  bound.
- Recipe and archive seeds are mutually exclusive. The first valid `propose_workflow_draft` artifact uses expected Draft
  version zero and follows the existing application validator, confirmation, and materialization path.

### 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Missing archive or target product | browser API `404`; no seed/Draft/conversation |
| Product-bound archive targets a different product | browser API `400`; no write |
| Same product/key reused with a different request hash | browser API `409`; existing rebuild remains authoritative |
| Conversation asks for another product's archive | internal tool `404`; archive existence is not disclosed |
| Unsupported section, negative offset, or limit outside bounds | internal tool `400` |
| Encoded tool result exceeds 256 KiB | internal tool `409`; model must narrow the request |
| Archive seed and recipe seed both exist | Agent context conflict; Turn does not receive ambiguous seed guidance |
| Required artifact is invalid | native tool rejection; no Draft revision is appended and the same Turn may correct it |

### 5. Good / Base / Bad Cases

- Good: the Agent reads summary, selected node/copy pages, asset metadata, and one chosen image before proposing a
  target-specific Draft that the user reviews.
- Base: an archived user template is rebuilt for a different product after explicit target selection; the Agent treats
  old structure as guidance and writes new facts/prompts/references.
- Bad: serialize hundreds of archives, complete run history, or every gallery image into product context.
- Bad: attach archived image IDs to the first Turn or copy the archive payload directly into Draft version 1.

### 6. Tests Required

- API/application tests assert atomic empty-Draft creation, original archive immutability, idempotent retry/conflict,
  target-product scope, global template selection, and latest workbench loading.
- Context/tool tests assert metadata-only seed context, recipe/seed exclusivity, kind-specific sections, cursor/offset
  paging, 50/10/256 KiB limits, asset metadata without URL/bytes, and cross-product denial.
- Go tests assert tool names, strict schemas, internal endpoint paths, request bodies, configured limits, and contract
  version 3.
- A real browser/provider run must show bounded archive inspect calls, explicit image inspection, Question confirmation,
  zero DAG before artifact attachment, and the existing Draft confirmation surface after a valid artifact.

### 7. Wrong vs Correct

Wrong:

```go
context["legacy_archive_payload"] = loadAllArchiveRowsAndImages()
```

Correct:

```go
registerReadTool("list_legacy_archives_v1", boundedListHandler)
registerReadTool("inspect_legacy_archive_v1", boundedSectionHandler)
// The model selects sections and image IDs inside the conversation scope.
```

## Scenario: Draft-first Agent product workspace creation

### 1. Scope / Trigger

- Trigger: changing the public product-creation route, the workspace draft/recovery/finalization API, canonical reference
  upload staging, the pre-intake Turn guard, or AgentConversation intake idempotency columns.
- This scenario owns the formal `/products/new` write contract. Recipe and legacy-archive rebuilds retain their seed-based
  version-zero contracts.

### 2. Signatures

- `POST /api/v2/agent-product-workspaces/drafts`: strict JSON `{name}` plus required `Idempotency-Key`; returns
  `AgentProductWorkspaceSnapshotResponse` with `created=true|false` and `intake_finalized=false`.
- `GET /api/v2/agent-product-workspaces/{conversation_id}`: returns the current workspace snapshot with `created=false`.
- `POST /api/v2/agent-product-workspaces/{conversation_id}/intake`: multipart `selection` plus repeated `images`, with a
  required `Idempotency-Key`; returns the finalized snapshot.
- `POST /api/v2/agent-product-workspaces`: the transitional one-request compatibility endpoint.
- Application boundaries are `create_agent_product_draft_workspace(...)`, `get_agent_product_workspace(...)`, and
  `finalize_agent_product_workspace_intake(...)`. Canonical writes are shared through `stage_canonical_product(...)`,
  `stage_canonical_product_assets(...)`, and `stage_canonical_product_with_assets(...)`.
- Migration `20260815_0041` adds nullable `AgentConversation.intake_idempotency_key: String(200)` and
  `intake_request_hash: String(64)` plus `ck_agent_conversations_intake_idempotency_pair`.

### 3. Contracts

- Draft creation atomically persists one normalized Product, one collecting WorkflowDraft with no intake or revision, and
  one AgentConversation whose harness run ID equals its conversation ID. It creates no media, cover, Turn, workflow, node,
  edge, run, recipe, or template record.
- The creation request hash binds the normalized product name. A replay with the same key and hash returns the same
  aggregate. A replay with changed content conflicts. Draft and transitional composite creation deliberately share one
  global creation-key namespace; clients switching operation shape must generate a new key instead of replaying a key from
  the other endpoint.
- Intake finalization locks the conversation, Draft, and Product. It accepts the existing selection limits and one to six
  verified PNG/JPEG/WEBP uploads, stages canonical media through storage compensation, writes immutable `WorkflowIntakeV1`,
  and persists both intake idempotency fields in the same commit. References are equal inputs; finalization does not assign
  a cover.
- The intake request hash binds structured selection plus each upload's order, filename, declared MIME type, and byte
  SHA-256. Same-key replay returns the original assets; a different key or payload after finalization conflicts.
- A plain empty workspace cannot reserve its first Agent Turn. A Draft with intake, a current revision, a recipe seed, or a
  legacy archive seed remains eligible under its existing lifecycle contract.
- The compatibility composite endpoint reuses the same staging and workspace-record primitives. It must not copy product,
  media validation, storage compensation, Draft, or conversation initialization logic.
- Downgrade to `20260815_0040` is allowed only while every intake idempotency pair is null; populated finalization identity
  blocks destructive downgrade.

### 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Missing/blank/oversized name or unknown JSON field | browser API `422` or business `400`; no workspace write |
| Missing, malformed, or drifted creation idempotency key | `400`/`409`; an existing aggregate remains authoritative |
| Unknown conversation | `404`; no Product or Draft probing through fallback creation |
| Intake has invalid taxonomy/counts or reference count outside `1..6` | `400`; empty Draft remains recoverable |
| Upload bytes, declared MIME, or measured image metadata disagree | `400`; DB rollback and storage compensation |
| Finalization after Turn/revision/seed/state transition | `409`; no new media or intake mutation |
| Same finalization key and hash after ambiguous response | `200`, `created=false`, original asset IDs |
| Finalization key/hash drift or partial idempotency pair | `409`; persisted intake is unchanged |

### 5. Good/Base/Bad Cases

- Good: create a workspace, close the session, recover by conversation ID, finalize two references, lose the response, and
  replay with the same key to obtain the same two asset IDs and no workflow.
- Base: open an unfinished draft through its product URL and route back to the same intake workspace.
- Base: call the compatibility composite endpoint and receive the established fully collected version-zero workspace.
- Bad: create the Product in one request and later create a second Draft/conversation during intake finalization.
- Bad: start a Turn on a plain empty Draft or assign the first uploaded reference as a semantic main image.

### 6. Tests Required

- Application tests assert draft-only row counts, replay/drift, restore, storage failure compensation, finalization replay,
  post-finalization drift, and the empty-Draft Turn guard.
- HTTP tests assert strict JSON, multipart field names, status/response shape, one to six files, and zero ProductWorkflow rows.
- ORM/migration tests assert both field lengths, the pair check, populated downgrade refusal, empty downgrade, and history
  preservation.
- An isolated PostgreSQL test must close/reopen sessions across create, restore, finalize, and replay; assert one Product,
  exact media/asset counts, zero revisions, and zero workflows.

### 7. Wrong vs Correct

Wrong:

```python
product = create_product_and_commit(...)
draft = create_draft_and_commit(product.id)
upload_references_and_commit(draft.id, images)
```

Correct:

```python
workspace = create_agent_product_draft_workspace(session, name=name, idempotency_key=key)
workspace = finalize_agent_product_workspace_intake(
    session,
    conversation_id=workspace.conversation.id,
    selection=selection,
    image_uploads=images,
    idempotency_key=intake_key,
)
```

Each command owns one atomic boundary, and both commands converge through persisted request identity.

## Failure And Logging Rules

- Syntactic/business rejection maps to stable `400`/`404`/`409` responses. Network failures, malformed upstream state,
  or missing service configuration map to `503` at the browser boundary.
- A failed start keeps the durable projection and a bounded safe sync error. An unbound projection can retry the same Go
  idempotency key.
- Pollable network failures retain durable state and schedule a later sync. Artifact validation conflicts are retained for
  operator/user correction and are not treated as successful sync.
- Harness `failed` and `unknown` error strings are diagnostic data. ProductFlow projects stable user-facing messages for
  those statuses and logs only run/Turn IDs plus the safe status category; provider response bodies do not enter browser
  state or ProductFlow logs.
- Logs may contain conversation, projection, harness Turn IDs, status, and safe failure categories. They must not contain
  internal/provider tokens, complete user input, artifact bodies, image bytes, base64, data URLs, storage paths, or
  complete provider requests/responses.

## Deployment And Validation

- Compose persists `/data` in `productflow-agent-data`, passes infrastructure and internal-auth settings through
  environment variables, and starts the worker after backend and Agent health checks pass. Runtime provider credentials
  and model options come from the authenticated ProductFlow endpoint.
- `scripts/release.sh` checks the Agent `/healthz` endpoint from the Compose network because the service has no host port.
- Required Go gates: `gofmt`, `go vet ./...`, `go test ./...`, and `go test -race ./...`.
- Required backend gates: focused Agent/API/migration tests, Ruff, and the full pytest suite.
- Required artifact changes test the full generated schema, local harness rejection/retry, ProductFlow business
  rejection/retry, cross-product reference rejection, and attachment-time revalidation. A real provider run must use the
  complete production schema rather than a reduced probe.
- Migration changes require a PostgreSQL 16 `previous -> head -> previous -> head` round trip with sentinel preservation.
- `TestLiveProviderTwoTurnTranscript` is opt-in through `PRODUCTFLOW_RUN_LIVE_AGENT=1`; environment credentials are
  exposed by its ProductFlow fixture through the authenticated runtime-config endpoint, and Manager must fetch them rather
  than receive a direct provider override. The default suite skips it and consumes no provider quota.
- Docker verification must build from a clean repository context, run as the non-root `productflow` user, return the fixed
  harness commit from `/healthz`, reject missing internal auth, and contain no sibling checkout dependency.
