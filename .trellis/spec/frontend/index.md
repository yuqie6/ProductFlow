# Frontend Development Guidelines

> Project-specific frontend conventions for ProductFlow.

---

## Overview

These files document the frontend conventions that are actually present in this repository. They are based on `AGENTS.md`,
`web/package.json`, `web/tsconfig*.json`, `web/vite.config.ts`, `justfile`, and the current code under `web/src/`.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | React/Vite app layout, pages, components, lib boundaries | Filled |
| [Component Guidelines](./component-guidelines.md) | Function components, props, Tailwind styling, forms/accessibility | Filled |
| [Hook Guidelines](./hook-guidelines.md) | React Query, mutations/cache updates, polling, local hooks | Filled |
| [State Management](./state-management.md) | Server/local/URL state split and query key conventions | Filled |
| [Quality Guidelines](./quality-guidelines.md) | TypeScript build gate, API centralization, UI review checklist | Filled |
| [Type Safety](./type-safety.md) | Strict TS, DTO mirroring, ApiError, runtime validation reality | Filled |
| [Product Workbench DAG](./product-workbench-dag.md) | Product detail DAG workbench UI, API DTOs, and cache contracts | Filled |

---

## Pre-Development Checklist

Before frontend changes, read:

1. `./directory-structure.md`
2. `./quality-guidelines.md`
3. The topic-specific file for the area you are changing:
   - components/forms: `./component-guidelines.md`
   - hooks/data fetching: `./hook-guidelines.md`
   - state/cache behavior: `./state-management.md`
   - API DTOs/types: `./type-safety.md`
   - product workbench DAG: `./product-workbench-dag.md`

If a frontend change consumes or changes backend API contracts, also read `../backend/error-handling.md`,
`../backend/database-guidelines.md`, or `../backend/directory-structure.md` as relevant.

---

**Language**: All documentation in this directory is written in English.
