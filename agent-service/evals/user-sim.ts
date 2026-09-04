/** L3 multi-turn user simulator. The agent still uses PiRuntimeManager; answers come from task.user_sim. */

import { randomUUID } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import type { Config } from "../src/config.js";
import type { TurnAnswer, TurnState } from "../src/contracts.js";
import { PiRuntimeManager } from "../src/runtime-manager.js";
import { loadSkillCatalog, type SkillCatalog } from "../src/skills.js";
import { TurnStore } from "../src/store.js";
import { gradeOperations, gradeTerminal, gradeTools, gradeWrites } from "./graders/index.js";
import type { EvalCallRecord as GraderCallRecord } from "./graders/types.js";
import { formatEvalReport, type EvalReport } from "./harness.js";
import { loadGlobalDraftSchema } from "./json-schema.js";
import { loadEvalTaskSet } from "./loader.js";
import { mergeToolCalls, requireLiveEvalEnv, type LiveEvalResult } from "./live-runner.js";
import { currentGitProvenance, hashCanonicalJSON } from "./provenance.js";
import { buildRunReport, formatRunReport } from "./report.js";
import { EvalRunStorage, newEvalRunID, type EvalRunMetadata } from "./run-storage.js";
import type { EvalCallRecord, EvalTask, EvalTrialRecord, EvalUserSim, EvalWorld } from "./schema.js";
import { createStubWorld, overlayEvalPageContext } from "./stub-world.js";

const UNCONFIRMED_WRITE_TOOLS = new Set([
  "apply_graph_change_set_v1",
  "finalize_product_intake_v1",
]);

export interface UserSimGrade {
  passed: boolean;
  unconfirmed_writes: number;
  turns: number;
  errors: string[];
}

export function unconfirmedWriteCount(calls: readonly EvalCallRecord[], userAgreed: boolean): number {
  if (userAgreed) return 0;
  return calls.filter((call) => UNCONFIRMED_WRITE_TOOLS.has(call.name)).length;
}

export function scriptedTurnAnswer(step: EvalUserSim["scripted_answers"][number]): TurnAnswer {
  const selected = step.answer && typeof step.answer === "object"
    ? (step.answer as { selected_option?: unknown }).selected_option
    : undefined;
  if (typeof selected === "number") return { option: selected };
  if (step.text?.trim()) return { text: step.text };
  return { skip: true };
}

export function gradeUserSim(task: EvalTask, args: {
  terminal: string | null;
  calls: readonly EvalCallRecord[];
  turns: number;
  userAgreed: boolean;
}): UserSimGrade {
  const errors: string[] = [];
  const sim = task.user_sim;
  if (!sim) errors.push("task is missing user_sim");
  errors.push(...gradeTerminal(task.expect.terminal, args.terminal ?? "failed").errors);
  errors.push(...gradeTools(task.expect.tools, args.calls as GraderCallRecord[]).errors);
  errors.push(...gradeOperations(task.expect.ops, args.calls as GraderCallRecord[]).errors);
  errors.push(...gradeWrites(task.expect.writes, args.calls as GraderCallRecord[]).errors);
  const unconfirmed = unconfirmedWriteCount(args.calls, args.userAgreed);
  if (unconfirmed > 0) errors.push(`unconfirmed writes: ${unconfirmed}`);
  if (sim && args.turns > sim.max_turns) errors.push(`used ${args.turns} turns; max ${sim.max_turns}`);
  return { passed: errors.length === 0, unconfirmed_writes: unconfirmed, turns: args.turns, errors };
}

export async function runUserSimEvals(options: { trials?: number; filter?: string; concurrency?: number } = {}): Promise<LiveEvalResult> {
  requireLiveEvalEnv();
  const trials = options.trials ?? 1;
  const catalog = await loadSkillCatalog();
  const taskSet = await loadEvalTaskSet({ catalog });
  const tasks = selectSimTasks(taskSet.tasks, options.filter);
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
    task_set_hash: hashCanonicalJSON({ tasks }),
    model: process.env.AGENT_PROVIDER_MODEL?.trim() || "gpt-4.1",
    provider_kind: process.env.AGENT_PROVIDER_KIND?.trim() || "openai",
    reasoning_effort: process.env.AGENT_PROVIDER_REASONING_EFFORT?.trim() || null,
    trials,
    concurrency: 1,
    production_max_concurrent_turns: 3,
    layers: ["l3"],
  };
  await storage.init(metadata);

  const outcomes = [];
  for (const task of tasks) {
    const world = taskSet.worlds.get(task.world)!;
    for (let trial = 1; trial <= trials; trial += 1) {
      const outcome = await runSimTrial(runID, task, world, trial, catalog, storage);
      process.stderr.write(
        `${outcome.record.passed ? "pass" : "fail"} ${task.id} trial=${trial}${outcome.record.errors.length ? ` error=${outcome.record.errors.join("; ")}` : ""}\n`,
      );
      outcomes.push(outcome);
    }
  }
  const report: EvalReport = { ok: outcomes.every((row) => row.ok), rows: outcomes };
  const metrics = buildRunReport(runID, outcomes.map((row) => row.record), trials);
  await storage.finish(
    { report: formatEvalReport(report), markdown: formatRunReport(metrics), metrics },
    { successful: report.ok, publishLatest: false },
  );
  return { runID, runDir: storage.runDir, report, metrics };
}

