# 任务：连续生图活动 Status/SSE 随 queued/running 增长的成本

状态：完成
类型：实现
认领者：主代理-perf-0905-2020
认领于：2026-09-05T20:20:21+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：dispatch 恢复与正常投递共存尾延迟由维护者核对照后再发，本单不连开

本任务已验收，随交付提交关闭；认领与关闭遵循 [Issue 协议](../README.md)。

## 问题来源

父账本「下一步如何选择」第 2 项与 PERF-08 剩余问题：`serialize.go:loadStatus` 全量读取 queued/running 并组装 effects，SSE 复用它。详情 HTTP gate 是历史页面形状；现有 `just go-test-imagesession-sse-load` 钉死 26 条 queued。商家一次多任务入队后，活动集放大是否让 Status/SSE 超过现行 `<1MiB` 回归上限或查询/回读失控，尚未用可复跑入口回答。不得丢掉必要活动任务。 [故障可见状态](perf-imagesession-recovery-visible.md) 不覆盖这条读取成本。

## 做成什么样

固定字段宽度，测量同一热会话 queued/running 增长时 Status 与 SSE 的任务数、effects、payload 字节、PG 回读和延迟，并给出是否改运行时的依据：

1. 复用隔离 PG 与真实 HTTP。至少覆盖现有 SSE 夹具规模（26）以及更大的活动集（建议 100、300，若内存/时间不够写明实际跑到的规模和原因）。
2. 断言活动任务不截断、`has_active` 与列表一致、effect 响应字段不变、原始 `request_json`/`result_json` 不进 Status。
3. 对照现行 ImageSession HTTP 回归：固定 fixture 响应 `<1MiB`。超过或查询次数随活动集线性失控时，在本单记录根因，只在因果点修复；不得分页丢掉活动任务，不得引入缓存或兼容层。
4. 测量可以结论为保持全量活动集，但必须写出商家可见成本和未覆盖边界（并发订阅、真实 prompt 宽度、生产分布）。

## 前置与并行

- 前置：[详情有界](perf-imagesession-detail.md)、[HTTP gate](perf-imagesession-http-load.md)、状态字段窄修与 SSE 重复快照过滤已交付。本单消费现行 `loadStatus` / SSE。
- 冻结输入：不改 provider、生图调度、列表/history cursor、详情最多 60 条任务的合同。
- 运行资源：仅隔离临时库与 httptest；不重建共享 dev DB，不调用真实 provider，不停 `just dev`，不占图片采集浏览器或 inbox/pool。原始日志 `/tmp/productflow-perf-imagesession-active-status-0905/`。
- 占用核对：`eval-development-baseline` 独立 checkout/STORAGE_ROOT；`image-eval-pool` 占用共享 API/worker 与真实 provider；`eval-skills` 阻塞并冻结 Skill。本单写入 `go/internal/imagesession/`、连续生图 Status 合并路径与本任务文档，与上述无交集。看板 README 已有其他组未提交行，提交时只暂存本任务变更。

## 只改这些文件

根因确认后的实际修改路径：

- `go/internal/imagesession/serialize.go`、`dto.go`、`status_projection_test.go`、`active_status_load_test.go`
- `web/src/pages/image-chat/branching.ts` 与 `branching.test.ts`：Status 省略 prompt 后 overlay 保留缓存提示词，新任务 id 回源详情
- `justfile`：`go-test-imagesession-active-status`
- `docs/ARCHITECTURE.md`、`docs/ARCHITECTURE.en.md`、`docs/ROADMAP.md`、`docs/ROADMAP.en.md`、父账本、归档索引、历史证据

未改 schema、queue、provider、详情 60 条上限、全局 `TaskTimeout`。

## 不要碰

- Agent、Graph 执行、queue 状态机、imageeval、schema、provider 设置、共享 DB
- 不引入 Redis cache、新分页协议或新业务 API；不以索引使用与否单独决定优化
- 其他任务未提交 diff

## 现在代码在哪

