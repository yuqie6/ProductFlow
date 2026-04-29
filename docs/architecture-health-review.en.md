# ProductFlow Architecture Health Review

[中文](architecture-health-review.md) | English

> Review date: 2026-04-26
> Scope: current repository static structure, real file sizes, current layering, and test/build configuration.
> Purpose: guide later staged refactoring tasks; this document does not perform any code refactoring.

## 1. Overall Score and Conclusion

**Overall health: 7.0 / 10.**

ProductFlow's foundational architecture direction is healthy: the backend already has a FastAPI presentation / application / domain / infrastructure four-layer structure; external dependencies such as providers, storage, queue, and runtime settings are mostly isolated in infrastructure; the frontend uses React + TypeScript strict + TanStack Query, with API and DTOs centralized through `web/src/lib/api.ts` and `web/src/lib/types.ts`. The current system can support fast product iteration.

The main architecture debt is not "wrong direction", but **oversized hot modules, a thin domain layer, and overly concentrated test/error boundaries**. These issues do not block features immediately, but they will keep increasing iteration cost for the DAG workbench, image sessions, provider expansion, error classification, and frontend interaction regressions.

The recommended route is **incremental modularization**: first add test/quality guardrails and error-type boundaries, then split the hottest backend workflow and frontend ProductDetail workbench modules, and finally introduce repository/domain service abstractions according to real business hotspots. Do not rewrite the whole project domain model or migration history in one pass.

## 2. Review Method and Evidence Sources

This review is based on live inspection of the current repository, not generic architecture advice. Main commands used:

```bash
git status --short
find backend/src/productflow_backend backend/tests -type f -name '*.py' -print0 | xargs -0 wc -l | sort -nr
find web/src -type f \( -name '*.ts' -o -name '*.tsx' \) -print0 | xargs -0 wc -l | sort -nr
find web -maxdepth 4 -type f \( -name '*test*' -o -name '*spec*' -o -name 'eslint.config.*' -o -name '.eslintrc*' -o -name '.prettierrc*' \)
rg -n "from sqlalchemy\.orm import Session|productflow_backend\.infrastructure" backend/src/productflow_backend/application backend/src/productflow_backend/presentation/routes
jq '.scripts' web/package.json
```

The worktree baseline during review was recorded as clean by the Trellis PRD. The visible change at that time only added this document and did not modify backend, frontend, or migration code.

## 3. Current Structure Snapshot

### Backend

| Area | Real Evidence | Observation |
| --- | ---: | --- |
| Backend Python file count | 57 `.py` files across `backend/src/productflow_backend` + `backend/tests` | Monorepo size is still manageable |
| Backend source line-count sample | About 10,892 Python lines across backend source + tests | Feature density is concentrated in a few hot files |
| Presentation module count | 21 `.py` files under `backend/src/productflow_backend/presentation/` | Routes/schemas have basic separation |
| Infrastructure module count | 21 `.py` files under `backend/src/productflow_backend/infrastructure/` | Provider, storage, queue, and DB have adapter layers |
| Application module count | 7 `.py` files under `backend/src/productflow_backend/application/` | Business orchestration is visibly concentrated |
| Domain module count | Mainly `enums.py` (84 lines) and empty `__init__.py` under `backend/src/productflow_backend/domain/` | Domain layer is mostly shared enums |
| Alembic migrations | 12 migrations under `backend/alembic/versions/` | Iteration is frequent but migration discipline exists |

### Frontend

| Area | Real Evidence | Observation |
| --- | ---: | --- |
| Frontend source file count | 26 `.ts` / `.tsx` / `.css` files under `web/src` | File count is small, but pages carry heavy responsibilities |
| Frontend source line-count sample | About 6,276 TypeScript/TSX lines under `web/src` | `ProductDetailPage.tsx` is too dominant |
| Top-level directories | `web/src/components`, `web/src/lib`, `web/src/pages`, `web/src/pages/product-detail` | Page-local splitting for product detail has started |
| package scripts | `web/package.json` only has `dev` / `build` / `preview` | No frontend lint, format, or test scripts |
| Test/format configuration | No `*test*` / `*spec*` / `eslint.config.*` / `.prettierrc*` found | Frontend automated regression coverage is almost absent |

