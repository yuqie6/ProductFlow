import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, it, vi } from "vitest";

import { API_VERSION, type TurnState } from "../src/contracts.js";
import { PiRuntimeManager } from "../src/runtime-manager.js";
import { TurnStore } from "../src/store.js";
import { runLiveEvals } from "./live-runner.js";
import * as goWorld from "./go-world.js";
import { loadEvalTaskSet } from "./loader.js";

vi.mock("./go-world.js", () => ({ openGoEvalHost: vi.fn() }));

const roots: string[] = [];

afterEach(async () => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  for (const root of roots.splice(0)) await rm(root, { recursive: true, force: true });
});

it("keeps the final Go observation before closing the host", async () => {
  const root = await mkdtemp(join(tmpdir(), "productflow-live-runner-observation-"));
  roots.push(root);
  vi.stubEnv("STORAGE_ROOT", root);
  vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
  vi.stubEnv("AGENT_PROVIDER_API_KEY", "not-used");
  vi.stubEnv("VITEST", "");

  const { tasks } = await loadEvalTaskSet();
  const task = tasks.find((candidate) => candidate.id === "graph-editing-rename-node")!;
  const terminal: TurnState = {
    api_version: API_VERSION,
    run_id: "fixture",
    turn_id: "fixture",
    status: "succeeded",
    input: {
      input_text: task.utterances[0],
      asset_ids: [],
      idempotency_key: "fixture",
      page_context: task.page_context,
    },
    output: "diagnostic output",
    error: "",
    tool_steps: [],
    created_at: "2026-09-05T00:00:00Z",
    updated_at: "2026-09-05T00:00:00Z",
    started_at: null,
    finished_at: null,
  };
  const order: string[] = [];
  const host = {
    observeFinal: vi.fn().mockImplementation(async () => {
      order.push("observe");
      return { errors: ["final graph content mismatch"], state: { graph: { revision: 4 } }, readback_errors: [] };
    }),
    observe: vi.fn(),
    close: vi.fn().mockImplementation(async () => {
      order.push("close");
    }),
  } as unknown as Awaited<ReturnType<typeof goWorld.openGoEvalHost>>;
  vi.mocked(goWorld.openGoEvalHost).mockResolvedValue(host);
  vi.spyOn(PiRuntimeManager.prototype, "start").mockResolvedValue(terminal);
  vi.spyOn(PiRuntimeManager.prototype, "close").mockImplementation(async () => {
    order.push("manager-close");
  });
  vi.spyOn(TurnStore.prototype, "getState").mockResolvedValue(terminal);
  vi.spyOn(TurnStore.prototype, "events").mockResolvedValue([]);

  const result = await runLiveEvals({ tasks: [task], trials: 1, concurrency: 1 });
  const record = JSON.parse((await readFile(join(result.runDir, "trials.jsonl"), "utf8")).trim());
  const transcript = JSON.parse(await readFile(join(result.runDir, record.transcript_path), "utf8"));

  expect(goWorld.openGoEvalHost).toHaveBeenCalledWith(task, expect.anything(), { layer: "l1", overlay: "full" });
  expect(order).toEqual(["manager-close", "observe", "close"]);
  expect(host.observeFinal).toHaveBeenCalledTimes(1);
  expect(record.passed).toBe(false);
  expect(record.status).toBe("succeeded");
  expect(record.errors).toContain("final graph content mismatch");
  expect(result.metrics.measurementEligible).toBe(true);
  expect(transcript.final_observation).toEqual({
    errors: ["final graph content mismatch"],
    state: { graph: { revision: 4 } },
    readback_errors: [],
  });
});

it("marks an observation failure unobservable after preserving the terminal outcome", async () => {
  const root = await mkdtemp(join(tmpdir(), "productflow-live-runner-observation-error-"));
  roots.push(root);
  vi.stubEnv("STORAGE_ROOT", root);
  vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
  vi.stubEnv("AGENT_PROVIDER_API_KEY", "not-used");
  vi.stubEnv("VITEST", "");

  const { tasks } = await loadEvalTaskSet();
  const task = tasks.find((candidate) => candidate.id === "graph-editing-rename-node")!;
  const terminal: TurnState = {
    api_version: API_VERSION,
    run_id: "fixture",
    turn_id: "fixture",
    status: "succeeded",
    input: {
      input_text: task.utterances[0],
      asset_ids: [],
      idempotency_key: "fixture",
      page_context: task.page_context,
    },
    output: "diagnostic output",
    error: "",
    tool_steps: [],
    created_at: "2026-09-05T00:00:00Z",
    updated_at: "2026-09-05T00:00:00Z",
    started_at: null,
    finished_at: null,
  };
  const order: string[] = [];
  const host = {
    observeFinal: vi.fn().mockImplementation(async () => {
      order.push("observe");
      throw new Error("database readback unavailable");
    }),
    observe: vi.fn(),
    close: vi.fn().mockImplementation(async () => {
      order.push("close");
    }),
  } as unknown as Awaited<ReturnType<typeof goWorld.openGoEvalHost>>;
  vi.mocked(goWorld.openGoEvalHost).mockResolvedValue(host);
  vi.spyOn(PiRuntimeManager.prototype, "start").mockResolvedValue(terminal);
  vi.spyOn(PiRuntimeManager.prototype, "close").mockImplementation(async () => {
    order.push("manager-close");
  });
  vi.spyOn(TurnStore.prototype, "getState").mockResolvedValue(terminal);
  vi.spyOn(TurnStore.prototype, "events").mockResolvedValue([]);

  const result = await runLiveEvals({ tasks: [task], trials: 1, concurrency: 1 });
  const record = JSON.parse((await readFile(join(result.runDir, "trials.jsonl"), "utf8")).trim());
  const transcript = JSON.parse(await readFile(join(result.runDir, record.transcript_path), "utf8"));

  expect(order).toEqual(["manager-close", "observe", "close"]);
  expect(record.terminal).toBe("succeeded");
  expect(record.status).toBe("unobservable");
  expect(record.passed).toBe(false);
  expect(record.errors).toContain("final observation failed: database readback unavailable");
  expect(result.metrics.measurementEligible).toBe(false);
  expect(transcript.final_observation).toEqual({
    errors: ["database readback unavailable"],
    state: {},
    readback_errors: ["database readback unavailable"],
  });
});
