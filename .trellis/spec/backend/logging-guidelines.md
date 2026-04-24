# Backend Logging Guidelines

> Current logging reality and safe extension rules for ProductFlow.

---

## Overview

The application backend currently has very little explicit application logging. There are no `logging.getLogger(...)` calls
in `backend/src/productflow_backend/`; runtime output mainly comes from Uvicorn/FastAPI, Dramatiq, exceptions, tests, and
persisted job state.

Current observability files and mechanisms:

- `backend/src/productflow_backend/workers.py` persists async job status through application use cases rather than logging
  retry state only.
- `backend/src/productflow_backend/application/use_cases.py` updates `JobRun.status`, `failure_reason`, `attempts`,
  `started_at`, and `finished_at`.
- `backend/src/productflow_backend/presentation/routes/jobs.py` exposes persisted job state through `/api/jobs/{job_id}`.
- `backend/tests/test_workflow.py` asserts job failure/retry and API behavior through database state and HTTP responses.

Because this project does not yet have a custom structured logging setup, do not invent one in random modules. If logging
is needed, add it deliberately and consistently.

---

## What Exists Today

### Server and worker logs

- `just backend-run` runs Uvicorn through `uv run --directory backend uvicorn productflow_backend.main:app --reload ...`.
- `just backend-worker` runs Dramatiq through `uv run --directory backend dramatiq --processes 2 --threads 4
  productflow_backend.workers`.

Those tools provide process-level logs. Application code does not currently wrap them with custom formatting.

### Persisted operational state

For product copy/poster jobs, durable state is preferred over log-only state:

- `JobRun.status` tracks `queued`, `running`, `succeeded`, or `failed`.
- `JobRun.failure_reason` stores the user/operator-visible failure reason.
- `JobRun.attempts` and retry timestamps are used by the worker flow.

This is why tests in `backend/tests/test_workflow.py` verify job records rather than scraping logs.

---

## If You Add Logging

Use the Python standard `logging` module unless the project first adopts a broader logging dependency. Recommended shape
for new modules:

```python
import logging

logger = logging.getLogger(__name__)
```

Then log at the boundary where the event is meaningful:

- `info`: lifecycle events that operators need, such as worker job start/finish or provider mode selection.
- `warning`: recoverable anomalies, such as a failed thumbnail variant fallback in `LocalStorage.resolve_for_variant(...)`
  if that behavior becomes hard to diagnose.
- `error` / `exception`: unexpected failures that are being handled and would otherwise disappear.
- `debug`: local-only details that are too noisy for normal runs.

Do not add `print(...)` to backend application code for diagnostics. Use tests or temporary local instrumentation instead.

---

## Sensitive Data Rules

Never log secrets or full request payloads that may contain secrets:

- `Settings.admin_access_key` and `Settings.session_secret` from `backend/src/productflow_backend/config.py`.
- Provider keys such as `text_api_key` and `image_api_key`.
- Uploaded image bytes or data URLs built in `application/image_sessions.py::_session_data_url`.
- Session cookies or `request.session` contents.
- Raw provider responses if they can include prompts, base64 images, or credentials.

The settings API already hides secret values in `presentation/routes/settings.py::_public_value(...)`; keep logs at least as
strict as API responses.

---

## Preferred Evidence for Tests and Reviews

For regressions, prefer assertions on durable state and HTTP responses instead of log assertions:

- API response status/detail through `fastapi.testclient.TestClient` in `backend/tests/test_workflow.py`.
- Database rows through `get_session_factory()` and SQLAlchemy models.
- Storage files under the test `tmp_path` configured in `backend/tests/conftest.py`.
- Alembic migration success through `alembic.command.upgrade(...)` tests.

Use logs to aid diagnosis, not as the only source of truth for behavior.

---

## Avoid

- Adding a module-specific logging framework or JSON logger without a project-level decision.
- Logging provider API keys, admin keys, session secrets, upload bytes, data URLs, or complete user prompts.
- Replacing persisted job state with log-only status.
- Emitting noisy per-request logs from route handlers when Uvicorn already logs requests.
