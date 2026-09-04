# ProductFlow Roadmap

Directions that are not yet product fact, or that still lack real validation. Current capabilities live in [`PRD.en.md`](PRD.en.md), structure in [`ARCHITECTURE.en.md`](ARCHITECTURE.en.md), operations in [`USER_GUIDE.en.md`](USER_GUIDE.en.md).

Remaining audit work is the board at [`audits/tasks/README.md`](audits/tasks/README.md): claim one open task before coding. The ledgers keep contracts and evidence.

## Near term

### Studio increments

Do not write these into CONTEXT / PRD / ARCHITECTURE until they land.

| Item | User-visible | Out of scope |
|---|---|---|
| Shot list as default surface | Workbench opens on shot rows (type, count, status, thumbnail, run this shot); same schema-v3 canvas remains available | A Shot table or a second executor |
| Generate-set copy | One merchant action runs the existing DAG (content nodes first, then each shot); progress projected per shot | A second run model |
| Post-generation local edit | Inpaint / replace text / local redraw on an existing `ProductImageAsset`; new asset keeps lineage; failure does not replace the node's current result | A seventh node type, watermark-removal shelf, batch 200 |

Built-in DeliverySpec templates already live in ARCHITECTURE §7. The recipe library lists only recipes the user saved from a live graph.

### Agent conversation chrome and conversation path

- Collapsed sidebar, mobile conversation sheet, clipped suggestion chips. Decision boundary: [`adr/0009-agent-canvas-sandbox.md`](adr/0009-agent-canvas-sandbox.md).
- Question-count and visual-quality samples for conversation create.
- A separate browser regression for the conversation path. `just web-e2e-live-graph` covers skip-Agent direct create and workbench continuous actions.

### Agent durability

Lease, journal, effect reconciliation, SSE gap repair, and capacity-gate evidence live in [`audits/agent-production-readiness.md`](audits/agent-production-readiness.md). S1–S6 and G-01–G-05, G-07 are closed. Runtime-ownership S0–S6 is closed; evidence is in [`audits/agent-runtime-ownership.md`](audits/agent-runtime-ownership.md).

Remaining: G-06 behavior gates are the `run_id` recorded in [`audits/tasks/eval-skills.md`](audits/tasks/eval-skills.md) and [`audits/tasks/eval-live-layers.md`](audits/tasks/eval-live-layers.md). Until those pass, do not expand default capability, and do not treat Pi session files as durable proof. Background model calls stay unsupported per production D-03. The old Go Agent stays on `exp`. Session, Task, and WorkflowRun stay separate (`CONTEXT.md`).

### Agent evaluation system

Open guides (read one): [`audits/tasks/eval-skills.md`](audits/tasks/eval-skills.md), [`audits/tasks/eval-user-sim.md`](audits/tasks/eval-user-sim.md), [`audits/tasks/eval-labels.md`](audits/tasks/eval-labels.md), [`audits/tasks/eval-go-loader.md`](audits/tasks/eval-go-loader.md), [`audits/tasks/eval-live-layers.md`](audits/tasks/eval-live-layers.md). Frozen decisions and historical `run_id`: [`audits/agent-eval-system.md`](audits/agent-eval-system.md). Do not record pass^k as product fact without a ledger `run_id`.

### Agent Self-Harness

Start here: [`audits/tasks/harness-artifact.md`](audits/tasks/harness-artifact.md). Full program contract: [`audits/agent-self-harness.md`](audits/agent-self-harness.md). Evolution must not edit graders to inflate scores.

### Canvas document-authority tests

C0–C3, C5, and C6 are landed. Remaining: [`audits/tasks/canvas-inspector-midrun.md`](audits/tasks/canvas-inspector-midrun.md).

### Runtime performance governance

Open guides: [`audits/tasks/perf-imagesession-detail.md`](audits/tasks/perf-imagesession-detail.md), [`audits/tasks/perf-dispatcher-latency.md`](audits/tasks/perf-dispatcher-latency.md), [`audits/tasks/perf-capacity-metrics.md`](audits/tasks/perf-capacity-metrics.md). Baseline and lock order: [`audits/performance-governance.md`](audits/performance-governance.md). SaaS tenant admission is a new baseline.

### Image production quality

- Real-provider contracts for size, format, and advanced fields.
- Internal Taobao listing eval: [`audits/tasks/image-eval-pool.md`](audits/tasks/image-eval-pool.md).
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
