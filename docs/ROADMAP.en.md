# ProductFlow Roadmap

Directions that are not yet product fact, or that still lack real validation. Current capabilities live in [`PRD.en.md`](PRD.en.md), structure in [`ARCHITECTURE.en.md`](ARCHITECTURE.en.md), operations in [`USER_GUIDE.en.md`](USER_GUIDE.en.md).

## Near term

### Workbench proof

Leaving the Agent must still leave a complete canvas. Interaction follows [`specs/workbench.md`](specs/workbench.md): commands on the object, failures on the object, results immediately operable. This bar is not deferred for Agent-sandbox slices.

Workbench code already follows [`USER_GUIDE.en.md`](USER_GUIDE.en.md) and that spec. Still missing: continuous-action evidence on 1440 / 1024 / 390 with a real provider and PostgreSQL / Redis / worker — copy/paste, illegal-connect reasons, failures on the object, recipe preview confirm, bottom drawer leaving canvas visible, no console/network errors.

After that proof, delete `specs/workbench.md` and keep user-facing sentences in the user guide.

### Agent canvas sandbox

Landed: Turn run identity, create as live graph, canvas vs global session ownership, `sameRuntimeScope` ignoring prompt / live-graph refreshes, no product-path WorkflowDraft writes (intake lives on Product), and the Goal loop on an explicit `AgentTask` (a finished graph run is not Goal complete). Current contract: CONTEXT / PRD / ARCHITECTURE. Conversation chrome remains in [`adr/0009-agent-canvas-sandbox.md`](adr/0009-agent-canvas-sandbox.md).

The Goal loop (run graph → inspect → edit canvas → run again) is on the product conversation sidebar. Complete is user-only; graph-checkable criteria are not automated. Agent conversation chrome (collapsed sidebar, mobile sheet, clipped chips) is out of this item. Canvas continuous actions still follow the workbench-proof bar above.

Question-count and visual-quality samples for conversation create are still missing. `just web-e2e-live-graph` still covers skip-Agent direct create until the conversation path has its own browser regression.

Pi boundary: [`adr/0007-pi-agent-runtime-boundary.md`](adr/0007-pi-agent-runtime-boundary.md) and [`specs/pi-agent-runtime-integration.md`](specs/pi-agent-runtime-integration.md). Background durable Tasks and multi-instance reconciliation expand only after a dedicated gate. The old Go Agent on `exp` is not an implicit fallback on main.

### Studio increment

[`specs/studio-increments.md`](specs/studio-increments.md): shot list as the default surface, generate-set copy, recommended types on create, post-generation local edits, and a human fidelity checklist. Do not write these into CONTEXT / PRD / ARCHITECTURE until they land. Built-in DeliverySpec templates already live in ARCHITECTURE §7. The recipe library does not ship official canvas templates; users save their own recipes.

### Image production quality

- Real-provider contracts for size, format, and advanced fields.
- Samples for product fidelity, text accuracy, and visual consistency.
- Delivery specs, crop preview, and batch download.
- Failure, cancel, retry, and provider-note feedback.

### Mainline cleanup

Delete leftover V1 archives, Gallery backfill, WorkflowDraft write stubs, and compatibility readers under [`adr/0010-mainline-no-compatibility.md`](adr/0010-mainline-no-compatibility.md). Do not add deployment-grade backfill or cutover evidence.

### Agent durability

Real provider, real stores, SSE reconnect, and the production switch for background durable / reconciliation. See [`rollout/pi-agent-durability.md`](rollout/pi-agent-durability.md). Session, Task, and WorkflowRun stay separate objects (`CONTEXT.md`). A business Task scheduler, cross-process durable admission, and a unified Fresh Observation harness are still unbuilt.

## Medium term

- Richer fact conflict resolution and structured spec entry.
- User-level visual-system save, compare, and reuse across products.
- Image quality scores, similar-candidate clustering, and selection help.
- More image provider adapters and observability.
- Workflow run cost, latency, and failure-rate stats.

## Engineering runtime: move the business backend to Go

Replace the business API, worker, and async dispatcher with vertical Go packages. Decision: [`adr/0011-go-vertical-slice-rewrite.md`](adr/0011-go-vertical-slice-rewrite.md). Do not write Gin / GORM / asynq into CONTEXT / PRD / ARCHITECTURE as current fact before cutover.

- Product contract: [`specs/go-backend-rewrite-prd.md`](specs/go-backend-rewrite-prd.md)
- Design: [`specs/go-backend-rewrite-design.md`](specs/go-backend-rewrite-design.md)
- Replace the business API, worker, and async dispatcher only. Web and the Node.js/Pi Agent keep their contracts.
- The freeze line is live Python user-visible behavior plus the HTTP / SSE / queue pack. Product WorkflowDraft must not return.
- Workbench browser proof runs in parallel and remains the cutover and product-quality gate. It does not enter the workbench spec's implementation.
- The Go Agent on `exp` is not the starting point.

## SaaS

SaaS needs its own design for tenants, quotas and billing, object storage and retention, compatibility windows, audit, and SLOs. Those contracts start at the SaaS baseline and do not constrain the current live demo.
