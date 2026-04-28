<p align="center">
  <img src="docs/assets/productflow-brand-concept.png" alt="ProductFlow brand concept: product card connected to AI copy and image workflow nodes" width="168">
</p>

# ProductFlow

ProductFlow 是一个面向单人或小团队商家的开源自托管商品素材工作台。它把商品资料、AI 文案、参考图、AI/模板海报、连续生图会话和可视化商品工作流放在同一个私有部署里，目标是让运营者更快地把单个商品整理成可复用的电商素材。

本仓库当前不是多租户 SaaS，也不包含托管服务账号。自托管部署需自行准备 PostgreSQL、Redis、后端、worker 和前端，并配置可用的文本/图片模型供应商。

## 当前功能状态

已实现并在代码中可见的能力：

- 单管理员访问密钥登录，基于 Cookie session 访问后台 API。
- 商品列表、分页浏览、创建商品、商品详情工作台，以及受全局开关保护的商品删除。
![商品创建列表示例图](images/preview1.png)

- 产品内 guided onboarding：顶部导航可随时开始/继续/重置引导，首页展示进度卡片，操作页不占用工作空间。
- ProductFlow workbench：商品详情页以节点画布组织商品资料、参考图、文案和生图流程。
- 画布交互：鼠标滚轮缩放、左键拖动画布空白处平移、节点拖拽定位、节点连线拖拽创建、边删除、右侧面板宽度记忆。
![商品创建工作台示例图](images/preview2.png)
- 商品原图、参考图和连续生图参考图支持点击选择或拖拽上传，带 MIME、大小、像素和数量限制。
- 参考图节点是单图槽位：手动上传或上游生图填充都会替换当前图，旧素材留在商品历史/素材列表中。
- 文案生成、文案编辑、文案确认、历史文案查看；文案节点可编辑标题、卖点、海报主标题和 CTA。
- 生图节点是触发/配置节点，不直接承载图片；生成结果填充到连接的下游参考图节点，并在参考图/图片侧栏里预览和下载。
- 两种海报产出模式：本地 Pillow 模板渲染、远程图片 provider 生成。
- 海报下载、海报重新生成、商品历史时间线，以及右侧 Images 面板聚合可下载素材。
- 独立图片会话：上传参考图、连续生成图片、把生成图挂回商品。
- 提示词配置：设置页可覆盖商品理解、文案生成、工作台生图和连续生图的默认提示词模板。
![商品创建工作台示例图](images/preview3.png)
![商品创建工作台示例图](images/preview4.png)
- 运行时设置页：可在数据库中覆盖 provider、模型、图片尺寸、上传限制、任务重试、业务删除开关等业务配置，secret 不回显。
- 异步任务：Dramatiq + Redis 投递，PostgreSQL 记录状态，启动时恢复未完成任务/工作流。

仍然不在当前范围内：多用户/多租户、团队权限、支付、托管账号体系、自动投放/自动上架、视频生成、生产级容器编排模板。

## 产品入口与文档

- 新手操作：`docs/USER_GUIDE.md`
- 架构说明：`docs/ARCHITECTURE.md`
- 路线图：`docs/ROADMAP.md`
- 品牌资产：`docs/assets/productflow-brand-concept.png`、`docs/assets/productflow-mark.svg`
- Web metadata / favicon 资产：`web/public/productflow-brand-concept.png`、`web/public/productflow-mark.svg`

## 技术栈

- 后端：Python 3.12、FastAPI、SQLAlchemy、Alembic、Dramatiq、Redis、PostgreSQL、Pillow、OpenAI Python SDK。
- 前端：React 19、Vite、TypeScript、React Router、TanStack Query、Tailwind CSS 4。
- 本地开发入口：根目录 `justfile`；无 `just` 时可直接执行下文列出的原始命令。
- 文档：`docs/PRD.md`、`docs/USER_GUIDE.md`、`docs/ARCHITECTURE.md`、`docs/ROADMAP.md`。

## 开源依赖与致谢

ProductFlow 的应用代码之外，也保留了一套面向 AI 协作的项目工作流资产。这里优先感谢**真诚、友善、团结、专业**的 Linuxdo 社区
<p>
  <a href="https://linux.do">
    <img src="https://img.shields.io/badge/LinuxDo-community-1f6feb" alt="LinuxDo">
  </a>
</p>

