# Repository Guidelines

## Working Method

Read the live implementation, call chain, tests, and current diff before deciding what is true. Use `CONTEXT.md` for domain vocabulary and stable invariants, `docs/adr/` for accepted architecture decisions, and `docs/rollout/` for unfinished operational work. When documentation conflicts with code or tests, verify the live behavior and correct the documentation.

Do not require a repository task, planning phase, session journal, or workflow ceremony for ordinary work. For broad changes, state the scope and validation plan in the conversation or issue. Persist only decisions that will remain useful after the change.

Keep modifications at the real causal boundary. Reuse existing models, enums, exceptions, query helpers, fixtures, and UI components. Add guards or abstractions only for a demonstrated failure mode or invariant. Never revive retired V1 runtime behavior as a fallback.

Before a cross-layer change, trace `input -> wire schema -> application use case -> persistence/external effect -> response -> frontend projection`. Assign each validation rule to the narrowest layer that has enough information, and test at the boundary where drift would become observable. Search all readers and writers before changing an enum, persisted JSON shape, route, provider field, or shared UI projection.

Search for an existing implementation before adding a helper, API, state store, component, or constant. Extract an abstraction only when it removes repeated non-trivial logic or establishes one real owner. After deletion or a contract rename, scan code, tests, configuration, docs, and historical-value readers for residue.

Documentation ownership is defined in `docs/README.md`. Stable docs describe current behavior and must name current code owners or tests where the claim is implementation-sensitive. Planned work belongs in `docs/ROADMAP.md`; deployment evidence belongs in `docs/rollout/` and `docs/operations/`.

## Project Structure & Module Organization
ProductFlow is a single-administrator, single-merchant workspace. The backend lives in `backend/src/productflow_backend/` and uses `presentation/` for FastAPI routes and schemas, `application/` for use cases, `domain/` for enums and database-free rules, and `infrastructure/` for database, storage, queues, providers, and service clients. Alembic migrations are in `backend/alembic/versions/`; backend tests are in `backend/tests/`. The main Agent service is the Node.js/Pi adapter in `agent-service/`; the legacy Go runtime is kept only on `exp`. The React/Vite app lives in `web/src/`, with pages in `web/src/pages/`, shared UI in `web/src/components/`, and API/type helpers in `web/src/lib/`. Read `backend/AGENTS.md` or `web/AGENTS.md` before editing that package.

## Build, Test, and Development Commands
Use the root `justfile` whenever possible:

- `just backend-install` — install backend dependencies with `uv` dev extras.
- `docker compose up -d` — start local PostgreSQL and Redis.
- `just backend-migrate` — apply Alembic migrations with dev env vars.
- `just backend-run` — run the FastAPI API on the dev port.
- `just backend-worker` — run Dramatiq workers for async jobs.
- `just agent-service-install` — install the Node.js/Pi Agent dependencies from the lockfile.
- `just agent-service-run` — run the Node.js/Pi workflow Agent service.
- `just agent-service-test` — run Agent service tests.
- `just dev` — start local PostgreSQL/Redis, apply migrations, then run backend, worker, Pi Agent, and Web in parallel.
- `just backend-test` — run backend pytest tests.
- `just docs-check` — verify documented routes, code-owner paths, and local Markdown links.
- `just web-install` — install frontend dependencies with pnpm.
- `just web-dev` — run Vite with the API proxy configured.
- `just web-build` — type-check and build the frontend.

## Coding Style & Naming Conventions
Python targets 3.12 and uses Ruff with 120-character lines plus `E`, `F`, `I`, `UP`, and `B` lint rules. Keep imports sorted, prefer typed functions, and name modules/functions in `snake_case`. React components and pages use `PascalCase` filenames, such as `ProductListPage.tsx`; hooks, helpers, and API functions use `camelCase`. Keep provider-specific code behind infrastructure factories instead of leaking it into routes.

## Testing Guidelines
Backend tests use pytest and are discovered from `backend/tests/` as `test_*.py`. Add workflow-level coverage when changing product, Draft, workflow, settings, provider, archive, or image-session behavior. Run `just backend-test` before backend commits, `just agent-service-test` for Node.js/Pi changes, and the frontend test/lint/build gate described in `web/AGENTS.md` for frontend changes. Schema changes require an Alembic revision and focused migration regression coverage.

## Commit & Pull Request Guidelines
Recent history mixes Conventional Commit prefixes (`feat:`, `chore:`) with concise Chinese summaries. Use one focused commit per topic, for example `feat: 增加设置页模型配置`. Pull requests should describe the user-visible change, list verification commands, call out migrations/config changes, and include screenshots for UI updates.

## Documentation Style
Official docs, release notes, PR descriptions, and contribution guidance must stay concrete and verifiable. Avoid templated delivery copy and empty contrast/progress scaffolding:

- Do not use Chinese patterns like “这不是……而是……”, “不是……而是……”, “先把……打通”, or promotional “先……再……”.
- Do not use English patterns like “This is not ..., but ...”, “not ..., but ...”, “establishes the main loop”, or promotional “first ..., then ...”.
- Keep real technical sequencing when it matters, such as command order, migration steps, auto-save before run, and troubleshooting steps.
- State current facts and verified results; label future direction as unimplemented or planned.

## Security & Configuration Tips
Do not commit `.env`, `web/.env`, generated storage, caches, or build output. Keep secrets in files copied from `.env.example` / `web/.env.example`. Runtime database settings may override selected provider/model options, while `DATABASE_URL`, `REDIS_URL`, `SESSION_SECRET`, and `ADMIN_ACCESS_KEY` remain env-only.

## Repository Metadata

### Issue tracker

Issues and PRDs are tracked in GitHub Issues for `yuqie6/ProductFlow`. See `docs/agents/issue-tracker.md`.

### Triage labels

Triage uses the default five-label vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Domain documentation uses a single-context layout: root `CONTEXT.md` plus `docs/adr/` when they exist. See `docs/agents/domain.md`.
