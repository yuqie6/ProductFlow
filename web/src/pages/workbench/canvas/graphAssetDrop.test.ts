import { describe, expect, it } from "vitest";

import type { GraphNode, GraphNodeCatalog, GraphProjection } from "../../../lib/types";
import {
  boundImageAssetNodes,
  buildReuseConnectOperations,
  resolveGraphAssetDrop,
} from "./graphAssetDrop";

function node(partial: Partial<GraphNode> & Pick<GraphNode, "id" | "node_type">): GraphNode {
  return {
    title: partial.title ?? partial.id,
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

const catalog: GraphNodeCatalog = {
  version: 1,
  nodes: [
    { node_type: "product_source", output_data_type: "product_facts", kind: "source", accepts: [] },
    { node_type: "image_asset", output_data_type: "image_asset", kind: "source", accepts: [] },
    {
      node_type: "prompt_generation",
      output_data_type: "prompt",
      kind: "processing",
      accepts: [
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
      ],
    },
    {
      node_type: "image_generation",
      output_data_type: "image_asset",
      kind: "processing",
      accepts: [
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
      ],
    },
  ],
};

function graph(nodes: GraphNode[], edges: GraphProjection["edges"] = []): GraphProjection {
  return {
    id: "g1",
    product_id: "p1",
    title: "图",
    schema_version: 3,
    revision: 1,
    source_draft_revision_id: null,
    last_operation_group_id: null,
    can_undo: false,
    can_redo: false,
    nodes,
    edges,
    groups: [],
  };
}

describe("resolveGraphAssetDrop", () => {
  it("creates bound image_asset nodes on the pane without connecting them", () => {
    const plan = resolveGraphAssetDrop(graph([]), {
      assetIds: ["asset-a", "asset-b"],
      position: { x: 120, y: 80 },
      nodeId: null,
    }, catalog);
    expect(plan.kind).toBe("apply");
    if (plan.kind !== "apply") return;
    expect(plan.operations).toHaveLength(2);
    expect(plan.operations.every((operation) => operation.op === "create_node")).toBe(true);
    expect(plan.operations.map((operation) => (
      operation.op === "create_node" ? operation.bound_asset_id : null
    ))).toEqual(["asset-a", "asset-b"]);
  });

  it("rebinds an existing image_asset node without creating an edge", () => {
    const asset = node({ id: "asset-node", node_type: "image_asset", bound_asset_id: "old" });
    const plan = resolveGraphAssetDrop(graph([asset]), {
      assetIds: ["new-asset"],
      position: { x: 0, y: 0 },
      nodeId: "asset-node",
    }, catalog);
    expect(plan).toMatchObject({
      kind: "apply",
      operations: [{ op: "update_node_config", node_ref: "asset-node", bound_asset_id: "new-asset" }],
    });
  });

  it("asks before reusing a bound node when dropping onto a processing node", () => {
    const current = graph([
      node({ id: "prompt", node_type: "prompt_generation" }),
      node({ id: "existing", node_type: "image_asset", title: "参考图 1", bound_asset_id: "asset-a" }),
    ]);
    const plan = resolveGraphAssetDrop(current, {
      assetIds: ["asset-a"],
      position: { x: 40, y: 40 },
      nodeId: "prompt",
    }, catalog);
    expect(plan.kind).toBe("choose_reuse");
    if (plan.kind !== "choose_reuse") return;
    expect(plan.existing.map((item) => item.id)).toEqual(["existing"]);
    expect(boundImageAssetNodes(current, "asset-a")).toHaveLength(1);
  });

  it("creates and connects when no reusable node exists", () => {
    const plan = resolveGraphAssetDrop(graph([node({ id: "image", node_type: "image_generation" })]), {
      assetIds: ["asset-a"],
      position: { x: 24, y: 24 },
      nodeId: "image",
    }, catalog);
    expect(plan.kind).toBe("apply");
    if (plan.kind !== "apply") return;
    expect(plan.operations.map((operation) => operation.op)).toEqual(["create_node", "connect_nodes"]);
  });

  it("does not auto-merge by selected nodes or identical assets when connecting reuse", () => {
    const operations = buildReuseConnectOperations("existing", "prompt");
    expect(operations).toEqual([expect.objectContaining({
      op: "connect_nodes",
      source_ref: "existing",
      target_ref: "prompt",
    })]);
  });

  it("ignores drops on nodes that cannot take a reference", () => {
    const plan = resolveGraphAssetDrop(graph([node({ id: "facts", node_type: "product_source" })]), {
      assetIds: ["asset-a"],
      position: { x: 0, y: 0 },
      nodeId: "facts",
    }, catalog);
    expect(plan.kind).toBe("ignored");
  });

  it("ignores drops onto processing nodes until catalog is loaded", () => {
    const plan = resolveGraphAssetDrop(graph([node({ id: "prompt", node_type: "prompt_generation" })]), {
      assetIds: ["asset-a"],
      position: { x: 0, y: 0 },
      nodeId: "prompt",
    }, null);
    expect(plan.kind).toBe("ignored");
  });
});
