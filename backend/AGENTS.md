# Backend Engineering Guidelines

## Read First

Use `docs/ARCHITECTURE.md` for the current code map and `CONTEXT.md` for domain invariants. Before editing a backend subsystem, read its route/schema, application entrypoint, models or external adapter, and the closest tests. Paths in old design notes are not ownership evidence.

## Layer Boundaries

- `presentation/` validates HTTP input, invokes application use cases, serializes output, and maps known domain errors. Routes do not assemble provider payloads or own multi-step business transactions.
- `application/` owns business orchestration and transaction boundaries.
- `domain/` contains enums, business errors, and database-free rules.
- `infrastructure/` adapts SQLAlchemy, storage, Redis/Dramatiq, providers, and the Agent service.
- `workers.py` is a composition root: it creates dependencies and invokes application execution without duplicating business rules.
- Online code understands schema-v2 only. Legacy source shapes are restricted to `application/legacy_retirement/`, immutable archive reads, and Alembic history.

Current ownership:

- Agent workspace and Turn projection: `agent/product_workspaces.py`, `conversations.py`, `control.py`, `sync.py`, `tools.py`.
- Draft validation and materialization: `workflow_drafts/contracts.py`, `service.py`, `materialization.py`.
- Current workflow commands and execution: `product_workflow/` public entry, `v2_*.py`, `execution.py`, `run_state.py`, plus `domain/workflow_rules.py`.
- Product images: `product_images/` (`queries.py`, `mutations.py`, `archives.py`, `assets.py`). `MediaObject` primitives live in `media_objects.py`.
- Provider/runtime configuration: `settings.py`, `runtime_settings.py`, `infrastructure/provider_config.py`, and provider adapters under `infrastructure/prompt/` and `infrastructure/image/`.
- Durable submission/recovery: `queue_submission.py`, `durable_recovery.py`, `agent/sync.py`, and `workers.py`.

## Database And Transactions

- Separate Session lifecycle from transaction ownership. FastAPI dependencies and workers create/close Sessions. Public application command entrypoints commonly own one business transaction and explicitly commit/rollback; internal `stage_*` and composition helpers only add/flush and never hide a commit.
- Read the complete call chain before changing transaction ownership. Do not infer it from who constructed the Session.
- After a database error, roll back before reusing the session. Do not catch and continue in the same failed transaction.
- Scope child lookups by their owning product, workflow, Draft, or conversation, even when ids are globally unique.
- Use deterministic ordering with an id tie-breaker and bounded cursor pagination for large collections.
- Keep count and item filters identical. Do not load media bytes or unbounded JSON for list projections.
- Prefer named foreign keys, unique constraints, indexes, and checks when they express stable business invariants.
- Acquire locks in deterministic aggregate/member order for concurrent batch mutations. Expected-state or idempotency mismatches are explicit conflicts.

## Persistent Contracts

- `MediaObject` owns immutable media metadata; `ProductImageAsset` owns product-scoped identity. Workflow bindings never store a path as identity.
- WorkflowDraft revisions, fact sets, visual systems, prompt artifacts, recipes, and generation records preserve prior-run lineage instead of mutating historical payloads.
- Agent mutations and materialization use explicit idempotency keys plus request hashes. Reusing a key with different input is a conflict.
- Preserve `unknown` when an Agent Turn or side effect cannot be proven.
- GenerationSpec, provider-effective parameters, measured output, and DeliverySpec remain distinct contracts.

## External Effects

- Validate before provider or storage work.
- A new storage write followed by database failure compensates only files created by that attempt.
- Never delete pre-existing shared media as generic rollback cleanup.
- Provider-specific request mapping stays in infrastructure adapters; public errors must not expose secrets, provider bodies, filesystem paths, or tracebacks.
- Durable queue submission persists the queued business row before broker delivery. Broker failure must leave an observable failed/pending state and return a typed queue error.
- Duplicate worker delivery is handled by durable identity, atomic claim, and idempotency. Process-local state is not durable authority.

## Errors And Logging

- Expected business failures use `domain/errors.py` and the shared mapping in `presentation/errors.py`; routes do not duplicate mappings.
- Use validation errors for malformed or cross-field business input, conflicts for valid commands rejected by current state, and scoped not-found errors when cross-owner existence must remain hidden.
- Network ambiguity from providers or the Agent service does not prove failure. Preserve retryable or `unknown` state until reconciliation proves an outcome.
- `infrastructure/logging.py` owns logging setup and request/worker correlation. Reset contextvars in `finally`.
- Durable rows, HTTP responses, and storage state are behavioral evidence. Logs are diagnostic and must exclude secrets, cookies, full prompts/transcripts, provider bodies, bytes, base64, and data URLs.
- Do not introduce another logging/metrics framework during unrelated work.

## Workflow, Agent, And Images

- Use `domain/workflow_rules.py` instead of local DAG algorithms. Current workflow and node schema is 2; current node types come from `domain/enums.py`.
- Draft confirmation targets an explicit revision. Materialization validates the complete artifact and commits graph plus reveal events atomically.
- The main Agent runtime and session/event transcript live in the Node.js Pi adapter; ProductFlow owns business objects and the Web projection. Main does not promise background durable execution, effect reconciliation, or multi-instance claims. Turn/tool idempotency requires both key and request hash; ambiguous mutations reconcile and remain `unknown` when unprovable.
- Agent asset reads are bounded metadata followed by explicit image inspection. Never send a full library, data URLs, or media bytes in text history.
- `MediaObject` owns immutable byte metadata; `ProductImageAsset` owns product-scoped identity. Folder deletion changes organization only. Reference rebinding uses one explicit asset id and preserves historical lineage.
- Generation intent, provider-effective values, measured output, and delivery rendition are distinct contracts. Candidate quantity is business input, not an advanced provider field.

## Migrations

- Every schema change has an Alembic revision and a focused regression test.
- Fresh-database `upgrade head` must work. PostgreSQL-specific enum, lock, or transaction behavior requires live validation when affected.
- Historical revisions are immutable unless explicitly repairing a demonstrated broken revision.
- A migration must not call an Agent/provider or assume storage is mounted.
- Any future V1 destructive cleanup must call `assert_legacy_cutover_cleanup_ready` in its owning transaction and follow `docs/operations/legacy-v1-cutover.md`.

## Verification

Run focused tests while editing, then:

```bash
uv run --directory backend ruff check .
uv run --directory backend pytest
```

Use the opt-in PostgreSQL/Redis/provider gates from the `justfile` when a change touches dialect-sensitive transactions, durable recovery, delivery rendering, Agent intake, or provider behavior.

Add the closest regression first: route/schema, application transition/rollback, graph rule, provider wire payload, queue recovery, storage compensation, Agent reconciliation, or migration transform. Historical-value compatibility requires a stored fixture through API serialization and frontend rendering; a newly constructed current DTO is insufficient.
