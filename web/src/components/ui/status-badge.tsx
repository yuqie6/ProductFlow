import { Loader2 } from "lucide-react";
import type { ReactNode } from "react";

import type { WorkflowNodeDisplayStatus } from "../../lib/types";
import { cn } from "./cn";

export type StatusBadgeStatus = WorkflowNodeDisplayStatus | "frozen";

export const STATUS_BADGE_CLASS_NAMES: Record<StatusBadgeStatus, string> = {
  idle: "border-border-l1 bg-surface-subtle text-text-muted",
  queued: "border-state-warning/35 bg-state-warning-soft text-state-warning",
  running: "border-accent/35 bg-accent-soft text-accent",
  succeeded: "border-state-success/35 bg-state-success-soft text-state-success",
  failed: "border-state-error/35 bg-state-error-soft text-state-error",
  cancelled: "border-border-l1 bg-surface-subtle text-text-muted",
  skipped: "border-border-l1 bg-surface-subtle text-text-muted",
  unknown: "border-border-l1 bg-surface-subtle text-text-muted",
  frozen: "border-state-frozen/35 bg-state-frozen-soft text-state-frozen",
};

export function statusBadgeClass(status: StatusBadgeStatus): string {
  return STATUS_BADGE_CLASS_NAMES[status];
}

export function StatusBadge({
  status,
  children,
  className,
  spinning = false,
}: {
  status: StatusBadgeStatus;
  children: ReactNode;
  className?: string;
  spinning?: boolean;
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-semibold",
        statusBadgeClass(status),
        className,
      )}
    >
      {spinning || status === "running" || status === "queued" ? (
        <Loader2 size={10} className="animate-spin motion-reduce:animate-none" aria-hidden="true" />
      ) : null}
      {children}
    </span>
  );
}
