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
      className={`flex w-full flex-col items-center rounded-xl px-1 py-2 text-[10px] font-medium transition-all transition-spring ${
        active
          ? "bg-white text-indigo-600 shadow-[0_2px_8px_rgba(99,102,241,0.15)] ring-1 ring-indigo-500/30 scale-[1.05] dark:bg-slate-800 dark:text-slate-100 dark:ring-1 dark:ring-indigo-500/50 dark:shadow-[0_4px_12px_rgba(0,0,0,0.3)]"
          : "text-slate-500 hover:scale-[1.05] hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800/60 dark:hover:text-white"
      }`}
    >
      <span className={`transition-transform duration-300 ${active ? "scale-110" : ""}`}>{icon}</span>
      <span className="mt-1 leading-tight">{label}</span>
    </button>
  );
}
