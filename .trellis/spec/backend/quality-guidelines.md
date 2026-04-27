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

### Scenario: Production-style Docker Compose self-host runtime

#### 1. Scope / Trigger

- Trigger: editing `docker-compose.yml`, Dockerfiles, example env files, or README/docs for the self-hosted runtime.
- Applies to the full Compose stack: PostgreSQL, Redis, FastAPI API, Dramatiq worker, built Web static server, and shared storage.

#### 2. Signatures

- One-click start: `docker compose up -d --build`.
- Manual migration path: `docker compose run --rm productflow-backend alembic upgrade head`.
- Direct API health: `GET /healthz` returns `{"status":"ok"}`.
- Web proxy smoke path: `GET /api/healthz` through nginx proxies to backend `GET /healthz`.

#### 3. Contracts

- `productflow-backend` and `productflow-worker` must use Compose service names for runtime dependencies:
  - `DATABASE_URL=postgresql+psycopg://productflow:<password>@productflow-postgres:5432/productflow`
  - `REDIS_URL=redis://productflow-redis:6379/0`
- Container storage must use a shared in-container path `STORAGE_ROOT=/app/storage`.
- `STORAGE_HOST_PATH` is host-only Compose interpolation for production bind mounts. When unset, `/app/storage` is backed
  by the named volume `productflow-storage`; when set, it may point at an existing host directory such as
  `/home/cot/ProductFlow-release/shared/storage` for old systemd production storage reuse.
- Local hot-reload development must stay isolated on `.env.dev` / `STORAGE_ROOT=./backend/storage-dev`; do not depend on
  shell-sourcing production `.env` for development commands.
- Web self-host runtime must serve Vite build output as static files and proxy same-origin `/api/*` to the backend service.
- Runtime must not require host `uv`, `pnpm`, or `just`; those tools are only for local development.

#### 4. Validation & Error Matrix

- Missing `POSTGRES_PASSWORD` in `.env` -> Compose config/start should fail before launching Postgres.
- Postgres/Redis unhealthy -> backend must wait via `depends_on.condition: service_healthy`.
- Migration failure -> backend container must fail before serving API traffic.
- Backend unhealthy -> worker and web must wait for backend health before starting.
- Web `/api/*` not proxied -> same-origin frontend API calls fail even if static files load.
- Old systemd production files disappear after migration -> check whether `STORAGE_HOST_PATH` was set to the existing host
  storage directory before Compose created/used a fresh named volume.
- `STORAGE_HOST_PATH` leaks into container application config or replaces `STORAGE_ROOT` -> fix Compose env wiring; the app
  should still see `STORAGE_ROOT=/app/storage`.

#### 5. Good/Base/Bad Cases

- Good: `docker compose up -d --build` starts all five services, API health is OK, and web `/api/healthz` returns backend health.
- Good: `STORAGE_HOST_PATH=/home/cot/ProductFlow-release/shared/storage docker compose up -d --build` bind-mounts old
  production files while API/worker still run with `STORAGE_ROOT=/app/storage`.
- Base: local development starts only `productflow-postgres` and `productflow-redis`, while host `just` commands run API/worker/web.
- Bad: `DATABASE_URL` points at `localhost` from inside containers; that targets the app container itself, not Postgres.
- Bad: setting container `STORAGE_ROOT=/home/cot/ProductFlow-release/shared/storage`; that host path does not exist inside
  the container and bypasses the stable `/app/storage` contract.
- Bad: using Vite dev server or host `pnpm` as the documented production-style self-host web runtime.

#### 6. Tests Required

- Run `docker compose config --quiet` after Compose/env edits.
- For storage-related Compose changes, render config with `STORAGE_HOST_PATH` both unset and set; assert backend/worker
  mount `/app/storage`, keep `STORAGE_ROOT=/app/storage`, and do not expose `STORAGE_HOST_PATH` in container env.
- Build container images with `docker compose build productflow-backend productflow-web` or a full `docker compose up -d --build` smoke.
- Smoke a disposable or safe project with direct API health, web health, and web `/api/healthz` proxy checks when practical.
- Keep normal backend/frontend gates green when Dockerfiles or docs depend on package commands: backend tests/ruff and frontend lint/test/build.

