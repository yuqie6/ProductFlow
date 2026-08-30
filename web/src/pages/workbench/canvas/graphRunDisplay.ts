/**
 * 把 WorkflowGraphRun 投影到画布节点状态。
 *
 * 进行中的 queued/running 覆盖历史节点运行。unknown 是无法证明的供应商副作用，不是 UI 加载标记。
 */

import type { TranslationKey } from "../../../lib/i18n";
import type {
  GraphNodeRun,
  GraphPlannedAction,
  GraphProjection,
  GraphRun,
  GraphRunInputTraceEntry,
  GraphRunScope,
  WorkflowNodeStatus,
} from "../../../lib/types";

export interface GraphNodeRunPresentation {
  status: WorkflowNodeStatus;
  failureReason: string | null;
  lastRunAt: string | null;
  retryable: boolean;
  runId: string | null;
  plannedAction: GraphPlannedAction | null;
  progressPhase: string | null;
  elapsedLabel: string | null;
  attemptCount: number;
}

export const LIVE_RUN_STATUSES = new Set(["queued", "running"]);

export function graphRunsAreLive(runs: readonly GraphRun[] | undefined): boolean {
  return Boolean(runs?.some((run) => LIVE_RUN_STATUSES.has(run.status)));
}

export function graphQueuedRuns(runs: readonly GraphRun[] | undefined): GraphRun[] {
  return (runs ?? []).filter((run) => run.status === "queued");
}

export function graphRunningRuns(runs: readonly GraphRun[] | undefined): GraphRun[] {
  return (runs ?? []).filter((run) => run.status === "running");
}

/** 历史运行补缺口；进行中的运行覆盖它仍拥有的每个节点。 */
export function graphNodeRunPresentations(
  runs: readonly GraphRun[],
): Record<string, GraphNodeRunPresentation> {
  const presentations: Record<string, GraphNodeRunPresentation> = {};
  for (const run of runs) {
    for (const nodeRun of run.node_runs) {
      if (!nodeRun.node_id) continue;
      if (presentations[nodeRun.node_id]) continue;
      presentations[nodeRun.node_id] = presentationFromNodeRun(run, nodeRun);
    }
  }
  const live = runs.find((run) => run.status === "running")
    ?? runs.find((run) => run.status === "queued");
  if (live) {
    for (const nodeRun of live.node_runs) {
      if (!nodeRun.node_id) continue;
      presentations[nodeRun.node_id] = presentationFromNodeRun(live, nodeRun);
    }
  }
  return presentations;
}

function presentationFromNodeRun(
  run: GraphRun,
  nodeRun: GraphNodeRun,
): GraphNodeRunPresentation {
  const failed = nodeRun.status === "failed";
  return {
    status: nodeRun.status,
    failureReason: nodeRun.failure_reason ?? (failed ? run.failure_reason : null),
    lastRunAt: nodeRun.finished_at ?? nodeRun.started_at,
    retryable: failed && run.is_retryable,
    runId: run.id,
    plannedAction: nodeRun.planned_action ?? null,
    progressPhase: nodeRun.progress_phase ?? null,
    elapsedLabel: formatElapsed(nodeRun.started_at, nodeRun.finished_at, nodeRun.status),
    attemptCount: nodeRun.attempt_count,
  };
}

export function formatElapsed(
  startedAt: string,
  finishedAt: string | null | undefined,
  status?: string,
): string | null {
  const start = Date.parse(startedAt);
  if (!Number.isFinite(start)) return null;
  const end = finishedAt ? Date.parse(finishedAt) : Date.now();
  if (!Number.isFinite(end)) return null;
  const ms = Math.max(0, end - start);
  if (status === "skipped" && ms < 100) return `${ms}ms`;
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`;
}

export function graphProgressPhaseLabelKey(phase: string | null | undefined): TranslationKey | null {
  switch (phase) {
    case "claimed":
      return "graph.phase.claimed";
    case "prepared":
      return "graph.phase.prepared";
    case "provider_call":
      return "graph.phase.provider_call";
    case "provider_result_received":
      return "graph.phase.provider_result_received";
    case "requeued_after_idle":
      return "graph.phase.requeued_after_idle";
    default:
      return null;
  }
}

export function graphRunScopeLabelKey(scope: GraphRunScope): TranslationKey {
  if (scope === "node") return "graph.runs.scope.node";
  if (scope === "to_node") return "graph.runs.scope.toNode";
  if (scope === "selection") return "graph.runs.scope.selection";
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

export interface GraphIncomingSourceEntry {
  id: string;
  sourceNodeId: string | null;
  title: string;
  role: string;
  order: number;
  artifactId: string | null;
  artifactType: GraphRunInputTraceEntry["artifact_type"];
  assetId: string | null;
  versionId: string | null;
}

export function graphIncomingSourceEntries(
  node: { incoming: Array<{ id: string; node_id: string; role: string; order: number }> },
  graph: GraphProjection,
): GraphIncomingSourceEntry[] {
  return node.incoming
    .slice()
    .sort((left, right) => left.order - right.order || left.id.localeCompare(right.id))
    .map((edge) => {
      const source = graph.nodes.find((item) => item.id === edge.node_id);
      const title = source?.title?.trim() ?? "";
      return {
        id: edge.id,
        sourceNodeId: edge.node_id,
        title: title || (source ? "—" : ""),
        role: edge.role,
        order: edge.order,
        artifactId: source?.current_artifact_id ?? null,
        artifactType: source?.current_artifact_type ?? null,
        assetId: source?.bound_asset_id ?? source?.preview_asset_id ?? null,
        versionId: source ? currentSourceVersionId(source) : null,
      };
    });
}

export function graphRunInputTraceEntries(nodeRun: GraphNodeRun): GraphIncomingSourceEntry[] {
  const context = nodeRun.compiled_context;
  const promptEdgeId = contextString(context, "prompt_edge_id");
  const promptArtifactId = contextString(context, "prompt_artifact_id");
  return (nodeRun.input_trace ?? [])
    .slice()
    .sort((left, right) => left.order - right.order || left.edge_id.localeCompare(right.edge_id))
    .map((item) => {
      const promptArtifact = item.edge_id === promptEdgeId ? promptArtifactId : null;
      return {
        id: item.edge_id,
        sourceNodeId: item.source_node_id,
        title: item.source_title?.trim() ?? "",
        role: item.role,
        order: item.order,
        artifactId: item.artifact_id ?? promptArtifact,
        artifactType: item.artifact_type ?? (promptArtifact ? "prompt" : null),
        assetId: item.asset_id ?? null,
        versionId: item.version_id ?? null,
      };
    });
}

function contextString(context: Record<string, unknown> | null, key: string): string | null {
  const value = context?.[key];
  return typeof value === "string" && value ? value : null;
}

function currentSourceVersionId(source: { config: Record<string, unknown> }): string | null {
  for (const key of ["fact_set_version_id", "visual_system_version_id"]) {
    const value = source.config[key];
    if (typeof value === "string" && value) return value;
  }
  return null;
}

export function graphContextEntries(value: Record<string, unknown> | null): Array<{
  key: string;
  labelKey: TranslationKey;
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
