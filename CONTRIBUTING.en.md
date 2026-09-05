# Contributing to ProductFlow

[中文](CONTRIBUTING.md) | English

Thank you for considering contributing code, documentation, or issue reports to ProductFlow. ProductFlow is currently positioned as an open-source self-hosted project, with priority on local reproducibility, truthful documentation, and clear data/secret boundaries.

## Before You Start

For initial orientation or setup, consult `README.en.md`. For day-to-day changes, follow [AGENTS.md](AGENTS.md) and read material relevant to the task. Reuse context already read while its relevant inputs remain unchanged:

- Backend changes use `go/AGENTS.md`; frontend changes use `web/AGENTS.md`, alongside the affected implementation and tests.
- Domain or product-semantic changes use the relevant definitions in `CONTEXT.md` and `docs/PRD.en.md`; unclear code ownership calls for the relevant section of `docs/ARCHITECTURE.en.md`.
- Documentation edits use the ownership map in `docs/README.md`; unfinished directions belong in `docs/ROADMAP.en.md`. Read `docs/adr/` only for historical questions.
- The retired FastAPI tree is on `retired/python`; do not merge it back. Do not commit `.env`, `web/.env`, storage, caches, build outputs, logs, or local database dumps.

## Local Development

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
docker compose up -d productflow-postgres productflow-redis
just agent-service-install
just web-install
just go-migrate
just go-api
just go-worker
just go-dispatcher
just agent-service-run
just web-dev
```

Or run `just dev` to start PostgreSQL, Redis, migrations, the API, worker, dispatcher, Pi Agent, and Web together. It stops existing development processes and applies migrations; check other tasks' resource use in a shared environment and reuse a suitable running stack when available. The default `mock` provider does not require a real API key.

## Common Checks

Choose checks from the [root verification matrix](AGENTS.md#testing-guidelines). Local logic changes need a regression near the trigger; shared contracts, persistence and cross-module changes need affected reader and writer coverage. Record required checks separately from optional broader validation.

| Scope | Verification entrypoint |
|---|---|
| Local Go fix / full backend gate | [go/AGENTS.md](go/AGENTS.md#tests); the full gate is `just go-test` |
| Local Node.js/Pi fix / full service gate | Focused tests and contract checks in the root rules; the full gate is `just agent-service-test` |
| Local frontend fix / full frontend gate | [web/AGENTS.md](web/AGENTS.md#verification); the full gate includes tests, lint and `just web-build` with bundle budgets |
| Documentation and engineering instructions only | `just docs-check`, relevant references and diff; walk through positive and negative rule triggers, without business builds by default |

Real-provider, capacity and frozen model evaluations apply when required by the task contract or the claim being made, with the appropriate environment, authorization and resource isolation. Report actual commands, results and unverified items. Local checks do not establish a full release pass. Completion criteria are in [AGENTS.md](AGENTS.md#completion).

## Documentation Style

Official docs, release notes, PR descriptions, and contribution guidance should stay concrete and verifiable. Avoid templated delivery copy:

- Do not use empty contrast patterns such as "This is not ..., but ..." or "not ..., but ...".
- Do not use "establishes the main loop" or promotional "first ..., then ..." scaffolding to describe progress.
- Chinese docs should also avoid "这不是……而是……", "不是……而是……", "先把……打通", and promotional "先……再……" scaffolding.
- Keep real technical sequencing when it matters, such as command order, migration steps, auto-save before run, or troubleshooting steps.
- State current facts and verified results; label future direction as unimplemented or planned.

## Code Conventions

- The business backend is vertically sliced under `go/internal/`; conventions live in `go/AGENTS.md`.
- Provider-specific SDK calls stay in `go/internal/providers`; routes should not call providers directly.
- Frontend API requests are centralized in `web/src/lib/api.ts`, and DTO types are centralized in `web/src/lib/types.ts`.
- Database schema changes go through GORM models and constraint patches in `go/internal/platform/db/schema`, and should include regression coverage where practical.
- Changes involving upload, storage, secrets, or provider keys should consider security boundaries first.

## Commits and PRs

Prefer one focused topic per PR. The PR description should include:

- User-visible changes.
- Key implementation notes.
- Whether migrations or configuration changes are included.
- Verification commands run and their results.
- Screenshots or recordings for UI changes, when applicable.

Formal version tags use annotated tags with bilingual Chinese/English messages. The tag message should include release positioning, main contents, verification commands, and explicit boundaries; do not keep one-off release-preparation checklists as repository docs. Suggested format:

```text
ProductFlow vX.Y.Z

中文：
<一句话版本定位>

包含：
- ...

已验证：
- ...

边界：
- ...

English:
<One-sentence release positioning>

Includes:
- ...

Verified:
- ...

Boundaries:
- ...
```

## Engineering Knowledge

`AGENTS.md` holds repository and layer-specific engineering constraints, `CONTEXT.md` records current domain boundaries.
`docs/adr/` is a historical archive. Product requirements and PRDs use GitHub Issues; delegated execution contracts and status live in task files in the [local public task pool](docs/audits/tasks/README.md), with the board as a synchronized index. Direct fixes need no ticket. Git status establishes actual changes and commit ownership.
