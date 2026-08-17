<p align="center">
  <img src="docs/assets/productflow-brand-concept.png" alt="ProductFlow product-workflow brand artwork" width="168">
</p>

# ProductFlow

[中文](README.md) | [English](README.en.md)

<p align="center">
  <a href="https://draw.devbin.de"><strong>Live Demo</strong></a>
</p>

ProductFlow is an open-source product-visual workspace for a single merchant. A user uploads real product references, chooses the required image types and quantities, and works with a workflow Agent to clarify price, style, text language, copy requirements, and other missing information. The confirmed result becomes an editable, executable image-production workflow.

The public instance is a personal live demo with one administrator and one merchant. Operators may reset public demo data under the demo policy, but deployed-instance upgrades do not depend on resetting the database or storage. Multi-tenancy, billing, team permissions, and formal SaaS compatibility policies belong to a later stage.

## Current Capabilities

### Agent Product Creation

- `/products/new` is the canonical creation route.
- Users choose hero, selling-point, scene, detail, SKU, dimension, and other image types. Each type defaults to two images and has an independent quantity control.
- Every product starts with one to six real product reference images in PNG, JPEG, or WebP.
- The Agent can ask for price, generation style, image text language, copy requirements, and visual-system decisions.
- After the user confirms the WorkflowDraft, the UI streams materialization progress and reveals the product workbench while continuing the same Agent conversation in the sidebar.

### V2 Workflow Canvas

- The only current node types are `product_context`, `reference_image`, `prompt_generation`, and `image_generation`.
- The canvas supports adding nodes, drawing edges, moving, deleting, multi-selecting, zooming, panning, automatic layout, and keyboard shortcuts.
- Folders organize local workflow sections and reduce visual complexity in larger DAGs.
- The inspector edits prompts, reference bindings, visual-system overrides, aspect ratio, resolution, quality, text policy, and delivery specifications.
- Generated images are held by their image nodes and also enter the product's canonical image library.
- Workflow reuse comes from user-saved `WorkflowRecipe` records or `recipe_fragment` records.

### Image Library and Iterative Generation

- `/media-library` is the cross-product canonical media library with search, folders, tags, archive/restore, and batch organization. Workflow sub-libraries associate the same assets without copying media bytes.
- The product image library provides an Explorer-style directory tree, folders, filters, sorting, rename, move, preview, multi-selection, and downloads.
- Each image node binds one current image; the library retains every upload, workflow result, and image-session attachment for the product.
- Product cover selection is automatic, with current cover APIs available for an explicit change.
- `/image-chat` supports reference images, branch bases, multiple candidates, cancel, retry, download, and save-to-product.
- `/gallery` stores image-session results that the user explicitly collects.

### Providers and Runtime

- `/settings` manages provider profiles and the three purposes: `prompt`, `agent`, and `image`.
- A profile stores Base URL, API key, capabilities, and default models. Secrets are never echoed back.
- Image bindings support OpenAI Responses, OpenAI Images-compatible APIs, and Google Gemini image capabilities.
- Advanced image fields include quality, format, compression, background, moderation, action, input fidelity, and partial images. Business selections own generation count.
- Agent Turns run in a separate Go service backed by the vendored `agent-harness` durable runtime. SSE supports reconnect and replay.
- Dramatiq and Redis execute image and delivery jobs. PostgreSQL stores business state, and storage holds media bytes.

## Current Scope

- Single administrator and single merchant.
- No multi-tenancy, team permissions, billing, hosted accounts, automatic publishing, ad delivery, or video generation.
- Public demo data may be reset during upgrades.
- Historical Alembic revisions remain so a fresh database can reach the current schema. Online runtime contracts are V2-only.

## Routes

| Route | Purpose |
|---|---|
| `/products` | Product list |
| `/products/new` | Agent product creation |
| `/products/:productId` | Agent conversation, V2 workflow, and image library |
| `/image-chat` | Iterative text/image generation |
| `/media-library` | Global media library |
| `/gallery` | Compatibility redirect for the retired collected-image bookmark |
| `/history` | V1 history archives and Agent rebuild |
| `/settings` | Providers and runtime settings |
| `/help` | In-product help |

Repository documentation:

- [Documentation map](docs/README.md)
- [Product requirements](docs/PRD.en.md)
- [User guide](docs/USER_GUIDE.en.md)
- [Architecture](docs/ARCHITECTURE.en.md)
- [Roadmap](docs/ROADMAP.en.md)
- [Changelog](CHANGELOG.md)

## Technology

- Backend: Python 3.12+, FastAPI, SQLAlchemy, Alembic, Dramatiq, Redis, PostgreSQL, and Pillow.
- Agent service: Go, vendored `agent-harness`, SQLite durable journal, and Responses API.
- Frontend: React 19, Vite, TypeScript, React Router, TanStack Query, XYFlow, and Tailwind CSS 4.
- Model SDKs: OpenAI Python/Go ecosystem and Google GenAI.

## Repository Layout

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

## Docker Compose

### 1. Prepare Configuration

```bash
cp .env.example .env
```

Replace at least:

- `ADMIN_ACCESS_KEY`
- `SETTINGS_ACCESS_TOKEN`
- `SESSION_SECRET`
- `POSTGRES_PASSWORD`
- `AGENT_SERVICE_INTERNAL_TOKEN`

### 2. Start the Full Stack

```bash
docker compose up -d --build
```

Compose starts PostgreSQL, Redis, FastAPI, the Dramatiq worker, the Agent service, and Web. The backend container runs `alembic upgrade head` before Uvicorn.

Default endpoints:

- Web: `http://127.0.0.1:29281`
- Backend health: `http://127.0.0.1:29280/healthz`
- Web proxy health: `http://127.0.0.1:29281/api/healthz`

Log in with `ADMIN_ACCESS_KEY`, unlock settings with `SETTINGS_ACCESS_TOKEN`, and configure the `prompt`, `agent`, and `image` bindings.

### 3. Data and Logs

```bash
docker compose logs -f productflow-backend productflow-worker productflow-agent-service productflow-web
docker compose down
```

`docker compose down` keeps volumes. To intentionally clear demo PostgreSQL, Redis, media storage, and the Agent journal:

```bash
docker compose down -v
```

Set `STORAGE_HOST_PATH=/absolute/host/path` to use a host directory. When omitted, Compose uses the `productflow-storage` named volume.

## Local Development

### 1. Prerequisites

- Python 3.12+ and `uv`
- Go 1.26.5+
- Node.js 20+ and `pnpm`
- Docker / Docker Compose
- `just` (recommended)

### 2. Development Configuration

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
```

Keep the PostgreSQL password consistent between `.env` and `.env.dev`. Use separate random values for the admin login, settings unlock, and internal Agent service token.

### 3. Install and Migrate

```bash
docker compose up -d productflow-postgres productflow-redis
just backend-install
just web-install
just backend-migrate
```

### 4. Start Four Development Processes

Run each command in a separate terminal:

```bash
just backend-run
just backend-worker
just agent-service-run
just web-dev
```

Default development endpoints:

- API: `http://127.0.0.1:29282`
- Agent service: `http://127.0.0.1:29284`
- Web: `http://127.0.0.1:29283`

## Verification

```bash
uv run --directory backend ruff check src tests
just backend-test
pnpm --dir web test:run
pnpm --dir web lint
just web-build
just agent-service-test
```

Live PostgreSQL/Redis recovery and provider checks are opt-in:

```bash
just backend-test-live-recovery
just backend-test-live-delivery-renditions
just backend-test-live-agent-product-intake
just agent-service-test-live
```

## Release Script

```bash
just release-dry-run
just release
```

`release-dry-run` validates Compose and prints the current release action. `release` runs `docker compose up -d --build --remove-orphans`, then checks the backend, Agent service, Web, and Web API proxy. It does not delete volumes.

## Main API Resources

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

FastAPI OpenAPI and `backend/src/productflow_backend/presentation/routes/` are authoritative for the complete contract.

## Open Source and Security

- License: MIT; see [LICENSE](LICENSE).
- Contribution guide: [CONTRIBUTING.md](CONTRIBUTING.md).
- Security reporting: [SECURITY.md](SECURITY.md).
- Do not commit `.env`, `web/.env`, storage, build output, caches, logs, or migration evidence directories.
- Keep provider API keys in private configuration only.

ProductFlow uses OpenAI Codex during development. Thanks to the [LinuxDo](https://linux.do) community.