#### 7. Wrong vs Correct

Wrong:

```yaml
DATABASE_URL: postgresql+psycopg://productflow:password@localhost:15432/productflow
```

Correct:

```yaml
DATABASE_URL: postgresql+psycopg://productflow:${POSTGRES_PASSWORD}@productflow-postgres:5432/productflow
```

Wrong:

```yaml
environment:
  STORAGE_ROOT: /home/cot/ProductFlow-release/shared/storage
volumes:
  - productflow-storage:/app/storage
```

Correct:

```yaml
environment:
  STORAGE_ROOT: /app/storage
volumes:
  - ${STORAGE_HOST_PATH:-productflow-storage}:/app/storage
```

### Scenario: Keep Compose release and open-source examples clean

#### 1. Scope / Trigger

- Trigger: editing repository release helpers, example env files, or ignore rules that affect what can be published.
- Applies to `scripts/release.sh`, `justfile`, `docker-compose.yml`, `.env.example`, `.env.dev.example`,
  `web/.env.example`, `.gitignore`, and `.trellis/.gitignore`.

#### 2. Signatures

- `just release` / `scripts/release.sh` is the single-host Docker Compose production update helper.
- `just release-dry-run` sets `DRY_RUN=1` and must not stop legacy services, build images, start containers, switch
  symlinks, or delete volumes.
- The actual release path validates Compose config, stops legacy user-level systemd services when present, runs
  `docker compose up -d --build --remove-orphans`, and performs HTTP health checks.
- Legacy services are `productflow-backend.service`, `productflow-worker.service`, and `productflow-web.service`.
- Supported override: `LEGACY_SYSTEMD_ACTION=skip` skips the legacy service stop step after the operator has handled port
  ownership manually.

#### 3. Contracts

- Example env files must contain placeholders or mock-provider defaults only; never commit real secrets or private hostnames.
- Local env backups such as `.env.bak-*` must stay ignored; they may contain copied production secrets and must not be
  inspected, tracked, or included in open-source release hygiene diffs.
- Release/update helpers must not delete Docker volumes; `docker compose down -v` is only a documented manual reset.
- Dry-run must remain non-switching and non-service-starting while still validating `docker compose config --quiet` and
  showing the real command sequence.
- Release helpers must not shell-source `.env`; use Docker Compose's env parsing for service configuration and read only
  the specific local values needed for health-check URLs without executing the file.
- Compose release must gracefully tolerate missing or inactive legacy systemd services but should try to stop them before
  binding the production ports.
- Keep `.trellis/spec/`, `.trellis/workflow.md`, and `.trellis/scripts/` source-controlled; keep `.trellis/tasks/` and
  `.trellis/workspace/` out of public tracking.

#### 4. Validation & Error Matrix

- Real token/private key in a tracked or newly added file -> remove it and rotate the secret before publishing.
- Untracked `.env.bak-*` appears in `git status` -> add/verify ignore coverage without reading or modifying the backup
  file content.
- `just release-dry-run` starts/stops services or builds images -> fix immediately; dry-run is for safe planning.
- `just release` fails because old systemd services still occupy 29280/29281 -> ensure the helper stops legacy services or
  clearly reports the port-binding failure.
- `.trellis/tasks/` appears in `git ls-files` -> remove it from the index without deleting the local task context.

#### 5. Good/Base/Bad Cases

- Good: `just release-dry-run` validates Compose config and prints the planned `docker compose up -d --build --remove-orphans` flow without side effects.
- Good: `just release` stops legacy services, recreates Compose services, passes backend and web `/api/healthz` checks, and leaves volumes intact.
- Base: `.env.dev.example` uses local service ports and mock providers while allowing contributors to opt into real
  providers by setting their own untracked env file.
- Bad: release script creates tar snapshots, flips a `.release/current` symlink, or restarts `productflow-*.service` after
  Compose has become the production runtime.

#### 6. Tests Required

