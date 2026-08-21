import { describe, expect, it } from "vitest";

import type { GraphNodeRun, GraphProjection } from "../../../lib/types";
import {
  graphContextEntries,
  graphNodeRunPreviewAssetId,
  graphRunScopeLabelKey,
} from "./graphRunDisplay";

const graph: GraphProjection = {
  id: "g1",
  product_id: "p1",
  title: "t",
  schema_version: 3,
  revision: 2,
  source_draft_revision_id: null,
  last_operation_group_id: null,
  nodes: [{
    id: "image",
    node_type: "image_generation",
    title: "主图 1",
    position_x: 0,
    position_y: 0,
    config: {},
    bound_asset_id: null,
    group_id: null,
    preview_asset_id: "preview-1",
    config_status: "ready",
    unused: false,
    incoming: [],
    outgoing: [],
  }],
  edges: [],
  groups: [],
};

function nodeRun(partial: Partial<GraphNodeRun>): GraphNodeRun {
  return {
    id: "nr1",
    node_id: "image",
    status: "succeeded",
    sort_order: 0,
    compiled_context: null,
    output: null,
    failure_reason: null,
    started_at: "2026-08-21T00:00:00Z",
    finished_at: "2026-08-21T00:00:01Z",
    ...partial,
  };
}

describe("graph run display", () => {
  it("labels run scopes without exposing enum strings to the page", () => {
    expect(graphRunScopeLabelKey("graph")).toBe("graph.runs.scope.graph");
    expect(graphRunScopeLabelKey("node")).toBe("graph.runs.scope.node");
    expect(graphRunScopeLabelKey("to_node")).toBe("graph.runs.scope.toNode");
  });

  it("prefers an explicit product image on the node run, then the current preview", () => {
    expect(graphNodeRunPreviewAssetId(nodeRun({ output: { product_image_asset_id: "out-1" } }), graph)).toBe("out-1");
    expect(graphNodeRunPreviewAssetId(nodeRun({ output: { artifact_id: "art-1" } }), graph)).toBe("preview-1");
    expect(graphNodeRunPreviewAssetId(nodeRun({ status: "failed" }), graph)).toBeNull();
  });

  it("flattens compiled context for the evidence list", () => {
    expect(graphContextEntries({
      fact_count: 3,
      reference_asset_ids: ["a", "b"],
    })).toEqual([
      { key: "fact_count", labelKey: "graph.runs.context.factCount", value: "3" },
      { key: "reference_asset_ids", labelKey: "graph.runs.context.referenceAssets", value: "a · b" },
    ]);
  });
});
