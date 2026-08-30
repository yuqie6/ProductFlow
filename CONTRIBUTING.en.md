# Contributing to ProductFlow

[中文](CONTRIBUTING.md) | English

Thank you for considering contributing code, documentation, or issue reports to ProductFlow. ProductFlow is currently positioned as an open-source self-hosted project, with priority on local reproducibility, truthful documentation, and clear data/secret boundaries.

## Before You Start

1. Read `README.en.md` to understand the project positioning and local startup flow.
2. Read `docs/README.md`, `docs/PRD.en.md`, and `docs/ARCHITECTURE.en.md` to understand documentation ownership and current feature boundaries.
3. If you change the backend, read `go/AGENTS.md`; the sealed Python tree uses `backend/AGENTS.md`.
4. If you change the frontend, read `web/AGENTS.md`.
5. For cross-layer or product-semantic changes, check `CONTEXT.md` and `docs/adr/`; unfinished directions belong in `docs/ROADMAP.md`.
6. Do not commit `.env`, `web/.env`, storage, caches, build outputs, logs, or local database dumps.

## Local Development

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
docker compose up -d productflow-postgres productflow-redis
just backend-install
just agent-service-install
just web-install
just backend-migrate
just backend-run
just backend-worker
just backend-async-dispatcher
just agent-service-run
just web-dev
```

Or run `just dev` to start PostgreSQL, Redis, migrations, the API, worker, dispatcher, Pi Agent, and Web together. The default `mock` provider does not require a real API key.

## Common Checks

For backend changes, run:

```bash
uv run --directory backend ruff check .
just backend-test
```

For frontend changes, run:

```bash
just web-build
```

For documentation or open-source governance file changes, at least confirm that referenced commands, paths, and configuration files exist.

```bash
just docs-check
```

## Documentation Style

Official docs, release notes, PR descriptions, and contribution guidance should stay concrete and verifiable. Avoid templated delivery copy:

- Do not use empty contrast patterns such as "This is not ..., but ..." or "not ..., but ...".
- Do not use "establishes the main loop" or promotional "first ..., then ..." scaffolding to describe progress.
- Chinese docs should also avoid "这不是……而是……", "不是……而是……", "先把……打通", and promotional "先……再……" scaffolding.
- Keep real technical sequencing when it matters, such as command order, migration steps, auto-save before run, or troubleshooting steps.
- State current facts and verified results; label future direction as unimplemented or planned.

## Code Conventions

- Python targets version 3.12, Ruff line width is 120, and lint rules are defined in `backend/pyproject.toml`.
- The backend keeps the `presentation` / `application` / `domain` / `infrastructure` layering.
- Provider-specific SDK calls should stay in `infrastructure/prompt` or `infrastructure/image`; routes should not call providers directly.
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

`AGENTS.md` holds repository and layer-specific engineering constraints, `CONTEXT.md` records current domain boundaries,
and `docs/adr/` records architecture decisions that need durable rationale. GitHub Issues and the actual Git state carry task status.
