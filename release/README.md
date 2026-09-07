# ProductFlow 发行物安装（无 git 工作树）

自营与自托管使用**同一套**发行物：版本化镜像 tag、锁定 Compose、`.env` 样例与 `VERSION` / `images.env`。本目录是源码树内的安装包模板；发布者用 `scripts/release-pack.sh` 打出 `dist/release/productflow-<tag>/`（及 `.tar.gz`）交给空主机。

本路径覆盖总纲「可安装」方向与演练合同 **D1**（干净安装步骤）。**不**包含备份/恢复（B3）、稳定版 N→N+1（B5），也**不**宣称 **R6** 已通过。

## 镜像 tag 约定

| 字段 | 含义 |
|---|---|
| `PRODUCTFLOW_VERSION` | 人类可读版本；首个稳定版前默认 `0.0.0` |
| `PRODUCTFLOW_GIT_SHA` | 完整 git commit（构建时解析） |
| `PRODUCTFLOW_IMAGE_TAG` | **不可变**安装 pin：`<VERSION>-<sha12>`，例如 `0.0.0-4309ebe4aabc` |
| `PRODUCTFLOW_GO_IMAGE` | `${REGISTRY}productflow-go:<IMAGE_TAG>` |
| `PRODUCTFLOW_AGENT_IMAGE` | `${REGISTRY}productflow-agent:<IMAGE_TAG>` |
| `PRODUCTFLOW_WEB_IMAGE` | `${REGISTRY}productflow-web:<IMAGE_TAG>` |

- 安装包只 pin 上述不可变 tag，不要用浮动的 `latest` 作为支持口径。
- `PRODUCTFLOW_REGISTRY` / `REGISTRY` 可选前缀（需自带尾部 `/`，脚本也会补上），例如 `localhost:5000/` 或 `ghcr.io/org/`。
- 上游数据面保持 `postgres:16`、`redis:7`（由 `images.env` 写出，可按需改 pin）。

构建 / 推送 / 打包（在**有源码**的构建机上）：

```bash
bash scripts/release-build-images.sh
# 可选：本地 registry 或远端仓库
PRODUCTFLOW_REGISTRY=localhost:5000/ bash scripts/release-push-images.sh
bash scripts/release-pack.sh
# 离线主机：把镜像打进包内
RELEASE_PACK_SAVE_IMAGES=1 bash scripts/release-pack.sh
```

`just release-build-images` / `just release-pack` 为同上入口。开发机日常 `just release`（`scripts/release.sh`）仍是「当前工作树 compose up --build + 探活」，**不是**空主机发行物安装。

## 空主机安装（D1 方向）

前置：Docker Engine + Compose 插件；发行目录或 `.tar.gz`；**没有** ProductFlow git checkout 也可。

1. 解包到空目录，例如 `/opt/productflow`：

```bash
tar -xzf productflow-<IMAGE_TAG>.tar.gz
cd productflow-<IMAGE_TAG>
```

2. 准备密钥（不要提交真实 `.env`）：

```bash
cp .env.example .env
# 至少填写 ADMIN_ACCESS_KEY、SETTINGS_ACCESS_TOKEN、SESSION_SECRET、
# POSTGRES_PASSWORD、AGENT_SERVICE_INTERNAL_TOKEN（≥32 字符）
```

3. 取得镜像（三选一）：

- **Registry**：已 push 时，在能拉取的主机上直接 `docker compose ... pull`（见下）。
- **离线 tar**：若包内有 `images/*.tar`，先 `docker load -i images/productflow-images-<IMAGE_TAG>.tar`。
- **构建机 retag**：仅排障用；正式交付应提供 registry 或 save 档案。

4. 校验并启动（生产式端口：不发布 PG/Redis/metrics 到宿主）：

```bash
docker compose --env-file images.env --env-file .env \
  -f docker-compose.yml -f docker-compose.prod-ports.yml config --quiet

docker compose --env-file images.env --env-file .env \
  -f docker-compose.yml -f docker-compose.prod-ports.yml up -d
```

5. 探活（与 `scripts/release.sh` 同类；按 `.env` 中 `APP_HOST_PORT` / `WEB_PORT` 调整）：

```bash
curl -fsS "http://127.0.0.1:${APP_HOST_PORT:-29280}/healthz"
curl -fsS "http://127.0.0.1:${WEB_PORT:-29281}/healthz"
curl -fsS "http://127.0.0.1:${WEB_PORT:-29281}/api/healthz"
docker compose --env-file images.env --env-file .env \
  -f docker-compose.yml -f docker-compose.prod-ports.yml \
  exec -T productflow-agent-service \
  node -e 'fetch("http://127.0.0.1:29284/healthz").then(r=>r.text()).then(console.log)'
```

6. 用 `ADMIN_ACCESS_KEY` 登录；用 `SETTINGS_ACCESS_TOKEN` 解锁设置页。未配置 provider 时应仍可管理，生成入口表现为不可用（总纲「可配置」；完整缺凭据演练证据另记）。

停止栈且**保留**卷：`docker compose ... down`。清空演示数据才加 `-v`。不要对共享开发项目名 `productflow` 执行 `down`，除非该主机上没有其他人的开发栈。

## 包内文件

| 文件 | 作用 |
|---|---|
| `VERSION` | 发行身份（version、git sha、镜像名） |
| `images.env` | Compose 镜像变量 pin（由 `VERSION` 抽出） |
| `docker-compose.yml` | 仅 `image:`，无 `build:` |
| `docker-compose.prod-ports.yml` | 生产式端口 overlay |
| `.env.example` | 启动密钥与端口样例（无秘密） |
| `SHA256SUMS` | 包内文本文件校验 |
| `images/*.tar` | 可选；`RELEASE_PACK_SAVE_IMAGES=1` 时生成 |

## 构建环境限制

若构建机访问 `registry.npmjs.org`、`proxy.golang.org` 或目标镜像仓库 TLS/网络失败，`release-build-images` / `release-push-images` 会失败。应如实记录，**不得**把「用旧镜像 retag + 包结构校验」写成「当前 HEAD 全量 `docker compose build` / registry push 已通」。空主机 D1 实跑可与本发行物同隔离窗口预约，或另开证据单；未完成实跑前不关闭 R6。

## 明确不做

- B3 备份/恢复脚本与一致点自动化
- B5 稳定版 N→N+1 升级包
- 容量 SLA、RPO/RTO、R6 总项通过声明
