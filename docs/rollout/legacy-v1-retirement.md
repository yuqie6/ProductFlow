# Legacy V1 Retirement Status

Last reviewed against the working tree on 2026-08-23.

This document tracks deployment evidence for retiring the V1 workflow surface. The online workflow authority is now schema-v3 `workflow_graphs`; this rollout does not describe the current graph contract.

## Code Checkpoint

- Canonical `MediaObject` / `ProductImageAsset` identity and the product library are online.
- WorkflowDraft confirmation and direct create persist schema-v3 graphs.
- Versioned product facts, visual systems, prompt artifacts, graph runs, folders, recipes, and delivery renditions are online.
- ProductFlow uses the Node.js/Pi Agent adapter with bounded tools, questions, SSE replay, and artifact synchronization.
- `/products/new` is the creation entry and `/products/:productId` is the unified Agent, graph, and image-library workbench.
- Immutable V1 workflow, template, and Canvas Agent archives remain available through paged export/backfill and the read-only history UI.
- The V1 editor, executor, mutation routes, built-in template catalog, and read-time default-DAG creation have been removed.
- The repository retains cutover preflight, freeze state, and the persistent evidence gate.

These source-code facts do not prove that any deployed database and storage set has completed retirement.

## Historical Checkout Evidence

The 2026-08-16 checkout passed 384 backend tests with 8 skipped, 217 frontend tests in 47 files, frontend lint/type/build, the then-current Agent service gate, and Ruff. Those results only describe that checkout and are not a current CI claim or deployment approval.

## Deployment Evidence Still Required

- Run source audit and cutover preflight against a restored production database and storage copy.
- Verify database and storage backup restoration and record the real verification timestamp.
- Enable and verify the V1 freeze for deployments that can still run a pre-retirement application version.
- Drain all active and unknown V1 workflow, node, and Canvas Agent runs.
- Export and backfill every workflow, user-template, and Canvas Agent archive page; repeat to prove idempotency.
- Reconcile source counts and hashes, archive hashes, canonical asset mappings, missing media, and provider configuration.
- Exercise archive browse, download, export, and Agent rebuild in supported browsers.
- Exercise current product creation, Draft confirmation, schema-v3 graph persist and run, media-library linkage, rendition, and Agent reconnect against real PostgreSQL, Redis, workers, and configured providers.
- Approve the `legacy_cutover_gates` singleton with the resulting hashes and restore timestamp.
- Obtain separate review before implementing or running source-table or media cleanup.

## Stop Conditions

- Unknown source profile or unrecognized persisted value.
- Unstable source or archive hash/count.
- Missing canonical mapping required by an archive.
- Active or unknown retained execution.
- Invalid provider binding required by archive rebuild or current operation.
- Archive rebuild bypassing the current structured confirmation contract.
- Backup or storage restore not actually verified.
- Any cleanup path that can run while the evidence gate is pending.

## Operator Procedure

Follow `docs/operations/legacy-v1-cutover.md`. Evidence may contain private product data and must remain in a restricted, non-Git directory.
