# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Layout

This repo uses a single-context domain documentation layout.

Expected files, when they exist:

- `CONTEXT.md` at the repo root
- `docs/adr/` for architectural decision records
- `docs/ARCHITECTURE.md` for current code ownership and runtime flow
- package `AGENTS.md` files for executable implementation constraints

These files do not need to exist before a skill can run. If they are absent, proceed silently. Producer skills such as `/grill-with-docs` can create them later when real terminology or decisions need to be recorded.

## Before exploring, read these

- `CONTEXT.md` at the repo root, if it exists.
- ADRs under `docs/adr/` that touch the area about to be changed, if any exist.
- The relevant ownership section in `docs/ARCHITECTURE.md` and package `AGENTS.md` before proposing code changes.

## Use the glossary's vocabulary

When output names a domain concept in an issue title, refactor proposal, hypothesis, or test name, use the term as defined in `CONTEXT.md`.

If the concept is missing from the glossary, either reconsider whether that term belongs in the project language, or note it as a gap for `/grill-with-docs`.

## Flag ADR conflicts

If output contradicts an existing ADR, surface it explicitly instead of silently overriding the decision.
