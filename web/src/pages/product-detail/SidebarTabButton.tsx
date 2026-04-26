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
          ? "bg-zinc-900 text-white shadow-sm"
          : "text-zinc-500 hover:bg-white hover:text-zinc-900"
      }`}
    >
      {icon}
      <span className="mt-1 leading-none">{label}</span>
    </button>
  );
}
