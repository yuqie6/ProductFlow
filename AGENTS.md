# Repository Guidelines

## Working Method

Inspect the current diff and the implementation, callers and tests relevant to the task. Load applicable package rules when entering that package. Reuse documents and verification already read in the session while their relevant inputs remain unchanged; refresh when files, environment, ownership or context may have changed.

Read documentation by question: `CONTEXT.md` for domain terms and invariants, `docs/ARCHITECTURE.md` for ownership and runtime flow, `docs/PRD.md` / `docs/USER_GUIDE.md` for affected product behavior, and `docs/ROADMAP.md` for unfinished directions. Read the relevant sections rather than treating this list as an opening checklist. `docs/adr/` is a historical archive, not current design; do not write new ADRs. When code and documentation disagree, determine whether the implementation or the document is wrong. Read-only reviews report the discrepancy; authorized fixes update the affected implementation or living documentation.

Default to a complete, observable outcome owned by one primary agent. A directly assigned design, feature, migration or fix does not require board issues for its internal steps. Read the design against the current implementation, state scope and validation for substantial work, and continue through implementation, integration and acceptance within the authorized objective. Use a short conversation plan for internal sequencing; do not require a planning phase or session journal.

Design documents own the intended outcome, key tradeoffs, invariants and final acceptance criteria. Choose implementation steps from the actual causal chain. Split work when it enables independent delegation, handoff, concurrent ownership or separately frozen evidence; keep tightly coupled changes under one delivery owner. Use the shared board at [`docs/audits/tasks/README.md`](docs/audits/tasks/README.md) for those coordination needs or when the user explicitly asks to publish or claim a task. Existing board work remains subject to its protocol. Persist durable decisions and evidence in their existing document owners; avoid copying implementation checklists across designs, group documents and indexes.

Keep modifications at the real causal boundary. Reuse existing models, enums, exceptions, query helpers, fixtures, and UI components. Add guards or abstractions only for a demonstrated failure mode or invariant. Never revive retired V1 runtime behavior as a fallback. Do not add compatibility shims, dual serializers, or old-data migration commands; delete leftover paths.

Before a cross-layer change, trace `input -> wire schema -> application use case -> persistence/external effect -> response -> frontend projection`. Assign each validation rule to the narrowest layer that has enough information, and test at the boundary where drift would become observable. Search all readers and writers before changing an enum, persisted JSON shape, route, provider field, or shared UI projection.

Search for an existing implementation before adding a helper, API, state store, component, or constant. Extract an abstraction only when it removes repeated non-trivial logic or establishes one real owner. After deletion or a contract rename, scan code, tests, configuration, and docs for residue. Do not keep readers for retired shapes.

When editing documentation, use the ownership map in [`docs/README.md`](docs/README.md). For publishing, claiming, blocking, reviewing or closing a board issue, load [`docs/audits/tasks/README.md`](docs/audits/tasks/README.md); it owns the full coordination procedure. Board tasks require confirmed ownership before task-specific investigation or planning, including read-only exploration by the primary agent. Direct work must still respect existing file ownership and frozen runtime resources.

## Multi-Agent Delivery

The primary agent owns:

- live-truth inspection, causal analysis, architecture and contract decisions;
- decomposition into independently verifiable implementation slices;
- assignment of exclusive file or module ownership while agents are running;
- review of every sub-agent diff and reconciliation with concurrent user changes;
- cross-slice integration, deletion of obsolete paths and final verification;
- user-facing status, risk and completion claims.

Implementation sub-agents receive one claimed board task at a time, with outcome, exclusive scope, invariants, dependencies and completion evidence specified by the [task template](docs/audits/tasks/_template.md). Keep one writer per file or tightly coupled module. Parallel slices must have non-conflicting write scopes, frozen inputs and runtime resources. Use at most three implementation agents alongside the primary agent when four slots are available. Worktree-local commits do not establish mutual exclusion.

Each executor self-reviews its complete task diff and reports evidence; the primary agent reviews every sub-agent delivery and coordinates integration. Sub-agents stop for assignment after delivery and must not push, reset, revert, or declare the overall program complete. The primary agent continues necessary work within the user's authorized objective. For new work requiring delegation or shared ownership, adjust or publish its board task and confirm ownership and dependencies before proceeding. Internal steps of direct work can continue after checking conflicts; a completed slice does not end the primary agent's responsibility for the assigned outcome. New objectives or actions beyond existing authorization require user approval.

## Project Structure & Module Organization
ProductFlow is a single-administrator, single-merchant workspace. The live business backend is `go/internal/` (Gin HTTP, GORM on pgx, asynq). Schema authority is `go/cmd/productflow-migrate`. The main Agent service is the Node.js/Pi adapter in `agent-service/`; the legacy Go runtime is kept only on `exp`. The React/Vite app lives in `web/src/`, with pages in `web/src/pages/`, shared UI in `web/src/components/`, and API/type helpers in `web/src/lib/`. Package rules live in `go/AGENTS.md` and `web/AGENTS.md`; consult `go/README.md` for Go setup or runtime questions. The retired FastAPI tree is on `retired/python`; do not merge it back.

## Build, Test, and Development Commands
Use the root `justfile` whenever possible:

- `docker compose up -d productflow-postgres productflow-redis` — start local PostgreSQL and Redis.
- `just go-migrate` — apply GORM `CreateTable`/`AddColumn` plus ExtraDDL with dev env vars. AutoMigrate is not used.
- `just go-api` — run the Go business API (default local runtime).
- `just go-worker` — run the Go asynq worker.
- `just go-dispatcher` — run the Go async dispatcher.
- `just go-test` — run the full Go suite with dev env vars and serial package execution.
- `just agent-service-install` — install the Node.js/Pi Agent dependencies from the lockfile.
- `just agent-service-run` — run the Node.js/Pi workflow Agent service.
- `just agent-service-test` — run Agent service tests.
- `just dev` — stop leftover API/worker/dispatcher/Agent/Web processes, start local PostgreSQL/Redis, apply migrations, then run Go API, worker, dispatcher, Pi Agent, and Web in parallel.
- `just dev-stop` — stop leftover API/worker/dispatcher/Agent/Web processes from a previous `just dev`.
- `just docs-check` — verify documented routes, code-owner paths, local Markdown links, and internal issue/board consistency.
- `just web-install` — install frontend dependencies with pnpm.
- `just web-dev` — run Vite with the API proxy configured.
- `just web-build` — type-check, build the frontend, and check bundle budgets.
- `just web-e2e-live-graph` — opt-in browser gate: skip-Agent create, run the full graph, real prompt/image providers.

Use an existing suitable dev stack when available. Before `just dev` / `just dev-stop`, check whether its processes or database are in use by another task. Real-provider runs require the task's credentials, budget authorization and resource isolation; an opt-in command does not grant them.

## Coding Style & Naming Conventions
Go packages under `go/internal/` use the module layout in `go/AGENTS.md`. React components and pages use `PascalCase` filenames, such as `ProductListPage.tsx`; hooks, helpers, and API functions use `camelCase`. Keep provider-specific code behind infrastructure factories instead of leaking it into routes.

## Testing Guidelines
Choose verification from the changed behavior and its consumers. A fixed task or release contract may require more; retain that requirement unless its owner changes the contract before evaluation.

| Change | Required evidence |
|---|---|
| Documentation or engineering instructions only | Relevant links, commands, diff and `just docs-check`; for rule changes, walk through positive and negative trigger cases. No business build or live run by default. |
| Local logic or interaction | Focused regression at the causal boundary plus applicable package static checks. |
| Shared wire types, state machines, persistence or schema | Tests for affected readers and writers and cross-module behavior; schema changes include a focused migrate regression. |
| Layout, copy or theme | The affected UI and relevant viewport, locale, theme and input states, as scoped in `web/AGENTS.md`. |
| Release, capacity or real-model quality claim | The corresponding full build, budget, browser or frozen live evaluation gate. A local pass does not establish the broader claim. |

Go commands and PostgreSQL prerequisites are in `go/AGENTS.md`; `just go-test` is the full backend gate. For Node.js/Pi code changes, run focused tests and check affected generated contracts with `just agent-service-check-contracts`; use `just agent-service-test` for shared runtime changes or the full service gate. Product runtime prompts and Skills are behavior changes, not documentation-only edits. Skill changes follow `agent-service/.pi/skills/README.md`; real-model evals remain opt-in unless the task contract requires them. Frontend checks are defined in `web/AGENTS.md`. Reuse valid results when their code, dependencies, configuration and relevant environment are unchanged. Broaden or repeat checks for new evidence or changed inputs.

## Completion

A task is complete when its requested output is delivered, required behavior has proportionate evidence, the complete task diff and temporary artifacts or processes have been reviewed, and necessary documentation and authorized delivery commits are finished. State unverified items and their impact. Read-only reviews and discussions finish with findings or decisions and do not authorize file changes.

Continue authorized work through implementation, verification and integration without repeated permission requests. If a required check is blocked, report the missing input or environment and do not claim completion; optional broader checks do not block a verified local result. Finish other independent work within the objective where possible. Do not expand into unrelated fixes merely to make a full suite green. Stop at a genuine authorization, dependency or resource boundary rather than a turn boundary.

## Commit & Pull Request Guidelines
Load [the commit procedure](.cursor/rules/module-review-commit.mdc) when preparing this task's first commit; reuse it until it changes or context is missing. Board delivery and archive mechanics belong to the task protocol. A reviewed exclusive delivery is committed in the same turn; that is standing authorization for this repo, subject to the user's current instructions. Do not wait for a separate commit request. Pull requests describe the user-visible change, report actual verification and gaps, call out migrations/config changes, and include relevant UI screenshots.

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

Product issues and PRDs are tracked in GitHub Issues for `yuqie6/ProductFlow`; delegated execution tasks, including independent user requests, share `docs/audits/tasks/`. A local execution task does not require a GitHub issue. See `docs/agents/issue-tracker.md` for routing and cross-links.

### Triage labels

Triage uses the default five-label vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Domain documentation uses a single-context layout: root `CONTEXT.md`. `docs/adr/` is a historical archive, not live design. See `docs/agents/domain.md`.

### Audit task board

Each business group has exactly one document containing its responsibilities, contracts, gates and evidence. [`docs/audits/README.md`](docs/audits/README.md) indexes those documents. When merging groups, consolidate active content and delete the old group files; move historical evidence into existing history docs. Group tasks and independent user requests share [`docs/audits/tasks/README.md`](docs/audits/tasks/README.md); claiming and conflict checks apply to both.

Group boundaries follow independently verifiable outcomes. Sequence each group's necessary implementation and verification tasks internally; consume already delivered, fixed contracts from other groups. Record genuine startup blockers instead of assuming future interfaces exist. Independent evaluation means frozen scoring and protected evidence, not a required human handoff on every run. Runtime self-evolution has one final user approval; development-task coordination and review remain governed by the shared board.
