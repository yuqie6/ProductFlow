import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, it, vi } from "vitest";
import { API_VERSION, type TurnState } from "../src/contracts.js";
import { PiRuntimeManager } from "../src/runtime-manager.js";
import { TurnStore } from "../src/store.js";
import { runLiveEvals } from "./live-runner.js";
import { loadEvalTaskSet } from "./loader.js";
import * as stubWorld from "./stub-world.js";

const roots: string[] = [];
afterEach(async () => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  for (const root of roots.splice(0)) await rm(root, { recursive: true, force: true });
});

it.each(["observed", "missing", "unknown_terminal"])("persists actual observability independently of the model terminal (%s)", async (mode) => {
  const missing = mode === "missing";
  const unobservable = mode !== "observed";
  const root = await mkdtemp(join(tmpdir(), "productflow-live-observability-"));
  roots.push(root);
  vi.stubEnv("STORAGE_ROOT", root);
  vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
  vi.stubEnv("AGENT_PROVIDER_API_KEY", "not-used");
  vi.stubEnv("PRODUCTFLOW_AGENT_EVAL_FILTER", "");
  vi.stubEnv("PRODUCTFLOW_AGENT_EVAL_SUITE", "");
  const { tasks } = await loadEvalTaskSet();
  const task = tasks.find((task) => task.id === (missing
    ? "graph-editing-dissolve-and-reorder" : "graph-editing-delete-one-node"))!;
  const terminal: TurnState = {
    api_version: API_VERSION, run_id: "fixture", turn_id: "fixture", status: mode === "unknown_terminal" ? "unknown" : "succeeded",
    input: { input_text: task.utterances[0], asset_ids: [], idempotency_key: "fixture", page_context: task.page_context },
    output: "diagnostic output", error: "", tool_steps: [], created_at: "2026-09-05T00:00:00Z",
    updated_at: "2026-09-05T00:00:00Z", started_at: null, finished_at: null,
  };
  const create = stubWorld.createStubWorld;
  let stub: stubWorld.StubWorld;
  vi.spyOn(stubWorld, "createStubWorld").mockImplementation((...args) => (stub = create(...args)));
  vi.spyOn(PiRuntimeManager.prototype, "start").mockImplementation(async () => {
    const params = missing ? {
      base_graph_revision: 3, summary: "dissolve only", operations: [{ op: "dissolve_group", group_ref: "group-main" }],
    } : task.reference.scripted_calls.at(-1)!.params as Parameters<typeof stub.client.applyGraphChangeSet>[1];
    if (missing) await expect(stub.client.applyGraphChangeSet("conv", params, "key")).rejects.toMatchObject({ code: "eval_unobservable" });
    else await stub.client.applyGraphChangeSet("conv", params, "key");
    return terminal;
  });
  vi.spyOn(PiRuntimeManager.prototype, "close").mockResolvedValue();
  vi.spyOn(TurnStore.prototype, "getState").mockResolvedValue(terminal);
  vi.spyOn(TurnStore.prototype, "events").mockResolvedValue([]);
  const result = await runLiveEvals({ tasks: [task], trials: 1, concurrency: 1 });
  const record = JSON.parse((await readFile(join(result.runDir, "trials.jsonl"), "utf8")).trim());
  expect(record.status).toBe(unobservable ? "unobservable" : "succeeded");
  expect(record.terminal).toBe(terminal.status);
  expect(record.tool_calls[0].outcome).toBe(missing ? "unknown" : "succeeded");
  expect(result.metrics.measurementEligible).toBe(!unobservable);
  if (mode === "unknown_terminal") {
    expect(record.passed).toBe(false);
    expect(record.errors).toContain("unobservable terminal outcome: unknown; raw trial is diagnostic only");
  }
  if (missing) {
    expect(record.passed).toBe(false);
    expect(record.errors).toContain("unobservable tool outcome: apply_graph_change_set_v1; raw trial is diagnostic only");
  }
  const transcript = JSON.parse(await readFile(join(result.runDir, record.transcript_path), "utf8"));
  expect(transcript.output).toBe(terminal.output);
  expect(transcript.stub_calls[0].outcome).toBe(missing ? "unknown" : "succeeded");
});
