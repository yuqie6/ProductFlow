# Go Backend Guidelines

Default runtime: `just go-api`, `just go-worker`, `just go-dispatcher`. Schema authority is `productflow-migrate`: GORM `CreateTable`/`AddColumn` plus ExtraDDL (`just go-migrate`). AutoMigrate is not used. Command transactions use `tx.WithGorm` and schema models (`Create` / `Updates` / `Take` plus `platform/db` locking clauses). Partial updates use `map[string]any` or `Select`, never a zero-value struct. Do not add `pfdb.Exec`/`Query`/`QueryRow` on command paths. PostgreSQL pool remains for health checks and recovery entrypoints. Do not use GORM associations to replace existing delete paths. See `docs/adr/0012-gorm-command-writes.md`.

## Layout

Vertical slices under `internal/`: `auth`, `settings`, `product`, `graph`, `library`, `recipe`, `imagesession`, `delivery`, `localedit`, `agent`, `providers`. Shared primitives live in `internal/platform/` (`apperr`, `httpx`, `queue`, `db`, `db/schema`, `config`, `tx`, `storage`, `canonjson`, `log`).

`graph` must not import `product`, `recipe`, or `delivery` (use `graph.DeliveryQueuer`). Agent must not write graph tables directly; call `graph` package functions.

Terminal logs are readable lines (time, level, process, message, `key=value`). JSON logs rotate under `STORAGE_ROOT/logs` (`productflow-api.log`, `productflow-worker.log`, `productflow-dispatcher.log`) and keep caller/stack for troubleshooting. Override the directory with `LOG_DIR`. `LOG_FORMAT=json` writes JSON to stderr as well. Idle dispatcher cycles, `/healthz`, and Agent heartbeats are Debug (files only unless `LOG_LEVEL=DEBUG`). Tests: `go/internal/platform/log`, `go/internal/platform/config`.

## Contracts

- Cookie name `session`. Errors `{"detail":"..."}`. HTTP 不 enqueue broker；dispatcher 标 SENT 后再入队。
- JSON extra=forbid via `DisallowUnknownFields` → 400 `请求体无效`.
- HTTP does not enqueue the broker. Write the business row and `async_dispatches` PENDING; dispatcher marks SENT then enqueues. Worker `MaxRetry=0`.
- Unprovable provider results stay `unknown` and are not auto-retried as failure.

## Tests

```bash
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./... -count=1 -p 1'
```

Packages that touch PostgreSQL skip without `DATABASE_URL`. `testdb.Pool` connects to `<dbname>_gotest_<package>` (created and migrated on first use), not the live just-dev database. `just go-test` still runs `go test -p 1` so packages do not race `CREATE DATABASE` / first migrate. Test harnesses that hit admin routes must set `AdminAccessRequired: true` and overlay `app_settings.admin_access_required=true`.
