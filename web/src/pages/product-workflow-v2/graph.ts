import type {
  ProductWorkflowV2,
  WorkflowEdgeV2,
  WorkflowFolderV2,
  WorkflowNodeStatus,
  WorkflowNodeTypeV2,
  WorkflowNodeV2,
} from "../../lib/types";

export const V2_NODE_WIDTH = 248;
export const V2_NODE_HEIGHT = 236;
export const V2_FOLDER_PADDING = 32;

export interface FolderBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface FolderSummary {
  member_count: number;
  node_types: WorkflowNodeTypeV2[];
  status: "idle" | "queued" | "running" | "succeeded" | "failed";
  preview_asset_ids: string[];
  inbound_edge_count: number;
  outbound_edge_count: number;
}

export interface GlobalRealNode {
  kind: "node";
  id: string;
  node: WorkflowNodeV2;
  position: { x: number; y: number };
}

export interface GlobalFolderNode {
  kind: "folder";
  id: string;
  folder: WorkflowFolderV2;
  member_ids: string[];
  bounds: FolderBounds;
  summary: FolderSummary;
  position: { x: number; y: number };
}

export type GlobalCanvasNode = GlobalRealNode | GlobalFolderNode;

export interface GlobalCanvasEdge {
  id: string;
  source: string;
  target: string;
  source_handle: string | null;
  target_handle: string | null;
  original_edge_ids: string[];
  count: number;
  projected: boolean;
}

export interface GlobalGraphProjection {
  nodes: GlobalCanvasNode[];
  edges: GlobalCanvasEdge[];
}

export interface LocalFolderGraph {
  folder: WorkflowFolderV2;
  nodes: WorkflowNodeV2[];
  edges: WorkflowEdgeV2[];
}

export interface WorkflowNodeLayoutPositionV2 {
  node_id: string;
  position_x: number;
  position_y: number;
}

const STATUS_PRIORITY: Record<WorkflowNodeStatus, number> = {
  idle: 0,
  succeeded: 1,
  queued: 2,
  cancelled: 3,
  failed: 3,
  running: 4,
};

const NODE_TYPE_ORDER: WorkflowNodeTypeV2[] = [
  "product_context",
  "reference_image",
  "prompt_generation",
  "image_generation",
];

export function folderSyntheticNodeId(folderId: string): string {
  return `folder:${folderId}`;
}

export function deriveFolderBounds(members: WorkflowNodeV2[]): FolderBounds {
  if (members.length === 0) {
    throw new Error("Cannot derive bounds for an empty workflow folder");
  }
  const minX = Math.min(...members.map((node) => node.position_x));
  const minY = Math.min(...members.map((node) => node.position_y));
  const maxX = Math.max(...members.map((node) => node.position_x + V2_NODE_WIDTH));
  const maxY = Math.max(...members.map((node) => node.position_y + V2_NODE_HEIGHT));
  return {
    x: minX - V2_FOLDER_PADDING,
    y: minY - V2_FOLDER_PADDING,
    width: maxX - minX + V2_FOLDER_PADDING * 2,
    height: maxY - minY + V2_FOLDER_PADDING * 2,
  };
}

export function deriveFolderSummary(
  workflow: ProductWorkflowV2,
  folderId: string,
): FolderSummary {
  const members = workflow.nodes.filter((node) => node.folder_id === folderId);
  if (members.length === 0) {
    throw new Error("Workflow folder has no members");
  }
  const memberIds = new Set(members.map((node) => node.id));
  const runnable = members.filter((node) =>
    node.node_type === "prompt_generation" || node.node_type === "image_generation");
  const allRunnableSucceeded = runnable.length > 0
    && runnable.every((node) => node.status === "succeeded");
  const highestIncompleteStatus = runnable
    .filter((node) => node.status !== "succeeded")
    .reduce<WorkflowNodeStatus>(
      (current, node) => STATUS_PRIORITY[node.status] > STATUS_PRIORITY[current] ? node.status : current,
      "idle",
    );
  const status = allRunnableSucceeded
    ? "succeeded"
    : highestIncompleteStatus === "cancelled"
      ? "failed"
      : highestIncompleteStatus;
  const previewAssetIds = members
    .filter((node) => node.node_type === "image_generation")
    .flatMap((node) => {
      const candidate = node.bound_image_asset_id ?? node.output_json?.result_asset_id;
      return typeof candidate === "string" && candidate ? [candidate] : [];
    })
    .filter((assetId, index, all) => all.indexOf(assetId) === index)
    .slice(0, 3);
  return {
    member_count: members.length,
    node_types: NODE_TYPE_ORDER.filter((nodeType) => members.some((node) => node.node_type === nodeType)),
    status,
    preview_asset_ids: previewAssetIds,
    inbound_edge_count: workflow.edges.filter(
      (edge) => !memberIds.has(edge.source_node_id) && memberIds.has(edge.target_node_id),
    ).length,
    outbound_edge_count: workflow.edges.filter(
      (edge) => memberIds.has(edge.source_node_id) && !memberIds.has(edge.target_node_id),
    ).length,
  };
}

