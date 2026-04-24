# Backend Error Handling

> Actual error propagation and API error response patterns used by ProductFlow.

---

## Overview

ProductFlow keeps business validation in the application layer and HTTP status mapping in the presentation layer.
API error details are currently Chinese strings because the private workspace UI is Chinese.

Key files:

- `backend/src/productflow_backend/application/use_cases.py`
- `backend/src/productflow_backend/application/image_sessions.py`
- `backend/src/productflow_backend/presentation/routes/products.py`
- `backend/src/productflow_backend/presentation/routes/image_sessions.py`
- `backend/src/productflow_backend/presentation/routes/settings.py`
- `backend/src/productflow_backend/presentation/upload_validation.py`
- `backend/src/productflow_backend/presentation/routes/auth.py`

---

## Business Errors

Application use cases raise `ValueError` for expected business failures:

- Missing records: `_get_product_or_raise(...)` raises `ValueError("商品不存在")` in `application/use_cases.py`.
- Invalid edits or state transitions: helpers such as `_normalize_required_text(...)`, `_normalize_price(...)`, and
  copy/poster use cases raise `ValueError` with user-facing messages.
- Image-session failures such as missing sessions/assets are raised from `application/image_sessions.py` with messages
  like `"连续生图会话不存在"` and `"会话参考图不存在"`.

Routes catch these `ValueError`s and convert them to `HTTPException`.

---

## Route-Level Mapping

Resource route modules use a small `_raise_http_error(...)` helper. Current product and image-session routes share the
same convention:

```python
def _raise_http_error(exc: ValueError) -> None:
    detail = str(exc)
    if detail.endswith("不存在"):
        raise HTTPException(status_code=404, detail=detail) from exc
    raise HTTPException(status_code=400, detail=detail) from exc
```

Examples:

- `presentation/routes/products.py` catches `ValueError` around `create_product(...)`, `get_product_detail(...)`,
  `update_copy_set(...)`, `create_poster_job(...)`, and other use cases.
- `presentation/routes/image_sessions.py` catches `ValueError` around session CRUD, reference image upload/delete,
  generation, and attach-to-product actions.
- `presentation/routes/jobs.py` maps missing jobs to 404 directly.

When adding new use cases, keep expected business failures as `ValueError` in application code and map them at the route
boundary. Do not import FastAPI `HTTPException` into application modules.

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
