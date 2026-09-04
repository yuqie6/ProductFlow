# Repository Guidelines

## Working Method

Read the live implementation, call chain, tests, and current diff before deciding what is true. Use `CONTEXT.md` for domain vocabulary and stable invariants, `docs/PRD.md` / `docs/ARCHITECTURE.md` / `docs/USER_GUIDE.md` for current product and runtime shape, and `docs/ROADMAP.md` for unfinished directions. Do not treat `docs/adr/` as current design, and do not write new ADRs. When documentation conflicts with code or tests, verify the live behavior and correct the living documentation.

Do not require a repository task, planning phase, session journal, or workflow ceremony for ordinary work. Planned parallel slices use the audit task board at [`docs/audits/tasks/README.md`](docs/audits/tasks/README.md); each `docs/audits/*.md` ledger is a business-group charter. For other broad changes, state the scope and validation plan in the conversation or issue. Persist only decisions that will remain useful after the change.

Keep modifications at the real causal boundary. Reuse existing models, enums, exceptions, query helpers, fixtures, and UI components. Add guards or abstractions only for a demonstrated failure mode or invariant. Never revive retired V1 runtime behavior as a fallback. Do not add compatibility shims, dual serializers, or old-data migration commands; delete leftover paths.

Before a cross-layer change, trace `input -> wire schema -> application use case -> persistence/external effect -> response -> frontend projection`. Assign each validation rule to the narrowest layer that has enough information, and test at the boundary where drift would become observable. Search all readers and writers before changing an enum, persisted JSON shape, route, provider field, or shared UI projection.

Search for an existing implementation before adding a helper, API, state store, component, or constant. Extract an abstraction only when it removes repeated non-trivial logic or establishes one real owner. After deletion or a contract rename, scan code, tests, configuration, and docs for residue. Do not keep readers for retired shapes.

Documentation ownership is defined in `docs/README.md`. Stable docs describe current behavior and must name current code owners or tests where the claim is implementation-sensitive. Planned work belongs in `docs/ROADMAP.md`. For publishing, claiming, blocking, reviewing or closing an internal issue, follow [`docs/audits/tasks/README.md`](docs/audits/tasks/README.md). The issue file owns its status; the board is a checked index. Read the assigned issue plus live code, tests and applicable repository rules.

## Multi-Agent Delivery

The primary agent owns:

- live-truth inspection, causal analysis, architecture and contract decisions;
- decomposition into independently verifiable implementation slices;
- assignment of exclusive file or module ownership while agents are running;
- review of every sub-agent diff and reconciliation with concurrent user changes;
- cross-slice integration, deletion of obsolete paths and final verification;
- user-facing status, risk and completion claims.

Implementation sub-agents receive one bounded causal slice at a time. For work on the audit task board, that slice is exactly one claimed file under `docs/audits/tasks/`. Each task packet must state:

- the concrete outcome and user-visible behavior;
- current implementation and contract anchors to inspect;
- files or modules the agent owns and boundaries it must not edit;
- required wire, persistence and runtime invariants;
- focused tests and completion evidence;
- prerequisites, frozen evaluation inputs and exclusive runtime resources where applicable;
- claim fields (`状态` / `认领者` / `认领于`) so other agents can see occupancy.

Keep one writer per file or tightly coupled module at a time. Run independent slices concurrently only when their write scopes, frozen inputs and runtime resources do not conflict and each slice is claimed on the board. With four total agent slots, use at most three implementation agents alongside the primary agent. The primary agent confirms claims in one coordinating workspace; local commits in separate worktrees do not provide mutual exclusion.

The executor self-reviews its exclusive diff and reports evidence; the primary agent reviews every sub-agent delivery before acceptance. One designated Git writer (the primary agent by default) serializes claim, delivery and closure commits. Closing an issue updates the parent ledger's affected conclusions and evidence links, then archives the issue. Only the primary agent publishes follow-up issues after checking prerequisites. The executor **stops for assignment**. Procedure: `docs/audits/tasks/README.md` and `.cursor/rules/module-review-commit.mdc`. Sub-agents must not push, reset, revert, or declare the overall program complete.

