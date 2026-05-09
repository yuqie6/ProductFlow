# Backend Directory Structure

> Actual backend organization for ProductFlow.

---

## Overview

The backend is a Python 3.12 FastAPI application under `backend/src/productflow_backend/`.
It follows the four-layer structure already described in `AGENTS.md` and `docs/ARCHITECTURE.md`:

- `presentation/` owns FastAPI routing, request dependencies, upload validation, and Pydantic response/request schemas.
- `application/` owns workflow use cases and cross-infrastructure orchestration.
- `domain/` owns shared enum values and small domain concepts.
- `infrastructure/` owns database, local storage, queues, provider implementations, and poster rendering.

`backend/src/productflow_backend/main.py` intentionally stays tiny and only exposes `app = create_app()` from
`presentation/api.py`.

---

## Directory Layout

```text
backend/
├── pyproject.toml                       # Python 3.12, pytest, Ruff, dependencies
├── alembic/
│   ├── env.py                           # Reads Settings.database_url and Base.metadata
│   └── versions/                        # Manual Alembic revisions, e.g. 20260424_0006_add_app_settings.py
├── src/productflow_backend/
│   ├── main.py                          # ASGI app entrypoint
│   ├── config.py                        # env settings + runtime database overrides
│   ├── workers.py                       # Dramatiq actors
│   ├── domain/enums.py                  # enum values shared by DB/API/frontend
│   ├── domain/errors.py                 # typed business errors shared by application and presentation mapping
│   ├── application/
│   │   ├── contracts.py                 # Pydantic contracts between use cases and providers/renderers
│   │   ├── image_sessions.py            # continuous image-session use cases
│   │   ├── product_workflows.py          # stable facade for route/worker product workflow imports
│   │   ├── product_workflow/             # product workflow internals split by owner module
│   │   │   ├── artifacts.py              # workflow artifact materialization and summaries
│   │   │   ├── context.py                # product/upstream/reference execution context helpers
│   │   │   ├── execution.py              # workflow run kickoff/execution and node dispatch
│   │   │   ├── graph.py                  # workflow graph loading, defaults, lookup, ordering
│   │   │   ├── image_generation.py       # image_generation node executor
│   │   │   ├── mutations.py              # workflow graph/edit use cases
│   │   │   ├── query.py                  # narrow workflow query service for execution hot paths
│   │   │   ├── run_state.py              # workflow run/node-run state transitions
│   │   │   ├── templates.py              # canvas template materialization helpers
│   │   │   └── user_templates.py         # user-saved canvas template use cases
│   │   ├── queue_submission.py           # durable task enqueue failure handling helper
│   │   └── use_cases.py                 # product/copy/poster workflow use cases
│   ├── presentation/
│   │   ├── api.py                       # FastAPI app factory, middleware, router registration
│   │   ├── deps.py                      # FastAPI dependencies, including auth/session dependency
│   │   ├── errors.py                    # shared route-boundary business error to HTTP mapping
│   │   ├── image_variants.py            # shared image download/variant URL and filename helpers
│   │   ├── upload_validation.py         # upload size/MIME/pixel validation
│   │   ├── routes/                      # APIRouter modules by resource
│   │   └── schemas/                     # Pydantic DTOs and serializer helpers
│   └── infrastructure/
│       ├── db/models.py                 # SQLAlchemy typed declarative models
│       ├── db/session.py                # engine/session factory dependencies
│       ├── storage.py                   # LocalStorage and image variants
│       ├── queue.py                     # Dramatiq broker and enqueue helpers
│       ├── text/                        # text provider interfaces/factories/implementations
│       ├── image/                       # image provider interfaces/factories/implementations
│       └── poster/renderer.py           # Pillow template poster renderer
└── tests/
    ├── conftest.py                      # sqlite test settings and DB fixtures
    ├── helpers.py                       # shared pytest/image/client helpers
    ├── test_auth_settings_runtime_config.py
    ├── test_error_handling.py
    ├── test_product_crud_jobs.py
    ├── test_product_workflow_*.py
    ├── test_image_sessions.py
    ├── test_storage_upload_validation.py
    ├── test_provider_payloads.py
    ├── test_queue_recovery.py
    ├── test_logging_behavior.py
    └── test_migrations_database_constraints.py
```

---

## Layer Responsibilities

### Presentation layer

Put HTTP concerns in `backend/src/productflow_backend/presentation/`:

- App assembly and middleware belong in `presentation/api.py`.
- Authentication/session dependencies belong in `presentation/deps.py` and `presentation/routes/auth.py`.
- Resource routes are grouped under `presentation/routes/`, for example:
  - `presentation/routes/products.py` handles `/api/products`, copy sets, posters, and source-asset downloads.
  - `presentation/routes/image_sessions.py` handles `/api/image-sessions` and session asset downloads.
  - `presentation/routes/settings.py` handles `/api/settings` runtime configuration.
