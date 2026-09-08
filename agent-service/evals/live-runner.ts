/** Opt-in L1 evaluation: production Pi runtime, real model, deterministic ProductFlow stub world. */

import { randomUUID } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import type { Config } from "../src/config.js";
import { DEPLOYED_HARNESS } from "../src/harness.js";
import type { TurnState } from "../src/contracts.js";
import { PiRuntimeManager } from "../src/runtime-manager.js";
import { loadSkillCatalog, type SkillCatalog } from "../src/skills.js";
import { TurnStore } from "../src/store.js";
import { gradeBudget, gradeOperations, gradeTerminal, gradeTools, gradeWrites } from "./graders/index.js";
import type { BudgetExpectation, EvalCallRecord as GraderCallRecord, GradeResult } from "./graders/types.js";
import { formatEvalReport, type EvalReport, type EvalReportRow } from "./harness.js";
import { loadGlobalDraftSchema } from "./json-schema.js";
import { loadEvalTaskSet } from "./loader.js";
import { currentGitProvenance, hashCanonicalJSON } from "./provenance.js";
import { buildRunReport, formatRunReport, isUnobservableTrial, type RunReport } from "./report.js";
import { EvalRunStorage, newEvalRunID, type EvalRunMetadata } from "./run-storage.js";
import type { EvalCallRecord, EvalTask, EvalTrialRecord, EvalWorld } from "./schema.js";
import { createStubWorld, overlayEvalPageContext } from "./stub-world.js";
import { openGoEvalHost } from "./go-world.js";
import { taskSplit } from "./split.js";
import { PRODUCTION_MAX_CONCURRENT_TURNS_DEFAULT, resolveLiveEvalConcurrency } from "./live-concurrency.js";
import { loadCollection, selectCollection } from "./collections.js";

export { PRODUCTION_MAX_CONCURRENT_TURNS_DEFAULT, resolveLiveEvalConcurrency };

export interface LiveEvalOptions {
  trials?: number;
  filter?: string;
  suite?: string;
  concurrency?: number;
  skillRoot?: string;
  layer?: "l1" | "l3" | "l5";
  tasks?: EvalTask[];
  collectionPath?: string;
  collectionPurpose?: string;
}

export interface LiveEvalResult {
  runID: string;
  runDir: string;
  report: EvalReport;
  metrics: RunReport;
}

interface TrialOutcome {
  row: EvalReportRow;
  record: EvalTrialRecord;
}

export function requireLiveEvalEnv(): void {
  if (process.env.PRODUCTFLOW_RUN_AGENT_EVALS !== "1") {
    throw new Error("set PRODUCTFLOW_RUN_AGENT_EVALS=1 to run live agent evals");
  }
  if (!process.env.AGENT_PROVIDER_API_KEY?.trim()) {
    throw new Error("set AGENT_PROVIDER_API_KEY, configure AGENT_PROVIDER_MODEL, then run just agent-evals-live");
  }
}

