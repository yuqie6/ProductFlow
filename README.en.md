<p align="center">
  <img src="docs/assets/productflow-brand-concept.png" alt="ProductFlow brand concept: product card connected to AI copy and image workflow nodes" width="168">
</p>

# ProductFlow

[中文](README.md) | English

ProductFlow is an open-source, self-hosted product creative workspace for solo merchants and small teams. It brings product information, AI copywriting, reference images, AI/template posters, iterative image sessions, and a visual product workflow into one private deployment so operators can turn a single product into reusable ecommerce assets faster.

This repository is not a multi-tenant SaaS product and does not include hosted service accounts. A self-hosted deployment requires you to provide PostgreSQL, Redis, the backend, the worker, the frontend, and usable text/image model providers.

## Current Feature Status

Implemented and visible in the codebase:

- Single-admin access-key login with Cookie session access to backend APIs.
- Product list, paginated browsing, product creation, product detail workbench, and product deletion protected by a global switch.
![Product creation list example](images/preview1.png)

- In-product guided onboarding: the top navigation can start, continue, or reset the guide at any time; the home page shows a progress card; operation pages keep the workspace clear.
- ProductFlow workbench: the product detail page organizes product information, reference images, copy, and image-generation flow on a node canvas.
- Canvas interactions: mouse-wheel zoom, left-drag panning on blank canvas, node dragging, edge creation by dragging connections, edge deletion, and remembered right-side panel width.
![Product workbench example](images/preview2.png)
- Product source images, reference images, and iterative image-session references support click-to-select and drag-and-drop upload, with MIME, size, pixel, and count limits.
- Reference image nodes are single-image slots: manual upload or upstream image generation replaces the current image, while older assets remain in the product history/asset list.
- Copy generation, copy editing, copy confirmation, and copy history viewing; copy nodes can edit title, selling points, poster headline, and CTA.
- Image-generation nodes are trigger/configuration nodes and do not directly hold images; generated results are written into connected downstream reference image nodes and can be previewed/downloaded from the reference image or Images sidebar.
- Two poster output modes: local Pillow template rendering and remote image-provider generation.
- Poster download, poster regeneration, product history timeline, and a right-side Images panel that aggregates downloadable assets.
- Standalone image sessions: upload reference images, generate images iteratively, and attach generated images back to products.
- Standalone image sessions support durable async generation tasks, queue position, lightweight status refresh, multiple candidates, and a single-column mobile workflow.
- Generated image gallery: iterative image results can be saved to `/gallery` for centralized source, prompt, size, model, and download browsing.
- Prompt configuration: the settings page can override default prompt templates for product understanding, copy generation, workbench image generation, and iterative image generation.
![Product workbench example](images/preview3.png)
![Product workbench example](images/preview4.png)
- Runtime settings page: provider, model, image size, upload limits, job retry, business deletion switch, and other business configuration can be overridden in the database; secrets are not echoed back.
- Async jobs: Dramatiq + Redis for dispatch, PostgreSQL for state, and startup recovery for unfinished product workflows and iterative image-generation tasks.
- Lightweight polling while running: active iterative image generation and product workflows poll status responses only, then refresh full details after completion to reduce frontend rendering and backend serialization load.

Still out of scope: multi-user/multi-tenant support, team permissions, payments, hosted account systems, automatic ad placement/listing, video generation, Kubernetes/Helm/released container images, and other production orchestration packages. The in-repository Docker Compose self-hosting path is available.

## Product Entry Points and Docs

- New user guide: `docs/USER_GUIDE.en.md`
- Architecture guide: `docs/ARCHITECTURE.en.md`
- Current architecture health review: `docs/architecture-health-review.en.md`
- Roadmap: `docs/ROADMAP.en.md`
- Version history: `CHANGELOG.md`
- Brand assets: `docs/assets/productflow-brand-concept.png`, `docs/assets/productflow-mark.svg`
- Web metadata / favicon assets: `web/public/productflow-brand-concept.png`, `web/public/productflow-mark.svg`

## Tech Stack

- Backend: Python 3.12, FastAPI, SQLAlchemy, Alembic, Dramatiq, Redis, PostgreSQL, Pillow, OpenAI Python SDK.
- Frontend: React 19, Vite, TypeScript, React Router, TanStack Query, Tailwind CSS 4.
- Local development entrypoint: root `justfile`; if `just` is unavailable, raw commands are listed below.
- Docs: `docs/PRD.en.md`, `docs/USER_GUIDE.en.md`, `docs/ARCHITECTURE.en.md`, `docs/ROADMAP.en.md`, `CHANGELOG.md`.