## 4. Existing Strengths

1. **Backend layering direction is correct**
   - `backend/src/productflow_backend/presentation/`, `application/`, `domain/`, and `infrastructure/` are organized by responsibility.
   - `backend/src/productflow_backend/main.py` only exposes the app entrypoint; actual assembly is in `presentation/api.py`.

2. **External dependencies have infrastructure boundaries**
   - Text providers live under `backend/src/productflow_backend/infrastructure/text/`.
   - Image providers live under `backend/src/productflow_backend/infrastructure/image/`.
   - Storage boundary is in `backend/src/productflow_backend/infrastructure/storage.py`.
   - Queue / recovery boundary is in `backend/src/productflow_backend/infrastructure/queue.py`.

3. **Async job semantics already value recoverability**
   - Traditional `JobRun` and `WorkflowRun` both use the database as authoritative state; Redis/Dramatiq only dispatches.
   - `backend/src/productflow_backend/infrastructure/queue.py` includes unfinished job/workflow recovery entrypoints.
   - Backend spec already defines contracts for active run uniqueness, enqueue failure marking failed state, duplicate message no-op, and similar behavior.

4. **API DTOs and frontend types are centralized**
   - Backend response/request schemas live under `backend/src/productflow_backend/presentation/schemas/`.
   - Frontend DTOs live in `web/src/lib/types.ts`, and the API client lives in `web/src/lib/api.ts`.
   - This provides a reliable boundary for later page splitting and backend application refactoring.

5. **Backend regression coverage is already strong**
   - `backend/tests/test_workflow.py` covers auth, settings, workflow DAG, image sessions, provider payloads, Alembic upgrade, queue recovery, and other key paths.
   - Although the test file is too large, the business protection surface is real.

## 5. Issue Severity Table

| ID | Severity | Issue | Main Evidence | Impact | Recommended Priority |
| --- | --- | --- | --- | --- | --- |
| A1 | P1 | Backend workflow application mega-module | `application/product_workflows.py` has 1,675 lines and about 55 top-level class/function declarations | Large regression surface when changing DAG logic; hard to locate state/execution/artifact boundaries | Phase 1 |
| A2 | P1 | Frontend ProductDetail workbench page is too large | `web/src/pages/ProductDetailPage.tsx` has 2,430 lines and about 25 top-level components/functions/constants | Canvas, sidebar, inspector, gallery, and mutation state are coupled; interaction regression risk is high | Phase 1 |
| A3 | P1 | Frontend lacks automated tests and lint/format gates | `web/package.json` only has `dev/build/preview`; no tests or ESLint/Prettier config found | UI refactors lack behavior guardrails; review depends on humans and `tsc` | Phase 1 |
| A4 | P2 | Domain layer is anemic; business rules are scattered in application | `domain/enums.py` has 84 lines; application directly implements many DAG/copy/image rules | Rule reuse and error-type upgrades are difficult; application files keep growing | Phase 2 |
| A5 | P2 | Application directly depends on SQLAlchemy Session and infrastructure details | `application/use_cases.py`, `image_sessions.py`, and `product_workflows.py` import `Session` and infrastructure adapters | Unit tests must think in DB/adapter terms; persistence/provider boundaries are hard to replace | Phase 2 |
| A6 | P2 | Error handling depends on Chinese string suffix checks | `presentation/errors.py:8-14` uses `detail.endswith("不存在")` to decide 404 | Copy changes can alter HTTP semantics; multi-language/error-code expansion is fragile | Phase 1-2 |
| A7 | P2 | Backend tests cover a lot but are concentrated in one giant file | `backend/tests/test_workflow.py` has 3,256 lines and about 63 test functions | New regression tests are hard to place; partial runs are slower; helper reuse boundaries are weak | Phase 2 |
| A8 | P3 | Alembic migrations are frequent; release stabilization will need a strategy | 12 revisions under `backend/alembic/versions/`, including several DAG fix migrations on 2026-04-24 | Acceptable now; before release, clarify historical compatibility and fresh install quality | Phase 3 |
| A9 | P3 | `web/src/pages/product-detail/` has started splitting but remains utility-heavy | The directory has `workflowConfig.ts`, `galleryImages.ts`, download components, etc., but the core components remain in the main page | Direction is correct, but stateful UI boundaries are not truly split yet | Handle with A2 |

