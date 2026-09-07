# 任务：备份恢复脚本与一致点 runbook（B3）

状态：完成
类型：实现
认领者：sub-release/release-backup-restore
认领于：2026-09-07T12:37:30+08:00
完成于：2026-09-07T12:48:00+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：B4 D3 恢复演练证据；不宣称 R6

按 [Issue 协议](../README.md) 认领。承接 [发行基线](release-readiness-baseline.md) **B3**；前置 [B2 发行物](release-versioned-artifact.md) 已交付。协调者审核：2026-09-07 CTO 通过——备份/恢复脚本与隔离数据面冒烟成立；未宣称 D3 全栈/R6。

## 问题来源

总纲要求可恢复：数据库、媒体、Pi 持久材料与可启动配置须一致备份。当前有机制描述，无官方 dump/卷打包、校验清单与恢复 runbook 脚本。

## 做成什么样

1. 备份/恢复脚本（或明确 compose 步骤脚本）覆盖：PostgreSQL、storage 媒体、agent `/data`、部署 `.env`；可选 Redis；记录 commit/digest 与是否排空在途作业。
2. Runbook：一致点建立、恢复到新目录/实例的步骤与业务断言清单（对齐 D2）。
3. 不在本任务宣称完整 D3 隔离实跑通过（可留给 B4）；不写虚假 RPO/RTO/SLA。

## 前置与并行

- 前置：B2 归档；`release/` 安装包可用。
- 运行资源：隔离卷/项目；禁止共享 `productflow` down；不读真实生产秘密入库。
- 排他写入：备份/恢复脚本、`release/` 或 docs 中 runbook、父章程 B3、本文件。勿改 auth/product/delivery 业务实现。

## 只改这些文件

- `scripts/release_backup_common.sh`、`scripts/release-backup.sh`、`scripts/release-restore.sh`
- `scripts/release-pack.sh`（发行包装入备份脚本）
- `release/README.md`（备份/恢复 runbook）
- `justfile`（`release-backup` / `release-restore`）
- 根 `README.md`（发行节指针）
- `docs/audits/performance-governance.md`（B3 结论）
- 本文件

## 不要碰

业务 schema、评测、共享开发数据、密钥明文入库、B5 升级合同。

## 合同

- 备份对象不能只有数据库。
- 自营与自托管同一工具链。
- 完成 ≠ R6 / ≠ D3 全项实跑。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核 / 归档提交。
- 交接：实现与隔离冒烟已完成；等待协调者审核。未 git commit；未改看板 README。隔离项目已 `down -v`。未宣称 D3/R6。

## 证据

- 修改/新增：`scripts/release_backup_common.sh`、`release-backup.sh`、`release-restore.sh`；`release-pack.sh` 带入包内 `scripts/`；`release/README.md` 备份节；`justfile`；根 `README.md`；`docs/audits/performance-governance.md`；本文件。
- 备份对象：`pg_dump -Fc`、storage tar、agent `/data` tar、`.env`（0600）；可选 Redis / agent traces。`MANIFEST` 含 `OBJECTS`、`PRODUCTFLOW_GIT_SHA` / image digest（可解析时）、`CONSISTENCY_MODE`、`JOBS_DRAINED`。
- 一致点：`drain` 仅 `stop` worker/dispatcher/Agent（非 `down`/`down -v`）；`crash` 热备并记录风险。
- 安全：默认拒绝 Compose 项目名 `productflow`（需 `PRODUCTFLOW_ALLOW_SHARED_PROJECT=1`）；秘密只写备份目录，不入库。
- 隔离冒烟：`pf-b3-src-20260907` → 备份 → `pf-b3-dst-20260907` 恢复（`SKIP_COMPOSE_UP=1`）；探针表 `b3_probe`、媒体 `media/b3/probe.bin`、Pi `sessions/b3-probe.txt`、`.env` 均还原；`sha256sum -c CHECKSUMS` 通过；拒绝共享项目名已测。共享 `productflow`（PG/Redis）全程未 down；验证后隔离项目 `down -v`，删除 `/tmp` 工作目录。
- **非宣称**：未做完整 D3 业务断言（登录/媒体 HTTP/provider/Agent health 全栈）；未跑 R6；未写 RPO/RTO/SLA。全栈 `compose up` 恢复留给 B4（本冒烟用 stub 镜像仅验证数据面）。
- 验证：`bash -n` 脚本；`RELEASE_PACK_OUT=… release-pack` 含备份脚本与 README 节；`just docs-check`；相关路径 `git diff --check`。
- 自审：未改 auth/product/delivery/schema/评测；未提交 git；未改看板 README；秘密未入库。
- 交付定位：随本任务提交（协调者审核后）。

- 审核者：CTO（本会话）；结论：通过。可拆 B4 D3 恢复演练。
