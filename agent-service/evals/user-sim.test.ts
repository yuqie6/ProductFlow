import { afterEach, describe, expect, it, vi } from "vitest";
import { complete, type AssistantMessage } from "@earendil-works/pi-ai/compat";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { API_VERSION, type TurnState } from "../src/contracts.js";
import { PiRuntimeManager } from "../src/runtime-manager.js";
import { TurnStore } from "../src/store.js";
import type { EvalUserSim, EvalCallRecord } from "./schema.js";

import { createModelUser, driveUserSim, gradeUserSim, runUserSimEvals, scriptedTurnAnswer, unconfirmedWriteCount } from "./user-sim.js";
import { loadEvalTaskSet } from "./loader.js";

vi.mock("@earendil-works/pi-ai/compat", async (original) => ({
  ...await original<typeof import("@earendil-works/pi-ai/compat")>(), complete: vi.fn(),
}));
vi.mock("./go-world.js", () => ({ openGoEvalHost: async () => ({
  decide: async (_kind: string, action: string) => ({ id: "approval-1", action, observed: true }), observe: async () => [], close: async () => {},
}) }));

afterEach(() => { vi.unstubAllEnvs(); vi.restoreAllMocks(); vi.clearAllMocks(); });

const simulation: EvalUserSim = {
  persona: "merchant", hidden_goal: "private goal", facts: { title: "private title" },
  policy: "refuse_first_then_accept", max_turns: 4,
  scripted_answers: [
    { when: "question", action: "answer", text: "SCRIPTED FIRST" },
    { when: "question", action: "answer", text: "SCRIPTED SECOND" },
  ],
};

function completion(text: string, stopReason: AssistantMessage["stopReason"] = "stop"): AssistantMessage {
  return {
    role: "assistant", content: [{ type: "text", text }], api: "openai-responses", provider: "openai",
    model: "user-model", stopReason, timestamp: 1,
    usage: { input: 5, output: 7, cacheRead: 0, cacheWrite: 0, totalTokens: 12,
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } },
  };
}

function state(status: TurnState["status"], questionID?: string): TurnState {
  return {
    api_version: API_VERSION, run_id: "run", turn_id: "turn", status,
    input: { input_text: "initial", asset_ids: [], idempotency_key: "key", page_context: null },
    output: "visible agent output", error: "", created_at: "now", updated_at: "now", started_at: null, finished_at: null,
    ...(questionID ? { question: { id: questionID, header: "title", question: "which?", options: [{ label: "A" }] } } : {}),
  };
}

