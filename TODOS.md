# TODOs

## Future: Category Playbook Admin UI

**What:** Add an admin UI for editing DB-backed category playbooks.

**Why:** Playbooks define buyer objections, required visual proof, risky claims, tone, image sequence, and platform notes. Once the schema stabilizes through real seller use, runtime editing will let the single admin tune category behavior without a code deploy.

**Pros:** Faster playbook iteration, easier category tuning, less need for migrations/deploys for wording changes, and clearer visibility into what the generator believes about each category.

**Cons:** Adds CRUD, validation UX, seed/update semantics, and risk of bad runtime edits degrading generated kits. It should not ship before the v1 playbook schema is proven with real products.

**Context:** `/plan-eng-review` D4 chose DB-backed CategoryPlaybooks from day one, but D12 deferred admin CRUD. v1 should seed and validate starter playbooks for fashion, cosmetics/beauty, electronics accessories, home goods, food, and other/custom. Start later from `backend/src/productflow_backend/application/launch_kit/playbooks.py`, the CategoryPlaybook model/migration, and the Store Profile/settings UI patterns.

**Depends on / blocked by:** LaunchKit CategoryPlaybook model, seed data, payload schema versioning, and at least one validation cycle with real seller SKUs.