> There is no P0 right now: no architecture issue was found that immediately makes the system unrunnable or corrupts data. The risks are mainly maintainability and future refactoring cost.

## 6. Key Issue Details

### A1. `product_workflows.py` Carries Too Many Responsibilities

**Evidence**

- `backend/src/productflow_backend/application/product_workflows.py`: 1,675 lines.
- About 55 top-level declarations, covering CRUD, node copy edit, image upload/bind, edge create/delete, run kickoff, run execution, node execution, concurrency, context collection, reference fill, and history query.
- The same file contains public use cases such as `start_product_workflow_run(...)` and many private execution details such as `_execute_image_generation(...)`, `_collect_incoming_context(...)`, and `_fill_reference_node(...)`.

**Impact**

- DAG rules, execution recovery, artifact writeback, and provider input construction affect each other inside one file.
- Adding node-level retry, node duplication, more provider parameters, or run logs will likely keep growing the same module.
- During review, it is hard to tell whether a change modifies "graph persistence", "run scheduling", or "node business execution".

**Recommended plan**

Do not change external APIs or database schema first. Split the file into low-risk modules under the same application package:

```text
application/product_workflows.py              # keep public use-case facade, import submodules
application/product_workflow_mutations.py     # node/edge/upload/bind/copy edit/delete
application/product_workflow_runs.py          # kickoff, active run, enqueue failure, run history
application/product_workflow_execution.py     # execute run/node, claim, failure transition
application/product_workflow_context.py       # incoming context, product context, reference assets
application/product_workflow_artifacts.py     # SourceAsset/PosterVariant/CopySet writeback and slot fill
```

Move only one responsibility category at a time, keeping public function names and route imports unchanged. The first pass can extract `context` and `artifacts`, because they usually affect API entrypoints the least.

**Acceptance**

- `backend/src/productflow_backend/application/product_workflows.py` drops below 600 lines and serves as a facade/aggregation entrypoint.
- Route imports remain unchanged, or change only once in an easily reviewable way.
- `uv run --directory backend ruff check .` passes.
- `just backend-test` passes, especially workflow DAG, selected-node run, image-generation, and queue recovery tests.

**Risk and rollback**

- Risks: circular imports, helper visibility changes, and accidentally changing SQLAlchemy relationship stale behavior.
- Reduce risk by moving functions only: no renames, no logic changes; each PR/commit splits one responsibility area.
- Rollback is direct because schema and APIs do not change: revert the split commit.

### A2. `ProductDetailPage.tsx` Is the Biggest Frontend Iteration Bottleneck

**Evidence**

- `web/src/pages/ProductDetailPage.tsx`: 2,430 lines, the largest frontend file.
- Top-level declarations include canvas path logic, pan/zoom guard, sidebar tab, runs panel, image preview modal, images panel, workflow node card, inspector panels, multiple concrete inspectors, `TextArea`, and more.
- `web/src/pages/product-detail/` already exists as a local split directory, but the main page still holds most stateful UI and component implementations.

**Impact**

- Workbench canvas drag, edge connection, right-side tabs, image preview, node inspectors, autosave/run mutation, and cache state are coupled in one file.
- Without frontend tests, any split can introduce regressions such as drag snapping back, polling stopping, cache not refreshing, or download/fill misfires.
- New features will likely keep being appended to the main page, compounding technical debt.

**Recommended plan**

Split into page-local directories first instead of global `components/`, avoiding premature generalization:

