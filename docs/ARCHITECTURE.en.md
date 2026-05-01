# ProductFlow Architecture

[中文](ARCHITECTURE.md) | English

[Current architecture health, completed cleanup, and remaining risks are tracked in `docs/architecture-health-review.en.md`; this document stays focused on system structure.]

## 1. System Overview

ProductFlow consists of the frontend, backend API, background worker, PostgreSQL, Redis, and local file storage:

```text
React/Vite web
  -> FastAPI backend
    -> PostgreSQL metadata
    -> Redis/Dramatiq queue
    -> local storage files
    -> text provider / image provider
  -> Dramatiq worker
    -> same database, queue, storage and providers
```

The default self-hosted path is driven by the root `docker-compose.yml`. `docker compose up -d --build` builds and starts PostgreSQL, Redis, the FastAPI backend, the Dramatiq worker, and the nginx-served Web static site. API/worker containers connect to dependencies through `productflow-postgres:5432` and `productflow-redis:6379`, and share persistent storage mounted at `/app/storage`. When `STORAGE_HOST_PATH` is not set, storage uses the Docker named volume `productflow-storage`. When migrating from an older systemd production environment, you can set the host-only variable `STORAGE_HOST_PATH=/home/cot/ProductFlow-release/shared/storage` to bind-mount an existing host storage directory to `/app/storage`; the runtime container still keeps `STORAGE_ROOT=/app/storage`. The backend container runs Alembic migrations before starting `uvicorn`.

The production update entrypoint is `just release`, which calls `scripts/release.sh` to validate Compose configuration, stop legacy user-level systemd services (`productflow-backend.service`, `productflow-worker.service`, `productflow-web.service`, used to free old release ports 29280/29281), run `docker compose up -d --build --remove-orphans`, and perform HTTP health checks. `just release-dry-run` only validates configuration and prints the plan; it does not stop old services, build, or start containers. Normal updates do not delete Docker volumes.

Local hot-reload development is still driven by the root `justfile`: you can start only `productflow-postgres` and `productflow-redis`, then run the API, worker, and frontend separately with `just backend-run`, `just backend-worker`, and `just web-dev`. The development environment uses `STORAGE_ROOT=./backend/storage-dev` from `.env.dev`, isolated from production Compose storage. Do not start local development processes by shell-sourcing production `.env`.

## 2. Backend Layering

Backend code lives under `backend/src/productflow_backend/` and is organized by layer:

- `presentation/`: FastAPI app, routes, auth dependencies, Pydantic schemas, and upload validation.
- `application/`: use-case logic for products, copy, posters, gallery, image sessions, and product workflows. Product workflow logic is split into graph / mutations / query / execution / context / artifacts / dependencies modules, with `product_workflows.py` kept as the compatibility facade.
- `domain/`: stable enums such as task status, asset type, and workflow node type.
- `infrastructure/`: SQLAlchemy models/session, queue, storage, text/image providers, and poster renderer.
- `workers.py`: Dramatiq actor entrypoint.
- `config.py`: environment configuration, runtime configuration definitions, and database override reading.

The route layer only handles input adaptation, authentication, error mapping, and serialization. Provider calls, job state changes, and workflow progression stay inside application/infrastructure boundaries.

## 3. Frontend Structure

Frontend code lives under `web/src/`:

- `pages/`: login, product list, product creation, product detail, gallery, settings, and image-session pages (current routes include `/image-chat`, `/products/:productId/image-chat`, `/gallery`, and `/settings`).
- `components/`: shared UI such as the top navigation, status tags, and image drag-and-drop upload area.
- `lib/api.ts`: centralized REST API request wrapper.
- `lib/types.ts`: frontend DTO types that must stay aligned with backend schemas.

The frontend uses TanStack Query for server state. The product detail page and iterative image page use lightweight status polling while work is active:

- Iterative image generation polls `['image-session-status', selectedSessionId]`, merges task state only, then refreshes the full session after completion.
- Product workflows poll `['product-workflow-status', productId]`, merge node/run state only, then refresh full workflow and product artifact queries after completion.

Do not reintroduce active polling for complete `ImageSessionDetailResponse` or complete `ProductWorkflowResponse`; those payloads include image history, node configuration, artifact references, and run records, and high-frequency refresh increases frontend render cost and backend serialization work.

The product detail page is currently the ProductFlow workbench: the canvas handles nodes, edges, zoom, pan, and node dragging; the right sidebar handles Details, Runs, and Images. Canvas zoom ratio and sidebar width are browser-local preferences, while workflow nodes, edges, run state, and artifacts remain database-backed.

## 4. Main Data Model Lines

Traditional product creative chain:

```text
Product
  -> SourceAsset(original/reference/processed)
  -> CreativeBrief
  -> CopySet(draft/confirmed)
  -> PosterVariant(main_image/promo_poster)
```

Iterative image-generation chain:

```text
ImageSession
  -> ImageSessionAsset(reference_upload/generated_image)
  -> ImageSessionRound(one generated candidate per row)
  -> ImageSessionGenerationTask(durable async generation task)
  -> optional Product attachment
  -> optional ImageGalleryEntry
```

Product DAG workflow chain:

```text
ProductWorkflow
  -> WorkflowNode(product_context/reference_image/copy_generation/image_generation)
  -> WorkflowEdge
  -> WorkflowRun
  -> WorkflowNodeRun
```

PostgreSQL is the source of truth for metadata and run state. Redis/Dramatiq is only responsible for dispatching background execution messages.

Workflow node semantics for users:

- `product_context`: product information entrypoint for one product workflow.
- `reference_image`: a single current reference image slot; manual upload or upstream image generation replaces the current image, while old assets remain in product history/assets.
- `copy_generation`: copy generation and editable copy fields.
- `image_generation`: image-generation trigger/configuration node; image artifacts are written into downstream reference image nodes instead of being displayed on the image-generation node itself.

## 5. Async Jobs and Recovery

There are currently two background execution entrypoints:

1. `WorkflowRun`: used for product DAG workflow execution.
2. `ImageSessionGenerationTask`: used for iterative image generation.

Shared principles:

- Database records are persisted first; Redis messages are only recoverable dispatch attempts.
- Database constraints prevent duplicate active workflow runs for the same product.
- If enqueue fails, the newly created run/task is marked failed to avoid stuck active state.
- API startup recovers queued unfinished tasks/workflows.
- Worker startup can reset stale running state and re-dispatch work.
- Iterative image generation no longer treats a user-configurable hard total timeout as product semantics. Running tasks persist `progress_updated_at`, completed candidate count, current candidate, and provider response state; stale-running recovery uses the latest progress heartbeat for idle detection and only falls back to `started_at` for older rows.
- The iterative image worker's Dramatiq `time_limit` remains only as an internal failsafe, not as a user-tunable generation deadline.
- Dramatiq actors should no-op on duplicate messages for terminal/currently-running records.
- The global generation concurrency limit is enforced by counting active `WorkflowRun` and `ImageSessionGenerationTask` rows in the database.
- `/api/generation-queue` returns the global durable queue overview; iterative image status responses include the current task's queue position.

Related entrypoints:

- `productflow_backend.infrastructure.queue.recover_unfinished_workflow_runs`
- `productflow_backend.infrastructure.queue.recover_unfinished_image_session_generation_tasks`
- `productflow_backend.workers`

## 6. Provider Architecture

ProductFlow separates model capabilities by modality.

Text providers live under `infrastructure/text/` with a unified interface:

- `generate_brief(product_input)`
- `generate_copy(brief, product_input)`

Current implementations:

- `mock`
- `openai` (Responses API compatible)

Image providers live under `infrastructure/image/` and serve poster generation and image sessions. Current implementations:

- `mock`
- `openai_responses` (Responses API `image_generation` tool, supporting `input_image`; iterative image generation prefers background response + retrieve polling and writes provider status into task progress)

Provider selection is controlled by `config.py` and corresponding factories. Routes do not directly depend on concrete SDKs.

## 7. Poster Generation

Posters have two modes:

- `template`: render with local Pillow templates, suitable for development/testing without image model keys.
- `generated`: package confirmed copy, product images, and reference images as image-provider input and generate the result with a remote model.

Both modes target two artifact types:

- `main_image`: 1:1 ecommerce main image.
- `promo_poster`: 3:4 promotional poster.

## 8. Configuration Layers

Configuration is split into two categories:

1. Env-only infrastructure configuration: `DATABASE_URL`, `REDIS_URL`, `SESSION_SECRET`, `ADMIN_ACCESS_KEY`, `SETTINGS_ACCESS_TOKEN`, and similar values. These must be available before the application can access the database, or they protect the secondary unlock for the settings page, so runtime DB overrides are not supported.
2. Runtime business configuration: provider, model, image size, upload limits, task retry, global generation concurrency limit, poster mode, prompt templates, login-gate switch, business deletion switch, and similar values. They can be provided as defaults by `.env` / `.env.dev`, or written to `app_settings` through `/api/settings` after login and settings-page unlock.

Secret configuration values are not echoed back in API responses.

The login gate `admin_access_required` is enabled by default. When enabled, private APIs require an admin marker in the Cookie session through `require_admin`, and invalid `ADMIN_ACCESS_KEY` values still return 401. When disabled, normal workspace/private APIs can be used without the admin key, and `GET /api/auth/session` returns `authenticated=true` and `access_required=false`; complete `/api/settings` reads/writes still require the independent `SETTINGS_ACCESS_TOKEN` unlock.

The business deletion switch `deletion_enabled` is disabled by default. When disabled, the backend rejects whole-product deletion and whole iterative image-session deletion at the route boundary, so demo sites do not lose evidence after problematic content is deleted. Workflow node/edge editing and reference-image deletion are not affected. `DELETE /api/auth/session` and restoring database overrides from the settings page are not part of business deletion protection.

Prompt template overrides cover product understanding, copy generation, workbench image generation, and iterative image generation. Infrastructure configuration and secret reading stay behind backend boundaries; the frontend only displays configuration items, sources, and save state.

## 9. File Storage and Downloads

Local files are managed by `LocalStorage` in `infrastructure/storage.py`. It constrains relative paths under the configured `STORAGE_ROOT` and rejects absolute paths or path traversal. In production Compose containers, `STORAGE_ROOT` is fixed to `/app/storage`; `STORAGE_HOST_PATH` only controls the host bind-mount source and should not be passed into application logic as a replacement for `STORAGE_ROOT`.

User-downloadable files are read through controlled routes, for example:

- `/api/posters/{poster_id}/download`
- `/api/source-assets/{asset_id}/download`
- `/api/image-session-assets/{asset_id}/download`

Do not bypass the storage service by directly concatenating user-controlled paths.

## 10. Security Boundaries

The current security model is "single-admin self-hosted":

- Admin-key login, not public registration.
- `ADMIN_ACCESS_KEY` is read only from environment variables and does not enter database configuration. The login gate can be disabled through the `admin_access_required` runtime switch, but stays enabled by default.
- The settings page uses an independent `SETTINGS_ACCESS_TOKEN` for secondary unlock; the session stores only the unlocked marker, not the plaintext token. Disabling the login gate does not disable this secondary unlock.
- Session cookies are signed with `SESSION_SECRET`.
- CORS is controlled by `BACKEND_CORS_ORIGINS`.
- Uploaded files have MIME, size, pixel, and count limits.
- Provider API keys are stored in env or database configuration, and APIs do not echo secrets.

Currently not provided: multi-user isolation, object-level permissions, audit logs, or production WAF configuration.
