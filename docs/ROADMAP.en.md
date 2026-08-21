# ProductFlow Roadmap

This document records only directions that remain unimplemented or lack real validation evidence. Current capabilities live in `PRD.en.md`, current code structure in `ARCHITECTURE.en.md`, and V1 cutover evidence in `rollout/workflow-v2-cutover.md`. Schema-v3 and free-canvas target contracts are entered only from §2; do not write them into the current-implementation sections of `CONTEXT.md`, `PRD.en.md`, or `ARCHITECTURE.en.md`.

## Near-Term Priorities

### 0. Migrate the Agent Runtime to Pi (main switched; acceptance evidence pending)

- `main` now uses the Node.js 22 + Pi SDK ProductFlow Agent adapter. ProductFlow keeps ownership of the FastAPI, Draft, confirmation, WorkflowRun, media, and Web projection contracts.
- The old Go Agent service and `agent-harness` are retained on `exp` for durable Turns, background Tasks, crash recovery, effect reconciliation, and scheduling research. They are not an implicit runtime fallback on `main`.
- Both lines share Tool, Context, Draft, event, and quality fixtures while keeping runtime journals, session storage, and schedulers separate.
- Implementation rules and exit criteria live in `docs/adr/0007-pi-agent-runtime-boundary.md` and `docs/specs/pi-agent-runtime-integration.md`.
- Remaining validation: real provider, PostgreSQL/Redis, browser, SSE reconnect, and background durable/reconciliation gates.
- Phases 0 through 4 focus on interactive Turns, bounded reads, Questions, Drafts, and pending WorkflowRun requests. Background Task and durable recovery support expands only after its dedicated gate passes.

### 1. Agent Creation Quality

- Build end-to-end regression cases with real products and real providers.
- Evaluate question count, fact accuracy, visual-system consistency, and per-image prompt quality.
- Improve confirmation density, conflict handling, and edit feedback.
- Validate Turn reconnect, restart, question answering, and materialization recovery.

### 2. Schema-v3 Free Canvas and Agent Graph Collaboration

- The target contract lives in `docs/adr/0008-free-canvas-agent-graph-authority.md`. The first implementation slice is specified in `docs/specs/schema-v3-admission-slice.md` (Draft; no code until approved). The current governance checkout is the schema-v2 baseline and contains no schema-v3 graph, run, API, workbench, or migration implementation.
- Validate the governance baseline by assigning one online owner to Product, WorkflowDraft, ProductWorkflow, WorkflowRun, Agent Session/Task/Conversation, MediaLibraryAsset, and ProductImageAsset, then produce repeatable full-test, live-service, and browser evidence on the current code.
- The person at the workbench is the primary operator; the Agent is optional help. Keep the intake form (image types, quantities, references) and offer two entries onto the same v3 graph: direct create (preset template ChangeSet; prompts and style come from running nodes) and Agent create.
- Build one application service from a confirmed WorkflowDraft to the initial v3 graph (Agent entry only), followed by the Node Catalog, typed edges, Graph Context Compiler, ChangeSets, graph revisions, operation groups, and revision-snapshot runs. GraphProposal and Recipe ChangeSets are out of the first slice.
- Reuse the mature v1/v2 canvas shell, node presentation, inspector, run sidebar, and asset-selection experience while replacing their data contracts with v3 graph/revision contracts. Do not reintroduce old DTOs, queries, mutations, plan keys, or hidden reference merging.
- Complete the `MediaLibraryAsset -> ProductImageAsset -> WorkflowMediaLibraryAsset -> image_asset node -> reference edge` flow, including drop, bind, rebind, unused state, multiple consumers, and aggregate reference inputs.
- Route user canvas edits through one Graph Command Service in the first slice. Recipes still create a WorkflowDraft and then the initial-graph adapter. Agent live-graph GraphProposals come after the first slice. Route Agent run requests, page controls, and workers through one v3 WorkflowRun contract.
- Retire v2 routes, schemas, application services, pages, hooks, API clients, types, and tests only after the v3 user journey passes its complete gate. Each deletion slice must preserve V1 archives, the Gallery bridge, and historical canvas readers.
- The current migration head is `20260820_0070`. A restarted v3 implementation must create a new migration chain from the current schema; the discarded `0071-0074` migrations do not count as current implementation or acceptance evidence.
- The final gate covers a real provider, PostgreSQL/Redis/worker, Agent ChangeSets, concurrency conflicts, desktop and 390px browsers, console/network errors, context recompilation after edge changes, run history, cancellation, retry, and retired-runtime residue scans.

