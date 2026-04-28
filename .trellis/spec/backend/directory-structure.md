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
  `ProductInput`, `CreativeBriefPayload`, `CopyPayload`, and `PosterGenerationInput`.
- `application/time.py` is the shared application timestamp helper for timezone-aware UTC values.
- `application/queue_submission.py` owns the small shared helper for "durable row persisted, queue delivery failed"
  handling. Submit use cases use it to mark the persisted task failed and raise `QueueUnavailableError`.
- `application/product_workflow_graph.py` owns product workflow graph loading, default graph templates, lookup helpers,
  topological ordering, and latest-run ordering. Keep these graph/query concerns out of
  `application/product_workflows.py`, which owns mutations and execution orchestration.
- Product workflow application logic is split by executable boundary:
  - `application/product_workflows.py` is the stable facade for route/queue/worker/test imports. Keep existing public
    use-case names available there while implementations live in cohesive submodules.
  - `application/product_workflow_mutations.py` owns workflow graph/edit use cases: create/update/delete nodes and edges,
    upload/bind reference images, edit generated copy, and normalize the product-context singleton.
  - `application/product_workflow_execution.py` owns workflow run kickoff/execution, node-run claiming, failure
    transitions, selected-node planning, and provider/render orchestration.
  - `application/product_workflow_query.py` is the narrow workflow query-service trial for execution/reuse hot paths
    such as run reloads, node/edge lookups, source-asset existence checks, and first-class artifact lookup. Do not broaden
    this into whole-project repository conversion without a dedicated architecture task.
  - `application/product_workflow_dependencies.py` owns explicit workflow execution dependency seams for text/image
    provider resolution and poster renderer construction, while the default resolvers continue to route through the
    `product_workflows.py` facade for monkeypatch compatibility.
  - `application/product_workflow_context.py` owns product/incoming context collection, config parsing, upstream text
    assembly, reference input collection, and downstream reference target discovery.
  - `application/product_workflow_artifacts.py` owns workflow artifact summaries and materialization helpers such as
    workflow-local copy sets, reference slot fill, generated image records, and poster-to-reference source lookup.
  Avoid importing submodules through the facade from inside other submodules except for explicit compatibility shims
  needed by existing monkeypatch targets; prefer direct submodule imports to prevent circular dependencies.

This layer receives a SQLAlchemy `Session` from callers. It is allowed to call infrastructure adapters such as
`LocalStorage`, provider factories, and `PosterRenderer`, but FastAPI-specific types should not leak into it.

### Domain layer

`backend/src/productflow_backend/domain/enums.py` is the shared home for enum values such as `ProductWorkflowState`,
`SourceAssetKind`, `CopyStatus`, `JobStatus`, `PosterKind`, and `ImageSessionAssetKind`. The same string
values are mirrored in `web/src/lib/types.ts`, so enum changes are cross-layer changes.

`backend/src/productflow_backend/domain/errors.py` is the shared home for typed business errors such as `BusinessError`,
`BusinessValidationError`, and `NotFoundError`. Application use cases may raise these errors, while HTTP status conversion
still belongs in `presentation/errors.py`.

`backend/src/productflow_backend/domain/workflow_rules.py` owns DB-free workflow graph business rules such as topological
ordering, selected-node execution planning, and missing-upstream decisions. Application modules adapt ORM rows into the
small domain rule shapes before applying those rules; SQLAlchemy artifact existence checks stay in application/query
services.

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
- Business error mapping: route modules import `raise_value_error_as_http(...)` from
  `presentation/errors.py` instead of redefining `ValueError -> HTTPException` logic locally.
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
