# 任务：修正 Compose Web 上游并提供生产端口 overlay

状态：完成
类型：实现
认领者：sub-release/release-compose-proxy-overlay
认领于：2026-09-07T12:02:00+08:00
完成于：2026-09-07T12:11:00+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：B2 版本化发行物；不在本任务宣称 R6 或空主机完整安装通过

按 [Issue 协议](../README.md) 认领并确认所有权后调查与实现。承接 [发行基线](release-readiness-baseline.md) 批次 B1。协调者审核：2026-09-07 CTO 通过——nginx 上游与 overlay 正确；隔离探活充分；「非 HEAD 全量 build」缺口已诚实记录，不阻塞 B1 合同，留给 B2/R6。

## 问题来源

基线核实：`web/nginx.conf` 将 `/api/` 与 `/healthz` 代理到 `productflow-backend:29280`，而 `docker-compose.yml` 服务名为 `productflow-go-api`，无 network alias → 容器内 API 反代按 DNS 会失败。开发 Compose 默认映射 PG/Redis/metrics 宿主端口，与总纲「不按开发设置直接暴露公网」不一致。

## 做成什么样

1. Web 容器内 `/api/healthz`（及 `/api/`）可到达 Go API：修正 nginx 上游名，或在 Compose 为 API 增加等价 alias；二者择一，须与文档/`release.sh` 探活一致。
2. 提供**可选**生产端口 overlay（或等价 compose 片段）：默认不把 PG、Redis、dispatcher/worker metrics 映射到公网可达宿主端口；开发用 `docker-compose.yml` 行为可保留，但文档写清生产应如何叠用 overlay。
3. 在隔离 compose 项目验证四项 health（含经 web 的 API health），不改动共享开发站正在使用的容器。

## 前置与并行

- 前置：`release-readiness-baseline` 已归档；父章程 B1 合同。
- 冻结输入：认领时 HEAD；不扩展为 B2 发行物或备份脚本。
- 运行资源：仅隔离 `docker compose -p <独立名>`；禁止 `just dev-stop`、禁止对共享 `productflow` 项目 `down`/改卷；不读生产 `.env` 秘密写入仓库。
- 排他写入：`web/nginx.conf`、相关 compose/overlay、README 安装说明中与本修复直接相关的段落、本文件与父章程 B1 结论。

## 只改这些文件

- `web/nginx.conf`（和/或 compose 中 API 的 `networks` alias）
- `docker-compose*.yml` 或新增 `docker-compose.prod-ports.yml`（名称以调查为准）
- 根 `README.md` / 部署说明中与上游名、生产端口叠用直接相关的最小修订
- `docs/audits/performance-governance.md` B1 结论行
- 本文件

## 不要碰

业务 Go/Web 应用逻辑、schema、评测、provider 密钥、备份/升级脚本（B3+）、共享开发栈数据卷。

## 现在代码在哪

`web/nginx.conf` 现 `proxy_pass http://productflow-go-api:29280/...`；`docker-compose.prod-ports.yml` 用 `ports: !override []` 去掉 PG/Redis/metrics 宿主映射；`scripts/release.sh` 四项 health 仍适用。

## 合同

- 修正后同一发行物路径上 web→api 可达；自营与自托管不靠特例服务名。
- 生产 overlay 默认不暴露 PG/Redis/metrics；Operator 仍可用开发 compose 做本机调试。
- 隔离验证通过不等于 R6；不写虚假容量或 SLA。

## 怎么验收

1. `docker compose config`（含 overlay）无错误上游引用。
2. 隔离项目 up 后：`release.sh` 同类四项探活，且经 web 访问 `/api/healthz` 成功。
3. 生产 overlay 下宿主未映射 PG/Redis/metrics（或仅绑 127.0.0.1——若选此策略须在证据写明）。
4. `just docs-check`；相关 diff `--check`。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者审核 / 归档提交。
- 交接：实现与隔离探活已完成；等待协调者审核。未 git commit；未改看板 README。隔离项目已 `down -v`。

## 证据

- 修改文件：`web/nginx.conf`、`docker-compose.prod-ports.yml`（新增）、`README.md`、`docs/audits/performance-governance.md`、本文件。
- 隔离项目名：`pf-b1-proxy-20260907`。
- Overlay 策略：**不发布** PG(`5432`)、Redis(`6379`)、dispatcher metrics(`29285`)、worker metrics(`29286`) 到宿主（非 127.0.0.1 绑定）。Web/API 仍发布（验证用 `WEB_PORT=39281`、`APP_HOST_PORT=39280`）。
- 探活（与 `scripts/release.sh` 同类）：
  - `backend /healthz` → `http://127.0.0.1:39280/healthz` → `{"status":"ok"}`
  - `agent service /healthz` → `docker compose exec … productflow-agent-service` → `"runtime":"productflow-pi"`
  - `web /healthz` → `http://127.0.0.1:39281/healthz` → `ok`
  - `web proxy /api/healthz` → `http://127.0.0.1:39281/api/healthz` → `{"status":"ok"}`
- `docker compose … config`：含 overlay 通过；渲染结果 PG/Redis/worker/dispatcher `ports: None`。
- 镜像说明：Docker 构建期访问 registry.npmjs.org / proxy.golang.org TLS 超时；验证用宿主 `pnpm` 构建的 web dist + nginx 运行时镜像、既有 `productflow-go-cgo-check:local`（Go）与 `productflow-agent-audit:c87d8aca`（Agent）打标签后 `--no-build` 启动。nginx 配置与仓库 `web/nginx.conf` 一致（容器内已 grep `productflow-go-api`）。不构成「当前 HEAD 全量 `docker compose build`」证据。
- 共享 `productflow` 项目（仅 PG/Redis）全程未 `down`/改卷；验证后隔离项目 `down -v` 并删除临时 `/tmp` env。
- 自审：未改业务逻辑/schema/评测；未提交 git；未改看板 README；未宣称 R6/B2；未把秘密写入仓库。
- 交付定位：随本任务提交（协调者审核后）。

- 审核者：CTO（本会话）；结论：通过，可拆 B2。交付 commit 待用户授权。
