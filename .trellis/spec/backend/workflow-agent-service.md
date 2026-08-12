# Workflow Agent Service

> Executable contracts for the scoped Go agent service, FastAPI projection layer, internal tools, and durable Turn sync.

## Ownership

- `agent-service/` owns the Go process, the fixed agent-harness source snapshot, conversation-scoped harness services,
  SQLite journals, native multimodal provider requests, and the internal Turn control plane.
- `application/agent_conversations.py` owns PostgreSQL conversation and Turn projection state.
- `application/agent_control.py` owns start, answer, cancel, resume, and harness-state projection orchestration.
- `application/agent_sync.py` owns browser-independent polling, restart recovery, and required-artifact attachment.
- `application/agent_tools.py` owns scope-bound product context, asset metadata/content reads, and reconcilable asset
  display-name changes.
- `infrastructure/agent_service.py` owns the FastAPI-to-Go HTTP/SSE client.
- `presentation/routes/agent_conversations.py` exposes session-authenticated browser APIs.
- `presentation/routes/agent_internal.py` exposes bearer-authenticated ProductFlow tool APIs to the Go service.

The initial implementation is single-instance and single-merchant. Multi-instance journal coordination, gallery folders,
workflow materialization, and the frontend Agent conversation are separate tasks.

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
`AgentToolMutation` stores the idempotency ledger for reconcilable ProductFlow mutations.

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

## Required Workflow Artifact

Every completed workflow-design Turn must call strict `propose_workflow_draft` with the current
`WorkflowDraftPayloadV1` JSON Schema. A prose-only completion fails the harness artifact gate.

When a Turn reaches `awaiting_confirmation`, the sync path calls `append_workflow_draft_revision(...)` with harness Turn
ID and artifact step ID. The existing `(draft_id, source_turn_id, source_artifact_step_id)` uniqueness contract makes
replay idempotent. Only the resulting revision ID is stored on the Turn projection. Confirming the Draft marks an
associated Agent conversation `completed`.

## ProductFlow Tool Gateway

Read tools are bound to the conversation closure and expose no scope IDs in their schemas:

- `get_product_workflow_context_v1`
- `list_product_image_assets_v1`
- `inspect_product_image_assets_v1`

Asset listing returns bounded metadata pages without URLs or storage paths. Inspect accepts explicit IDs only, returns at
most six images, and revalidates file byte count, MIME type, dimensions, and SHA-256 against verified media metadata.

`rename_product_image_asset_v1` is a reconcilable durable effect. Prepare records the expected and target display names;
execute uses the harness invocation idempotency key; reconcile distinguishes `applied`, `not_applied`, `conflict`, and
`unknown` from the ProductFlow mutation ledger and current asset state.

The service has no tools for deleting assets, changing covers, creating missing brand material, writing individual DAG
nodes, or materializing a Draft. Gallery folder tools are added only after the gallery folder domain exists.

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

## Failure And Logging Rules

- Syntactic/business rejection maps to stable `400`/`404`/`409` responses. Network failures, malformed upstream state,
  or missing service configuration map to `503` at the browser boundary.
- A failed start keeps the durable projection and a bounded safe sync error. An unbound projection can retry the same Go
  idempotency key.
- Pollable network failures retain durable state and schedule a later sync. Artifact validation conflicts are retained for
  operator/user correction and are not treated as successful sync.
- Logs may contain conversation, projection, harness Turn IDs, status, and safe failure categories. They must not contain
  internal/provider tokens, complete user input, artifact bodies, image bytes, base64, data URLs, storage paths, or
  complete provider requests/responses.

## Deployment And Validation

- Compose persists `/data` in `productflow-agent-data`, passes internal/provider settings only through environment
  variables, and starts the worker after backend and Agent health checks pass.
- `scripts/release.sh` checks the Agent `/healthz` endpoint from the Compose network because the service has no host port.
- Required Go gates: `gofmt`, `go vet ./...`, `go test ./...`, and `go test -race ./...`.
- Required backend gates: focused Agent/API/migration tests, Ruff, and the full pytest suite.
- Migration changes require a PostgreSQL 16 `previous -> head -> previous -> head` round trip with sentinel preservation.
- `TestLiveProviderTwoTurnTranscript` is opt-in through `PRODUCTFLOW_RUN_LIVE_AGENT=1`; the default suite skips it and
  consumes no provider quota.
- Docker verification must build from a clean repository context, run as the non-root `productflow` user, return the fixed
  harness commit from `/healthz`, reject missing internal auth, and contain no sibling checkout dependency.
