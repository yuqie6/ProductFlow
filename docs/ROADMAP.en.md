# ProductFlow Roadmap

This document records only directions that remain unimplemented or lack real validation evidence. Current capabilities live in `PRD.en.md`, current code structure in `ARCHITECTURE.en.md`, and V1 cutover evidence in `rollout/workflow-v2-cutover.md`.

## Near-Term Priorities

### 0. Migrate the Agent Runtime to Pi

- `main` currently still uses the Go Agent service and the `agent-harness` snapshot. This remains the current-state description until migration is delivered.
- The target `main` runtime is a Pi SDK-based ProductFlow Agent adapter. ProductFlow keeps ownership of the FastAPI, Draft, confirmation, WorkflowRun, media, and Web projection contracts.
- The current self-built harness moves to the `exp` branch for durable Turns, background Tasks, crash recovery, effect reconciliation, and scheduling research. It is not an implicit runtime fallback on `main`.
- Both lines share Tool, Context, Draft, event, and quality fixtures while keeping runtime journals, session storage, and schedulers separate.
- Implementation rules and exit criteria live in `docs/adr/0007-pi-agent-runtime-boundary.md` and `docs/specs/pi-agent-runtime-integration.md`.
- Phases 0 through 4 focus on interactive Turns, bounded reads, Questions, Drafts, and pending WorkflowRun requests. Background Task and durable recovery support expands only after its dedicated gate passes.

### 1. Agent Creation Quality

- Build end-to-end regression cases with real products and real providers.
- Evaluate question count, fact accuracy, visual-system consistency, and per-image prompt quality.
- Improve confirmation density, conflict handling, and edit feedback.
- Validate Turn reconnect, restart, question answering, and materialization recovery.

### 2. Workbench Interaction

- Continue refining the existing node cards, inspector, command bar, and sidebar.
- Improve automatic layout, folder organization, cross-folder routing, and navigation for large DAGs.
- Verify node creation, edges, multi-select, shortcuts, and mobile touch in real browsers.
- Control workbench bundle size and initial load time.

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
