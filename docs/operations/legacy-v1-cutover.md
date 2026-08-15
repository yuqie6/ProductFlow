# Legacy V1 Cutover Runbook

`20260816_0042` preserves retained V1 source tables, rows, media mappings, and archive tables. It creates the singleton `legacy_cutover_gates` row in `pending` phase. The revision does not call an Agent/provider, inspect storage, or delete data.

## Required Evidence

Before any separately approved destructive cleanup, operators must have:

- a source audit report hash and recognized source profile;
- an archive backfill/export reconciliation hash;
- a canonical asset mapping reconciliation hash;
- a successful database and storage restore verification timestamp;
- zero active or unknown V1 workflow/node/Canvas Agent execution rows.

Evidence is written through the application boundary so the singleton gate records one transactionally consistent approval. Report payloads and secrets are not copied into the gate.

## Commands

Inspect the durable gate:

```bash
uv run --directory backend python -m productflow_backend.commands.manage_legacy_cutover_gate status
```

Approve the gate after the external reports and restore rehearsal have passed:

```bash
uv run --directory backend python -m productflow_backend.commands.manage_legacy_cutover_gate approve \
  --source-profile current_with_agent_workspace_finalization_and_cutover_gate_20260816_0042 \
  --source-report-sha256 <source-report-sha256> \
  --archive-report-sha256 <archive-report-sha256> \
  --canonical-report-sha256 <canonical-report-sha256> \
  --backup-restore-verified-at 2026-08-16T12:00:00+00:00
```

A future cleanup entrypoint must call `assert_legacy_cutover_cleanup_ready` in its owning transaction before deleting any retained source/runtime object. While the gate is `pending`, evidence is incomplete, or a legacy execution row is active/unknown, the guard raises a conflict and cleanup must stop.

After a separately reviewed cleanup transaction completes, it may record `cleaned` with `mark_legacy_cutover_cleaned`. This marker does not itself perform cleanup and cannot be used to reopen the gate.

## Recovery Boundary

If audit, backup restore, canonical mapping, archive reconciliation, or Agent rebuild fails, leave the gate pending. Re-run the affected report/backfill using its existing hash and idempotency boundary. Do not reset the database, rewrite an archive snapshot, or re-enable the V1 executor to recover.
