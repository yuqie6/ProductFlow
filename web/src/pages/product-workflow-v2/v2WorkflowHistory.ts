import type {
  CreateReferenceWorkflowNodeV2Input,
  ProductWorkflowV2,
  WorkflowNodeTypeV2,
} from "../../lib/types";

export interface V2WorkflowNodePosition {
  node_id: string;
  position_x: number;
  position_y: number;
}

export type V2WorkflowHistoryAction =
  | {
      kind: "layout";
      before: V2WorkflowNodePosition[];
      after: V2WorkflowNodePosition[];
    }
  | {
      kind: "reference-created";
      nodeId: string;
      input: Omit<CreateReferenceWorkflowNodeV2Input, "expected_edit_version">;
    }
  | {
      kind: "node-duplicated";
      sourceNodeId: string;
      sourceNodeType: WorkflowNodeTypeV2;
      createdRootNodeId: string;
    }
  | {
      kind: "edge";
      initialOperation: "created" | "deleted";
      sourceNodeId: string;
      targetNodeId: string;
      edgeId: string;
    };

export interface V2WorkflowHistoryState {
  undo: V2WorkflowHistoryAction[];
  redo: V2WorkflowHistoryAction[];
}

export interface V2WorkflowVersionIdentity {
  workflowId: string;
  editVersion: number;
}

export const EMPTY_V2_WORKFLOW_HISTORY: V2WorkflowHistoryState = { undo: [], redo: [] };

export function shouldClearV2WorkflowHistory(
  previous: V2WorkflowVersionIdentity,
  next: V2WorkflowVersionIdentity,
  locallyAcceptedEditVersion: number | null,
): boolean {
  if (previous.workflowId === next.workflowId && previous.editVersion === next.editVersion) {
    return false;
  }
  if (previous.workflowId !== next.workflowId) {
    return true;
  }
  return locallyAcceptedEditVersion !== next.editVersion;
}

export function captureV2WorkflowPositions(
  workflow: ProductWorkflowV2,
  nodeIds: Iterable<string>,
): V2WorkflowNodePosition[] {
  const selected = new Set(nodeIds);
  return workflow.nodes
    .filter((node) => selected.has(node.id))
    .map((node) => ({
      node_id: node.id,
      position_x: node.position_x,
      position_y: node.position_y,
    }))
    .sort((left, right) => left.node_id.localeCompare(right.node_id));
}

export function findAddedV2RootNodeId(
  before: ProductWorkflowV2,
  after: ProductWorkflowV2,
  rootType: WorkflowNodeTypeV2,
): string {
  const beforeIds = new Set(before.nodes.map((node) => node.id));
  const candidates = after.nodes.filter(
    (node) => node.node_type === rootType && !beforeIds.has(node.id),
  );
  if (candidates.length !== 1) {
    throw new Error("结构命令没有生成唯一的根节点");
  }
  return candidates[0].id;
}

export function findV2WorkflowEdgeId(
  workflow: ProductWorkflowV2,
  sourceNodeId: string,
  targetNodeId: string,
): string {
  const candidates = workflow.edges.filter(
    (edge) => edge.source_node_id === sourceNodeId && edge.target_node_id === targetNodeId,
  );
  if (candidates.length !== 1) {
    throw new Error("结构命令没有生成唯一的连线");
  }
  return candidates[0].id;
}
