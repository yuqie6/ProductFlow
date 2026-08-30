# ProductFlow Roadmap

Directions that are not yet product fact, or that still lack real validation. Current capabilities live in [`PRD.en.md`](PRD.en.md), structure in [`ARCHITECTURE.en.md`](ARCHITECTURE.en.md), operations in [`USER_GUIDE.en.md`](USER_GUIDE.en.md).

## Near term

### Studio increments

Do not write these into CONTEXT / PRD / ARCHITECTURE until they land.

| Item | User-visible | Out of scope |
|---|---|---|
| Shot list as default surface | Workbench opens on shot rows (type, count, status, thumbnail, run this shot); same schema-v3 canvas remains available | A Shot table or a second executor |
| Generate-set copy | One merchant action runs the existing DAG (content nodes first, then each shot); progress projected per shot | A second run model |
| Recommended types on create | One click fills missing image types; a second click does not overwrite existing shot config | Official canvas-template recipes |
| Post-generation local edit | Inpaint / replace text / local redraw on an existing `ProductImageAsset`; new asset keeps lineage; failure does not replace the node's current result | A seventh node type, watermark-removal shelf, batch 200 |
| Human fidelity checklist | People check form, color, and text on the result; then local-edit or rerun that shot | Auto quality scores as a formal gate |

Built-in DeliverySpec templates already live in ARCHITECTURE §7. The recipe library lists only recipes the user saved from a live graph.

### Agent conversation chrome and conversation path

- Collapsed sidebar, mobile conversation sheet, clipped suggestion chips. Decision boundary: [`adr/0009-agent-canvas-sandbox.md`](adr/0009-agent-canvas-sandbox.md).
- Question-count and visual-quality samples for conversation create.
- A separate browser regression for the conversation path. `just web-e2e-live-graph` covers skip-Agent direct create and workbench continuous actions.

### Agent durability

Until these gates pass, do not expand default capability, and do not treat Pi session files as durable proof. Boundary: [`adr/0007-pi-agent-runtime-boundary.md`](adr/0007-pi-agent-runtime-boundary.md). The old Go Agent on `exp` is not an implicit fallback on main.

- Real provider, PostgreSQL / Redis, and browser.
- A production claim for SSE reconnect.
- Background durable Tasks, cross-process claim, and full effect reconciliation.
- An independent Fresh Observation harness (re-read before/after side effects is already a rule).

Session, Task, and WorkflowRun stay separate objects (`CONTEXT.md`).

### Image production quality

- Real-provider contracts for size, format, and advanced fields.
- Samples for product fidelity, text accuracy, and visual consistency.
- Delivery specs, crop preview, and batch download.
- Failure, cancel, retry, and provider-note feedback.

## Medium term

- Richer fact conflict resolution and structured spec entry.
- User-level visual-system save, compare, and reuse across products.
- Image quality scores, similar-candidate clustering, and selection help.
- More image provider adapters and observability.
- Workflow run cost, latency, and failure-rate stats.

## SaaS

SaaS needs its own design for tenants, quotas and billing, object storage and retention, compatibility windows, audit, and SLOs. Those contracts start at the SaaS baseline and do not constrain the current live demo.
