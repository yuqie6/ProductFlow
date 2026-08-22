# ProductFlow Architecture

## 1. System Boundary

ProductFlow is a single-administrator, single-merchant workspace with seven runtime units:

1. React/Vite Web.
2. FastAPI business API.
3. Dramatiq worker.
4. PostgreSQL async dispatcher.
5. Node.js 22 + Pi SDK ProductFlow Agent service.
6. PostgreSQL.
7. Redis and media storage.

The browser reaches only Web and FastAPI. The Agent service calls FastAPI internal endpoints with a dedicated bearer token; FastAPI controls Agent Turns over the agent-service internal HTTP/SSE API. API, worker, and the async dispatcher share PostgreSQL, Redis, and storage. `just dev` and Docker Compose both start the dispatcher.

This document describes the current implementation only. Module ownership comes from the live source tree and behavior evidence comes from the referenced tests. Product contracts live in `PRD.en.md`, durable rationale in `adr/`, and incomplete deployment evidence in `rollout/`. Read `adr/0007-pi-agent-runtime-boundary.md` and `specs/pi-agent-runtime-integration.md` when changing the Agent service. Remaining GraphProposal and Recipe ChangeSet work is entered only from `ROADMAP.en.md`.

## 2. Backend Layers

`backend/src/productflow_backend/` keeps four boundaries:

- `presentation/`: FastAPI routes, request/response schemas, authentication, upload reads, and HTTP error mapping.
- `application/`: product, Agent conversation, WorkflowDraft, schema-v3 graph, image library, image session, settings, and asynchronous use cases.
- `domain/`: enums, business errors, and database-free DAG rules.
- `infrastructure/`: SQLAlchemy, provider clients, Redis/Dramatiq, storage, logging, and the Agent service client.

Routes do not own complex transactions or provider payload construction. The application layer owns business transactions, and infrastructure adapts external systems. FastAPI dependencies and workers own Session lifetime; a public application command may own one explicit commit/rollback, while internal `stage_*` helpers only assemble or flush.

Current code ownership:

| Capability | Application/Domain owner | HTTP/External owner | Primary regression tests |
|---|---|---|---|
| Agent product creation | `agent/product_workspaces.py`, `product_intake.py` | `routes/agent_product_workspaces.py` | `test_agent_product_workspaces.py` |
| Agent Session and Task | `agent/sessions.py`, `tasks.py` | `routes/agent_sessions.py`, `routes/agent_tasks.py` | `test_agent_sessions.py`, `test_agent_tasks.py` |
| Agent Turn and sync | `agent/conversations.py`, `control.py`, `sync.py` | `routes/agent_conversations.py`, `infrastructure/agent_service.py` | `test_workflow_agent_service.py` |
| Global media library | `media_library/` (`queries.py`, `service.py`, `organization.py`, `workflow.py`) | `routes/media_library.py` | `test_media_library.py`, `test_media_library_api.py` |
| Global library organization Draft | `media_library/draft_contracts.py`, `media_library/drafts.py`, `agent/control.py` | `routes/global_agent_conversations.py`, `routes/agent_internal.py` | `test_media_library_drafts.py` |
| Draft and graph persist | `workflow_drafts/contracts.py`, `service.py`, `product_workflow/graph_draft_persist.py` | `routes/workflow_drafts.py`, `routes/workflow_graphs.py` | `test_workflow_draft_contracts.py`, `test_graph_draft_persist.py` |
| schema-v3 graph and execution | `domain/graph_catalog.py`, `domain/graph_rules.py`, `product_workflow/graph_*.py` | `routes/workflow_graphs.py`, `workers.py` | graph compiler/run tests |
| Recipes | `workflow_recipes/` | `routes/workflow_recipes.py` | `test_workflow_recipes.py` |
| Delivery renditions | `delivery_renditions/` | `routes/delivery_renditions.py` | `test_delivery_renditions.py` |
| Product image library | `product_images/` (`queries.py`, `mutations.py`, `archives.py`, `assets.py`), `media_objects.py` | `routes/products.py` | `test_product_gallery_explorer.py`, `test_media_objects.py` |
| Iterative image generation | `image_sessions/` (`service.py`, `generation.py`) | `routes/image_sessions.py`, image adapters | image-session/provider tests |
| Settings and providers | `settings.py`, `runtime_settings.py` | `routes/settings.py`, `infrastructure/provider_config.py` | settings/provider/runtime tests |
| Async dispatch | `async_delivery.py`, `durable_recovery.py` | `commands/run_async_dispatcher.py`, Compose `productflow-async-dispatcher` | `test_async_delivery.py`, `test_async_dispatcher_command.py` |
| V1 archive cutover | `legacy_archives.py`, `legacy_retirement/` | `routes/legacy_archives.py`, `commands/` | archive/cutover/migration tests |
| Errors and logging | `domain/errors.py` | `presentation/errors.py`, `infrastructure/logging.py`, middleware and workers | `test_error_handling.py`, `test_logging_behavior.py` |

