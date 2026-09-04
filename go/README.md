# Go 业务后端

默认运行时是 `just go-api` / `just go-worker` / `just go-dispatcher`。schema 用 `just go-migrate`（GORM `CreateTable`/`AddColumn` + ExtraDDL，不使用 AutoMigrate）。竖切与当前形状见 [`docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md)。退休的 FastAPI 树在 `retired/python`。

```bash
just go-test
just go-migrate
just go-api
just go-worker
just go-dispatcher
```

`GET /healthz` 返回 `{"status":"ok"}`。`GET /healthz/ready` 额外 ping PostgreSQL，不是现有 Web 合同。

Session cookie 名是 `session`，签名用 Go cookie store。

`STORAGE_ROOT` 相对路径相对**仓库根**解析（`just go-api` 使用 `go run -C go`，cwd 留在仓库根；Go `config.Load` 也会按 `go.mod` 所在 `go/` 的上一级收绝对路径）。本地默认 `./storage-dev`。Compose 里是 `/app/storage`。

JSON 日志写滚动文件，终端默认是可读行。默认目录是 `STORAGE_ROOT/logs`（本地即 `storage-dev/logs/`）：`productflow-api.log`、`productflow-worker.log`、`productflow-dispatcher.log`。可用 `LOG_DIR` 改路径；`LOG_FORMAT=json` 让 stderr 也输出 JSON；`LOG_MAX_BYTES` / `LOG_BACKUP_COUNT` / `LOG_RETENTION_DAYS` 控制滚动与按天清理。空闲 dispatcher 周期、`/healthz` 和 Agent heartbeat 只进文件（Debug），终端默认不刷。

HTTP 只写业务行和 `async_dispatches` PENDING，不在请求里打 broker。dispatcher 先标 SENT 再 asynq 投递；worker `MaxRetry=0`。无法证明的供应商结果标 `unknown`，不自动当失败重试。

Compose 默认启动三个 Go 进程，占用 `APP_HOST_PORT`（默认 29280）。Go dispatcher 是唯一 durable scanner。
