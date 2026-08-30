import { describe, expect, it } from "vitest";

import { isMoveNodesOnly } from "./graphChangeSetQueue";

describe("isMoveNodesOnly", () => {
  it("accepts a non-empty set containing only move operations", () => {
    expect(isMoveNodesOnly([
      { op: "move_nodes", nodes: [["node-1", 10, 20]] },
      { op: "move_nodes", nodes: [["node-2", 30, 40]] },
    ])).toBe(true);
  });

  it("rejects empty or semantic operations", () => {
    expect(isMoveNodesOnly([])).toBe(false);
    expect(isMoveNodesOnly([{ op: "rename_node", node_ref: "node-1", title: "新标题" }])).toBe(false);
    expect(isMoveNodesOnly([
      { op: "move_nodes", nodes: [["node-1", 10, 20]] },
      { op: "update_node_config", node_ref: "node-1", config: {} },
    ])).toBe(false);
  });
});
