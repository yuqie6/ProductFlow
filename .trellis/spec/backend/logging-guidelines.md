# Backend Logging Guidelines

> Current logging reality and safe extension rules for ProductFlow.

---

## Overview

The application backend now uses standard-library logging with a process-wide configuration that keeps stdout visible and
writes persistent rotating log files. Runtime output also comes from Uvicorn/FastAPI, Dramatiq, exceptions, tests, and
persisted durable task state.

Current observability files and mechanisms:

- `backend/src/productflow_backend/infrastructure/logging.py` configures stdout plus rotating file logs and deletes expired
  log files.
- `backend/src/productflow_backend/infrastructure/logging.py` also owns the process-local log context backed by
  `contextvars`: `request_id`, `workflow_run_id`, and `image_session_generation_task_id`.
- `backend/src/productflow_backend/presentation/api.py` sets API request context from `X-Request-ID` or a generated id and
  returns the same value in the response header.
- `backend/src/productflow_backend/workers.py` sets worker context at the Dramatiq actor boundary for product workflow runs
  and continuous image-session generation tasks, then clears it when the actor returns or raises.
- `backend/src/productflow_backend/workers.py` persists async workflow and continuous image generation state through
  application use cases rather than logging retry state only.
- `backend/src/productflow_backend/application/product_workflow/execution.py` and
  `backend/src/productflow_backend/application/product_workflow/run_state.py` update `WorkflowRun` /
  `WorkflowNodeRun` status and failure fields.
- `backend/src/productflow_backend/application/image_sessions.py` updates `ImageSessionGenerationTask` status,
  failure, attempt, queue, and result fields.
- `backend/tests/test_queue_recovery.py`, `backend/tests/test_product_workflow_queue_recovery.py`, and
  `backend/tests/test_logging_behavior.py` assert workflow/image-session retry, recovery, and logging behavior through
  durable state, filesystem state, and HTTP responses.

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

For product workflow and continuous image-session tasks, durable state is preferred over log-only state:

- `WorkflowRun.status` / `WorkflowNodeRun.status` track DAG execution.
- `ImageSessionGenerationTask.status` tracks `queued`, `running`, `succeeded`, or `failed`.
- Failure reason, attempt, queue, and result fields are stored on the owning durable rows.

This is why queue/recovery tests verify durable records rather than scraping logs.

---

## If You Add Logging

Use the Python standard `logging` module unless the project first adopts a broader logging dependency. Recommended shape
for new modules:

```python
import logging

logger = logging.getLogger(__name__)
```

Then log at the boundary where the event is meaningful:

- `info`: lifecycle events that operators need, such as worker job start/finish, queue recovery summaries, or provider
  mode selection.
- `warning`: recoverable anomalies worth investigation, such as provider fallback, sanitized provider failure, capacity
  requeue, or a failed thumbnail variant fallback in `LocalStorage.resolve_for_variant(...)` if that behavior becomes
  hard to diagnose.
- `exception`: unexpected failures that are caught and would otherwise disappear, such as durable enqueue failure after
  a row was already created or an automatic retry enqueue failure.
- `error`: handled failures that are not exceptions at the logging site but still need operator attention.
- `debug`: local-only details that are too noisy for normal runs.

Do not add noisy route-level logs for ordinary successful requests or ordinary user-caused `4xx` business validation
errors. Uvicorn access logs and HTTP responses already cover those paths.

Do not add `print(...)` to backend application code for diagnostics. Use tests or temporary local instrumentation instead.

## Scenario: Request and worker log context

### 1. Scope / Trigger
- Trigger: adding API middleware, worker actor boundaries, queue recovery, or logging formatter changes that affect
  request/job/task correlation.

### 2. Signatures
- `new_request_id() -> str`
- `set_request_id(request_id: str) -> Token[str]` / `reset_request_id(token: Token[str]) -> None`
- `set_workflow_run_id(workflow_run_id: str) -> Token[str]` / `reset_workflow_run_id(token: Token[str]) -> None`
- `set_image_session_generation_task_id(task_id: str) -> Token[str]` /
  `reset_image_session_generation_task_id(token: Token[str]) -> None`
- `current_log_context() -> dict[str, str]`
- API header contract: request `X-Request-ID` is optional; response `X-Request-ID` is always set.

### 3. Contracts
- Log lines include stable, human-readable fields: `request_id`, `workflow_run_id`, and
  `image_session_generation_task_id`.
- API requests accept incoming `X-Request-ID`; when missing, the backend generates one and returns it in the same response
  header.
- API request id correlation must not change any JSON response model or route response shape.
- Business error responses converted by the global typed handler still return the normal `X-Request-ID` response header.
- `run_product_workflow_run(...)` sets `workflow_run_id` only while that Dramatiq actor executes.
- `run_image_session_generation_task(...)` sets `image_session_generation_task_id` only while that Dramatiq actor executes.
- Ordinary process logs outside request/worker context use `-` placeholders.
- Do not manually pass request ids, workflow run ids, or generation task ids through application DTOs just to support
  logging. Use the existing contextvar boundary helpers.