## 3. Frontend Structure

`web/src/App.tsx` registers the current pages:

- `/login`
- `/products`
- `/products/new`
- `/products/new/agent`, redirect only
- `/products/:productId`
- `/image-chat`
- `/media-library`
- `/gallery`
- `/history` and `/history/:archiveKind/:archiveId`
- `/settings`
- `/help`

Page code lives in `web/src/pages/`. Shared visual components live in `web/src/components/`. HTTP, DTOs, i18n, and browser preferences live in `web/src/lib/`.

The product workbench lives in `pages/workbench/` and is split by duty:

- `workbench/agent/`: page orchestration, conversation, SSE events, questions, Draft confirmation, and graph persist.
- `workbench/canvas/`: current Graph canvas, inspector, runs, recipes, and delivery renditions.
- `workbench/chrome/`: canvas chrome, node cards, sidebar, shortcuts, and image Explorer.

Dependency direction is `agent -> canvas, chrome` and `canvas -> chrome`. `chrome` must not import agent or canvas.

TanStack Query owns server state. React state owns local forms, selection, and canvas interaction. `api.ts` is the browser HTTP boundary.

Current frontend ownership:

| Capability | Owner | Primary tests |
|---|---|---|
| Agent creation form | `AgentProductCreatePage.tsx`, `pages/product-create/` | selection/form/workspace API tests |
| Agent conversation, SSE, Draft confirmation | `pages/workbench/agent/` | reducer, event, conversation, confirmation, reveal tests |
| Graph canvas and inspector | `pages/workbench/canvas/` | graph catalog/layout/canvas, inspector, runs, and rendition tests |
| Global media library and workflow sub-library | `MediaLibraryPage.tsx`, `workbench/canvas/WorkflowMediaLibraryPanel.tsx` | media library/application tests, web build |
| Global Agent Dock | `components/GlobalAgentDock.tsx` | `GlobalAgentDockComponents.test.ts` |
| Shared workbench and image library | `pages/workbench/chrome/` | shortcuts, interaction, image-explorer tests |
| HTTP and wire DTOs | `lib/api.ts`, `lib/types.ts` | `lib/*Api.test.ts`, TypeScript build |
| Read-only history | `LegacyHistoryPage.tsx`, `pages/legacy-history/` | legacy history/model/API tests |

## 4. Agent Creation Flow

```text
image types + quantities + 1..6 uploads
  -> Product + ProductImageAsset + WorkflowDraft + AgentSession + AgentConversation
  -> ProductFlow submits Agent Turn
  -> agent-service / Pi SDK ProductFlow adapter
  -> ProductFlow internal read, proposal, and pending-request tools
  -> versioned WorkflowDraft artifact
  -> user confirmation
  -> schema-v3 graph persist
  -> product workbench
```

ProductFlow owns products, Drafts, confirmation, WorkflowGraphRun, and the Web projection. The Agent service runs the model loop with the Pi SDK and stores session/event files under its data root; those files are not business authority. PostgreSQL stores AgentSession, AgentTask, AgentConversation, Turn projections, PageContextSnapshot, question state, WorkflowDraft revisions, and the cross-instance browser event store `agent_turn_events`.

Product creation writes Product, assets, WorkflowDraft, the product Conversation, the onboarding AgentTask, and AgentSession in one business transaction. The onboarding Task starts as `WAITING_USER`, closes as `SUCCEEDED` after intake, and does not own workflow execution. The Global Conversation and product Conversation share a Session and do not merge transcripts. Standalone Session creation does not require a title; a temporary title comes from the first global Turn, and an explicit rename wins. Global Agent product-workspace creation reconciles with `creation_idempotency_key` and `creation_request_hash`.

`GlobalAgentDock` owns Session/Task lists, search, jumps, and pending organization Drafts. It does not own the canvas or WorkflowGraphRun. Global media organization only publishes a `LibraryOrganizationDraft`; ProductFlow re-reads facts and applies the Draft after user confirmation.

