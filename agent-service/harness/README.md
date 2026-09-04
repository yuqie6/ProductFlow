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

## Evolution Diagnostics

`AGENT_EVOLUTION_TRACES=1` enables structural diagnostics for claimed `TurnRuntime.execute` attempts. The default is `0`; disabled collection creates no trace files and does not inspect event content. Enabling requires an absolute `STORAGE_ROOT` (the existing dev-env wrapper resolves it). Files go under `STORAGE_ROOT/agent-evolution-traces/`. Compose mounts a dedicated `productflow-agent-traces` volume at that path with the Agent's UID 10001 ownership, without mounting the product media volume. Use one Agent writer per trace directory; this collector does not implement multi-process coordination.

`src/evolution-traces.ts` owns projection, a separate asynchronous write queue and retention. The runtime never awaits trace writes in model, tool or journal paths. Limits are fixed outside the editable harness: 4 KiB per record, 64 KiB per attempt file, 128 pending records including the in-flight write, and 256 owned files with a 16 MiB reserved-byte ceiling. Active files are not evicted; a full directory with only active attempts rejects new traces. Closed and restart-leftover files are evicted oldest first. Files use mode 0600 and newly created directories use 0700. Shutdown drains the diagnostic queue after executions stop; abrupt crashes can lose queued records.

Records contain the frozen harness/Skill hashes, hashed Turn and call correlation keys, attempt/fence, effective provider/model identifiers, model finish reason/token counts, allowlisted tool and operation names, revision and option/operation counts, and acknowledged terminal status. At most 32 operation names are retained with `ops_truncated`; arbitrary parameters, user/assistant text, tool results, URLs, credentials, paths, media and reasoning content are omitted. These structural records are not a replayable transcript or an anonymization guarantee. Miners must not infer intent or factual correctness from them alone.

A valid complete attempt requires its header, sequential records, and `attempt_end.complete=true` with no dropped records. `complete` describes collection completeness, not task success. Missing footer, malformed partial JSON, sequence gaps, I/O errors or `complete=false` mean incomplete evidence. No observed terminal remains null. `/healthz.evolution_traces` reports `enabled`, pending/dropped records, I/O errors and evictions without raw error text. Trace failures never become business failures. No exporter, external telemetry backend, Miner or playbook consumer is enabled.

Tests: `src/evolution-traces.test.ts`, `src/config.test.ts`, and the off/on/I/O-failure cases in `src/pi-runtime.e2e.test.ts`. Field selection follows the separation of operational metadata from opt-in sensitive content in the [OpenTelemetry GenAI conventions](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-spans.md); no OpenTelemetry exporter or schema compliance is claimed.
