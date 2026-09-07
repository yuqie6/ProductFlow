# 任务：R6 候选 pin 对齐与正式 D4

状态：完成
类型：证据
认领者：sub-release/release-r6-pin-and-formal-d4
认领于：2026-09-07T15:46:45+08:00
完成于：2026-09-07T16:00:37+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：资源预算↔部署规模证据门；总纲 R6 关闭裁定（仅当其余 R6 项齐）

按 [Issue 协议](../README.md) 认领。承接 [干净候选门](release-r6-clean-candidate-gate.md)。协调者审核：2026-09-07 CTO 通过——pin 对齐与正式 D4 PASS 可采信；业务门槛 **R6 仍未通过**（资源预算未测）。

## 问题来源

[干净候选门](release-r6-clean-candidate-gate.md) 已在 `e8cb494d`+最窄修补上使 G-07 PASS，并诚实记录：**总纲 R6 仍未通过**。缺口为发行 pin≠G-07 候选、无正式冻结稳定版对 D4、资源预算未测。修补已入库于交付 commit `67b0f3092158d72c5e6e118761808fe5f48ef5a1`（短 `67b0f309`）。

## 做成什么样

1. 以交付 commit `67b0f3092158d72c5e6e118761808fe5f48ef5a1`（短 `67b0f309`）为 G-07 源码身份；对该 SHA 执行 `release-build-images`（或诚实记录 TLS/网络失败）与 `release-pack`，产生新不可变 pin `VERSION-<sha12>`。
2. 以旧 pin `0.0.0-5ed2b916b569` 为 N、新 pin 为 N+1，在隔离项目上跑**正式 D4**（`release-upgrade.sh` 主路径 + migrate fail-stop）；**不得**用同镜像等价 retag 冒充冻结对。
3. 资源预算↔部署规模：本窗若不做须标明缺口，不得因此伪称 R6 通过。
4. 更新父章程与 `release/README.md`；**全项未齐不得标 R6 通过**。

## 前置与并行

- 前置：干净候选门已归档完成；协调树交付 commit `67b0f309`。
- 冻结输入：`67b0f3092158d72c5e6e118761808fe5f48ef5a1`；禁止窗口内再改被测运行时行为。
- 隔离 Docker 项目；禁止共享 `productflow` down。
- 排他写入：本文件、父章程 R6/D4 条目、`release/README.md`、必要时 pack 产出路径备注；不改 Skill/grader。

## 只改这些文件

- `docs/audits/tasks/archive/release-r6-pin-and-formal-d4.md`（本文件）
- `docs/audits/performance-governance.md`（R6 / D4；**不得**标 R6 通过除非证据齐）
- `release/README.md`（一句 pin/D4 备注）
- 若 pack 脚本发现合同缺口：最窄修复 `scripts/release-*.sh` / `release/` 模板（须列入证据）

## 不要碰

- 把 R6 标为通过而缺 pin=G-07 或正式 D4；用 B5 等价 retag 冒充冻结对；Skill/grader；共享 `productflow` down。

## 现在代码在哪

- G-07 PASS 证据：[release-r6-clean-candidate-gate](release-r6-clean-candidate-gate.md)
- B5 等价夹具（≠正式 D4）：[release-n-to-n1-upgrade](release-n-to-n1-upgrade.md)
- 脚本：`scripts/release-build-images.sh` / `release-pack.sh` / `release-upgrade.sh`
- 父章程：[performance-governance.md](../../performance-governance.md) D4/B6/R6

## 合同

- 总纲 R6；完成可 FAIL；缺 pin 对齐或正式 D4 则不得关闭 R6。
- 正式 D4 ≠ B5 等价 retag。

## 怎么验收

- 新 pin 的 `VERSION` / `images.env` / 包内 sha 与交付 commit 一致（或诚实映射表）
- 隔离 D4：升级主路径四项 health；`UPGRADE_SIMULATE_MIGRATE_FAIL` fail-stop + 钉回 N
- 父章程结论与证据链接一致；未宣称 SLA/RPO/RTO

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已归档；交付随本任务提交。跟进见 [release-r6-resource-budget](../release-r6-resource-budget.md)。

## 证据

### 窗口与资源（2026-09-07）

- 工作树：`codex/development`；`git rev-parse HEAD` = `67b0f3092158d72c5e6e118761808fe5f48ef5a1`（与冻结 SHA 一致）。
- 认领脏文件仅协调文档；build/pack 用该 HEAD SHA，未改被测运行时。
- 产物目录：`/tmp/pf-r6-d4-20260907/`（`evidence/`、`packs/`、`backups/`、`n/`、`n1/`）。
- 隔离项目：`pf-r6-d4-n-20260907`（in-place N→N+1；结束后 `down -v`）。
- 宿主端口：`APP_HOST_PORT=49480`、`WEB_PORT=49481`；叠用 `docker-compose.prod-ports.yml`。
- 共享项目：全程仅 `productflow` 的 postgres/redis；未 `down` / `down -v` / `just dev-stop`。

### Pin 映射表

| 角色 | IMAGE_TAG | GIT_SHA | 镜像 ID（本地） |
|---|---|---|---|
| N（旧） | `0.0.0-5ed2b916b569` | （既有发行 pin） | go `5771257ad5c7…` / agent `b9ce68ecc499…` / web `d9347a90cf09…` |
| N+1（新） | `0.0.0-67b0f3092158` | `67b0f3092158d72c5e6e118761808fe5f48ef5a1` | go `963f67e9e16b…` / agent `a620e77da59f…` / web `58ebc8f5d68a…` |

