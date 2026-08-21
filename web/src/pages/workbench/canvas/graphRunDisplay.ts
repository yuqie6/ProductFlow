import type { TranslationKey } from "../../../lib/i18n";
import type {
  GraphNodeRun,
  GraphProjection,
  GraphRun,
  GraphRunScope,
  WorkflowNodeStatus,
} from "../../../lib/types";

export interface GraphNodeRunPresentation {
  status: WorkflowNodeStatus;
  failureReason: string | null;
  lastRunAt: string | null;
  retryable: boolean;
  runId: string | null;
}

const LIVE_RUN_STATUSES = new Set(["queued", "running"]);

export function graphNodeRunPresentations(
  runs: readonly GraphRun[],
): Record<string, GraphNodeRunPresentation> {
  const presentations: Record<string, GraphNodeRunPresentation> = {};
  for (const run of runs) {
    for (const nodeRun of run.node_runs) {
      if (!nodeRun.node_id || presentations[nodeRun.node_id]) continue;
      presentations[nodeRun.node_id] = presentationFromNodeRun(run, nodeRun);
    }
  }
  const live = runs.find((run) => LIVE_RUN_STATUSES.has(run.status));
  if (!live) return presentations;
  for (const nodeRun of live.node_runs) {
    if (!nodeRun.node_id) continue;
    presentations[nodeRun.node_id] = presentationFromNodeRun(live, nodeRun);
  }
  return presentations;
}

function presentationFromNodeRun(run: GraphRun, nodeRun: GraphNodeRun): GraphNodeRunPresentation {
  const failed = nodeRun.status === "failed";
  return {
    status: nodeRun.status,
    failureReason: nodeRun.failure_reason ?? (failed ? run.failure_reason : null),
    lastRunAt: nodeRun.finished_at ?? nodeRun.started_at,
    retryable: failed && run.is_retryable,
    runId: run.id,
  };
}

export function graphRunScopeLabelKey(scope: GraphRunScope): TranslationKey {
  if (scope === "node") return "graph.runs.scope.node";
  if (scope === "to_node") return "graph.runs.scope.toNode";
  return "graph.runs.scope.graph";
}

export function graphNodeRunPreviewAssetId(
  nodeRun: GraphNodeRun,
  graph: GraphProjection,
): string | null {
  const output = nodeRun.output;
  if (output && typeof output.product_image_asset_id === "string" && output.product_image_asset_id) {
    return output.product_image_asset_id;
  }
  if (nodeRun.status !== "succeeded" || !nodeRun.node_id) return null;
  return graph.nodes.find((node) => node.id === nodeRun.node_id)?.preview_asset_id ?? null;
}

const CONTEXT_LABEL_KEYS = {
  incoming_edge_ids: "graph.runs.context.incomingEdges",
  input_digest: "graph.runs.context.inputDigest",
  fact_count: "graph.runs.context.factCount",
  brief_count: "graph.runs.context.briefCount",
  reference_asset_ids: "graph.runs.context.referenceAssets",
  visual_system_version_id: "graph.runs.context.visualVersion",
  prompt_artifact_id: "graph.runs.context.promptArtifact",
  prompt_edge_id: "graph.runs.context.promptEdge",
} as const;

export function graphContextEntries(value: Record<string, unknown> | null): Array<{
  key: string;
  labelKey: TranslationKey | null;
  value: string;
}> {
  if (!value) return [];
  return Object.entries(value).flatMap(([key, item]) => {
    const labelKey = CONTEXT_LABEL_KEYS[key as keyof typeof CONTEXT_LABEL_KEYS] ?? null;
    if (!labelKey) return [];
    return [{ key, labelKey, value: formatContextValue(item) }];
  });
}

function formatContextValue(value: unknown): string {
  if (value == null || value === "") return "—";
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (Array.isArray(value)) {
    return value.length ? value.map((item) => formatContextValue(item)).join(" · ") : "—";
  }
  return JSON.stringify(value);
}
