# Go Backend Guidelines

Default runtime: `just go-api`, `just go-worker`, `just go-dispatcher`. Schema authority remains Alembic in `backend/`. Do not add GORM or AutoMigrate.

## Layout

Vertical slices under `internal/`: `auth`, `settings`, `product`, `graph`, `library`, `recipe`, `imagesession`, `delivery`, `localedit`, `agent`, `providers`. Shared primitives live in `internal/platform/` (`apperr`, `httpx`, `queue`, `db`, `config`, `tx`, `storage`, `canonjson`).

`graph` must not import `product`, `recipe`, or `delivery` (use `graph.DeliveryQueuer`). Agent must not write graph tables directly; call `graph` package functions.

## Contracts

- Cookie name `session`. Errors `{"detail":"..."}`. Queue unavailable is 503 `任务队列暂不可用，请稍后重试`.
- JSON extra=forbid via `DisallowUnknownFields` → 400 `请求体无效`.
- HTTP does not enqueue the broker. Write the business row and `async_dispatches` PENDING; dispatcher marks SENT then enqueues. Worker `MaxRetry=0`.
- Unprovable provider results stay `unknown` and are not auto-retried as failure.

## Tests

```bash
bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./... -count=1'
```

Packages that touch PostgreSQL skip without `DATABASE_URL`. Test harnesses that hit admin routes must set `AdminAccessRequired: true` and overlay `app_settings.admin_access_required=true`.
