import { describe, expect, it } from "vitest";

import { gradeBudget, gradeOperations, gradeTerminal, gradeTools, gradeWrites, valueAtPath } from "./index.js";

const calls = [
  { name: "get_product_workflow_context_v1", params: { response_format: "concise" }, ts: "2026-09-04T00:00:00Z" },
  {
    name: "apply_graph_change_set_v1",
    params: {
      base_graph_revision: 3,
      operations: [{ op: "rename_node", node_ref: "node-prompt-1", title: "新标题" }],
    },
    ts: "2026-09-04T00:00:01Z",
  },
];

describe("eval graders", () => {
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
