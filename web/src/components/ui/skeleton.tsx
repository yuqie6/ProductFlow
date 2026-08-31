import { cn } from "./cn";

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-shimmer", className)} aria-hidden="true" />;
}

export function PanelSkeleton({
  rows = 4,
  label,
  compact = false,
  className,
}: {
  rows?: number;
  label?: string;
  compact?: boolean;
  className?: string;
}) {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={label}
      className={cn("space-y-3 p-panel", compact && "space-y-2 p-2", className)}
    >
      {Array.from({ length: rows }, (_, index) => (
        <div key={index} className="flex gap-3">
          <Skeleton className={cn("shrink-0", compact ? "h-9 w-9" : "h-12 w-12")} />
          <div className="min-w-0 flex-1 space-y-2 py-1">
            <Skeleton className="h-3 w-3/4" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        </div>
      ))}
    </div>
  );
}
