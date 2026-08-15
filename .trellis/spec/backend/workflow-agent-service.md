# Workflow Agent Service

## Scope

Read this guide before changing:

- `agent-service/`
- ProductFlow Agent conversation/control/sync code
- Agent internal routes and tools
- WorkflowDraft artifacts
- SSE event projection
- Agent provider binding

The service exists to clarify product requirements, inspect and organize selected product assets, and produce a confirmable WorkflowDraft. ProductFlow remains the business-data authority.

## Runtime Ownership

### ProductFlow FastAPI

- Creates Product, uploaded ProductImageAsset rows, WorkflowDraft, and AgentConversation.
- Reserves idempotent AgentTurnProjection rows.
- Sends control requests to agent-service.
- Projects Turn state and durable events into PostgreSQL.
- Exposes bounded internal product context, asset reads, and mutation endpoints.
- Validates and stores WorkflowDraft artifacts.
- Owns user confirmation and workflow materialization.

### Go Agent Service

- Hosts one `agent-harness` durable service.
- Resolves current Agent provider configuration from ProductFlow.
- Starts, cancels, resumes, answers, and streams Turns.
- Registers read tools and durable mutation tools.
- Stores durable journal/checkpoint data under `AGENT_DATA_ROOT`.
- Preserves token-level deltas and structured tool results across restart.

### Browser

- Talks only to ProductFlow public APIs.
- Reconnects to ProductFlow SSE with event sequence.
- Never receives the internal service token or provider secret.

## HTTP Boundaries

Browser-facing ProductFlow routes are product/conversation scoped:

- create/read conversation;
- bounded Turn pages;
- submit/get/cancel/resume Turn;
- answer question;
- stream events.

Agent-service routes are under `/internal/v1` and require the shared bearer token:

- start Turn;
- get Turn;
- cancel;
- resume;
- answer;
- stream events.

ProductFlow internal tool routes are under `/api/internal/v1` and require the same service trust boundary. Do not expose them through browser navigation or CORS.

Strict JSON decoding rejects unknown fields where the contract requires it. Request bodies are bounded.

## Provider Configuration

The Agent service fetches the active `agent` ProviderBinding from:

```text
GET /api/internal/v1/agent-runtime/provider-config
```

The response contains the resolved interface, model, request settings, endpoint, and secret only for the authenticated internal service.

Rules:

- missing/disabled binding is an explicit unavailable/configuration failure;
- the browser settings API never returns the secret;
- the running Agent service does not read provider credentials from general environment variables;
- `AGENT_PROVIDER_*` variables are reserved for the opt-in live test.

## ProductFlow Projection

`AgentConversation` is product and WorkflowDraft scoped. Its id is also the durable run scope.

`AgentTurnProjection` stores:

- ProductFlow projection id;
- idempotency key and request hash;
- harness Turn id;
- input text and selected ProductImageAsset ids;
- projected status;
- output text/refusal;
- question payload;
- last event sequence;
- WorkflowDraft revision linkage;
- safe errors and timestamps.

The projection state machine mirrors harness states without inventing success:

- queued
- running
- requires_input
- awaiting_confirmation
- succeeded
- failed
- cancel_requested
- canceled
- unknown

Failed, canceled, or unknown Turns do not become trusted transcript history.

## Turn Submission

Submission rules:

- conversation and assets belong to the Product;
- text is normalized and bounded;
- asset count follows the current multimodal limit;
- idempotency key is required;
- same key + same request returns the existing projection;
- same key + different request is a conflict;
- projection is committed before remote submission;
- remote ambiguity is recorded and reconciled, not rewritten as failure or success.

Subsequent Turns inherit the latest trusted durable transcript within the same run.

## Events and SSE

Durable event fields include:

- schema version;
- run id;
- Turn id;
- per-Turn sequence;
- timestamp;
- step id and attempt id when relevant.

Token-level text deltas are persisted before delivery. If persistence fails, provider streaming stops and the attempt is conservatively unknown.

ProductFlow SSE:

- forwards bounded projected events;
- uses monotonic event sequence as id;
- supports reconnect with Last-Event-ID or after cursor;
- deduplicates repeated provider/service delivery;
- emits heartbeat without advancing business state.

The frontend must converge to persisted projection after reconnect.

## WorkflowDraft Artifact

The required Agent output is a versioned WorkflowDraft artifact. It includes:

- product facts and conflicts;
- selected image types and quantities;
- workflow-level visual system;
- per-type/per-image prompt plans;
- explicit reference bindings;
- GenerationSpec and DeliverySpec;
- folders, nodes, and edges;
- readiness for user confirmation.

