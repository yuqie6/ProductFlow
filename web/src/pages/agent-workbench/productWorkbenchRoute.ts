import type { AgentWorkbenchBootstrap } from "../../lib/types";

export type ProductWorkbenchRouteTarget = "agent_v2" | "legacy_v1" | "legacy_empty";

export function productWorkbenchRouteTarget(
  bootstrap: AgentWorkbenchBootstrap,
): ProductWorkbenchRouteTarget {
  if (bootstrap.mode === "agent_v2") {
    return "agent_v2";
  }
  return bootstrap.has_existing_v1_workflow ? "legacy_v1" : "legacy_empty";
}
