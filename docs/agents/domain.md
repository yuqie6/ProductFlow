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

`docs/adr/` may exist as a historical archive. Skills must not read it to learn current design, and must not create new ADRs.

These files do not need to exist before a skill can run. If they are absent, proceed silently. Producer skills such as `/grill-with-docs` update `CONTEXT.md` when a term is resolved; they do not add ADRs.

## Before exploring, read these

- `CONTEXT.md` at the repo root, if it exists.
- The relevant ownership section in `docs/ARCHITECTURE.md` and package `AGENTS.md` before proposing code changes.
- Live code, tests, and the current diff. They win over any document.

## Use the glossary's vocabulary

When output names a domain concept in an issue title, refactor proposal, hypothesis, or test name, use the term as defined in `CONTEXT.md`.

If the concept is missing from the glossary, either reconsider whether that term belongs in the project language, or update `CONTEXT.md` when the term is actually resolved.

## Current design wins

If a historical ADR, audit note, or old paragraph contradicts live code or `CONTEXT.md` / `ARCHITECTURE.md`, follow the live sources and update the living docs. Do not block a change because it disagrees with an ADR.