ProductFlow validates the artifact independently. Invalid artifacts become safe Turn/Draft failures and do not partially materialize a graph.

An awaiting-confirmation Turn is trusted only after ProductFlow has persisted the corresponding WorkflowDraftRevision.

## Read Tools

Current read tools:

- `get_product_workflow_context_v1`
- `list_product_image_assets_v2`
- `inspect_product_image_assets_v1`

Contract:

- context returns bounded current Product/Draft data;
- list returns names, directories, origin, image type, dimensions, and ids with cursor bounds;
- inspect accepts explicitly selected ids and returns a multimodal ToolResult;
- image bytes obey ToolResult limits;
- full-library bytes are never returned in one call.

Tool name suffixes are wire-schema versions and may evolve independently of ProductWorkflow schema version.

## Durable Mutation Tools

Current mutation tools:

- `create_product_image_folder_v1`
- `rename_product_image_folder_v1`
- `rename_product_image_asset_v1`
- `move_product_image_assets_v1`

Every durable mutation uses:

1. Prepare: validate scope and capture expected state.
2. Execute: apply with idempotency key and request hash.
3. Reconcile: inspect ProductFlow state after ambiguous completion.

`AgentToolMutation` records prepared content, status, result, and error/reconciliation data.

Rules:

- product/conversation scope is mandatory;
- repeated identical calls are idempotent;
- same key with different content conflicts;
- stale expected names/folders conflict;
- partial batch mutation is rejected or reconciled according to the typed contract;
- a failed prepare creates no mutation row;
- unknown side effects remain unknown until reconciliation proves applied/failed.

## ToolResult Contract

Structured tool results use schema version 1:

- `input_text` parts;
- `input_image` parts;
- PNG, JPEG, or WebP;
- at most 16 images per ToolResult;
- at most 20 MiB per image;
- at most 64 MiB total per result.

HTTPS image URLs are absolute, have no userinfo, and include caller-verified media type and size. Embedded bytes are checked against declared type and length.

Journal/checkpoint data keeps structured URL/byte fields. Data URLs are created only while constructing a provider request and are not appended to tool text history.

## Questions and Confirmation

Questions are used for missing information that affects workflow/output. Each question has a stable id and either options or free text according to the harness contract.

- answering a non-current question conflicts;
- repeated identical answer is idempotent where supported;
- resume continues the same Turn;
- final workflow confirmation is a ProductFlow Draft action, separate from harness question answering.

## Cancellation and Recovery

- Browser cancel marks ProductFlow cancel_requested and calls agent-service.
- Agent service durably records cancellation state.
- ProductFlow sync converges the projection.
- Restart recovery scans unfinished projections and rechecks harness state.
- A provider success without durable delta/tool/result persistence is not reported as succeeded.
- Safe terminal transcript can seed the next Turn; untrusted terminal state cannot.

## Compaction

agent-harness may compact older completed Turn boundaries when context limits require it.

- latest user input remains verbatim;
- current tool round and recent Turns stay available;
- encrypted reasoning/output items required by the provider contract are preserved;
- compaction is durable and replayable;
- compaction never mutates ProductFlow business artifacts.

## Logging and Security

Never log:

- provider API keys;
- internal bearer token;
- full uploaded image bytes;
- data URLs/base64 images;
- session/admin/settings secrets;
- complete private transcript at info level.

Safe logs may include ids, status, sequence, counts, byte sizes, provider name, model, duration, and normalized error code.

## Tests Required

ProductFlow tests:

- conversation/Turn idempotency and scope;
- bounded Turn pagination;
- state projection and sync recovery;
- question/cancel/resume;
- WorkflowDraft artifact validation/linkage;
- read-tool bounds and multimodal responses;
- durable mutation prepare/apply/reconcile;
- provider config secret boundary.

Agent-service tests:

- internal auth and strict JSON;
- start/get/cancel/resume/answer;
- SSE reconnect and event sequence;
- tool schemas and no-argument strict tools;
- ToolResult image limits and replay;
- restart recovery and transcript inheritance;
- mutation reconciliation;
- provider config refresh.

Run:

```bash
uv run --directory backend pytest tests/test_workflow_agent_service.py
go test ./...
```

Use the opt-in live provider test after provider/runtime changes.

## Forbidden Patterns

- Sending the complete product library to the model.
- Letting agent-service write ProductFlow tables directly.
- Browser access to internal tokens or provider secrets.
- Plain-text base64/data URLs in transcript history.
- Non-durable token streaming.
- Treating network ambiguity as a clean failure.
- Tool mutation without idempotency and reconciliation.
- Materializing an invalid or unconfirmed Draft.
- A second Agent creation route or conversation store.