Main promises interactive Turns, cancel, question answers, SSE reconnect, and cross-process Pi session context reload. It does not promise in-place model-request recovery, background durable Tasks, complete multi-instance scheduling, or full effect reconciliation. Lease, fencing, continuation Turns, the `tool_steps` allowlist, and effect reconciliation are defined by `application/agent/`, `agent-service/src/pi-runtime.ts`, and `test_workflow_agent_service.py`, `test_agent_product_workspaces.py`, and `test_media_library_drafts.py`.

Implementation path: `routes/agent_product_workspaces.py` → `agent/product_workspaces.py`; Turn control `agent/control.py` → `infrastructure/agent_service.py` → `agent-service/src/pi-runtime.ts`; projection `agent/sync.py`; product Drafts `workflow_drafts/service.py` and `product_workflow/graph_draft_persist.py`; global media Drafts `media_library/drafts.py`.

## 5. WorkflowDraft

WorkflowDraft is the persistent boundary between Agent output and user confirmation. A revision contains:

- Product facts with source, state, and conflicts.
- Selected image types and per-type quantities.
- Workflow-level visual system.
- Per-image prompts, reference bindings, and generation specifications.
- Folder, node, and edge plans.
- Optional user-recipe seed.

Draft states cover collecting, awaiting_confirmation, confirmed, materializing, ready, and the failed/cancelled terminals. Every Agent artifact appends a revision. Confirmation targets an explicit revision to prevent concurrent overwrite.

## 6. Online schema-v3 Graph

The online workflow lives on `workflow_graphs` with schema version 3. Node types are:

- `product_source`: product-facts entry.
- `image_asset`: one-to-one ProductImageAsset binding.
- `creative_brief`: running the node writes a creative brief from product facts and photos; the result is editable.
- `visual_system`: running the node writes style and background constraints from product facts and photos; the result is editable.
- `prompt_generation`: running the node writes a prompt from upstream context; the result is editable.
- `image_generation`: generate images from the current prompt and GenerationSpec. Running this node does not fill empty visual or brief nodes; run those first, or use run-to-node.

Canvas groups are one-level visual folders. You can enter a group and remember its viewport separately from the full graph. Groups do not change DAG execution, grow ports, or run/cancel/retry. Cross-group edges stay visible on the full graph. Edges use Node Catalog data types and roles. Node inspector forms render from the same `config_fields` document and save with `update_node_config`.

`WorkflowGraphRun` and `WorkflowGraphNodeRun` store execution state. Execution reads the run snapshot, not the live graph. Image results write ProductImageAsset and `WorkflowGraphArtifact` rows.

Workflow runs are created and validated through ProductFlow business endpoints. The workbench can submit the whole graph or one node without an Agent Conversation first. Agent run requests go through `agent_workflow_run_requests.py`; user confirmation uses the same `graph_runs.py` / `graph_execution.py` constraints.

WorkflowRecipe stores user-created full workflows or fragments. Saving extracts a live schema-v3 graph fragment (nodes, edges, groups) without product identity, bound assets, generated results, or media bytes. Applying a full recipe to another product still creates a reviewable Draft; the workbench previews the nodes and edges that will be created. Fragment recipes return an explicit conflict against an existing workflow. The save HTTP entry is `POST /api/v3/products/{product_id}/workflows/{workflow_id}/recipes`.

Graph rules live in `domain/graph_catalog.py` and `domain/graph_rules.py`. Structure commands use `graph_commands.py` / `graph_apply.py`. Runs use `graph_runs.py` / `graph_execution.py`. HTTP entry is `presentation/routes/workflow_graphs.py`.

## 7. Image Model

`MediaObject` stores path, MIME type, byte size, dimensions, hash, and verification state. `ProductImageAsset` stores product-scoped display name, origin, folder, parent image, and image type.

Current origins:

- `upload`
- `workflow_generation`
- `image_session_attach`

`legacy_import` identifies readable canonical migration assets only; it is not an online write origin.

The product library, node reference bindings, covers, and delivery renditions all use ProductImageAsset ids. Every image-session asset also has a MediaObject; saving it to a product creates a ProductImageAsset.

DeliveryRenditionJob reads a ProductImageAsset and asynchronously emits a crop, resize, and format-specific delivery file. It does not replace the source image.

GenerationSpec records model-generation intent. Provider-effective values and decoded actual output live in run/generation records. DeliverySpec is a separate deterministic contract and cannot invoke the image model.