```text
web/src/pages/product-detail/
  canvas/
    WorkflowCanvas.tsx
    WorkflowNodeCard.tsx
    edges.ts
    pointerGuards.ts
    zoom.ts
  sidebar/
    ProductDetailSidebar.tsx
    RunsPanel.tsx
    ImagesPanel.tsx
    ImagePreviewModal.tsx
  inspectors/
    InspectorPanel.tsx
    ProductContextInspector.tsx
    ReferenceImageInspector.tsx
    CopyNodeInspector.tsx
    ImageGenerationInspector.tsx
  mutations/
    workflowCache.ts
    useWorkflowMutations.ts   # extract only after real reuse appears
```

First extract pure display or low-state components such as `RunsPanel`, `ImagePreviewModal`, `TextArea`, and some inspectors. Extract canvas pointer/zoom logic in a second pass.

**Acceptance**

- `ProductDetailPage.tsx` drops below 1,500 lines in the first stage, with a final target below 900 lines.
- `just web-build` passes.
- Manual or future automated checks verify: node dragging does not snap back, wheel zoom coordinates are correct, "run selected" saves draft first, workflow completion refreshes product/history/list, and image download/fill does not trigger node selection/drag.

**Risk and rollback**

- Risks: extracted components may receive too many props and become fake splits; stale closures may affect mutations/cache.
- Reduce risk by extracting presentational components first, then hooks.
- Rollback by component split commit; backend data is unaffected.

### A3. Frontend Quality Gates Are Insufficient

**Evidence**

- `web/package.json` scripts only contain:
  - `dev`: `vite`
  - `build`: `tsc --noEmit -p tsconfig.app.json && tsc --noEmit -p tsconfig.node.json && vite build`
  - `preview`: `vite preview`
- No `eslint.config.*`, `.eslintrc*`, `.prettierrc*`, `*test*`, or `*spec*` was found.

**Impact**

- TypeScript catches type errors, but not hook dependency issues, accessibility issues, unused-variable style, formatting drift, or interaction regressions.
- ProductDetail splitting lacks minimal regression tests, concentrating risk in manual acceptance.

**Recommended plan**

Add lightweight gates first; do not introduce a heavy toolchain all at once:

1. Configure ESLint with React hooks, TypeScript, and basic import rules.
2. Configure Prettier or clearly choose ESLint formatting, avoiding tool rule conflicts.
3. Introduce minimal Vitest + Testing Library examples, prioritizing pure helpers and a few key UI areas:
   - `galleryImages.ts` deduplication logic.
   - `imageDownloads.ts` filename/URL logic.
   - workflow polling / active run helpers if extracted into pure functions.
4. Keep canvas pointer behavior as a manual checklist for now; add higher-cost tests after component boundaries stabilize.

**Acceptance**

- `web/package.json` adds `lint`, `format:check`, or equivalent scripts.
- Local/CI quality commands include at least `just web-build` plus frontend lint.
- 3-5 low-cost unit tests cover product-detail pure functions.

**Risk and rollback**

- Risk: introducing lint for the first time may create large style noise.
- Reduce risk by starting with permissive rules and fixing only low-risk findings in the first PR.
- Rollback is independent: config and scripts can be reverted without affecting runtime features.

### A4/A5. Domain Layer and Repository Abstractions Should Be Introduced Incrementally

**Evidence**

- `backend/src/productflow_backend/domain/enums.py` has 84 lines; the domain layer mainly holds enum values.
- `application/use_cases.py`, `application/image_sessions.py`, and `application/product_workflows.py` directly import SQLAlchemy `Session` and `productflow_backend.infrastructure.*`.
- The current application layer implements business rules, ORM queries, relationship choices, adapter construction, and some runtime configuration reading.

**Impact**

- Current tests mostly use DB fixtures / TestClient; pure rule-level unit tests are hard to add.
- If object storage, multi-provider capability probing, or multi-user permissions are added later, the application dependency surface will keep growing.

**Recommended plan**

