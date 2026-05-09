# Backend Error Handling

> Actual error propagation and API error response patterns used by ProductFlow.

---

## Overview

ProductFlow keeps business validation in the application layer and HTTP status mapping in the presentation layer.
API error details are currently Chinese strings because the private workspace UI is Chinese.

Key files:

- `backend/src/productflow_backend/application/use_cases.py`
- `backend/src/productflow_backend/application/image_sessions.py`
- `backend/src/productflow_backend/presentation/errors.py`
- `backend/src/productflow_backend/presentation/routes/products.py`
- `backend/src/productflow_backend/presentation/routes/image_sessions.py`
- `backend/src/productflow_backend/presentation/routes/settings.py`
- `backend/src/productflow_backend/presentation/upload_validation.py`
- `backend/src/productflow_backend/presentation/routes/auth.py`

---

## Business Errors

Application use cases raise typed business errors for expected failures where the HTTP status is part of the business
semantics. Keep the error classes below the presentation layer and map them to HTTP only at the route boundary.

Key class home:

- `backend/src/productflow_backend/domain/errors.py`

Current typed errors:

- `BusinessError`: base class for expected user-facing failures, defaults to `400`.
- `BusinessValidationError`: explicit `400` for valid HTTP requests that are invalid for the current workflow state or
  selected resource.
- `NotFoundError`: explicit `404` for missing domain/application resources.
- `ResourceBusyError`: explicit `429` for future hard resource boundaries that cannot be represented as durable queued
  work. Current durable generation submissions should queue instead of using this error for a full running-capacity slot.
- `QueueUnavailableError`: explicit `503` when a durable task row was created but Redis/Dramatiq delivery failed.

Typed business errors intentionally subclass `ValueError` for Python compatibility with older code paths, but HTTP
mapping must not rely on raw `ValueError` catches or message suffixes.

Use typed errors for newly touched expected failures:

- Missing records: `_get_product_or_raise(...)` raises `NotFoundError("商品不存在")` in `application/use_cases.py`.
- Missing workflow/session resources such as products, workflows, nodes, edges, image sessions, source assets, copy sets,
  and poster variants should use `NotFoundError`.
- Explicit workflow validation such as the missing poster file case raises `BusinessValidationError("海报文件不存在")`
  so it remains a `400` without a string-content exception in typed mapping.
- Workflow graph integrity or selection problems that were legacy `400`s, such as an edge referencing a node outside the
  loaded graph, should use `BusinessValidationError` rather than `NotFoundError`.

The legacy route-level `ValueError` fallback has been removed from production code after the high-traffic route migration.
Newly touched application/business failures must use typed errors. Parser, provider, and internal normalization helpers
may still raise raw `ValueError` when they do not own route-facing HTTP status semantics.

FastAPI registers a global handler for typed `BusinessError` subclasses during app creation. Route handlers may let typed
business errors propagate directly; the response remains the standard FastAPI-compatible shape:

```json
{"detail": "<message>"}
```

Do not add global handlers for raw `ValueError` or `Exception`.

---

## Route-Level Mapping

`presentation/errors.py::register_exception_handlers(...)` is called from `presentation/api.py::create_app(...)` and
registers the `BusinessError` HTTP boundary. This handler maps only typed expected business failures.

Do not reintroduce a shared route adapter that maps raw `ValueError` to HTTP by Chinese string content. If a route-facing
use case still exposes a raw expected business failure, convert the owner use case to `BusinessValidationError`,
`NotFoundError`, `QueueUnavailableError`, or another existing typed `BusinessError` subclass.

Examples:

- `presentation/routes/product_workflows.py` lets typed business errors propagate through the global handler after the
  workflow use cases were inventoried as typed at the route boundary.
- `presentation/routes/products.py`, `presentation/routes/image_sessions.py`, and `presentation/routes/gallery.py` let
  typed business errors propagate through the global handler after their route-facing failures were inventoried as typed.
- Download/file-serving routes may still catch `ValueError` from `LocalStorage.resolve_for_variant(...)` and raise direct
  `HTTPException(404)` because the route owns file-serving semantics.
- Settings routes may still translate local configuration normalization `ValueError` into direct `HTTPException(400)`
  because settings unlock/runtime validation is presentation-owned.

When adding or touching use cases, prefer `BusinessValidationError` / `NotFoundError` over raw `ValueError` for expected
business failures. Keep using direct `HTTPException` for HTTP-owned protocol boundaries such as auth/session, settings
unlock, upload validation, and download/file serving. Do not import FastAPI `HTTPException` into application modules.

### Scenario: Typed business errors at the route boundary

