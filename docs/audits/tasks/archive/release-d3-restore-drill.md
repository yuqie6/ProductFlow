# 任务：D3 恢复演练证据（B4）

状态：完成
类型：证据
认领者：sub-release/release-d3-restore-drill
认领于：2026-09-07T13:36:00+08:00
完成于：2026-09-07T13:42:00+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：B5 N→N+1；不宣称 R6

按 [Issue 协议](../README.md) 认领。承接 [B3 备份恢复](release-backup-restore.md) 与父章程 D3。协调者审核：2026-09-07 CTO 通过——隔离全栈 D3 主路径证据成立；在途 lease UNKNOWN 已诚实记录；≠R6/B5。

## 问题来源

B3 已交付备份/恢复脚本与数据面冒烟；缺完整 D3：新目录/实例恢复后权限、资产、任务与关键服务健康的可复核证据。

## 做成什么样

1. 用官方 `release-backup` / `release-restore` 在隔离项目做一次全栈恢复演练（真实或接近真实的 compose 镜像，非仅 stub 数据面）。
2. 记录：登录/会话、媒体 HTTP、Agent health、在途作业收敛（或明确 unknown）、版本身份与 CHECKSUMS。
3. 证据写入本文件与父章程 B4 行；缺口诚实列出。不写虚假 RPO/RTO/SLA；不宣称 R6。

## 前置与并行

- 前置：B3 已归档。
- 隔离 compose 项目与卷；禁止共享 `productflow` down；不入库真实生产秘密。
- 排他写入：本文件、父章程 B4 结论、必要时 `release/README.md` 演练备注。勿改业务 schema/auth/product。

## 只改这些文件

- `docs/audits/tasks/release-d3-restore-drill.md`（本文件）
- `docs/audits/performance-governance.md`（B4 结论行）
- `release/README.md`（B4 演练备注，一句）

## 合同

- 父章程 D3 / B4；完成 ≠ R6 / ≠ B5。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核 / 归档。
- 交接：隔离全栈 D3 演练证据已写入；状态保持认领等审核。未 git commit/push/reset；未改看板 README；未改 auth/product/graph。共享 `productflow` 未 down。隔离项目已 `down -v`，`/tmp/pf-d3-drill-20260907` 已清理。≠ R6 / ≠ B5。

## 证据

### 窗口与资源（2026-09-07）

- 工作树分支：`codex/development`；演练时 `HEAD`=`3e79f74761a348260c18bd5303b34f248cd0ff67`（与发行物 pin 不同，见下）。
- 发行物：`dist/release/productflow-0.0.0-5ed2b916b569/`（本地已有 `productflow-{go,agent,web}:0.0.0-5ed2b916b569`）。
- 临时目录：`/tmp/pf-d3-drill-20260907/{src,dst,backups,evidence,seed}`（结束后删除）。
- 源项目：`pf-d3-src-20260907`；目标项目：`pf-d3-dst-20260907`（唯一后缀；默认拒绝共享名已复核）。
- 宿主端口：`APP_HOST_PORT=49280`、`WEB_PORT=49281`；叠用 `docker-compose.prod-ports.yml`（PG/Redis/metrics 不发布到宿主）。
- 密钥：仅演练合成值写入临时 `.env`（`ADMIN_ACCESS_REQUIRED=true`）；**未入库**。
- 共享项目：全程仅 `productflow` 的 postgres/redis 在跑；未对其执行 `down` / `down -v` / `just dev-stop`。

### 命令序列（摘要）

1. `docker compose -p pf-d3-src-20260907 … up -d`（发行包 `images.env` + 合成 `.env` + prod-ports）。
2. 源栈四项 health 通过；migrate `Exited (0)`。
3. 探针：`POST /api/auth/session`（`admin_key`，发行镜像合同）→ `POST /api/v2/products` 上传 PNG → Agent `/data/sessions/d3-probe.txt` → PG 插入假 `provider_profiles`（`d3-fake-api-key-not-production`）。
4. `COMPOSE_PROJECT_NAME=pf-d3-src-20260907 PRODUCTFLOW_RELEASE_DIR=…/src BACKUP_OUT=…/backups CONSISTENCY_MODE=drain bash scripts/release-backup.sh`。
5. 源项目 `down -v`（释放端口；备份已落盘）。
6. `COMPOSE_PROJECT_NAME=pf-d3-dst-20260907 PRODUCTFLOW_RELEASE_DIR=…/dst BACKUP_DIR=…/productflow-backup-20260907T053611Z-pf-d3-src-20260907 bash scripts/release-restore.sh`（`VERIFY_CHECKSUMS=1`）。
7. 目标栈断言后 `down -v`；删除临时目录。

