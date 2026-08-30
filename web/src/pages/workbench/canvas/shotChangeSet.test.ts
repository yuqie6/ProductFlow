import { describe, expect, it } from "vitest";

import type { GraphNode, GraphProjection } from "../../../lib/types";
import {
  buildCreateShotOperations,
  shotGenerationSpec,
  shotRunRequest,
} from "./shotChangeSet";

function node(input: Partial<GraphNode> & Pick<GraphNode, "id" | "node_type">): GraphNode {
  return {
    title: input.id,
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
    ...input,
  };
}

function graph(nodes: GraphNode[], groups: GraphProjection["groups"] = []): GraphProjection {
  return {
    id: "g1",
    product_id: "p1",
    title: "t",
    schema_version: 3,
    revision: 1,
    last_operation_group_id: null,
    can_undo: false,
    can_redo: false,
    nodes,
    edges: [],
    groups,
  };
}

describe("shotChangeSet", () => {
  it("builds a group, prompt, image, and only shared plus selected identity connections", () => {
    const current = graph([
      node({ id: "source", node_type: "product_source" }),
      node({ id: "visual", node_type: "visual_system" }),
      node({ id: "brief", node_type: "creative_brief" }),
      node({ id: "identity", node_type: "image_asset", bound_asset_id: "asset-1", config: { role: "product_identity" } }),
      node({ id: "other", node_type: "image_asset", bound_asset_id: "asset-2", config: { role: "product_identity" } }),
    ]);
    const operations = buildCreateShotOperations({
      imageTypeKey: "hero",
      title: "首屏海报图",
      position: { x: 400, y: 80 },
      graph: current,
      selectedNodeIds: ["identity"],
    });
    const createOps = operations.filter((op) => op.op === "create_node" || op.op === "create_group");
    const connectOps = operations.filter((op) => op.op === "connect_nodes");
    expect(createOps.map((op) => op.op)).toEqual(["create_group", "create_node", "create_node"]);
    expect(createOps.some((op) => op.op === "create_node" && op.node_type === "prompt_generation")).toBe(true);
    expect(createOps.some((op) => op.op === "create_node" && op.node_type === "image_generation")).toBe(true);
    const sources = connectOps.map((op) => op.source_ref);
    expect(sources).toContain("source");
    expect(sources).toContain("visual");
    expect(sources).toContain("brief");
    expect(sources).toContain("identity");
    expect(sources).not.toContain("other");
    const imageOp = createOps.find((op) => op.op === "create_node" && op.node_type === "image_generation");
    expect(imageOp?.config).toMatchObject({
      image_type_key: "hero",
      generation_spec: { aspect_ratio: "3:4", text_policy: "none" },
    });
  });

  it("does not invent connections when shared nodes are missing and ignores evidence types", () => {
    const empty = graph([]);
    expect(buildCreateShotOperations({
      imageTypeKey: "certification",
      title: "资质认证图",
      position: { x: 0, y: 0 },
      graph: empty,
      selectedNodeIds: [],
    })).toEqual([]);
    const operations = buildCreateShotOperations({
      imageTypeKey: "detail",
      title: "细节展示图",
      position: { x: 24, y: 24 },
      graph: empty,
      selectedNodeIds: ["missing"],
    });
    expect(operations.filter((op) => op.op === "connect_nodes")).toHaveLength(1);
  });

  it("runs every image in a group with one selection run", () => {
    const current = graph([
      node({ id: "prompt", node_type: "prompt_generation", group_id: "shot-hero" }),
      node({ id: "image-2", node_type: "image_generation", group_id: "shot-hero", position_y: 120 }),
      node({ id: "image-1", node_type: "image_generation", group_id: "shot-hero", position_y: 40 }),
      node({ id: "other", node_type: "image_generation", group_id: "shot-detail" }),
    ], [{ id: "shot-hero", title: "首屏", member_ids: ["prompt", "image-1", "image-2"] }]);
    expect(shotRunRequest(current, "shot-hero")).toEqual({
      scope: "selection",
      node_ids: ["image-1", "image-2"],
    });
  });

  it("defaults infographic shots to required on-image copy", () => {
    expect(shotGenerationSpec("faq").text_policy).toBe("required");
    expect(shotGenerationSpec("hero").text_policy).toBe("none");
  });
});
