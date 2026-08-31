/**
 * schema-v3 Node Catalog 的客户端投影。
 *
 * 连线规则和可编辑配置键来自 API 返回的 Catalog。运行时输入仍归编译器；这里只判断画布连线/拖放是否合法。
 */

import type { TranslationKey } from "../../../lib/i18n";
import type {
  GraphCatalogConfigField,
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

export function graphNodeConfigFields(
  catalog: GraphNodeCatalog | null | undefined,
  nodeType: GraphNodeType,
): GraphCatalogConfigField[] {
  return graphCatalogNode(catalog, nodeType)?.config_fields ?? [];
}

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

/** Catalog 按源节点输出类型接受连线，不看标题或资产路径。 */
export function graphConnectionContract(
  catalog: GraphNodeCatalog | null | undefined,
  sourceType: GraphNodeType,
  targetType: GraphNodeType,
  targetRole?: GraphEdgeRole | string | null,
): GraphCatalogInputContract | null {
  const source = graphCatalogNode(catalog, sourceType);
  const target = graphCatalogNode(catalog, targetType);
  if (!source || !target) return null;
  if (targetRole && targetRole !== "input") {
    return target.accepts.find((input) => input.role === targetRole && input.data_type === source.output_data_type) ?? null;
  }
  return target.accepts.find((input) => input.data_type === source.output_data_type) ?? null;
}

export function graphInputPorts(
  catalog: GraphNodeCatalog | null | undefined,
  nodeType: GraphNodeType,
): GraphCatalogInputContract[] {
  return graphCatalogNode(catalog, nodeType)?.accepts ?? [];
}

export function graphPortDataTypeClass(dataType: GraphCatalogInputContract["data_type"] | "output"): string {
  switch (dataType) {
    case "product_facts":
      return "!border-kind-product !bg-kind-product-soft";
    case "image_asset":
      return "!border-kind-image !bg-kind-image-soft";
    case "creative_brief":
      return "!border-kind-brief !bg-kind-brief-soft";
    case "visual_system":
      return "!border-kind-visual !bg-kind-visual-soft";
    case "prompt":
      return "!border-kind-prompt !bg-kind-prompt-soft";
    default:
      return "";
  }
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

/** 允许不完整 DAG；只拒绝自环、类型不兼容、基数冲突和环。 */
export function graphConnectionInvalidReason(
  graph: GraphProjection,
  sourceNodeId: string,
  targetNodeId: string,
  catalog: GraphNodeCatalog | null | undefined,
  targetHandle?: string | null,
  ignoredEdgeId?: string | null,
): TranslationKey | null {
  if (!catalog) return "graph.connect.catalogMissing";
  if (sourceNodeId === targetNodeId) return "graph.connect.self";
  const source = graph.nodes.find((node) => node.id === sourceNodeId);
  const target = graph.nodes.find((node) => node.id === targetNodeId);
  if (!source || !target) return "graph.connect.missingNode";
  const contract = graphConnectionContract(catalog, source.node_type, target.node_type, targetHandle);
  if (!contract) return "graph.connect.incompatible";
  if (graph.edges.some((edge) => (
    edge.id !== ignoredEdgeId
    && edge.source_node_id === sourceNodeId
    && edge.target_node_id === targetNodeId
    && edge.role === contract.role
  ))) {
    return "graph.connect.duplicate";
  }
  if (contract.max_count != null) {
    const sameRoleCount = graph.edges.filter((edge) => (
      edge.id !== ignoredEdgeId
      && edge.target_node_id === targetNodeId
      && edge.data_type === contract.data_type
      && edge.role === contract.role
    )).length;
    if (sameRoleCount >= contract.max_count) return "graph.connect.cardinality";
  }
  return wouldCreateCycle(graph, sourceNodeId, targetNodeId, ignoredEdgeId) ? "graph.connect.cycle" : null;
}

export function isGraphConnectionValid(
  graph: GraphProjection,
  sourceNodeId: string,
  targetNodeId: string,
  catalog: GraphNodeCatalog | null | undefined,
  targetHandle?: string | null,
  ignoredEdgeId?: string | null,
): boolean {
  return graphConnectionInvalidReason(graph, sourceNodeId, targetNodeId, catalog, targetHandle, ignoredEdgeId) === null;
}

/** 能否运行只看 incoming 边；断开边就失去该输入。 */
export function missingRequiredRunRoles(
  node: GraphNode,
  catalog: GraphNodeCatalog | null | undefined,
): GraphEdgeRole[] {
  const spec = graphCatalogNode(catalog, node.node_type);
  if (!spec) return [];
  return spec.accepts
    .filter((input) => input.required_to_run)
    .filter((input) => !node.incoming.some((edge) => (
      edge.role === input.role && edge.data_type === input.data_type
    )))
    .map((input) => input.role);
}

export function missingRequiredRunNodes(
  graph: GraphProjection,
  catalog: GraphNodeCatalog | null | undefined,
  nodeIds?: ReadonlySet<string>,
): Array<{ node: GraphNode; roles: GraphEdgeRole[] }> {
  return graph.nodes
    .filter((node) => !nodeIds || nodeIds.has(node.id))
    .map((node) => ({ node, roles: missingRequiredRunRoles(node, catalog) }))
    .filter((item) => item.roles.length > 0);
}

const FALLBACK_PROCESSING_TYPES = new Set<GraphNodeType>([
  "creative_brief",
  "visual_system",
  "prompt_generation",
  "image_generation",
]);

function nodeIsProcessing(
  node: GraphNode,
  catalog: GraphNodeCatalog | null | undefined,
): boolean {
  const spec = graphCatalogNode(catalog, node.node_type);
  if (spec) return spec.kind === "processing";
  return FALLBACK_PROCESSING_TYPES.has(node.node_type);
}

/** 整图入队条件与后端一致：至少有一个具备必需输入的处理节点。 */
export function graphHasRunnableProcessingNode(
  graph: GraphProjection,
  catalog: GraphNodeCatalog | null | undefined,
): boolean {
  return graph.nodes.some((node) => (
    nodeIsProcessing(node, catalog) && missingRequiredRunRoles(node, catalog).length === 0
  ));
}

export function missingRunNodesSummary(
  items: Array<{ node: GraphNode; roles: GraphEdgeRole[] }>,
  roleLabel: (role: GraphEdgeRole) => string,
): string {
  return items
    .map(({ node, roles }) => `${node.title}: ${roles.map(roleLabel).join(" · ")}`)
    .join(" · ");
}

export function graphPortVisualState(
  graph: GraphProjection,
  nodeId: string,
  handleType: "source" | "target",
  connection: { inProgress: boolean; fromNodeId: string | null; fromType: "source" | "target" | null },
  catalog: GraphNodeCatalog | null | undefined,
  role?: string | null,
): WorkflowCanvasPortVisualState {
  if (handleType === "target" && role) {
    const node = graph.nodes.find((item) => item.id === nodeId);
    const spec = node ? graphCatalogNode(catalog, node.node_type) : null;
    const port = spec?.accepts.find((input) => input.role === role);
    if (port?.required_to_run && node && !node.incoming.some((edge) => edge.role === role)) {
      if (!connection.inProgress) return "missing";
    }
  }
  if (!connection.inProgress || !connection.fromNodeId || !connection.fromType) return "idle";
  if (connection.fromNodeId === nodeId && connection.fromType === handleType) return "origin";
  if (connection.fromType === handleType) return "idle";
  const sourceNodeId = handleType === "target" ? connection.fromNodeId : nodeId;
  const targetNodeId = handleType === "target" ? nodeId : connection.fromNodeId;
  const targetHandle = handleType === "target" ? role : undefined;
  return isGraphConnectionValid(graph, sourceNodeId, targetNodeId, catalog, targetHandle) ? "valid-target" : "invalid-target";
}

function wouldCreateCycle(graph: GraphProjection, sourceNodeId: string, targetNodeId: string, ignoredEdgeId?: string | null): boolean {
  const outgoing = new Map<string, string[]>();
  for (const edge of graph.edges) {
    if (edge.id === ignoredEdgeId) continue;
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
  return nodeIsProcessing(node, catalog);
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

const DATA_TYPE_LABEL_KEYS: Record<GraphCatalogInputContract["data_type"], TranslationKey> = {
  product_facts: "graph.dataType.product_facts",
  image_asset: "graph.dataType.image_asset",
  creative_brief: "graph.dataType.creative_brief",
  visual_system: "graph.dataType.visual_system",
  prompt: "graph.dataType.prompt",
};

export function graphDataTypeLabelKey(
  dataType: GraphCatalogInputContract["data_type"] | string,
): TranslationKey | null {
  return DATA_TYPE_LABEL_KEYS[dataType as GraphCatalogInputContract["data_type"]] ?? null;
}