### 4. Validation & Error Matrix
- Missing request header -> generate a non-empty request id and return it in `X-Request-ID`.
- Incoming request header present -> preserve the exact value and return the same value in `X-Request-ID`.
- Route handler raises -> request context still resets in `finally`; response header should remain attached when the
  exception is converted into an HTTP response by middleware/exception handling.
- Worker actor returns or raises -> worker context resets in `finally`.
- Ordinary startup/recovery logs -> formatter fields render as `-`, not stale ids.

### 5. Good/Base/Bad Cases
- Good: API log emitted during a request includes `request_id=<id>` and the HTTP response has the same `X-Request-ID`.
- Good: product workflow worker logs include `workflow_run_id=<run id>` without manual string interpolation at every
  logging call.
- Good: continuous image-session worker logs include `image_session_generation_task_id=<task id>`.
- Base: process startup, Uvicorn lifecycle, and queue recovery logs render `request_id=- workflow_run_id=-
  image_session_generation_task_id=-`.
- Bad: passing request ids through Pydantic response bodies or application DTOs.
- Bad: setting a contextvar without resetting the token in `finally`.

### 6. Tests Required
- Formatter unit test asserting both active context values and stable `-` placeholders.
- API middleware test asserting incoming/generated `X-Request-ID` and context cleanup after the request.
- Worker actor boundary test asserting `workflow_run_id` / `image_session_generation_task_id` during execution and cleanup
  afterward.
- Run `uv run --directory backend ruff check .` and backend tests after formatter or middleware changes.

### 7. Wrong vs Correct
#### Wrong

```python
token = set_workflow_run_id(workflow_run_id)
execute_product_workflow_run(workflow_run_id)
```

#### Correct

```python
token = set_workflow_run_id(workflow_run_id)
try:
    execute_product_workflow_run(workflow_run_id)
finally:
    reset_workflow_run_id(token)
```

## Scenario: Metrics boundary

### 1. Scope / Trigger
- Trigger: requests to add queue metrics, generation counters, Prometheus/OpenTelemetry integration, or a metrics endpoint.

### 2. Signatures
- No metrics endpoint exists in the current backend contract.
- Durable state entrypoints remain the source for operational inspection:
  `WorkflowRun`, `WorkflowNodeRun`, `ImageSessionGenerationTask`, `recover_unfinished_workflow_runs(...)`, and
  `recover_unfinished_image_session_generation_tasks(...)`.

### 3. Contracts
- Do not add Prometheus, OpenTelemetry, structlog, loguru, APM agents, or a metrics endpoint without a dedicated task.
- Do not add ad-hoc route-level counters.
- Keep generation progress and failure evidence in durable database state:
- `WorkflowRun` / `WorkflowNodeRun` statuses for product workflow progress and failure counts.
- `ImageSessionGenerationTask` status, attempts, queue fields, progress heartbeat, and failure reason for continuous image
  generation.
- Queue recovery summaries from `recover_unfinished_workflow_runs(...)` and
  `recover_unfinished_image_session_generation_tasks(...)`.

### 4. Validation & Error Matrix
- Need current task/run progress -> query durable rows through existing application/API paths.
- Need queue recovery evidence -> use recovery summaries and persisted task/run state.
- Need external metrics scraping -> create a dedicated observability task before introducing dependencies or endpoint
  contracts.

### 5. Good/Base/Bad Cases
- Good: document a metrics tradeoff in this spec or task research before adding implementation.
- Base: rely on durable statuses and request/worker ids for investigation.
- Bad: adding `/metrics` opportunistically during unrelated logging work.
- Bad: logging full prompts, provider responses, upload bytes, cookies, or data URLs as a substitute for metrics.

### 6. Tests Required
- No tests are required for a documented non-implementation decision.
- If a future metrics endpoint is approved, add endpoint tests plus secret/payload redaction coverage.

### 7. Wrong vs Correct
#### Wrong

```python
app.include_router(metrics_router)
```

#### Correct

```text
Record the metrics decision in the observability task/spec, then implement only after the endpoint contract is approved.
```

---

## Sensitive Data Rules

Never log secrets or full request payloads that may contain secrets:

- `Settings.admin_access_key` and `Settings.session_secret` from `backend/src/productflow_backend/config.py`.
- Provider keys such as `text_api_key` and `image_api_key`.
- Uploaded image bytes or data URLs built in `application/image_sessions.py::_session_data_url`.
- Session cookies or `request.session` contents.
- Full prompts or full request bodies.
- Raw provider responses if they can include prompts, base64 images, credentials, provider request bodies, or provider
  response payloads.
- Upload bytes, multipart bodies, image base64 strings, and generated data URLs.

The settings API already hides secret values in `presentation/routes/settings.py::_public_value(...)`; keep logs at least as
strict as API responses.

Prefer IDs, counts, status values, enum names, queue positions, task/run ids, and already-sanitized concise failure
reasons. When logging provider failures, use the same sanitized/category-level detail that is safe for durable failure
state, not raw exception payloads.

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
