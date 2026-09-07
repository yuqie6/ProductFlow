# 任务：R6 资源预算与部署规模对应

状态：完成
类型：证据
认领者：sub-release/release-r6-resource-budget
认领于：2026-09-07T16:02:23+08:00
完成于：2026-09-07T16:12:13+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：总纲 R6 关闭裁定汇总门（仅当本门与既有 D1/D3/D4/G-07/pin 证据齐）

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

[pin + 正式 D4](release-r6-pin-and-formal-d4.md) 已交付 pin `0.0.0-67b0f3092158`（≡`67b0f309`）与正式 D4 PASS，并诚实记录：**总纲 R6 仍未通过**，主缺口为 ROADMAP R6 的「资源预算与部署规模对应」未测。父章程禁止把未测容量写成 SLA。

## 做成什么样

1. 在隔离 compose 上起 pin `0.0.0-67b0f3092158`（可用既有 pack `/tmp/pf-r6-d4-20260907/packs/…` 或同 pin 重 pack）；采证**空闲稳态**与**轻负载**（至少一次四项 health + 一次登录或 `/api` 探针）下各服务容器的 CPU/内存占用（`docker stats` 或等价），并记录宿主可用核数/内存。
2. 写出**部署规模对应表**：默认单副本拓扑（go-api / worker / dispatcher / agent / web / postgres / redis）→ 观测到的资源足迹区间；对照代码/配置中的并发与 admission 上限（如 generation capacity、worker 并发相关 env），说明「推荐最小主机规格」的**观测依据**，**不得**写成产品 SLA/RPO/RTO。
3. 缺口诚实列出（未测满载、未测多商、未测 staging 双副本等）。
4. 更新父章程与必要时 `release/README.md` 一句；**不得单独因本门标 R6 通过**——R6 关闭另开汇总或由维护者裁定。

## 前置与并行

- 前置：pin+正式 D4 已归档；冻结 pin `0.0.0-67b0f3092158` / SHA `67b0f309…`。
- 隔离项目；禁止共享 `productflow` down。
- 排他写入：本文件、父章程资源预算/R6 条目、必要时 `release/README.md`。

## 只改这些文件

- `docs/audits/tasks/archive/release-r6-resource-budget.md`（本文件）
- `docs/audits/performance-governance.md`（资源预算证据；**不得**假标 R6 通过）
- `release/README.md`（可选一句）

## 不要碰

- 杜撰 SLA/RPO/RTO；把观测数字写成保证吞吐；Skill/grader；共享 `productflow` down。

## 现在代码在哪

- pin/D4 证据：[release-r6-pin-and-formal-d4](release-r6-pin-and-formal-d4.md)
- 容量/admission 历史：父章程 PERF 节与 `go/internal` generation capacity
- 发行拓扑：`release/` + `docker-compose*.yml`

## 合同

- ROADMAP R6「资源预算与部署规模对应」；完成可 FAIL；观测证据 ≠ SLA。
- 本门 PASS 不自动关闭总纲 R6。

## 怎么验收

- 隔离栈 pin 身份可核对；stats 原始日志入库证据目录
- 对应表含拓扑、观测区间、对照的配置上限、明确非 SLA 声明
- 父章程与证据链接一致；`just docs-check` PASS

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已归档；交付随本任务提交。跟进见 [release-r6-close-ruling](release-r6-close-ruling.md)。

## 证据

### 窗口与资源（2026-09-07）

- 认领者：`sub-release/release-r6-resource-budget`；工作树 `codex/development`（协调脏文件仅文档）。
- 冻结 pin：`0.0.0-67b0f3092158` ≡ SHA `67b0f3092158d72c5e6e118761808fe5f48ef5a1`（包内 `VERSION` / `images.env` 与本地镜像 tag 一致）。
- 发行包：复用 `/tmp/pf-r6-d4-20260907/packs/productflow-0.0.0-67b0f3092158/` → 安装副本 `/tmp/pf-r6-budget-20260907/install/`。
- 隔离项目：`COMPOSE_PROJECT_NAME=pf-r6-budget-20260907`；叠用 `docker-compose.yml` + `docker-compose.prod-ports.yml`。
- 宿主端口：`APP_HOST_PORT=49580`、`WEB_PORT=49581`（避开共享 `15432`/`16379` 与本机 `2928x`）。
- 证据目录：`/tmp/pf-r6-budget-20260907/evidence/`。
- 共享项目：全程仅 `productflow` 的 postgres/redis；**未** `down` / `down -v` / `just dev-stop`。结束后隔离栈 `down -v`。