- 包路径：`/tmp/pf-r6-d4-20260907/packs/productflow-0.0.0-67b0f3092158/`（含 `VERSION` / `images.env` / `SHA256SUMS` / `scripts/release-upgrade.sh`；`RELEASE_PACK_SAVE_IMAGES=1`）。
- **非**同镜像等价 retag：N 与 N+1 三组件 image ID 均不同（见 `evidence/image-id-map.txt`）。

### Build / pack

| 步骤 | 命令 / 结果 |
|---|---|
| `release-build-images`（首次） | go **PASS** → `productflow-go:0.0.0-67b0f3092158`；agent/web **FAIL**：Docker 内访问 `registry.npmjs.org`（Cloudflare `104.16.*:443`）`ETIMEDOUT` / IPv6 `ENETUNREACH`（日志 `evidence/release-build-images.log`）。**不得**写成「默认脚本一次全量 PASS」。 |
| agent/web 重试 | 宿主 curl npm 可达；`--network=host` 仍超时。临时 Dockerfile（仅证据目录，**未入库**）注入 `COREPACK_NPM_REGISTRY`/`npm_config_registry=https://registry.npmmirror.com` 后 agent+web **PASS**（`evidence/release-build-images-retry-npmmirror.log`）。源码身份仍为冻结 SHA。 |
| `release-pack` | `RELEASE_PACK_OUT=/tmp/pf-r6-d4-20260907/packs RELEASE_PACK_SAVE_IMAGES=1` → exit 0；`VERSION`/`images.env` 中 `PRODUCTFLOW_GIT_SHA`/`IMAGE_TAG` 与上表一致。 |

脚本合同缺口：**未改** `scripts/release-*.sh` / 仓库内 Dockerfile；构建机对 npmjs TLS 失败已诚实记录，并用镜像 registry 完成同 SHA 构建以便 D4。

### 正式 D4（隔离）

脚本：仓库 `scripts/release-upgrade.sh`（旧 N 发行包 `dist/release/productflow-0.0.0-5ed2b916b569` **无** B3/B5 `scripts/`，演练时从 N+1 包拷入 N 安装目录以便备份/升级工具链；升级逻辑未改）。

| 步骤 | exit | 结果 |
|---|---|---|
| N 栈 up + 四项 health | 0 | PASS（`smoke-n-baseline.txt`） |
| 拒绝 `COMPOSE_PROJECT_NAME=productflow` | 1 | PASS（`refuse-shared.log`） |
| `UPGRADE_SIMULATE_MIGRATE_FAIL=1` | 1 | PASS：`UPGRADE STOPPED`；`images.env` 钉回 `0.0.0-5ed2b916b569`；PG 探针 `pre-upgrade` 仍在（`d4-fail-stop.log`） |
| 主路径 `release-upgrade.sh` | 0 | PASS：`0.0.0-5ed2b916b569` → `0.0.0-67b0f3092158`；migrate 0；四项 health；PG + Agent `/data/sessions/r6-d4-probe.txt` 保留（`d4-main-path.log` / `smoke-n1-after-upgrade.txt`） |

### 协调者复验（抽样）

| 检查 | 结果 |
|---|---|
| 包内 `VERSION` / `PRODUCTFLOW_GIT_SHA` | `67b0f3092158d72c5e6e118761808fe5f48ef5a1` / tag `0.0.0-67b0f3092158` |
| `image-id-map.txt` N≠N+1 三组件 | 一致 |
| `smoke-n1-after-upgrade.txt` 四项 health | PASS |
| `d4-fail-stop.log` migrate fail-stop + 钉回 N | PASS |
| 本地 docker images 两 pin 并存 | 确认 |

### 缺口（诚实）

1. **资源预算↔部署规模：本窗未测** → 不得因此关闭总纲 R6。
2. 默认 `release-build-images` 一次跑通 agent/web 在本机构失败（npmjs TLS）；全量声明须带上述网络备注。
3. 旧 N 发行包缺 upgrade 脚本目录（B2 时代产物）；正式安装应以含 B5 脚本的包为准。
4. 无 registry RepoDigest；本地 tag + docker save。
5. 在途 lease / unknown 未挂夹具；商品 multipart 创建未作为本窗主断言。
6. **≠ 总纲 R6 通过**；不写 RPO/RTO/SLA。

### 父章程结论

- 发行 pin **已对齐** G-07 交付 commit：`0.0.0-67b0f3092158` ≡ `67b0f309…`。
- 正式 D4（非等价 retag）主路径 + migrate fail-stop：**PASS**。
- **总纲 R6 仍未通过**（至少缺资源预算）。

### 未宣称

- 未宣称 R6 通过；未写容量/RPO/RTO/SLA。
- 未 push / reset。

### 审核

- 审核者：CTO（本会话）；结论：证据任务**完成**；pin 对齐 + 正式 D4 PASS；业务门槛 **R6 未通过**。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/release-r6-pin-and-formal-d4.md` 查询）
- Issue 结果 / 业务门槛结果 / 剩余缺口：完成；**R6 未通过**；剩余＝资源预算↔部署规模（及章程其余未齐项）。
