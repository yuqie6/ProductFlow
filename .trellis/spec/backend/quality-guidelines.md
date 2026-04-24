# Backend Quality Guidelines

> Backend quality standards reflected by ProductFlow's current code, tests, and tooling.

---

## Tooling

Backend tooling is defined in `backend/pyproject.toml` and root `justfile`:

- Python target: `>=3.12`.
- Ruff line length: `120`.
- Ruff target version: `py312`.
- Ruff lint selections: `E`, `F`, `I`, `UP`, `B`; `B008` is intentionally ignored for FastAPI dependency defaults.
- Pytest discovers tests under `backend/tests/`.

Common commands:

```bash
just backend-test
uv run --directory backend ruff check .
just backend-migrate
just backend-run
just backend-worker
```

Use the root `justfile` where possible so local env loading and ports match the project.

### Scenario: Keep release snapshots and open-source examples clean

#### 1. Scope / Trigger

- Trigger: editing repository release helpers, example env files, or ignore rules that affect what can be published.
- Applies to `scripts/release.sh`, `.env.example`, `.env.dev.example`, `web/.env.example`, `.gitignore`, and
  `.trellis/.gitignore`.

#### 2. Signatures

- `scripts/release.sh` is an optional single-host snapshot helper.
- Supported overrides include `RELEASE_ROOT`, `BACKEND_PYTHON`, `APP_PORT`, and `WEB_PORT`.
- The default `RELEASE_ROOT` must stay repository-relative (currently `.release/`), not a developer-specific absolute path.

#### 3. Contracts

- Example env files must contain placeholders or mock-provider defaults only; never commit real secrets or private hostnames.
- Release snapshots must exclude runtime data, generated storage, local env files, per-developer Trellis state, caches, and
  release output directories.
- Keep `.trellis/spec/`, `.trellis/workflow.md`, and `.trellis/scripts/` source-controlled; keep `.trellis/tasks/` and
  `.trellis/workspace/` out of public tracking.

#### 4. Validation & Error Matrix

- Real token/private key in a tracked or newly added file -> remove it and rotate the secret before publishing.
- Default release path references `/home/<user>` or another private absolute path -> replace it with a repo-relative or
  documented override path.
- `.trellis/tasks/` appears in `git ls-files` -> remove it from the index without deleting the local task context.

#### 5. Good/Base/Bad Cases

- Good: `RELEASE_ROOT` defaults to `.release/`, `.release/` is ignored, and tar excludes `.release`, storage, env, and
  Trellis runtime state.
- Base: `.env.dev.example` uses local service ports and mock providers while allowing contributors to opt into real
  providers by setting their own untracked env file.
- Bad: A release helper copies `backend/storage/`, `.env`, `.trellis/tasks/`, or a developer-specific path into the
  publishable artifact.

#### 6. Tests Required

- Run `bash -n scripts/release.sh` after shell helper edits.
- Run `git diff --check` and `git diff --cached --check` before committing release hygiene changes.
- Run a high-confidence secret pattern scan over tracked and newly added files, excluding lockfiles if needed for noise.
- Confirm referenced files and `just` commands exist when README or docs are updated.

#### 7. Wrong vs Correct

Wrong:

```bash
RELEASE_ROOT="${RELEASE_ROOT:-/absolute/local/ProductFlow-release}"
```

Correct:

```bash
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RELEASE_ROOT="${RELEASE_ROOT:-$repo_root/.release}"
```

---

## Required Patterns

### Keep provider code behind infrastructure factories

Existing provider selection is centralized in:

- `backend/src/productflow_backend/infrastructure/text/factory.py`
- `backend/src/productflow_backend/infrastructure/image/factory.py`

Routes and use cases call provider interfaces/factories, not concrete SDK classes directly. If adding providers, update the
factory, config definitions, tests, and settings UI types together.

### Validate inputs at the correct boundary

- FastAPI `Query` constraints are used for list pagination in `presentation/routes/products.py`.
- Upload MIME/size/pixel validation is centralized in `presentation/upload_validation.py`.
- Business text/price normalization lives in `application/use_cases.py` helpers such as `_normalize_required_text(...)` and
  `_normalize_price(...)`.
- Runtime settings normalization lives in `backend/src/productflow_backend/config.py`.

Do not duplicate these checks in multiple pages/routes.

### Preserve workflow-level tests

`backend/tests/test_workflow.py` is the main regression suite. It covers:

- Auth/session behavior.
- Settings API persistence and validation.
- SQLAlchemy enum value storage.
- End-to-end product/copy/poster workflow.
- Reference image upload/deletion.
- Continuous image-session behavior.
- Alembic upgrade path.
- OpenAI Responses image provider parsing behavior.

When changing product, copy, poster, settings, upload, image-session, provider, or migration behavior, add or update tests
in this workflow style.

### Keep storage safe

Use `LocalStorage` from `backend/src/productflow_backend/infrastructure/storage.py` for storage paths. It resolves relative
paths under the configured root and rejects absolute/path-traversal paths. Do not build download paths manually in routes.

### Keep async job semantics idempotent

Job creation and workers are designed to avoid duplicate active jobs and duplicate execution:

- Active job uniqueness is enforced with `uq_job_runs_one_active_per_product_kind` in models/migration.
- Queue send failures are marked with `mark_job_enqueue_failed(...)` before returning 503.
- Dramatiq actors use `max_retries=0`; application code owns retry state.
- Backend/worker startup must call `recover_unfinished_jobs(...)` so a database-persisted job is not stranded when the
  process restarts before the Redis message is consumed.
- Product workflow runs follow the same durable-delivery rule with `recover_unfinished_workflow_runs(...)`: the
  `workflow_runs` / `workflow_node_runs` tables are authoritative, Dramatiq is only delivery, and duplicate messages must
  no-op for terminal or currently-running runs.

Preserve these semantics when editing job code.

#### Scenario: Recover stranded async jobs after process restart

##### 1. Scope / Trigger

- Trigger: queue infra integration for `job_runs` plus Dramatiq/Redis.
- Applies when editing `backend/src/productflow_backend/infrastructure/queue.py`,
  `backend/src/productflow_backend/workers.py`, or product job route enqueue behavior.

##### 2. Signatures

- `recover_unfinished_jobs(reset_stale_running: bool = False, stale_running_after: timedelta = 30 minutes)`
- API startup calls `recover_unfinished_jobs()` for `queued` jobs only.
- Dramatiq worker startup calls `recover_unfinished_jobs(reset_stale_running=True)` so old `running` jobs can be retried.

##### 3. Contracts

- `queued + is_retryable=true` means "DB says work should happen"; startup must send the job ID back to Redis.
- `running + is_retryable=true + started_at <= stale cutoff` means "worker likely died"; worker startup may reset it to
  `queued` before sending.
- `succeeded`, `failed`, non-retryable, and recent `running` jobs must not be reset.

##### 4. Validation & Error Matrix

- Redis enqueue fails during recovery -> log the failure and leave the DB state retryable for the next startup.
- DB read/update fails during recovery -> rollback, log, and do not crash normal app startup.
- Unknown `JobKind` -> treat as a programming error in the queue helper, not as a silent no-op.
- Workflow run Redis enqueue failure -> mark the just-created workflow run `failed` before returning `503`, so the active
  run uniqueness guard is not stranded.
- Workflow worker startup may reset stale `workflow_node_runs.status = 'running'` back to `queued`, then re-enqueue the
  parent run; API startup should only re-enqueue runs that do not have a node currently running.

##### 5. Good/Base/Bad Cases

- Good: API restarts after committing a `queued` poster job but before sending Redis; startup re-sends the job.
- Base: Worker sees a duplicate Redis message for a job already `running` or `succeeded`; `_mark_job_running(...)` returns
  false and the actor exits.
- Bad: Resetting every `running` job on API startup; that can duplicate currently active generation work.

##### 6. Tests Required

- Regression test that a `queued` retryable job is sent by `recover_unfinished_jobs()`.
- Regression test that a stale `running` retryable job is reset to `queued` only when `reset_stale_running=True`.
- Regression tests that workflow run kickoff enqueues the Dramatiq actor, enqueue failure marks the run failed, queued
  workflow runs are recovered, stale running node runs are reset during worker recovery, and duplicate workflow messages
  do not execute terminal/currently-running runs.
- Keep `uv run --directory backend ruff check .` and backend pytest green after queue changes.

##### 7. Wrong vs Correct

Wrong:

```python
# Only Redis is trusted; DB queued jobs are ignored on restart.
run_poster_generation.send(job_id)
```

Correct:

```python
# DB remains authoritative for unfinished work; Redis messages are recoverable delivery attempts.
recover_unfinished_jobs(reset_stale_running=True)
```

---

## Testing Requirements

Run at least these checks for backend changes:

```bash
uv run --directory backend ruff check .
just backend-test
```

For schema changes, also run:

```bash
just backend-migrate
```

and add/update an Alembic revision under `backend/alembic/versions/`. Existing tests should continue to cover both
`Base.metadata.create_all(...)` fixtures and Alembic upgrade behavior.

---

## Forbidden Patterns

- Business logic in FastAPI route handlers beyond input adaptation, use-case calls, error mapping, and serialization.
- Provider-specific SDK calls from `presentation/` modules.
- New database columns or tables without an Alembic migration.
- Enum string changes without updating frontend types and regression tests.
- Unbounded list endpoints that load all rows for UI lists.
- Raw filesystem access for user-controlled storage paths; go through `LocalStorage.resolve(...)`.
- Broad `except Exception` that hides failures. Existing broad catches are narrow boundary cases:
  queue enqueue failure in `presentation/routes/products.py`, config table bootstrap tolerance in `config.py`, and provider
  error classification in application/provider code.
- Committing generated storage, cache directories, `.env`, build output, or pycache files.

---

## Review Checklist

When reviewing backend changes, check:

- Does the change respect the presentation/application/domain/infrastructure layer split?
- Are Pydantic DTOs in `presentation/schemas/` and frontend types in `web/src/lib/types.ts` still aligned?
- Are database model changes mirrored by Alembic migrations and tests?
- Are enum values stored/returned as stable lowercase string values?
- Are uploads, image sizes, and storage paths still bounded?
- Are job failures persisted in `JobRun` and visible via `/api/jobs/{job_id}`?
- Are provider secrets hidden from API responses and logs?
- Do `uv run --directory backend ruff check .` and `just backend-test` pass?