### 宿主清单

| 项 | 值 |
|---|---|
| CPU | 16 逻辑核（8 核 × 2 线程）；AMD Ryzen 9 7940HX |
| 内存 | 15 Gi total；采证时 available ≈ 1.8–3.1 Gi（宿主另有共享栈与其它容器） |
| Docker | Server 29.3.1 |
| 原始日志 | `evidence/host-inventory.txt` |

### Pin / 镜像身份

| 组件 | tag | Image ID |
|---|---|---|
| go（api/worker/dispatcher/migrate） | `productflow-go:0.0.0-67b0f3092158` | `963f67e9e16b` |
| agent | `productflow-agent:0.0.0-67b0f3092158` | `a620e77da59f` |
| web | `productflow-web:0.0.0-67b0f3092158` | `58ebc8f5d68a` |
| postgres / redis | `postgres:16` / `redis:7` | 上游 |

见 `evidence/pin-verify-before-down.txt`、`evidence/compose-up.log`。

### 探活与轻负载探针

| 步骤 | 结果 |
|---|---|
| 四项 health（API `/healthz`、web `/healthz`、web `/api/healthz`、Agent `/healthz` `runtime=productflow-pi`） | PASS（`evidence/smoke-health.txt`） |
| 空闲稳态：settle ≥25 s 后 `docker stats --no-stream` ×5 | `evidence/docker-stats-idle.txt` |
| 轻负载：20×三路 health + 并发 health 采样 ×5 | `evidence/light-load-probes.txt`、`docker-stats-light-load.txt` |
| 登录：本 pin 为 bootstrap 鉴权面；`POST /api/auth/bootstrap` → `POST /api/auth/session`（email/password）→ `authenticated=true`；经 web 反代 `GET /api/auth/session`；`GET /api/merchants` | PASS（`evidence/auth-login-probes.txt`） |
| 登录后并发 health+`/api/auth/session`+`/api/merchants` 采样 ×5 | `evidence/docker-stats-light-load-auth.txt` |
| 汇总 | `evidence/stats-summary.txt` |

**非 SLA 声明：** 下表为指定 pin、单副本、空库/近空库、无真实 provider、无 Graph/生图作业窗口内的容器 RSS/瞬时 CPU 观测，**不是**吞吐保证、可用性 SLA、RPO/RTO。

### 部署规模对应表（默认单副本）

拓扑：`go-api` ×1、`go-worker` ×1、`go-dispatcher` ×1、`agent-service` ×1、`web` ×1、`postgres` ×1、`redis` ×1（migrate 一次性退出，不计入稳态）。

| 容器 | 空闲 Mem（MiB） | 轻负载 Mem（MiB） | 空闲 CPU% 区间 | 轻负载 CPU% 区间 |
|---|---|---|---|---|
| agent-service | 69.9–70.2 | 70.1–70.4 | 0.00–9.56（偶发毛刺） | 0.00–0.01 |
| go-api | 9.9–9.9 | 12.6–16.6 | 0.00–2.71 | 0.00–0.07 |
| go-dispatcher | 26.3–26.5 | 26.6–27.1 | 0.21–0.25 | 0.24–1.23 |
| go-worker | 30.1–30.1 | 30.1–30.2 | 0.04–0.13 | 0.05–0.21 |
| postgres | 63.8–64.1 | 64.4–69.2 | 0.08–0.11 | 0.08–5.11 |
| redis | 5.7–5.8 | 5.7–5.8 | 0.26–2.85 | 0.23–2.71 |
| web | 18.9–18.9 | 19.2–19.2 | 0.00 | 0.00 |
| **合计（各服务 median Mem 之和）** | **≈225 MiB** | **≈233 MiB** | — | — |

