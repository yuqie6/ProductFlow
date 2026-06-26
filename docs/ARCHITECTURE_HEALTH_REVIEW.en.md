# ProductFlow Architecture Health Review

[中文](ARCHITECTURE_HEALTH_REVIEW.md) | English

> Review date: 2026-04-28
> Scope: current repository live facts, completed governance work, still-unimplemented boundaries, and next architecture risks.
> Purpose: serve as the current architecture health entrypoint, replacing the removed historical backend audit checklist and historical architecture review snapshot.

## 1. Overall Conclusion

**Current health: 8.0 / 10.**

ProductFlow is currently in a state where the single-merchant self-hosted workspace can iterate sustainably. The backend keeps the FastAPI presentation / application / domain / infrastructure layering. Product DAG workflows and iterative image-generation durable tasks both use PostgreSQL state as the source of truth, while Redis/Dramatiq only handle dispatch and background execution. The frontend uses React, TypeScript, and TanStack Query; the API client and DTOs are centralized in `web/src/lib/`, and the product detail page has started splitting into page-local components and utilities.

Compared with the historical review, several key governance items have landed:

- The ProductWorkflow application has been split into graph, mutations, query, execution, context, artifacts, dependencies, and related modules; `product_workflows.py` now mainly serves as a compatibility facade.
- The frontend now has ESLint and Vitest scripts, so it is no longer limited to TypeScript build checks.
- `ProductDetailPage.tsx` has moved Runs, Images, Inspector, NodeCard, canvas utilities, download helpers, and related pieces into `web/src/pages/product-detail/`.
- Iterative image generation now uses durable `ImageSessionGenerationTask` records, with startup recovery, queue position, and failure state.
- The generated image gallery has landed, and iterative image results can be saved to `/gallery`.
- Iterative image generation has a mobile main-view, side-drawer, bottom generation sheet, and bottom action bar layout, reducing panel crowding on phones.
- Iterative image generation and product workflows both use lightweight status polling while running, then refresh full details after completion.

The main risk has shifted from "oversized hot modules and missing frontend quality gates" to "state consistency as async chains grow, frontend interaction regression coverage, and clear productionization boundaries." No P0 architecture issue was found that should immediately block feature development.

## 2. Current Real Module Structure

### Backend

Backend code lives under `backend/src/productflow_backend/`:

- `presentation/`: FastAPI app, routes, schemas, auth dependencies, upload validation, and error mapping.
- `application/`: products, traditional copy/poster jobs, iterative image generation, gallery, generation admission, and product workflow use cases.
- `domain/`: shared enums and domain error types.
- `infrastructure/`: SQLAlchemy models/session, Alembic, Redis/Dramatiq queue, storage, text/image providers, poster renderer, and logging.
- `workers.py`: Dramatiq actor entrypoint.
- `config.py`: environment variables, runtime business configuration definitions, and DB override reading.

The ProductWorkflow application is no longer a single file carrying all responsibilities. Current modules include:

- `product_workflows.py`: stable facade for routes.
- `product_workflow/graph.py`: workflow queries, default graph structure, and lightweight status snapshot.
- `product_workflow/mutations.py`: nodes, edges, reference image slots, and copy-node editing.
- `product_workflow/query.py`: product workflow detail query.
- `product_workflow/execution.py`: run creation, node scheduling, and non-image node execution.
- `product_workflow/run_state.py`: node-run claim, failure, cancellation, and capacity requeue state transitions.
- `product_workflow/image_generation.py`: image-generation node execution, provider timeout, safe failure, and artifact writeback.
- `product_workflow/context.py`: upstream context collection and provider input context construction.
- `product_workflow/artifacts.py`: `CopySet`, `SourceAsset`, `PosterVariant`, and related artifact writeback helpers.
- `product_workflow/templates.py`: canvas template materialization helpers.
- `product_workflow/user_templates.py`: user-saved node-group template create/list/rename/archive/apply use cases.
- `product_workflow_dependencies.py`: execution dependency injection seam for tests and future orchestration of text/image providers and renderer.

There are currently two background execution state families:

- `WorkflowRun` / `WorkflowNodeRun`: product DAG workflow runs.
- `ImageSessionGenerationTask`: iterative image-generation durable async tasks.

