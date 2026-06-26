import type { ProductWorkflow, WorkflowEdge, WorkflowNode, WorkflowNodeType } from "../../lib/types";

export interface WorkflowHistoryPoint {
  x: number;
  y: number;
}

export interface RestorableWorkflowNode {
  oldId: string;
  node_type: WorkflowNodeType;
  title: string;
  position_x: number;
  position_y: number;
  config_json: Record<string, unknown>;
}

export interface RestorableWorkflowEdge {
  oldId: string;
  source_node_id: string;
  target_node_id: string;
  source_handle: string | null;
  target_handle: string | null;
}

const ARTIFACT_SPECIFIC_CONFIG_KEYS = new Set([
  "copy_set_id",
  "creative_brief_id",
  "download_url",
  "filled_reference_node_ids",
  "filled_source_asset_ids",
  "generated_poster_variant_ids",
  "node_id",
  "poster_variant_id",
  "poster_variant_ids",
  "preview_url",
  "product_id",
  "source_asset_id",
  "source_asset_ids",
  "source_poster_variant_id",
  "storage_path",
  "thumbnail_url",
  "workflow_id",
]);
const ARTIFACT_SPECIFIC_KEY_SUFFIXES = ["_id", "_ids", "_url", "_path"];

export type WorkflowHistoryStep =
  | { kind: "deleteNodes"; nodeIds: string[] }
  | { kind: "restoreNodes"; nodes: RestorableWorkflowNode[]; edges: RestorableWorkflowEdge[] }
  | { kind: "deleteEdges"; edgeIds: string[] }
  | { kind: "restoreEdges"; edges: RestorableWorkflowEdge[] }
  | {
      kind: "moveNodes";
      moves: Array<{ nodeId: string; from: WorkflowHistoryPoint; to: WorkflowHistoryPoint }>;
    };

function sanitizeRestorableConfigValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(sanitizeRestorableConfigValue);
  }
  if (value && typeof value === "object") {
    const sanitized: Record<string, unknown> = {};
    for (const [key, nestedValue] of Object.entries(value)) {
      const normalizedKey = key.toLowerCase();
      if (
        ARTIFACT_SPECIFIC_CONFIG_KEYS.has(normalizedKey) ||
        ARTIFACT_SPECIFIC_KEY_SUFFIXES.some((suffix) => normalizedKey.endsWith(suffix))
      ) {
        continue;
      }
      sanitized[key] = sanitizeRestorableConfigValue(nestedValue);
    }
    return sanitized;
  }
  return value;
}

export function sanitizeRestorableNodeConfig(config: Record<string, unknown>): Record<string, unknown> {
  return sanitizeRestorableConfigValue(config) as Record<string, unknown>;
}

export function workflowHistoryStepRequiresConfirmation(step: WorkflowHistoryStep): boolean {
  return step.kind === "deleteNodes" || step.kind === "deleteEdges";
}

export function getWorkflowStructureSignature(workflow: ProductWorkflow): string {
  const nodeIds = workflow.nodes.map((node) => node.id).sort().join(",");
  const edgeIds = workflow.edges.map((edge) => edge.id).sort().join(",");
  return [workflow.id, workflow.updated_at, nodeIds, edgeIds].join("|");
}

export function getInternalWorkflowEdges(edges: WorkflowEdge[], nodeIds: Set<string>): WorkflowEdge[] {
  return edges.filter((edge) => nodeIds.has(edge.source_node_id) && nodeIds.has(edge.target_node_id));
}

export function workflowNodeToRestorableNode(node: WorkflowNode): RestorableWorkflowNode {
  return {
    oldId: node.id,
    node_type: node.node_type,
    title: node.title,
    position_x: node.position_x,
    position_y: node.position_y,
    config_json: sanitizeRestorableNodeConfig(node.config_json),
  };
}

export function workflowEdgeToRestorableEdge(edge: WorkflowEdge): RestorableWorkflowEdge {
  return {
    oldId: edge.id,
    source_node_id: edge.source_node_id,
    target_node_id: edge.target_node_id,
    source_handle: edge.source_handle,
    target_handle: edge.target_handle,
  };
}

export function createRestoreNodesStep(nodes: WorkflowNode[], edges: WorkflowEdge[]): WorkflowHistoryStep {
  return {
    kind: "restoreNodes",
    nodes: nodes.map(workflowNodeToRestorableNode),
    edges: edges.map(workflowEdgeToRestorableEdge),
  };
}

export function createRestoreEdgesStep(edges: WorkflowEdge[]): WorkflowHistoryStep {
  return {
    kind: "restoreEdges",
    edges: edges.map(workflowEdgeToRestorableEdge),
  };
}
