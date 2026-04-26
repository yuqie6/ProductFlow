# Backend Logging Guidelines

> Current logging reality and safe extension rules for ProductFlow.

---

## Overview

The application backend now uses standard-library logging with a process-wide configuration that keeps stdout visible and
writes persistent rotating log files. Runtime output also comes from Uvicorn/FastAPI, Dramatiq, exceptions, tests, and
persisted job state.

Current observability files and mechanisms:

- `backend/src/productflow_backend/infrastructure/logging.py` configures stdout plus rotating file logs and deletes expired
  log files.
- `backend/src/productflow_backend/workers.py` persists async job status through application use cases rather than logging
  retry state only.
- `backend/src/productflow_backend/application/use_cases.py` updates `JobRun.status`, `failure_reason`, `attempts`,
  `started_at`, and `finished_at`.
- `backend/src/productflow_backend/presentation/routes/jobs.py` exposes persisted job state through `/api/jobs/{job_id}`.
- `backend/tests/test_queue_recovery.py`, `backend/tests/test_product_workflow_queue_recovery.py`, and
  `backend/tests/test_logging_behavior.py` assert job/workflow retry, recovery, and logging behavior through durable state,
  filesystem state, and HTTP responses.

Because this project uses a small standard-library logging setup, do not invent a separate framework in random modules. If
logging is needed, add it deliberately and consistently through `logging.getLogger(__name__)`.

---

## What Exists Today

### Server and worker logs

- `just backend-run` runs Uvicorn through `uv run --directory backend uvicorn productflow_backend.main:app --reload ...`.
- `just backend-worker` runs Dramatiq through `uv run --directory backend dramatiq --processes 2 --threads 4
  productflow_backend.workers`.

Those tools provide process-level logs. ProductFlow configures the root Python logger once per process so application logs
continue to reach stdout/stderr and are mirrored into a rotating file handler.

### Persisted operational state

For product copy/poster jobs, durable state is preferred over log-only state:

- `JobRun.status` tracks `queued`, `running`, `succeeded`, or `failed`.
- `JobRun.failure_reason` stores the user/operator-visible failure reason.
- `JobRun.attempts` and retry timestamps are used by the worker flow.

This is why queue/recovery tests verify job records rather than scraping logs.

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

- API response status/detail through `fastapi.testclient.TestClient` in the relevant `backend/tests/test_*.py` topic file.
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

## Scenario: Persistent API and worker logs

### 1. Scope / Trigger
- Trigger: backend process startup, worker startup, workflow execution, queue recovery, or log retention changes.

### 2. Signatures
- `configure_logging(settings: Settings | None = None) -> None` configures root logging once per process.
- `cleanup_old_logs(settings: Settings | None = None) -> int` deletes `*.log*` files older than retention days.
- Environment-backed settings: `LOG_DIR`, `LOG_LEVEL`, `LOG_MAX_BYTES`, `LOG_BACKUP_COUNT`, `LOG_RETENTION_DAYS`.

### 3. Contracts
- API startup calls `configure_logging(...)` during app creation and `cleanup_old_logs(...)` during lifespan startup before
  queue recovery.
- Dramatiq worker import calls `configure_logging()`, and the Dramatiq CLI startup path calls `cleanup_old_logs()` before
  job/workflow recovery.
- Default log path is the repository backend storage log file (`backend/storage/logs/productflow.log`, resolved from
  the backend package location rather than the process working directory); storage/log files are ignored by git. `LOG_DIR`
  may still override the directory explicitly.
- File logs use `RotatingFileHandler` with configured max bytes and backup count. Stdout/stderr logging remains available
  for service managers.
- Uvicorn `uvicorn.error` and `uvicorn.access` records must also be mirrored into the same persistent file when their
  logger propagation stops before root, including human-readable access status text such as `200 OK`. Do not add
  ProductFlow stream handlers to Uvicorn loggers, and keep a single shared ProductFlow file handler instance across
  root/Uvicorn mirrors so console output and file lines are not duplicated.

### 4. Validation & Error Matrix
- Log dir missing -> create it.
- `LOG_RETENTION_DAYS <= 0` -> skip age cleanup.
- One expired log cannot be deleted -> log exception and continue other files.
- Sensitive config/provider values -> never log them; log IDs, statuses, and concise failure reasons only.

### 5. Good/Base/Bad Cases
- Good: workflow run created, node start/success/failure, queue recovery, and cleanup summary appear in persistent logs.
- Base: tests assert cleanup by filesystem state rather than scraping log text.
- Bad: adding `print(...)` diagnostics or logging provider keys/full prompts/upload bytes.

### 6. Tests Required
- Unit regression that `cleanup_old_logs(...)` deletes expired log files and preserves fresh logs.
- Backend ruff and workflow tests after adding new logger calls.

### 7. Wrong vs Correct
#### Wrong

```python
print(f"run failed: {provider_payload}")
```

#### Correct

```python
logger.warning("工作流运行失败: run_id=%s failed_node_id=%s reason=%s", run_id, node_id, reason)
```
