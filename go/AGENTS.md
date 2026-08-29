# Go Backend Guidelines

Default runtime: `just go-api`, `just go-worker`, `just go-dispatcher`. Schema authority is `productflow-migrate`: GORM `CreateTable`/`AddColumn` plus ExtraDDL (`just go-migrate`). AutoMigrate is not used. Command transactions use `tx.WithGorm` and `*gorm.DB`; raw SQL goes through `platform/db` Query/Exec. PostgreSQL pool remains for health checks, recovery entrypoints, and `FOR UPDATE` / advisory locks. Do not use GORM associations to replace existing delete paths.

## Layout

Vertical slices under `internal/`: `auth`, `settings`, `product`, `graph`, `library`, `recipe`, `imagesession`, `delivery`, `localedit`, `agent`, `providers`. Shared primitives live in `internal/platform/` (`apperr`, `httpx`, `queue`, `db`, `db/schema`, `config`, `tx`, `storage`, `canonjson`, `log`).

`graph` must not import `product`, `recipe`, or `delivery` (use `graph.DeliveryQueuer`). Agent must not write graph tables directly; call `graph` package functions.

JSON logs go to stderr and rotating files under `STORAGE_ROOT/logs` (`productflow-api.log`, `productflow-worker.log`, `productflow-dispatcher.log`). Override with `LOG_DIR`. Tests: `go/internal/platform/log`, `go/internal/platform/config`.

## Contracts

- Cookie name `session`. Errors `{"detail":"..."}`. HTTP 不 enqueue broker；dispatcher 标 SENT 后再入队。
- JSON extra=forbid via `DisallowUnknownFields` → 400 `请求体无效`.
- HTTP does not enqueue the broker. Write the business row and `async_dispatches` PENDING; dispatcher marks SENT then enqueues. Worker `MaxRetry=0`.
- Unprovable provider results stay `unknown` and are not auto-retried as failure.

## Tests

```bash
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./... -count=1'
```

Packages that touch PostgreSQL skip without `DATABASE_URL`. Test harnesses that hit admin routes must set `AdminAccessRequired: true` and overlay `app_settings.admin_access_required=true`.