采样口径：`docker stats --no-stream`；CPU% 为相对宿主瞬时占比；Mem 为容器 Usage（非 limit 承诺）。宿主同时跑共享 `productflow` postgres/redis 与其它容器，数字反映**隔离栈容器自身**足迹，不扣除宿主争用。

### 对照：配置 / 代码并发上限（默认发行值）

| 上限 | 默认 / 范围 | 入口 |
|---|---|---|
| asynq worker `Concurrency` | **4**（硬编码） | `go/cmd/productflow-worker/main.go` |
| 全库生图 admission `generation_max_concurrent_tasks` | 默认 **3**，夹紧 **1–20** | `go/internal/platform/generation/limit.go`；样例 env `GENERATION_MAX_CONCURRENT_TASKS` |
| Agent `AGENT_MAX_CONCURRENT_TURNS` | 默认 **3** | `agent-service/src/config.ts`；`release/.env.example` |
| dispatcher claim 批 | 默认 100；空闲 1 s / recovery 10 s | 父章程锁与时间预算表 |

观测窗口**未**触达上述并发上限（无 provider、无 asynq 业务信封、Agent `active_turns=0`）。加 worker 副本不能替代全库生图槽推导（父章程既有结论）。

### 观测依据的最小主机建议（非 SLA）

在本窗证据下，默认单副本发行栈空闲/轻探活合计容器 RSS ≈ **0.23–0.25 GiB**，瞬时 CPU 在探活下多为个位数百分比。结合：（1）镜像体积合计约 go 195 MB + agent 313 MB + web 61 MB + postgres/redis 上游层；（2）PostgreSQL / Node 在真实作业与缓存增长后会明显高于空库；（3）默认可同时存在 worker 并发 4、生图槽至多 20、Agent Turn 3，满并发成本**未测**；（4）生产还需 OS、Docker、备份窗口与日志余量。

**观测依据建议（Operator 选型参考，禁止当 SLA）：** 单主机默认单副本自托管，优先按 **≥2 vCPU / ≥4 GiB RAM** 起步规划磁盘与 swap；若仅跑空闲探活，本窗足迹远低于该规格，但不足以支撑「可按默认并发跑真实生图/Agent」的声明。更大规格应随满载/多商测量再定。

### 缺口（诚实）

1. **未测满载**：无真实 prompt/image provider、无 Graph/ImageSession/Agent Turn 并发打满。
2. **未测多商家 / 多租户**资源隔离与公平调度。
3. **未测** `docker-compose.staging.yml` 双副本 API/worker/dispatcher 共享卷。
4. **未测** 媒体大文件、ZIP 导出、25k 规模 HTTP 门在发行拓扑上的容器 RSS。
5. 宿主采证时内存紧张（available 曾低至 ≈1.8 GiB），与共享栈共存；数字不可直接外推空主机。
6. **本门 PASS ≠ 总纲 R6 通过**；R6 关闭需维护者按 D1/D3/D4/G-07/pin/本门与章程其余未齐项裁定。
7. 不写 RPO/RTO/SLA；不保证吞吐。

### 父章程 / release README

- 已回写 [performance-governance.md](../../performance-governance.md) 资源预算观测与「R6 仍未通过」。
- `release/README.md` 一句指向本任务证据。

### 验证

- `just docs-check`：PASS（`/tmp/pf-r6-budget-20260907/evidence/docs-check.txt`）。
- **未** `git commit` / `push` / `reset`。

### 审核

- 审核者：CTO（本会话）；结论：证据任务**完成**；空闲/轻负载足迹与对应表可采信；**非 SLA**。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/release-r6-resource-budget.md` 查询）
- Issue 结果 / 业务门槛结果 / 剩余缺口：完成；本门 ≠ 自动关闭 R6；R6 关闭见 [release-r6-close-ruling](release-r6-close-ruling.md)。
