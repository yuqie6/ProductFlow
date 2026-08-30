# ProductFlow HTTP 合同包

2026-08-29 历史封印快照的机器可读尺子。行为说明仍以 `docs/PRD.md`、`docs/ARCHITECTURE.md` 和测试为准。

当前业务 API 是 Go（`go/cmd/productflow-api`）。Go 是现行 HTTP 行为的来源：封印文件与 Go 不一致时改 Go。json 默认不再重生。

`openapi.json` 与 `http-routes.json` 是 2026-08-29 历史封印快照。

| 文件 | 内容 |
|---|---|
| `openapi.json` | 2026-08-29 封印的 OpenAPI 3 schema |
| `http-routes.json` | method + path + route name |
| `session.md` | Cookie session |
| `sse.md` | Agent Turn SSE |
| `queue.md` | durable 投递与 actor 名 |
| `errors.md` | 错误 JSON |