- `go/internal/imagesession/serialize.go`：`loadStatus` 按 `imageSessionActiveTaskStatuses` `Find` 全量活动任务并 `listEffectsByTaskIDs`；`serializeStatusTask` 清空 prompt
- `go/internal/imagesession/service.go`：`Status` 走 `loadStatus`
- `go/internal/imagesession/sse.go`：每个回读调用 `Service.Status`
- `go/internal/imagesession/active_status_load_test.go`：包内 26/100/300 HTTP；`just go-test-imagesession-active-status` 隔离规模门
- `web/src/pages/image-chat/branching.ts`：`mergeImageSessionStatusIntoDetail` overlay；新任务 id 触发详情刷新

## 合同

- PERF-08：PG 权威；列表/history 有界；详情活动/近期/首屏合计最多 60 且互不挤掉。Status/SSE 必须返回该会话全部必要活动任务，不得为限制数量丢掉 queued/running。
- `has_active_generation_task` 由同次任务列表推导。
- 状态与详情共用的 effect 摘要不加载 `request_json`/`result_json`。
- Status/SSE 不下发 `prompt`；提示词以详情和提交响应为准。
- 空任务投影不读全局队列；有活动任务时队列字段仍要返回。
- 父账本 ImageSession HTTP 回归：固定 fixture `<1MiB`；SSE 专项不等同完整容量门。

## 怎么验收

- 包内 HTTP/SSE/状态回归通过；新增或扩展的活动集测量有效执行。
- 记录各规模的任务数、effects、payload、回读、样本数与 p50/p95（样本不足不报分位数）。
- 若改运行时：同输入对照，并补不截断活动任务的回归。
- `just docs-check` 与本任务 `git diff --check` 通过。
- 缺少实际 PG 运行或有效数据断言不得关闭。仅本地延迟不决定组完成。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：主代理-perf-0905-2020
- 交接：测试进程已退出。隔离库由 `testdb` / `IsolatedMigrated` 清理。未调用真实 provider，未停共享 API/worker/dispatcher，未改图片池或其他任务 diff。

## 证据

- 日期：2026-09-05，Asia/Shanghai。基线工作树 HEAD `91e7f86e` 加本任务 diff；共享工作树含其他组 WIP，不声明整树验收。
- 夹具：1 running + 其余 queued，各 1 条 effect（64KiB `request_json`/`result_json` 不得出现在 Status），prompt=`httpLoadPrompt`×40（2160B），进度 `note` 512B。
- 修改前包内真实 HTTP：26=107,272B、100=411,976B、300≥1,048,577B（触及 `<1MiB` 上限）；每请求查询 8 次，任务列表与 effect 各 1 次。
- 根因：Status/SSE 与详情共用完整 `TaskResponse`，每 2s 回读把静态 prompt 再发一遍。查询次数不随 N 线性增加。
- 实现：`serializeStatusTask` 清空 prompt，`json:"prompt,omitempty"` 不下发该键；详情仍带完整提示词。前端 overlay 在 Status 缺 prompt 时保留缓存值；Status 出现未知任务 id 时回源详情。未对 GORM 使用 `Omit("prompt")`，避免污染后续详情查询。
- 修改后包内 HTTP：26=50,800B、100=194,775B、300=584,042B。
- `just go-test-imagesession-active-status`（IsolatedMigrated）：26=50,784B；100=194,751B；300 条 warmup=10 samples=100，p50=16.243555ms，p95=18.161542ms，max=584,030B，queries_per_request=8，SSE 快照 584,030B。原始日志 `/tmp/productflow-perf-imagesession-active-status-0905/results.jsonl`。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1'`：PASS，8.340s。
- `pnpm --dir web exec vitest run src/pages/image-chat/branching.test.ts`：31 passed。
- `just docs-check` 与本任务 `git diff --check`：PASS。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/perf-imagesession-active-status.md` 查询）。
- 审核者 / 结论：主代理-perf-0905-2020 自审，无独立子代理审核。复核完整 diff：因果点是 Status JSON 重复 prompt；活动任务未截断；effect 字段仍在；未加缓存或分页；未暂存其他任务文件。
- Issue 结果：PASS。业务门槛：固定夹具下活动 Status 保持全量任务且 `<1MiB`；未宣称组完成或生产 SLO。剩余缺口：真实 prompt 宽于 2160B、多订阅并发、rounds COUNT/关联扫描与生产分布未覆盖。