- [LinuxDo](https://linux.do) 学 ai, 上 L 站!

同时感谢对本仓库结构、开发方式和协作体验影响最大的开源项目。

<p>
  <a href="https://github.com/mindfold-ai/Trellis">
    <img src="https://raw.githubusercontent.com/mindfold-ai/Trellis/main/assets/trellis.png" alt="Trellis" height="32">
  </a>
  &nbsp;
  <a href="https://openai.com/codex/">
    <img src="https://img.shields.io/badge/OpenAI%20Codex-AI%20coding-412991?logo=openai&logoColor=white" alt="OpenAI Codex">
  </a>
  &nbsp;
</p>

- [Trellis](https://github.com/mindfold-ai/Trellis) 为本项目提供任务工作流、规范沉淀和上下文注入约定；仓库保留 `.trellis/workflow.md`、`.trellis/scripts/` 和 `.trellis/spec/`，方便贡献者理解需求、实现、检查和收尾方式。
- [OpenAI Codex](https://openai.com/codex/) / Codex CLI 参与本项目的开发协作流程；仓库中的 `.codex/`、`.agents/skills/` 和 `AGENTS.md` 用于保存面向 AI coding agent 的项目级指令、hooks、技能和子代理配置。

## 仓库结构

```text
ProductFlow/
  README.md
  LICENSE
  CONTRIBUTING.md
  SECURITY.md
  .env.example
  .env.dev.example
  docker-compose.yml
  .dockerignore
  justfile
  scripts/
    release.sh
    with_dev_env.sh
  docs/
    PRD.md
    USER_GUIDE.md
    ARCHITECTURE.md
    ROADMAP.md
    assets/
      productflow-brand-concept.png
      productflow-mark.svg
  backend/
    Dockerfile
    pyproject.toml
    alembic.ini
    alembic/versions/
    src/productflow_backend/
    tests/
  web/
    Dockerfile
    nginx.conf
    package.json
    public/
      productflow-brand-concept.png
      productflow-mark.svg
    src/
  .trellis/
    workflow.md
    scripts/
    spec/
```

`.trellis/spec/`、`.trellis/workflow.md` 和 `.trellis/scripts/` 是项目开发规范和任务工具，保留在仓库中便于贡献者理解约定；`.trellis/tasks/` 和 `.trellis/workspace/` 是本地任务/开发者运行上下文，不应公开跟踪。

## 快速开始：Docker Compose 一键自托管

该路径面向单机自托管部署。默认配置可运行基础流程；配置真实模型供应商、持久化存储和反向代理/HTTPS 后，可作为小规模生产运行的基础方式。宿主机仅需 Docker / Docker Compose，无需安装 Python、`uv`、Node、`pnpm` 或 `just`。Compose 会构建并启动 PostgreSQL、Redis、后端 API、Dramatiq worker 和 Web 静态站点。

### 1. 复制并修改环境变量

```bash
cp .env.example .env
```

至少修改以下值：

- `ADMIN_ACCESS_KEY`：登录后台使用的管理员密钥。
- `SETTINGS_ACCESS_TOKEN`：配置页二次解锁令牌，必须与登录密钥分开。
- `SESSION_SECRET`：签名 session cookie 的长随机字符串。
- `POSTGRES_PASSWORD`：PostgreSQL 密码；Compose 会用它拼出容器内的 `DATABASE_URL`。

默认 provider 为 `mock`，`POSTER_GENERATION_MODE=template`，无需真实模型密钥即可完成创建商品、生成文案和模板海报等基础流程。切换真实模型前，先阅读“模型与供应商配置”。

### 2. 一键构建并启动

```bash
docker compose up -d --build
```

不要在该命令后追加服务名；追加服务名只会启动指定服务。完整自托管栈应一次启动全部服务。

Compose 默认启动：

- PostgreSQL：服务名 `productflow-postgres`，Compose volume `productflow-postgres-data`，宿主机端口 `${POSTGRES_HOST_PORT:-15432}`。
- Redis：服务名 `productflow-redis`，AOF 持久化 Compose volume `productflow-redis-data`，宿主机端口 `${REDIS_HOST_PORT:-16379}`。
- 后端 API：服务名 `productflow-backend`，宿主机端口 `${APP_HOST_PORT:-29280}`。
- Dramatiq worker：服务名 `productflow-worker`，与 API 共享数据库、Redis 和 storage 卷。
- Web：服务名 `productflow-web`，nginx 静态服务，宿主机端口 `${WEB_PORT:-29281}`。

如端口已被占用，可在 `.env` 中修改 `APP_HOST_PORT`、`WEB_PORT`、`POSTGRES_HOST_PORT` 或 `REDIS_HOST_PORT`，再重新执行 `docker compose up -d --build`。容器内部仍通过服务名互联，无需修改应用内的 `DATABASE_URL` / `REDIS_URL`。

容器内应用会使用 Compose 网络服务名连接依赖：

```text
DATABASE_URL=postgresql+psycopg://productflow:<POSTGRES_PASSWORD>@productflow-postgres:5432/productflow
REDIS_URL=redis://productflow-redis:6379/0
STORAGE_ROOT=/app/storage
```

容器运行时 `STORAGE_ROOT` 固定为 `/app/storage`，不要写入宿主机路径。默认上传和生成文件存入 Docker named volume `productflow-storage`，容器重启后数据保留。

从旧 systemd 生产环境迁移到 Compose 时，如已有生产文件目录（例如 `/home/cot/ProductFlow-release/shared/storage`），可在 `.env` 中设置 host-only 变量复用旧文件：

```bash
STORAGE_HOST_PATH=/home/cot/ProductFlow-release/shared/storage
```

`STORAGE_HOST_PATH` 仅用于 Compose bind mount 的宿主机路径；API/worker 容器内仍使用 `STORAGE_ROOT=/app/storage`。留空或不设置时使用 `productflow-storage` named volume。普通更新不要执行 `docker compose down -v`，也不要为切换 storage 挂载删除 Docker volume；如需回到 named volume，移除 `STORAGE_HOST_PATH` 后重新执行 `docker compose up -d`。

### 3. 数据库迁移

`productflow-backend` 启动命令会先执行：

```bash
alembic upgrade head
```

迁移成功后才会启动 `uvicorn`。升级代码后如需手动重跑迁移，执行：

```bash
docker compose run --rm productflow-backend alembic upgrade head
```

### 4. 访问与健康检查

默认端口下可执行：

```bash
docker compose ps
curl http://127.0.0.1:29280/healthz
curl http://127.0.0.1:29281/api/healthz
```

如已在 `.env` 中修改端口，请替换为对应值：

```bash
curl "http://127.0.0.1:<APP_HOST_PORT>/healthz"
curl "http://127.0.0.1:<WEB_PORT>/api/healthz"
```

预期 API 返回：

```json
{"status":"ok"}
```

Web 默认入口：`http://127.0.0.1:29281`（改过端口时使用 `.env` 中的 `WEB_PORT`）。使用 `.env` 中的 `ADMIN_ACCESS_KEY` 登录。Web 镜像提供 Vite build 后的静态资源，nginx 将同源 `/api/*` 请求反向代理到 `productflow-backend:29280`。

### 5. 日志、停止与清理

```bash
docker compose logs -f productflow-backend productflow-worker productflow-web
docker compose down
```

停止服务不会删除数据卷。确认需要清空数据库、Redis 和 storage 时再执行：

```bash
docker compose down -v
```

## 本地开发路径

修改代码并使用热重载开发时，使用本地开发路径。

### 1. 准备工具

需要本机已有：

- Python 3.12+
- `uv`
- Node.js 20+ 或兼容版本
- `pnpm`
- Docker / Docker Compose
- `just`（可选；下文同时列出原始命令）

### 2. 复制环境变量

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
```

`.env.example` 的 `DATABASE_URL` / `REDIS_URL` 面向 Compose 容器网络；本地热重载开发命令会通过 `.env.dev` 使用宿主机 `localhost:${POSTGRES_HOST_PORT:-15432}` 和 `localhost:${REDIS_HOST_PORT:-16379}`。至少需要把 `.env` / `.env.dev` 中的这些值改成自己的随机值：

- `ADMIN_ACCESS_KEY`：登录后台使用的管理员密钥。
- `SETTINGS_ACCESS_TOKEN`：配置页二次解锁令牌，必须与登录密钥分开。
- `SESSION_SECRET`：签名 session cookie 的长随机字符串。
- `POSTGRES_PASSWORD`：本地 PostgreSQL 密码，同时保持 `.env.dev` 的 `DATABASE_URL` 中密码一致。

`.env.dev.example` 使用开发端口、Redis DB 1 和 `backend/storage-dev`，数据库名与默认 `docker-compose.yml` 保持一致；使用单独的开发数据库时，先在 PostgreSQL 中创建对应数据库，再调整 `.env.dev` 的 `DATABASE_URL`。本地开发的 storage 与生产 Compose 隔离：`just backend-run` / `just backend-worker` 及对应原始命令会读取 `.env.dev` 中的 `STORAGE_ROOT=./backend/storage-dev`，不要通过 `source .env` 或把生产 `STORAGE_HOST_PATH` 导入 shell 来启动开发进程。

### 3. 仅启动开发依赖

本地热重载开发只用 Compose 启动 PostgreSQL 和 Redis；API、worker 和 Web 由下一步的本机命令启动。完整自托管栈使用上文的 `docker compose up -d --build`。

```bash
docker compose up -d productflow-postgres productflow-redis
```

### 4. 安装依赖并迁移数据库

使用 `just`：

```bash
just backend-install
just web-install
just backend-migrate
```

无 `just` 时：

```bash
uv sync --directory backend --extra dev
pnpm --dir web install
bash scripts/with_dev_env.sh uv run --directory backend alembic upgrade head
```

### 5. 启动后端、worker 和前端

开三个终端分别运行。使用 `just`：

```bash
just backend-run
just backend-worker
just web-dev
```

无 `just` 时：

```bash
bash scripts/with_dev_env.sh bash -lc 'uv run --directory backend uvicorn productflow_backend.main:app --reload --host 0.0.0.0 --port "${APP_PORT:-29282}"'
bash scripts/with_dev_env.sh uv run --directory backend dramatiq --processes 2 --threads 4 productflow_backend.workers
bash scripts/with_dev_env.sh bash -lc 'web_port="${WEB_PORT:-29283}"; api_target="${VITE_DEV_PROXY_TARGET:-http://127.0.0.1:${APP_PORT:-29282}}"; VITE_API_BASE_URL= VITE_DEV_PROXY_TARGET="$api_target" pnpm --dir web dev -- --host 0.0.0.0 --port "$web_port" --strictPort'
```

默认开发端口来自 `.env.dev.example`：

- API：`http://localhost:29282`
- Web：`http://localhost:29283`

打开 Web 页面后，使用 `ADMIN_ACCESS_KEY` 登录。登录后可先点顶部导航右侧的 **开始引导**，按产品内提示创建商品、补资料、生成文案和图片。

### 6. 开发健康检查

```bash
curl http://127.0.0.1:29282/healthz
```

预期返回：

```json
{"status":"ok"}
```

## 模型与供应商配置

ProductFlow 把文本和图片能力分开配置。基础设施配置（数据库、Redis、session、管理员密钥）仍然只从环境变量读取；业务配置可在前端 `/settings` 页面写入数据库并覆盖环境变量默认值。

业务整删默认关闭：`DELETION_ENABLED=false` 时商品删除和连续生图会话删除 API 会返回 403，以便体验站保留违规内容溯源证据。工作流节点/连线编辑和参考图删除不受该开关影响。需要清理整条商品或会话数据时，管理员可在 `/settings` 显式开启“启用业务删除”，或通过环境默认值开启。

文本 provider：

- `TEXT_PROVIDER_KIND=mock`：本地假实现，适合开发和测试。
- `TEXT_PROVIDER_KIND=openai`：OpenAI Responses 兼容接口。
- 相关变量：`TEXT_API_KEY`、`TEXT_BASE_URL`、`TEXT_BRIEF_MODEL`、`TEXT_COPY_MODEL`。

图片 provider：

- `IMAGE_PROVIDER_KIND=mock`：本地假图实现。
- `IMAGE_PROVIDER_KIND=openai_responses`：OpenAI Responses `image_generation` 工具，支持参考图输入和 `previous_response_id` 连续上下文。
- 相关变量：`IMAGE_API_KEY`、`IMAGE_BASE_URL`、`IMAGE_GENERATE_MODEL`、`IMAGE_MAIN_IMAGE_SIZE`、`IMAGE_PROMO_POSTER_SIZE`、`IMAGE_ALLOWED_SIZES`。

海报模式：

- `POSTER_GENERATION_MODE=template`：用本地模板/Pillow 渲染，不调用图片模型。
- `POSTER_GENERATION_MODE=generated`：把确认版文案和商品/参考图交给图片 provider 生成海报。

提示词模板：

- `/settings` 的提示词分组可覆盖商品理解、文案生成、工作台生图和连续生图模板。
- 单次需求建议写在文案/生图节点里；长期统一口吻或格式再改配置页模板。

## 常用命令

| 目的 | 使用 `just` | 无 `just` 时执行 |
|---|---|---|
| 安装后端依赖 | `just backend-install` | `uv sync --directory backend --extra dev` |
| 安装前端依赖 | `just web-install` | `pnpm --dir web install` |
| 应用开发库迁移 | `just backend-migrate` | `bash scripts/with_dev_env.sh uv run --directory backend alembic upgrade head` |
| 启动开发 API | `just backend-run` | `bash scripts/with_dev_env.sh bash -lc 'uv run --directory backend uvicorn productflow_backend.main:app --reload --host 0.0.0.0 --port "${APP_PORT:-29282}"'` |
| 启动 Dramatiq worker | `just backend-worker` | `bash scripts/with_dev_env.sh uv run --directory backend dramatiq --processes 2 --threads 4 productflow_backend.workers` |
| 运行 backend pytest | `just backend-test` | `uv run --directory backend pytest` |
| 启动 Vite 开发服务器 | `just web-dev` | `bash scripts/with_dev_env.sh bash -lc 'web_port="${WEB_PORT:-29283}"; api_target="${VITE_DEV_PROXY_TARGET:-http://127.0.0.1:${APP_PORT:-29282}}"; VITE_API_BASE_URL= VITE_DEV_PROXY_TARGET="$api_target" pnpm --dir web dev -- --host 0.0.0.0 --port "$web_port" --strictPort'` |
| TypeScript 检查 + Vite build | `just web-build` | `pnpm --dir web build` |
| 发布 dry run | `just release-dry-run` | `DRY_RUN=1 bash scripts/release.sh` |
| 生产更新 | `just release` | `bash scripts/release.sh` |

`just release` / `bash scripts/release.sh` 是 Docker Compose 生产更新入口：先执行 `docker compose config --quiet`，再尝试停止可能占用 `29280/29281` 的 legacy user-level systemd 服务 `productflow-backend.service`、`productflow-worker.service`、`productflow-web.service`，随后执行 `docker compose up -d --build --remove-orphans` 并检查 backend `/healthz`、web `/healthz` 和 web 代理 `/api/healthz`。该流程不会删除 Docker volumes；不要用 `docker compose down -v` 做普通更新。复用旧 systemd 生产文件时，先在 `.env` 中设置 `STORAGE_HOST_PATH=/home/cot/ProductFlow-release/shared/storage`；已手动迁走旧服务时，可临时执行 `LEGACY_SYSTEMD_ACTION=skip bash scripts/release.sh`，或使用 `LEGACY_SYSTEMD_ACTION=skip just release`。

`just release-dry-run` / `DRY_RUN=1 bash scripts/release.sh` 只校验 Compose 配置并打印实际发布会执行的步骤；不会停止 systemd 服务、不会构建镜像，也不会启动或切换运行中的服务。

## 主要 API 资源

后端只暴露 REST API。主要入口包括：

- `POST /api/auth/session`、`GET /api/auth/session`、`DELETE /api/auth/session`
- `/api/products`、`/api/products/{product_id}`、`/api/products/{product_id}/history`
- `/api/products/{product_id}/reference-images`、`/api/source-assets/{asset_id}`、`/api/source-assets/{asset_id}/download`
- `/api/products/{product_id}/copy-jobs`、`/api/copy-sets/{copy_set_id}`、`/api/copy-sets/{copy_set_id}/confirm`
- `/api/products/{product_id}/poster-jobs`、`/api/posters/{poster_id}/regenerate`、`/api/posters/{poster_id}/download`
- `/api/image-sessions`、`/api/image-sessions/{image_session_id}`、`/api/image-session-assets/{asset_id}/download`
- `/api/products/{product_id}/workflow`、`/api/products/{product_id}/workflow/run`、`/api/workflow-nodes/{node_id}`、`/api/workflow-edges/{edge_id}`
- `/api/settings`
- `/api/jobs/{job_id}`

## 开源与安全边界

- License：MIT，见 `LICENSE`。
- 贡献指南：见 `CONTRIBUTING.md`。
- 安全报告：见 `SECURITY.md`。
- 不要提交 `.env`、`web/.env`、本地 storage、构建产物、缓存、日志或 `.trellis/tasks/` / `.trellis/workspace/`。
- 真实 provider API key 只应放在本地环境变量或私有部署配置中，不应写入 issue、PR 或文档示例。
