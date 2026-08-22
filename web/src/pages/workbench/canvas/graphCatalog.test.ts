import { describe, expect, it } from "vitest";

import type { GraphNodeCatalog, GraphProjection } from "../../../lib/types";
import {
  GRAPH_NODE_TYPE_ORDER,
  graphNodeHasInput,
  graphNodeTypeOrder,
  inspectableGraphNodeId,
  isGraphConnectionValid,
} from "./graphCatalog";

const catalog: GraphNodeCatalog = {
  version: 1,
  nodes: [
    { node_type: "product_source", output_data_type: "product_facts", kind: "source", accepts: [] },
    { node_type: "image_asset", output_data_type: "image_asset", kind: "source", accepts: [] },
    {
      node_type: "creative_brief", output_data_type: "creative_brief", kind: "processing", accepts: [
        { data_type: "product_facts", role: "facts", max_count: 1, required_to_run: false },
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
      ]
    },
    {
      node_type: "visual_system", output_data_type: "visual_system", kind: "processing", accepts: [
        { data_type: "product_facts", role: "facts", max_count: 1, required_to_run: false },
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
      ]
    },
    {
      node_type: "prompt_generation",
      output_data_type: "prompt",
      kind: "processing",
      accepts: [
        { data_type: "product_facts", role: "facts", max_count: null, required_to_run: false },
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
        { data_type: "creative_brief", role: "brief", max_count: null, required_to_run: false },
        { data_type: "visual_system", role: "visual_guidance", max_count: 1, required_to_run: false },
      ],
    },
    {
      node_type: "image_generation",
      output_data_type: "image_asset",
      kind: "processing",
      accepts: [
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: true },
        { data_type: "visual_system", role: "visual_guidance", max_count: 1, required_to_run: false },
        { data_type: "prompt", role: "prompt", max_count: 1, required_to_run: true },
      ],
    },
  ],
};

const graph: GraphProjection = {
  id: "g1",
  product_id: "p1",
  title: "t",
  schema_version: 3,
  revision: 1,
  source_draft_revision_id: null,
  last_operation_group_id: null,
  can_undo: false,
  can_redo: false,
  nodes: [
    {
      id: "source",
      node_type: "product_source",
      title: "商品",
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
    },
    {
      id: "visual",
      node_type: "visual_system",
      title: "视觉",
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
    },
    {
      id: "visual-2",
      node_type: "visual_system",
      title: "视觉 2",
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
    },
    {
      id: "prompt",
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
    },
    {
      id: "image",
      node_type: "image_generation",
      title: "图",
      position_x: 0,
      position_y: 0,
      config: {},
      bound_asset_id: null,
      group_id: null,
      preview_asset_id: null,
      config_status: "incomplete",
      unused: false,
      incoming: [],
      outgoing: [],
    },
  ],
  edges: [],
  groups: [],
};

describe("isGraphConnectionValid", () => {
  it("allows facts into prompt and prompt into image", () => {
    expect(isGraphConnectionValid(graph, "source", "prompt", catalog)).toBe(true);
    expect(isGraphConnectionValid(graph, "prompt", "image", catalog)).toBe(true);
  });

  it("rejects facts into image generation", () => {
    expect(isGraphConnectionValid(graph, "source", "image", catalog)).toBe(false);
  });

  it("rejects a second prompt edge into the same image node", () => {
    const withPrompt = {
      ...graph,
      edges: [{
        id: "e1",
        source_node_id: "prompt",
        target_node_id: "image",
        data_type: "prompt" as const,
        role: "prompt" as const,
        order: 0,
      }],
    };
    expect(isGraphConnectionValid(withPrompt, "prompt", "image", catalog)).toBe(false);
  });

  it("rejects a second visual_guidance edge from catalog cardinality", () => {
    const withVisual = {
      ...graph,
      edges: [{
        id: "e1",
        source_node_id: "visual",
        target_node_id: "prompt",
        data_type: "visual_system" as const,
        role: "visual_guidance" as const,
        order: 0,
      }],
    };
    expect(isGraphConnectionValid(withVisual, "visual", "prompt", catalog)).toBe(false);
    expect(isGraphConnectionValid(withVisual, "visual-2", "prompt", catalog)).toBe(false);
  });

  it("fails closed when the catalog is not loaded", () => {
    expect(isGraphConnectionValid(graph, "source", "prompt", null)).toBe(false);
    expect(isGraphConnectionValid(graph, "prompt", "image", undefined)).toBe(false);
  });
});

describe("graphNodeHasInput", () => {
  it("uses catalog accepts, not a local type list", () => {
    expect(graphNodeHasInput("prompt_generation", catalog)).toBe(true);
    expect(graphNodeHasInput("visual_system", catalog)).toBe(true);
    expect(graphNodeHasInput("creative_brief", catalog)).toBe(true);
    expect(graphNodeHasInput("product_source", catalog)).toBe(false);
    expect(graphNodeHasInput("prompt_generation", null)).toBe(false);
    expect(graphNodeHasInput("image_generation", undefined)).toBe(false);
    expect(graphNodeHasInput("product_source", null)).toBe(false);
  });
});

describe("graphNodeTypeOrder", () => {
  it("falls back to the display order when catalog is missing", () => {
    expect(graphNodeTypeOrder(null)).toEqual(GRAPH_NODE_TYPE_ORDER);
    expect(graphNodeTypeOrder(catalog)).toEqual(GRAPH_NODE_TYPE_ORDER);
  });
});

describe("inspectableGraphNodeId", () => {
  it("only returns ids that still exist on the current graph", () => {
    expect(inspectableGraphNodeId(graph, "source")).toBe("source");
    expect(inspectableGraphNodeId(graph, "group:missing")).toBeNull();
    expect(inspectableGraphNodeId(graph, null)).toBeNull();
  });
});
