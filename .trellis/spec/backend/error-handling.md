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
- `ResourceBusyError`: explicit `429` when global provider/worker admission control is at capacity.
- `QueueUnavailableError`: explicit `503` when a durable task row was created but Redis/Dramatiq delivery failed.

Typed business errors intentionally subclass `ValueError` during the migration so existing route catches continue to
work, but new code should choose the typed class instead of relying on message suffixes.

Use typed errors for newly touched expected failures:

- Missing records: `_get_product_or_raise(...)` raises `NotFoundError("商品不存在")` in `application/use_cases.py`.
- Missing workflow/session resources such as products, workflows, nodes, edges, image sessions, source assets, copy sets,
  and poster variants should use `NotFoundError`.
- Explicit workflow validation such as the missing poster file case raises `BusinessValidationError("海报文件不存在")`
  so it remains a `400` without a string-content exception in typed mapping.
- Workflow graph integrity or selection problems that were legacy `400`s, such as an edge referencing a node outside the
  loaded graph, should use `BusinessValidationError` rather than `NotFoundError`.

Legacy application use cases may still raise `ValueError` for expected business failures while migration is incremental:

- Invalid edits or state transitions: helpers such as `_normalize_required_text(...)`, `_normalize_price(...)`, and
  copy/poster use cases raise `ValueError` with user-facing messages.

Routes catch these typed errors through the existing `ValueError` boundary and convert them to `HTTPException`.

---

## Route-Level Mapping

Resource route modules import the shared `presentation/errors.py::raise_value_error_as_http(...)` helper. Do not
redefine this mapping in each route file:

```python
try:
    product = get_product_detail(session, product_id)
except ValueError as exc:
    raise_value_error_as_http(exc)
```

The helper preserves the route-boundary contract:

- `BusinessError` subclasses map by explicit `status_code`, not by Chinese message content.
- legacy `ValueError("海报文件不存在")` remains `400` for compatibility.
- legacy `ValueError` messages ending with `"不存在"` remain `404` for compatibility.
- other expected legacy business `ValueError`s remain `400`.

Examples:

- `presentation/routes/products.py` catches `ValueError` around `create_product(...)`, `get_product_detail(...)`,
  `update_copy_set(...)`, `confirm_copy_set(...)`, and other use cases.
- `presentation/routes/image_sessions.py` catches `ValueError` around session CRUD, reference image upload/delete,
  generation, and attach-to-product actions.
- `presentation/routes/product_workflows.py` catches `ValueError` around workflow graph edits and run kickoff.

When adding or touching use cases, prefer `BusinessValidationError` / `NotFoundError` over raw `ValueError` for expected
business failures. Keep using the shared presentation helper at the route boundary. Do not import FastAPI
`HTTPException` into application modules.

### Scenario: Typed business errors at the route boundary

#### 1. Scope / Trigger

- Trigger: adding or touching expected application/business failures that routes convert into API errors.
- Applies to `application/` use cases, `domain/errors.py`, and `presentation/errors.py`.

#### 2. Signatures

- `BusinessError(message: str)` -> default `status_code = 400`.
- `BusinessValidationError(message: str)` -> `status_code = 400`.
- `NotFoundError(message: str)` -> `status_code = 404`.
- `presentation.errors.raise_value_error_as_http(exc: ValueError) -> NoReturn`.

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
- Legacy `ValueError("旧资源不存在")` -> `404` while migration is incomplete.
- Legacy `ValueError("海报文件不存在")` -> `400` for backward compatibility.
- Legacy `ValueError("普通业务错误")` -> `400`.

#### 5. Good/Base/Bad Cases

- Good: `_get_product_or_raise(...)` raises `NotFoundError("商品不存在")`.
- Base: existing normalization helpers may still raise `ValueError("价格格式不正确")` and map to `400`.
- Bad: adding a new `if detail.endswith(...)` or exact Chinese string branch for newly converted typed errors.

#### 6. Tests Required

- Unit test `raise_value_error_as_http(NotFoundError("资源已移除"))` returns `404` even without an `"不存在"` suffix.
- Unit test generic `BusinessError` returns `400`.
- Unit or route test poster-file missing remains `400` with detail `"海报文件不存在"`.
- Unit test legacy `ValueError` fallback preserves old `404` / `400` behavior.

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

- `application/product_workflow_execution.py::submit_product_workflow_run(...)` creates/reuses `WorkflowRun` rows,
  enqueues when `_workflow_run_should_enqueue(...)` says delivery is needed, and marks enqueue failures through
  `mark_workflow_run_enqueue_failed(...)`.
