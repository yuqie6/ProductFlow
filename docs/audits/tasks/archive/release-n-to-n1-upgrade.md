# 任务：N→N+1 升级合同与夹具（B5）

状态：完成
类型：实现
认领者：sub-release/release-n-to-n1-upgrade
认领于：2026-09-07T13:48:00+08:00
完成于：2026-09-07T14:00:00+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：R6 汇总另门；不宣称 R6

按 [Issue 协议](../README.md) 认领。承接 [B4 D3 演练](release-d3-restore-drill.md) 与父章程 D4/B5。协调者审核：2026-09-07 CTO 通过——upgrade 脚本与等价夹具/fail-stop 证据成立；≠R6/冻结稳定版对。

## 问题来源

首个稳定版起须有 N→N+1 升级合同、夹具与文档歧义收窄；当前仅有备份/恢复与单次 D3 证据。

## 做成什么样

1. 钉死 N→N+1：迁移失败停机/回退、双版本夹具最小步骤、文档中歧义句收窄。
2. 隔离双版本或等价夹具可跑通主路径；缺口诚实列出。
3. 不宣称 R6；不写虚假 RPO/RTO/SLA。

## 前置与并行

- 前置：B4 已归档。
- 隔离项目；禁止共享 `productflow` down。
- 排他写入：升级脚本/文档、父章程 B5、本文件。勿改业务 schema 除非迁移合同必需。

## 只改这些文件

- `scripts/release-upgrade.sh`（新增）
- `scripts/release-pack.sh`（包装入 upgrade 脚本）
- `justfile`（`release-upgrade`）
- `release/README.md`（B5/D4 合同）
- `CONTEXT.md`、`docs/README.md`（歧义收窄）
- 根 `README.md`（升级入口指针）
- `docs/audits/performance-governance.md`（B5 结论行）
- 本文件

## 不要碰

`auth` / `product` / `graph` / `imagesession` 业务实现；看板 `docs/audits/tasks/README.md`；共享 `productflow` down；git commit/push/reset。

## 合同

- 父章程 B5/D4；完成 ≠ R6；等价夹具 ≠ 冻结稳定版对。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核 / 归档。
- 交接：合同、脚本、文档歧义收窄与隔离等价夹具证据已写入；状态保持认领等审核。未 git commit/push/reset；未改看板 README；未改 auth/product/graph/imagesession。共享 `productflow` 未 down。隔离项目已 `down -v`，`/tmp/pf-b5-upgrade-20260907` 已清理。≠ R6。

## 证据

### 窗口与资源（2026-09-07）

- 工作树分支：`codex/development`。
- 发行物 N：`dist/release/productflow-0.0.0-5ed2b916b569`（本地镜像 `productflow-{go,agent,web}:0.0.0-5ed2b916b569`）。
- 等价 N+1：`docker tag` → `productflow-{go,agent,web}:0.0.0-b5n1equiv`（同字节；**非**冻结稳定版对）。
- 临时目录：`/tmp/pf-b5-upgrade-20260907/{n,n1,backups,evidence}`（结束后删除）。
- 隔离项目：`pf-b5-n-20260907`（唯一后缀；默认拒绝共享名已测）。
- 宿主端口：`APP_HOST_PORT=49380`、`WEB_PORT=49381`；叠用 `docker-compose.prod-ports.yml`。
- 密钥：仅演练合成 `.env`；**未入库**。
- 共享项目：全程仅 `productflow` 的 postgres/redis；未对其执行 `down` / `down -v` / `just dev-stop`。

### 合同落地

