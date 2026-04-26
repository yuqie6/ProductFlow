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

Typed business errors intentionally subclass `ValueError` during the migration so existing route catches continue to
work, but new code should choose the typed class instead of relying on message suffixes.

Use typed errors for newly touched expected failures:

- Missing records: `_get_product_or_raise(...)` raises `NotFoundError("商品不存在")` in `application/use_cases.py`.
- Missing workflow/session resources such as products, workflows, nodes, edges, image sessions, source assets, copy sets,
  poster variants, and jobs should use `NotFoundError`.
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
  `update_copy_set(...)`, `create_poster_job(...)`, and other use cases.
- `presentation/routes/image_sessions.py` catches `ValueError` around session CRUD, reference image upload/delete,
  generation, and attach-to-product actions.
- `presentation/routes/product_workflows.py` catches `ValueError` around workflow graph edits and run kickoff.
- `presentation/routes/jobs.py` maps missing jobs to 404 directly.

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

`presentation/deps.py::require_admin` protects private API routes with a session flag and raises:

- `401` with detail `"请先登录"` when the session is not authenticated.

`presentation/routes/auth.py::create_session` compares the submitted admin key with `Settings.admin_access_key` and raises:

- `401` with detail `"管理员密钥不正确"` for an invalid key.

Routes that require auth use `dependencies=[Depends(require_admin)]` on the router, for example
`presentation/routes/products.py`, `presentation/routes/image_sessions.py`, `presentation/routes/jobs.py`, and
`presentation/routes/settings.py`.

---

## Upload and Resource Boundary Errors

`presentation/upload_validation.py` raises `HTTPException` directly because it owns HTTP-specific upload status codes:

- `415` for unsupported declared or detected MIME types.
- `413` when the uploaded image exceeds `upload_max_image_bytes`.
- `400` for empty files, undecodable images, pixel count overflow, or declared/detected MIME mismatch.

Routes call `read_validated_image_upload(...)` before delegating to application use cases. Do not duplicate image decoding
or byte-size checks in individual route handlers.

---

## Queue and Job Errors

Product routes create job records first, then enqueue through `infrastructure/queue.py`:

- If enqueueing a copy/poster job fails, `presentation/routes/products.py` catches the exception,
  calls `mark_job_enqueue_failed(...)`, and raises `503` with detail `"任务队列暂不可用，请稍后重试"`.
- Worker actors in `backend/src/productflow_backend/workers.py` use `@dramatiq.actor(max_retries=0)` and rely on
  `execute_copy_job(...)` / `execute_poster_job(...)` to persist failure/retry state.

Keep queue send failures visible to the API caller; do not leave a `QUEUED` job silently unenqueued.

Public-demo resource-consuming entrypoints must run through the shared generation admission check before creating new
provider/worker work:

- copy jobs
- poster jobs and poster regeneration
- workflow run kickoff
- synchronous image-session generation

For idempotent routes that can return an already-active `JobRun` or `WorkflowRun`, check/reuse the existing active record
before applying admission control. The cap protects creation of new resource-consuming work; it must not block duplicate
submissions from seeing the already-created job/run or from re-enqueueing a stranded active workflow run.

When the cap is reached, return the stable FastAPI error shape with status `429` and detail
`"当前生成任务较多，请稍后再试"`. Do not leak queue, Redis, provider, or filesystem exception strings to users; persist or
return a generic queue/provider failure detail instead.

---

## Provider and Runtime Errors

Provider-specific API errors are handled inside application/provider code and persisted on `JobRun.failure_reason` for
async product flows. Continuous image sessions are currently synchronous; `presentation/routes/image_sessions.py` converts
`RuntimeError` from generation into `400`.

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
