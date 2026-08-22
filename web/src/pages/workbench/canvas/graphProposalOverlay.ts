import type { GraphEdge, GraphNode, GraphProjection, GraphProposalOverlay } from "../../../lib/types";

export type GraphProposalState = "added" | "deleted" | "changed";

export function graphProposalNodeStates(proposal: GraphProposalOverlay | null | undefined): Record<string, GraphProposalState> {
  if (!proposal || proposal.stale) return {};
  const states: Record<string, GraphProposalState> = {};
  for (const nodeId of proposal.deleted_node_ids) states[nodeId] = "deleted";
  for (const nodeId of proposal.changed_node_ids) states[nodeId] = "changed";
  for (const node of proposal.added_nodes) states[node.id] = "added";
  return states;
}

export function graphProposalEdgeStates(proposal: GraphProposalOverlay | null | undefined): Record<string, GraphProposalState> {
  if (!proposal || proposal.stale) return {};
  const states: Record<string, GraphProposalState> = {};
  for (const edgeId of proposal.deleted_edge_ids) states[edgeId] = "deleted";
  for (const edge of proposal.added_edges) states[edge.id] = "added";
  return states;
}

export function overlayGraphProposal(graph: GraphProjection): GraphProjection {
  const proposal = graph.pending_proposal;
  if (!proposal || proposal.stale) return graph;
  const existingIds = new Set(graph.nodes.map((node) => node.id));
  const addedNodes: GraphNode[] = proposal.added_nodes
    .filter((node) => !existingIds.has(node.id))
    .map((node) => ({
      id: node.id,
      node_type: node.node_type,
      title: node.title,
      position_x: node.position_x,
      position_y: node.position_y,
      config: node.config,
      bound_asset_id: null,
      group_id: node.group_id,
      preview_asset_id: null,
      config_status: "incomplete",
      unused: true,
      incoming: [],
      outgoing: [],
    }));
  const existingEdgeIds = new Set(graph.edges.map((edge) => edge.id));
  const addedEdges: GraphEdge[] = proposal.added_edges
    .filter((edge) => !existingEdgeIds.has(edge.id))
    .map((edge) => ({
      id: edge.id,
      source_node_id: edge.source_node_id,
      target_node_id: edge.target_node_id,
      data_type: edge.data_type,
      role: edge.role,
      order: edge.order,
    }));
  return {
    ...graph,
    nodes: [...graph.nodes, ...addedNodes],
    edges: [...graph.edges, ...addedEdges],
  };
}
