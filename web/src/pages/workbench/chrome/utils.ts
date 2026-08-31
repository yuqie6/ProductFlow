import type { WorkflowNodeDisplayStatus } from "../../../lib/types";
import { statusBadgeClass } from "../../../components/ui/status-badge";

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

export function statusClass(status: WorkflowNodeDisplayStatus): string {
  return statusBadgeClass(status);
}
