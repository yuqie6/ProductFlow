import type { ProductWorkflow, WorkflowNode } from "../../lib/types";

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

export function nodeIdleSummary(node: WorkflowNode): string {
  if (node.node_type === "product_context") {
    return "商品资料";
  }
  if (node.node_type === "reference_image") {
    return "参考图";
  }
  if (node.node_type === "copy_generation") {
    return "文案";
  }
  return "可直接生成图片";
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

export function outputCount(output: Record<string, unknown>, key: string): number {
  const value = output[key];
  if (Array.isArray(value)) {
    return value.filter((item) => typeof item === "string" && item.length > 0).length;
  }
  return typeof value === "string" && value.length > 0 ? 1 : 0;
}

export function outputText(
  output: Record<string, unknown>,
  key: string,
): string | null {
  const value = output[key];
  return typeof value === "string" && value.trim() ? value.trim() : null;
}