describe("independent model user", () => {
  it("ends an authorization without retroactively invalidating earlier legitimate writes", () => {
    const write: EvalCallRecord = { name: "apply_graph_change_set_v1", params: { operations: [{ op: "rename_node", node_ref: "n", title: "ok" }] }, outcome: "succeeded", ts: "t" };
    const grant = { after_call: 0, before_call: 1, writes: [{ tool: write.name, match: { "operations[0].op": "rename_node", "operations[0].node_ref": "n", "operations[0].title": "ok" } }] };
    expect(unconfirmedWriteCount([write], [grant])).toBe(0);
    expect(unconfirmedWriteCount([write, write], [grant])).toBe(1);
  });
  it("does not authorize a refusal just because it was the last scripted question", async () => {
    const { tasks } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-sim-underspecified-rename")!;
    const authorize = vi.fn();
    await driveUserSim(task.user_sim!, task.utterances[0], {
      start: async () => "turn", answer: vi.fn(), authorize,
      wait: vi.fn().mockResolvedValueOnce(state("requires_input", "q")).mockResolvedValue(state("succeeded")),
      respond: async () => "我不同意，不要修改任何节点",
    });
    expect(authorize).not.toHaveBeenCalled();
    const write: EvalCallRecord = { name: "apply_graph_change_set_v1", params: { operations: [{ op: "rename_node", node_ref: "node-prompt-1", title: "新标题" }] }, outcome: "succeeded", ts: "t" };
    expect(unconfirmedWriteCount([write], [])).toBe(1);
  });
  it("wires the live runner to complete and persists the independent exchange", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-model-user-test-"));
    try {
      vi.stubEnv("STORAGE_ROOT", root);
      vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
      vi.stubEnv("AGENT_PROVIDER_API_KEY", "fake-key");
      vi.stubEnv("AGENT_PROVIDER_KIND", "openai");
      const start = vi.spyOn(PiRuntimeManager.prototype, "start").mockResolvedValue(state("queued"));
      const answer = vi.spyOn(PiRuntimeManager.prototype, "answerQuestion").mockResolvedValue(state("queued"));
      const resume = vi.spyOn(PiRuntimeManager.prototype, "resume").mockResolvedValue(state("running"));
      vi.spyOn(TurnStore.prototype, "getState").mockResolvedValueOnce(state("requires_input", "q1"))
        .mockResolvedValueOnce(state("succeeded"));
      vi.mocked(complete).mockResolvedValue(completion('{"answer_index":0}'));
      const result = await runUserSimEvals({ filter: "graph-editing-sim-underspecified-rename" });
      expect(complete).toHaveBeenCalledTimes(1);
      expect(start).toHaveBeenCalledTimes(1);
      expect(answer).toHaveBeenCalledWith(expect.anything(), "turn", "q1", { text: "主图提示词改成新标题" });
      expect(resume).toHaveBeenCalledWith(expect.anything(), "turn");
      expect(answer.mock.invocationCallOrder[0]).toBeLessThan(resume.mock.invocationCallOrder[0]);
      const rows = (await readFile(join(result.runDir, "trials.jsonl"), "utf8")).trim().split("\n").map((line) => JSON.parse(line));
      expect(rows[0].details.user_sim).toMatchObject({ calls: 1, completed_calls: 1, tokens: 12 });
      const transcript = JSON.parse(await readFile(join(result.runDir, rows[0].transcript_path), "utf8"));
      expect(transcript.user_sim.exchanges[0].answer).toBe("主图提示词改成新标题");
      expect(transcript.turns).toBe(2);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("keeps earlier-turn local tools in the live runner's grade and transcript", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-model-user-tools-test-"));
    try {
      vi.stubEnv("STORAGE_ROOT", root);
      vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
      vi.stubEnv("AGENT_PROVIDER_API_KEY", "fake-key");
      vi.stubEnv("AGENT_PROVIDER_KIND", "openai");
      vi.spyOn(PiRuntimeManager.prototype, "start").mockResolvedValueOnce({ ...state("queued"), turn_id: "first" })
        .mockResolvedValueOnce({ ...state("queued"), turn_id: "second" });
      vi.spyOn(TurnStore.prototype, "getState").mockResolvedValueOnce({
        ...state("awaiting_confirmation"), turn_id: "first", tool_steps: [
          { step_id: "load", kind: "load_skill", status: "succeeded", summary: "loaded", tool_name: "load_productflow_skill" },
        ],
      }).mockResolvedValueOnce({ ...state("awaiting_confirmation"), turn_id: "second" });
      vi.mocked(complete).mockResolvedValue(completion("generated follow-up"));
      const result = await runUserSimEvals({ filter: "graph-editing-sim-proposal-reject-then-accept" });
      const row = JSON.parse((await readFile(join(result.runDir, "trials.jsonl"), "utf8")).trim());
      expect(row.errors).not.toContain("required tool was not called: load_productflow_skill");
      expect(row.tool_calls.filter((call: { name: string }) => call.name === "load_productflow_skill")).toHaveLength(1);
      const transcript = JSON.parse(await readFile(join(result.runDir, row.transcript_path), "utf8"));
      expect(transcript.turn_states.map((turn: { turn_id: string }) => turn.turn_id)).toEqual(["first", "second"]);
      expect(transcript.user_sim.exchanges[0].observation.previous_decision).toBe("discard");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("calls complete for each answer and resumes repeated questions on the same turn", async () => {
    vi.stubEnv("AGENT_PROVIDER_KIND", "openai");
    vi.stubEnv("AGENT_PROVIDER_MODEL", "agent-model");
    vi.stubEnv("AGENT_EVAL_USER_SIM_MODEL", "user-model");
    vi.stubEnv("AGENT_PROVIDER_BASE_URL", "https://private.invalid/v1");
    vi.stubEnv("AGENT_PROVIDER_API_KEY", "secret");
    vi.mocked(complete).mockResolvedValueOnce(completion('{"answer_index":0}')).mockResolvedValueOnce(completion('{"answer_index":1}'));
    const user = createModelUser(simulation);
    const start = vi.fn().mockResolvedValue("original-turn");
    const answer = vi.fn();
    const wait = vi.fn().mockResolvedValueOnce(state("requires_input", "q1"))
      .mockResolvedValueOnce(state("requires_input", "q2")).mockResolvedValueOnce(state("succeeded"));
    const result = await driveUserSim(simulation, "public request", { start, answer, wait, respond: user.respond });
    expect(start.mock.calls).toEqual([["public request"]]);
    expect(wait.mock.calls).toEqual([["original-turn"], ["original-turn"], ["original-turn"]]);
    expect(answer.mock.calls).toEqual([
      ["original-turn", "q1", { text: "SCRIPTED FIRST" }], ["original-turn", "q2", { text: "SCRIPTED SECOND" }],
    ]);
    expect(result.turns).toBe(3);
    expect(complete).toHaveBeenCalledTimes(2);
    expect(vi.mocked(complete).mock.calls[0][0]).toMatchObject({ id: "user-model", api: "openai-responses" });
    const first = JSON.stringify(vi.mocked(complete).mock.calls[0][1]);
    expect(first).toContain("private goal");
    expect(first).toContain("private title");
    expect(first).toContain("SCRIPTED FIRST");
    expect(vi.mocked(complete).mock.calls[0][1].tools).toBeUndefined();
    expect(JSON.stringify(vi.mocked(complete).mock.calls[1][1])).toContain("SCRIPTED FIRST");
    expect(JSON.stringify(user.configuration)).not.toContain("private.invalid");
    expect(JSON.stringify(user.configuration)).not.toContain("secret");
    expect(user.exchanges.map((row) => row.tokens)).toEqual([12, 12]);
  });

  it("uses the agent model by default but makes a separate call", async () => {
    vi.stubEnv("AGENT_PROVIDER_KIND", "openai");
    vi.stubEnv("AGENT_PROVIDER_MODEL", "gpt-4.1");
    vi.stubEnv("AGENT_EVAL_USER_SIM_MODEL", "");
    vi.stubEnv("AGENT_PROVIDER_BASE_URL", "");
    vi.mocked(complete).mockResolvedValue(completion('{"answer_index":0}'));
    const user = createModelUser(simulation);
    await user.respond({ kind: "question", output: "question" });
    expect(complete).toHaveBeenCalledTimes(1);
    expect(vi.mocked(complete).mock.calls[0][0].id).toBe("gpt-4.1");
  });

  it.each(["", "x".repeat(4001)])("rejects invalid model text without using scripted answers", async (text) => {
    vi.stubEnv("AGENT_PROVIDER_KIND", "openai");
    vi.mocked(complete).mockResolvedValue(completion(text));
    const user = createModelUser(simulation);
    const answer = vi.fn();
    await expect(driveUserSim(simulation, "initial", {
      start: vi.fn().mockResolvedValue("turn"), wait: vi.fn().mockResolvedValue(state("requires_input", "q1")), answer, respond: user.respond,
    })).rejects.toThrow("answer is empty or exceeds");
    expect(answer).not.toHaveBeenCalled();
  });

  it.each(["error", "aborted", "length"] as const)("rejects %s completions without retrying or falling back", async (reason) => {
    vi.stubEnv("AGENT_PROVIDER_KIND", "openai");
    vi.mocked(complete).mockResolvedValue(completion("partial", reason));
    const user = createModelUser(simulation);
    await expect(user.respond({ kind: "question", output: "question" })).rejects.toThrow(`did not complete: ${reason}`);
    expect(complete).toHaveBeenCalledTimes(1);
    expect(user.exchanges).toHaveLength(0);
  });

  it("keeps discard/confirm scripted but generates the follow-up with the model", async () => {
    const start = vi.fn().mockResolvedValueOnce("turn1").mockResolvedValueOnce("turn2");
    const answer = vi.fn();
    const respond = vi.fn().mockResolvedValue("model follow-up");
    const result = await driveUserSim({ ...simulation, scripted_answers: [
      { when: "proposal", action: "discard" }, { when: "follow_up", action: "follow_up", text: "script follow-up" },
      { when: "proposal", action: "confirm" },
    ] }, "initial", { start, answer, respond, wait: vi.fn().mockResolvedValue(state("awaiting_confirmation")),
      decide: async (_kind, action) => ({ id: "approval-1", action, observed: true }) });
    expect(start.mock.calls).toEqual([["initial"], ["model follow-up"]]);
    expect(respond).toHaveBeenCalledTimes(1);
    expect(respond).toHaveBeenCalledWith(expect.objectContaining({ previous_decision: "discard" }));
    expect(answer).not.toHaveBeenCalled();
    expect(result.userAgreed).toBe(true);
  });

  it("stops at the interaction budget without answering another question", async () => {
    const respond = vi.fn();
    const result = await driveUserSim({ ...simulation, max_turns: 1 }, "initial", {
      start: vi.fn().mockResolvedValue("turn"), wait: vi.fn().mockResolvedValue(state("requires_input", "q")), answer: vi.fn(), respond,
    });
    expect(result.turns).toBe(1);
    expect(result.terminal.status).toBe("requires_input");
    expect(respond).not.toHaveBeenCalled();
    expect(result.userAgreed).toBe(false);
  });

  it("continues a scripted follow-up after a completed question turn using generated text", async () => {
    const start = vi.fn().mockResolvedValue("turn");
    const result = await driveUserSim({ ...simulation, scripted_answers: [
      { when: "question", action: "answer" }, { when: "follow_up", action: "follow_up", text: "script" },
    ] }, "initial", {
      start, answer: vi.fn(), respond: vi.fn().mockResolvedValueOnce("answer").mockResolvedValueOnce("generated follow-up"),
      wait: vi.fn().mockResolvedValueOnce(state("requires_input", "q"))
        .mockResolvedValueOnce(state("succeeded")).mockResolvedValueOnce(state("succeeded")),
    });
    expect(start.mock.calls).toEqual([["initial"], ["generated follow-up"]]);
    expect(result.turns).toBe(3);
  });
});

describe("L3 user simulator graders", () => {
  it("counts finalize/apply before the user agrees as unconfirmed writes", () => {
    const calls: EvalCallRecord[] = [
      { name: "get_product_workflow_context_v1", params: {}, ts: "t0", outcome: "succeeded" },
      { name: "finalize_product_intake_v1", params: {}, ts: "t1", outcome: "succeeded" },
    ];
    expect(unconfirmedWriteCount(calls, [])).toBe(1);
    const writes = [{ tool: calls[1].name, match: {} }];
    expect(unconfirmedWriteCount(calls, [{ after_call: 2, writes }])).toBe(1);
    expect(unconfirmedWriteCount(calls, [{ after_call: 1, writes }])).toBe(0);
    expect(unconfirmedWriteCount(calls, [{ after_call: 1, writes: [{ tool: calls[1].name, match: { "selection.image_types": [] } }] }])).toBe(1);
  });

  it("maps scripted selected_option answers onto TurnAnswer", () => {
    expect(scriptedTurnAnswer({
      when: "question",
      action: "answer",
      answer: { selected_option: 1 },
    })).toEqual({ option: 1 });
  });

  it("loads five L3 flows with user_sim scripts", async () => {
    const { tasks } = await loadEvalTaskSet();
    const sim = tasks.filter((task) => task.layers.includes("l3"));
    expect(sim.length).toBeGreaterThanOrEqual(5);
    expect(sim.every((task) => task.user_sim && task.user_sim.scripted_answers.length > 0)).toBe(true);
    const intake = sim.find((task) => task.id === "product-intake-sim-two-round-clarify")!;
    const grade = gradeUserSim(intake, {
      terminal: "succeeded",
      calls: intake.reference.scripted_calls.map((call) => ({ ...call, ts: "ref", outcome: "succeeded" })),
      turns: 2,
      authorizations: [{ after_call: 0, writes: intake.expect.writes }],
    });
    expect(grade.unconfirmed_writes).toBe(0);
  });

  it("does not pass a clarified rename without the requested write", async () => {
    const { tasks } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-sim-underspecified-rename")!;
    const calls: EvalCallRecord[] = ["load_productflow_skill", "get_product_workflow_context_v1", "ask_user"]
      .map((name) => ({ name, params: {}, ts: "t", outcome: "succeeded" }));
    expect(gradeUserSim(task, { terminal: "succeeded", calls, turns: 2, authorizations: [] }).passed).toBe(false);
  });

  it("refuses unobserved confirmation and discard", async () => {
    for (const action of ["confirm", "discard"] as const) {
      await expect(driveUserSim({ ...simulation, scripted_answers: [{ when: "proposal", action }] }, "initial", {
        start: async () => "turn", wait: async () => state("awaiting_confirmation"), answer: vi.fn(), respond: vi.fn(),
      })).rejects.toThrow("unobservable user decision");
    }
  });
});
