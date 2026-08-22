import { ApiError } from "../../../lib/api";
import type { AgentWorkbenchBootstrap, GraphProjection } from "../../../lib/types";

export type ProductWorkbenchRouteInput = {
  workflow_draft: {
    intake: AgentWorkbenchBootstrap["workflow_draft"]["intake"];
    current_revision: { id: string } | null;
    recipe_seed: AgentWorkbenchBootstrap["workflow_draft"]["recipe_seed"];
    legacy_archive_seed: AgentWorkbenchBootstrap["workflow_draft"]["legacy_archive_seed"];
  };
};

export type ProductWorkbenchRouteTarget = "agent" | "agent_intake";

export type ProductWorkbenchSurface<TAgent extends ProductWorkbenchRouteInput = AgentWorkbenchBootstrap> =
  | { kind: "loading" }
  | { kind: "error"; error: unknown }
  | { kind: "intake"; bootstrap: TAgent }
  | { kind: "agent"; bootstrap: TAgent }
  | { kind: "graph"; graph: GraphProjection };

export function isWorkflowGraphMissing(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

export function isAgentWorkbenchMissing(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409;
}

export function resolveProductWorkbenchSurface<TAgent extends ProductWorkbenchRouteInput>(input: {
  graph?: GraphProjection;
  graphPending: boolean;
  graphError: unknown;
  agent?: TAgent;
  agentPending: boolean;
  agentError: unknown;
}): ProductWorkbenchSurface<TAgent> {
  if (input.graphPending || input.agentPending) {
    return { kind: "loading" };
  }
  if (input.graphError && !isWorkflowGraphMissing(input.graphError)) {
    return { kind: "error", error: input.graphError };
  }
  if (input.agentError && !isAgentWorkbenchMissing(input.agentError)) {
    return { kind: "error", error: input.agentError };
  }
  if (input.agent) {
    if (productWorkbenchRouteTarget(input.agent) === "agent_intake" && !input.graph) {
      return { kind: "intake", bootstrap: input.agent };
    }
    return { kind: "agent", bootstrap: input.agent };
  }
  if (input.graph) {
    return { kind: "graph", graph: input.graph };
  }
  return { kind: "error", error: input.agentError ?? input.graphError ?? new Error("workbench unavailable") };
}

export function productWorkbenchRouteTarget(
  bootstrap: ProductWorkbenchRouteInput,
): ProductWorkbenchRouteTarget {
  void bootstrap;
  return "agent";
}

export function agentProductWorkbenchPath(
  productId: string,
  agentSessionId?: string | null,
  agentTaskId?: string | null,
): string {
  const params = new URLSearchParams();
  if (agentSessionId) {
    params.set("agent_session_id", agentSessionId);
  }
  if (agentTaskId) {
    params.set("agent_task_id", agentTaskId);
  }
  const query = params.size ? `?${params}` : "";
  return `/products/${encodeURIComponent(productId)}${query}`;
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
