# ProductFlow Architecture

## 1. System Boundary

ProductFlow is a single-administrator, single-merchant workspace with six runtime units:

1. React/Vite Web.
2. FastAPI business API.
3. Dramatiq worker.
4. Go workflow Agent service.
5. PostgreSQL.
6. Redis and media storage.

The browser reaches only Web and FastAPI. The Agent service calls FastAPI internal endpoints with a dedicated bearer token; FastAPI controls Agent Turns over the agent-service internal HTTP/SSE API. API and worker share PostgreSQL, Redis, and storage.

This document describes the current implementation only. Module ownership comes from the live source tree and behavior evidence comes from the referenced tests. Product contracts live in `PRD.en.md`, durable rationale in `adr/`, and incomplete deployment evidence in `rollout/`.

## 2. Backend Layers

`backend/src/productflow_backend/` keeps four boundaries:

- `presentation/`: FastAPI routes, request/response schemas, authentication, upload reads, and HTTP error mapping.
- `application/`: product, Agent conversation, WorkflowDraft, V2 workflow, image library, image session, settings, and asynchronous use cases.
- `domain/`: enums, business errors, and database-free DAG rules.
- `infrastructure/`: SQLAlchemy, provider clients, Redis/Dramatiq, storage, logging, and the Agent service client.

Routes do not own complex transactions or provider payload construction. The application layer owns business transactions, and infrastructure adapts external systems. FastAPI dependencies and workers own Session lifetime; a public application command may own one explicit commit/rollback, while internal `stage_*` helpers only assemble or flush.

Current code ownership:

| Capability | Application/Domain owner | HTTP/External owner | Primary regression tests |
|---|---|---|---|
| Agent product creation | `agent_product_workspaces.py`, `agent_product_intake.py` | `routes/agent_product_workspaces.py` | `test_agent_product_workspaces.py` |
| Agent Turn and sync | `agent_conversations.py`, `agent_control.py`, `agent_sync.py` | `routes/agent_conversations.py`, `infrastructure/agent_service.py` | `test_workflow_agent_service.py` |
| Draft and materialization | `workflow_drafts/contracts.py`, `service.py`, `materialization.py` | `routes/workflow_drafts.py` | Draft contract/materialization tests |
| V2 graph and execution | `domain/workflow_rules.py`, `product_workflow/v2_*.py`, `execution.py` | `routes/workflow_drafts.py`, `workers.py` | workflow domain/run/node/recovery tests |
| Product image library | `gallery.py`, `gallery_assets.py`, `gallery_mutations.py`, `gallery_archives.py`, `media_assets.py` | `routes/products.py` | gallery/media tests |
| Iterative image generation | `image_sessions.py`, `image_generation_core.py` | `routes/image_sessions.py`, image adapters | image-session/provider tests |
| Settings and providers | `settings.py`, `runtime_settings.py` | `routes/settings.py`, `infrastructure/provider_config.py` | settings/provider/runtime tests |
| V1 archive cutover | `legacy_archives.py`, `legacy_retirement/` | `routes/legacy_archives.py`, `commands/` | archive/cutover/migration tests |
| Errors and logging | `domain/errors.py` | `presentation/errors.py`, `infrastructure/logging.py`, middleware and workers | error/logging tests |

## 3. Frontend Structure

`web/src/App.tsx` registers the current pages:

- `/login`
- `/products`
- `/products/new`
- `/products/new/agent`, redirect only
- `/products/:productId`
- `/image-chat`
- `/gallery`
- `/history` and `/history/:archiveKind/:archiveId`
- `/settings`
- `/help`

Page code lives in `web/src/pages/`. Shared visual components live in `web/src/components/`. HTTP, DTOs, i18n, and browser preferences live in `web/src/lib/`.

The product workbench composes three existing component groups:

- `agent-workbench/`: conversation, SSE events, questions, Draft confirmation, and materialization reveal.
- `product-workflow-v2/`: V2 canvas, command bar, inspector, runs, recipes, and delivery renditions.
- `product-detail/`: reused canvas chrome, node cards, sidebar, shortcuts, and image Explorer.

TanStack Query owns server state. React state owns local forms, selection, and canvas interaction. `api.ts` is the browser HTTP boundary.

Current frontend ownership:

| Capability | Owner | Primary tests |
|---|---|---|
| Agent creation form | `AgentProductCreatePage.tsx`, `pages/product-create/` | selection/form/workspace API tests |
| Agent conversation, SSE, Draft confirmation | `pages/agent-workbench/` | reducer, event, conversation, confirmation, reveal tests |
| V2 canvas and inspector | `pages/product-workflow-v2/` | graph, canvas, command, draft, history, rendition tests |
| Shared workbench and image library | `pages/product-detail/` | shortcuts, interaction, image-explorer tests |
| HTTP and wire DTOs | `lib/api.ts`, `lib/types.ts` | `lib/*Api.test.ts`, TypeScript build |
| Read-only history | `LegacyHistoryPage.tsx`, `pages/legacy-history/` | legacy history/model/API tests |

## 4. Agent Creation Flow

```text
image types + quantities + 1..6 uploads
  -> Product + ProductImageAsset + WorkflowDraft + AgentConversation
  -> ProductFlow submits Agent Turn
  -> agent-service / agent-harness durable execution
  -> ProductFlow internal read and mutation tools
  -> versioned WorkflowDraft artifact
  -> user confirmation
  -> schema-v2 workflow materialization
  -> reveal event stream
  -> product workbench
```

ProductFlow is authoritative for business data. The Agent service stores durable Turn transcript, tool calls/results, and token deltas. PostgreSQL stores AgentConversation, AgentTurnProjection, question state, and WorkflowDraft revisions.

