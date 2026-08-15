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

## 2. Backend Layers

`backend/src/productflow_backend/` keeps four boundaries:

- `presentation/`: FastAPI routes, request/response schemas, authentication, upload reads, and HTTP error mapping.
- `application/`: product, Agent conversation, WorkflowDraft, V2 workflow, image library, image session, settings, and asynchronous use cases.
- `domain/`: enums, business errors, and database-free DAG rules.
- `infrastructure/`: SQLAlchemy, provider clients, Redis/Dramatiq, storage, logging, and the Agent service client.

Routes do not own complex transactions or provider payload construction. The application layer owns business transactions, and infrastructure adapts external systems.

## 3. Frontend Structure

`web/src/App.tsx` registers the current pages:

- `/products`
- `/products/new`
- `/products/:productId`
- `/image-chat`
- `/gallery`
- `/settings`
- `/help`

Page code lives in `web/src/pages/`. Shared visual components live in `web/src/components/`. HTTP, DTOs, i18n, and browser preferences live in `web/src/lib/`.

The product workbench composes three existing component groups:

- `agent-workbench/`: conversation, SSE events, questions, Draft confirmation, and materialization reveal.
- `product-workflow-v2/`: V2 canvas, command bar, inspector, runs, recipes, and delivery renditions.
- `product-detail/`: reused canvas chrome, node cards, sidebar, shortcuts, and image Explorer.

TanStack Query owns server state. React state owns local forms, selection, and canvas interaction. `api.ts` is the browser HTTP boundary.

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

When reading product assets, the Agent first receives bounded metadata and then inspects selected images. Image tool results use a versioned multimodal contract; the full library and data URLs are not concatenated into text history.

Agent mutations such as rename, folder creation, and move use prepare/apply/reconcile contracts with idempotency keys so network interruption and restart can be reconciled.

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

WorkflowRun and WorkflowNodeRun store execution state. The worker schedules ready nodes after their upstream dependencies succeed. Prompt artifacts are versioned. Image results become ProductImageAsset records and bind back to target nodes.

WorkflowRecipe stores user-created full workflows and fragments. Recipe payloads store reusable structure and configuration, without product identity, generated results, or media bytes.

## 7. Image Model

`MediaObject` stores path, MIME type, byte size, dimensions, hash, and verification state. `ProductImageAsset` stores product-scoped display name, origin, folder, parent image, and image type.

Current origins:

- `upload`
- `workflow_generation`
- `image_session_attach`

The product library, node reference bindings, covers, and delivery renditions all use ProductImageAsset ids. Every image-session asset also has a MediaObject; saving it to a product creates a ProductImageAsset.

DeliveryRenditionJob reads a ProductImageAsset and asynchronously emits a crop, resize, and format-specific delivery file. It does not replace the source image.

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

## 11. Schema Evolution

SQLAlchemy metadata describes only current online models. Historical Alembic revisions remain so a fresh database can upgrade to head. Destructive schema cleanup lives in migrations. Runtime code does not read retired models or keep parallel routes and executors.

## 12. Quality Gates

- Backend: Ruff, full pytest, SQLite migration, and opt-in PostgreSQL/Redis live tests.
- Frontend: Vitest, ESLint, TypeScript, and Vite production build.
- Agent service: `go test ./...` and an opt-in live-provider transcript test.
- Cross-layer changes add real browser, database, or provider validation according to risk.