export async function runLiveEvals(options: LiveEvalOptions = {}): Promise<LiveEvalResult> {
  requireLiveEvalEnv();
  const trials = positiveInteger(options.trials ?? envInteger("PRODUCTFLOW_AGENT_EVAL_TRIALS", 3), "trials");
  const { concurrency, productionMaxConcurrentTurns } = resolveLiveEvalConcurrency(options.concurrency);
  const catalog = await loadSkillCatalog(options.skillRoot);
  const taskSet = await loadEvalTaskSet({ catalog });
  const layer = options.layer ?? "l1";
  if (Boolean(options.collectionPath) !== Boolean(options.collectionPurpose)) throw new Error("Collection path and purpose are both required");
  if (options.collectionPath && (options.tasks || options.filter || options.suite || layer !== "l1"
    || process.env.PRODUCTFLOW_AGENT_EVAL_FILTER?.trim() || process.env.PRODUCTFLOW_AGENT_EVAL_SUITE?.trim())) {
    throw new Error("Frozen collection runs require unfiltered L1 tasks");
  }
  const collection = options.collectionPath
    ? selectCollection(await loadCollection(options.collectionPath, taskSet), options.collectionPurpose!, taskSet)
    : undefined;
  const tasks = collection?.tasks ?? selectTasks(options.tasks ?? taskSet.tasks, options.filter, options.suite, layer);
  const runID = newEvalRunID();
  const storage = new EvalRunStorage(runID);
  const provenance = await currentGitProvenance();
  const metadata: EvalRunMetadata = {
    schema_version: 1,
    run_id: runID,
    created_at: new Date().toISOString(),
    commit: provenance.commit,
    worktree_dirty: provenance.worktree_dirty,
    worktree_hash: provenance.worktree_hash,
    skill_catalog_hash: catalog.hash,
    harness_hash: DEPLOYED_HARNESS.hash,
    task_set_hash: hashCanonicalJSON({
      tasks,
      worlds: Object.fromEntries(taskSet.worlds),
    }),
    model: process.env.AGENT_PROVIDER_MODEL?.trim() || "gpt-4.1",
    provider_kind: process.env.AGENT_PROVIDER_KIND?.trim() || "openai",
    reasoning_effort: process.env.AGENT_PROVIDER_REASONING_EFFORT?.trim() || null,
    trials,
    concurrency,
    production_max_concurrent_turns: productionMaxConcurrentTurns,
    layers: [layer],
    ...(collection ? { collection: collection.identity } : {}),
  };
  await storage.init(metadata);

  const jobs = tasks.flatMap((task) => Array.from({ length: trials }, (_, index) => ({ task, trial: index + 1 })));
  const outcomes = await mapConcurrent(jobs, concurrency, async ({ task, trial }) => {
    const world = taskSet.worlds.get(task.world)!;
    const outcome = await runTrial(runID, task, world, trial, catalog, storage, layer);
    process.stderr.write(
      `${outcome.record.passed ? "pass" : "fail"} ${task.id} trial=${trial} calls=${outcome.row.callCount} tokens=${outcome.row.tokenCount ?? "unavailable"}${outcome.row.error ? ` error=${outcome.row.error}` : ""}\n`,
    );
    return outcome;
  });
  const report: EvalReport = { ok: outcomes.every((outcome) => outcome.row.ok), rows: outcomes.map((outcome) => outcome.row) };
  const metrics = buildRunReport(runID, outcomes.map((outcome) => outcome.record), trials);
  await storage.finish(
    { report: formatEvalReport(report), markdown: formatRunReport(metrics), metrics },
    { successful: report.ok, publishLatest: true },
  );
  return { runID, runDir: storage.runDir, report, metrics };
}

export { formatEvalReport };

