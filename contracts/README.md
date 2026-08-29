# ProductFlow HTTP 合同包

封印基线（2026-08-29 live Python）的机器可读尺子。Go 与剩余 Python 共用。行为说明仍以 `docs/PRD.md`、`docs/ARCHITECTURE.md` 和测试为准。

当前业务 API 是 Go（`go/cmd/productflow-api`）。Go 是现行 HTTP 行为的来源：封印文件与 Go 不一致时改 Go；若要改封印，等有专用导出后再单独更新。

`openapi.json` 与 `http-routes.json` 是 2026-08-29 Python 封印快照，默认路径不会从 FastAPI 重新生成。

| 文件 | 内容 |
|---|---|
| `openapi.json` | 2026-08-29 Python 封印的 OpenAPI 3 schema |
| `http-routes.json` | method + path + route name |
| `session.md` | Cookie session |
| `sse.md` | Agent Turn SSE |
| `queue.md` | durable 投递与 actor 名 |
| `errors.md` | 错误 JSON |
