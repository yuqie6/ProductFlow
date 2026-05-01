# Contributing to ProductFlow

[中文](CONTRIBUTING.md) | English

Thank you for considering contributing code, documentation, or issue reports to ProductFlow. ProductFlow is currently positioned as an open-source self-hosted project, with priority on local reproducibility, truthful documentation, and clear data/secret boundaries.

## Before You Start

1. Read `README.en.md` to understand the project positioning and local startup flow.
2. Read `docs/PRD.en.md` and `docs/ARCHITECTURE.en.md` to understand the current feature boundaries.
3. If you change the backend, consult `.trellis/spec/backend/`.
4. If you change the frontend, consult `.trellis/spec/frontend/`.
5. Do not commit `.env`, `web/.env`, storage, caches, build outputs, logs, or `.trellis/tasks/` / `.trellis/workspace/`.

## Local Development

```bash
cp .env.example .env
cp .env.dev.example .env.dev
cp web/.env.example web/.env
docker compose up -d
just backend-install
just web-install
just backend-migrate
just backend-run
just backend-worker
just web-dev
```

The default `mock` provider does not require a real API key.

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
- Provider-specific SDK calls should stay in `infrastructure/text` or `infrastructure/image`; routes should not call providers directly.
- Frontend API requests are centralized in `web/src/lib/api.ts`, and DTO types are centralized in `web/src/lib/types.ts`.
- Database schema changes require an Alembic migration and should include regression coverage where practical.
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

## Trellis Directory Notes

The repository keeps `.trellis/spec/`, `.trellis/workflow.md`, and `.trellis/scripts/` as development specifications and task tooling. `.trellis/tasks/` and `.trellis/workspace/` are local task/developer records and should not be committed.
