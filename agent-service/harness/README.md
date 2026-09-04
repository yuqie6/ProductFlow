# ProductFlow Harness Artifact

`src/harness.ts` loads this directory into `DEPLOYED_HARNESS` at module startup. The Pi adapter, model checkpoints, health response and Node eval runners share that frozen artifact, its SHA-256 hash and system prompt. Missing files and unknown manifest fields fail startup. There is no runtime reload or promotion path.

## Editable Surface

The behavior files are `instructions/bootstrap.md`, `instructions/execution.md`, `instructions/verification.md` and `instructions/failure-recovery.md`. P1 moves the existing behavior paragraphs without rewriting them.

`manifest.json` declares empty `skill_overlays` and `runtime_control` objects. P1 rejects nonempty values: overlays and runtime controls have no execution semantics yet. Future stages must declare and test their allowed fields before using them. Controller budgets, scoring rules, lineage and acceptance thresholds never belong to these editable surfaces.

## Frozen Surface

`go/prompts/agent/runtime-policy.md` owns business authority, scope, available-tool boundaries, the question protocol and user confirmation. The existing generator bundles it into `src/runtime-policy.generated.ts`. The manifest pins the digest of that bundled text. Changing either the policy without its reviewed pin or the pin without the policy makes loading fail. The manifest schema, pin, loader and tool schemas are not candidate-editable files.

## Identity

The artifact contains schema version, frozen-policy digest, all four instruction bodies, empty overlays and empty runtime control. Its hash is SHA-256 over `canonicalJSON(artifact)` from `src/tool-manifest.ts`: object keys are sorted recursively; arrays retain order; strings retain exact UTF-8 content, including newlines. Paths, checkout location and manifest key order do not enter the hash.

Base Skill content retains the existing `SkillCatalog.hash`; it is not an overlay or part of this artifact hash. New `before_model_request` checkpoints carry `harness_hash`; Go validates canonical lowercase SHA-256 and stores it in `agent_model_invocations` in the same transaction. Replays cannot change that identity. Historical invocation rows remain NULL without backfill. `/healthz` reports the loaded hash; L1/L3/L5 `run.json` uses the same process object, while the Go L2 runner reads the hash from its spawned Pi process. Lineage, candidate evolution and measured quality gains are not claimed.

Deployment requires `just go-migrate` to add the nullable column and coordinated deployment of the Go checkpoint validator and Node sender. An old Node sender without `harness_hash` is rejected; drain existing Turns before that deployment. No shared development services are restarted by these tests.

## Verification

`harness/harness.test.ts` runs in `just agent-service-test`. It checks canonical identity, each behavior section, immutable snapshots, the frozen policy pin, rejected inactive fields and missing artifacts. The Pi adapter boundary test is `src/pi-runtime-harness.test.ts`.

Attribution checks: `src/pi-runtime.e2e.test.ts`, `src/server.test.ts`, `evals/harness-attribution.test.ts`, `evals/run-storage.test.ts`, and `go/internal/agent/harness_attribution_test.go` cover the emitted checkpoint, health, runner metadata, persisted file, HTTP-to-DB identity, replay conflicts and nullable schema addition.
