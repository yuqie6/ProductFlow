import type { TranslationKey } from "../../../lib/i18n";
import type { GraphChangeSet, GraphEdge, GraphGroup, GraphNode, GraphProjection } from "../../../lib/types";
import type { WorkflowCanvasViewport } from "./canvasState";

export const GRAPH_NODE_WIDTH = 248;
export const GRAPH_NODE_HEIGHT = 236;
export const GRAPH_LAYOUT_GAP_X = 420;
export const GRAPH_LAYOUT_GAP_Y = 72;
export const GRAPH_SNAP = 24;
export const GRAPH_GROUP_PADDING = 32;
export const GRAPH_DUPLICATE_OFFSET = 48;

export interface GraphNodePosition {
  node_id: string;
  position_x: number;
  position_y: number;
}

export interface GraphGroupBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export function snapGraphCoordinate(value: number): number {
  return Math.round(value / GRAPH_SNAP) * GRAPH_SNAP;
}

export function graphViewportCenterPosition(
  viewport: WorkflowCanvasViewport | null,
): { position_x: number; position_y: number } {
  if (!viewport || !(viewport.zoom > 0) || !(viewport.surface_width > 0) || !(viewport.surface_height > 0)) {
    return {
      position_x: snapGraphCoordinate(120),
      position_y: snapGraphCoordinate(120),
    };
  }
  const centerX = (viewport.surface_width / 2 - viewport.x) / viewport.zoom;
  const centerY = (viewport.surface_height / 2 - viewport.y) / viewport.zoom;
  return {
    position_x: snapGraphCoordinate(centerX - GRAPH_NODE_WIDTH / 2),
    position_y: snapGraphCoordinate(centerY - GRAPH_NODE_HEIGHT / 2),
  };
}

export function createdGraphNodeIds(before: GraphProjection, after: GraphProjection): string[] {
  const known = new Set(before.nodes.map((node) => node.id));
  return after.nodes.filter((node) => !known.has(node.id)).map((node) => node.id);
}

export function graphChangeSetClientRef(prefix: string): string {
  const id = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `${prefix}-${id}`.slice(0, 80);
}

export function buildGraphAutoLayoutPositions(graph: GraphProjection): GraphNodePosition[] {
  const inDegree = new Map(graph.nodes.map((node) => [node.id, 0]));
  const adjacency = new Map(graph.nodes.map((node) => [node.id, [] as string[]]));
  const nodeById = new Map(graph.nodes.map((node) => [node.id, node]));
  for (const edge of graph.edges) {
    if (!nodeById.has(edge.source_node_id) || !nodeById.has(edge.target_node_id)) continue;
    adjacency.get(edge.source_node_id)!.push(edge.target_node_id);
    inDegree.set(edge.target_node_id, (inDegree.get(edge.target_node_id) ?? 0) + 1);
  }

  const depth = new Map(graph.nodes.map((node) => [node.id, 0]));
  const queue = graph.nodes
    .filter((node) => inDegree.get(node.id) === 0)
    .sort((left, right) => left.position_y - right.position_y || left.position_x - right.position_x)
    .map((node) => node.id);
  for (let cursor = 0; cursor < queue.length; cursor += 1) {
    const nodeId = queue[cursor];
    for (const targetId of adjacency.get(nodeId) ?? []) {
      depth.set(targetId, Math.max(depth.get(targetId) ?? 0, (depth.get(nodeId) ?? 0) + 1));
      const remaining = (inDegree.get(targetId) ?? 1) - 1;
      inDegree.set(targetId, remaining);
      if (remaining === 0) queue.push(targetId);
    }
  }

  const layers = new Map<number, GraphNode[]>();
  for (const node of graph.nodes) {
    const nodeDepth = depth.get(node.id) ?? 0;
    const layer = layers.get(nodeDepth) ?? [];
    layer.push(node);
    layers.set(nodeDepth, layer);
  }

  const nextPositions = new Map<string, { x: number; y: number }>();
  for (const [layerDepth, layer] of [...layers.entries()].sort(([left], [right]) => left - right)) {
    layer.sort((left, right) => left.position_y - right.position_y || left.position_x - right.position_x);
    const totalHeight = layer.length * GRAPH_NODE_HEIGHT + Math.max(0, layer.length - 1) * GRAPH_LAYOUT_GAP_Y;
    let nextY = Math.max(72, 360 - totalHeight / 2);
    for (const node of layer) {
      nextPositions.set(node.id, {
        x: snapGraphCoordinate(72 + layerDepth * GRAPH_LAYOUT_GAP_X),
        y: snapGraphCoordinate(nextY),
      });
      nextY += GRAPH_NODE_HEIGHT + GRAPH_LAYOUT_GAP_Y;
    }
  }

  return graph.nodes.flatMap((node) => {
    const position = nextPositions.get(node.id);
    if (!position || (position.x === node.position_x && position.y === node.position_y)) return [];
    return [{ node_id: node.id, position_x: position.x, position_y: position.y }];
  });
}