| 项 | 内容 |
|---|---|
| 脚本 | `scripts/release-upgrade.sh`：预检 → drain 备份 → N pin 快照 → 换 N+1 pin → `compose run --rm migrate` → 四项 health；失败停 app 层、钉回 N pin、打印 `release-restore` 回退 |
| 包装 | `release-pack.sh` 将 upgrade 脚本写入包内 `scripts/` 与 `SHA256SUMS` |
| Runbook | `release/README.md` 新增「稳定版 N→N+1」节（起点、版本对、最小步骤、失败停机/回退、等价夹具） |
| 歧义收窄 | `CONTEXT.md` Product + Mainline Scope；`docs/README.md` 写作规则 6：禁止退役范式兼容 ≠ 禁止稳定版经营数据 `schema.Apply` N→N+1 |

### 命令序列（摘要）

1. 复制发行包到 `/tmp/.../n`，生成合成 `.env`；retag 三镜像为 `0.0.0-b5n1equiv` 写入 `/tmp/.../n1`。
2. `docker compose -p pf-b5-n-20260907 … up -d`；四项 health；migrate Exit 0。
3. 探针：`POST /api/auth/session`（`admin_key`）PASS；`POST /api/v2/products` 上传 **400**（见缺口）；PG `b5_upgrade_probe` + Agent `/data/sessions/b5-probe.txt` PASS。
4. `COMPOSE_PROJECT_NAME=productflow … release-upgrade.sh` → 拒绝（refuse OK）。
5. `UPGRADE_SIMULATE_MIGRATE_FAIL=1` → exit≠0、`UPGRADE STOPPED`、`images.env` 钉回 N、探针行仍在。
6. 正式 `release-upgrade.sh` → `0.0.0-5ed2b916b569` → `0.0.0-b5n1equiv`；migrate exit 0；四项 health；探针保留。
7. `RELEASE_PACK_OUT=… release-pack` 含 `scripts/release-upgrade.sh`；`just docs-check` PASS。
8. 隔离项目 `down -v`；删除临时目录与 retag 标签。

### 断言结果

| 断言 | 结果 |
|---|---|
| 拒绝共享项目名 `productflow` | PASS |
| fail-stop（模拟 migrate 失败）停 app 层并钉回 N pin | PASS |
| fail-stop 后经营探针行仍在 | PASS |
| 主路径换 pin + migrate 0 + 四项 health | PASS |
| PG `b5_upgrade_probe.note=pre-upgrade` 升级后仍在 | PASS |
| Agent `b5-probe.txt` 升级后仍在 | PASS |
| `release-pack` 含 upgrade 脚本 | PASS |
| `just docs-check` | PASS |
| 商品 multipart 创建（发行镜像） | **FAIL/未用**（session OK，create 400；主路径改用 PG+Agent 探针） |
| 共享 `productflow` 未动 | PASS |
| 冻结稳定版对正式 D4 | **未做**（诚实缺口） |

### 缺口（诚实）

1. **无冻结稳定版对**：仅等价 retag；不能写成正式 D4 / 稳定 N→N+1 已在生产 pin 上验收。
2. **发行镜像商品创建 400**：本夹具未深究 multipart 字段合同；未改 product/auth；登录会话可用。
3. **真实 schema 差分 migrate**：N 与等价 N+1 同 `schema.Apply` 字节；未证明含破坏性 DDL 的失败回退数据面（fail-stop 为模拟 entrypoint）。
4. **在途 lease / unknown**：未挂真实作业夹具。
5. **镜像 digest**：本地 tag，无 registry RepoDigest。
6. **≠ R6**；不写 RPO/RTO/SLA。

### 非宣称

- 未宣称 R6 通过；未宣称冻结稳定版对 D4；未写容量/RPO/RTO/SLA。
- 未把「等价夹具 PASS」写成「任意 HEAD 全量 build/push 或稳定发布已通」。

### 自审

- 未改 auth/product/graph/imagesession/schema 业务实现；未改看板 README；未 git commit/push/reset。
- 未对共享项目名 `productflow` 执行破坏性 compose。
- 隔离项目与临时目录已清理。

- 审核者：CTO（本会话）；结论：通过。可拆 R6 汇总门；正式 D4 需冻结稳定版对。
