import { Tooltip } from "../../../components/ui/tooltip";
import type { ReactNode } from "react";

interface SidebarTabButtonProps {
  toolId: string;
  active: boolean;
  label: string;
  title: string;
  icon: ReactNode;
  onClick: () => void;
}

export function SidebarTabButton({
  toolId,
  active,
  label,
  title,
  icon,
  onClick,
}: SidebarTabButtonProps) {
  return (
    <Tooltip content={title} side="left">
      <button
        type="button"
        data-sidebar-tool={toolId}
        aria-pressed={active}
        aria-label={title}
        onClick={onClick}
        className={`flex w-full flex-col items-center rounded-panel px-1 py-2 text-[10px] font-medium outline-none transition-[background-color,color,transform] duration-fast focus-visible:ring-2 focus-visible:ring-focus-ring motion-reduce:transition-none min-h-11 ${active
            ? "bg-surface-raised text-accent shadow-elev-1 ring-1 ring-accent/30"
            : "text-text-muted hover:bg-surface-subtle hover:text-text-primary"
          }`}
      >
        <span>{icon}</span>
        <span className="mt-1 leading-tight">{label}</span>
      </button>
    </Tooltip>
  );
}