### MANIFEST 关键字段

备份目录：`productflow-backup-20260907T053611Z-pf-d3-src-20260907`

| 字段 | 值 |
|---|---|
| `PRODUCTFLOW_BACKUP_FORMAT` | `1` |
| `BACKUP_STARTED_AT` / `FINISHED_AT` | `2026-09-07T05:36:11Z` / `2026-09-07T05:36:17Z` |
| `COMPOSE_PROJECT` | `pf-d3-src-20260907` |
| `CONSISTENCY_MODE` | `drain` |
| `JOBS_DRAINED` | `true` |
| `DRAINED_SERVICES` | `productflow-go-worker,productflow-go-dispatcher,productflow-agent-service` |
| `OBJECTS` | `postgres,storage,agent-data,env` |
| `PRODUCTFLOW_GIT_SHA` | `5ed2b916b5698cc10623f5a724acc708e2927a4a` |
| `PRODUCTFLOW_IMAGE_TAG` | `0.0.0-5ed2b916b569` |
| `PRODUCTFLOW_*_IMAGE_DIGEST` | 三镜像均为 `unresolved:<local-tag>`（无 registry digest） |
| `INCLUDE_REDIS` | `0` |
| `D3_CLAIM` / `R6_CLAIM` | `false` / `false` |

`sha256sum -c CHECKSUMS`：备份后与恢复前均 **OK**（dump / storage / agent-data / MANIFEST / env）。

### 恢复后断言结果

探针身份：商品 `e27741db-181e-4dc0-a5bd-66882eab1f43`（D3探针商品）；资产 `e54e0318-45a5-4abc-bb88-2dd5951f3b55`；媒体对象 `47beef6f-d367-4c0f-819d-d41e9ea40acf`（`verified`）。

| 断言 | 结果 |
|---|---|
| migrate Exit 0 | PASS |
| API `/healthz` | PASS `{"status":"ok"}` |
| web `/healthz` | PASS `ok` |
| web `/api/healthz` | PASS `{"status":"ok"}` |
| Agent `/healthz` `runtime=productflow-pi` | PASS |
| 同一合成 `ADMIN_ACCESS_KEY` 登录 + `authenticated=true` | PASS |
| 错误密钥 401 | PASS |
| 探针媒体 HTTP 下载（API + web 反代）非 missing | PASS（75 字节 PNG） |
| `media_objects.verification_status=verified` | PASS |
| provider `has_api_key`（假密钥行） | PASS |
| Agent `/data/sessions/d3-probe.txt` | PASS `d3-probe-src` |
| 版本身份与 CHECKSUMS | PASS（digest 未解析见缺口） |
| 在途作业收敛 | **UNKNOWN**（备份前后 `async_dispatches` 为空；`JOBS_DRAINED=true`；未挂真实 provider lease，故 unknown-not-auto-fail **未实跑**） |
| 共享 `productflow` 未动 | PASS |

### 缺口（诚实）

1. **在途作业 / unknown 收敛**：无活动 lease / 无供应商结果行；合同路径未用真实作业夹具证明，标记 UNKNOWN，不伪装 PASS。
2. **镜像 digest**：本地 tag 无 registry RepoDigest；MANIFEST 记 `unresolved`。
3. **发行包 scripts/**：所用 `dist/release/productflow-0.0.0-5ed2b916b569` 目录当时无包内 `scripts/`；演练改用源码树 `scripts/release-*.sh`（与 B3 工具链同一套）。
4. **鉴权合同是发行镜像**：`0.0.0-5ed2b916b569` 支持 `admin_key` 会话；`/api/auth/bootstrap` 404。工作树 HEAD 已有商家 bootstrap，**本演练不覆盖 HEAD 鉴权面**（且禁止改 auth）。
5. **未配真实商用 provider**；provider 断言仅为 PG 明文假密钥行可还原。
6. **≠ R6 / ≠ B5**；不写 RPO/RTO/SLA。

### 非宣称

- 未宣称 R6 通过；未宣称 B5；未宣称生产 RPO/RTO。
- 未把「脚本存在」写成「任意 HEAD 全量 build/push 已通」。

### 自审

- 未改 auth/product/graph/schema；未改看板 README；未 git commit/push/reset。
- 未对共享项目名 `productflow` 执行破坏性 compose；备份脚本拒绝共享名已测。
- 隔离项目与临时目录已清理。

- 审核者：CTO（本会话）；结论：通过。可拆 B5 N→N+1；在途 lease 夹具可另发。