async function runTrial(
  runID: string,
  task: EvalTask,
  world: EvalWorld,
  trial: number,
  catalog: SkillCatalog,
  storage: EvalRunStorage,
  layer: EvalTrialRecord["layer"],
): Promise<TrialOutcome> {
  const startedAt = new Date();
  const utterance = task.utterances[(trial - 1) % task.utterances.length];
  const root = await mkdtemp(join(tmpdir(), `productflow-agent-eval-${task.id}-`));
  const conversationID = randomUUID();
  const harnessRunID = randomUUID();
  const draftSchema = task.scope === "global" ? loadGlobalDraftSchema() as Record<string, unknown> : {};
  const store = new TurnStore(root);
  await store.init();
  const stub = createStubWorld(task, world, conversationID, harnessRunID, draftSchema);
  const manager = new PiRuntimeManager(liveConfig(root), store, stub.client, catalog);
  let terminal: TurnState | null = null;
  let tokenCount: number | null = null;
  let errors: string[] = [];
  let events: Awaited<ReturnType<TurnStore["events"]>> = [];
  let observationHost: Awaited<ReturnType<typeof openGoEvalHost>> | undefined;
  let finalObservation: { errors: string[]; state: Record<string, unknown>; readback_errors: string[] } | null = null;
  let finalObservationUnknown = false;
  try {
    if (task.observability_blocker) throw new Error(`unobservable eval input: ${task.observability_blocker}`);
    if (layer === "l1" && !process.env.VITEST) {
      observationHost = await openGoEvalHost(task, stub, {
        layer: "l1",
        overlay: "full",
      });
    }
    const started = await manager.start({
      lookup: { conversationID },
      input: {
        input_text: utterance,
        asset_ids: task.scope === "global" ? [] : task.page_context.selected_asset_ids,
        idempotency_key: `eval-${task.id}-${trial}`,
        page_context: overlayEvalPageContext(task, world),
      },
    });
    terminal = await waitForTerminal(store, harnessRunID, started.turn_id, 180_000);
    events = await store.events(harnessRunID, started.turn_id, 0);
    tokenCount = tokenCountFromEvents(events);
    const calls = mergeToolCalls(terminal, stub.calls);
    errors = gradeTrial(task, world, terminal, calls, tokenCount, Date.now() - startedAt.getTime());
  } catch (error) {
    errors = [errorMessage(error)];
  } finally {
    await manager.close().catch((error) => {
      finalObservationUnknown = true;
      errors.push(`manager close failed: ${errorMessage(error)}`);
    });
    if (observationHost) {
      try {
        finalObservation = await observationHost.observeFinal();
        errors.push(...finalObservation.errors);
        if (finalObservation.readback_errors.length > 0) {
          finalObservationUnknown = true;
          errors.push(...finalObservation.readback_errors.map((detail) => `final readback failed: ${detail}`));
        }
      } catch (error) {
        const message = errorMessage(error);
        finalObservationUnknown = true;
        finalObservation = { errors: [message], state: {}, readback_errors: [message] };
        errors.push(`final observation failed: ${message}`);
      }
    }
    await observationHost?.close().catch(() => undefined);
  }

  const calls = mergeToolCalls(terminal, stub.calls);
  const unobservedTools = calls.filter((call) => call.outcome === "unknown").map((call) => call.name);
  const unobservable = finalObservationUnknown || isUnobservableTrial({
    status: task.observability_blocker ? "unobservable" : terminal?.status ?? "failed",
    terminal: terminal?.status ?? null, tool_calls: calls,
  });
  if (terminal?.status === "unknown") {
    errors.unshift("unobservable terminal outcome: unknown; raw trial is diagnostic only");
  }
  if (unobservedTools.length > 0) {
    errors.unshift(`unobservable tool outcome: ${[...new Set(unobservedTools)].join(", ")}; raw trial is diagnostic only`);
  }
  const durationMS = Date.now() - startedAt.getTime();
  const transcriptPath = await storage.writeTranscript(task.id, trial, {
    schema_version: 1,
    run_id: runID,
    task_id: task.id,
    trial,
    utterance,
    terminal_status: terminal?.status ?? null,
    tool_steps: terminal?.tool_steps ?? [],
    output: terminal?.output ?? "",
    thinking: terminal?.thinking ?? "",
    error: terminal?.error ?? errors.join("; "),
    ...(finalObservation ? { final_observation: finalObservation } : {}),
    events: events.map((event) => ({ sequence: event.sequence, kind: event.kind, created_at: event.created_at })),
    stub_calls: stub.calls,
  });
  const record: EvalTrialRecord = {
    schema_version: 1,
    run_id: runID,
    layer,
    task_id: task.id,
    skill: task.skill,
    suite: task.suite,
    trial,
    split: taskSplit(task),
    utterance,
    started_at: startedAt.toISOString(),
    duration_ms: durationMS,
    status: unobservable ? "unobservable" : terminal?.status ?? "failed",
    passed: errors.length === 0,
    errors,
    terminal: terminal?.status ?? null,
    tool_calls: calls,
    token_count: tokenCount,
    transcript_path: transcriptPath,
  };
  await storage.appendTrial(record);
  await rm(root, { recursive: true, force: true, maxRetries: 8, retryDelay: 50 }).catch(() => undefined);
  return {
    record,
    row: {
      id: `${task.id}#${trial}`,
      skillName: task.skill,
      ok: record.passed,
      callCount: terminal?.tool_steps?.filter((step) => step.tool_name && step.tool_name !== "productflow_context_injection").length ?? 0,
      tokenCount,
      ...(errors.length > 0 ? { error: errors.join("; ") } : {}),
    },
  };
}