Global generation admission is implemented by `application/admission.py`, counting active `WorkflowRun` and `ImageSessionGenerationTask` rows in the database. `/api/generation-queue` exposes the current queue overview; iterative image status responses include queue position.

### Frontend

Frontend code lives under `web/src/`:

- `App.tsx`: route entrypoint, including `/login`, `/products`, `/products/new`, `/products/:productId`, `/image-chat`, `/gallery`, `/help`, and `/settings`.
- `pages/`: login, product list, product creation, product detail, iterative image generation, gallery, help, and settings pages.
- `pages/product-detail/`: product detail workbench page-local components, canvas helpers, download helpers, tests, and types.
- `pages/image-chat/`: iterative image status merge and branch-selection helpers.
- `pages/gallery/`: gallery layout and selection helpers.
- `components/`: top navigation, status tags, image drag-and-drop area, and image parameter controls.
- `lib/api.ts` / `lib/types.ts`: REST API client and frontend DTOs.

Frontend quality entrypoints from `web/package.json`:

- `pnpm --dir web lint`
- `pnpm --dir web test:run`
- `pnpm --dir web build`

## 3. Completed Governance

### 3.1 Documentation Aligned with Product Reality

`README.md`, `docs/PRD.md`, `docs/ARCHITECTURE.md`, `docs/ROADMAP.md`, and `docs/USER_GUIDE.md` now cover the current mainline:

- Single-admin self-hosting, not multi-tenant SaaS.
- ProductFlow workbench, iterative image generation, gallery, settings page, and runtime configuration.
- Async execution and lightweight status polling.
- Docker Compose self-hosting path and local development path.
- Current explicit exclusions: multi-tenancy, payments, automatic placement, object storage, Helm, or released container images.

The historical backend audit checklist and historical architecture review snapshot were removed so old line counts, old test entrypoints, and old issue tables do not keep being read as current facts.

### 3.2 Product Workflow Split

The product DAG workflow has been split from a concentrated application file into responsibility-named modules. Routes still call through a stable facade, reducing the API-layer impact of the split.

This split resolves the largest backend hotspot risk from the historical review, but it has not turned the domain layer into a full workflow domain model. The current approach still lets application use cases directly orchestrate SQLAlchemy models, provider input, and artifact writeback.

### 3.3 Frontend Quality Gates

The frontend now has ESLint and Vitest scripts, with helper-level tests already present:

- `web/src/lib/imageSizes.test.ts`
- `web/src/pages/gallery/helpers.test.ts`
- `web/src/pages/image-chat/branching.test.ts`
- `web/src/pages/product-detail/galleryImages.test.ts`
- `web/src/pages/product-detail/reactFlowAdapters.test.ts`
- `web/src/pages/product-detail/selection.test.ts`
- `web/src/pages/product-detail/utils.test.ts`

These cover lightweight status merging, ReactFlow adapters, selection reconciliation, gallery layout, iterative image branching, image sizing, and other key helper behavior. Component-level interaction tests are still limited.

### 3.4 Iterative Image Durable Tasks

Iterative image generation has evolved from synchronous generation requests to durable tasks:

- API creates an `ImageSessionGenerationTask`, then enqueues it.
- The worker executes the task and writes back `ImageSessionRound` / generated assets.
- The status endpoint returns lightweight task snapshots, queue position, failure reasons, and the latest round information.
- API/worker startup recovers unfinished image-session generation tasks.
- Repeated execution of terminal/currently-running tasks stays no-op or follows controlled state transitions.

### 3.5 Gallery and Asset Review

The gallery has landed as both an independent page and backend resource:

- `GET /api/gallery` lists collected generated images.
- `POST /api/gallery` saves iterative image generated assets as gallery entries.
- `ImageGalleryEntry` keeps source session, round, prompt, size, model, and download entrypoint.
- Frontend `/gallery` provides centralized browsing and preview.

### 3.6 Lightweight Polling While Running

The running-state refresh strategy has moved from "frequently fetch full detail" to lightweight status polling:

- Product workflows poll `/api/products/{product_id}/workflow/status` while running.
- Iterative image generation polls `/api/image-sessions/{image_session_id}/status` while running.
- Status responses carry only run state, node/task lightweight fields, queue information, and necessary counters.
- When status reaches a terminal state or new results are detected, the frontend refreshes full workflow/session details.

This reduces repeated serialization of large objects, image-history rerenders, and accidental overwrites of local interaction state while product detail and iterative image pages are running.

## 4. Current Main Risks

### R1. Application Still Carries Many Domain Rules

ProductWorkflow has been split, but business rules still mostly live in application modules around SQLAlchemy models. This is acceptable short-term. If node-level retry, skip, duplicate, version comparison, and provider routing keep expanding, rules may spread further across execution, mutations, context, and artifacts.

Recommendation: keep governance incremental. Extract domain services/value objects only when the same rule repeats across multiple use cases, or when tests must work around too much database state just to validate a rule. Do not preemptively rewrite a large domain model.

### R2. Frontend Component-Level Regression Coverage Is Still Limited

Vitest now covers helpers and some hooks, but real ProductDetail workbench interactions still rely on manual verification: node dragging, edge creation/deletion, right-panel switching, save-draft-before-run, image fill, and download misclick protection.

The next quality investment should prioritize interactions that are easiest to regress and hardest to exhaustively check by hand, rather than chasing a generic coverage percentage.

### R3. Async State Consistency Remains the Long-Term Core Risk

The system now has two durable state chains: `WorkflowRun` and `ImageSessionGenerationTask`. They share the "database as source of truth, queue is recoverable, duplicate messages no-op" principle, but each chain still has its own state transitions and failure handling.

Any future background task should reuse these principles and include tests for queue recovery, enqueue failure, duplicate messages, terminal no-op, and API status snapshots.

### R4. Productionization Boundaries Still Need Clear Wording

The Docker Compose self-hosted path is available, but ProductFlow is still not a full production platform:

- It is not a multi-user or multi-tenant system.
- There is no object-storage adapter layer; storage is currently local filesystem storage.
- There is no SSE/WebSocket push; running state depends on polling.
- There is no Helm chart or released container image; the current path builds from the repository through Compose.
- There is no audit admin, object-level permission model, payment system, or hosted account system.

These items remain unimplemented boundaries. Docs and the roadmap must keep marking them as future directions to avoid misleading deployment expectations.

### R5. Provider Error Classification and Observability Can Improve Further

Provider calls are isolated in the infrastructure layer, but failure classification, retry guidance, rate-limit messaging, and log correlation for real OpenAI-compatible providers can still become more detailed. Current logs and error handling are enough for development and small self-hosted use, but not a complete observability system for complex production debugging.

## 5. Recommended Next Steps

1. **Prioritize key ProductDetail workbench interaction tests**  
   Cover the state transitions most likely to regress: status merge does not lose node structure, run completion triggers full refresh, node drag coordinates stay stable, and image fill/download does not trigger incorrect selection.

2. **Capture a shared checklist for durable tasks**  
   Every new background task should answer: when DB state lands, how enqueue failure is written back, how worker duplicate messages no-op, which states API startup recovers, and whether the status endpoint is lightweight.

3. **Keep docs split between current facts and future plans**  
   Current facts belong in README, PRD, ARCHITECTURE, USER_GUIDE, and this review. Object storage, SSE/WebSocket, Helm, multi-tenancy, and similar items should stay in roadmap future directions or out-of-scope sections unless implemented.

4. **Keep splitting by real hotspots, not global rewrites**  
   The ProductWorkflow and ProductDetail split direction has worked. Future splits should follow real modification hotspots from new features, avoiding repository, domain service, or complex frontend state layers introduced only for architecture completeness.

## 6. Current Verification Entrypoints

Backend:

- `just backend-test`
- `uv run --directory backend pytest`

Frontend:

- `pnpm --dir web lint`
- `pnpm --dir web test:run`
- `just web-build`
- `pnpm --dir web build`

Documentation:

- `git diff --check`

This review is documentation-level. It does not claim that the full backend/frontend test matrix was rerun. Behavior facts come from current source, routes, scripts, and existing test entrypoints.
