# Go 业务后端（双轨）

Cutover 前 Python `backend/` 仍是默认进程。这里按 [`docs/specs/go-backend-rewrite-design.md`](../docs/specs/go-backend-rewrite-design.md) 竖切实现。

```bash
just go-test
just go-api
```

`GET /healthz` 与 Python 相同：`{"status":"ok"}`。`GET /healthz/ready` 额外 ping PostgreSQL，不是现有 Web 合同。

已接线的浏览器合同：`/api/auth/session`、`GET /api/settings/lock-state`、`GET /api/settings/runtime`。Session cookie 名仍是 `session`，签名用 Go 自己的 cookie store，cutover 会要求重新登录。

P3 媒体原语：`internal/media` 核验 PNG/JPEG/WEBP、按 Python 文案做 415/413/400 上传校验，写入 `media_objects`（`verification_status=verified`），并生成 preview（最长边 1600）与 thumbnail（320）JPEG 派生图。

P4 商品 API 已接到 Go HTTP：四条出生命令、`GET/PUT /api/v3/products/{id}/facts`（不可变 version + expected 冲突 409）、封面与追加上传、商品图库 Explorer（系统目录/用户文件夹/keyset cursor/ZIP 打包）、以及 `deletion_enabled` 门禁下的商品/图片删除。下载走 `/api/v2/product-image-assets/{id}/download`。

P6 schema-v3 Graph Command HTTP 已接线：Catalog、空画布、ChangeSet、撤销重做、提案确认/丢弃。跑图入队仍归 P8。

P7 工作流配方 HTTP 已接线：从 live 图提取、列表/归档、预览与应用。应用走 Graph Command（`actor_type=recipe`），完整配方创建空商品上的新图，片段合并进已有图。`origin=official` 对在线路由一律 404。
