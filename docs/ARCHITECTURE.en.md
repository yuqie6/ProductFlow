# ProductFlow Architecture

## 1. System Boundary

ProductFlow is a single-administrator, single-merchant workspace with seven runtime units:

1. React/Vite Web.
2. Go business API.
3. Go worker.
4. Go async dispatcher.
5. Node.js 22 + Pi SDK ProductFlow Agent service.
6. PostgreSQL.
7. Redis and media storage.

The browser reaches only Web and the business API. The Agent service calls internal business-API endpoints with a dedicated bearer token; the API controls Agent Turns over the agent-service internal HTTP/SSE API. API, worker, and the async dispatcher share PostgreSQL, Redis, and storage. `just dev` and Docker Compose both start the dispatcher. Default processes are `go/cmd/productflow-api`, `productflow-worker`, and `productflow-dispatcher`. Schema is applied by `productflow-migrate` before those processes start. `backend/` keeps the sealed Python tree and an optional Python fallback (Compose profile `python`) and is not the default runtime.

This document describes the current implementation only. Module ownership comes from the live source tree and behavior evidence comes from the referenced tests. Product contracts live in `PRD.en.md` and durable rationale in `adr/`. Read `adr/0007-pi-agent-runtime-boundary.md` and `specs/pi-agent-runtime-integration.md` when changing the Agent service.

## 2. Backend Layers

The business backend is vertically sliced under `go/internal/`. HTTP uses Gin, PostgreSQL access uses GORM (still on the pgx driver; command transactions use `tx.WithGorm` and raw SQL), and async delivery uses an asynq envelope. PostgreSQL `async_dispatches` and business tables remain the state authority. Schema authority is `productflow-migrate`: GORM `CreateTable`/`AddColumn` plus ExtraDDL (CHECK / enum / partial unique / FK). AutoMigrate is not used.

`backend/src/productflow_backend/` is the sealed Python tree and migration source, not the default process.

Current code ownership:

| Capability | Package | HTTP / process | Primary regression tests |
|---|---|---|---|
| Agent product creation | `go/internal/product`, `go/internal/agent` | `productflow-api` | `go/internal/product`, `go/internal/agent` |
| Agent Session and Task | `go/internal/agent` | `productflow-api` | `go/internal/agent/http_test.go` |
| Agent Turn and sync | `go/internal/agent` | `productflow-api`, `productflow-worker` | `go/internal/agent` |
| Agent tools and context | `go/internal/agent` | internal HTTP | `go/internal/agent/surface_test.go` |
| Global media library | `go/internal/library` | `productflow-api` | `go/internal/library` |
| Global library organization Draft | `go/internal/library` | `productflow-api` | `go/internal/library`, `go/internal/agent` |
| schema-v3 graph and execution | `go/internal/graph` | `productflow-api`, `productflow-worker` | `go/internal/graph` |
| Recipes | `go/internal/recipe` | `productflow-api` | `go/internal/recipe` |
| Delivery renditions | `go/internal/delivery` | `productflow-api`, `productflow-worker` | `go/internal/delivery` |
| Product image library | `go/internal/product`, `go/internal/media` | `productflow-api` | `go/internal/product` |
| Iterative image generation | `go/internal/imagesession` | `productflow-api`, `productflow-worker` | `go/internal/imagesession` |
| Local image edits | `go/internal/localedit` | `productflow-api`, `productflow-worker` | `go/internal/localedit` |
| Settings and providers | `go/internal/settings`, `go/internal/providers` | `productflow-api`; worker resolves bindings | `go/internal/settings`, `go/internal/providers` |
| Async dispatch | `go/internal/platform/queue` | `productflow-dispatcher`, `productflow-worker` | `go/internal/platform/queue`, graph/image-session delivery tests |
| Schema evolution | `go/internal/platform/db/schema` | `productflow-migrate` | `go/internal/platform/db/schema` |
| Errors and logging | `go/internal/platform/apperr`, `httpx`, `log` | middleware and workers | platform and package HTTP tests |

