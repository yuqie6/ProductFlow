import type {
  ProductWorkflow,
  ProductWorkflowStatus,
  WorkflowNode,
  WorkflowNodeRun,
  WorkflowRun,
} from "../../lib/types";

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

export function readStoredNumber(key: string, fallback: number): number {
  if (typeof window === "undefined") {
    return fallback;
  }
  const raw = window.localStorage.getItem(key);
  if (!raw) {
    return fallback;
  }
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : fallback;
}

export function outputStringArray(node: WorkflowNode, key: string): string[] {
  const value = node.output_json?.[key] ?? node.config_json[key];
  if (typeof value === "string") {
    return [value];
  }
  if (Array.isArray(value)) {
    return value.filter((item): item is string => typeof item === "string");
  }
  return [];
}

export function configString(
  node: WorkflowNode | null,
  key: string,
  fallback = "",
): string {
  const value = node?.config_json[key];
  return typeof value === "string" ? value : fallback;
}

export function statusClass(status: WorkflowNode["status"]): string {
  return {
    idle: "border-zinc-200 bg-white text-zinc-500",
    queued: "border-amber-200 bg-amber-50 text-amber-700",
    running: "border-blue-200 bg-blue-50 text-blue-700",
    succeeded: "border-emerald-200 bg-emerald-50 text-emerald-700",
    failed: "border-red-200 bg-red-50 text-red-700",
  }[status];
}

export function isImageWorkflowNodeWaiting(node: WorkflowNode): boolean {
  return (
    (node.node_type === "image_generation" || node.node_type === "reference_image") &&
    (node.status === "queued" || node.status === "running")
  );
}

export function imageWorkflowNodeWaitingLabel(node: WorkflowNode): string {
  if (!isImageWorkflowNodeWaiting(node)) {
    return "";
  }
  if (node.node_type === "reference_image") {
    return node.status === "queued" ? "参考图排队更新" : "参考图更新中";
  }
  return node.status === "queued" ? "生图排队中" : "生图生成中";
}

export function hasActiveWorkflow(workflow: ProductWorkflow | undefined | null): boolean {
  if (!workflow) {
    return false;
  }
  return (
    workflow.runs.some((run) => run.status === "running") ||
    workflow.nodes.some((node) => node.status === "queued" || node.status === "running")
  );
}

export function isProductWorkflowStatusActive(status: ProductWorkflowStatus | undefined | null): boolean {
  if (!status) {
    return false;
  }
  return (
    status.has_active_workflow ||
    status.runs.some((run) => run.status === "running") ||
    status.nodes.some((node) => node.status === "queued" || node.status === "running")
  );
}

export function mergeProductWorkflowStatusIntoDetail(
  workflow: ProductWorkflow,
  status: ProductWorkflowStatus,
): ProductWorkflow {
  if (workflow.id !== status.id) {
    return workflow;
  }
  const nodeStatusById = new Map(status.nodes.map((node) => [node.id, node]));
  const existingRunById = new Map(workflow.runs.map((run) => [run.id, run]));
  const statusRuns = status.runs.map((run): WorkflowRun => {
    const existingRun = existingRunById.get(run.id);
    const existingNodeRunById = new Map((existingRun?.node_runs ?? []).map((nodeRun) => [nodeRun.id, nodeRun]));
    return {
      id: run.id,
      workflow_id: run.workflow_id,
      status: run.status,
      started_at: run.started_at,
      finished_at: run.finished_at,
      failure_reason: run.failure_reason,
      node_runs: run.node_runs.map((nodeRun): WorkflowNodeRun => {
        const existingNodeRun = existingNodeRunById.get(nodeRun.id);
        return {
          id: nodeRun.id,
          workflow_run_id: nodeRun.workflow_run_id,
          node_id: nodeRun.node_id,
          status: nodeRun.status,
          failure_reason: nodeRun.failure_reason,
          started_at: nodeRun.started_at,
          finished_at: nodeRun.finished_at,
          output_json: existingNodeRun?.output_json ?? null,
          copy_set_id: existingNodeRun?.copy_set_id ?? null,
          poster_variant_id: existingNodeRun?.poster_variant_id ?? null,
          image_session_asset_id: existingNodeRun?.image_session_asset_id ?? null,
        };
      }),
    };
  });
  const statusRunIds = new Set(statusRuns.map((run) => run.id));
  return {
    ...workflow,
    title: status.title,
    active: status.active,
    updated_at: status.updated_at,
    nodes: workflow.nodes.map((node) => {
      const nodeStatus = nodeStatusById.get(node.id);
      if (!nodeStatus) {
        return node;
      }
      return {
        ...node,
        status: nodeStatus.status,
        failure_reason: nodeStatus.failure_reason,
        last_run_at: nodeStatus.last_run_at,
        updated_at: nodeStatus.updated_at,
      };
    }),
    runs: [...statusRuns, ...workflow.runs.filter((run) => !statusRunIds.has(run.id))],
  };
}

export function shouldRefreshProductWorkflowDetailFromStatus(
  workflow: ProductWorkflow | undefined | null,
  status: ProductWorkflowStatus,
): boolean {
  if (!workflow || workflow.id !== status.id) {
    return false;
  }
  if (hasActiveWorkflow(workflow) && !isProductWorkflowStatusActive(status)) {
    return true;
  }
  const latestStatusRun = status.runs[0];
  if (!latestStatusRun || latestStatusRun.status === "running") {
    return false;
  }
  const currentRun = workflow.runs.find((run) => run.id === latestStatusRun.id);
  return !currentRun || currentRun.status !== latestStatusRun.status || currentRun.finished_at !== latestStatusRun.finished_at;
}

export function outputText(
  output: Record<string, unknown>,
  key: string,
): string | null {
  const value = output[key];
  return typeof value === "string" && value.trim() ? value.trim() : null;
}
