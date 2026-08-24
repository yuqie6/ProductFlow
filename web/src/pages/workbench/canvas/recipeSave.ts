/**
 * 按 live 图选择决定配方抽取范围。
 *
 * 配方只存结构和配置，不存商品身份、绑定资产、生成结果或媒体 bytes。
 */

import type { GraphProjection } from "../../../lib/types";

export type RecipeSaveKind = "workflow" | "group" | "selection";

export interface RecipeSaveRequest {
  source_type: RecipeSaveKind;
  group_id?: string | null;
  node_ids?: string[];
  expected_graph_revision: number;
}

export function uniqueSelectedGroupId(graph: GraphProjection, selectedNodeIds: string[]): string | null {
  const groupIds = new Set(
    graph.nodes
      .filter((node) => selectedNodeIds.includes(node.id) && node.group_id)
      .map((node) => node.group_id as string),
  );
  if (groupIds.size !== 1) return null;
  return [...groupIds][0] ?? null;
}

export function resolveRecipeSaveRequest(
  graph: GraphProjection,
  selectedNodeIds: string[],
  enteredGroupId: string | null,
  kind: RecipeSaveKind,
  explicitNodeIds?: string[],
): RecipeSaveRequest | null {
  if (kind === "workflow") {
    if (!graph.nodes.length) return null;
    return { source_type: "workflow", expected_graph_revision: graph.revision };
  }
  if (kind === "group") {
    const groupId = enteredGroupId ?? uniqueSelectedGroupId(graph, selectedNodeIds);
    if (!groupId || !graph.groups.some((group) => group.id === groupId)) return null;
    if (!graph.nodes.some((node) => node.group_id === groupId)) return null;
    return { source_type: "group", group_id: groupId, expected_graph_revision: graph.revision };
  }
  const nodeIds = [...new Set(explicitNodeIds?.length ? explicitNodeIds : selectedNodeIds)];
  if (!nodeIds.length) return null;
  if (nodeIds.some((nodeId) => !graph.nodes.some((node) => node.id === nodeId))) return null;
  return { source_type: "selection", node_ids: nodeIds, expected_graph_revision: graph.revision };
}
