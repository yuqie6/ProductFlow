# Backend Development Guidelines

> Project-specific backend conventions for ProductFlow.

---

## Overview

These files document the backend conventions that are actually present in this repository. They are based on
`AGENTS.md`, `backend/pyproject.toml`, `justfile`, `docs/ARCHITECTURE.md`, and the current code under
`backend/src/productflow_backend/` and `backend/tests/`.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | FastAPI/application/domain/infrastructure layout and file placement | Filled |
| [Database Guidelines](./database-guidelines.md) | SQLAlchemy models, sessions, Alembic migrations, runtime settings | Filled |
| [Error Handling](./error-handling.md) | ValueError-to-HTTP mapping, upload errors, auth, queue/provider boundaries | Filled |
| [Quality Guidelines](./quality-guidelines.md) | Ruff/pytest tooling, tests, required/forbidden backend patterns | Filled |
| [Logging Guidelines](./logging-guidelines.md) | Current minimal logging reality and safe logging extension rules | Filled |

---

## Pre-Development Checklist

Before backend changes, read:

1. `./directory-structure.md`
2. `./quality-guidelines.md`
3. The topic-specific file for the area you are changing:
   - database/schema/config: `./database-guidelines.md`
   - API/business failures/uploads: `./error-handling.md`
   - observability/logging: `./logging-guidelines.md`
   - product workbench DAG: `./product-workflow-dag.md`

If a backend change affects frontend API contracts, also read `../frontend/type-safety.md` and
`../frontend/state-management.md`.

---

**Language**: All documentation in this directory is written in English.
