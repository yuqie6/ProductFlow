# Backend Quality Guidelines

## Required Gates

```bash
uv run --directory backend ruff check src tests
uv run --directory backend pytest
cd agent-service && go test ./...
```

Use focused tests during implementation and complete gates before finish.

## Tooling

- Python target: 3.12+.
- Ruff line length: 120.
- Ruff rules: E, F, I, UP, B.
- pytest discovers `backend/tests/test_*.py`.
- Alembic owns schema changes.
- Go service uses its module's declared Go version.

## Layer Boundaries

- Presentation maps HTTP and typed errors.
- Application owns business transactions and orchestration.
- Domain owns database-free rules.
- Infrastructure owns providers, database adapters, storage, queue, and service clients.
- Workers are composition roots.

Do not move code across a layer only to satisfy a file-size preference. Split when ownership or executable boundary is real.

## Provider Seams

Provider-specific SDK code stays under infrastructure. Application code consumes current protocols/resolved config.

Current purposes:

- prompt;
- Agent;
- image.

Tests inject narrow dependency seams or provider clients at the owner module. Avoid patching a broad facade that no longer owns construction.

Provider failures expose safe messages/codes and never leak request headers, API keys, or raw private payload.

## Input Validation

Validate at the boundary that has the needed knowledge:

- Pydantic: wire shape and scalar bounds.
- Presentation upload helper: byte/MIME/pixel/count.
- Application: ownership, lifecycle, conflicts, cross-field business rules.
- Domain: DAG invariants.
- Infrastructure adapter: provider capability/wire projection.
- Database: stable relational/check uniqueness constraints.

Do not duplicate the same rule with slightly different limits in multiple layers.

## Image Generation Contract

All image entry points share:

- canonical size parsing/bounds;
- runtime maximum dimension;
- reference count/byte limits;
- current advanced tool-field allowlist;
- safe provider metadata projection;
- ProductImageAsset/MediaObject persistence;
- durable task status.

Candidate quantity is explicit business input. Provider adapters may map it to their native count field internally.

Actual provider dimensions are measured from returned bytes and recorded. A mismatch is surfaced as provider metadata/note, not silently reported as requested size.

## Durable Queue Submission

The submit boundary:

1. validates request;
2. creates durable queued rows;
3. commits them;
4. sends a Dramatiq message;
5. on broker failure, marks the persisted task failed through the shared submission helper and raises QueueUnavailableError.

Never hold an uncommitted database transaction while assuming broker delivery creates durability.

## Worker Execution

Workers:

- create/close their Session;
- claim work atomically;
- re-read current task state;
- respect cancellation/terminal state;
- call application execution;
- persist attempt/result safely;
- release concurrency admission;
- never trust duplicate message delivery to be unique.

Application execution is idempotent at the durable task/run identity.

## Recovery

Startup recovery:

- classifies queued, running, stale, terminal, and provider-in-progress rows;
- commits recovery state before message redelivery;
- sends messages outside the database transaction;
- records per-item delivery failure;
- preserves provider ambiguity;
- does not reset succeeded/canceled rows.

Use opt-in PostgreSQL/Redis tests for transaction ownership, broker delivery, or recovery changes.

## Agent Durability

The Go service and ProductFlow projection follow `workflow-agent-service.md`.

Quality requirements:

- persisted token-level deltas before forwarding;
- monotonic event sequence;
- reconnect/replay;
- trusted transcript inheritance only from safe terminal states;
- bounded multimodal ToolResult;
- durable mutation prepare/execute/reconcile;
- unknown on unprovable completion.

## Storage Safety

- Validate uploads before permanent write.
- Store normalized unique paths.
- Track bytes created by the current mutation.
- Remove only newly created bytes on database failure.
- Post-commit cleanup is explicit and best effort.
- Never accept arbitrary browser file paths.
- Do not log bytes, base64, or data URLs.

## Database and Transactions

- Borrowed Session helpers do not commit/close.
- One business operation has one owning transaction.
- Lock ordering is deterministic for batch moves/deletes.
- Query parent scope with child id.
- Version/idempotency conflicts are explicit.
- Immutable lineage remains immutable.

Read `database-guidelines.md` for detailed contracts.

## Error Handling

Expected business failures use typed domain/application errors. Shared presentation handlers preserve a stable `{"detail": "..."}` response.

Unexpected errors:

- are logged with request/correlation context;
- return a safe generic message;
- keep stack/internal payload out of the response;
- do not get reclassified as validation errors.

Provider/queue/storage/Agent errors have typed safe boundaries.

## Logging

Logs may contain:

- ids;
- status;
- counts and sizes;
- provider/model names;
- durations;
- safe error code;
- recovery summary.

Logs must not contain:

- API keys or internal/admin/settings tokens;
- cookie/session values;
- full prompts or Agent private transcripts at info level;
- upload/media bytes;
- base64/data URLs;
- provider auth headers/raw private response.

## Migrations

For schema changes:

- add an Alembic revision;
- update SQLAlchemy metadata;
- test fresh upgrade;
- test the specific transform/constraint;
- validate PostgreSQL-specific enum/transaction behavior live;
- do not silently edit historical revisions;
- destructive cleanup has an explicit irreversible downgrade.

Runtime code should use only head schema.

## Docker Compose and Release

The current Compose stack includes:

- PostgreSQL;
- Redis;
- backend;
- worker;
- Agent service;
- Web.

`scripts/release.sh`:

1. reads current ports;
2. validates Compose;
3. in dry-run mode prints actions only;
4. otherwise runs `docker compose up -d --build --remove-orphans`;
5. checks backend, Agent service, Web, and Web proxy health;
6. never deletes volumes.

`STORAGE_HOST_PATH` is an optional current host bind mount. Container storage root remains `/app/storage`.

## Historical Data Compatibility

When a migration preserves JSON or enum-like values, add at least one fixture using a value from the deployed source profile, not only a newly generated current payload. Verify the complete read path from database row through Pydantic serialization, HTTP bootstrap, frontend union/exhaustive label maps, and a rendered route. Archive-only values remain readable and labeled without restoring retired runtime behavior.

The minimum smoke check for a preserved workbench record is:

```text
migrated database -> GET /api/v2/products/{product_id}/agent-workbench -> rendered /products/{product_id}
```

A unit test that validates only a newly constructed current payload is insufficient evidence for a migration boundary.

## Testing Requirements

Add a regression test near the trigger:

- route/schema -> request/response/error;
- application -> state transition/transaction;
- graph -> DB-free rules and integration;
- provider -> wire payload and failure mapping;
- queue/recovery -> duplicate/redelivery/broker failure;
- media/storage -> validation and compensation;
- Agent -> projection/events/tools/recovery;
- migration -> schema/data constraint.

Use real provider/DB/browser validation where mocks cannot establish the contract.

## Review Checklist

- Trace the live call chain.
- Confirm existing model/helper/exception/test fixture reuse.
- Check transaction owner and external side-effect order.
- Check idempotency and retry.
- Check safe errors/logging.
- Check frontend DTO/config impact.
- Run focused and full gates.
- Scan for deleted symbol/path/config residue.
- Run `git diff --check`.

## Forbidden Patterns

- Route-level provider or storage orchestration.
- Broad catch-and-fallback after a failed transaction.
- Hidden commit in a helper.
- Process-local state as durable task authority.
- Duplicate async execution path.
- Silent result truncation.
- Secret or image bytes in logs/history.
- Runtime fallback to a removed provider/config/workflow path.
- Production code kept only for an obsolete deployment cutover.
