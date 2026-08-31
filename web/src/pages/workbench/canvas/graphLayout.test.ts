import { describe, expect, it } from "vitest";

import type { GraphNode, GraphProjection } from "../../../lib/types";
import {
  GRAPH_NODE_HEIGHT,
  GRAPH_NODE_MEDIA_HEIGHT,
  GRAPH_NODE_WIDTH,
  GRAPH_SNAP,
  buildDeleteNodeOperations,
  buildDuplicateGraphOperations,
  buildPinImageAssetOperations,
  graphNodeHasPinnableOutput,
  buildGraphAutoLayoutPositions,
  buildRenameGroupOperations,
  computeGraphGroupBounds,
  graphNodeLayoutHeight,
  createdGraphNodeIds,
  graphCanvasView,
  graphAvailableNodePosition,
  graphViewportCenterPosition,
  selectedGraphEdges,
  selectionInsideGroup,
} from "./graphLayout";

function node(partial: Partial<GraphNode> & Pick<GraphNode, "id" | "node_type">): GraphNode {
  return {
    title: partial.id,
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
    ...partial,
  };
}

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
    node({ id: "source", node_type: "product_source", position_x: 400, position_y: 10 }),
    node({ id: "prompt", node_type: "image_prompt", position_x: 10, position_y: 10, group_id: "group-1" }),
    node({ id: "image", node_type: "image_generation", position_x: 10, position_y: 300, group_id: "group-1" }),
  ],
  edges: [
    { id: "e1", source_node_id: "source", target_node_id: "prompt", data_type: "product_facts", role: "facts", order: 0 },
    { id: "e2", source_node_id: "prompt", target_node_id: "image", data_type: "prompt", role: "prompt", order: 0 },
  ],
  groups: [{ id: "group-1", title: "一组", member_ids: ["prompt", "image"] }],
};