## 3. Frontend Structure

`web/src/App.tsx` registers the current pages:

- `/login`
- `/products`
- `/products/new`
- `/products/new/agent`, redirect only
- `/products/:productId`
- `/image-chat`
- `/media-library`
- `/settings`
- `/help`

Page code lives in `web/src/pages/`. Shared visual components live in `web/src/components/`. HTTP, DTOs, i18n, and browser preferences live in `web/src/lib/`.

The product workbench lives in `pages/workbench/` and is split by duty:

- `workbench/agent/`: page orchestration, conversation, SSE events, questions, graph-proposal confirmation, and the Goal loop.
- `workbench/canvas/`: current Graph canvas, inspector, runs, recipes, and delivery renditions.
- `workbench/chrome/`: canvas chrome, node cards, sidebar, shortcuts, and image Explorer.

Dependency direction is `agent -> canvas, chrome` and `canvas -> chrome`. `chrome` must not import agent or canvas.

TanStack Query owns server state. React state owns local forms, selection, and canvas interaction. `api.ts` is the browser HTTP boundary.

Current frontend ownership:

| Capability | Owner | Primary tests |
|---|---|---|
| Agent creation form | `AgentProductCreatePage.tsx`, `pages/product-create/` | selection/form/workspace API tests |
| Agent conversation, SSE, Goal | `pages/workbench/agent/` | reducer, event, conversation, Goal, and proposal tests |
| Graph canvas and inspector | `pages/workbench/canvas/` | graph catalog/layout/canvas, inspector, runs, and rendition tests |
| Global media library and workflow sub-library | `MediaLibraryPage.tsx`, `workbench/canvas/WorkflowMediaLibraryPanel.tsx` | media library/application tests, web build |
| Global Agent Dock | `components/GlobalAgentDock.tsx` | `GlobalAgentDockComponents.test.ts` |
| Shared workbench and image library | `pages/workbench/chrome/` | shortcuts, interaction, image-explorer tests |
| HTTP and wire DTOs | `lib/api.ts`, `lib/types.ts` | `lib/*Api.test.ts`, TypeScript build |

## 4. Agent Creation Flow

```text
product name (+ optional types and 1..6 uploads)
  -> Product + live schema-v3 graph + product-owned AgentSession + AgentConversation
  -> user first message
  -> ProductFlow submits Agent Turn
  -> agent-service / Pi SDK ProductFlow adapter
  -> apply / propose ChangeSet on the live graph
  -> product workbench
```

ProductFlow owns products, graph-proposal confirmation, WorkflowGraphRun, and the Web projection. The Agent service runs the model loop with the Pi SDK and stores session/event files under its data root; those files are not business authority. PostgreSQL stores AgentSession, AgentTask, AgentConversation, Turn projections, PageContextSnapshot, question state, `LibraryOrganizationDraft` revisions, and the cross-instance browser event store `agent_turn_events`. Event `run_id` and the Turn projection `harness_run_id` use `expected_harness_run_id` in `application/agent/turn_projection.py`: the Task run when a Turn is bound to a Task, otherwise the Conversation run.

Product creation writes one business transaction. It does not create an onboarding Task or auto-submit a Turn. Name-only graphs contain `product_source`. A complete Agent form uses the same graph template as direct create (`graph.BuildDirectCreateTemplate`) but a different persist set: Agent form-complete writes Product intake and does not set cover; direct create (`POST /api/v3/products`) writes no intake, sets cover to the first image, and creates no Session. `POST /api/v2/products` can still create a covered product without a live graph; an empty graph can be added later with `POST /api/v3/products/{id}/workflows`. Canvas Sessions have a non-null `product_id`; the global Dock list contains only Sessions with `product_id` null. Standalone global Session creation does not require a title; a temporary title comes from the first global Turn, and an explicit rename wins. Global Agent product-workspace creation opens a new canvas Session and reconciles with `creation_idempotency_key` and `creation_request_hash`.

