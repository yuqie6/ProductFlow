# ProductFlow Roadmap

## Current Stage: Personal Live Demo

The current release includes:

- Agent product creation, image-type quantities, and one to six real references.
- Durable Agent Turns, questions, confirmation, and streamed workflow materialization.
- A schema-v2 DAG with four node types, folders, node editing, edges, and run history.
- User-saved full workflow recipes and fragments.
- A canonical product image library with user folders, batch actions, and delivery renditions.
- Iterative image generation, candidate branches, Gallery, and save-to-product.
- Prompt, Agent, and image provider configuration.

Deployed-instance upgrades require backup/restore verification, canonical asset mapping, legacy archive reconciliation, and Agent rebuild gates. Engineering maintains one online V2 schema, API, and frontend flow; migration snapshots are bounded upgrade support and do not create a V1 dual executor.

## Near-Term Priorities

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

### 5. Development Experience

- Shorten local startup, migration, test, and real-provider validation paths.
- Keep configuration examples, README, Help, and Trellis specs aligned with current code.
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