#### 1. Scope / Trigger

- Trigger: adding or touching expected application/business failures that routes convert into API errors.
- Applies to `application/` use cases, `domain/errors.py`, and `presentation/errors.py`.

#### 2. Signatures

- `BusinessError(message: str)` -> default `status_code = 400`.
- `BusinessValidationError(message: str)` -> `status_code = 400`.
- `NotFoundError(message: str)` -> `status_code = 404`.
- `presentation.errors.register_exception_handlers(app: FastAPI) -> None`.
- `presentation.errors.business_error_exception_handler(request: Request, exc: BusinessError) -> JSONResponse`.

#### 3. Contracts

- API response shape remains FastAPI standard `{"detail": "<message>"}`.
- `detail` is `str(exc)` / the Chinese user-facing message.
- Typed error subclasses may carry status semantics internally, but routes must not add frontend-visible `code` fields
  unless a future cross-layer task updates frontend handling.

#### 4. Validation & Error Matrix

- `NotFoundError("商品不存在")` -> `404`, `{"detail": "商品不存在"}`.
- `BusinessValidationError("海报文件不存在")` -> `400`, `{"detail": "海报文件不存在"}`.
- `BusinessValidationError("工作流连线引用了不存在的节点")` -> `400`,
  `{"detail": "工作流连线引用了不存在的节点"}`.
- `BusinessError("请选择一张图片")` -> `400`, `{"detail": "请选择一张图片"}`.
- `QueueUnavailableError("任务队列暂不可用，请稍后重试")` -> `503`,
  `{"detail": "任务队列暂不可用，请稍后重试"}`.
- Raw `ValueError("旧资源不存在")` must not be globally mapped to HTTP. Convert the owning route-facing use case to a typed
  business error instead.

#### 5. Good/Base/Bad Cases

- Good: `_get_product_or_raise(...)` raises `NotFoundError("商品不存在")`.
- Base: provider/Pydantic payload normalization may still raise `ValueError` inside parsing boundaries; route-facing
  business use cases should raise typed errors.
- Bad: adding a new `if detail.endswith(...)` or exact Chinese string branch for newly converted typed errors.

#### 6. Tests Required

- Unit test typed `NotFoundError("资源已移除")` maps to `404` even without an `"不存在"` suffix.
- Unit test generic `BusinessError` maps to `400`.
- Unit test `QueueUnavailableError` maps to `503`.
- Route/global-handler test typed `BusinessError` preserves response shape `{"detail": "..."}` and does not add a `code`
  field.
- Unit or route test poster-file missing remains `400` with detail `"海报文件不存在"`.
- Application-level test newly touched business validations raise `BusinessValidationError`, for example product field
  validation, workflow graph validation, and image-session generation validation.
- Regression test route surfaces that used to have wrappers, such as product detail, image-session detail/generation, and
  gallery save, use the global typed handler.
- Regression test the legacy raw `ValueError` helper is absent from `presentation.errors`.

#### 7. Wrong vs Correct

Wrong:

```python
if detail.endswith("不存在"):
    raise HTTPException(status_code=404, detail=detail)
```

Correct:

```python
if product is None:
    raise NotFoundError("商品不存在")
```

---

## Authentication and Session Errors

`presentation/deps.py::require_admin` protects private API routes with a session flag when
`get_runtime_settings().admin_access_required` is true. It raises:

- `401` with detail `"请先登录"` when the session is not authenticated.

When `admin_access_required` is false, `require_admin` allows private workspace routes without the admin login session.
This does not bypass `presentation/routes/settings.py::require_settings_unlocked`; full settings reads/writes still require
the independent `SETTINGS_ACCESS_TOKEN` unlock.

`presentation/routes/auth.py::create_session` compares the submitted admin key with `Settings.admin_access_key` while
login is required and raises:

- `401` with detail `"管理员密钥不正确"` for an invalid key.

When login is disabled, `POST /api/auth/session` is a harmless no-op success and leaves the current session untouched. `GET
/api/auth/session` returns `authenticated=true` and `access_required=false`; after login is re-enabled, an unauthenticated
session again returns `authenticated=false` and `access_required=true`.

Routes that require auth use `dependencies=[Depends(require_admin)]` on the router, for example
`presentation/routes/products.py`, `presentation/routes/image_sessions.py`, `presentation/routes/product_workflows.py`,
and `presentation/routes/settings.py`.

---

## Upload and Resource Boundary Errors

`presentation/upload_validation.py` raises `HTTPException` directly because it owns HTTP-specific upload status codes:

- `415` for unsupported declared or detected MIME types.
- `413` when the uploaded image exceeds `upload_max_image_bytes`.
- `400` for empty files, undecodable images, pixel count overflow, or declared/detected MIME mismatch.

Routes call `read_validated_image_upload(...)` before delegating to application use cases. Do not duplicate image decoding
or byte-size checks in individual route handlers.

---

## Queue and Durable Task Errors

Application submit use cases create durable work first, then enqueue through `infrastructure/queue.py`:

- `application/product_workflow/execution.py::submit_product_workflow_run(...)` creates/reuses `WorkflowRun` rows,
  enqueues when `_workflow_run_should_enqueue(...)` says delivery is needed, and marks enqueue failures through
  `mark_workflow_run_enqueue_failed(...)`.
- `application/image_sessions.py::submit_image_session_generation_task(...)` creates the
  `ImageSessionGenerationTask`, enqueues it, and marks enqueue failures through
  `mark_image_session_generation_task_enqueue_failed(...)`.
- All submit use cases raise `QueueUnavailableError("任务队列暂不可用，请稍后重试")` after marking persisted state failed.
  The typed business error handler preserves status `503` plus the stable FastAPI error shape
  `{"detail": "任务队列暂不可用，请稍后重试"}`.
- Worker actors in `backend/src/productflow_backend/workers.py` use `@dramatiq.actor(max_retries=0)` and rely on
  application execution entrypoints to persist failure/retry state.

Keep queue send failures visible to the API caller; do not leave a `QUEUED` durable task silently unenqueued. Route
modules should remain HTTP adapters: they call the submit use case, let typed `BusinessError`s reach the global handler,
and serialize the returned model.

Public-demo durable generation entrypoints persist queued work before provider execution:

- workflow run kickoff
- continuous image-session generation

For idempotent routes that can return an already-active `WorkflowRun`, check/reuse the existing active record before
creating another durable run. The global cap protects provider/worker execution, not durable backlog creation; submissions
must remain able to create queued work and re-enqueue stranded active workflow runs while all running slots are occupied.

When a worker sees the running cap is reached, leave the durable task queued, do not call the provider, and schedule a
delayed delivery retry. Do not leak queue, Redis, provider, or filesystem exception strings to users. Provider messages may
be persisted only after sanitization and categorization. Common provider/network failures should map to concise
user-facing categories: rate limit/quota, content-policy refusal, connection interruption, request timeout, unsupported
parameters, and provider 5xx/service failure. Safe, actionable uncategorized details such as unsupported dimensions can be
shown with a `图片生成失败：...` prefix, while messages containing API keys, tokens, base URLs, prompts, request bodies,
file paths, or tracebacks must fall back to the generic queue/provider failure detail.

### Scenario: Continuous image-session worker partial-success and timeout handling

#### 1. Scope / Trigger

- Trigger: changing continuous image-session generation, Dramatiq worker actors, generation task recovery, or provider
  failure handling for `ImageSessionGenerationTask`.
- This is an infra + database contract because PostgreSQL is the authoritative task/result state while Redis/Dramatiq is
  only delivery.

#### 2. Signatures

- Durable task table: `image_session_generation_tasks`.
- Result tables: `image_session_assets` and `image_session_rounds`.
- Worker actor: `workers.run_image_session_generation_task(task_id: str)`.
- Application entrypoint: `application.image_sessions.execute_image_session_generation_task(task_id: str) -> None`.
- Continuous generation task statuses use existing `JobStatus`: `queued`, `running`, `succeeded`, `failed`.
- Stale-running recovery uses a heartbeat/idle model: compare the cutoff with
  `ImageSessionGenerationTask.progress_updated_at`, falling back to `started_at` for older rows that do not have progress
  metadata. The user-facing runtime setting is `image_session_stale_running_after_minutes`, defaulting to 90 minutes.
- The Dramatiq actor keeps only an internal worker failsafe `time_limit` from
  `image_session_worker_failsafe_time_limit_minutes`; this is not the product-level timeout decision.

#### 3. Contracts

- A queued generation task may transition only through worker-owned execution:
  - `queued` -> `running` only when `_mark_image_generation_task_running(...)` wins the compare-and-set update and the
    global running generation capacity still has a free slot.
  - `running` -> `succeeded` after all requested candidates have been saved.
  - `running` -> `failed` after provider failure, timeout, or other safe-to-handle worker exception.
- The global generation cap has one DB-backed execution gate: worker claims check current `running` work before entering
  provider execution, so existing queued backlog can grow without allowing provider calls to exceed the configured cap.
- PostgreSQL runtime must serialize worker-claim capacity checks with the transaction advisory lock. SQLite tests may skip
  the advisory lock, but production worker processes must not rely on in-process counters.