export function graphCanvasView(
  graph: GraphProjection,
  groupId: string | null | undefined,
): GraphProjection {
  if (!groupId) return graph;
  if (!graph.groups.some((group) => group.id === groupId)) return graph;
  const nodes = graph.nodes.filter((node) => node.group_id === groupId);
  const memberIds = new Set(nodes.map((node) => node.id));
  return {
    ...graph,
    nodes,
    edges: graph.edges.filter(
      (edge) => memberIds.has(edge.source_node_id) && memberIds.has(edge.target_node_id),
    ),
    groups: [],
  };
}

export function selectionInsideGroup(
  graph: GraphProjection,
  groupId: string,
  selectedNodeIds: readonly string[],
): string[] {
  const members = new Set(
    graph.nodes.filter((node) => node.group_id === groupId).map((node) => node.id),
  );
  return selectedNodeIds.filter((nodeId) => members.has(nodeId));
}

export function computeGraphGroupBounds(graph: GraphProjection, group: GraphGroup): GraphGroupBounds | null {
  const members = graph.nodes.filter((node) => group.member_ids.includes(node.id));
  if (!members.length) return null;
  const minX = Math.min(...members.map((node) => node.position_x));
  const minY = Math.min(...members.map((node) => node.position_y));
  const maxX = Math.max(...members.map((node) => node.position_x + GRAPH_NODE_WIDTH));
  const maxY = Math.max(...members.map((node) => node.position_y + GRAPH_NODE_HEIGHT));
  return {
    x: minX - GRAPH_GROUP_PADDING,
    y: minY - GRAPH_GROUP_PADDING - 28,
    width: maxX - minX + GRAPH_GROUP_PADDING * 2,
    height: maxY - minY + GRAPH_GROUP_PADDING * 2 + 28,
  };
}

export function selectedGraphEdges(graph: GraphProjection, nodeIds: readonly string[]): GraphEdge[] {
  const selected = new Set(nodeIds);
  return graph.edges.filter((edge) => selected.has(edge.source_node_id) && selected.has(edge.target_node_id));
}

export function buildDuplicateGraphOperations(
  graph: GraphProjection,
  nodeIds: readonly string[],
  offset = GRAPH_DUPLICATE_OFFSET,
): { operations: GraphChangeSet["operations"]; clientRefs: string[] } {
  const selected = graph.nodes.filter((node) => nodeIds.includes(node.id));
  if (!selected.length) {
    return { operations: [], clientRefs: [] };
  }
  const refById = new Map(selected.map((node) => [node.id, graphChangeSetClientRef("node")]));
  const operations: GraphChangeSet["operations"] = selected.map((node) => ({
    op: "create_node",
    client_ref: refById.get(node.id),
    node_type: node.node_type,
    title: node.title,
    position_x: node.position_x + offset,
    position_y: node.position_y + offset,
    config: node.config,
    bound_asset_id: node.bound_asset_id,
    ...(node.group_id ? { group_ref: node.group_id } : {}),
  }));
  for (const edge of selectedGraphEdges(graph, nodeIds)) {
    operations.push({
      op: "connect_nodes",
      client_ref: graphChangeSetClientRef("edge"),
      source_ref: refById.get(edge.source_node_id),
      target_ref: refById.get(edge.target_node_id),
    });
  }
  return { operations, clientRefs: [...refById.values()] };
}

export function buildDeleteNodeOperations(nodeIds: readonly string[]): GraphChangeSet["operations"] {
  return nodeIds.map((nodeId) => ({ op: "delete_node", node_ref: nodeId }));
}

export function buildRenameGroupOperations(groupId: string, title: string): GraphChangeSet["operations"] {
  return [{ op: "rename_group", group_ref: groupId, title }];
}

export function defaultGraphNodeConfig(nodeType: GraphNode["node_type"]): Record<string, unknown> {
  if (nodeType === "product_source") {
    return {
      source_product_id: null,
      fact_set_version_id: null,
    };
  }
  if (nodeType !== "image_generation") return {};
  return {
    generation_spec: {
      aspect_ratio: "1:1",
      resolution_tier: "high",
      quality_intent: "high",
      reference_fidelity: "high",
      background_intent: "auto",
      text_policy: "none",
    },
  };
}

export function graphNodeTitleKey(nodeType: GraphNode["node_type"]): TranslationKey {
  switch (nodeType) {
    case "product_source":
      return "graph.node.productSource";
    case "image_asset":
      return "graph.node.imageAsset";
    case "creative_brief":
      return "graph.node.creativeBrief";
    case "visual_system":
      return "graph.node.visualSystem";
    case "prompt_generation":
      return "graph.node.promptGeneration";
    case "image_generation":
      return "graph.node.imageGeneration";
    default:
      return "graph.node.productSource";
  }
}
