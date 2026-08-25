# Repository Guidelines

## Working Method

Read the live implementation, call chain, tests, and current diff before deciding what is true. Use `CONTEXT.md` for domain vocabulary and stable invariants, `docs/adr/` for accepted architecture decisions, and `docs/rollout/` for unfinished operational work. When documentation conflicts with code or tests, verify the live behavior and correct the documentation.

Do not require a repository task, planning phase, session journal, or workflow ceremony for ordinary work. For broad changes, state the scope and validation plan in the conversation or issue. Persist only decisions that will remain useful after the change.

Keep modifications at the real causal boundary. Reuse existing models, enums, exceptions, query helpers, fixtures, and UI components. Add guards or abstractions only for a demonstrated failure mode or invariant. Never revive retired V1 runtime behavior as a fallback.

Before a cross-layer change, trace `input -> wire schema -> application use case -> persistence/external effect -> response -> frontend projection`. Assign each validation rule to the narrowest layer that has enough information, and test at the boundary where drift would become observable. Search all readers and writers before changing an enum, persisted JSON shape, route, provider field, or shared UI projection.

Search for an existing implementation before adding a helper, API, state store, component, or constant. Extract an abstraction only when it removes repeated non-trivial logic or establishes one real owner. After deletion or a contract rename, scan code, tests, configuration, docs, and historical-value readers for residue.

Documentation ownership is defined in `docs/README.md`. Stable docs describe current behavior and must name current code owners or tests where the claim is implementation-sensitive. Planned work belongs in `docs/ROADMAP.md`; deployment evidence belongs in `docs/rollout/` and `docs/operations/`.

## Multi-Agent Delivery

For broad implementation work, the primary agent is the orchestrator and integrator. The project-scoped Codex default is `gpt-5.6-sol` with `medium` reasoning. Implementation sub-agents use `gpt-5.6-luna` with `max` reasoning unless the user explicitly selects another model.

The primary agent owns:

- live-truth inspection, causal analysis, architecture and contract decisions;
- decomposition into independently verifiable implementation slices;
- assignment of exclusive file or module ownership while agents are running;
- review of every sub-agent diff and reconciliation with concurrent user changes;
- cross-slice integration, deletion of obsolete paths and final verification;
- user-facing status, risk and completion claims.

Implementation sub-agents receive one bounded causal slice at a time. Each task packet must state:

- the concrete outcome and user-visible behavior;
- current implementation and contract anchors to inspect;
- files or modules the agent owns and boundaries it must not edit;
- required wire, persistence and runtime invariants;
- focused tests and completion evidence;
- known concurrent work and prohibited cleanup, commit or destructive Git actions.

Spawn implementation agents with the project-defined `implementer` role and bounded context via `fork_turns: none` or a small positive turn count. The role is bound by `.codex/agent-layers/luna-max.toml` to `gpt-5.6-luna` with `max` reasoning; the project `default` sub-agent role uses the same layer. Do not rely on per-call model overrides because a configured agent role owns the effective model and reasoning effort. Include all necessary repository context in the task packet when using `fork_turns: none`.

Keep one writer per file or tightly coupled module at a time. Run independent slices concurrently only when their ownership and contracts do not overlap. With four total agent slots, use at most three implementation agents alongside the primary agent. Serialize work when two slices share a DTO, route, migration, page orchestrator or generated contract.

Sub-agents do not commit, push, reset, revert unrelated changes or declare the overall task complete. They report changed files, behavior, tests, unresolved risks and assumptions. The primary agent reads the resulting diff, runs integration checks at the shared boundary and may return a focused correction task to the same agent.

The primary agent may make narrow integration edits after reviewing sub-agent work. Substantial implementation discovered during integration is split into another Luna task when it has a clear ownership boundary. Ordinary small fixes and read-only investigations do not require delegation ceremony.

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
- `just dev` — stop leftover API/worker/dispatcher/Agent/Web processes, start local PostgreSQL/Redis, apply migrations, then run backend, worker, dispatcher, Pi Agent, and Web in parallel.
- `just dev-stop` — stop leftover API/worker/dispatcher/Agent/Web processes from a previous `just dev`.
- `just backend-test` — run backend pytest tests.
- `just docs-check` — verify documented routes, code-owner paths, and local Markdown links.
- `just web-install` — install frontend dependencies with pnpm.
- `just web-dev` — run Vite with the API proxy configured.
- `just web-build` — type-check and build the frontend.
- `just web-e2e-live-graph` — opt-in browser gate: skip-Agent create, run the full graph, real prompt/image providers.

## Coding Style & Naming Conventions
Python targets 3.12 and uses Ruff with 120-character lines plus `E`, `F`, `I`, `UP`, and `B` lint rules. Keep imports sorted, prefer typed functions, and name modules/functions in `snake_case`. React components and pages use `PascalCase` filenames, such as `ProductListPage.tsx`; hooks, helpers, and API functions use `camelCase`. Keep provider-specific code behind infrastructure factories instead of leaking it into routes.

## Testing Guidelines
Backend tests use pytest and are discovered from `backend/tests/` as `test_*.py`. Add workflow-level coverage when changing product, Draft, workflow, settings, provider, archive, or image-session behavior. Run `just backend-test` before backend commits, `just agent-service-test` for Node.js/Pi changes, and the frontend test/lint/build gate described in `web/AGENTS.md` for frontend changes. Schema changes require an Alembic revision and focused migration regression coverage. Skip-Agent full-graph browser coverage against real providers is `just web-e2e-live-graph`; it is not part of the default frontend gate.

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