export function projectGlobalGraph(workflow: ProductWorkflowV2): GlobalGraphProjection {
  const nodeById = new Map(workflow.nodes.map((node) => [node.id, node]));
  const membersByFolder = new Map<string, WorkflowNodeV2[]>();
  for (const node of workflow.nodes) {
    if (node.folder_id) {
      const members = membersByFolder.get(node.folder_id) ?? [];
      members.push(node);
      membersByFolder.set(node.folder_id, members);
    }
  }
  const nodes: GlobalCanvasNode[] = [];
  for (const folder of [...workflow.folders].sort((left, right) => left.order - right.order || left.id.localeCompare(right.id))) {
    const members = membersByFolder.get(folder.id) ?? [];
    if (members.length === 0) {
      continue;
    }
    const bounds = deriveFolderBounds(members);
    nodes.push({
      kind: "folder",
      id: folderSyntheticNodeId(folder.id),
      folder,
      member_ids: members.map((node) => node.id).sort(),
      bounds,
      summary: deriveFolderSummary(workflow, folder.id),
      position: { x: bounds.x, y: bounds.y },
    });
  }
  nodes.push(...workflow.nodes
    .filter((node) => node.folder_id === null)
    .map<GlobalRealNode>((node) => ({
      kind: "node",
      id: node.id,
      node,
      position: { x: node.position_x, y: node.position_y },
    })));

  const endpoint = (nodeId: string): string => {
    const node = nodeById.get(nodeId);
    if (!node) {
      throw new Error(`Workflow edge references missing node ${nodeId}`);
    }
    return node.folder_id ? folderSyntheticNodeId(node.folder_id) : node.id;
  };
  const directEdges: GlobalCanvasEdge[] = [];
  const projectedByPair = new Map<string, GlobalCanvasEdge>();
  for (const edge of workflow.edges) {
    const source = endpoint(edge.source_node_id);
    const target = endpoint(edge.target_node_id);
    if (source === target) {
      continue;
    }
    const projected = source !== edge.source_node_id || target !== edge.target_node_id;
    if (!projected) {
      directEdges.push({
        id: edge.id,
        source,
        target,
        source_handle: edge.source_handle,
        target_handle: edge.target_handle,
        original_edge_ids: [edge.id],
        count: 1,
        projected: false,
      });
      continue;
    }
    const pair = `${source}\u0000${target}`;
    const existing = projectedByPair.get(pair);
    if (existing) {
      existing.original_edge_ids.push(edge.id);
      existing.original_edge_ids.sort();
      existing.count = existing.original_edge_ids.length;
    } else {
      projectedByPair.set(pair, {
        id: `projected:${source}->${target}`,
        source,
        target,
        source_handle: null,
        target_handle: null,
        original_edge_ids: [edge.id],
        count: 1,
        projected: true,
      });
    }
  }
  return {
    nodes,
    edges: [...directEdges, ...projectedByPair.values()].sort((left, right) => left.id.localeCompare(right.id)),
  };
}

