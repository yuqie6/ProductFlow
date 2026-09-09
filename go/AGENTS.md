# Go Backend Guidelines

Default runtime: `just go-api`, `just go-worker`, `just go-dispatcher`. Schema authority is `productflow-migrate`: GORM `CreateTable`/`AddColumn` plus ExtraDDL (`just go-migrate`). AutoMigrate is not used. Command transactions use `tx.WithGorm` and schema models (`Create` / `Updates` / `Take` plus `platform/db` locking clauses). Partial updates use `map[string]any` or `Select`, never a zero-value struct. Do not add `pfdb.Exec`/`Query`/`QueryRow` on command paths. PostgreSQL pool remains for health checks and recovery entrypoints. Do not use GORM associations to replace existing delete paths.

## Layout

Vertical slices under `internal/`: `auth`, `settings`, `product`, `graph`, `library`, `recipe`, `imagesession`, `delivery`, `layout`, `localedit`, `agent`, `providers`, `imageeval`. Shared primitives live in `internal/platform/` (`apperr`, `httpx`, `queue`, `db`, `db/schema`, `config`, `tx`, `storage`, `canonjson`, `log`). Fixed model prompt copy lives in `prompts/` (markdown, `go:embed`); graph, providers, and agent load it. Agent-service packs `prompts/agent/runtime-policy.md` at generate/build time. Do not put Skill bodies, JSON schemas, or seed-assembly branches in `prompts/`.

`graph` must not import `product`, `recipe`, or `delivery` (use `graph.DeliveryQueuer`). Agent must not write graph tables directly; call `graph` package functions.

Terminal logs are readable lines (time, level, process, message, `key=value`). JSON logs rotate under `STORAGE_ROOT/logs` (`productflow-api.log`, `productflow-worker.log`, `productflow-dispatcher.log`) and keep caller/stack for troubleshooting. Override the directory with `LOG_DIR`. `LOG_FORMAT=json` writes JSON to stderr as well. Idle dispatcher cycles, `/healthz`, and Agent heartbeats are Debug (files only unless `LOG_LEVEL=DEBUG`). Tests: `go/internal/platform/log`, `go/internal/platform/config`.

## Contracts

- Cookie name `session`. Errors `{"detail":"..."}`.
- JSON extra=forbid via `DisallowUnknownFields` → 400 `请求体无效`.
- Write business rows and River jobs in the same explicit GORM transaction using `StageTaskForActor` / `StageTask`. River owns job delivery, snooze and infrastructure retry. The dispatcher process only runs business recovery. Preserve execution identity on recovery; rotate it on explicit business retry.
- Unprovable provider results stay `unknown` and are not auto-retried as failure.

## Tests

For a local change, run the affected package and the regression nearest the changed behavior, with dev environment variables loaded. For shared contracts, persistence or schema, include affected readers and writers; schema changes need a focused migrate regression. Use `just go-test` for changes across shared platform behavior or the full backend release gate. Documentation-only changes follow the root documentation checks.

Full suite with an explicitly fresh run when required by the task or changed environment:

```bash
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./... -count=1 -p 1'
```

Packages that touch PostgreSQL skip without `DATABASE_URL`. `testdb.Pool` connects to `<dbname>_gotest_<package>` (created and migrated on first use), not the live just-dev database. `just go-test` still runs `go test -p 1` so packages do not race `CREATE DATABASE` / first migrate. Test harnesses that hit admin routes must set `AdminAccessRequired: true` and overlay `app_settings.admin_access_required=true`.

A skipped database test is not persistence evidence. Reuse valid results when relevant code and environment are unchanged; opt-in capacity and real-provider gates apply when needed for the task's claim. Coordinate concurrent tests of the same package database.
