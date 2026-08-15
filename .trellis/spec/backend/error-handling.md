# Backend Error Handling

## Boundary

Expected business failures are typed in `domain/errors.py` and mapped once in `presentation/errors.py`.

Application code raises typed failures. Routes do not wrap every call in local try/except. Infrastructure failures are translated at the application/service boundary that can produce a safe business meaning.

## Response Shape

Public API failures preserve FastAPI's common shape:

```json
{"detail":"safe user-facing message"}
```

Validation may use FastAPI/Pydantic structured 422 details. Do not expose stack traces, provider payloads, SQL text, file paths, or secrets.

## Status Mapping

Use the existing typed classes/mapping. Typical semantics:

- 400: malformed business command or unsupported setting.
- 401: missing/invalid admin session.
- 403: authenticated but operation disabled/not unlocked.
- 404: product-scoped resource absent.
- 409: stale expected state, lifecycle conflict, idempotency mismatch, active-run conflict.
- 413: body/upload/result capacity exceeded where mapped.
- 422: wire validation.
- 429: admission/rate capacity when explicitly modeled.
- 502/503: safe provider, broker, or Agent service unavailable boundary.
- 500: unexpected internal failure.

Choose status by contract, not by the exception class easiest to raise.

## Business Validation

Use `BusinessValidationError` for cross-field/ownership/lifecycle rules that cannot be expressed by Pydantic alone:

- cross-product asset/reference;
- invalid graph edge/cycle;
- missing required Draft fact/reference;
- unsupported provider capability;
- invalid folder/recipe source;
- image-generation spec conflict.

Use `ConflictError` for valid requests that cannot apply to current state:

- stale expected name/folder/edit version/binding;
- repeated idempotency key with different request;
- node/runs active during invalidating mutation;
- answering a non-current Agent question.

Use `NotFoundError` for scoped lookup that intentionally hides cross-owner existence.

## Upload Errors

`presentation/upload_validation.py` reads uploads with explicit limits:

- count;
- body bytes;
- allowed MIME;
- actual decoded format;
- pixels/dimensions;
- empty/corrupt content.

Validation happens before application storage writes. Error messages state the violated safe limit and never echo bytes.

## Provider Errors

Provider adapters may receive private/raw errors. Translate them into a typed provider failure with:

- safe code/message;
- retryability;
- provider/model identifiers when safe;
- optional bounded provider note.

Do not include API key, Authorization header, full request prompt, raw response body, base64, or data URL.

Provider output validation failures are distinct from transport unavailability. Missing image output, invalid MIME, oversized ToolResult, and actual-size mismatch follow their specific contracts.

## Queue Errors

If durable rows commit but Dramatiq delivery fails:

- mark the task/run failed or delivery-pending according to the shared queue submission contract;
- raise `QueueUnavailableError`;
- keep the persisted row observable;
- do not return success.

Duplicate delivery is handled by atomic claim/idempotency, not an HTTP exception.

## Agent Service Errors

`AgentServiceRequestError` carries remote status/code and safe message.

ProductFlow maps:

- unavailable/timeouts to a safe service-unavailable response and recoverable projection state;
- remote not-found/conflict to the matching scoped action where appropriate;
- ambiguous submission/control to unknown/cancel-requested state pending sync.

A network error must not prove that a Turn or tool mutation failed.

SSE disconnect is a reconnect condition. It is not automatically a failed Turn.

## Workflow Errors

Graph and Draft operations validate before mutation:

- wrong Product/workflow/node scope;
- stale revision/edit version;
- invalid node type/config;
- edge cycle/missing endpoint;
- reference ownership;
- materialization idempotency;
- run lifecycle.

Partial graph writes are rolled back. The response uses the typed error that caused rejection.

Workflow run and node-run rows store a safe failure reason for user inspection. Provider/internal stack remains in logs only with redaction.

## Image Session Errors

Generation task state exposes:

- safe failure reason;
- provider notes;
- actual/requested dimensions;
- queue/admission state;
- retryability as supported.

One failed candidate/task does not corrupt completed candidate assets. Retry creates/uses the current durable retry contract and preserves earlier rounds.

## Storage Errors

- Missing expected media returns a typed missing-media failure.
- Invalid arbitrary paths are never accepted.
- New-file/database failure triggers storage compensation.
- Cleanup failure after commit is logged and does not falsify the committed business result.
- Archive request rejects the full batch if any selected item is missing/unverified/oversized.

## Settings Errors

Settings validation reports the named safe field:

- unknown key;
- invalid number/select/multi-select;
- missing prompt content;
- provider capability/model mismatch;
- duplicate profile/name conflict;
- wrong schema/compatibility marker;
- missing required secret during import/create.

Secret values are never returned in detail.

## Unexpected Errors

The global handler:

- attaches request id/correlation context;
- logs exception with stack;
- returns a generic safe 500 detail;
- does not turn programmer errors into 400.

Do not catch broad Exception only to continue in an uncertain transaction.

## Logging

Expected business rejections normally log at info/warning only when operationally useful. Unexpected failures log exception context.

Include ids/status/code/counts, not private content. Follow `logging-guidelines.md`.

## Tests

For a changed boundary, test:

- expected HTTP status and detail;
- no partial state;
- no secret/private payload in response;
- retry/unknown semantics;
- scoped 404 versus conflict;
- global unexpected error shape;
- logging redaction when relevant.

Use real provider/queue/DB reproduction where mock behavior cannot establish the failure mode.

## Avoid

- Raw `ValueError` as a public business contract.
- Route-local duplicate mappings.
- Returning provider exception text.
- Retrying non-idempotent work inside an exception handler.
- Reusing a failed Session without rollback.
- Treating timeout as proven provider failure.
- Hiding a persisted failed task behind a generic toast-only response.
- Swallowing storage cleanup errors without logging.