- When a worker sees no running capacity for a queued task, it must leave the task `queued`, keep `attempts` unchanged,
  set progress to a waiting state, and schedule a delayed delivery retry. It must not call the provider or mark the task
  failed just because capacity is currently full.
- Multi-candidate image-session generation must persist each successful candidate independently:
  - save generated bytes under storage;
  - insert `image_session_assets`;
  - insert matching `image_session_rounds`;
  - commit that candidate before requesting the next candidate.
- If a later candidate fails or times out, earlier committed candidates remain visible and keep one
  `generation_group_id`.
- Running tasks must persist durable progress metadata while they advance:
  - `completed_candidates`;
  - `active_candidate_index`;
  - `progress_phase`;
  - `progress_updated_at`;
  - current `provider_response_id` / `provider_response_status` when available.
- OpenAI Responses image generation should use background response creation and `responses.retrieve(...)` polling when the
  provider supports it. Each provider status response should refresh task progress while generation is still working.
- Provider progress metadata fields are nullable because queued, legacy, failed-before-provider, and capacity-waiting tasks
  may not have provider state. Writers are `application/image_sessions.py` worker progress helpers and
  `infrastructure/queue.py` recovery helpers; readers are image-session status/detail serializers and recovery queries.
  Serializers must pass through `None` for missing old rows rather than inventing placeholder values.
- `progress_metadata` is a compact backend-owned snapshot for UI/debug display. Current keys are optional and may include
  `provider_response`, `candidate_index`, `candidate_count`, `generated_asset_id`, and `round_id`; code that reads it must
  tolerate missing keys and non-provider tasks.
- Failure/retry settlement must update `ImageSessionGenerationTask` independently from the parent `ImageSession`. Parent
  `updated_at`/title touches should use tolerant SQL `UPDATE image_sessions ... WHERE id = ...`-style updates instead of
  requiring a live parent ORM instance, because the session row may have been deleted or a previously loaded ORM row may
  be stale while a worker is settling provider failure.
- Worker-owned failed/timeout tasks use application-level retry instead of Dramatiq actor retries:
  - `workers.run_image_session_generation_task` must keep `max_retries=0`.
  - each worker claim increments `ImageSessionGenerationTask.attempts`.
  - while `attempts` is below the application cap, failure resets the same task to `queued` and re-enqueues it.
  - after the cap is reached, the task becomes `failed`, sets `finished_at`, and remains `is_retryable=true` so the
    owning image session can expose a manual retry action.
  - a sanitized safe detail, generic detail, or partial-success `failure_reason` is stored only on the terminal failed
    state.
  - `completed_candidates` and `result_generation_group_id` must be preserved when at least one candidate was already
    persisted.
- `KeyboardInterrupt` and `SystemExit` must still propagate. Other `BaseException` subclasses raised by Dramatiq time
  limits should be converted into durable task failure state before returning.

#### 4. Validation & Error Matrix

- All candidates succeed -> task `succeeded`, `failure_reason = null`, `result_generation_group_id` set.
- Candidate 1 succeeds, candidate 2 provider call fails -> task `failed`, first round/asset remains, failure reason
  mentions partial completion without provider secrets.
- Candidate 1 succeeds, candidate 2 raises `TimeLimitExceeded` -> task `failed`, first round/asset remains, failure
  reason is `"已生成 1/2 张候选，但任务超时，剩余候选未完成。"`.
- Stale running task with fresh `progress_updated_at` but old `started_at` -> remains `running`; recovery must not reset it.
- Stale running task with already completed candidates -> recovery marks `failed` with the partial-timeout reason and does
  not auto-requeue, because the worker's in-flight provider state is unknown.
- Timeout or safe worker exception before any candidate is committed -> task `failed`, no rounds/assets, sanitized safe
  detail or generic `"图片生成失败，请稍后重试"` only after the automatic retry cap is reached.
- Queued task consumed while global running capacity is full -> task remains `queued`, `attempts` stays unchanged,
  provider is not called, progress phase becomes a waiting-for-capacity state, and the task is re-enqueued with delay.
- Partial failure after candidate 1 of 2 -> automatic retry preserves the existing generation group and resumes at
  candidate 2 without calling the provider for candidate 1 again.
- Duplicate worker message for a terminal task -> no-op; do not call the provider again.
- Duplicate worker message for an already running non-stale task -> no-op; recovery handles stale running tasks separately.
- Provider failure after the parent `ImageSession` has disappeared or become stale -> worker still marks the durable
  task failed/retryable when the task row exists; no unhandled `StaleDataError` should escape the actor.