export function buildAutoLayoutNodePositions(
  workflow: ProductWorkflowV2,
  openFolderId: string | null,
): WorkflowNodeLayoutPositionV2[] {
  const projected = openFolderId
    ? (() => {
        const local = buildLocalFolderGraph(workflow, openFolderId);
        return {
          nodes: local.nodes.map((node) => ({
            id: node.id,
            x: node.position_x,
            y: node.position_y,
            height: V2_NODE_HEIGHT,
            memberIds: [node.id],
          })),
          edges: local.edges.map((edge) => ({ source: edge.source_node_id, target: edge.target_node_id })),
        };
      })()
    : (() => {
        const global = projectGlobalGraph(workflow);
        return {
          nodes: global.nodes.map((item) => item.kind === "folder" ? {
            id: item.id,
            x: item.position.x,
            y: item.position.y,
            height: 210,
            memberIds: item.member_ids,
          } : {
            id: item.id,
            x: item.position.x,
            y: item.position.y,
            height: V2_NODE_HEIGHT,
            memberIds: [item.node.id],
          }),
          edges: global.edges.map((edge) => ({ source: edge.source, target: edge.target })),
        };
      })();

  const nodeById = new Map(projected.nodes.map((node) => [node.id, node]));
  const inDegree = new Map(projected.nodes.map((node) => [node.id, 0]));
  const adjacency = new Map(projected.nodes.map((node) => [node.id, [] as string[]]));
  for (const edge of projected.edges) {
    if (!nodeById.has(edge.source) || !nodeById.has(edge.target)) continue;
    adjacency.get(edge.source)!.push(edge.target);
    inDegree.set(edge.target, (inDegree.get(edge.target) ?? 0) + 1);
  }

  const depth = new Map(projected.nodes.map((node) => [node.id, 0]));
  const queue = projected.nodes
    .filter((node) => inDegree.get(node.id) === 0)
    .sort((left, right) => left.y - right.y || left.x - right.x)
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

  const layers = new Map<number, typeof projected.nodes>();
  for (const node of projected.nodes) {
    const nodeDepth = depth.get(node.id) ?? 0;
    const layer = layers.get(nodeDepth) ?? [];
    layer.push(node);
    layers.set(nodeDepth, layer);
  }

  const projectedPositions = new Map<string, { x: number; y: number }>();
  for (const [layerDepth, layer] of [...layers.entries()].sort(([left], [right]) => left - right)) {
    layer.sort((left, right) => left.y - right.y || left.x - right.x);
    const totalHeight = layer.reduce((sum, node) => sum + node.height, 0) + Math.max(0, layer.length - 1) * 72;
    let nextY = Math.max(72, 360 - totalHeight / 2);
    for (const node of layer) {
      projectedPositions.set(node.id, {
        x: snapLayoutCoordinate(72 + layerDepth * 420),
        y: snapLayoutCoordinate(nextY),
      });
      nextY += node.height + 72;
    }
  }

  const currentNodeById = new Map(workflow.nodes.map((node) => [node.id, node]));
  const nextRealPositions = new Map<string, { x: number; y: number }>();
  for (const projectedNode of projected.nodes) {
    const target = projectedPositions.get(projectedNode.id);
    if (!target) continue;
    const deltaX = target.x - projectedNode.x;
    const deltaY = target.y - projectedNode.y;
    for (const memberId of projectedNode.memberIds) {
      const member = currentNodeById.get(memberId);
      if (!member) continue;
      nextRealPositions.set(memberId, {
        x: Math.round(member.position_x + deltaX),
        y: Math.round(member.position_y + deltaY),
      });
    }
  }

  return workflow.nodes.flatMap((node) => {
    const position = nextRealPositions.get(node.id);
    if (!position || (position.x === node.position_x && position.y === node.position_y)) return [];
    return [{ node_id: node.id, position_x: position.x, position_y: position.y }];
  });
}

function snapLayoutCoordinate(value: number): number {
  return Math.round(value / 24) * 24;
}

export function buildLocalFolderGraph(
  workflow: ProductWorkflowV2,
  folderId: string,
): LocalFolderGraph {
  const folder = workflow.folders.find((candidate) => candidate.id === folderId);
  if (!folder) {
    throw new Error("Workflow folder does not exist");
  }
  const nodes = workflow.nodes.filter((node) => node.folder_id === folderId);
  const nodeIds = new Set(nodes.map((node) => node.id));
  return {
    folder,
    nodes,
    edges: workflow.edges.filter(
      (edge) => nodeIds.has(edge.source_node_id) && nodeIds.has(edge.target_node_id),
    ),
  };
}

export function visibleRealNodeIds(
  workflow: ProductWorkflowV2,
  openFolderId: string | null,
): string[] {
  return workflow.nodes
    .filter((node) => openFolderId ? node.folder_id === openFolderId : node.folder_id === null)
    .map((node) => node.id);
}
