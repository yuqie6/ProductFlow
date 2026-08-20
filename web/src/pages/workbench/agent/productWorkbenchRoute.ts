import type { AgentWorkbenchBootstrap } from "../../../lib/types";

type AgentV2WorkbenchBootstrap = AgentWorkbenchBootstrap;

export type ProductWorkbenchRouteInput = Pick<AgentV2WorkbenchBootstrap, "active_workflow"> & {
  workflow_draft: Pick<
    AgentV2WorkbenchBootstrap["workflow_draft"],
    "intake" | "current_revision" | "recipe_seed" | "legacy_archive_seed"
  >;
};

export type ProductWorkbenchRouteTarget = "agent_v2" | "agent_intake";

export function productWorkbenchRouteTarget(
  bootstrap: ProductWorkbenchRouteInput,
): ProductWorkbenchRouteTarget {
  const draft = bootstrap.workflow_draft;
  if (
    bootstrap.active_workflow === null &&
    draft.intake === null &&
    draft.current_revision === null &&
    draft.recipe_seed === null &&
    draft.legacy_archive_seed === null
  ) {
    return "agent_intake";
  }
  return "agent_v2";
}

export function agentProductIntakeResumePath(
  conversationId: string,
  agentSessionId?: string | null,
  agentTaskId?: string | null,
): string {
  const params = new URLSearchParams({ workspace: conversationId });
  if (agentSessionId) {
    params.set("agent_session_id", agentSessionId);
  }
  if (agentTaskId) {
    params.set("agent_task_id", agentTaskId);
  }
  return `/products/new?${params}`;
}