function gradeTrial(
  task: EvalTask,
  _world: EvalWorld,
  terminal: TurnState,
  calls: readonly EvalCallRecord[],
  tokenCount: number | null,
  durationMS: number,
): string[] {
  const grades: GradeResult[] = [
    gradeTerminal(task.expect.terminal, terminal.status),
    gradeTools(task.expect.tools, calls as GraderCallRecord[]),
    gradeOperations(task.expect.ops, calls as GraderCallRecord[]),
    gradeWrites(task.expect.writes, calls as GraderCallRecord[]),
    gradeBudget(task.expect.budget as BudgetExpectation | undefined, {
      tool_calls: terminal.tool_steps?.filter((step) => step.tool_name && step.tool_name !== "productflow_context_injection").length ?? 0,
      tokens: tokenCount,
      duration_ms: durationMS,
    }),
  ];
  const question = task.expect.question;
  if (question && typeof question === "object" && (question as { required?: unknown }).required === true && !terminal.question) {
    grades.push({ passed: false, errors: ["expected a user question"] });
  }
  if (task.inject?.first_write_409) {
    const attempts = calls.filter((call) => call.name === task.inject?.first_write_409);
    if (attempts.length !== 2) {
      grades.push({ passed: false, errors: [`${task.inject.first_write_409} used ${attempts.length} attempts after injected 409; expected 2`] });
    } else {
      const lastRevision = valueAtPath(attempts[1].params, "base_graph_revision");
      if (typeof lastRevision !== "number") {
        grades.push({ passed: false, errors: [`retry omitted base_graph_revision after injected 409`] });
      }
    }
  }
  return grades.flatMap((grade) => grade.errors);
}

export function mergeToolCalls(terminal: Pick<TurnState, "updated_at" | "tool_steps" | "question"> | null, recorded: readonly EvalCallRecord[]): EvalCallRecord[] {
  const calls = [...recorded];
  const recordedCounts = new Map<string, number>();
  for (const call of recorded) {
    const key = `${call.name}:${call.outcome}`;
    recordedCounts.set(key, (recordedCounts.get(key) ?? 0) + 1);
  }
  const seenCounts = new Map<string, number>();
  for (const step of terminal?.tool_steps ?? []) {
    const name = step.tool_name;
    if (!name || name === "productflow_context_injection") continue;
    const outcome = step.status === "succeeded" || (name === "ask_user" && terminal?.question)
      ? "succeeded" : step.status === "failed" ? "failed" : "unknown";
    const key = `${name}:${outcome}`;
    const seen = (seenCounts.get(key) ?? 0) + 1;
    seenCounts.set(key, seen);
    if (seen <= (recordedCounts.get(key) ?? 0)) continue;
    calls.push({
      name, params: {}, ts: terminal?.updated_at ?? new Date().toISOString(),
      outcome
    });
  }
  return calls;
}

