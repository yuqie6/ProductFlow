import type { WorkflowNodeV2 } from "../../../lib/types";

export function shouldOpenArtifactsForSelection(
  currentNodeIds: string[],
  nextNodeIds: string[],
  nodes: WorkflowNodeV2[],
): boolean {
  const selectionChanged = nextNodeIds.length !== currentNodeIds.length
    || nextNodeIds.some((nodeId, index) => nodeId !== currentNodeIds[index]);
  return selectionChanged
    && nextNodeIds.length === 1
    && nodes.some((node) => node.id === nextNodeIds[0] && node.node_type === "image_generation");
}
