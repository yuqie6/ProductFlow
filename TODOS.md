# TODOs

## Future: Editable Generated LaunchKit Content

**What:** Let sellers edit generated titles, descriptions, hooks, hashtags, and checklist items in the LaunchKit detail page before copying or exporting.

**Why:** v1 generation is deterministic and manual-export first. Real sellers will still need small tone, SKU, promotion, and compliance edits before publishing to Shopee or TikTok Shop.

**Pros:** Makes LaunchKit useful beyond one-shot generation, improves seller trust, and creates cleaner feedback about what sections needed edits.

**Cons:** Adds content persistence, dirty-state UX, export snapshot invalidation, and version/history questions. It should not blur v1 into marketplace API integration.

**Context:** Current LaunchKit detail supports copying generated blocks and Markdown export, but generated content is read-only.

**Depends on / blocked by:** Stable generated content schema and a decision on whether edits overwrite variants or create revision records.

## Future: Store Profile UI and Generator Inputs

**What:** Add a seller/store profile screen and wire StoreProfile defaults into LaunchKit generation.

**Why:** Many constraints repeat across products: shop tone, banned claims, warranty/shipping defaults, target buyer, and preferred CTA patterns.

**Pros:** Less repeated seller input, more consistent generated copy, and a better path toward paid workspace value.

**Cons:** Adds profile setup UX and precedence rules between store defaults, product notes, and category playbooks.

**Context:** StoreProfile is already modeled, but the v1 LaunchKit flow does not expose or consume it.

**Depends on / blocked by:** Real seller validation of which store-level fields matter most.

## Future: AI Provider-backed LaunchKit Generation

**What:** Add optional AI provider generation behind strict structured JSON schemas, safety guardrails, and deterministic fallback.

**Why:** Deterministic v1 proves workflow shape and manual export. Provider-backed generation can produce more varied copy once the schema, guardrails, and feedback loop are stable.

**Pros:** Better content variety, richer buyer angles, and stronger category adaptation.

**Cons:** Adds cost, latency, provider failure modes, prompt-injection risk, JSON validation, and refusal/error UX.

**Context:** Current LaunchKit generation is deterministic by design and already flags oversized/prompt-injection-like reference input.

**Depends on / blocked by:** Stable LaunchKit schemas, validation fixtures, and a clear fallback policy when provider output is malformed or unsafe.

## Future: Category Playbook Admin UI

**What:** Add an admin UI for editing DB-backed category playbooks.

**Why:** Playbooks define buyer objections, required visual proof, risky claims, tone, image sequence, and platform notes. Once the schema stabilizes through real seller use, runtime editing will let the single admin tune category behavior without a code deploy.

**Pros:** Faster playbook iteration, easier category tuning, less need for migrations/deploys for wording changes, and clearer visibility into what the generator believes about each category.

**Cons:** Adds CRUD, validation UX, seed/update semantics, and risk of bad runtime edits degrading generated kits. It should not ship before the v1 playbook schema is proven with real products.

**Context:** `/plan-eng-review` D4 chose DB-backed CategoryPlaybooks from day one, but D12 deferred admin CRUD. v1 seeds and validates starter playbooks for fashion, beauty, electronics accessories, home goods, food, and other/custom. Start later from `backend/src/productflow_backend/application/launch_kit/playbooks.py`, the CategoryPlaybook model/migration, and the Store Profile/settings UI patterns.

**Depends on / blocked by:** LaunchKit CategoryPlaybook model, seed data, payload schema versioning, and at least one validation cycle with real seller SKUs.

## Completed

### LaunchKit Vietnam seller foundation

**Completed:** 2026-06-21

Implemented the reviewed Vietnam seller fork plan: LaunchKit models/API, category playbook seeding, deterministic generation, Markdown/checklist export, inline demo generation, seller feedback, copy actions, input guardrails, export tracking, Vietnamese UI copy, queue recovery, and E2E coverage. The shipped v1 keeps Shopee/TikTok API integration out of scope and preserves `/products` as Advanced Mode.
