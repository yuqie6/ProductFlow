import type { TranslationKey } from "../../../lib/i18n";
import type {
  GraphCatalogInputContract,
  GraphCatalogNode,
  GraphEdgeRole,
  GraphNode,
  GraphNodeCatalog,
  GraphNodeType,
  GraphProjection,
} from "../../../lib/types";
import type { WorkflowCanvasPortVisualState } from "../chrome/WorkflowCanvasChrome";
import type { WorkflowNodePresentationKind } from "../chrome/WorkflowNodeCard";

export const GRAPH_NODE_TYPE_ORDER: GraphNodeType[] = [
  "product_source",
  "image_asset",
  "creative_brief",
  "visual_system",
  "prompt_generation",
  "image_generation",
];

export function graphCatalogNode(
  catalog: GraphNodeCatalog | null | undefined,
  nodeType: GraphNodeType,
): GraphCatalogNode | null {
  return catalog?.nodes.find((node) => node.node_type === nodeType) ?? null;
}

export function graphNodeTypeOrder(catalog: GraphNodeCatalog | null | undefined): GraphNodeType[] {
  if (!catalog?.nodes.length) return GRAPH_NODE_TYPE_ORDER;
  const known = new Set<GraphNodeType>(GRAPH_NODE_TYPE_ORDER);
  const ordered = catalog.nodes
    .map((node) => node.node_type)
    .filter((nodeType): nodeType is GraphNodeType => known.has(nodeType));
  return ordered.length ? ordered : GRAPH_NODE_TYPE_ORDER;
}

export function graphConnectionContract(
  catalog: GraphNodeCatalog | null | undefined,
  sourceType: GraphNodeType,
  targetType: GraphNodeType,
): GraphCatalogInputContract | null {
  const source = graphCatalogNode(catalog, sourceType);
  const target = graphCatalogNode(catalog, targetType);
  if (!source || !target) return null;
  return target.accepts.find((input) => input.data_type === source.output_data_type) ?? null;
}

export function graphNodeHasInput(
  nodeType: GraphNodeType,
  catalog: GraphNodeCatalog | null | undefined,
): boolean {
  const node = graphCatalogNode(catalog, nodeType);
  return Boolean(node && node.accepts.length > 0);
}

export function graphNodePresentationKind(nodeType: GraphNodeType): WorkflowNodePresentationKind {
  return nodeType;
}

export function isGraphConnectionValid(
  graph: GraphProjection,
  sourceNodeId: string,
  targetNodeId: string,
  catalog: GraphNodeCatalog | null | undefined,
): boolean {
  if (!catalog) return false;
  if (sourceNodeId === targetNodeId) return false;
  const source = graph.nodes.find((node) => node.id === sourceNodeId);
  const target = graph.nodes.find((node) => node.id === targetNodeId);
  if (!source || !target) return false;
  const contract = graphConnectionContract(catalog, source.node_type, target.node_type);
  if (!contract) return false;
  if (graph.edges.some((edge) => edge.source_node_id === sourceNodeId && edge.target_node_id === targetNodeId)) {
    return false;
  }
  if (contract.max_count != null) {
    const sameRoleCount = graph.edges.filter((edge) => (
      edge.target_node_id === targetNodeId
      && edge.data_type === contract.data_type
      && edge.role === contract.role
    )).length;
    if (sameRoleCount >= contract.max_count) return false;
  }
  return !wouldCreateCycle(graph, sourceNodeId, targetNodeId);
}

export function graphPortVisualState(
  graph: GraphProjection,
  nodeId: string,
  handleType: "source" | "target",
  connection: { inProgress: boolean; fromNodeId: string | null; fromType: "source" | "target" | null },
  catalog: GraphNodeCatalog | null | undefined,
): WorkflowCanvasPortVisualState {
  if (!connection.inProgress || !connection.fromNodeId || !connection.fromType) return "idle";
  if (connection.fromNodeId === nodeId && connection.fromType === handleType) return "origin";
  if (connection.fromType === handleType) return "idle";
  const sourceNodeId = handleType === "target" ? connection.fromNodeId : nodeId;
  const targetNodeId = handleType === "target" ? nodeId : connection.fromNodeId;
  return isGraphConnectionValid(graph, sourceNodeId, targetNodeId, catalog) ? "valid-target" : "invalid-target";
}

function wouldCreateCycle(graph: GraphProjection, sourceNodeId: string, targetNodeId: string): boolean {
  const outgoing = new Map<string, string[]>();
  for (const edge of graph.edges) {
    const targets = outgoing.get(edge.source_node_id) ?? [];
    targets.push(edge.target_node_id);
    outgoing.set(edge.source_node_id, targets);
  }
  const pending = [targetNodeId];
  const visited = new Set<string>();
  while (pending.length) {
    const nodeId = pending.pop()!;
    if (nodeId === sourceNodeId) return true;
    if (visited.has(nodeId)) continue;
    visited.add(nodeId);
    pending.push(...(outgoing.get(nodeId) ?? []));
  }
  return false;
}

export function inspectableGraphNodeId(
  graph: GraphProjection,
  nodeId: string | null | undefined,
): string | null {
  if (!nodeId) return null;
  return graph.nodes.some((node) => node.id === nodeId) ? nodeId : null;
}

export function isProcessingNode(
  node: GraphNode,
  catalog: GraphNodeCatalog | null | undefined,
): boolean {
  return graphCatalogNode(catalog, node.node_type)?.kind === "processing";
}

const EDGE_ROLE_LABEL_KEYS: Record<GraphEdgeRole, TranslationKey> = {
  facts: "graph.inspector.role.facts",
  reference: "graph.inspector.role.reference",
  brief: "graph.inspector.role.brief",
  visual_guidance: "graph.inspector.role.visual_guidance",
  prompt: "graph.inspector.role.prompt",
};

export function graphEdgeRoleLabelKey(role: string): TranslationKey | null {
  return EDGE_ROLE_LABEL_KEYS[role as GraphEdgeRole] ?? null;
}
