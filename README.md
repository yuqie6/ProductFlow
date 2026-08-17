<p align="center">
  <img src="docs/assets/productflow-brand-concept.png" alt="ProductFlow 商品工作流品牌图" width="168">
</p>

# ProductFlow

[中文](README.md) | [English](README.en.md)

<p align="center">
  <a href="https://draw.devbin.de"><strong>体验站 / Live Demo</strong></a>
</p>

ProductFlow 是面向单商家创作者的开源商品视觉工作台。用户上传真实商品参考图、选择所需图片类型和数量，工作流 Agent 通过对话补齐价格、风格、文字语种、文案要求等信息，再生成可编辑、可运行的图片生产工作流。

当前公网实例是个人项目的 live demo。项目采用单管理员、单商家数据模型；运营方可以按演示政策重置公开体验数据，但已部署实例的升级流程不依赖重置数据库或 storage。多租户、计费、团队权限和正式 SaaS 兼容策略属于后续阶段。

## 当前产品能力

### Agent 创建商品

- `/products/new` 是唯一正式创建入口。
- 用户选择首屏海报、卖点、场景、细节、SKU、尺寸等图片类型；每种类型默认生成 2 张，可独立调整数量。
- 每个商品需要上传 1 至 6 张真实商品参考图，支持 PNG、JPEG 和 WebP。
- Agent 可追问商品价格、生图风格、图片文字语种、文案要求和视觉体系等缺失信息。
- 用户确认 WorkflowDraft 后，页面流式展示物化过程，并平滑进入商品工作台；右侧继续同一段 Agent 对话。

### V2 工作流画布

- 当前节点类型只有 `product_context`、`reference_image`、`prompt_generation`、`image_generation`。
- 画布保留节点拖动、自由添加、连线、删除、多选、缩放、平移、自动布局和快捷键。
- 文件夹用于收纳局部流程，降低大型 DAG 的排线和浏览压力。
- 节点详情可编辑提示词、参考图绑定、视觉体系覆盖、生图比例、分辨率、质量、文字策略和交付规格。
- 生图结果写入对应节点，同时进入该商品的统一图片库。
- 工作流复用只来自用户主动保存的 `WorkflowRecipe` 或局部 `recipe_fragment`。

### 图片库与连续生图

- `/media-library` 是跨商品长期保存的全局素材库，支持搜索、文件夹、标签、归档/恢复和批量组织；工作流子图库通过关联使用全局素材，不复制媒体文件。
- 商品图片库使用类似资源管理器的目录树、文件夹、筛选、排序、重命名、移动、预览、批量选择和下载。
- 每个图片节点只绑定自己的当前承载图；图片库保存商品的全部上传图、工作流生成图和会话转入图。
- 商品封面从图片资产自动选择，仍可通过当前封面 API 显式调整。
- `/image-chat` 支持参考图、分支基图、多候选生成、取消、重试、下载和保存到商品图片库。
- `/gallery` 保存用户主动收藏的连续生图结果。

### Provider 与运行

- `/settings` 管理供应商档案和 `prompt`、`agent`、`image` 三类用途绑定。
- 供应商档案保存 Base URL、API Key、能力和默认模型；secret 不回显。
- 图片用途支持 OpenAI Responses、OpenAI Images 兼容接口和 Google Gemini 图片能力。
- 高级图片参数包括质量、格式、压缩、背景、审核、action、input fidelity 和 partial images；生成数量由业务选择决定。
- Agent Turn 使用独立 Go 服务和内置的 `agent-harness` durable runtime，SSE 支持断线续传。
- 图片生成和交付图任务由 Dramatiq + Redis 执行，PostgreSQL 保存业务状态，storage 保存媒体字节。

## 当前边界

- 单管理员、单商家实例。
- 不提供多租户、团队权限、支付、托管账号、自动上架、广告投放或视频生成。
- 公网体验站数据可能在版本升级时重置。
- Alembic 历史迁移保留用于从空库构建当前 schema；在线运行时只有当前 V2 合同。

## 页面入口

| 路由 | 用途 |
|---|---|
| `/products` | 商品列表 |
| `/products/new` | Agent 创建商品 |
| `/products/:productId` | Agent 对话 + V2 工作流 + 图片库 |
| `/image-chat` | 连续文/图生图 |
| `/media-library` | 全局素材库 |
| `/gallery` | 旧收藏画廊书签兼容重定向 |
| `/history` | V1 历史归档与 Agent 重建 |
| `/settings` | Provider 与运行时配置 |
| `/help` | 产品内帮助 |

仓库文档：

- [文档地图](docs/README.md)
- [产品需求](docs/PRD.md)
- [用户指南](docs/USER_GUIDE.md)
- [架构说明](docs/ARCHITECTURE.md)
- [路线图](docs/ROADMAP.md)
- [版本记录](CHANGELOG.md)

## 技术栈

