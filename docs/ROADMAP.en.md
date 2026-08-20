# ProductFlow Roadmap

This document records only directions that remain unimplemented or lack real validation evidence. Current capabilities live in `PRD.en.md`, current code structure in `ARCHITECTURE.en.md`, and V1 cutover evidence in `rollout/workflow-v2-cutover.md`.

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

- The target contract lives in `docs/adr/0008-free-canvas-agent-graph-authority.md`. The current governance checkout is at the `e6cf9e66` schema-v2 baseline and contains no schema-v3 graph, run, API, workbench, or migration implementation.
- Validate the governance baseline by assigning one online owner to Product, WorkflowDraft, ProductWorkflow, WorkflowRun, Agent Session/Task/Conversation, MediaLibraryAsset, and ProductImageAsset, then produce repeatable full-test, live-service, and browser evidence for e6.
- Build one application service from a confirmed WorkflowDraft to the initial v3 graph, followed by the Node Catalog, typed edges, Graph Context Compiler, ChangeSets, graph revisions, operation groups, GraphProposals, and revision-snapshot runs.
- Reuse the mature v1/v2 canvas shell, node presentation, inspector, run sidebar, and asset-selection experience while replacing their data contracts with v3 graph/revision contracts. Do not reintroduce old DTOs, queries, mutations, plan keys, or hidden reference merging.
- Complete the `MediaLibraryAsset -> ProductImageAsset -> WorkflowMediaLibraryAsset -> image_asset node -> reference edge` flow, including drop, bind, rebind, unused state, multiple consumers, and aggregate reference inputs.
- Route Agent proposals, direct user edits, and recipes through one Graph Command Service. Route Agent run requests, page controls, and workers through one v3 WorkflowRun contract.
- Retire v2 routes, schemas, application services, pages, hooks, API clients, types, and tests only after the v3 user journey passes its complete gate. Each deletion slice must preserve V1 archives, the Gallery bridge, and historical canvas readers.
- The current migration head is `20260820_0070`. A restarted v3 implementation must create a new migration chain from the current schema; the discarded `0071-0074` migrations do not count as current implementation or acceptance evidence.
- The final gate covers a real provider, PostgreSQL/Redis/worker, Agent ChangeSets, concurrency conflicts, desktop and 390px browsers, console/network errors, context recompilation after edge changes, run history, cancellation, retry, and retired-runtime residue scans.

### 3. Image Production Quality

- Add real-provider contracts for dimensions, formats, and advanced fields.
- Build evaluation samples for product-form fidelity, text accuracy, and visual consistency.
- Improve delivery specifications, crop preview, and batch download.
- Refine failure, cancel, retry, and provider-note feedback.

### 4. Library and Recipes

- Improve large-library search, directory counts, batch move, and selection feedback.
- Strengthen reconciliation and explainable results for Agent library-organization tools.
- Improve recipe previews, version notes, and pre-application difference review.
- Keep recipes explicitly user-saved.

### 5. Global Agent and Human Workflow

- Keep direct workflow editing, whole-workflow and single-node runs, cancellation, retry, and run history available without an Agent Session.
- Keep Session and Task scope separate from the current route; page context assists interpretation and does not rewrite a Task goal.
- Expose ProductFlow capabilities to Pi through versioned Skills, bounded per-turn Context, and controlled ProductFlow Tools.
- Require backend scope, revision, idempotency, validation, and user confirmation for every business side effect.
- Keep the current global Agent, Session, Task, and human-workflow design in `docs/specs/global-agent-human-workflow-design.md`; keep Pi runtime rules in `docs/specs/pi-agent-runtime-integration.md`.

### 6. Development Experience

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

## SaaS Stage

SaaS requires separate design for:

- tenants, workspaces, and member roles;
- quotas, billing, cost attribution, and abuse controls;
- object storage, backup, recovery, and retention;
- schema/API compatibility windows and migration commitments;
- audit logs, compliance, privacy, and formal SLOs.

Those contracts begin with the SaaS baseline and do not constrain the current live demo.
