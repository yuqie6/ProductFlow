# 任务：连续生图详情读取有界

状态：开放
认领者：—
认领于：—
业务组：性能
父账本：performance-governance.md
完成后可拆：无（PERF-12 Agent Session 目标规模由性能组章程在当前三份都归档后再发）

读完本文件就可以改代码。认领前不要改「只改这些文件」。认领步骤见 [README.md](README.md)。

## 做成什么样

`GET /api/image-sessions/:id` 不再一次 `Find` 出该会话所有匹配的 generation tasks。活动任务与首屏轮次所需任务有上限或 keyset；完整历史继续走已有 `GET /history`。

现在 `serializeDetailTasks`（`go/internal/imagesession/serialize.go`）对 status 过滤后无 LIMIT。热会话任务多时详情会胀。`just go-test-imagesession-query-plan` 已证明列表/history 索引；COUNT/无 LIMIT 详情仍可能 Seq Scan。

## 只改这些文件

- `go/internal/imagesession/`（详情任务查询、DTO 注释、HTTP/query-plan 测试）
- 本文件

前端若必须跟着分页字段改，只允许 `web/src/lib/api.ts` 里 ImageSession 详情类型，以及直接消费 `generation_tasks` 的连续生图页。不要改工作台画布。

## 不要碰

- `go/internal/graph`、`go/internal/imageeval`、Agent、queue
- 列表游标合同（已落地）

## 合同

- PostgreSQL 仍是权威。不要用 Redis 缓存详情来「变快」。
- `History` 已经 keyset；不要把 history 再塞回详情。
- 状态轮询继续用 `Status` / SSE，不要让详情承担轮询。
- DTO 字段名保持 `generation_tasks`；语义改为有界活动集。若需要 `has_more_tasks`，一起改测试与前端类型。

## 怎么验收

```bash
go test -C go ./internal/imagesession -count=1 -p 1
just go-test-imagesession-query-plan
```

query plan 或测试必须证明详情任务查询带 LIMIT / 有界 OR，而不是全表匹配任务。把 execution ms 与是否 Seq Scan 写在下面。

## 证据

- 测试名：
- plan：
- 日期 / commit：