Global media-library reads, folder/tag/archive organization, source saves, and workflow associations are owned by `application/media_library/`, `routes/media_library.py`, `MediaLibraryPage.tsx`, and `WorkflowMediaLibraryPanel.tsx`, using bounded cursor pages and preview/thumbnail URLs. `/gallery` preserves a bookmark redirect; the old `/api/gallery` route, DTOs, and online runtime owner have been removed. `application/legacy_retirement/media_library.py` and `commands/backfill_media_library.py` provide bounded migration reads for the retained current-schema table; `legacy_retirement/gallery_bridge.py` and the four `legacy_gallery_bridge` commands own the old Canvas revision's Gallery-only manifest, target import, reconciliation, and source retirement. The product workbench's `workbench/chrome/image-explorer/` continues to own product-scoped manual selection and binding.

## 8. Provider Architecture

`ProviderProfile` stores endpoint, secret, capabilities, default models, and provider configuration. `ProviderBinding` maps one profile to a purpose:

- `prompt`: visual system, creative brief, and prompt nodes.
- `agent`: workflow Agent.
- `image`: workflow and image-session generation.

FastAPI resolves prompt/image bindings. The Agent service obtains the agent binding through an internal-token-protected endpoint. Missing bindings, disabled profiles, and empty models produce explicit configuration failures.

Runtime image-tool settings are filtered through the allowed-field contract before a provider adapter maps them. Candidate count comes from image-type quantity or image-session generation_count and is not an advanced tool option.

## 9. Asynchronous Work and Recovery

- Dramatiq executes workflow nodes, image-session candidates, and delivery renditions.
- The async dispatcher scans durable dispatch/recovery rows in PostgreSQL and delivers them to Redis. `just dev` and Compose both start this process.
- Redis provides the broker and concurrency admission.
- PostgreSQL stores queued/running/terminal states, attempts, and safe errors.
- Worker startup recovers unfinished jobs that can be safely redelivered.
- Agent service uses Pi sessions and local event files for runtime recovery; events written with the current lease/fencing token are appended to PostgreSQL `agent_turn_events`, and FastAPI SSE replays them by cursor. Browser disconnect does not cancel the Agent. Startup recovery only requeues never-started queued Turns. Unprovable outcomes remain `unknown`. Background durable Tasks and full reconciliation live in `ROADMAP.en.md` and `rollout/pi-agent-durability.md`.
- ProductFlow Turn sync trusts only state that satisfies the Agent service wire contract and preserves unprovable outcomes as unknown.

## 10. Configuration and Security

Environment variables hold infrastructure and secrets required before database access:

- database, Redis, and storage
- admin, session, and settings tokens
- Agent service address and internal token
- upload, logging, and worker base settings

Provider profiles, purpose bindings, and business runtime settings are stored through `/settings`. The page requires an administrator session and independent `SETTINGS_ACCESS_TOKEN`.

Uploads are checked for MIME, actual image format, byte size, pixel count, and count before persistence. Download endpoints locate storage through database assets and never accept arbitrary file paths.

## 11. Schema Evolution And V1 Retirement

SQLAlchemy metadata describes current online models and bounded archive/cutover models. Historical Alembic revisions remain so a fresh database can upgrade to head. Runtime code does not read V1 source shapes or keep parallel routes and executors.

`20260816_0042` creates only the `legacy_cutover_gates` evidence boundary and retains V1 source/archive rows. Removing the online V1 code path does not prove that a deployment completed its data cutover. Production source/archive/canonical hashes, restore-rehearsal time, and zero active/unknown runs must be produced through the runbook. Any future destructive cleanup must pass the gate in the same transaction.

## 12. Quality Gates

- Backend: Ruff, full pytest, SQLite migration, and opt-in PostgreSQL/Redis live tests.
- Frontend: Vitest, ESLint, TypeScript, and Vite production build. The skip-Agent full-graph browser gate against real prompt/image providers is opt-in: `just web-e2e-live-graph`.
- Agent service: `pnpm --dir agent-service test`, `pnpm --dir agent-service build`, and explicit live provider/dependency gates.
- Cross-layer changes add real browser, database, or provider validation according to risk.

Code/document synchronization rules:

- Route changes update `App.tsx`, `lib/api.ts`, the PRD page table, and the user guide together.
- User-operation changes update `USER_GUIDE.en.md` and `web/src/pages/HelpPage.tsx` together.
- Enum/DTO changes check `domain/enums.py`, Pydantic schemas, `lib/types.ts`, label maps, and parser tests.
- Transaction/queue/recovery changes trace the application entrypoint, durable row, broker call, worker claim, and recovery tests.
- Historical-value changes use a persisted fixture through migration, API serialization, and frontend rendering; a newly constructed current DTO alone does not prove compatibility.
- Module moves update this ownership table and package `AGENTS.md`, and remove references to the old path.
