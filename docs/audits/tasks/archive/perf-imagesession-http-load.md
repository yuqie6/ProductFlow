# 任务：连续生图目标规模 HTTP 读取验收

状态：完成
类型：实现
认领者：主代理-perf-0905-1644
认领于：2026-09-05T16:44:19+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：无

本任务已验收，随交付提交关闭；认领与关闭遵循 [Issue 协议](../README.md)。

## 问题来源

用户授权本会话负责平台可靠性组落地。父账本 PERF-08 和 [详情有界任务](perf-imagesession-detail.md) 已记录目标规模 SQL 与返回数量，但尚无同一规模的 HTTP payload/延迟证据。返回有界不能证明序列化和关联数据成本有界。本任务补可复跑的 HTTP 验收入口，不预设存在运行时缺陷。

## 做成什么样

复用既有隔离 PG 夹具与真实 HTTP handler，覆盖连续生图列表、详情和历史读取，验证响应合同及数量边界，记录数据规模、payload bytes、warmup/样本数与 p50/p95。门槛按现有合同与现场基线确定并写明依据；不将本地 fixture 结果声明为生产 SLO。若发现运行时缺陷，先在本单记录真实根因和必要修改范围，再修复并补回归。

## 前置与并行

- 前置：`perf-imagesession-detail` 已交付有界详情与目标规模 SQL 测试。
- 冻结输入：保持既有 list/history cursor、详情最多 60 条任务、PG 权威和 Status/SSE 合同，不改 provider 或生图调度。
- 运行资源：仅用独立临时测试数据库及 httptest；不重建共享 dev DB，不调用 provider，不停 dev 进程，不占图片采集浏览器或目录。原始采证目录 `/tmp/productflow-perf-imagesession-http-0905-1644/`。
- 认领确认：用户授权本会话负责本组交付；本组主代理已核对共享看板、现有三项保留认领的任务包和 Git diff 占用，登记本单及看板后复读确认。与 eval 的 Go Agent 文件、图片池、Skill 固定 checkout 无写入交集。无认领提交。

## 只改这些文件

- `go/internal/imagesession/`：有界读取调查、HTTP 负载测试及贴近触发点的必要回归；运行时改动须先补根因。
- `justfile`：仅新增本专项 opt-in 命令（如有必要）。
- 本文件、父账本及公共任务索引：仅本任务登记与验收结论。

## 不要碰

- Agent、Graph、queue、imageeval、Web、schema、provider 设置、共享 DB 和其它任务 WIP。
- 不引入 Redis cache、新分页协议或新业务 API；不以索引使用与否独立决定优化。

## 现在代码在哪

- `go/internal/imagesession/serialize.go`：详情活动/近期终态/首屏任务有界、队列排名。
- `go/internal/imagesession/service.go`、`http.go`：列表、详情、history 服务和 HTTP 入口。
- `go/internal/imagesession/query_plan_test.go`、既有 HTTP 测试：隔离目标规模夹具和响应回归。
- `just go-test-imagesession-query-plan`：25k 会话、10k 轮次、1k 任务既有测量入口。

## 合同

PERF-08：PG 业务权威、列表批量摘要、history keyset、详情任务最多 60 条且活动集/首屏不互相挤掉；完整历史不塞回详情。HTTP 测量须消费真实响应并断言状态、字段和数据身份，不能只测 serializer 或无数据的空路径。

## 怎么验收

- 包内 HTTP/查询回归和新增测量辅助测试通过。
- opt-in PG HTTP gate 有效执行：目标规模夹具、固定样本数、数据合同和 payload 边界、逐路径延迟摘要，原始日志保留。
- 复跑 `just go-test-imagesession-query-plan`，对照 SQL 和 HTTP 的证据范围。
- `just docs-check`、本任务 `git diff --check`，自审后一次交付提交。
- 缺少实际 PG 运行或有效数据断言不得关闭。仅本地延迟结果不决定组内整体通过。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：主代理-perf-0905-1644
- 交接：本任务所有测试进程均退出，隔离 `pf_ihttp_*` / `pf_iplan_*` 库由测试清理；未调用 provider、重建 dev 库或停止共享进程。其它任务现有改动保留。

