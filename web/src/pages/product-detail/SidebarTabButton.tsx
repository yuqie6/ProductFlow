import type { ReactNode } from "react";

interface SidebarTabButtonProps {
  active: boolean;
  label: string;
  title: string;
  icon: ReactNode;
  onClick: () => void;
}

export function SidebarTabButton({
  active,
  label,
  title,
  icon,
  onClick,
}: SidebarTabButtonProps) {
  return (
    <button
      type="button"
      aria-pressed={active}
      title={title}
      onClick={onClick}
      className={`flex w-full flex-col items-center rounded-lg px-1 py-2 text-[10px] font-medium transition-colors ${
        active
          ? "bg-white text-slate-950 shadow-sm"
          : "text-slate-400 hover:bg-white/10 hover:text-white"
      }`}
    >
      {icon}
      <span className="mt-1 leading-none">{label}</span>
    </button>
  );
}
