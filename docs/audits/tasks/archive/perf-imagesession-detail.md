# 任务：连续生图详情读取有界

状态：完成
类型：实现
认领者：主代理-0905-0336
认领于：2026-09-05T03:36:11+08:00
业务组：性能
父账本：performance-governance.md
完成后可拆：无（PERF-12 Agent Session 目标规模由性能组章程在当前三份都归档后再发）

本任务已关闭。认领与归档步骤见 [README.md](../README.md)，业务组结论见[父账本](../../performance-governance.md)。

## 做成什么样

`GET /api/image-sessions/:id` 不再一次 `Find` 出该会话所有匹配的 generation tasks。活动任务与首屏轮次所需任务有上限或 keyset；完整历史继续走已有 `GET /history`。

认领时 `serializeDetailTasks`（`go/internal/imagesession/serialize.go`）对 status 过滤后无 LIMIT。热会话任务多时详情会胀。当时 `just go-test-imagesession-query-plan` 已证明列表/history 索引；COUNT/无 LIMIT 详情仍可能 Seq Scan。

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

- 日期：2026-09-05（Asia/Shanghai）。认领提交 `9c10b99e`，代码交付 `232e51f7`。
- 采证基线：`f9b72cae` 加本任务 6 个 Go 文件的 diff；提交后文件内容与采证一致。共享工作树另有文档治理和 image-eval 改动，不声称整树 clean 或全仓验收。
- 实现：活动任务、近期失败/未知/取消及无对应轮次的成功任务、首屏轮次对应任务分别 `LIMIT 20`；合并按 `created_at DESC, id DESC` 排序并去重，`generation_tasks` 最多 60 条。旧的终态任务不再全部放入详情；`GET /history` 的已落盘轮次分页、Status/SSE 的活动集合同不变。
- 同一路径的 `queuedPositions` 原先加载全库 queued ID；现由 PostgreSQL 窗口函数计算原有全局排名，只读取本次所需 ID 的位置。不改队列调度、admission 或 schema。
- HTTP 回归：`TestImageSessionGetBoundsTasksWithoutCrowdingActiveOrFirstScreen` 使用 80 条本会话任务、25 个轮次和另一个会话，验证各组上限、活动/首屏任务不被挤掉、重叠去重为 59 条、同时间 ID 排序、状态和会话隔离、重试响应。
- 排名回归：`TestQueuedPositionsReturnsOnlyRequestedGlobalRanks` 验证跨会话全局排名、同时间 ID 顺序、仅返回所需 queued ID、重复 ID 与空输入。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1 -v'`：通过，6.694s；包内 opt-in query-plan 默认跳过，已以下列专项命令单独执行。原始日志 `/tmp/productflow-perf-imagesession-detail-tests.log`。
- `just go-test-imagesession-query-plan`：通过，3.141s。隔离迁移库内 25,000 会话、10,000 轮次、1,000 任务；捕获生产 serializer 发出的 SQL，三次任务读取均以 Limit 为顶层，分别返回 10/20/20 条。原始计划 `/tmp/productflow-perf-imagesession-detail-plan.log`。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=3 -p 1 -race -run "TestImageSessionGetBoundsTasks|TestQueuedPositions"'`：通过，2.941s。原始日志 `/tmp/productflow-perf-imagesession-detail-race.log`。
- `just docs-check`：归档、索引与链接调整后通过；`git diff --check` 通过。

| 查询 | execution ms | Seq Scan |
|---|---:|---|
| 活动任务，LIMIT 20 | 0.037 | 无；使用 status 索引 |
| 近期终态/无轮次成功任务，LIMIT 20 | 0.059 | 任务使用 session_created 索引；计划含 rounds Seq Scan 子计划，本次 Actual Loops=0 |
| 首屏轮次关联任务，LIMIT 20 | 0.133 | 有；扫描任务表后排序，返回 20 条 |
| rounds COUNT | 1.552 | 有；仍需计数完整热会话 |

## 验收与关闭

- 审核者：主代理-0905-0336，自审；没有独立子代理审核。
- 审核结论：实现与回归满足本任务合同，代码仅改 `go/internal/imagesession/` 六个文件。无新路由、DTO 字段、前端修改、迁移或 Redis 缓存；未修改 Graph、Agent、imageeval 或 queue 模块。
- Issue 结果：PASS，已交付。任务有界不等于数据库扫描成本有界；PERF-08 继续保留 COUNT、关联查询扫描和真实负载端到端 payload/延迟的观察项。性能组整体未验收完成。
- 关闭时更新父账本 PERF-08 与验证记录、移除看板行、修正 ROADMAP 中英文旧链接并加入归档索引。
- 完成后可拆：无；dispatcher 时延和容量指标两项尚未验收，不发布 PERF-12 后续任务。
