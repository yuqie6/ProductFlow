import type { GraphChangeSet, GraphNode } from "../../../lib/types";

/** 位置同步没有业务语义，可以在最新 revision 上安全重放一次。 */
export function isMoveNodesOnly(operations: GraphChangeSet["operations"]): boolean {
  return operations.length > 0 && operations.every((operation) => operation.op === "move_nodes");
}

export function buildNodeCommitChangeSet(input: {
  node: GraphNode;
  title?: string;
  config?: Record<string, unknown>;
  boundAssetId?: string | null;
  baseGraphRevision: number;
}): GraphChangeSet | null {
  const operations: GraphChangeSet["operations"] = [];
  if (input.title && input.title !== input.node.title) {
    operations.push({ op: "rename_node", node_ref: input.node.id, title: input.title });
  }
  if (input.config || input.boundAssetId !== undefined) {
    operations.push({
      op: "update_node_config",
      node_ref: input.node.id,
      config: input.config ?? input.node.config,
      bound_asset_id: input.boundAssetId === undefined ? input.node.bound_asset_id : input.boundAssetId,
    });
  }
  if (!operations.length) return null;
  return {
    base_graph_revision: input.baseGraphRevision,
    summary: "更新节点",
    operations,
  };
}
