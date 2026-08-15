# Workflow V2 Cutover Status

Last reviewed against `main` on 2026-08-16.

## Code Delivered

- Canonical MediaObject/ProductImageAsset model and product library.
- WorkflowDraft revisions, confirmation, atomic schema-v2 materialization, and reveal events.
- Versioned product facts, visual systems, prompt artifacts, image nodes, workflow runs, folders, recipes, and delivery renditions.
- Go Agent service integration, durable Turns, bounded ProductFlow tools, questions, replay, and artifact synchronization.
- `/products/new` Agent-first creation and the unified `/products/:productId` workbench.
- Immutable V1 workflow/template/Canvas Agent archives, paged export/backfill, read-only history UI, and Agent rebuild seed.
- V1 online editor, executor, mutation routes, built-in template catalog, and read-time default-DAG creation removed.
- Legacy cutover preflight, freeze state, and persistent evidence gate.

## Automated Evidence

Current local gate on 2026-08-16:

- Backend: 384 passed, 8 skipped.
- Frontend: 47 test files and 217 tests passed; ESLint, TypeScript, and Vite build passed.
- Agent service: `go test -count=1 ./...` passed.
- Ruff: passed.

These results validate the checkout. They do not substitute for deployment-specific production data and browser evidence.

## Deployment Evidence Still Required

- Run the source audit and cutover preflight against a restored production database/storage copy.
- Verify database and storage backup restoration and record the real verification timestamp.
- Enable and verify the V1 freeze for deployments that can still run a pre-cutover application version.
- Drain all active and unknown V1 workflow, node, and Canvas Agent runs.
- Export and backfill every workflow, user-template, and Canvas Agent archive page; repeat to prove idempotency.
- Reconcile source counts/hashes, archive hashes, canonical asset mappings, missing media, and provider configuration.
- Exercise archive browse/download/export and Agent rebuild in real browsers at the supported viewports.
- Exercise `/products/new`, Draft confirmation, materialization, workbench transition, V2 run, gallery, rendition, and Agent reconnect against real PostgreSQL/Redis and configured providers.
- Approve the `legacy_cutover_gates` singleton with the resulting hashes and restore timestamp.
- Obtain separate review before implementing or running any source-table/media destructive cleanup.

## Stop Conditions

- Unknown source profile or unrecognized persisted value.
- Unstable source/archive hash or count.
- Missing canonical mapping required by an archive.
- Active or unknown retained execution.
- Invalid provider binding required by V2 operation.
- Archive rebuild bypassing structured Draft confirmation.
- Backup or storage restore not actually verified.
- Any cleanup path that can run while the evidence gate is pending.

## Operator Procedure

Follow `docs/operations/legacy-v1-cutover.md`. Evidence contains private product data and must remain in a restricted, non-Git directory.
