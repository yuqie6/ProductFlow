# Go 业务后端

默认运行时是 `just go-api` / `just go-worker` / `just go-dispatcher`。Python `backend/` 保留 Alembic 与 Compose profile `python` 回退。实现按 [`docs/specs/go-backend-rewrite-design.md`](../docs/specs/go-backend-rewrite-design.md) 竖切。

```bash
just go-test
just go-api
just go-worker
just go-dispatcher
```

`GET /healthz` 返回 `{"status":"ok"}`。`GET /healthz/ready` 额外 ping PostgreSQL，不是现有 Web 合同。

Session cookie 名仍是 `session`，签名用 Go cookie store；从 Python 切过来需要重新登录。

HTTP 只写业务行和 `async_dispatches` PENDING，不在请求里打 broker。dispatcher 先标 SENT 再 asynq 投递；worker `MaxRetry=0`。无法证明的供应商结果标 `unknown`，不自动当失败重试。

Compose 默认启动三个 Go 进程，占用 `APP_HOST_PORT`（默认 29280）。不要同时跑 Python dispatcher 与 Go dispatcher。
