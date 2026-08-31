import { describe, expect, it } from "vitest";

import type { GraphNodeCatalog, GraphProjection } from "../../../lib/types";
import {
  GRAPH_NODE_TYPE_ORDER,
  graphConnectionInvalidReason,
  graphDataTypeLabelKey,
  graphNodeHasInput,
  graphNodeTypeOrder,
  graphPortDataTypeClass,
  inspectableGraphNodeId,
  isGraphConnectionValid,
  graphHasRunnableProcessingNode,
  missingRequiredRunNodes,
  missingRequiredRunRoles,
  missingRunNodesSummary,
} from "./graphCatalog";

const catalog: GraphNodeCatalog = {
  version: 1,
  nodes: [
    { node_type: "product_source", output_data_type: "product_facts", kind: "source", accepts: [] },
    { node_type: "image_asset", output_data_type: "image_asset", kind: "source", accepts: [] },
    {
      node_type: "creative_brief", output_data_type: "creative_brief", kind: "document", accepts: [
        { data_type: "product_facts", role: "facts", max_count: 1, required_to_run: false },
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
      ]
    },
    {
      node_type: "visual_system", output_data_type: "visual_system", kind: "document", accepts: [
        { data_type: "product_facts", role: "facts", max_count: 1, required_to_run: false },
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
      ]
    },
    {
      node_type: "image_prompt",
      output_data_type: "prompt",
      kind: "document",
      accepts: [
        { data_type: "product_facts", role: "facts", max_count: null, required_to_run: false },
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
        { data_type: "creative_brief", role: "brief", max_count: 1, required_to_run: false },
        { data_type: "visual_system", role: "visual_guidance", max_count: 1, required_to_run: false },
      ],
    },
    {
      node_type: "image_generation",
      output_data_type: "image_asset",
      kind: "effect",
      accepts: [
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
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
      node_type: "image_prompt",
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
    expect(isGraphConnectionValid(graph, "prompt", "image", catalog, "prompt")).toBe(true);
    expect(isGraphConnectionValid(graph, "prompt", "image", catalog, "reference")).toBe(false);
  });

  it("rejects facts into image generation", () => {
    expect(isGraphConnectionValid(graph, "source", "image", catalog)).toBe(false);
  });

  it("rejects a second prompt edge into the same image node", () => {
    const withPrompt = {
      ...graph,
      nodes: [...graph.nodes, {
        ...graph.nodes.find((node) => node.id === "prompt")!,
        id: "prompt-2",
        title: "提示词 2",
      }],
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

  it("allows reconnecting a max-one port after ignoring the edge being replaced", () => {
    const withPrompt = {
      ...graph,
      nodes: [...graph.nodes, {
        ...graph.nodes.find((node) => node.id === "prompt")!,
        id: "prompt-2",
        title: "提示词 2",
      }],
      edges: [{
        id: "e1",
        source_node_id: "prompt",
        target_node_id: "image",
        data_type: "prompt" as const,
        role: "prompt" as const,
        order: 0,
      }],
    };
    expect(isGraphConnectionValid(withPrompt, "prompt-2", "image", catalog, "prompt", "e1")).toBe(true);
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

  it("returns a user-visible reason for illegal connections", () => {
    expect(graphConnectionInvalidReason(graph, "source", "image", catalog)).toBe("graph.connect.incompatible");
    expect(graphConnectionInvalidReason(graph, "source", "source", catalog)).toBe("graph.connect.self");
    expect(graphConnectionInvalidReason(graph, "source", "prompt", null)).toBe("graph.connect.catalogMissing");
  });
});

describe("missingRequiredRunRoles", () => {
  it("lists Catalog required_to_run gaps on the node", () => {
    const image = graph.nodes.find((node) => node.id === "image");
    expect(image).toBeTruthy();
    expect(missingRequiredRunRoles(image!, catalog)).toEqual(["prompt"]);
  });
});

describe("missingRequiredRunNodes", () => {
  it("can scope the run blocker to a selected group", () => {
    const image = graph.nodes.find((node) => node.id === "image")!;
    const scoped = missingRequiredRunNodes(graph, catalog, new Set([image.id]));
    expect(scoped.map((item) => [item.node.id, item.roles])).toEqual([["image", ["prompt"]]]);
    expect(missingRequiredRunNodes(graph, catalog, new Set(["source"]))).toEqual([]);
  });
});

describe("graphHasRunnableProcessingNode", () => {
  it("keeps whole-graph run available when only some processing nodes lack required edges", () => {
    expect(graphHasRunnableProcessingNode(graph, catalog)).toBe(true);
  });

  it("blocks whole-graph run when no processing node can enqueue", () => {
    const sourceOnly: GraphProjection = {
      ...graph,
      nodes: graph.nodes.filter((node) => node.node_type === "product_source"),
    };
    expect(graphHasRunnableProcessingNode(sourceOnly, catalog)).toBe(false);
    const unwiredImage: GraphProjection = {
      ...graph,
      nodes: graph.nodes.filter((node) => node.id === "image" || node.id === "source"),
    };
    expect(graphHasRunnableProcessingNode(unwiredImage, catalog)).toBe(false);
  });
});

describe("missingRunNodesSummary", () => {
  it("joins node titles with missing roles", () => {
    const image = graph.nodes.find((node) => node.id === "image")!;
    expect(missingRunNodesSummary([{ node: image, roles: ["prompt"] }], (role) => role)).toBe("图: prompt");
  });
});

describe("graphNodeHasInput", () => {
  it("uses catalog accepts, not a local type list", () => {
    expect(graphNodeHasInput("image_prompt", catalog)).toBe(true);
    expect(graphNodeHasInput("visual_system", catalog)).toBe(true);
    expect(graphNodeHasInput("creative_brief", catalog)).toBe(true);
    expect(graphNodeHasInput("product_source", catalog)).toBe(false);
    expect(graphNodeHasInput("image_prompt", null)).toBe(false);
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

describe("graphDataTypeLabelKey", () => {
  it("maps catalog data types to tooltip copy keys", () => {
    expect(graphDataTypeLabelKey("prompt")).toBe("graph.dataType.prompt");
    expect(graphDataTypeLabelKey("product_facts")).toBe("graph.dataType.product_facts");
    expect(graphDataTypeLabelKey("unknown")).toBeNull();
  });
});

describe("graphPortDataTypeClass", () => {
  it("uses kind tokens instead of palette utility classes", () => {
    expect(graphPortDataTypeClass("product_facts")).toContain("kind-product");
    expect(graphPortDataTypeClass("image_asset")).toContain("kind-image");
    expect(graphPortDataTypeClass("creative_brief")).toContain("kind-brief");
    expect(graphPortDataTypeClass("visual_system")).toContain("kind-visual");
    expect(graphPortDataTypeClass("prompt")).toContain("kind-prompt");
    expect(graphPortDataTypeClass("product_facts")).not.toContain("slate-");
    expect(graphPortDataTypeClass("image_asset")).not.toContain("emerald-");
  });
});
