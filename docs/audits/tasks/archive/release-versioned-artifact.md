# 任务：版本化发行物与锁定安装包

状态：完成
类型：实现
认领者：sub-release/release-versioned-artifact
认领于：2026-09-07T12:29:00+08:00
完成于：2026-09-07T12:37:00+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：B3 备份/恢复脚本；空主机 D1 实跑可与本任务分证据或同隔离窗口预约

按 [Issue 协议](../README.md) 认领。承接 [发行基线](release-readiness-baseline.md) **B2**；前置 [B1 上游/端口](release-compose-proxy-overlay.md) 已交付。协调者审核：2026-09-07 CTO 通过——安装包路径与隔离探活成立；HEAD 全量 build/push 缺口已诚实记录，不阻塞 B2 合同。

## 问题来源

B1 修好 web→API，但仍依赖 git checkout + 本地 build。总纲要求固定版本镜像与配置样例，空主机按发行物安装，不依赖作者本地 storage。

## 做成什么样

1. 不可变镜像 tag 约定与构建/推送（或本地 registry）脚本；锁定 compose + env 样例 + 版本文件组成的安装包。
2. 文档：无 git 工作树的空主机用发行物完成 D1 方向安装步骤（可与本任务同窗口隔离实跑，或另发证据单；合同须写清）。
3. 不实现完整备份（B3）或 N→N+1（B5）；不宣称 R6。

## 前置与并行

- 前置：B1 归档；prod-ports overlay 可用。
- 运行资源：隔离 compose/registry；禁止共享 `productflow` down。
- 排他写入：发行脚本、版本文件、compose 锁定片段、README 安装节、父章程 B2、本文件。

## 只改这些文件

- `scripts/release_common.sh`、`scripts/release-build-images.sh`、`scripts/release-push-images.sh`、`scripts/release-pack.sh`
- `release/`（`docker-compose.yml`、`docker-compose.prod-ports.yml`、`.env.example`、`README.md`）
- 根 `README.md` 安装/发行节、`justfile` 入口
- `docs/audits/performance-governance.md` B2 结论
- 本文件

## 不要碰

业务逻辑、schema、评测、共享开发数据卷、密钥入库。

## 合同

- 自营与自托管同一发行物。
- 安装不依赖隐藏作者配置。
- 诚实记录构建环境限制（若 registry TLS 等仍失败，不得伪装 HEAD 全量 build 已通）。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核 / 归档提交。
- 交接：实现与隔离探活已完成；等待协调者审核。未 git commit；未改看板 README。隔离项目已 `down -v`。未实现 B3/R6。

## 证据

- 修改/新增：`scripts/release_common.sh`、`release-build-images.sh`、`release-push-images.sh`、`release-pack.sh`；`release/*`；`README.md`；`justfile`；`docs/audits/performance-governance.md`；本文件。
- Tag 约定：`PRODUCTFLOW_IMAGE_TAG=<VERSION>-<sha12>`（默认 `PRODUCTFLOW_VERSION=0.0.0`）；镜像 `productflow-{go,agent,web}:<tag>`；可选 `PRODUCTFLOW_REGISTRY`/`REGISTRY` 前缀。
- 打包样例：`dist/release/productflow-0.0.0-5ed2b916b569/` + `.tar.gz`（`dist/` gitignore；含 `VERSION`、`images.env`、锁定 compose、prod-ports、`.env.example`、`README.md`、`SHA256SUMS`）。源码基线 sha：`5ed2b916b5698cc10623f5a724acc708e2927a4a`（打包时 HEAD）。
- 隔离项目：`pf-b2-artifact-20260907`；宿主端口 `APP_HOST_PORT=39280`、`WEB_PORT=39281`；叠用包内 prod-ports。
- 探活：
  - backend `/healthz` → `{"status":"ok"}`
  - web `/healthz` → `ok`
  - web proxy `/api/healthz` → `{"status":"ok"}`
  - agent → `"runtime":"productflow-pi"`
- 渲染配置：无 `build:`；product 镜像 pin 到 tag；PG/Redis/metrics **未**发布到宿主（PublishedPort 0 / 空）；Web/API 保留。
- 无 git 路径：将 `.tar.gz` 解到 `/tmp` 空目录后 `docker compose … config --quiet` 通过。
- **构建限制（诚实）**：`timeout 120 bash scripts/release-build-images.sh` 在 `go mod download` 阶段被取消（网络/registry 拉取未在时限内完成）。隔离探活用既有 `productflow-go-cgo-check:local`、`productflow-agent-audit:c87d8aca` retag，以及宿主 `web/dist` + `nginx:1.27-alpine` 打出的 runtime web 镜像（非当前 HEAD 全量 `docker build` 三镜像）。**不构成**「HEAD 全量 release-build-images / registry push 已通」。`release-push-images` 未对远端/本地 registry 实推（无可用 registry 窗口）；离线 `RELEASE_PACK_SAVE_IMAGES=1` 路径已实现未在本窗口跑 save。
- 共享 `productflow`（仅 PG/Redis）全程未 `down`/改卷；未跑 `just dev-stop`。验证后隔离项目 `down -v`，删除 `/tmp` env。
- 验证：`just docs-check` 通过；相关路径 `git diff --check` 通过。
- 自审：未改业务逻辑/schema/评测/`go/internal/auth`；未提交 git；未改看板 README；未宣称 R6/B3；秘密未入库。
- 交付定位：随本任务提交（协调者审核后）。
- 审核者：CTO（本会话）；结论：通过。可拆 B3。
