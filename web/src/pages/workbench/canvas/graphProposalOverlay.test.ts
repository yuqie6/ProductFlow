import { describe, expect, it } from "vitest";

import type { GraphProjection, GraphProposalOverlay } from "../../../lib/types";
import { graphProposalNodeStates, overlayGraphProposal } from "./graphProposalOverlay";

function graph(): GraphProjection {
  return {
    id: "g1",
    product_id: "p1",
    title: "图",
    schema_version: 3,
    revision: 2,
    source_draft_revision_id: null,
    last_operation_group_id: null,
    can_undo: true,
    can_redo: false,
    nodes: [{
      id: "keep",
      node_type: "prompt_generation",
      title: "提示词",
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
    }],
    edges: [],
    groups: [],
  };
}

function proposal(overrides: Partial<GraphProposalOverlay> = {}): GraphProposalOverlay {
  return {
    id: "prop-1",
    summary: "加一个生图节点",
    base_graph_revision: 2,
    stale: false,
    added_nodes: [{
      id: "ghost-image",
      node_type: "image_generation",
      title: "主图 1",
      position_x: 200,
      position_y: 0,
      group_id: null,
      config: {},
    }],
    added_edges: [{
      id: "ghost-edge",
      source_node_id: "keep",
      target_node_id: "ghost-image",
      role: "prompt",
      data_type: "prompt",
      order: 0,
    }],
    deleted_node_ids: [],
    deleted_edge_ids: [],
    changed_node_ids: ["keep"],
    ...overrides,
  };
}

describe("graphProposalOverlay", () => {
  it("adds unapplied nodes and edges without replacing live ids", () => {
    const overlaid = overlayGraphProposal({ ...graph(), pending_proposal: proposal() });
    expect(overlaid.nodes.map((node) => node.id)).toEqual(["keep", "ghost-image"]);
    expect(overlaid.edges.map((edge) => edge.id)).toEqual(["ghost-edge"]);
    expect(graphProposalNodeStates(proposal())).toEqual({
      keep: "changed",
      "ghost-image": "added",
    });
  });

  it("ignores stale proposals", () => {
    const overlaid = overlayGraphProposal({ ...graph(), pending_proposal: proposal({ stale: true }) });
    expect(overlaid.nodes).toHaveLength(1);
    expect(graphProposalNodeStates(proposal({ stale: true }))).toEqual({});
  });
});