The primary agent may make narrow integration edits after reviewing sub-agent work. Substantial implementation discovered during integration is filed as a new `开放` board task, not started in the same turn. Ordinary small fixes and read-only investigations do not use the board unless the user asks to file a task.

## Project Structure & Module Organization
ProductFlow is a single-administrator, single-merchant workspace. The live business backend is `go/internal/` (Gin HTTP, GORM on pgx, asynq). Schema authority is `go/cmd/productflow-migrate`. The main Agent service is the Node.js/Pi adapter in `agent-service/`; the legacy Go runtime is kept only on `exp`. The React/Vite app lives in `web/src/`, with pages in `web/src/pages/`, shared UI in `web/src/components/`, and API/type helpers in `web/src/lib/`. Read `go/README.md` or `web/AGENTS.md` before editing that package. The retired FastAPI tree is on `retired/python`; do not merge it back.

## Build, Test, and Development Commands
Use the root `justfile` whenever possible:

- `docker compose up -d` — start local PostgreSQL and Redis.
- `just go-migrate` — apply GORM `CreateTable`/`AddColumn` plus ExtraDDL with dev env vars. AutoMigrate is not used.
- `just go-api` — run the Go business API (default local runtime).
- `just go-worker` — run the Go asynq worker.
- `just go-dispatcher` — run the Go async dispatcher.
- `just go-test` — run Go tests (`go test -C go ./...`).
- `just agent-service-install` — install the Node.js/Pi Agent dependencies from the lockfile.
- `just agent-service-run` — run the Node.js/Pi workflow Agent service.
- `just agent-service-test` — run Agent service tests.
- `just dev` — stop leftover API/worker/dispatcher/Agent/Web processes, start local PostgreSQL/Redis, apply migrations, then run Go API, worker, dispatcher, Pi Agent, and Web in parallel.
- `just dev-stop` — stop leftover API/worker/dispatcher/Agent/Web processes from a previous `just dev`.
- `just docs-check` — verify documented routes, code-owner paths, local Markdown links, and internal issue/board consistency.
- `just web-install` — install frontend dependencies with pnpm.
- `just web-dev` — run Vite with the API proxy configured.
- `just web-build` — type-check and build the frontend.
- `just web-e2e-live-graph` — opt-in browser gate: skip-Agent create, run the full graph, real prompt/image providers.

## Coding Style & Naming Conventions
Go packages under `go/internal/` use the module layout in `go/AGENTS.md`. React components and pages use `PascalCase` filenames, such as `ProductListPage.tsx`; hooks, helpers, and API functions use `camelCase`. Keep provider-specific code behind infrastructure factories instead of leaking it into routes.

## Testing Guidelines
Go tests live under `go/` and are the default backend gate: `just go-test` (needs `DATABASE_URL` for packages that use PostgreSQL). Run `just agent-service-test` for Node.js/Pi changes, and the frontend test/lint/build gate described in `web/AGENTS.md` for frontend changes. Schema changes go through GORM models plus constraint patches in `go/internal/platform/db/schema` and a focused migrate regression. Skip-Agent full-graph browser coverage against real providers is `just web-e2e-live-graph`; it is not part of the default frontend gate.

## Commit & Pull Request Guidelines
Recent history mixes Conventional Commit prefixes (`feat:`, `chore:`) with concise Chinese summaries. Use one focused commit per topic, for example `feat: 增加设置页模型配置`. A reviewed exclusive module slice is committed in the same turn; that is standing authorization for this repo. Do not wait for a separate commit request, and do not commit unresolved work, secrets, or files outside the slice. Pull requests should describe the user-visible change, list verification commands, call out migrations/config changes, and include screenshots for UI updates.

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

Product issues and PRDs are tracked in GitHub Issues for `yuqie6/ProductFlow`; bounded audit-group execution issues live in `docs/audits/tasks/`. See `docs/agents/issue-tracker.md` for routing and cross-links.

### Triage labels

Triage uses the default five-label vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Domain documentation uses a single-context layout: root `CONTEXT.md`. `docs/adr/` is a historical archive, not live design. See `docs/agents/domain.md`.

### Audit task board

Each `docs/audits/*.md` ledger is a business-group charter. Agents claim issues on [`docs/audits/tasks/README.md`](docs/audits/tasks/README.md).
