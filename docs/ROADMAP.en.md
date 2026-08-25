# ProductFlow Roadmap

Directions that are not yet product fact, or that still lack real validation. Current capabilities live in [`PRD.en.md`](PRD.en.md), structure in [`ARCHITECTURE.en.md`](ARCHITECTURE.en.md), operations in [`USER_GUIDE.en.md`](USER_GUIDE.en.md).

## Near term

### Workbench proof

Leaving the Agent must still leave a complete canvas. Interaction follows [`specs/workbench.md`](specs/workbench.md): commands on the object, failures on the object, results immediately operable. This bar is not deferred for Agent-sandbox slices.

Workbench code already follows [`USER_GUIDE.en.md`](USER_GUIDE.en.md) and that spec. Still missing: continuous-action evidence on 1440 / 1024 / 390 with a real provider and PostgreSQL / Redis / worker — copy/paste, illegal-connect reasons, failures on the object, recipe preview confirm, bottom drawer leaving canvas visible, no console/network errors.

After that proof, delete `specs/workbench.md` and keep user-facing sentences in the user guide.

### Agent canvas sandbox

Landed: Turn run identity, create as live graph, canvas vs global session ownership, `sameRuntimeScope` ignoring prompt / live-graph refreshes, and no product-path WorkflowDraft writes (intake lives on Product). Current contract: CONTEXT / PRD / ARCHITECTURE. Remaining: the Goal loop in [`adr/0009-agent-canvas-sandbox.md`](adr/0009-agent-canvas-sandbox.md) and [`specs/agent-canvas-sandbox.md`](specs/agent-canvas-sandbox.md).

The Goal loop (run graph → inspect → edit canvas → run again) is unbuilt. Agent conversation chrome (collapsed sidebar, mobile sheet, clipped chips) is out of this item. Canvas continuous actions still follow the workbench-proof bar above.

Question-count and visual-quality samples for conversation create are still missing. `just web-e2e-live-graph` still covers skip-Agent direct create until the conversation path has its own browser regression.

Pi boundary: [`adr/0007-pi-agent-runtime-boundary.md`](adr/0007-pi-agent-runtime-boundary.md) and [`specs/pi-agent-runtime-integration.md`](specs/pi-agent-runtime-integration.md). Background durable Tasks and multi-instance reconciliation expand only after a dedicated gate. The old Go Agent on `exp` is not an implicit fallback on main.

### Studio increment

[`specs/productflow-studio-requirements.md`](specs/productflow-studio-requirements.md): shot list as the default surface, generate-set copy, recommended types on create, post-generation local edits, platform delivery presets, and a human fidelity checklist. Do not write these into CONTEXT / PRD / ARCHITECTURE until they land. The recipe library does not ship official canvas templates; users save their own recipes.

### Image production quality

- Real-provider contracts for size, format, and advanced fields.
- Samples for product fidelity, text accuracy, and visual consistency.
- Delivery specs, crop preview, and batch download.
- Failure, cancel, retry, and provider-note feedback.

### Library cutover

Online entry is in the PRD. Still missing deployment-grade backfill of the old `ImageGalleryEntry` table, the Gallery-only bridge rehearsal and approval, and higher-scope Agent writes. See [`rollout/media-library-transition.md`](rollout/media-library-transition.md).

### Agent durability

Real provider, real stores, SSE reconnect, and the production switch for background durable / reconciliation. See [`rollout/pi-agent-durability.md`](rollout/pi-agent-durability.md). Product boundary: [`specs/global-agent-human-workflow-design.md`](specs/global-agent-human-workflow-design.md). A business Task scheduler, cross-process durable admission, and a unified Fresh Observation harness are still unbuilt.

## Medium term

- Richer fact conflict resolution and structured spec entry.
- User-level visual-system save, compare, and reuse across products.
- Image quality scores, similar-candidate clustering, and selection help.
- More image provider adapters and observability.
- Workflow run cost, latency, and failure-rate stats.

## Engineering runtime after workbench proof

Do not start the following, or write them into CONTEXT / PRD / ARCHITECTURE, before the workbench browser proof.

### Move the Python business backend to Go

- Product contract: [`specs/go-backend-rewrite-prd.md`](specs/go-backend-rewrite-prd.md)
- Design: [`specs/go-backend-rewrite-design.md`](specs/go-backend-rewrite-design.md)
- Replace the business API, worker, and async dispatcher only. Web and the Node.js/Pi Agent keep their contracts.
- Preconditions: workbench proof, v2 leftover gone, HTTP / SSE / session / queue pack exported.
- The Go Agent on `exp` is not the starting point.

## SaaS

SaaS needs its own design for tenants, quotas and billing, object storage and retention, compatibility windows, audit, and SLOs. Those contracts start at the SaaS baseline and do not constrain the current live demo.