- Run `bash -n scripts/release.sh` after shell helper edits.
- Run `just release-dry-run` or at minimum `DRY_RUN=1 bash scripts/release.sh` after release helper edits.
- Run `docker compose config --quiet` after Compose/env edits.
- For full release validation when practical, run `just release` and smoke backend `/healthz`, web `/healthz`, and web
  `/api/healthz`.
- Run `git diff --check` and `git diff --cached --check` before committing release hygiene changes.
- Run a high-confidence secret pattern scan over tracked and newly added files, excluding lockfiles if needed for noise.
- Confirm referenced files and `just` commands exist when README or docs are updated.

#### 7. Wrong vs Correct

Wrong:

```bash
systemctl --user restart productflow-backend.service productflow-worker.service productflow-web.service
```

Correct:

```bash
systemctl --user stop productflow-backend.service productflow-worker.service productflow-web.service || true
docker compose up -d --build --remove-orphans
```

---

## Required Patterns

### Keep provider code behind infrastructure factories

Existing provider selection is centralized in:

- `backend/src/productflow_backend/infrastructure/text/factory.py`
- `backend/src/productflow_backend/infrastructure/image/factory.py`

Routes and use cases call provider interfaces/factories, not concrete SDK classes directly. If adding providers, update the
factory, config definitions, tests, and settings UI types together.

Workflow execution has an additional explicit dependency seam in
`application/product_workflow_dependencies.py`. Default workflow execution dependencies must resolve providers through the
`application/product_workflows.py` facade so existing route/worker behavior and legacy monkeypatch tests remain compatible,
while focused tests may pass a `WorkflowExecutionDependencies` instance directly.

#### Scenario: Workflow execution dependency seams

##### 1. Scope / Trigger
- Trigger: editing workflow execution provider or renderer construction.

##### 2. Signatures
- `WorkflowExecutionDependencies(text_provider_resolver, image_provider_resolver, poster_renderer_factory)`.
- `run_product_workflow(..., dependencies=None)`, `execute_product_workflow_run(..., dependencies=None)`, and internal
  `_execute_node(..., dependencies=None)` accept this seam without changing API/worker call sites.

##### 3. Contracts
- `None` uses default facade-routed resolvers.
- Facade exports such as `product_workflows.get_text_provider` / `get_image_provider` remain monkeypatch-compatible.
- Custom dependencies may be passed by focused tests or future composition code; they must return provider interface
  instances, not concrete SDK payloads.

##### 4. Validation & Error Matrix
- Resolver/provider failure -> existing workflow failure handling persists the run/node failure reason.
- Missing image provider for generated mode -> remains a runtime execution failure, not a schema/API change.

##### 5. Good/Base/Bad Cases
- Good: a focused test injects fake providers through `WorkflowExecutionDependencies`.
- Base: existing tests monkeypatch `product_workflows.get_image_provider` and still affect default workflow execution.
- Bad: workflow execution imports a concrete provider SDK class or bypasses the facade default resolver.

##### 6. Tests Required
- Keep provider/workflow regression tests passing after resolver changes.
- Add a focused injection test when changing resolver behavior itself.

##### 7. Wrong vs Correct
Wrong:

```python
provider = OpenAIResponsesImageProvider()
```

Correct:

```python
provider = dependencies.image_provider()
```

### Validate inputs at the correct boundary

- FastAPI `Query` constraints are used for list pagination in `presentation/routes/products.py`.
- Upload MIME/size/pixel validation is centralized in `presentation/upload_validation.py`.
- Business text/price normalization lives in `application/use_cases.py` helpers such as `_normalize_required_text(...)` and
  `_normalize_price(...)`.
- Runtime settings normalization lives in `backend/src/productflow_backend/config.py`.

Do not duplicate these checks in multiple pages/routes.

### Scenario: Provider-neutral image generation size contract

#### 1. Scope / Trigger

- Trigger: editing continuous image sessions, workflow `image_generation` node config, runtime image settings, provider image payloads, or frontend image-size controls.
- Applies to the shared `WIDTHxHEIGHT` image-size contract across `ImageChatPage`, workflow Inspector, API schemas, runtime settings, workflow execution, and image providers.

