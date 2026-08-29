# Go 业务后端

默认运行时是 `just go-api` / `just go-worker` / `just go-dispatcher`。schema 用 `just go-migrate`（GORM AutoMigrate + 约束补钉）。Python `backend/` 保留封印树与 Compose profile `python` 回退。实现按 [`docs/specs/go-backend-rewrite-design.md`](../docs/specs/go-backend-rewrite-design.md) 竖切。

```bash
just go-test
just go-migrate
just go-api
just go-worker
just go-dispatcher
```

`GET /healthz` 返回 `{"status":"ok"}`。`GET /healthz/ready` 额外 ping PostgreSQL，不是现有 Web 合同。

Session cookie 名仍是 `session`，签名用 Go cookie store；从 Python 切过来需要重新登录。

`STORAGE_ROOT` 相对路径相对**仓库根**解析（`just go-api` 使用 `go run -C go`，cwd 留在仓库根；Go `config.Load` 也会按 `go.mod` 所在 `go/` 的上一级收绝对路径）。本地默认 `./storage-dev`。Compose 里是 `/app/storage`。

HTTP 只写业务行和 `async_dispatches` PENDING，不在请求里打 broker。dispatcher 先标 SENT 再 asynq 投递；worker `MaxRetry=0`。无法证明的供应商结果标 `unknown`，不自动当失败重试。

Compose 默认启动三个 Go 进程，占用 `APP_HOST_PORT`（默认 29280）。不要同时跑 Python dispatcher 与 Go dispatcher。