`GlobalAgentDock` owns Session/Task lists, search, jumps, and pending organization Drafts. It does not own the canvas or WorkflowGraphRun. Global media organization only publishes a `LibraryOrganizationDraft`; ProductFlow re-reads facts and applies the Draft after user confirmation.

Main promises interactive Turns, cancel, question answers, SSE reconnect, and cross-process Pi session context reload. Question answers are continuation Turns on the ProductFlow side (`control.answer_agent_question`); they do not call `AgentServiceClient.answer_question`. It does not promise in-place model-request recovery, background durable Tasks, complete multi-instance scheduling, or full effect reconciliation. Lease, fencing, continuation Turns, the `tool_steps` allowlist, and effect reconciliation are defined by `application/agent/`, `agent-service/src/pi-runtime.ts`, and `test_workflow_agent_service.py`, `test_agent_product_workspaces.py`, and `test_media_library_drafts.py`. Python `AgentToolStepKind` currently omits `apply_graph` / `propose_graph` that Pi already emits; the Go allowlist must match Pi and must not treat that gap as product contract.

Implementation path: `routes/agent_product_workspaces.py` → `agent/product_workspaces.py`; Turn control `agent/control.py` → `infrastructure/agent_service.py` → `agent-service/src/pi-runtime.ts`; projection `agent/sync.py`; global media Drafts `media_library/drafts.py`. Product `WorkflowDraft` HTTP is gone; those URLs return 404. A product Goal is an explicit `AgentTask`: a finished Turn or `WorkflowGraphRun` does not complete the Goal; the user completes it with `POST /api/v2/agent-tasks/{id}/complete`.

## 5. Product intake and retired WorkflowDraft topology

Image types, quantities, and reference asset ids live on Product intake. Create does not insert `WorkflowDraft`. A product Conversation only requires `product_id`. Product-path `propose_workflow_draft` / confirm / persist do not exist; those URLs return 404. The `workflow_drafts` table is dropped.

Global library organize still uses `LibraryOrganizationDraft`. Multi-node graph confirmation uses `WorkflowGraphProposal`.

## 6. Online schema-v3 Graph

The online workflow lives on `workflow_graphs` with schema version 3. The decision to keep one live graph instead of a second Draft topology is `adr/0008-free-canvas-agent-graph-authority.md`.

The graph has three authority objects: Node (config and current output reference), Edge (type, role, order, dependency), and Artifact (immutable result of one run). Users, the Agent, and recipes all write through `apply_graph_change_set`. ChangeSet operations: `create_node`, `update_node_config`, `rename_node`, `delete_node`, `connect_nodes`, `disconnect_edge`, `move_nodes`, `create_group`, `move_nodes_to_group`, `rename_group`, `dissolve_group`. Incomplete DAGs may persist; run-time checks completeness separately. Processing nodes expose at most one aggregate input port. Runtime context reads only the target node's incoming edges. Owners: `domain/graph_catalog.py` and `domain/graph_rules.py`.

Node types are:

- `product_source`: product-facts entry.
- `image_asset`: one-to-one ProductImageAsset binding.
- `creative_brief`: running the node writes a creative brief from product facts and photos; the result is editable.
- `visual_system`: running the node writes style and background constraints from product facts and photos; the result is editable.
- `prompt_generation`: running the node writes a prompt from upstream context; the result is editable.
- `image_generation`: generate images from the current prompt and GenerationSpec. Running this node does not fill empty visual or brief nodes; run those first, or use run-to-node.

Canvas groups are one-level visual folders. You can enter a group and remember its viewport separately from the full graph. Groups do not change DAG execution, grow ports, or run/cancel/retry. Cross-group edges stay visible on the full graph. Edges use Node Catalog data types and roles. Node inspector forms render from the same `config_fields` document and save with `update_node_config`.

