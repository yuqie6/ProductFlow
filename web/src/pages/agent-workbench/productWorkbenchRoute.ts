import type { AgentWorkbenchBootstrap } from "../../lib/types";

type AgentV2WorkbenchBootstrap = Extract<AgentWorkbenchBootstrap, { mode: "agent_v2" }>;
type LegacyWorkbenchBootstrap = Extract<AgentWorkbenchBootstrap, { mode: "legacy_v1" }>;

export type ProductWorkbenchRouteInput =
  | (Pick<AgentV2WorkbenchBootstrap, "mode" | "active_workflow"> & {
      workflow_draft: Pick<
        AgentV2WorkbenchBootstrap["workflow_draft"],
        "intake" | "current_revision" | "recipe_seed" | "legacy_archive_seed"
      >;
    })
  | Pick<LegacyWorkbenchBootstrap, "mode" | "has_existing_v1_workflow">;

export type ProductWorkbenchRouteTarget =
  | "agent_v2"
  | "agent_intake"
  | "legacy_v1"
  | "legacy_empty";

export function productWorkbenchRouteTarget(
  bootstrap: ProductWorkbenchRouteInput,
): ProductWorkbenchRouteTarget {
  if (bootstrap.mode === "agent_v2") {
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
  return bootstrap.has_existing_v1_workflow ? "legacy_v1" : "legacy_empty";
}

export function agentProductIntakeResumePath(conversationId: string): string {
  const params = new URLSearchParams({ workspace: conversationId });
  return `/products/new?${params}`;
}
