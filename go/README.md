# Go 业务后端（双轨）

Cutover 前 Python `backend/` 仍是默认进程。这里按 [`docs/specs/go-backend-rewrite-design.md`](../docs/specs/go-backend-rewrite-design.md) 竖切实现。

```bash
just go-test
just go-api
```

`GET /healthz` 与 Python 相同：`{"status":"ok"}`。`GET /healthz/ready` 额外 ping PostgreSQL，不是现有 Web 合同。