function selectSimTasks(tasks: readonly EvalTask[], filterOption?: string): EvalTask[] {
  const filter = filterOption?.trim() || process.env.PRODUCTFLOW_AGENT_EVAL_FILTER?.trim();
  const selected = tasks.filter((task) => {
    if (!task.layers.includes("l3") || !task.user_sim) return false;
    if (!filter) return true;
    const wanted = new Set(filter.split(",").map((item) => item.trim()));
    return wanted.has(task.id) || wanted.has(task.skill);
  });
  if (selected.length === 0) throw new Error("Agent eval filter matched no L3 tasks");
  return selected;
}

async function runSimTrial(
  runID: string,
  task: EvalTask,
  world: EvalWorld,
  trial: number,
  catalog: SkillCatalog,
  storage: EvalRunStorage,
): Promise<{ ok: boolean; id: string; skillName: string; callCount: number; tokenCount: number | null; error?: string; record: EvalTrialRecord }> {
  const startedAt = new Date();
  const sim = task.user_sim!;
  const root = await mkdtemp(join(tmpdir(), `productflow-agent-sim-${task.id}-`));
  const conversationID = randomUUID();
  const harnessRunID = randomUUID();
  const store = new TurnStore(root);
  await store.init();
  const stub = createStubWorld(
    task,
    world,
    conversationID,
    harnessRunID,
    task.scope === "global" ? loadGlobalDraftSchema() as Record<string, unknown> : {},
  );
  const manager = new PiRuntimeManager(simConfig(root), store, stub.client, catalog);
  let terminal: TurnState | null = null;
  let turns = 0;
  let userAgreed = false;
  let stepIndex = 0;
  let errors: string[] = [];
  try {
    let utterance = task.utterances[(trial - 1) % task.utterances.length];
    while (turns < sim.max_turns) {
      turns += 1;
      const started = await manager.start({
        lookup: { conversationID },
        input: {
          input_text: utterance,
          asset_ids: task.page_context.selected_asset_ids,
          idempotency_key: `eval-sim-${task.id}-${trial}-${turns}`,
          page_context: overlayEvalPageContext(task, world),
        },
      });
      terminal = await waitForTerminal(store, harnessRunID, started.turn_id, 180_000);
      if (terminal.status === "requires_input") {
        const step = sim.scripted_answers[stepIndex++];
        if (!step || step.when !== "question" || step.action !== "answer") {
          errors.push("expected a scripted question answer");
          break;
        }
        if (!terminal.question?.id) {
          errors.push("requires_input without a question id");
          break;
        }
        await manager.answerQuestion({ conversationID }, started.turn_id, terminal.question.id, scriptedTurnAnswer(step));
        terminal = await waitForTerminal(store, harnessRunID, started.turn_id, 180_000);
        continue;
      }
      if (terminal.status === "awaiting_confirmation") {
        const step = sim.scripted_answers[stepIndex++];
        if (!step) break;
        if (step.action === "confirm") {
          userAgreed = true;
          break;
        }
        if (step.action === "discard") {
          const follow = sim.scripted_answers[stepIndex++];
          if (follow?.action === "follow_up" && follow.text) {
            utterance = follow.text;
            continue;
          }
        }
        break;
      }
      if (terminal.status === "succeeded" || terminal.status === "failed") break;
      break;
    }
    const grade = gradeUserSim(task, {
      terminal: terminal?.status ?? null,
      calls: mergeToolCalls(terminal, stub.calls),
      turns,
      userAgreed,
    });
    errors = [...errors, ...grade.errors];
  } catch (error) {
    errors = [error instanceof Error ? error.message : String(error)];
  } finally {
    await manager.close().catch(() => undefined);
  }
  const durationMS = Date.now() - startedAt.getTime();
  const transcriptPath = await storage.writeTranscript(task.id, trial, {
    schema_version: 1,
    run_id: runID,
    task_id: task.id,
    trial,
    turns,
    stub_calls: stub.calls,
    terminal_status: terminal?.status ?? null,
    output: terminal?.output ?? "",
  });
  const record: EvalTrialRecord = {
    schema_version: 1,
    run_id: runID,
    layer: "l3",
    task_id: task.id,
    skill: task.skill,
    suite: task.suite,
    trial,
    utterance: task.utterances[0],
    started_at: startedAt.toISOString(),
    duration_ms: durationMS,
    status: terminal?.status ?? "failed",
    passed: errors.length === 0,
    errors,
    terminal: terminal?.status ?? null,
    tool_calls: stub.calls,
    token_count: null,
    transcript_path: transcriptPath,
  };
  await storage.appendTrial(record);
  await rm(root, { recursive: true, force: true, maxRetries: 8, retryDelay: 50 }).catch(() => undefined);
  return {
    record,
    ok: record.passed,
    id: `${task.id}#${trial}`,
    skillName: task.skill,
    callCount: stub.calls.length,
    tokenCount: null,
    ...(errors.length > 0 ? { error: errors.join("; ") } : {}),
  };
}

function simConfig(dataRoot: string): Config {
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
    if (["awaiting_confirmation", "succeeded", "failed", "canceled", "unknown", "requires_input"].includes(state.status)) {
      return state;
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error("user-sim turn did not reach a terminal state");
}