#### 2. Signatures

- Backend canonical normalizer: `normalize_image_generation_size(value: str, *, max_dimension: int | None = None) -> str`.
- Runtime max single-edge setting: `image_generation_max_dimension`.
- Public runtime config API: `GET /api/settings/runtime` returns `image_generation_max_dimension`.
- Built-in presets are application UI constants; runtime settings expose only the max single-edge limit, not a
  user-facing allowed-size preset list.
- Continuous image API request field: `GenerateImageSessionRoundRequest.size`.
- Workflow image node config field: `config_json.size` for nodes with `kind == "image_generation"`.
- Frontend shared picker: `ImageSizePicker` emits normalized lowercase `WIDTHxHEIGHT` strings.

#### 3. Contracts

- Store and pass image size as a provider-neutral lowercase `WIDTHxHEIGHT` string, for example `1024x1024` or `3840x2160`.
- Preset buttons are built-in ratio/tier shortcuts filtered by the runtime max single-edge setting; they are not a backend
  allowlist and are not loaded as arbitrary database-configured options.
- `normalize_image_generation_size(...)` must use `get_runtime_settings().image_generation_max_dimension` unless a focused
  caller/test passes an explicit `max_dimension`.
- Custom dimensions must be validated and calibrated by the backend before provider calls, not only by frontend controls.
- Continuous image generation and workflow image-generation nodes must use the same backend normalizer so their accepted/rejected sizes do not drift.
- Provider adapters should receive the normalized string unchanged unless a provider-specific adapter explicitly documents a conversion.

#### 4. Validation & Error Matrix

- Bad syntax such as `1024`, `1024*1024`, or missing dimensions -> request/config validation error.
- Non-positive dimensions such as `0x1024` or `1024x-1` -> request/config validation error.
- Dimensions above the project safety bounds -> normalize to a safe calibrated `WIDTHxHEIGHT` before provider dispatch.
- Uppercase separators/digits such as `3840X2160` -> normalize to lowercase `3840x2160`.
- `image_generation_max_dimension < 512` or `> 8192` -> settings validation error.
- Invalid runtime default image dimensions -> settings validation error instead of silently publishing broken provider defaults.

#### 5. Good/Base/Bad Cases

- Good: `3840x2160` entered in the workflow Inspector is saved as `3840x2160` and reaches the image provider as `image_size="3840x2160"`.
- Base: built-in presets such as `1024x1024`, `2048x2048`, and `3840x3840` render as picker buttons and submit the same canonical string.
- Bad: continuous image sessions accept custom sizes while workflow nodes only apply a loose string normalizer.
- Bad: frontend checks dimensions but backend forwards an oversized custom value to the provider.

#### 6. Tests Required

- Continuous image API tests must cover accepted custom dimensions, rejected malformed/non-positive dimensions, and
  oversized dimensions being stored as calibrated safe output sizes.
- Workflow DAG/API tests must cover node create/update normalization, invalid config rejection, provider input receiving
  the configured custom size, and oversized config being persisted as calibrated safe output size.
- Runtime settings tests must cover invalid default image dimensions and `image_generation_max_dimension` validation when
  validation behavior changes.
- Frontend helper/component tests should cover preset parsing, duplicate normalization, and custom-size round-tripping when picker logic changes.

#### 7. Wrong vs Correct

Wrong:

```python
# Workflow accepts any parseable size while continuous image uses different validation.
size = normalize_image_size(config_json.get("size", "1024x1024"))
```

Correct:

```python
# All image generation entry points share the same safety bounds and canonical form.
size = normalize_image_generation_size(config_json.get("size", "1024x1024"))
```

Wrong:

```tsx
<input value={draft.size} onChange={(event) => onChange({ size: event.target.value })} />
```

Correct:

```tsx
<ImageSizePicker value={draft.size} onChange={(size) => onChange({ size })} presets={imageSizePresets} />
```

### Preserve workflow-level tests

`backend/tests/test_*.py` is the backend regression suite and is split by behavior area. It covers:

- Auth/session behavior.
- Settings API persistence and validation.
- Typed business error and legacy `ValueError` HTTP mapping.
- SQLAlchemy enum value storage.
- End-to-end product/copy/poster workflow.
- Reference image upload/deletion.
- Continuous image-session behavior.
- Alembic upgrade path.
- OpenAI Responses image provider parsing behavior.

When changing product, copy, poster, settings, upload, image-session, provider, or migration behavior, add or update tests
in the matching topic file. Keep cross-cutting builders and polling/login helpers in `backend/tests/helpers.py` rather
than reintroducing a giant all-purpose test module.

When extracting workflow graph business rules, add at least one DB-free unit test for the domain rule in addition to any
API/integration regression. The application/query layer should own SQLAlchemy artifact existence checks; the domain rule
should own pure graph decisions.

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
- Continuous image-session generation follows the same durable-delivery rule with
  `image_session_generation_tasks` and `recover_unfinished_image_session_generation_tasks(...)`: `POST
  /api/image-sessions/{id}/generate` creates a queued DB task and returns `202`; worker execution creates the existing
  `image_session_rounds` / `image_session_assets` rows on success, and duplicate terminal messages must no-op.

Preserve these semantics when editing job code.

#### Scenario: Durable async continuous image-session generation

##### 1. Scope / Trigger

- Trigger: changing `POST /api/image-sessions/{id}/generate`, image-session generation persistence, queue delivery,
  worker execution, admission control, or frontend session detail polling for continuous image chat.
- Continuous image-session generation is a cross-layer async workflow: DB task rows are authoritative, Dramatiq/Redis is
  delivery only, and the frontend reconstructs queued/running/failed state from `ImageSessionDetail`.

##### 2. Signatures

- API: `POST /api/image-sessions/{image_session_id}/generate` returns `202 Accepted` after validation, durable task
  creation, and enqueue; it must not wait for an image provider call.
- DB: `image_session_generation_tasks` stores `session_id`, `prompt`, `size`, `base_asset_id`,
  `selected_reference_asset_ids`, `generation_count`, `status`, `failure_reason`, `result_generation_group_id`,
  `attempts`, `is_retryable`, `created_at`, `started_at`, and `finished_at`.
- Queue: `enqueue_image_session_generation_task(task_id: str)` sends the durable task ID; the worker actor consumes only
  the ID and reloads state from the database.
- Recovery: `recover_unfinished_image_session_generation_tasks(reset_stale_running: bool = False, stale_running_after:
  timedelta = 30 minutes)` re-sends queued tasks and, only for worker startup, may reset stale running tasks to queued.
- Response DTO: `ImageSessionDetailResponse.generation_tasks` exposes task summaries so route entry/refresh can show
  active or failed generation work without a separate orchestration endpoint.
- Each generation task summary includes global queue fields: `queue_active_count`, `queue_running_count`,
  `queue_queued_count`, `queue_max_concurrent_tasks`, `queued_ahead_count`, and `queue_position`.
- Queue overview API: `GET /api/generation-queue` returns `active_count`, `running_count`, `queued_count`, and
  `max_concurrent_tasks` for product/workflow surfaces that do not own a specific image-session task.

##### 3. Contracts

- Task statuses are `queued`, `running`, `succeeded`, and `failed`; queued/running rows count toward
  `generation_max_concurrent_tasks`.
- Queue position is computed from durable queued DB rows ordered by `created_at` across queued product jobs and
  image-session generation tasks. Running tasks have no queue position and should be displayed as front-of-queue work.
- New image-session generation work must call shared admission before creating a queued task. The count must be based on
  active DB rows, not an in-process slot, because API/worker processes may be replicated.
- Queue enqueue failure after task creation must mark the task `failed` (or otherwise return a stable `503`) before the
  route responds; do not strand a queued row that no worker can consume.
- Worker claim must be atomic at the database boundary: update `queued -> running` with a status condition and no-op when
  the row is terminal, already running, or already claimed by another worker.
- On success, create normal `image_session_assets` and `image_session_rounds` rows. Multi-candidate generations still use
  one `generation_group_id` with one round/asset per candidate.
