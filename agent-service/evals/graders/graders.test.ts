import { describe, expect, it } from "vitest";

import { gradeBudget, gradeOperations, gradeTerminal, gradeTools, gradeWrites, valueAtPath } from "./index.js";
import type { EvalCallRecord } from "./types.js";
import { loadEvalTaskSet } from "../loader.js";

const calls: EvalCallRecord[] = [
  { name: "get_product_workflow_context_v1", params: { response_format: "concise" }, ts: "2026-09-04T00:00:00Z", outcome: "succeeded" },
  {
    name: "apply_graph_change_set_v1",
    params: {
      base_graph_revision: 3,
      operations: [{ op: "rename_node", node_ref: "node-prompt-1", title: "新标题" }],
    },
    ts: "2026-09-04T00:00:01Z",
    outcome: "succeeded",
  },
];

describe("eval graders", () => {
  it("accepts equivalent workflow target sets but rejects a different target", async () => {
    const { tasks } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "run-diagnosis-global-multiple-workflows")!;
    const observed: EvalCallRecord[] = task.reference.scripted_calls.map((call) => ({ ...structuredClone(call), ts: "t", outcome: "succeeded" }));
    const read = observed.find((call) => call.name === "inspect_global_workflow_runs_v1")!;
    const params = read.params as { workflow_ids: string[] };
    params.workflow_ids.reverse();
    expect(gradeTools(task.expect.tools, observed).passed).toBe(true);
    params.workflow_ids[0] = "wrong-workflow";
    expect(gradeTools(task.expect.tools, observed).passed).toBe(false);
  });

  it("does not require redundant context or candidate-list reads", async () => {
    const { tasks } = await loadEvalTaskSet();
    for (const [id, redundant] of [
      ["run-diagnosis-inspect-node-after-detail", "get_product_workflow_context_v1"],
      ["product-intake-inspect-product-candidate", "list_products_v1"],
    ]) {
      const task = tasks.find((task) => task.id === id)!;
      const observed: EvalCallRecord[] = task.reference.scripted_calls.filter((call) => call.name !== redundant)
        .map((call) => ({ ...call, ts: "t", outcome: "succeeded" }));
      expect(gradeTools(task.expect.tools, observed).passed).toBe(true);
      expect(gradeTools(task.expect.tools, observed.slice(0, 1)).passed).toBe(false);
    }
  });

  it("preserves the requested node's image type while changing its design goal", async () => {
    const { tasks } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-update-node-config")!;
    const params = structuredClone(task.reference.scripted_calls.at(-1)!.params) as { operations: Array<{ config: { image_type_key: string } }> };
    const call: EvalCallRecord = { name: "apply_graph_change_set_v1", params, ts: "t", outcome: "succeeded" };
    expect(gradeWrites(task.expect.writes, [call]).passed).toBe(true);
    params.operations[0].config.image_type_key = "detail";
    expect(gradeWrites(task.expect.writes, [call]).passed).toBe(false);
  });
  it("requires the relevant successful read before the write", () => {
    const expected = { required: ["get_node_detail_v1"], reads: [{ tool: "get_node_detail_v1", match: { node_id: "node-prompt-1" } }] };
    const read: EvalCallRecord = { name: "get_node_detail_v1", params: { node_id: "node-prompt-1" }, outcome: "succeeded", ts: "t" };
    expect(gradeTools(expected, [read, calls[1]]).passed).toBe(true);
    expect(gradeTools(expected, [calls[1], read]).passed).toBe(false);
    expect(gradeTools(expected, [{ ...read, params: { node_id: "node-prompt-2" } }, calls[1]]).passed).toBe(false);
    expect(gradeTools(expected, [{ ...read, outcome: "failed" }, calls[1]]).passed).toBe(false);
  });
  it("binds created graph identities without requiring reference aliases", async () => {
    const { tasks } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "graph-editing-propose-scene-shot")!;
    const params = structuredClone(task.reference.scripted_calls.at(-1)!.params) as { operations: Array<Record<string, unknown>> };
    for (const op of params.operations) for (const key of ["client_ref", "source_ref", "target_ref", "group_ref"]) {
      if (typeof op[key] === "string" && ["g-scene", "p-scene", "i-scene-1"].includes(op[key] as string)) op[key] = `local-${op[key]}`;
    }
    const call: EvalCallRecord = { name: "propose_graph_change_set_v1", params, ts: "t", outcome: "succeeded" };
    expect(gradeWrites(task.expect.writes, [call]).passed).toBe(true);
    params.operations[3].source_ref = "node-prompt-1";
    params.operations[3].target_ref = "node-image-1";
    expect(gradeWrites(task.expect.writes, [call]).passed).toBe(false);
  });

  it("rejects an unsolicited force/rewrite while accepting omitted run defaults", async () => {
    const { tasks } = await loadEvalTaskSet();
    const task = tasks.find((task) => task.id === "workflow-run-request-run-current-workflow")!;
    const params = task.reference.scripted_calls.at(-1)!.params as object;
    const call: EvalCallRecord = { name: "request_workflow_run_v1", params, ts: "t", outcome: "succeeded" };
    expect(gradeWrites(task.expect.writes, [call]).passed).toBe(true);
    expect(gradeWrites(task.expect.writes, [{ ...call, params: { ...params, force: true, document_action: "rewrite" } }]).passed).toBe(false);
  });
  it("rejects failed writes and extra effects, while accepting a successful retry", () => {
    const expected = [{ tool: "apply_graph_change_set_v1", match: {
      "operations[0].op": "rename_node", "operations[0].node_ref": "node-prompt-1", "operations[0].title": "新标题",
    } }];
    const failed: EvalCallRecord = { ...calls[1], outcome: "failed" };
    expect(gradeWrites(expected, [failed]).passed).toBe(false);
    expect(gradeWrites(expected, [failed, calls[1]]).passed).toBe(true);
    expect(gradeTools({ required: [failed.name] }, [failed]).passed).toBe(false);
    const extra = { ...calls[1], params: { operations: [
      { op: "rename_node", node_ref: "node-prompt-1", title: "新标题" },
      { op: "delete_node", node_ref: "node-image-2" },
    ] } };
    expect(gradeWrites(expected, [extra]).passed).toBe(false);
    expect(gradeWrites(expected, [calls[1], extra]).passed).toBe(false);
    expect(gradeWrites([], [calls[1]]).passed).toBe(false);
  });

  it("matches independent deletes in either order without allowing extra nodes", () => {
    const expected = [{ tool: "propose_graph_change_set_v1", match: {
      "operations[0].op": "delete_node", "operations[0].node_ref": "a",
      "operations[1].op": "delete_node", "operations[1].node_ref": "b",
    } }];
    const call: EvalCallRecord = { name: expected[0].tool, outcome: "succeeded", ts: "t", params: {
      operations: [{ op: "delete_node", node_ref: "b" }, { op: "delete_node", node_ref: "a" }],
    } };
    expect(gradeWrites(expected, [call]).passed).toBe(true);
  });
  it("checks required and forbidden tools and operations", () => {
    expect(gradeTools({ required: ["get_product_workflow_context_v1"] }, calls).passed).toBe(true);
    expect(gradeTools({ forbidden: ["apply_graph_change_set_v1"] }, calls).passed).toBe(false);
    expect(gradeOperations({ required: ["rename_node"], forbidden: ["delete_node"] }, calls).passed).toBe(true);
  });

  it("matches nested write parameters by explicit path", () => {
    expect(valueAtPath(calls[1].params, "operations[0].node_ref")).toBe("node-prompt-1");
    expect(gradeWrites([{
      tool: "apply_graph_change_set_v1",
      match: { "base_graph_revision": 3, "operations[0].op": "rename_node", "operations[0].title": "新标题" },
    }], calls).passed).toBe(true);
    expect(gradeWrites([{
      tool: "apply_graph_change_set_v1",
      match: { "operations[0].op": "delete_node" },
    }], calls).passed).toBe(false);
  });

  it("treats unavailable token usage as a failed token budget check", () => {
    expect(gradeTerminal(["succeeded"], "succeeded").passed).toBe(true);
    expect(gradeBudget({ max_tool_calls: 2, max_duration_ms: 100 }, { tool_calls: 2, tokens: null, duration_ms: 50 }).passed).toBe(true);
    expect(gradeBudget({ max_tokens: 100 }, { tool_calls: 2, tokens: null, duration_ms: 50 }).errors).toContain("token usage is unavailable");
  });
});