- Request/response models and serializer functions live in `presentation/schemas/`, for example
  `presentation/schemas/products.py` and `presentation/schemas/image_sessions.py`.
- Cross-schema validators belong in `presentation/schemas/validators.py`; image download URL/filename helpers belong in
  `presentation/image_variants.py`.
- Upload validation stays in `presentation/upload_validation.py` because it raises HTTP status-specific exceptions.

Do not put provider calls, storage writes, or workflow state transitions directly in route handlers. Existing routes call
application functions such as `create_product(...)`, `submit_product_workflow_run(...)`, or
`submit_image_session_generation_task(...)` and then serialize the returned model.

### Application layer

Put workflow rules and orchestration in `backend/src/productflow_backend/application/`:

- `application/use_cases.py` owns the core product flow:
  product creation, reference images, copy/copy-confirmation edits, product deletion, and history reads.
- `application/image_sessions.py` owns continuous image-session behavior, including building provider context,
  trimming title text, attaching generated assets back to products, and deleting session storage.
- `application/contracts.py` contains Pydantic contracts shared with providers/renderers, such as
  `ProductInput`, `CreativeBriefPayload`, `CopyPayloadV2`, and `PosterGenerationInput`.
- `application/time.py` is the shared application timestamp helper for timezone-aware UTC values.
- `application/queue_submission.py` owns the small shared helper for "durable row persisted, queue delivery failed"
  handling. Submit use cases use it to mark the persisted task failed and raise `QueueUnavailableError`.
- Product workflow application logic is split by executable boundary:
  - `application/product_workflows.py` is the stable facade for route/queue/worker imports. Keep existing public use-case
    names available there while implementations live in cohesive submodules. Do not export private `_...` helpers or
    provider factory helpers through this facade.
  - `application/product_workflow/graph.py` owns product workflow graph loading, default graph templates, lookup helpers,
    topological ordering, and latest-run ordering. Keep these graph/query concerns out of
    `application/product_workflows.py`.
  - `application/product_workflow/mutations.py` owns workflow graph/edit use cases: create/update/delete nodes and edges,
    upload/bind reference images, edit generated copy, and normalize the product-context singleton.
  - `application/product_workflow/execution.py` owns workflow run kickoff/execution, selected-node planning, and node
    dispatch. Keep node-specific provider/render orchestration in cohesive owner modules instead of growing this file
    back into a full execution monolith.
  - `application/product_workflow/run_state.py` owns workflow run/node-run state transitions: atomic node-run claiming,
    run failure/cancel marking, capacity-wait requeue scheduling, and safe workflow failure reasons.
  - `application/product_workflow/image_generation.py` owns the `image_generation` node executor, including generated
    poster/reference artifact creation, provider failure sanitization, provider timeouts, and concurrent render/provider
    calls.
  - `application/product_workflow/query.py` is the narrow workflow query-service trial for execution/reuse hot paths
    such as run reloads, node/edge lookups, source-asset existence checks, and first-class artifact lookup. Do not broaden
    this into whole-project repository conversion without a dedicated architecture task.
  - `application/product_workflow_dependencies.py` owns explicit workflow execution dependency seams for text/image
    provider resolution and poster renderer construction. Default resolvers call the infrastructure provider factories
    directly; tests that need fake providers should pass `WorkflowExecutionDependencies` rather than patching the
    `product_workflows.py` facade.
  - `application/product_workflow/context.py` owns product/incoming context collection, config parsing, upstream text
    assembly, reference input collection, and downstream reference target discovery.
  - `application/product_workflow/artifacts.py` owns workflow artifact summaries and materialization helpers such as
    workflow-local copy sets, reference slot fill, generated image records, and poster-to-reference source lookup.
  - `application/product_workflow/templates.py` owns canvas template graph materialization helpers.
  - `application/product_workflow/user_templates.py` owns user-saved canvas template create/list/rename/archive/apply
    use cases.
  Avoid importing submodules through the facade from inside other submodules; prefer direct submodule imports to prevent
  circular dependencies.

This layer receives a SQLAlchemy `Session` from callers. It is allowed to call infrastructure adapters such as
`LocalStorage`, provider factories, and `PosterRenderer`, but FastAPI-specific types should not leak into it.

`application/image_generation_core.py` owns provider-agnostic image generation helpers that are shared by product workflow
image nodes and continuous image-session generation: reference id/path de-duplication, stored image reference payload
construction, provider tool option normalization, and provider output metadata augmentation. It must not know workflow
node IDs, image-session round IDs, HTTP schemas, queue delivery, or concrete provider clients.

### Domain layer

`backend/src/productflow_backend/domain/enums.py` is the shared home for enum values such as `ProductWorkflowState`,
`SourceAssetKind`, `CopyStatus`, `JobStatus`, `PosterKind`, and `ImageSessionAssetKind`. The same string
values are mirrored in `web/src/lib/types.ts`, so enum changes are cross-layer changes.

