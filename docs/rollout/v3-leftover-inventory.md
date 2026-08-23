# Schema-v3 leftover inventory

Last reviewed against the working tree after empty-graph create, compact inspector drawer, and illegal-connect canvas notice landed.

This is remaining residue and timing, not a start signal for the Go rewrite. Slice G browser evidence is still required before declaring the workbench gate complete. See `docs/ROADMAP.md` §2 and `docs/specs/go-backend-rewrite-prd.md`.

## Online V2 graph

Deleted from the application layer. `20260821_0080` dropped `product_workflows` and related online DAG tables. There is no `v2_execution.py`, no `submit_v2_workflow_run`, and no online `schema_version = 2` writer.

Remaining reads of `product_workflows` live only in `application/legacy_retirement/` (V1 archive/cutover evidence). Alembic history still creates then drops those tables so empty databases can upgrade.

HTTP `/api/v2/...` for products, Drafts, Agent workspaces, and image library is API versioning, not an online V2 graph contract.

## Draft topology and plan keys

Still present, by design, until Intent+ChangeSet:

- `WorkflowNodeType.product_context` / `reference_image` and plan-key fields in `workflow_drafts/contracts.py`.
- Draft confirmation UI still projects those Draft node types.
- `FORBIDDEN_GRAPH_CONFIG_KEYS` rejects plan keys on live graph config.
- Recipe extract must not persist those keys onto live `config_json`.
- Prompt provider request objects may still carry `image_plan_keys` as Draft-time identifiers; they are not live graph topology.

Leftover deletion is **not** finished.

## Intent + ChangeSet

Not done. Agent create still confirms a complete WorkflowDraft topology, then `graph_draft_persist.py` adapts it into a v3 ChangeSet. ADR 0008 §11 (`WorkflowIntent` + ChangeSet without a second topology) and v1 archive → Intent rebuild remain later slices.

## Contract pack (Go P0)

Not exported. No OpenAPI / SSE / session / queue freeze artifact, and no `go/` host.

P0 waits. The empty-graph route `POST /api/v3/products/{product_id}/workflows` just landed; slice G browser evidence is not captured. Freezing HTTP now would snapshot a workbench contract that is still under the restoration gate.

Do not start P1 (Go host) or later. Do not treat `exp` Go Agent as a business-backend seed.

## Adjacent leftovers (out of this close-out)

- Pi `background_durable_tasks` remains `false`; v3 graph-run provider ledger / unknown reconciliation is still open (`docs/rollout/pi-agent-durability.md`).
- Media-library physical cleanup and V1 deployment cutover evidence remain operational gates.
- Agent create-quality samples are unimplemented.
