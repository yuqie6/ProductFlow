import type { WorkflowNodeStatus } from "../../../lib/types";

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

export function statusClass(status: WorkflowNodeStatus): string {
  return {
    idle: "border-zinc-200 bg-white text-zinc-500 dark:border-slate-600 dark:bg-[#0b1220] dark:text-slate-200",
    queued: "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-400/40 dark:bg-amber-500/15 dark:text-amber-100",
    running: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-400/40 dark:bg-blue-500/15 dark:text-blue-100",
    succeeded: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-300/50 dark:bg-emerald-400/15 dark:text-emerald-50",
    failed: "border-red-200 bg-red-50 text-red-700 dark:border-red-400/40 dark:bg-red-500/15 dark:text-red-100",
    cancelled: "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300",
    unknown: "border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-400/40 dark:bg-orange-500/15 dark:text-orange-100",
  }[status];
}