`backend/src/productflow_backend/domain/errors.py` is the shared home for typed business errors such as `BusinessError`,
`BusinessValidationError`, and `NotFoundError`. Application use cases may raise these errors, while HTTP status conversion
still belongs in `presentation/errors.py`.

`backend/src/productflow_backend/domain/workflow_rules.py` owns DB-free workflow graph business rules such as topological
ordering, selected-node execution planning, and missing-upstream decisions. `domain/durable_generation_tasks.py` owns the
DB-free durable generation task contract shared by application submit/execution code, infrastructure queue recovery,
presentation status serializers, and worker actor assertions. Application modules adapt ORM rows into the small domain
rule/contract shapes before applying those rules; SQLAlchemy artifact existence checks stay in application/query services.

### Infrastructure layer

Put adapter code under `backend/src/productflow_backend/infrastructure/`:

- Database models/session setup: `infrastructure/db/models.py`, `infrastructure/db/session.py`.
- Local file storage and image variants: `infrastructure/storage.py`.
- Queue setup and enqueue helpers: `infrastructure/queue.py`.
- Provider interfaces and factories: `infrastructure/text/base.py`, `infrastructure/text/factory.py`,
  `infrastructure/image/base.py`, `infrastructure/image/factory.py`.
- Provider implementations stay behind those factories, for example `text/openai_provider.py`,
  `text/mock_provider.py`, `image/responses_provider.py`, and `image/mock_provider.py`.
- Continuous image chat generation is adapted in `infrastructure/image/chat_service.py`, which is called by
  `application/image_sessions.py` rather than directly from route handlers.

Provider-specific code must not leak into routes. If a new provider is added, extend the relevant infrastructure factory
and the runtime config definitions in `config.py`, then update tests and frontend settings types.

---

## Naming Conventions

- Python modules and functions use `snake_case`.
- Most product/image-session/settings route handler names end with `_endpoint`, e.g. `create_product_endpoint` and
  `generate_image_session_round_endpoint`; `presentation/routes/auth.py` keeps shorter names such as `create_session`.
- Internal helper functions are prefixed with `_`, e.g. `_raise_http_error`, `_get_product_or_raise`,
  `_load_database_values`.
- In `application/`, a leading `_` means "module-private". Do not import `_...` helpers/classes from sibling
  `application` modules. If a helper is intentionally shared across submodules, give it a public name in its owning
  module, for example `optional_config_text(...)`, `fill_reference_node(...)`, or `GeneratedWorkflowImage`. If it is
  only needed by one module, keep the `_...` helper in that module instead of exporting it through another file.
- Pydantic response/request classes use descriptive `PascalCase` names ending in `Response` or `Request`, for example
  `ProductDetailResponse`, `ConfigUpdateRequest`, and `GenerateImageSessionRoundRequest`.
- SQLAlchemy models use singular `PascalCase` class names and plural table names, e.g. `Product` -> `products`,
  `SourceAsset` -> `source_assets`.

---

## Examples to Copy

- App creation: `backend/src/productflow_backend/presentation/api.py` registers CORS, session middleware, `/healthz`,
  and routers in one place.
- Product route shape: `backend/src/productflow_backend/presentation/routes/products.py` accepts FastAPI inputs,
  delegates to `application/use_cases.py`, and serializes with `presentation/schemas/products.py`.
- Business error mapping: application use cases raise typed `BusinessError` subclasses, and
  `presentation/errors.py` registers the FastAPI handler that preserves the `{"detail": "..."}` response shape. Do not
  add route-local raw `ValueError` adapters for expected business failures.
- Continuous image sessions: `backend/src/productflow_backend/presentation/routes/image_sessions.py` delegates to
  `application/image_sessions.py`, which delegates provider-specific chat generation to
  `infrastructure/image/chat_service.py`, and keeps download handling in the route.
- Provider selection: `backend/src/productflow_backend/infrastructure/text/factory.py` and
  `backend/src/productflow_backend/infrastructure/image/factory.py` choose implementations from runtime settings.
- Tests: `backend/tests/test_*.py` are split by behavior area. Keep shared image/client/workflow helpers in
  `backend/tests/helpers.py`, and put regressions near their owning theme: auth/settings/runtime config, error handling,
  product CRUD, product workflow DAG/mutations/queue recovery, image sessions, storage/upload validation,
  provider payloads, queue/logging behavior, and migrations/database constraints.

---

## Avoid

- Adding new route modules without including them in `presentation/api.py`.
- Duplicating frontend-facing DTO shapes outside `presentation/schemas/`.
- Importing OpenAI, Pillow renderer details, Redis/Dramatiq, or storage path manipulation directly from route modules.
- Changing enum string values without updating SQLAlchemy models/migrations, Pydantic schemas/tests, and
  `web/src/lib/types.ts`.