- On provider/storage/runtime failure, mark the task `failed` with a generic safe user-facing reason such as
  `图片生成失败，请稍后重试`; never expose provider exception text, API keys, base URLs, local paths, request bodies, or
  tracebacks in API responses.
- The shared public demo workspace stays shared; do not add user/tenant ownership checks as part of this async path unless
  a separate product requirement introduces isolation.

##### 4. Validation & Error Matrix

- Missing session -> `404`, `连续生图会话不存在`.
- Invalid `generation_count`, `size`, `base_asset_id`, or selected references -> existing image-session validation errors;
  do not enqueue a task.
- Active generation cap reached -> `429`, `当前生成任务较多，请稍后再试`; no new task row should be created for that request.
- Redis/Dramatiq send failure after DB task creation -> mark task `failed`, then return `503`,
  `任务队列暂不可用，请稍后重试`.
- Duplicate Redis message for `succeeded`, `failed`, or `running` task -> no provider call and no new round/asset rows.
- API restart with queued retryable task -> re-enqueue without changing task semantics.
- Worker restart with stale running retryable task -> reset to queued, clear stale running timestamps as needed, and
  re-enqueue.

##### 5. Good/Base/Bad Cases

- Good: user requests 3 candidates; route returns `202` quickly with a queued task, worker later creates 3 generated
  assets and 3 rounds sharing one `generation_group_id`, then marks the task `succeeded`.
- Good: browser refreshes during generation; `ImageSessionDetail.generation_tasks` still contains queued/running task
  state, so the frontend resumes polling and disables duplicate submission.
- Base: worker receives the same task ID after the task already succeeded; it exits without calling the provider.
- Bad: route calls `generate_image_session_round(...)` or an image provider directly and holds the HTTP request open.
- Bad: active generation cap checks a process-local counter or lock; replicated API processes can exceed the public demo
  cap.
- Bad: worker claim reads a queued task, mutates the ORM object, and commits without a conditional `WHERE status='queued'`;
  concurrent duplicate messages may both call the provider.

##### 6. Tests Required

- Route/API test: submit generation returns `202`, persists a queued task, exposes the task in session detail, and does not
  call the provider synchronously.
- Enqueue failure test: mocked send failure marks the task failed and returns stable `503`.
- Worker success test: executing a queued task creates expected assets/rounds and marks the task succeeded with
  `result_generation_group_id`.
- Worker failure test: provider exception marks the task failed with a generic reason and does not leak the raw exception.
- Duplicate/no-op tests: terminal and already-running task messages do not call the provider or create extra rounds.
- Recovery tests: queued tasks are re-sent; stale running tasks reset only when `reset_stale_running=True`.
- Admission test: queued/running image-session generation tasks count toward `generation_max_concurrent_tasks`.
- Queue metadata test: queued task responses expose `queued_ahead_count` / `queue_position`, and the global overview
  counts active queued/running durable work.
- Frontend gate: update DTO types and run `just web-build` when `ImageSessionDetail` or task status rendering changes.

##### 7. Wrong vs Correct

Wrong:

```python
# HTTP request waits for provider and uses process-local admission state.
with admit_synchronous_generation(session):
    detail = generate_image_session_round(session, image_session_id, request.prompt, storage=storage)
```

Correct:

```python
# HTTP request persists durable work, sends delivery message, and returns 202.
task = create_image_session_generation_task(session, image_session_id, request)
enqueue_image_session_generation_task(task.id)
```

Wrong:

```python
task = session.get(ImageSessionGenerationTask, task_id)
if task.status == ImageSessionGenerationStatus.QUEUED:
    task.status = ImageSessionGenerationStatus.RUNNING
    session.commit()
    call_provider()
```

Correct:

```python
updated = session.execute(
    update(ImageSessionGenerationTask)
    .where(
        ImageSessionGenerationTask.id == task_id,
        ImageSessionGenerationTask.status == ImageSessionGenerationStatus.QUEUED,
    )
    .values(status=ImageSessionGenerationStatus.RUNNING, started_at=now_utc())
)
if updated.rowcount != 1:
    return
call_provider()
```

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