`WorkflowGraphRun` and `WorkflowGraphNodeRun` store execution state. Execution reads the run snapshot, not the live graph. Image results write ProductImageAsset and `WorkflowGraphArtifact` rows. One worker holds a run; independent processing nodes may call providers concurrently, limited by runtime `generation_max_concurrent_tasks`. A failed or unknown node does not stop independent siblings; downstream of a failed upstream is marked failed. Evidence: `graph_execution.py`, `test_graph_execution.py`.

Workflow runs are created and validated through ProductFlow business endpoints. The workbench can submit the whole graph or one node without an Agent Conversation first. Agent run requests go through `agent_workflow_run_requests.py`; user confirmation uses the same `graph_runs.py` / `graph_execution.py` constraints.

After a live graph exists, the Agent cannot submit a product Draft artifact. Single reversible edits go through `apply_graph_change_set`. Multi-node rewrites land as an unapplied `WorkflowGraphProposal`; ghost preview, confirm, and cancel happen on the canvas.

WorkflowRecipe stores user-created full workflows or fragments. The recipe library lists only recipes the user saved from a live graph; it does not ship preset canvas templates. Saving extracts a live schema-v3 graph fragment (nodes, edges, groups) without product identity, bound assets, generated results, or media bytes. A full recipe creates a live graph only when the target product has none; an existing graph returns a conflict. Fragment recipes merge into an existing schema-v3 graph or return an explicit conflict. They are not written as a Draft. Save HTTP is `POST /api/v3/products/{product_id}/workflows/{workflow_id}/recipes`; preview/apply are `POST /api/v3/products/{product_id}/workflow-recipes/{recipe_id}/preview` and `.../apply`.

A product with no live graph can `POST /api/v3/products/{product_id}/workflows` to persist an empty schema-v3 graph (revision 1, no nodes or edges). A second create is a conflict. Empty birth is not an empty ChangeSet (`operations` has min length 1). Later node/edge writes still use `apply_graph_change_set`.

Graph rules live in `domain/graph_catalog.py` and `domain/graph_rules.py`. Structure writes use `graph_commands.py` (`apply_graph_change_set` / `stage_apply_graph_change_set`); the in-memory apply engine is `apply_workflow_change_set` in `graph_apply.py`. Runs use `graph_runs.py` / `graph_execution.py` / `graph_run_durability.py`. HTTP entry is `presentation/routes/workflow_graphs.py`.

## 7. Image Model

`MediaObject` stores path, MIME type, byte size, dimensions, hash, and verification state. `ProductImageAsset` stores product-scoped display name, origin, folder, parent image, and image type.

Current origins:

- `upload`
- `workflow_generation`
- `image_session_attach`
- `local_edit`

The product library, node reference bindings, covers, and delivery renditions all use ProductImageAsset ids. Every image-session asset also has a MediaObject; saving it to a product creates a ProductImageAsset.

DeliveryRenditionJob reads a ProductImageAsset and asynchronously emits a crop, resize, and format-specific delivery file. It does not replace the source image. Built-in DeliverySpec templates live in `application/delivery_renditions/presets.py`. The read-only API order is Taobao/Tmall hero 3:4, JD hero 1:1, Amazon hero 1:1, detail portrait 3:4, and scene landscape 4:3. Templates are convenience defaults, not platform-compliance guarantees; users can override size, format, and byte limits. Source label: `docs/ARCHITECTURE.md §7`.

GenerationSpec records model-generation intent. Provider-effective values and decoded actual output live in run/generation records. DeliverySpec is a separate deterministic contract and cannot invoke the image model.

Global media-library reads, folder/tag/archive organization, source saves, and workflow associations are owned by `application/media_library/`, `routes/media_library.py`, `MediaLibraryPage.tsx`, and `WorkflowMediaLibraryPanel.tsx`, using bounded cursor pages and preview/thumbnail URLs. The product workbench's `workbench/chrome/image-explorer/` continues to own product-scoped manual selection and binding.

## 8. Provider Architecture

`ProviderProfile` stores endpoint, secret, capabilities, default models, and provider configuration. `ProviderBinding` maps one profile to a purpose:

- `prompt`: visual system, creative brief, and prompt nodes.
- `agent`: workflow Agent.
- `image`: workflow and image-session generation.

The Go business API resolves prompt/image bindings. The Agent service obtains the agent binding through an internal-token-protected endpoint. Missing bindings, disabled profiles, and empty models produce explicit configuration failures.

Runtime image-tool settings are filtered through the allowed-field contract before a provider adapter maps them. Candidate count comes from image-type quantity or image-session generation_count and is not an advanced tool option.

## 9. Asynchronous Work and Recovery

- The Go worker executes workflow nodes, image-session candidates, delivery renditions, and local edits.
- The async dispatcher scans durable dispatch/recovery rows in PostgreSQL and delivers them to Redis. `just dev` and Compose both start this process.
- Redis provides the broker and concurrency admission.
- PostgreSQL stores queued/running/terminal states, attempts, and safe errors.
- Worker startup recovers unfinished jobs that can be safely redelivered.
- Agent service uses Pi sessions and local event files for runtime recovery; events written with the current lease/fencing token are appended to PostgreSQL `agent_turn_events`, and the business API SSE replays them by cursor. Browser disconnect does not cancel the Agent. Startup recovery only requeues never-started queued Turns. Unprovable outcomes remain `unknown`. Background durable Tasks and full reconciliation live in `ROADMAP.en.md` and `rollout/pi-agent-durability.md`.
- ProductFlow Turn sync trusts only state that satisfies the Agent service wire contract and preserves unprovable outcomes as unknown.

## 10. Configuration and Security

Environment variables hold infrastructure and secrets required before database access:

- database, Redis, and storage
- admin, session, and settings tokens
- Agent service address and internal token
- upload, logging, and worker base settings

Provider profiles, purpose bindings, and business runtime settings are stored through `/settings`. The page requires an administrator session and independent `SETTINGS_ACCESS_TOKEN`.

Uploads are checked for MIME, actual image format, byte size, pixel count, and count before persistence. Download endpoints locate storage through database assets and never accept arbitrary file paths.

API, worker, and dispatcher JSON logs go to stderr and rotate under `STORAGE_ROOT/logs/` (local `storage-dev/logs/`, Compose `/app/storage/logs`): `productflow-api.log`, `productflow-worker.log`, `productflow-dispatcher.log`. `LOG_DIR` overrides the directory. Owner and tests: `go/internal/platform/log`.

## 11. Schema Evolution

Empty and existing databases both run `productflow-migrate`: GORM `CreateTable`/`AddColumn` creates or adds tables and columns, then ExtraDDL idempotently adds CHECKs, PostgreSQL enums, partial unique indexes, and FKs. AutoMigrate is not used (it rewrites unique index names on an existing head). The command does not drop retired tables or columns; those deletions stay explicit under ADR 0010. `backend/alembic/` is sealed history and is no longer on `just dev` or default Compose. The main repository does not write old-data backfill, freeze, or cutover gates. Following mainline may recreate the database and storage.

## 12. Quality Gates

- Backend: Go `go test ./...`, `productflow-migrate`, and opt-in PostgreSQL/Redis live tests.
- Frontend: Vitest, ESLint, TypeScript, and Vite production build. The skip-Agent full-graph browser gate against real prompt/image providers is opt-in: `just web-e2e-live-graph`.
- Agent service: `pnpm --dir agent-service test`, `pnpm --dir agent-service build`, and explicit live provider/dependency gates.
- Cross-layer changes add real browser, database, or provider validation according to risk.

Code/document synchronization rules:

- Route changes update `App.tsx`, `lib/api.ts`, the PRD page table, and the user guide together.
- User-operation changes update `USER_GUIDE.en.md` and `web/src/pages/HelpPage.tsx` together.
- Enum/DTO changes check Go DTOs, `lib/types.ts`, label maps, and parser tests.
- Transaction/queue/recovery changes trace the application entrypoint, durable row, broker call, worker claim, and recovery tests.
- Module moves update this ownership table and package `AGENTS.md`, and remove references to the old path.
