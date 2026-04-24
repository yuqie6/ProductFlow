<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

Use the `/trellis:start` command when starting a new session to:
- Initialize your developer identity
- Understand current project context
- Read relevant guidelines

Use `@/.trellis/` to learn:
- Development workflow (`workflow.md`)
- Project structure guidelines (`spec/`)
- Developer workspace (`workspace/`)

If you're using Codex, project-scoped helpers may also live in:
- `.agents/skills/` for reusable Trellis skills
- `.codex/agents/` for optional custom subagents

Keep this managed block so 'trellis update' can refresh the instructions.

<!-- TRELLIS:END -->
# Repository Guidelines

## Project Structure & Module Organization
ProductFlow is a private single-merchant workspace. The backend lives in `backend/src/productflow_backend/` and uses clear layers: `presentation/` for FastAPI routes and schemas, `application/` for use cases, `domain/` for enums/core concepts, and `infrastructure/` for database, storage, queues, text/image providers, and poster rendering. Alembic migrations are in `backend/alembic/versions/`; backend tests are in `backend/tests/`. The React/Vite app lives in `web/src/`, with pages in `web/src/pages/`, shared UI in `web/src/components/`, and API/type helpers in `web/src/lib/`. Product and architecture notes live in `docs/`.

## Build, Test, and Development Commands
Use the root `justfile` whenever possible:

- `just backend-install` — install backend dependencies with `uv` dev extras.
- `docker compose up -d` — start local PostgreSQL and Redis.
- `just backend-migrate` — apply Alembic migrations with dev env vars.
- `just backend-run` — run the FastAPI API on the dev port.
- `just backend-worker` — run Dramatiq workers for async jobs.
- `just backend-test` — run backend pytest tests.
- `just web-install` — install frontend dependencies with pnpm.
- `just web-dev` — run Vite with the API proxy configured.
- `just web-build` — type-check and build the frontend.

## Coding Style & Naming Conventions
Python targets 3.12 and uses Ruff with 120-character lines plus `E`, `F`, `I`, `UP`, and `B` lint rules. Keep imports sorted, prefer typed functions, and name modules/functions in `snake_case`. React components and pages use `PascalCase` filenames, such as `ProductListPage.tsx`; hooks, helpers, and API functions use `camelCase`. Keep provider-specific code behind infrastructure factories instead of leaking it into routes.

## Testing Guidelines
Backend tests use pytest and are discovered from `backend/tests/` as `test_*.py`. Add workflow-level coverage when changing product, copy, poster, settings, or image-session behavior. Run `just backend-test` before backend commits and `just web-build` before frontend commits. For schema or migration changes, include both an Alembic revision and a regression test where practical.

## Commit & Pull Request Guidelines
Recent history mixes Conventional Commit prefixes (`feat:`, `chore:`) with concise Chinese summaries. Use one focused commit per topic, for example `feat: 增加设置页模型配置`. Pull requests should describe the user-visible change, list verification commands, call out migrations/config changes, and include screenshots for UI updates.

## Security & Configuration Tips
Do not commit `.env`, `web/.env`, generated storage, caches, or build output. Keep secrets in files copied from `.env.example` / `web/.env.example`. Runtime database settings may override selected provider/model options, while `DATABASE_URL`, `REDIS_URL`, `SESSION_SECRET`, and `ADMIN_ACCESS_KEY` remain env-only.
