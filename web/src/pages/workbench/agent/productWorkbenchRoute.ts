/**
 * 根据 Agent bootstrap 与 live 图选择商品工作台表面。
 *
 * 缺图是 404（当作 null），缺 Agent 工作台是 409。直接创建可以先有图没有对话；
 * Agent 优先创建可以先有对话没有图。
 */

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

export function isHttpErrorStatus(error: unknown, status: number): boolean {
  if (typeof error !== "object" || error === null || !("status" in error)) {
    return false;
  }
  return (error as { status: unknown }).status === status;
}

export function isWorkflowGraphMissing(error: unknown): boolean {
  return isHttpErrorStatus(error, 404);
}

export function isAgentWorkbenchMissing(error: unknown): boolean {
  return isHttpErrorStatus(error, 409);
}

/** 页面加载只读取；没有对话时 409，由画布面打开侧栏再 ensure。 */
export function loadProductWorkbenchAgent(
  loaders: {
    getAgentWorkbench: (
      productId: string,
      agentSessionId?: string | null,
      agentTaskId?: string | null,
    ) => Promise<AgentWorkbenchBootstrap>;
    ensureAgentWorkbench: (
      productId: string,
      agentSessionId?: string | null,
    ) => Promise<AgentWorkbenchBootstrap>;
  },
  productId: string,
  agentSessionId?: string | null,
  agentTaskId?: string | null,
): Promise<AgentWorkbenchBootstrap> {
  void loaders.ensureAgentWorkbench;
  return loaders.getAgentWorkbench(productId, agentSessionId, agentTaskId);
}

/** 缺少 live 图当作 null，让 Agent 优先创建仍能打开。 */
export async function readWorkflowGraphOrNull(
  load: () => Promise<GraphProjection>,
): Promise<GraphProjection | null> {
  try {
    return await load();
  } catch (error) {
    if (isWorkflowGraphMissing(error)) return null;
    throw error;
  }
}

/** 图 404 表示还没有图，不是致命错误。Agent 409 表示对话不在了。 */
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