## Open Source Dependencies and Thanks

Beyond ProductFlow's application code, this repository keeps a set of project workflow assets for AI-assisted collaboration. Special thanks first to the sincere, kind, united, and professional Linuxdo community.

<p>
  <a href="https://linux.do">
    <img src="https://img.shields.io/badge/LinuxDo-community-1f6feb" alt="LinuxDo">
  </a>
</p>

- [LinuxDo](https://linux.do) 学 ai, 上 L 站!

Thanks also to the open-source projects that most influenced this repository's structure, development approach, and collaboration experience.

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

- [Trellis](https://github.com/mindfold-ai/Trellis) provides task workflow, specification capture, and context-injection conventions for this project. The repository keeps `.trellis/workflow.md`, `.trellis/scripts/`, and `.trellis/spec/` so contributors can understand the requirement, implementation, check, and wrap-up process.
- [OpenAI Codex](https://openai.com/codex/) / Codex CLI participates in this project's development collaboration flow. The repository's `.codex/`, `.agents/skills/`, and `AGENTS.md` store project-level instructions, hooks, skills, and sub-agent configuration for AI coding agents.

## Repository Structure

```text
ProductFlow/
  README.md
  README.en.md
  LICENSE
  CONTRIBUTING.md
  CONTRIBUTING.en.md
  SECURITY.md
  SECURITY.en.md
  CHANGELOG.md
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
    PRD.en.md
    USER_GUIDE.md
    USER_GUIDE.en.md
    ARCHITECTURE.md
    ARCHITECTURE.en.md
    ROADMAP.md
    ROADMAP.en.md
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

`.trellis/spec/`, `.trellis/workflow.md`, and `.trellis/scripts/` are project development specifications and task tools. They stay in the repository so contributors can understand the conventions. `.trellis/tasks/` and `.trellis/workspace/` are local task/developer runtime contexts and should not be publicly tracked.

## Quick Start: One-Command Self-Hosting with Docker Compose

This path is for single-machine self-hosted deployment. The default configuration can run the basic flow. After configuring real model providers, persistent storage, and reverse proxy/HTTPS, it can be used as a foundation for small-scale production. The host only needs Docker / Docker Compose; Python, `uv`, Node, `pnpm`, and `just` are not required. Compose builds and starts PostgreSQL, Redis, the backend API, the Dramatiq worker, and the Web static site.

### 1. Copy and edit environment variables

```bash
cp .env.example .env
```

At minimum, change these values:

- `ADMIN_ACCESS_KEY`: admin key used to log in to the backend UI.
- `SETTINGS_ACCESS_TOKEN`: secondary unlock token for the settings page; it must be different from the login key.
- `SESSION_SECRET`: long random string used to sign session cookies.
- `POSTGRES_PASSWORD`: PostgreSQL password; Compose uses it to build the in-container `DATABASE_URL`.

The default provider is `mock`, and `POSTER_GENERATION_MODE=template`, so you can complete basic flows such as creating products, generating copy, and rendering template posters without real model keys. Read "Model and Provider Configuration" before switching to real models.

### 2. Build and start everything

```bash
docker compose up -d --build
```

Do not append service names to this command; adding a service name starts only that service. The complete self-hosted stack should start all services together.

Compose starts these services by default:

- PostgreSQL: service name `productflow-postgres`, Compose volume `productflow-postgres-data`, host port `${POSTGRES_HOST_PORT:-15432}`.
- Redis: service name `productflow-redis`, AOF persistence volume `productflow-redis-data`, host port `${REDIS_HOST_PORT:-16379}`.
- Backend API: service name `productflow-backend`, host port `${APP_HOST_PORT:-29280}`.
- Dramatiq worker: service name `productflow-worker`, sharing database, Redis, and storage volumes with the API.
- Web: service name `productflow-web`, nginx static service, host port `${WEB_PORT:-29281}`.

If a port is already occupied, edit `APP_HOST_PORT`, `WEB_PORT`, `POSTGRES_HOST_PORT`, or `REDIS_HOST_PORT` in `.env`, then run `docker compose up -d --build` again. Containers still connect to one another through service names, so you do not need to change application `DATABASE_URL` / `REDIS_URL`.

The in-container application uses Compose network service names:

```text
DATABASE_URL=postgresql+psycopg://productflow:<POSTGRES_PASSWORD>@productflow-postgres:5432/productflow
REDIS_URL=redis://productflow-redis:6379/0
STORAGE_ROOT=/app/storage
```

At runtime, container `STORAGE_ROOT` is fixed to `/app/storage`; do not write host paths into it. By default, uploaded and generated files are stored in the Docker named volume `productflow-storage` and persist across container restarts.

When migrating from an older systemd production environment, if you already have a production file directory such as `/home/cot/ProductFlow-release/shared/storage`, set this host-only variable in `.env` to reuse it:

```bash
STORAGE_HOST_PATH=/home/cot/ProductFlow-release/shared/storage
```

`STORAGE_HOST_PATH` is only the host path used by the Compose bind mount. API/worker containers still use `STORAGE_ROOT=/app/storage`. If empty or unset, Compose uses the `productflow-storage` named volume. Do not run `docker compose down -v` for normal updates, and do not delete Docker volumes just to switch storage mounts. To return to the named volume, remove `STORAGE_HOST_PATH` and run `docker compose up -d`.

### 3. Database migration

The `productflow-backend` startup command first runs:

```bash
alembic upgrade head
```

`uvicorn` starts only after migrations succeed. After upgrading code, if you need to rerun migrations manually:

```bash
docker compose run --rm productflow-backend alembic upgrade head
```

### 4. Access and health checks

With default ports:

```bash
docker compose ps
curl http://127.0.0.1:29280/healthz
curl http://127.0.0.1:29281/api/healthz
```

If you changed ports in `.env`, replace them with the corresponding values:

```bash
curl "http://127.0.0.1:<APP_HOST_PORT>/healthz"
curl "http://127.0.0.1:<WEB_PORT>/api/healthz"
```

Expected API response:

```json
{"status":"ok"}
```

Default Web entrypoint: `http://127.0.0.1:29281` (or the `WEB_PORT` from `.env` if changed). Log in with `ADMIN_ACCESS_KEY` from `.env`. The Web image serves Vite-built static assets through nginx, and nginx reverse-proxies same-origin `/api/*` requests to `productflow-backend:29280`.

### 5. Logs, stop, and cleanup

```bash
docker compose logs -f productflow-backend productflow-worker productflow-web
docker compose down
```

Stopping services does not delete data volumes. Only run this when you are sure you want to clear the database, Redis, and storage:

```bash
docker compose down -v
```

## Local Development Path

Use the local development path when changing code and using hot reload.

### 1. Prepare tools

Required on the host:

- Python 3.12+
- `uv`
- Node.js 20+ or a compatible version
- `pnpm`
- Docker / Docker Compose
- `just` (optional; raw commands are also listed below)

### 2. Copy environment variables

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
```

The `DATABASE_URL` / `REDIS_URL` in `.env.example` target the Compose container network. Local hot-reload development commands use `.env.dev` to connect through host `localhost:${POSTGRES_HOST_PORT:-15432}` and `localhost:${REDIS_HOST_PORT:-16379}`. At minimum, change these values in `.env` / `.env.dev` to your own random values:

- `ADMIN_ACCESS_KEY`: admin key used to log in to the backend UI.
- `SETTINGS_ACCESS_TOKEN`: secondary unlock token for the settings page; it must be different from the login key.
- `SESSION_SECRET`: long random string used to sign session cookies.
- `POSTGRES_PASSWORD`: local PostgreSQL password; keep it consistent with the password in `.env.dev`'s `DATABASE_URL`.

`.env.dev.example` uses development ports, Redis DB 1, and `backend/storage-dev`. The database name matches the default `docker-compose.yml`. If you use a separate development database, create it in PostgreSQL first, then adjust `.env.dev`'s `DATABASE_URL`. Local development storage is isolated from production Compose storage: `just backend-run` / `just backend-worker` and their raw equivalents read `STORAGE_ROOT=./backend/storage-dev` from `.env.dev`. Do not start local development processes by shell-sourcing production `.env` or importing production `STORAGE_HOST_PATH`.

### 3. Start development dependencies only

For local hot reload, use Compose only for PostgreSQL and Redis. The API, worker, and Web are started by host commands in the next step. The complete self-hosted stack uses `docker compose up -d --build` from the previous section.

```bash
docker compose up -d productflow-postgres productflow-redis
```

### 4. Install dependencies and migrate the database

With `just`:

```bash
just backend-install
just web-install
just backend-migrate
```

Without `just`:

```bash
uv sync --directory backend --extra dev
pnpm --dir web install
bash scripts/with_dev_env.sh uv run --directory backend alembic upgrade head
```

### 5. Start backend, worker, and frontend

Run these in three terminals. With `just`:

```bash
just backend-run
just backend-worker
just web-dev
```

Without `just`:

```bash
bash scripts/with_dev_env.sh bash -lc 'uv run --directory backend uvicorn productflow_backend.main:app --reload --host 0.0.0.0 --port "${APP_PORT:-29282}"'
bash scripts/with_dev_env.sh uv run --directory backend dramatiq --processes 2 --threads 4 productflow_backend.workers
bash scripts/with_dev_env.sh bash -lc 'web_port="${WEB_PORT:-29283}"; api_target="${VITE_DEV_PROXY_TARGET:-http://127.0.0.1:${APP_PORT:-29282}}"; VITE_API_BASE_URL= VITE_DEV_PROXY_TARGET="$api_target" pnpm --dir web dev -- --host 0.0.0.0 --port "$web_port" --strictPort'
```

Default development ports come from `.env.dev.example`:

- API: `http://localhost:29282`
- Web: `http://localhost:29283`

Open the Web page and log in with `ADMIN_ACCESS_KEY`. After login, you can click **Start guide** in the top navigation and follow the in-product guide to create a product, fill product details, generate copy, and generate images.

### 6. Development health check

```bash
curl http://127.0.0.1:29282/healthz
```

Expected response:

```json
{"status":"ok"}
```

## Model and Provider Configuration

ProductFlow configures text and image capabilities separately. Infrastructure configuration (database, Redis, session, admin key) is still read only from environment variables. Business configuration can be written to the database from the frontend `/settings` page and override environment defaults.

The login gate `admin_access_required` is enabled by default: normal workspace pages and private APIs require login with `ADMIN_ACCESS_KEY` first. Administrators can disable this gate after the secondary `/settings` unlock, allowing the ordinary workspace/API to be used without the admin key. `ADMIN_ACCESS_KEY` still must remain in the environment for future re-enabling, and `SETTINGS_ACCESS_TOKEN` always protects settings reads and writes independently.

Business hard deletion is disabled by default: when `DELETION_ENABLED=false`, product deletion and iterative image-session deletion APIs return 403 so demo sites can preserve evidence for policy review. Workflow node/edge editing and reference-image deletion are not affected. To remove whole products or sessions, an administrator can explicitly enable "business deletion" in `/settings`, or enable the environment default.

Text providers:

- `TEXT_PROVIDER_KIND=mock`: local fake implementation for development and testing.
- `TEXT_PROVIDER_KIND=openai`: OpenAI Responses-compatible interface.
- Related variables: `TEXT_API_KEY`, `TEXT_BASE_URL`, `TEXT_BRIEF_MODEL`, `TEXT_COPY_MODEL`.

Image providers:

- `IMAGE_PROVIDER_KIND=mock`: local fake image implementation.
- `IMAGE_PROVIDER_KIND=openai_responses`: OpenAI Responses `image_generation` tool with reference image input. ProductFlow's current iterative image branch context is determined by the base image and reference images explicitly selected by the user; it does not automatically send the entire historical image chain to the provider.
- Related variables: `IMAGE_API_KEY`, `IMAGE_BASE_URL`, `IMAGE_GENERATE_MODEL`, `IMAGE_MAIN_IMAGE_SIZE`, `IMAGE_PROMO_POSTER_SIZE`, `IMAGE_ALLOWED_SIZES`.

Poster modes:

- `POSTER_GENERATION_MODE=template`: render with local templates/Pillow without calling an image model.
- `POSTER_GENERATION_MODE=generated`: send confirmed copy and product/reference images to the image provider to generate posters.

Prompt templates:

- The prompt group in `/settings` can override templates for product understanding, copy generation, workbench image generation, and iterative image generation.
- Put one-off requirements into copy/image nodes; update settings-page templates only for long-term shared tone or format.

## Common Commands

| Purpose | With `just` | Without `just` |
|---|---|---|
| Install backend dependencies | `just backend-install` | `uv sync --directory backend --extra dev` |
| Install frontend dependencies | `just web-install` | `pnpm --dir web install` |
| Apply development DB migration | `just backend-migrate` | `bash scripts/with_dev_env.sh uv run --directory backend alembic upgrade head` |
| Start development API | `just backend-run` | `bash scripts/with_dev_env.sh bash -lc 'uv run --directory backend uvicorn productflow_backend.main:app --reload --host 0.0.0.0 --port "${APP_PORT:-29282}"'` |
| Start Dramatiq worker | `just backend-worker` | `bash scripts/with_dev_env.sh uv run --directory backend dramatiq --processes 2 --threads 4 productflow_backend.workers` |
| Run backend pytest | `just backend-test` | `uv run --directory backend pytest` |
| Start Vite dev server | `just web-dev` | `bash scripts/with_dev_env.sh bash -lc 'web_port="${WEB_PORT:-29283}"; api_target="${VITE_DEV_PROXY_TARGET:-http://127.0.0.1:${APP_PORT:-29282}}"; VITE_API_BASE_URL= VITE_DEV_PROXY_TARGET="$api_target" pnpm --dir web dev -- --host 0.0.0.0 --port "$web_port" --strictPort'` |
| Run frontend lint | no just wrapper | `pnpm --dir web lint` |
| Run frontend unit tests | no just wrapper | `pnpm --dir web test:run` |
| TypeScript check + Vite build | `just web-build` | `pnpm --dir web build` |
| Release dry run | `just release-dry-run` | `DRY_RUN=1 bash scripts/release.sh` |
| Production update | `just release` | `bash scripts/release.sh` |

`just release` / `bash scripts/release.sh` is the Docker Compose production update entrypoint. It first runs `docker compose config --quiet`, then attempts to stop legacy user-level systemd services that may occupy ports `29280/29281` (`productflow-backend.service`, `productflow-worker.service`, `productflow-web.service`), then runs `docker compose up -d --build --remove-orphans` and checks backend `/healthz`, web `/healthz`, and web proxy `/api/healthz`. This process does not delete Docker volumes; do not use `docker compose down -v` for normal updates. To reuse files from an old systemd production setup, set `STORAGE_HOST_PATH=/home/cot/ProductFlow-release/shared/storage` in `.env` first. If you have already manually moved old services away, you can temporarily run `LEGACY_SYSTEMD_ACTION=skip bash scripts/release.sh`, or `LEGACY_SYSTEMD_ACTION=skip just release`.

`just release-dry-run` / `DRY_RUN=1 bash scripts/release.sh` only validates Compose configuration and prints the steps a real release would execute. It does not stop systemd services, build images, start containers, or switch running services.

## Main API Resources

The backend exposes REST APIs only. Main entrypoints include:

- `POST /api/auth/session`, `GET /api/auth/session`, `DELETE /api/auth/session`
- `/api/products`, `/api/products/{product_id}`, `/api/products/{product_id}/history`
- `/api/products/{product_id}/reference-images`, `/api/source-assets/{asset_id}`, `/api/source-assets/{asset_id}/download`
- `/api/copy-sets/{copy_set_id}`, `/api/copy-sets/{copy_set_id}/confirm`
- `/api/posters/{poster_id}/download`
- `/api/image-sessions`, `/api/image-sessions/{image_session_id}`, `/api/image-sessions/{image_session_id}/status`, `/api/image-session-assets/{asset_id}/download`
- `/api/gallery`
- `/api/generation-queue`
- `/api/products/{product_id}/workflow`, `/api/products/{product_id}/workflow/status`, `/api/products/{product_id}/workflow/run`, `/api/workflow-nodes/{node_id}`, `/api/workflow-edges/{edge_id}`
- `/api/settings`, `/api/settings/lock-state`, `/api/settings/unlock`, `/api/settings/runtime`

## Open Source and Security Boundaries

- License: MIT, see `LICENSE`.
- Contribution guide: see `CONTRIBUTING.en.md`.
- Security reporting: see `SECURITY.en.md`.
- Do not commit `.env`, `web/.env`, local storage, build outputs, caches, logs, or `.trellis/tasks/` / `.trellis/workspace/`.
- Real provider API keys should only be stored in local environment variables or private deployment configuration. Do not write them into issues, PRs, or documentation examples.
