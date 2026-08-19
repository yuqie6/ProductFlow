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

const V2_CONNECTION_HANDLES: Partial<
  Record<WorkflowNodeTypeV2, Partial<Record<WorkflowNodeTypeV2, { source: string; target: string }>>>
> = {
  product_context: {
    prompt_generation: { source: "facts", target: "facts" },
    image_generation: { source: "facts", target: "facts" },
  },
  reference_image: {
    prompt_generation: { source: "asset", target: "reference" },
    image_generation: { source: "asset", target: "reference" },
  },
  prompt_generation: {
    image_generation: { source: "prompt", target: "prompt" },
  },
  image_generation: {
    image_generation: { source: "image", target: "reference" },
  },
};

export interface V2ConnectionHandles {
  source: string;
  target: string;
}

export function getV2ConnectionHandles(
  sourceType: WorkflowNodeTypeV2,
  targetType: WorkflowNodeTypeV2,
): V2ConnectionHandles | null {
  return V2_CONNECTION_HANDLES[sourceType]?.[targetType] ?? null;
}

export function isV2WorkflowConnectionValid(
  workflow: ProductWorkflowV2,
  sourceNodeId: string,
  targetNodeId: string,
  sourceHandle?: string | null,
  targetHandle?: string | null,
): boolean {
  if (sourceNodeId === targetNodeId || sourceNodeId.startsWith("folder:") || targetNodeId.startsWith("folder:")) {
    return false;
  }
  const source = workflow.nodes.find((node) => node.id === sourceNodeId);
  const target = workflow.nodes.find((node) => node.id === targetNodeId);
  const handles = source && target
    ? getV2ConnectionHandles(source.node_type, target.node_type)
    : null;
  if (!source || !target || !handles) {
    return false;
  }
  if (
    (sourceHandle !== undefined && sourceHandle !== handles.source)
    || (targetHandle !== undefined && targetHandle !== handles.target)
  ) {
    return false;
  }
  if (workflow.edges.some(
    (edge) => edge.source_node_id === sourceNodeId && edge.target_node_id === targetNodeId,
  )) {
    return false;
  }

  const outgoing = new Map<string, string[]>();
  for (const edge of workflow.edges) {
    const targets = outgoing.get(edge.source_node_id) ?? [];
    targets.push(edge.target_node_id);
    outgoing.set(edge.source_node_id, targets);
  }
  const pending = [targetNodeId];
  const visited = new Set<string>();
  while (pending.length) {
    const nodeId = pending.pop()!;
    if (nodeId === sourceNodeId) {
      return false;
    }
    if (visited.has(nodeId)) {
      continue;
    }
    visited.add(nodeId);
    pending.push(...(outgoing.get(nodeId) ?? []));
  }
  return true;
}

export function isV2WorkflowLineageEdge(
  workflow: ProductWorkflowV2,
  edge: Pick<WorkflowEdgeV2, "source_node_id" | "target_node_id">,
): boolean {
  const source = workflow.nodes.find((node) => node.id === edge.source_node_id);
  const target = workflow.nodes.find((node) => node.id === edge.target_node_id);
  if (!source || !target) {
    return false;
  }
  if (source.node_type === "product_context" && target.node_type === "prompt_generation") {
    return true;
  }
  return source.node_type === "prompt_generation"
    && target.node_type === "image_generation"
    && typeof source.config_json.prompt_plan_key === "string"
    && source.config_json.prompt_plan_key === target.config_json.prompt_plan_key;
}

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
  // 全局画布始终渲染所有真实节点
  nodes.push(...workflow.nodes.map<GlobalRealNode>((node) => ({
    kind: "node",
    id: node.id,
    node,
    position: { x: node.position_x, y: node.position_y },
  })));

  // 连线直接连接真实节点，保持真实拓扑
  const edges: GlobalCanvasEdge[] = workflow.edges.map((edge) => ({
    id: edge.id,
    source: edge.source_node_id,
    target: edge.target_node_id,
    source_handle: edge.source_handle,
    target_handle: edge.target_handle,
    original_edge_ids: [edge.id],
    count: 1,
    projected: false,
  })).sort((left, right) => left.id.localeCompare(right.id));

  return {
    nodes,
    edges,
  };
}

export function buildAutoLayoutNodePositions(
  workflow: ProductWorkflowV2,
  openFolderId: string | null,
): WorkflowNodeLayoutPositionV2[] {
  const localGraph = openFolderId ? buildLocalFolderGraph(workflow, openFolderId) : null;
  const nodesToLayout = localGraph ? localGraph.nodes : workflow.nodes;
  const edgesToLayout = localGraph ? localGraph.edges : workflow.edges;

  const nodeById = new Map(nodesToLayout.map((node) => [node.id, node]));
  const inDegree = new Map(nodesToLayout.map((node) => [node.id, 0]));
  const adjacency = new Map(nodesToLayout.map((node) => [node.id, [] as string[]]));
  for (const edge of edgesToLayout) {
    if (!nodeById.has(edge.source_node_id) || !nodeById.has(edge.target_node_id)) continue;
    adjacency.get(edge.source_node_id)!.push(edge.target_node_id);
    inDegree.set(edge.target_node_id, (inDegree.get(edge.target_node_id) ?? 0) + 1);
  }

  const depth = new Map(nodesToLayout.map((node) => [node.id, 0]));
  const queue = nodesToLayout
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

  const layers = new Map<number, WorkflowNodeV2[]>();
  for (const node of nodesToLayout) {
    const nodeDepth = depth.get(node.id) ?? 0;
    const layer = layers.get(nodeDepth) ?? [];
    layer.push(node);
    layers.set(nodeDepth, layer);
  }

  const nextRealPositions = new Map<string, { x: number; y: number }>();
  for (const [layerDepth, layer] of [...layers.entries()].sort(([left], [right]) => left - right)) {
    layer.sort((left, right) => left.position_y - right.position_y || left.position_x - right.position_x);
    const totalHeight = layer.length * V2_NODE_HEIGHT + Math.max(0, layer.length - 1) * 72;
    let nextY = Math.max(72, 360 - totalHeight / 2);
    for (const node of layer) {
      nextRealPositions.set(node.id, {
        x: snapLayoutCoordinate(72 + layerDepth * 420),
        y: snapLayoutCoordinate(nextY),
      });
      nextY += V2_NODE_HEIGHT + 72;
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
    .filter((node) => openFolderId ? node.folder_id === openFolderId : true)
    .map((node) => node.id);
}