## 证据

- 日期：2026-09-05，Asia/Shanghai。基线 HEAD `1dccfb59a09802eab194ee0d568079c9c4c8b8e5` 加本任务测试 diff；被测 ImageSession 运行时代码未改动。共享工作树含其它组 WIP，不声明整树验收。
- 实现：`go/internal/imagesession/http_load_test.go` 新增 opt-in gate；`http_test.go` 提取注入数据库的已有 HTTP server 构造入口；`justfile` 新增 `go-test-imagesession-http-load`。没有修改业务运行时、schema、DTO 或前端。
- fixture：25,000 会话、单热会话 10,000 轮次 / 1,000 任务、990 条 applied effect、6 条参考图元数据。SQL count 校验规模；round/task prompt 2160B，task progress note 512B，round/effect 原始请求字段包含 4096B 填充及禁止泄漏标记。参考图仅为元数据；未创建图片文件，也未测图片下载。
- 测量边界：带真实登录 cookie 的 loopback HTTP，计时覆盖 client.Do 至完整响应体读取/关闭，不含客户端 JSON 校验。并发 1，每路径预热 10 次、正式 100 次；nearest-rank p50/p95 使用排序后的第 50/95 个样本。每次响应都校验 200、payload <1MiB 和内容；未登录详情必须 401。
- 内容断言：列表顺序/续页、热会话 10k count 和最新资产；详情 20 轮 / 50 个指定任务 / 6 参考元数据，queued 与 succeeded/effect 身份、进度、队列位置；详情游标实际续取历史第 21-40 条；history 20/100 的轮次及生成资产身份；原始 provider 请求不泄漏。
- 预算：列表及历史页沿用父账本建议初始预算 p95 <300ms、payload <1MiB。详情固定 fixture 同样设置 <1MiB 回归上限，仅记录延迟，不新增生产 SLO。固定 prompt/metadata 长度不代表所有合法业务输入的最大值。
- `just go-test-imagesession-http-load`：PASS，12.573s，七条路径各 100 个有效样本，无跳过。原始日志 `/tmp/productflow-perf-imagesession-http-0905-1644/http.log`。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1'`：PASS，6.756s；opt-in 项默认跳过，以上 HTTP 和下方 SQL 分别实跑。日志同目录 `package.log`。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -count=1 -p 1 -race'`：PASS，23.872s。日志同目录 `race.log`；未把 race 耗时混入 HTTP 测量。
- `just go-test-imagesession-query-plan`：PASS，2.400s。既有薄 SQL fixture 未修改；任务三组实际返回 10/20/20，execution 0.019/0.028/0.093ms；COUNT 1.185ms，COUNT 与首屏任务关联仍含 Seq Scan。日志同目录 `query-plan.log`。SQL fixture 与增强 HTTP fixture 行数一致，内容宽度不同，不能把两者毫秒数直接相减作为开销归因。
- `just docs-check` 与 `git diff --check`：PASS。第一次 docs-check 遇到工作流体验组正在同步的三条任务链接/看板暂态，未修改对方文件；对方同步完成后复跑通过。
- 交付定位：随本任务提交。
- 审核者 / 结论：主代理-perf-0905-1644 自审，无独立子代理审核。复核新增文件与完整 diff：复用既有 server/seed；测试在隔离库，未改运行时；响应身份、cursor、预算及真实 HTTP/PG 入口符合合同。
- Issue 结果：PASS，验收入口已实现且有效采证。PERF-08 继续部分完成；单热会话、单客户端、元数据读取不代表生产并发、媒体传输或真实访问分布。未复现需要改运行时的缺陷，不声明性能提升或整个组完成。

| 路径 | p50 ms | p95 ms | 最大 payload B |
|---|---:|---:|---:|
| list 20 | 23.77 | 25.54 | 4,279 |
| list 100 | 24.23 | 25.46 | 19,319 |
| list 下一页 20 | 2.93 | 3.39 | 3,884 |
| detail | 14.78 | 16.33 | 258,037 |
| history 20 | 3.70 | 4.47 | 60,865 |
| history 100 | 6.38 | 7.33 | 302,625 |
| history 下一页 20 | 3.84 | 4.65 | 60,585 |
