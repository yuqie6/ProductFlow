# ProductFlow HTTP 合同包

封印基线（2026-08-29 live Python）的机器可读尺子。Go 与剩余 Python 共用。行为说明仍以 `docs/PRD.md`、`docs/ARCHITECTURE.md` 和测试为准。

生成：

```bash
just export-http-contracts
```

| 文件 | 内容 |
|---|---|
| `openapi.json` | FastAPI 生成的 OpenAPI 3 schema |
| `http-routes.json` | method + path + route name |
| `session.md` | Cookie session |
| `sse.md` | Agent Turn SSE |
| `queue.md` | durable 投递与 actor 名 |
| `errors.md` | 错误 JSON |

Go 实现与 OpenAPI 冲突时改 Go，不改 Web / Agent service。重新导出后 diff 本目录。
