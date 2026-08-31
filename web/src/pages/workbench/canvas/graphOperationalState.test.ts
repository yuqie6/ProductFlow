import { describe, expect, it } from "vitest";

import type { GraphNode } from "../../../lib/types";
import { displayNodeState, graphNodeOperationalState } from "./graphOperationalState";

function node(partial: Partial<GraphNode>): GraphNode {
  return {
    id: "node-1",
    node_type: "image_prompt",
    title: "Prompt",
    position_x: 0,
    position_y: 0,
    config: {},
    bound_asset_id: null,
    group_id: null,
    preview_asset_id: null,
    config_status: "ready",
    unused: false,
    incoming: [],
    outgoing: [],
    ...partial,
  };
}

describe("graphNodeOperationalState", () => {
  it("separates document availability from run history", () => {
    const available = node({ document_origin: "collaborative" });
    expect(graphNodeOperationalState(available)).toEqual({
      status: "succeeded",
      labelKey: "graph.nodeState.documentAvailable",
    });
    expect(displayNodeState(available, "skipped")).toEqual(graphNodeOperationalState(available));
  });

  it("makes candidate review the primary operational state", () => {
    expect(graphNodeOperationalState(node({ pending_candidate_artifact_id: "artifact-1" }))).toEqual({
      status: "frozen",
      labelKey: "graph.nodeState.review",
    });
  });

  it("temporarily projects a live execution state", () => {
    expect(displayNodeState(node({ document_origin: "authored" }), "running")).toEqual({
      status: "running",
      labelKey: "detail.nodeStatus.running",
    });
  });
});