- `application/image_sessions.py::submit_image_session_generation_task(...)` creates the
  `ImageSessionGenerationTask`, enqueues it, and marks enqueue failures through
  `mark_image_session_generation_task_enqueue_failed(...)`.
- All submit use cases raise `QueueUnavailableError("任务队列暂不可用，请稍后重试")` after marking persisted state failed.
  Routes catch it through the normal `raise_value_error_as_http(...)` boundary and preserve status `503` plus the stable
  FastAPI error shape `{"detail": "任务队列暂不可用，请稍后重试"}`.
- Worker actors in `backend/src/productflow_backend/workers.py` use `@dramatiq.actor(max_retries=0)` and rely on
  application execution entrypoints to persist failure/retry state.

Keep queue send failures visible to the API caller; do not leave a `QUEUED` durable task silently unenqueued. Route
modules should remain HTTP adapters: they call the submit use case, map `ValueError`/`BusinessError` to HTTP, and
serialize the returned model.

Public-demo resource-consuming entrypoints must run through the shared generation admission check before creating new
provider/worker work:

- workflow run kickoff
- continuous image-session generation

For idempotent routes that can return an already-active `WorkflowRun`, check/reuse the existing active record before
applying admission control. The cap protects creation of new resource-consuming work; it must not block duplicate
submissions from seeing the already-created run or from re-enqueueing a stranded active workflow run.

When the cap is reached, return the stable FastAPI error shape with status `429` and detail
`"当前生成任务较多，请稍后再试"`. Do not leak queue, Redis, provider, or filesystem exception strings to users; persist or
return a generic queue/provider failure detail instead.

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
  - `queued` -> `running` when `_mark_image_generation_task_running(...)` wins the compare-and-set update.
  - `running` -> `succeeded` after all requested candidates have been saved.
  - `running` -> `failed` after provider failure, timeout, or other safe-to-handle worker exception.
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
- Failed/timeout tasks must set:
  - `status = failed`;
  - `finished_at` to an aware UTC timestamp;
  - `is_retryable = false`;
  - a generic or partial-success `failure_reason`;
  - `result_generation_group_id` when at least one candidate was already persisted.
- `KeyboardInterrupt` and `SystemExit` must still propagate. Other `BaseException` subclasses raised by Dramatiq time
  limits should be converted into durable task failure state before returning.

#### 4. Validation & Error Matrix

- All candidates succeed -> task `succeeded`, `failure_reason = null`, `result_generation_group_id` set.
- Candidate 1 succeeds, candidate 2 provider call fails -> task `failed`, first round/asset remains, failure reason
  mentions partial completion without provider secrets.
- Candidate 1 succeeds, candidate 2 raises `TimeLimitExceeded` -> task `failed`, first round/asset remains, failure
  reason is `"已生成 1/2 张候选，但任务超时，剩余候选未完成。"`.
- Stale running task with fresh `progress_updated_at` but old `started_at` -> remains `running`; recovery must not reset it.
- Stale running task with already completed candidates -> mark `failed` with the partial-timeout reason and do not retry.
- Timeout or safe worker exception before any candidate is committed -> task `failed`, no rounds/assets, generic
  `"图片生成失败，请稍后重试"`.
- Duplicate worker message for a terminal task -> no-op; do not call the provider again.
- Duplicate worker message for an already running non-stale task -> no-op; recovery handles stale running tasks separately.

#### 5. Good/Base/Bad Cases

- Good: a four-candidate request commits candidate 1, 2, and 3 before candidate 4 starts; if candidate 4 times out, the
  UI can still show the first three candidates and `has_active_generation_task` becomes false.
- Base: a one-candidate provider failure stores a generic failed task with no generated rounds.
- Bad: wrapping the entire multi-candidate loop in one transaction; this loses already-paid successful images when a later
  provider call fails.
- Bad: letting `TimeLimitExceeded` escape without task cleanup; this leaves `running` rows that block the UI and may be
  re-enqueued after restart.
- Bad: marking a partial-success task retryable; a restart could duplicate provider spend and create confusing duplicate
  candidates.

#### 6. Tests Required

- Worker test: partial success followed by `TimeLimitExceeded` keeps the committed round/asset, marks the task `failed`,
  sets `finished_at`, `is_retryable=false`, and clears active queue counts.
- Worker test: timeout outside the per-candidate loop still marks the task `failed` with the generic safe reason.
- Worker progress test: provider polling callbacks update durable progress fields while the task remains running.
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
- Treating all `ValueError`s as 500s; existing route helpers intentionally map them to 400/404.
- Adding new string-suffix status checks for converted business errors; add or reuse a typed `BusinessError` subclass
  instead.