The Agent service exposes a bounded tool-step projection through `tool.step` SSE events and Turn state `tool_steps`, with fields fixed to `step_id`, `kind`, `summary`, and `status`. Current kinds are `inspect_image`, `inspect_context`, `read_history`, `organize_assets`, and `propose_draft`; current statuses are `running`, `succeeded`, `failed`, and `unknown`. `question.required` remains owned by Question and is not projected as a tool step; there is no real `generate_image` Agent tool yet, so it is not added prematurely. `AgentTurnProjection.tool_steps_json` stores this web projection: a missing `tool_steps` keeps the existing snapshot for compatibility with older services, while an explicit `[]` clears it.

When reading product assets, the Agent first receives bounded metadata and then inspects selected images. Image tool results use a versioned multimodal contract; the full library and data URLs are not concatenated into text history.

Agent mutations such as rename, folder creation, and move use prepare/apply/reconcile contracts with idempotency keys so network interruption and restart can be reconciled.

The implementation path is: `routes/agent_product_workspaces.py` receives workspace creation; `agent_product_workspaces.py` persists Product, assets, Draft, and Conversation in one business transaction; `agent_control.py` calls `infrastructure/agent_service.py`; `agent_sync.py` projects the durable Turn; `workflow_drafts/service.py` validates the artifact; and `workflow_drafts/materialization.py` atomically writes the V2 graph and reveal events.

## 5. WorkflowDraft

WorkflowDraft is the persistent boundary between Agent output and user confirmation. A revision contains:

- Product facts with source, state, and conflicts.
- Selected image types and per-type quantities.
- Workflow-level visual system.
- Per-image prompts, reference bindings, and generation specifications.
- Folder, node, and edge plans.
- Optional user-recipe seed.

Draft states cover collecting, awaiting_confirmation, confirmed, materializing, ready, and the failed/cancelled terminals. Every Agent artifact appends a revision. Confirmation targets an explicit revision to prevent concurrent overwrite.

## 6. V2 Workflow

The online ProductWorkflow schema is fixed at 2. Node types are:

- `product_context`: product facts and visual-system entry.
- `reference_image`: one ProductImageAsset binding.
- `prompt_generation`: generate, edit, and version one-image prompts.
- `image_generation`: generate images from upstream context and GenerationSpec.

WorkflowFolder is a local visual group and does not alter DAG execution. WorkflowEdge represents dependency. Domain topological sorting rejects cross-workflow references and cycles.

Folders are one level deep and own no run state, ports, nesting, cancel, or retry semantics. Aggregate state is derived from member nodes.

WorkflowRun and WorkflowNodeRun store execution state. The worker schedules ready nodes after their upstream dependencies succeed. Prompt artifacts are versioned. Image results become ProductImageAsset records and bind back to target nodes.

Workflow execution is created and validated through ProductFlow business endpoints. The workflow page can submit the whole DAG or one node directly, without creating an Agent Conversation first; future Agent-triggered runs must reuse the same application use cases, permission, revision, and queue constraints.

WorkflowRecipe stores user-created full workflows and fragments. Recipe payloads store reusable structure and configuration, without product identity, generated results, or media bytes.

`domain/workflow_rules.py` owns graph rules. Structure commands, node editing, reference binding, and execution live under `application/product_workflow/v2_*.py`. Workers enter through `workers.py` and call `application/product_workflow/execution.py`; they do not maintain a second executor.

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

Library reads, asset detail, folder/move commands, ZIP construction, and media identity are owned by `gallery.py`, `gallery_assets.py`, `gallery_mutations.py`, `gallery_archives.py`, and `media_assets.py`. The browser's only library owner is `pages/product-detail/image-explorer/`, which uses bounded cursor pages and preview/thumbnail URLs.

## 8. Provider Architecture

`ProviderProfile` stores endpoint, secret, capabilities, default models, and provider configuration. `ProviderBinding` maps one profile to a purpose:

- `prompt`: prompt nodes.
- `agent`: workflow Agent.
- `image`: workflow and image-session generation.

FastAPI resolves prompt/image bindings. The Agent service obtains the agent binding through an internal-token-protected endpoint. Missing bindings, disabled profiles, and empty models produce explicit configuration failures.

Runtime image-tool settings are filtered through the allowed-field contract before a provider adapter maps them. Candidate count comes from image-type quantity or image-session generation_count and is not an advanced tool option.

## 9. Asynchronous Work and Recovery

- Dramatiq executes workflow nodes, image-session candidates, and delivery renditions.
- Redis provides the broker and concurrency admission.
- PostgreSQL stores queued/running/terminal states, attempts, and safe errors.
- Worker startup recovers unfinished jobs that can be safely redelivered.
- Agent service uses the agent-harness durable journal; SSE event sequences support Last-Event-ID replay.
- ProductFlow Turn sync trusts only reconcilable harness state and preserves unknown outcomes as unknown.

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
- Frontend: Vitest, ESLint, TypeScript, and Vite production build.
- Agent service: `go test ./...` and an opt-in live-provider transcript test.
- Cross-layer changes add real browser, database, or provider validation according to risk.

Code/document synchronization rules:

- Route changes update `App.tsx`, `lib/api.ts`, the PRD page table, and the user guide together.
- Enum/DTO changes check `domain/enums.py`, Pydantic schemas, `lib/types.ts`, label maps, and parser tests.
- Transaction/queue/recovery changes trace the application entrypoint, durable row, broker call, worker claim, and recovery tests.
- Historical-value changes use a persisted fixture through migration, API serialization, and frontend rendering; a newly constructed current DTO alone does not prove compatibility.
- Module moves update this ownership table and package `AGENTS.md`, and remove references to the old path.