#### 5. Good/Base/Bad Cases

- Good: a four-candidate request commits candidate 1, 2, and 3 before candidate 4 starts; if candidate 4 times out, the
  UI can still show the first three candidates and `has_active_generation_task` becomes false.
- Base: a one-candidate provider failure stores a generic failed task with no generated rounds.
- Bad: wrapping the entire multi-candidate loop in one transaction; this loses already-paid successful images when a later
  provider call fails.
- Bad: checking global capacity only when the HTTP request creates the queued row; restart recovery, automatic retry, or
  multiple Dramatiq messages can still let too many queued tasks enter provider execution.
- Bad: letting `TimeLimitExceeded` escape without task cleanup; this leaves `running` rows that block the UI and may be
  re-enqueued after restart.
- Bad: retrying a partial-success task from candidate 1 with a new generation group; this duplicates provider spend and
  creates confusing duplicate candidates.
- Bad: enabling generic Dramatiq actor retries; this bypasses the database retry cap and can duplicate work outside the
  application-level state machine.
- Bad: touching `task.session.updated_at` or a loaded `ImageSession` ORM object during failure settlement; stale/deleted
  parents can make task failure persistence itself fail.

#### 6. Tests Required

- Worker test: partial success followed by `TimeLimitExceeded` keeps the committed round/asset, auto-retries the same task,
  resumes at the remaining candidate, and does not duplicate the saved candidate.
- Worker test: repeated provider failure stops at the application retry cap, marks the task `failed`, exposes either the
  sanitized safe detail or generic safe reason as appropriate, and leaves `is_retryable=true`.
- Worker test: wrapped provider exceptions still inspect the exception chain so rate limits, content-policy refusals,
  connection interruptions, provider 5xx errors, and unsupported-parameter failures do not collapse into the outer generic
  request-failure text.
- Worker test: timeout outside the per-candidate loop still marks the task `failed` with the generic safe reason.
- Worker progress test: provider polling callbacks update durable progress fields while the task remains running.
- Worker test: provider failure still settles the task when the parent `ImageSession` row is missing/stale.
- Worker capacity test: if another workflow/image task already fills the global running cap, executing a queued
  `ImageSessionGenerationTask` leaves it queued, does not increment attempts, does not call the provider, and schedules a
  delayed requeue.
- Worker actor test: `run_image_session_generation_task` has an internal failsafe `time_limit`; user-facing timeout
  behavior is covered by stale-running idle recovery tests.
- Existing duplicate-message tests must continue proving terminal/running tasks do not call the provider again.

#### 7. Wrong vs Correct

Wrong:

```python
try:
    for _ in range(generation_count):
        save_candidate_without_commit()
    session.commit()
except Exception:
    session.rollback()
    raise
```

Correct:

```python
for candidate_index in range(1, generation_count + 1):
    try:
        save_candidate()
        session.commit()
    except BaseException as exc:
        session.rollback()
        mark_task_failed_without_retry(...)
        return
```

Wrong:

```python
if task.status == JobStatus.QUEUED:
    task.status = JobStatus.RUNNING
    session.commit()
    call_provider()
```

Correct:

```python
if not generation_running_capacity_available(session):
    keep_task_queued_and_reenqueue_later()
    return
claim_queued_task_as_running()
call_provider()
```

---

## Provider and Runtime Errors

Provider-specific API errors are handled inside application/provider code and persisted on durable workflow or
image-session task rows for async product flows.

`config.py::_load_database_config_overrides()` intentionally tolerates missing `app_settings` tables during fresh startup
by returning `{}` for operational/programming SQLAlchemy errors, but it re-raises unexpected non-SQLAlchemy exceptions.

---

## API Error Shape

FastAPI returns the standard shape:

```json
{"detail": "..."}
```

The frontend API wrapper in `web/src/lib/api.ts` expects this shape and throws `ApiError(status, detail)`. Keep `detail`
plain and user-readable; do not return stack traces or provider secrets.

---

## Avoid

- Raising `HTTPException` from `application/` or `infrastructure/` modules.
- Swallowing queue/provider/storage failures without updating job state or returning a meaningful HTTP error.
- Returning raw exception strings that may include API keys, paths outside storage root, or provider request bodies.
- Changing Chinese user-facing `detail` text without checking frontend pages that display it directly.
- Letting route-facing raw `ValueError`s reach the API boundary; convert expected business failures to typed
  `BusinessError` subclasses and leave parser/provider/internal `ValueError`s inside their owner boundaries.
- Adding new string-suffix status checks for converted business errors; add or reuse a typed `BusinessError` subclass
  instead.
