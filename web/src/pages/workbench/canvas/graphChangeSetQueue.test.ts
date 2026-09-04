import { describe, expect, it } from "vitest";

import type { GraphNode } from "../../../lib/types";
import { buildNodeCommitChangeSet, isMoveNodesOnly } from "./graphChangeSetQueue";

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

describe("buildNodeCommitChangeSet", () => {
  it("uses the inspector draft baseline instead of a later live revision", () => {
    const node: GraphNode = {
      id: "prompt",
      node_type: "image_prompt",
      title: "提示词",
      position_x: 0,
      position_y: 0,
      config: { prompt: { design_goal: "旧" } },
      bound_asset_id: null,
      group_id: null,
      preview_asset_id: null,
      config_status: "ready",
      unused: false,
      incoming: [],
      outgoing: [],
    };
    const changeSet = buildNodeCommitChangeSet({
      node,
      title: "提示词",
      config: { prompt: { design_goal: "运行中手填" } },
      boundAssetId: null,
      baseGraphRevision: 4,
    });
    expect(changeSet).toEqual({
      base_graph_revision: 4,
      summary: "更新节点",
      operations: [{
        op: "update_node_config",
        node_ref: "prompt",
        config: { prompt: { design_goal: "运行中手填" } },
        bound_asset_id: null,
      }],
    });
  });
});
