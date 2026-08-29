# Go 业务后端（双轨）

Cutover 前 Python `backend/` 仍是默认进程。这里按 [`docs/specs/go-backend-rewrite-design.md`](../docs/specs/go-backend-rewrite-design.md) 竖切实现。

```bash
just go-test
just go-api
```

`GET /healthz` 与 Python 相同：`{"status":"ok"}`。`GET /healthz/ready` 额外 ping PostgreSQL，不是现有 Web 合同。

已接线的浏览器合同：`/api/auth/session`、`GET /api/settings/lock-state`、`GET /api/settings/runtime`。Session cookie 名仍是 `session`，签名用 Go 自己的 cookie store，cutover 会要求重新登录。

P3 媒体原语：`internal/media` 核验 PNG/JPEG/WEBP、按 Python 文案做 415/413/400 上传校验，写入 `media_objects`（`verification_status=verified`），并生成 preview（最长边 1600）与 thumbnail（320）JPEG 派生图。商品下载 HTTP 仍在 P4 接到 `/api/v2/product-image-assets/{id}/download`。
