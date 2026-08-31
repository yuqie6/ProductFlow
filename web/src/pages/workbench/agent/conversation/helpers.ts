import { ApiError } from "../../../../lib/api";
import type {
  AgentCanvasFocus,
  AgentTurn,
  AgentWorkflowRunRequest,
  GraphProjection,
} from "../../../../lib/types";
import { selectAgentToolSteps, type AgentTurnEventState } from "../agentEventReducer";

export function canSubmitAgentConversationMessage(input: {
  activeTurn: AgentTurn | null | undefined;
}): boolean {
  return input.activeTurn == null;
}

export function agentConversationSubmitTaskId(routeTaskId: string | null | undefined): string | null {
  return routeTaskId ?? null;
}

export function resolveAgentCanvasFocusNodeIds(
  focus: Pick<AgentCanvasFocus, "node_ids" | "edge_ids" | "group_ids">,
  graph: GraphProjection | null | undefined,
): string[] {
  const collected = [...focus.node_ids];
  if (graph) {
    for (const edge of graph.edges) {
      if (focus.edge_ids.includes(edge.id)) {
        collected.push(edge.source_node_id, edge.target_node_id);
      }
    }
    for (const group of graph.groups) {
      if (focus.group_ids.includes(group.id)) {
        collected.push(...group.member_ids);
      }
    }
  }
  const known = graph ? new Set(graph.nodes.map((node) => node.id)) : null;
  const unique: string[] = [];
  const seen = new Set<string>();
  for (const id of collected) {
    if (seen.has(id) || (known && !known.has(id))) continue;
    seen.add(id);
    unique.push(id);
  }
  return unique;
}

export type AgentWorkflowRunRequestView = Omit<AgentWorkflowRunRequest, "expected_workflow_revision"> & {
  expected_workflow_revision: number | null;
};

export function workflowRequestFromTurn(
  turn: AgentTurn | null | undefined,
  eventStates: Readonly<Record<string, AgentTurnEventState>> | undefined,
): AgentWorkflowRunRequestView | null {
  if (!turn) return null;
  const steps = selectAgentToolSteps(turn, eventStates?.[turn.id] ?? null);
  const step = [...steps].reverse().find((item) => item.kind === "request_workflow_run");
  const meta = step?.meta ?? step?.details;
  const requestId = meta?.request_id ?? turn.workflow_run_request_id;
  if (!meta?.pending_confirmation || !meta.workflow_id || !requestId) return null;
  const revision = meta.expected_workflow_revision;
  return {
    id: requestId,
    conversation_id: turn.conversation_id,
    task_id: turn.task_id,
    product_id: meta.product_id ?? "",
    product_name: "",
    workflow_id: meta.workflow_id,
    workflow_title: meta.workflow_title ?? "",
    expected_workflow_revision: typeof revision === "number" && revision >= 1 ? revision : null,
    status: "awaiting_confirmation",
    workflow_run_id: null,
    workflow_run_status: null,
    source_step_id: step?.step_id ?? "",
    failure_reason: null,
    confirmed_at: null,
    finished_at: null,
    created_at: turn.created_at,
    updated_at: turn.updated_at,
    run_scope: "graph",
    target_node_id: null,
    target_node_ids: [],
    force: false,
    document_action: null,
  };
}

export function mergeWorkflowRunRequest(
  fetched: AgentWorkflowRunRequest | null | undefined,
  journal: AgentWorkflowRunRequestView | null,
): AgentWorkflowRunRequestView | null {
  return fetched ?? journal;
}

export function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}

export function errorDetailOrNull(error: unknown, fallback = "Agent 请求失败"): string | null {
  return error ? errorDetail(error, fallback) : null;
}
