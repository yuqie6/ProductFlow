# ProductFlow Roadmap

This document records only directions that remain unimplemented or lack real validation evidence. Current capabilities live in `PRD.en.md`, current code structure in `ARCHITECTURE.en.md`, and V1 cutover evidence in `rollout/workflow-v2-cutover.md`. The online schema-v3 graph is already documented there; GraphProposal, recipe extract from a live graph, and Recipe ChangeSet are entered only from §2.

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
- Validate Turn reconnect, restart, question answering, and Draft-confirm graph persist recovery.

### 2. Remaining schema-v3 work

The online workflow authority is already `workflow_graphs`. Current implementation is in `ARCHITECTURE.en.md`. Still unimplemented:

- Agent GraphProposal / ChangeSet against a live graph. Target contract: `docs/adr/0008-free-canvas-agent-graph-authority.md`.
- Extracting and saving a recipe from a live v3 graph, and applying a Recipe ChangeSet directly onto another graph. Applying a saved recipe still produces a reviewable Draft.
- Remaining interaction items in `docs/rollout/free-canvas-v3-interaction-parity.md`.
- The full gate with a real provider, PostgreSQL/Redis/worker, desktop and 390px browsers, console/network errors, cancel/retry, and a retired-runtime residue scan.

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
