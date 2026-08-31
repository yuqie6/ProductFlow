import type {
  GraphPlannedAction,
  GraphRunPreviewResponse,
  GraphRunSubmitInput,
} from "../../../lib/types";

const ACTION_RANK: Record<GraphPlannedAction, number> = {
  blocked: 4,
  generate: 3,
  frozen: 2,
  reuse: 1,
};

export function plannedActionsFromPreview(
  preview: GraphRunPreviewResponse | null | undefined,
): Record<string, GraphPlannedAction> {
  const next: Record<string, GraphPlannedAction> = {};
  for (const node of preview?.nodes ?? []) next[node.node_id] = node.planned_action;
  return next;
}

export function dominantPlannedAction(
  actions: readonly (GraphPlannedAction | null | undefined)[],
): GraphPlannedAction | null {
  let best: GraphPlannedAction | null = null;
  for (const action of actions) {
    if (!action) continue;
    if (!best || ACTION_RANK[action] > ACTION_RANK[best]) best = action;
  }
  return best;
}

export function plannedActionClassName(action: GraphPlannedAction | null | undefined): string {
  switch (action) {
    case "generate":
      return "outline outline-2 outline-accent/90";
    case "reuse":
      return "outline outline-2 outline-border-l3 opacity-80";
    case "frozen":
      return "outline outline-2 outline-state-frozen";
    case "blocked":
      return "outline outline-2 outline-state-error/90";
    default:
      return "";
  }
}

export function runPreviewPointerHandlers(
  input: GraphRunSubmitInput | null | undefined,
  show: ((input: GraphRunSubmitInput) => void) | undefined,
  hide: (() => void) | undefined,
): {
  onMouseEnter?: () => void;
  onMouseLeave?: () => void;
  onFocus?: () => void;
  onBlur?: () => void;
} {
  if (!input || !show) return {};
  return {
    onMouseEnter: () => show(input),
    onMouseLeave: hide,
    onFocus: () => show(input),
    onBlur: hide,
  };
}

export function failedNodesRunInput(run: {
  node_runs: ReadonlyArray<{ status: string; node_id?: string | null }>;
}): GraphRunSubmitInput | null {
  const nodeIds = run.node_runs
    .filter((nodeRun) => nodeRun.status === "failed" && nodeRun.node_id)
    .map((nodeRun) => nodeRun.node_id as string);
  if (!nodeIds.length) return null;
  return { scope: "selection", node_ids: nodeIds };
}
