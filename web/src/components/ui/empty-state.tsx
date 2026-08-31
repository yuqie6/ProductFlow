import type { ReactNode } from "react";

import { cn } from "./cn";
import { Button } from "./button";

export function EmptyState({
  icon,
  text,
  action,
  onAction,
  compact = false,
  className,
}: {
  icon?: ReactNode;
  text: string;
  action?: string;
  onAction?: () => void;
  compact?: boolean;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-2 px-6 text-center text-xs text-text-muted",
        compact ? "min-h-28" : "min-h-[220px]",
        className,
      )}
    >
      {icon ? <span className="text-text-muted">{icon}</span> : null}
      <span className="max-w-[260px] leading-5">{text}</span>
      {action && onAction ? (
        <Button variant="ghost" size="sm" onClick={onAction} className="mt-1 text-accent hover:text-accent-strong">
          {action}
        </Button>
      ) : null}
    </div>
  );
}

export { EmptyState as PanelState };
