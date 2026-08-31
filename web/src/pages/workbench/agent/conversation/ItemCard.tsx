import type { ReactNode } from "react";

interface ItemCardProps {
  children: ReactNode;
  className?: string;
  emphasized?: boolean;
}

export function ItemCard({ children, className = "", emphasized = false }: ItemCardProps) {
  return (
    <div
      data-agent-item-card
      className={[
        "overflow-hidden rounded-lg border",
        emphasized ? "border-accent/40 bg-accent-soft" : "border-border-l1 bg-surface-raised",
        className,
      ].filter(Boolean).join(" ")}
    >
      {children}
    </div>
  );
}
