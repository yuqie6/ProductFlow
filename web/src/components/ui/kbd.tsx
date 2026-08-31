import type { ReactNode } from "react";

import { cn } from "./cn";

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-flex h-5 min-w-5 items-center justify-center rounded-[5px] border border-border-l1 bg-surface-subtle px-1 font-sans text-[10px] font-semibold text-text-muted",
        className,
      )}
    >
      {children}
    </kbd>
  );
}
