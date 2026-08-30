import type { GraphDocumentOrigin, GraphNode } from "../../../lib/types";

export function isContentGraphNodeType(nodeType: GraphNode["node_type"]): boolean {
  return nodeType === "creative_brief" || nodeType === "visual_system" || nodeType === "prompt_generation";
}

export function graphDocumentOrigin(node: GraphNode | { document_origin?: string | null }): GraphDocumentOrigin | "" {
	const column = node.document_origin;
	if (column === "seed" || column === "generated" || column === "authored") return column;
	return "";
}