Do not do a one-shot "Clean Architecture rewrite". Extract small boundaries by hotspot:

1. **Domain errors**: introduce typed business exceptions first, replacing string suffix checks.
2. **Workflow domain helpers**: extract pure graph rules to the domain/application boundary, such as cycle check, target counting, and execution plan selection.
3. **Repository protocol for one hotspot only**: for example, introduce `WorkflowRepository` or an internal query service for workflow run/node queries, keeping the SQLAlchemy implementation.
4. **Provider dependency injection**: add replaceable factory parameters for hard-to-test provider/renderer construction, reducing monkeypatch surface.

**Acceptance**

- New pure rule modules have independent unit tests without FastAPI TestClient.
- Application public use-case signatures stay as stable as possible.
- During migration, not every use case must go through a repository; the new abstraction only needs to prove value in one business hotspot.

### A6. Error Handling Depends on String Suffixes and Is Semantically Fragile

**Evidence**

`backend/src/productflow_backend/presentation/errors.py:8-14`:

```python
def raise_value_error_as_http(exc: ValueError) -> NoReturn:
    detail = str(exc)
    if detail == "海报文件不存在":
        raise HTTPException(status_code=400, detail=detail) from exc
    if detail.endswith("不存在"):
        raise HTTPException(status_code=404, detail=detail) from exc
    raise HTTPException(status_code=400, detail=detail) from exc
```

**Impact**

- Text changes can change HTTP status.
- The same Chinese message may need different statuses in the future, which is hard to express.
- The frontend only receives `ApiError(status, detail)` and has no stable business error code.

**Recommended plan**

Two steps:

1. Introduce minimal typed exceptions:

```python
class BusinessError(ValueError):
    status_code: int = 400
    code: str = "business_error"

class NotFoundError(BusinessError):
    status_code = 404
    code = "not_found"
```

2. Make `raise_value_error_as_http(...)` support `BusinessError` first, while retaining the old `ValueError` fallback for a while. Gradually migrate `_get_*_or_raise` and explicit business failures to typed errors.

**Acceptance**

- Routes still return `{"detail": "..."}` and do not break the frontend.
- New tests cover typed not found -> 404, ordinary business error -> 400, and old `ValueError` fallback.
- New branches depending on `endswith("不存在")` are forbidden.

### A7. Backend Tests Need Splitting Without Weakening Workflow Protection

**Evidence**

- `backend/tests/test_workflow.py`: 3,256 lines.
- About 56 test functions covering settings, DAG, queue recovery, Alembic upgrade, and several other topics.
- `backend/tests/conftest.py` is only 50 lines, while many helper functions remain in the main test file.

**Impact**

- Adding and locating tests costs more.
- Running a single test category is not intuitive.
- Helper functions are hard to reuse and do not encourage themed fixture boundaries.

**Recommended plan**

Split test files by topic while preserving fixtures and test semantics:

```text
backend/tests/test_auth_settings.py
backend/tests/test_product_workflow_dag.py
backend/tests/test_product_jobs.py
backend/tests/test_image_sessions.py
backend/tests/test_storage_downloads.py
backend/tests/test_logging.py
backend/tests/test_migrations.py
backend/tests/helpers.py
```

Move tests first without changing assertions. Optimize shared fixtures only after the move.

**Acceptance**

- `just backend-test` passes.
- Original `test_workflow.py` drops below 800 lines or disappears.
- Each themed file can run independently, for example `pytest backend/tests/test_product_workflow_dag.py`.

## 7. Recommended Priority Order

1. **Add safety nets first: minimal frontend lint/test gate + backend typed error compatibility layer.**
   - This is insurance for later giant-file splits.
   - Impact is controlled and quickly improves review/regression confidence.

2. **Split low-state components and pure helpers from frontend ProductDetail.**
   - Start with `RunsPanel` / `ImagePreviewModal` / inspector display components.
   - Then split canvas pointer/zoom/stateful mutations.

