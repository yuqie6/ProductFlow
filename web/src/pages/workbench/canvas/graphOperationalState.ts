import type { TranslationKey } from "../../../lib/i18n";
import type { GraphNode, WorkflowNodeDisplayStatus } from "../../../lib/types";

export interface GraphNodeOperationalState {
  status: WorkflowNodeDisplayStatus | "frozen";
  labelKey: TranslationKey;
}

export function graphNodeOperationalState(node: GraphNode): GraphNodeOperationalState {
  if (node.pending_candidate_artifact_id) {
    return { status: "frozen", labelKey: "graph.nodeState.review" };
  }
  if (node.node_type === "product_source") {
    return node.config_status === "ready"
      ? { status: "succeeded", labelKey: "graph.nodeState.sourceAvailable" }
      : { status: "idle", labelKey: "graph.nodeState.needsContent" };
  }
  if (node.node_type === "image_asset") {
    return node.bound_asset_id
      ? { status: "succeeded", labelKey: "graph.nodeState.assetAvailable" }
      : { status: "idle", labelKey: "graph.nodeState.needsBinding" };
  }
  if (node.node_type === "creative_brief" || node.node_type === "visual_system" || node.node_type === "image_prompt") {
    if (node.config_status === "incomplete") {
      return { status: "idle", labelKey: "graph.nodeState.needsContent" };
    }
    if (node.document_origin === "seed") {
      return { status: "idle", labelKey: "graph.nodeState.canGenerate" };
    }
    return { status: "succeeded", labelKey: "graph.nodeState.documentAvailable" };
  }
  if (node.config_status === "incomplete") {
    return { status: "idle", labelKey: "graph.nodeState.needsInput" };
  }
  if (node.config_status === "stale") {
    return { status: "idle", labelKey: "graph.nodeState.inputChanged" };
  }
  return { status: "idle", labelKey: "graph.nodeState.canGenerate" };
}

export function displayNodeState(node: GraphNode, runtimeStatus: WorkflowNodeDisplayStatus): GraphNodeOperationalState {
  if (runtimeStatus === "queued" || runtimeStatus === "running" || runtimeStatus === "failed" || runtimeStatus === "cancelled" || runtimeStatus === "unknown") {
    return { status: runtimeStatus, labelKey: `detail.nodeStatus.${runtimeStatus}` as TranslationKey };
  }
  return graphNodeOperationalState(node);
}