describe("graph layout commands", () => {
  it("layers nodes by incoming DAG depth", () => {
    const positions = buildGraphAutoLayoutPositions(graph);
    const byId = Object.fromEntries(positions.map((item) => [item.node_id, item]));
    expect(byId.source.position_x).toBeLessThan(byId.prompt.position_x);
    expect(byId.prompt.position_x).toBeLessThan(byId.image.position_x);
  });

  it("keeps group members on one row and leaves room for media-height folder bounds", () => {
    const grouped = {
      ...graph,
      nodes: [
        ...graph.nodes,
        node({ id: "prompt-2", node_type: "image_prompt", position_x: 10, position_y: 600, group_id: "group-2" }),
        node({ id: "image-2", node_type: "image_generation", position_x: 10, position_y: 900, group_id: "group-2" }),
      ],
      edges: [
        ...graph.edges,
        { id: "e3", source_node_id: "source", target_node_id: "prompt-2", data_type: "product_facts" as const, role: "facts" as const, order: 1 },
        { id: "e4", source_node_id: "prompt-2", target_node_id: "image-2", data_type: "prompt" as const, role: "prompt" as const, order: 0 },
      ],
      groups: [
        ...graph.groups,
        { id: "group-2", title: "二组", member_ids: ["prompt-2", "image-2"] },
      ],
    };
    const positions = new Map(buildGraphAutoLayoutPositions(grouped).map((item) => [item.node_id, item]));
    const laidOut = {
      ...grouped,
      nodes: grouped.nodes.map((item) => ({
        ...item,
        position_x: positions.get(item.id)?.position_x ?? item.position_x,
        position_y: positions.get(item.id)?.position_y ?? item.position_y,
      })),
    };
    expect(positions.get("prompt")?.position_y).toBe(positions.get("image")?.position_y);
    expect(positions.get("prompt-2")?.position_y).toBe(positions.get("image-2")?.position_y);
    const first = computeGraphGroupBounds(laidOut, laidOut.groups[0])!;
    const second = computeGraphGroupBounds(laidOut, laidOut.groups[1])!;
    expect(first.y + first.height).toBeLessThan(second.y);
  });

  it("stacks same-layer shots inside a group instead of overlapping them", () => {
    const twoShots = {
      ...graph,
      nodes: [
        ...graph.nodes,
        node({ id: "image-b", node_type: "image_generation", position_x: 10, position_y: 400, group_id: "group-1" }),
      ],
      edges: [
        ...graph.edges,
        { id: "e-b", source_node_id: "prompt", target_node_id: "image-b", data_type: "prompt" as const, role: "prompt" as const, order: 1 },
      ],
      groups: [{ id: "group-1", title: "一组", member_ids: ["prompt", "image", "image-b"] }],
    };
    const positions = new Map(buildGraphAutoLayoutPositions(twoShots).map((item) => [item.node_id, item]));
    expect(positions.get("image")?.position_x).toBe(positions.get("image-b")?.position_x);
    expect(positions.get("image")?.position_y).not.toBe(positions.get("image-b")?.position_y);
    expect(Math.abs((positions.get("image")?.position_y ?? 0) - (positions.get("image-b")?.position_y ?? 0)))
      .toBeGreaterThanOrEqual(GRAPH_NODE_MEDIA_HEIGHT);
  });

  it("duplicates selected nodes and only their internal edges", () => {
    const { operations } = buildDuplicateGraphOperations(graph, ["prompt", "image"], 24);
    const creates = operations.filter((item) => item.op === "create_node");
    const connects = operations.filter((item) => item.op === "connect_nodes");
    expect(creates).toHaveLength(2);
    expect(connects).toHaveLength(1);
    expect(creates.every((item) => item.group_ref === "group-1")).toBe(true);
    expect(creates.every((item) => item.position_x === 10 + 24 || item.position_x === 10 + 24)).toBe(true);
  });

  it("preserves multi-input edge order when duplicating a selection", () => {
    const multiInput = {
      ...graph,
      nodes: [
        ...graph.nodes,
        node({ id: "asset-1", node_type: "image_asset" }),
        node({ id: "asset-2", node_type: "image_asset" }),
      ],
      edges: [
        ...graph.edges,
        { id: "e-ref-1", source_node_id: "asset-1", target_node_id: "prompt", data_type: "image_asset" as const, role: "reference" as const, order: 4 },
        { id: "e-ref-2", source_node_id: "asset-2", target_node_id: "prompt", data_type: "image_asset" as const, role: "reference" as const, order: 2 },
      ],
    };
    const { operations } = buildDuplicateGraphOperations(multiInput, ["asset-1", "asset-2", "prompt"], 24);
    const references = operations.filter((item) => item.op === "connect_nodes" && item.target_ref);
    expect(references.map((item) => item.order)).toContain(4);
    expect(references.map((item) => item.order)).toContain(2);
  });

  it("does not copy the origin column into duplicate create_node config", () => {
    const authored = {
      ...graph,
      nodes: graph.nodes.map((item) => item.id === "prompt" ? { ...item, document_origin: "authored" as const } : item),
    };
    const { operations } = buildDuplicateGraphOperations(authored, ["prompt"], 24);
    const create = operations.find((item) => item.op === "create_node");
    expect(create?.config).not.toHaveProperty("document_origin");
  });

  it("pins an image_generation current output as a nearby image_asset without a reference edge", () => {
    const source = node({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
      position_x: 48,
      position_y: 96,
      group_id: "group-1",
      preview_asset_id: "asset-out",
    });
    expect(graphNodeHasPinnableOutput(source)).toBe(true);
    const operations = buildPinImageAssetOperations(
      { ...graph, nodes: [...graph.nodes.filter((item) => item.id !== "image"), source] },
      "image",
      "图片素材",
    );
    expect(operations).toEqual([
      expect.objectContaining({
        op: "create_node",
        node_type: "image_asset",
        title: "图片素材",
        bound_asset_id: "asset-out",
        group_ref: "group-1",
        position_x: 96,
        position_y: 144,
        config: {},
      }),
    ]);
    expect(operations.some((item) => item.op === "connect_nodes")).toBe(false);
  });

  it("does not pin when the generation node has no current output asset", () => {
    expect(buildPinImageAssetOperations(graph, "image", "图片素材")).toEqual([]);
    expect(buildPinImageAssetOperations(graph, "prompt", "图片素材")).toEqual([]);
    expect(graphNodeHasPinnableOutput(graph.nodes.find((item) => item.id === "image")!)).toBe(false);
  });

  it("deletes a selection as one list of delete_node ops", () => {
    expect(buildDeleteNodeOperations(["prompt", "image"])).toEqual([
      { op: "delete_node", node_ref: "prompt" },
      { op: "delete_node", node_ref: "image" },
    ]);
  });

  it("computes group bounds around members", () => {
    const bounds = computeGraphGroupBounds(graph, graph.groups[0]);
    expect(bounds).not.toBeNull();
    expect(bounds!.width).toBeGreaterThan(248);
    expect(graphNodeLayoutHeight(graph.nodes.find((item) => item.id === "image")!)).toBe(356);
    expect(bounds!.y + bounds!.height).toBeGreaterThanOrEqual(300 + 356 + 32);
  });

  it("renames a group through a single ChangeSet op", () => {
    expect(buildRenameGroupOperations("group-1", "主图组")).toEqual([
      { op: "rename_group", group_ref: "group-1", title: "主图组" },
    ]);
  });

  it("keeps only edges whose both ends are selected", () => {
    expect(selectedGraphEdges(graph, ["prompt", "image"]).map((edge) => edge.id)).toEqual(["e2"]);
    expect(selectedGraphEdges(graph, ["source"])).toEqual([]);
  });

  it("places a new node at the snapped viewport center, not a fixed 120,120 stack", () => {
    const position = graphViewportCenterPosition({
      x: 100,
      y: 40,
      zoom: 1,
      surface_width: 1440,
      surface_height: 900,
    });
    expect(position.position_x).not.toBe(120);
    expect(position.position_y).not.toBe(120);
    expect(position.position_x % 24).toBe(0);
    expect(position.position_y % 24).toBe(0);
  });

  it("moves a new node to the nearest free grid slot when the viewport center is occupied", () => {
    const viewport = {
      x: 0,
      y: 0,
      zoom: 1,
      surface_width: 1024,
      surface_height: 768,
    };
    const center = graphViewportCenterPosition(viewport);
    const position = graphAvailableNodePosition(viewport, [
      node({ id: "occupied", node_type: "image_prompt", group_id: "group-1", ...center }),
    ]);
    expect(position).not.toEqual(center);
    expect(Math.abs(position.position_x % GRAPH_SNAP)).toBe(0);
    expect(Math.abs(position.position_y % GRAPH_SNAP)).toBe(0);
    expect(
      position.position_x + GRAPH_NODE_WIDTH + GRAPH_SNAP <= center.position_x
      || position.position_x >= center.position_x + GRAPH_NODE_WIDTH + GRAPH_SNAP
      || position.position_y + GRAPH_NODE_HEIGHT + GRAPH_SNAP <= center.position_y
      || position.position_y >= center.position_y + GRAPH_NODE_HEIGHT + GRAPH_SNAP,
    ).toBe(true);
  });

  it("selects clones by diffing node ids after a duplicate ChangeSet", () => {
    const after = {
      ...graph,
      nodes: [
        ...graph.nodes,
        node({ id: "prompt-copy", node_type: "image_prompt" }),
        node({ id: "image-copy", node_type: "image_generation" }),
      ],
    };
    expect(createdGraphNodeIds(graph, after)).toEqual(["prompt-copy", "image-copy"]);
  });

  it("keeps a group-local view without turning the group into a node, and leaves cross-group edges on the full graph", () => {
    expect(graph.edges.map((edge) => edge.id)).toEqual(["e1", "e2"]);
    const inside = graphCanvasView(graph, "group-1");
    expect(inside.nodes.map((node) => node.id)).toEqual(["prompt", "image"]);
    expect(inside.edges.map((edge) => edge.id)).toEqual(["e2"]);
    expect(inside.groups).toEqual([]);
    expect(graphCanvasView(graph, null).edges.map((edge) => edge.id)).toEqual(["e1", "e2"]);
    expect(selectionInsideGroup(graph, "group-1", ["source", "prompt", "image"])).toEqual(["prompt", "image"]);
  });

  it("auto-layouts only members when the view is an entered group", () => {
    const positions = buildGraphAutoLayoutPositions(graphCanvasView(graph, "group-1"));
    expect(positions.every((item) => item.node_id !== "source")).toBe(true);
  });
});