3. **Split backend `product_workflows.py` context/artifact/execution submodules.**
   - Keep routes and APIs unchanged.
   - Move one responsibility category at a time.

4. **Split backend test files while preserving coverage.**
   - Prefer not to split tests in the same PR as production workflow module splitting, to avoid changing production code and many test paths together.

5. **Introduce domain/repository abstractions by hotspot.**
   - Start with typed errors, pure rules, and workflow repository/query service.
   - Do not make the whole project repository-based at once.

6. **Review migration history and open-source release hygiene before release stabilization.**
   - The current migration count is acceptable. Decide whether to squash or keep the compatibility chain after features stabilize.

## 8. Staged Refactoring Roadmap

### Phase 0: Documentation and Baseline Confirmation (this task)

**Goal**: produce an actionable architecture review without changing code behavior.

**Deliverables**

- `docs/architecture-health-review.md`.
- Trellis PRD checklist marking documentation completion.

**Acceptance**

- The document includes real paths, real line counts, issue severity, and a route forward.
- `git diff --check` passes.
- Changes only include docs / Trellis task checklist.

### Phase 1: Quality Guardrails and Error Boundaries (low risk)

**Suggested tasks**

1. Add minimal ESLint/format check configuration for `web/`.
2. Add Vitest baseline coverage for product-detail pure helpers.
3. Introduce typed business errors in the backend while keeping old `ValueError` fallback.

**Acceptance**

- New commands run reliably locally.
- `just web-build`, frontend lint/test, `uv run --directory backend ruff check .`, and `just backend-test` pass.
- API error response shape remains frontend-compatible.

**Rollback**

- Toolchain configuration and typed error compatibility layer can be reverted independently.
- No schema migration involved.

### Phase 2: Incremental Frontend ProductDetail Split

**Suggested tasks**

1. Split `RunsPanel`, `ImagePreviewModal`, `TextArea`, and inspector components.
2. Split `ImagesPanel` and gallery/fill/download prop boundaries.
3. Split canvas pointer guard, edge path, and zoom pure logic.
4. Consider `useWorkflowMutations` or `useWorkflowCanvas` last, avoiding overly deep hooks at the start.

**Acceptance**

- `ProductDetailPage.tsx` gradually drops below 1,500 lines, with a final target below 900 lines.
- Keep page-local directories; do not move ProductDetail-only components into global `components/`.
- `just web-build` passes; new tests cover pure functions that can be tested.
- Manual acceptance covers canvas drag, zoom, run selected, image download/fill, and active run polling.

**Rollback**

- One commit per component split, so interaction regressions can be rolled back by commit.
- APIs and database are unchanged.

### Phase 3: Backend Workflow Modularization

**Suggested tasks**

1. Extract `product_workflow_context.py`: product context, incoming context, and reference asset input collection.
2. Extract `product_workflow_artifacts.py`: copy/poster/source asset materialization and reference slot fill.
3. Extract `product_workflow_execution.py`: node run claim, execute node/run, and failure transitions.
4. Extract `product_workflow_mutations.py`: node/edge/upload/bind/copy edit/delete.
5. Keep `product_workflows.py` as a facade until route imports stabilize, then reduce further.

**Acceptance**

- Public use-case names and HTTP API behavior remain unchanged.
- `product_workflows.py` drops below 600 lines.
- `uv run --directory backend ruff check .` and `just backend-test` pass.
- Workflow DAG tests still cover selected-node run, multi-target generation, reference fill, enqueue recovery, and duplicate message no-op.

**Rollback**

- Prefer pure moves and avoid mixing behavior changes.
- If circular imports or session lifecycle issues appear, revert the most recent module extraction commit.

### Phase 4: Test Structure Split

**Suggested tasks**

1. Extract `backend/tests/helpers.py`.
2. Split `test_workflow.py` by topic.
3. Optimize fixture names and reuse only after splitting is complete.

**Acceptance**

- `just backend-test` passes.
- Single-topic tests can run independently.
- Test-move PRs do not include production behavior changes.

**Rollback**

- Pure test path moves can be directly reverted.