### 3. Image Production Quality

- Add real-provider contracts for dimensions, formats, and advanced fields.
- Build evaluation samples for product-form fidelity, text accuracy, and visual consistency.
- Improve delivery specifications, crop preview, and batch download.
- Refine failure, cancel, retry, and provider-note feedback.

### 4. Global Library and Workflow Sub-library

Current online entry, organization, and organization Drafts live in `PRD.en.md`. Remaining:

- Deployed backfill, reference audit, observation window, and physical-cleanup eligibility for the retained current-schema `ImageGalleryEntry` table.
- Live deployment rehearsal, backup/restore evidence, and approval for the `legacy_canvas_agent_20260518_0032` Gallery-only bridge. Agent archives are outside that bridge.
- Higher-scope Agent writes such as cross-product organization.
- v3 frontend adaptation and browser acceptance for sub-library association, image-node binding, and `reference` edges.

### 5. Global Agent and Human Workflow

Current Session, Task, Dock, and pending WorkflowRun requests live in `PRD.en.md` and `ARCHITECTURE.en.md`. Remaining:

- A ProductFlow business-level Task scheduler.
- Cross-process durable admission; `AGENT_MAX_CONCURRENT_TURNS` only caps the current Agent process.
- A unified pre-effect Fresh Observation harness.
- More side-effect operations and richer jumps to affected objects.

The product boundary lives in `specs/global-agent-human-workflow-design.md`; Pi runtime rules live in `specs/pi-agent-runtime-integration.md`.

### 6. Recipes

- Improve recipe previews, version notes, and pre-application difference review.
- Keep recipes explicitly user-saved.

### 7. Development Experience

- Shorten local startup, migration, test, and real-provider validation paths.
- Keep configuration examples, README, the user guide, CONTEXT, ADRs, ARCHITECTURE, and package AGENTS aligned with current code.
- Add cross-layer contract tests that reduce DTO, route, and provider drift.
- Keep parallel compatibility models out of runtime code.

## Mid-Term Direction

- Richer product-fact conflict resolution and structured specification entry.
- User-level visual-system save, version comparison, and cross-product reuse.
- Image quality scoring, similar-candidate clustering, and human selection assistance.
- More image-provider adapters and observability.
- Workflow cost, latency, and failure-rate reporting.

## Engineering runtime after schema-v3

Do not start the items below before the schema-v3 governance baseline is done. They do not change the current FastAPI / Dramatiq / Alembic runtime, and they must not be written into the current-fact sections of `CONTEXT.md`, `PRD.en.md`, or `ARCHITECTURE.en.md`. Schema-v3 remains the main line.

### Move the business backend from Python to Go

- Product contract: `docs/specs/go-backend-rewrite-prd.md` (Draft)
- Implementation design and slices: `docs/specs/go-backend-rewrite-design.md` (Draft)
- Replace only the business API, worker, and async dispatcher. Keep the current Web and Node.js/Pi Agent service contracts.
- PostgreSQL remains authoritative for products, Drafts, graphs, runs, media, and Agent projections. Redis/asynq is recoverable delivery only.
- Default stack: Gin, GORM, Viper, zap, go-redis, and asynq. Internals are vertical capability modules. GORM must not AutoMigrate and must not replace `FOR UPDATE`, advisory locks, or `async_dispatches`.
- Start only after the online v3 graph is stable, v2 leftovers are deleted, and the HTTP/SSE/session/queue contract pack is exported.
- The Go Agent service on `exp` is not the starting point for this project.

## SaaS Stage

SaaS requires separate design for:

- tenants, workspaces, and member roles;
- quotas, billing, cost attribution, and abuse controls;
- object storage, backup, recovery, and retention;
- schema/API compatibility windows and migration commitments;
- audit logs, compliance, privacy, and formal SLOs.

Those contracts begin with the SaaS baseline and do not constrain the current live demo.