- 后端：Python 3.12+、FastAPI、SQLAlchemy、Alembic、Dramatiq、Redis、PostgreSQL、Pillow。
- Agent service：Go、同仓 `agent-harness`、SQLite durable journal、Responses API。
- 前端：React 19、Vite、TypeScript、React Router、TanStack Query、XYFlow、Tailwind CSS 4。
- 模型 SDK：OpenAI Python/Go 生态和 Google GenAI。

## 仓库结构

```text
ProductFlow/
  backend/
    alembic/versions/
    src/productflow_backend/
    tests/
  agent-service/
    cmd/productflow-agent-service/
    internal/
    third_party/agent-harness/
  web/
    src/
    public/
  docs/
  scripts/
  CONTEXT.md
  docker-compose.yml
  justfile
```

## Docker Compose 运行

### 1. 准备配置

```bash
cp .env.example .env
```

至少修改：

- `ADMIN_ACCESS_KEY`
- `SETTINGS_ACCESS_TOKEN`
- `SESSION_SECRET`
- `POSTGRES_PASSWORD`
- `AGENT_SERVICE_INTERNAL_TOKEN`

### 2. 启动完整栈

```bash
docker compose up -d --build
```

Compose 包含 PostgreSQL、Redis、FastAPI、Dramatiq worker、Agent service 和 Web。后端容器启动时自动执行 `alembic upgrade head`。

默认地址：

- Web：`http://127.0.0.1:29281`
- Backend health：`http://127.0.0.1:29280/healthz`
- Web proxy health：`http://127.0.0.1:29281/api/healthz`

启动后使用 `ADMIN_ACCESS_KEY` 登录，再用 `SETTINGS_ACCESS_TOKEN` 解锁设置页并配置 `prompt`、`agent`、`image` 用途。

### 3. 数据与日志

```bash
docker compose logs -f productflow-backend productflow-worker productflow-agent-service productflow-web
docker compose down
```

`docker compose down` 保留 volumes。确认需要清空演示数据库、Redis、媒体 storage 和 Agent journal 时执行：

```bash
docker compose down -v
```

设置 `STORAGE_HOST_PATH=/absolute/host/path` 可使用宿主机目录；未设置时使用 `productflow-storage` named volume。

## 本地开发

### 1. 准备工具

- Python 3.12+ 与 `uv`
- Go 1.26.5+
- Node.js 20+ 与 `pnpm`
- Docker / Docker Compose
- `just`（推荐）

### 2. 准备开发配置

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
```

保持 `.env` 和 `.env.dev` 的 PostgreSQL 密码一致，并为管理员、设置页和内部 Agent 服务分别设置不同的随机密钥。

### 3. 安装与迁移

```bash
docker compose up -d productflow-postgres productflow-redis
just backend-install
just web-install
just backend-migrate
```

### 4. 启动四个开发进程

分别在四个终端运行：

```bash
just backend-run
just backend-worker
just agent-service-run
just web-dev
```

默认开发地址：

- API：`http://127.0.0.1:29282`
- Agent service：`http://127.0.0.1:29284`
- Web：`http://127.0.0.1:29283`

## 常用验证

```bash
uv run --directory backend ruff check src tests
just backend-test
pnpm --dir web test:run
pnpm --dir web lint
just web-build
just agent-service-test
```

依赖真实 PostgreSQL/Redis 的恢复测试和真实 provider 测试为显式 opt-in：

```bash
just backend-test-live-recovery
just backend-test-live-delivery-renditions
just backend-test-live-agent-product-intake
just agent-service-test-live
```

## 发布脚本

```bash
just release-dry-run
just release
```

`release-dry-run` 校验 Compose 配置并打印当前发布动作。`release` 执行 `docker compose up -d --build --remove-orphans`，随后检查 backend、Agent service、Web 和 Web API proxy；它不会删除 volumes。

## 主要 API 资源

- `/api/auth/session`
- `/api/v2/agent-product-workspaces`
- `/api/v2/products`
- `/api/v2/products/{product_id}/agent-conversations`
- `/api/v2/products/{product_id}/agent-workbench`
- `/api/v2/products/{product_id}/workflow`
- `/api/v2/workflow-drafts`
- `/api/v2/workflow-recipes`
- `/api/v2/product-image-assets`
- `/api/image-sessions`
- `/api/gallery`
- `/api/settings`

完整合同以 FastAPI OpenAPI 和 `backend/src/productflow_backend/presentation/routes/` 为准。

## 开源与安全

- License：MIT，见 [LICENSE](LICENSE)。
- 贡献指南：[CONTRIBUTING.md](CONTRIBUTING.md)。
- 安全报告：[SECURITY.md](SECURITY.md)。
- 不要提交 `.env`、`web/.env`、storage、构建产物、缓存、日志或迁移证据目录。
- Provider API key 只放在私有配置中。

ProductFlow 在开发过程中使用 OpenAI Codex。感谢 [LinuxDo](https://linux.do) 社区。