### Phase 5: Domain Rules and Repository/Query Service Pilot

**Suggested tasks**

1. Extract workflow execution plan, cycle/target rules into pure functions or domain services.
2. Introduce a small repository/query service for workflow query/persistence, first serving the DAG hotspot.
3. Add explicit injection points for provider/renderer dependencies, reducing monkeypatch and global factory coupling.

**Acceptance**

- At least one set of business rules can be unit tested without a database.
- Application public use cases do not become significantly more complex because of abstraction.
- Do not require repository abstraction everywhere; copy the pattern only after value is clear.

**Rollback**

- The abstraction layer must be bounded to one business hotspot. If it only adds navigation cost, roll it back and keep any proven useful pure functions.

## 9. Acceptance Checklist for Later Refactoring Tasks

Every later refactor task should satisfy at least:

- **Behavior unchanged proof**: state whether public APIs, DTOs, or database schema changed; if not, explicitly write "behavior unchanged".
- **Target file metrics**: record key file line counts before and after, such as `ProductDetailPage.tsx`, `product_workflows.py`, and `test_workflow.py`.
- **Tests/build**:
  - Backend code changes: `uv run --directory backend ruff check .` + `just backend-test`.
  - Frontend code changes: `just web-build`; if lint/test is introduced, run those commands too.
  - Docs-only: `git diff --check` + path/line-count sanity check.
- **Key manual acceptance** (when ProductDetail/workflow is involved):
  - Open product detail and load workflow successfully.
  - Dragging nodes does not snap back.
  - Run selected saves current draft first.
  - Active workflow polling refreshes product detail/history/list after terminal state.
  - Image download, preview, and fill do not trigger incorrect node selection or dragging.
- **Rollback path**: state whether direct revert is possible; if there is a migration, explain downgrade/forward-fix strategy.

## 10. Risks

1. **The biggest risk when splitting giant files is "fake refactor + behavior drift"**
   - Moving code while casually changing logic makes regression localization difficult.
   - Each commit should do only one kind of action: move, rename, or behavior fix. Do not mix them.

2. **Frontend splitting can easily create props drilling**
   - If an extracted component has more than 15 props, pause and redesign the boundary.
   - Extract pure display components first, then decide whether to extract hooks.

3. **Repository abstraction too early increases navigation cost**
   - The current project is still single-merchant self-hosted; complex permissions/multi-tenancy are not near-term goals.
   - Pilot only in real complexity hotspots such as workflow DAG, provider, and storage.

4. **Error-type migration must keep API compatibility**
   - The frontend currently depends on `ApiError(status, detail)`.
   - Internal `code` can be added, but do not immediately change response shape unless frontend and tests are updated together.

5. **Do not squash migration history rashly during active iteration**
   - The current 12 migrations are not themselves the issue.
   - Evaluate fresh install and historical DB upgrade maintenance cost before release stabilization.

## 11. Explicit Non-Goals

For this round and the recommended route, these are not goals:

- Do not perform any actual refactor in this task.
- Do not rewrite the whole project domain model at once.
- Do not introduce repositories for every application use case at once.
- Do not change existing database schema or Alembic history.
- Do not change API response shape, DTO field names, or frontend routes.
- Do not introduce Redux/Zustand or another global state library to replace TanStack Query.
- Do not move ProductDetail-specific components prematurely into global `web/src/components/`.
- Do not pause product iteration just because frontend tests are missing; add low-cost guardrails first, then split hotspots.

## 12. Suggested Next Trellis Tasks

1. `frontend-quality-gate`: add ESLint/format check and a few Vitest helper tests for `web/`.
2. `typed-business-errors`: introduce typed business errors in the backend while keeping old `ValueError` mapping compatible.
3. `split-product-detail-low-risk-components`: split ProductDetail runs/images/inspector display components.
4. `split-product-workflow-context-artifacts`: split backend workflow context and artifact helpers.
5. `split-backend-workflow-tests`: split `backend/tests/test_workflow.py` by topic.
