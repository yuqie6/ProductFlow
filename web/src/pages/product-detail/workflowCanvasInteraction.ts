import type { CanvasInteractionMode } from "./types";

export const WORKFLOW_CANVAS_SELECTION_KEY_CODE = "Shift";
export const WORKFLOW_CANVAS_MULTI_SELECTION_KEY_CODES = ["Control", "Meta"];
export const WORKFLOW_CANVAS_PAN_ACTIVATION_KEY_CODE = "Space";
export const WORKFLOW_CANVAS_ZOOM_ACTIVATION_KEY_CODES = ["Control", "Meta"];
export const WORKFLOW_CANVAS_PAN_BUTTONS = [0];

const MOUSE_NODE_VISUAL_DRAG_THRESHOLD = 0;
const MOUSE_NODE_CLICK_COMMIT_DISTANCE = 3;
const TOUCH_NODE_POINTER_DRAG_THRESHOLD = 6;

export interface WorkflowCanvasInteractionPolicy {
  nodesDraggable: boolean;
  nodesConnectable: boolean;
  selectionOnDrag: boolean;
  selectNodesOnDrag: boolean;
  selectionKeyCode: string | null;
  multiSelectionKeyCode: string[] | null;
  panOnDrag: number[];
  canSelectByBox: boolean;
  nodeDragThreshold: number;
  nodeClickDistance: number;
}

export function deriveWorkflowCanvasInteractionPolicy({
  compact,
  mode,
  locked,
  connectionEditing,
}: {
  compact: boolean;
  mode: CanvasInteractionMode;
  locked: boolean;
  connectionEditing: boolean;
}): WorkflowCanvasInteractionPolicy {
  const nodesDraggable = !locked && (!compact || mode === "edit");
  return {
    nodesDraggable,
    nodesConnectable: nodesDraggable && connectionEditing,
    selectionOnDrag: false,
    selectNodesOnDrag: false,
    selectionKeyCode: compact ? null : WORKFLOW_CANVAS_SELECTION_KEY_CODE,
    multiSelectionKeyCode: compact ? null : WORKFLOW_CANVAS_MULTI_SELECTION_KEY_CODES,
    panOnDrag: WORKFLOW_CANVAS_PAN_BUTTONS,
    canSelectByBox: !compact,
    nodeDragThreshold: compact
      ? TOUCH_NODE_POINTER_DRAG_THRESHOLD
      : MOUSE_NODE_VISUAL_DRAG_THRESHOLD,
    nodeClickDistance: compact
      ? TOUCH_NODE_POINTER_DRAG_THRESHOLD
      : MOUSE_NODE_CLICK_COMMIT_DISTANCE,
  };
}

export function shouldToggleWorkflowCanvasNodeSelection({
  compact,
  mode,
  ctrlKey,
  metaKey,
  shiftKey,
}: {
  compact: boolean;
  mode: CanvasInteractionMode;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
}): boolean {
  return (compact && mode === "select") || ctrlKey || metaKey || shiftKey;
}

export function selectWorkflowCanvasNode(
  selectedNodeIds: string[],
  nodeId: string,
  toggle: boolean,
): string[] {
  if (!toggle) {
    return selectedNodeIds.length === 1 && selectedNodeIds[0] === nodeId
      ? selectedNodeIds
      : [nodeId];
  }
  return selectedNodeIds.includes(nodeId)
    ? selectedNodeIds.filter((selectedNodeId) => selectedNodeId !== nodeId)
    : [...selectedNodeIds, nodeId];
}
