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
      className={`flex w-full flex-col items-center rounded-lg px-1.5 py-2 text-xs font-medium transition-colors ${
        active
          ? "bg-white text-slate-950 shadow-sm dark:border dark:border-violet-400/35 dark:bg-violet-500/18 dark:text-violet-100 dark:shadow-violet-950/20"
          : "text-slate-400 hover:bg-white/10 hover:text-white dark:text-slate-400 dark:hover:bg-violet-500/12 dark:hover:text-slate-100"
      }`}
    >
      {icon}
      <span className="mt-1 leading-tight">{label}</span>
    </button>
  );
}