function selectTasks(tasks: readonly EvalTask[], filterOption?: string, suiteOption?: string, layer = "l1"): EvalTask[] {
  let selected = tasks.filter((task) => task.layers.includes(layer as EvalTask["layers"][number]));
  const suite = suiteOption?.trim() || process.env.PRODUCTFLOW_AGENT_EVAL_SUITE?.trim();
  if (suite) selected = selected.filter((task) => task.suite === suite);
  const rawFilter = filterOption?.trim() || process.env.PRODUCTFLOW_AGENT_EVAL_FILTER?.trim();
  if (rawFilter) {
    const wanted = new Set(rawFilter.split(",").map((item) => item.trim()).filter(Boolean));
    selected = selected.filter((task) => wanted.has(task.id) || wanted.has(task.skill));
  }
  if (selected.length === 0) throw new Error(`Agent eval filter matched no ${layer} tasks`);
  return selected;
}

function liveConfig(dataRoot: string): Config {
  return {
    listenAddress: "127.0.0.1:0",
    dataRoot,
    productFlowBaseURL: "http://127.0.0.1:9",
    internalToken: "0123456789abcdef0123456789abcdef",
    requestTimeoutMS: 5_000,
    providerRequestTimeoutMS: 180_000,
    maxBodyBytes: 96 << 20,
    maxIterations: 12,
    modelContextWindow: 128_000,
    autoCompactTokenLimit: 96_000,
    maxConcurrentTurns: 1,
    providerAPIKey: process.env.AGENT_PROVIDER_API_KEY?.trim() ?? "",
    providerBaseURL: process.env.AGENT_PROVIDER_BASE_URL?.trim() || null,
    providerModel: process.env.AGENT_PROVIDER_MODEL?.trim() || null,
    providerReasoningEffort: process.env.AGENT_PROVIDER_REASONING_EFFORT?.trim() || null,
    providerReasoningSummary: process.env.AGENT_PROVIDER_REASONING_SUMMARY?.trim() || null,
    providerTextVerbosity: process.env.AGENT_PROVIDER_TEXT_VERBOSITY?.trim() || null,
    providerServiceTier: process.env.AGENT_PROVIDER_SERVICE_TIER?.trim() || null,
    questionTimeoutMS: 8_000,
  };
}

async function waitForTerminal(store: TurnStore, runID: string, turnID: string, timeoutMS: number): Promise<TurnState> {
  const deadline = Date.now() + timeoutMS;
  while (Date.now() < deadline) {
    const state = await store.getState(runID, turnID);
    if (["awaiting_confirmation", "succeeded", "failed", "canceled", "unknown", "requires_input"].includes(state.status)) return state;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error("live eval turn did not reach a terminal state");
}

function tokenCountFromEvents(events: Awaited<ReturnType<TurnStore["events"]>>): number | null {
  let total = 0;
  for (const event of events) {
    const usage = event.payload && typeof event.payload === "object"
      ? (event.payload as { usage?: { total_tokens?: unknown } }).usage
      : undefined;
    if (usage && typeof usage.total_tokens === "number" && Number.isFinite(usage.total_tokens)) total += usage.total_tokens;
  }
  return total > 0 ? total : null;
}

async function mapConcurrent<T, R>(items: readonly T[], concurrency: number, work: (item: T) => Promise<R>): Promise<R[]> {
  const results = new Array<R>(items.length);
  let next = 0;
  await Promise.all(Array.from({ length: Math.min(concurrency, items.length) }, async () => {
    while (true) {
      const index = next;
      next += 1;
      if (index >= items.length) return;
      results[index] = await work(items[index]);
    }
  }));
  return results;
}

function envInteger(name: string, fallback: number): number {
  const raw = process.env[name]?.trim();
  return raw ? Number(raw) : fallback;
}

function positiveInteger(value: number, name: string): number {
  if (!Number.isSafeInteger(value) || value < 1) throw new Error(`${name} must be a positive integer`);
  return value;
}

function valueAtPath(value: unknown, path: string): unknown {
  let current = value;
  for (const part of path.replace(/\[(\d+)\]/gu, ".$1").split(".").filter(Boolean)) {
    if (!current || typeof current !== "object") return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return current;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
