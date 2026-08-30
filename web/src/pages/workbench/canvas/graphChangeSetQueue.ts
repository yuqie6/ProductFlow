import type { GraphChangeSet } from "../../../lib/types";

/** 位置同步没有业务语义，可以在最新 revision 上安全重放一次。 */
export function isMoveNodesOnly(operations: GraphChangeSet["operations"]): boolean {
  return operations.length > 0 && operations.every((operation) => operation.op === "move_nodes");
}
