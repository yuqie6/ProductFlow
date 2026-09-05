# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Layout

This repo uses a single-context domain documentation layout.

Expected files, when they exist:

- `CONTEXT.md` at the repo root
- `docs/ARCHITECTURE.md` for current code ownership and runtime flow
- `docs/PRD.md` and `docs/USER_GUIDE.md` for current product behavior
- `docs/ROADMAP.md` for unfinished directions
- package `AGENTS.md` files for executable implementation constraints

`docs/adr/` may exist as a historical archive. Read it only for historical questions; do not use it to establish current design or create new ADRs.

These files do not need to exist before a skill can run. If a reference is absent, continue with available evidence and mention the gap only when it affects the result. Domain discussion, including `/grill-with-docs`, remains read-only unless recording is authorized. Resolving a term does not itself authorize updating `CONTEXT.md`.

## Read By Question

- For a domain term or invariant, read the relevant `CONTEXT.md` definition.
- For ownership or runtime flow, read the affected section of `docs/ARCHITECTURE.md`.
- For product behavior, read the affected PRD or user-guide section.
- For implementation work, load applicable package rules and inspect the relevant live code, tests and diff. A local fix does not require reading all domain documents.

Reuse context already read while its relevant inputs remain unchanged. Follow the board's ownership protocol before task-specific exploration when taking a board issue.

## Use the glossary's vocabulary

When output names a domain concept in an issue title, refactor proposal, hypothesis, or test name, use the term as defined in `CONTEXT.md`.

If a concept is missing, first determine whether an existing term fits. Report a proposed definition during discussion; update `CONTEXT.md` only when recording is requested or the authorized implementation requires updating that contract.

## Current design wins

Use live evidence to distinguish actual behavior, intended contract and historical statements. Report discrepancies during read-only work. During authorized fixes, correct the affected implementation or living document based on that distinction; do not turn an implementation defect into a new requirement. A historical ADR does not block an authorized current design change.
