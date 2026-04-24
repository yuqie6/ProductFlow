import { LayoutGrid, LogOut } from "lucide-react";

interface TopNavProps {
  breadcrumbs?: string;
  onHome?: () => void;
  onLogout?: () => void;
}

export function TopNav({ breadcrumbs, onHome, onLogout }: TopNavProps) {
  return (
    <nav className="z-50 flex h-14 items-center justify-between border-b border-zinc-200 bg-white px-6">
      <div className="flex items-center space-x-2 text-sm">
        <button
          type="button"
          className="flex items-center font-semibold text-zinc-900 transition-colors hover:text-zinc-600"
          onClick={onHome}
        >
          <LayoutGrid size={16} className="mr-2" />
          ProductFlow
        </button>
        {breadcrumbs ? (
          <>
            <span className="text-zinc-300">/</span>
            <span className="font-medium text-zinc-600">{breadcrumbs}</span>
          </>
        ) : null}
      </div>
      {onLogout ? (
        <button
          type="button"
          onClick={onLogout}
          className="flex items-center text-xs font-medium text-zinc-500 transition-colors hover:text-zinc-900"
        >
          <LogOut size={14} className="mr-1.5" /> 退出登录
        </button>
      ) : null}
    </nav>
  );
}
