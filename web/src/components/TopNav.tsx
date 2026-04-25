import { LayoutGrid, LogOut, MessagesSquare, Settings } from "lucide-react";
import { Link, useLocation } from "react-router-dom";

import { OnboardingNavButton } from "./OnboardingGuide";

interface TopNavProps {
  breadcrumbs?: string;
  onHome?: () => void;
  onLogout?: () => void;
}

const navItems = [
  {
    label: "商品/工作台",
    to: "/products",
    icon: LayoutGrid,
    match: (pathname: string) => pathname.startsWith("/products") && !pathname.endsWith("/image-chat"),
  },
  {
    label: "连续生图",
    to: "/image-chat",
    icon: MessagesSquare,
    match: (pathname: string) => pathname.includes("image-chat"),
  },
  {
    label: "配置",
    to: "/settings",
    icon: Settings,
    match: (pathname: string) => pathname.startsWith("/settings"),
  },
];

function navItemClassName(active: boolean) {
  return [
    "inline-flex shrink-0 items-center rounded-xl px-3.5 py-2.5 text-sm font-semibold transition-colors sm:px-4 lg:px-5 lg:py-3 lg:text-[15px]",
    active ? "bg-zinc-900 text-white shadow-sm" : "text-zinc-500 hover:bg-white hover:text-zinc-900",
  ].join(" ");
}

export function TopNav({ breadcrumbs, onHome, onLogout }: TopNavProps) {
  const location = useLocation();

  return (
    <nav className="z-50 flex flex-col gap-3 border-b border-zinc-200 bg-white px-4 py-3 lg:grid lg:min-h-[80px] lg:grid-cols-[minmax(180px,1fr)_auto_minmax(180px,1fr)] lg:items-center lg:gap-4 lg:px-6">
      <div className="flex min-w-0 items-center space-x-2 text-sm">
        <button
          type="button"
          className="flex shrink-0 items-center text-base font-semibold text-zinc-900 transition-colors hover:text-zinc-600"
          onClick={onHome}
        >
          <LayoutGrid size={18} className="mr-2" />
          ProductFlow
        </button>
        {breadcrumbs ? (
          <>
            <span className="text-zinc-300">/</span>
            <span className="truncate font-medium text-zinc-600">{breadcrumbs}</span>
          </>
        ) : null}
      </div>

      <div className="flex min-w-0 justify-start overflow-x-auto lg:justify-center">
        <div className="flex min-w-max items-center gap-1.5 rounded-2xl border border-zinc-200 bg-zinc-50/80 p-1.5 shadow-sm">
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = item.match(location.pathname);
            return (
              <Link
                key={item.to}
                to={item.to}
                aria-current={active ? "page" : undefined}
                className={navItemClassName(active)}
              >
                <Icon size={16} className="mr-2" />
                <span>{item.label}</span>
              </Link>
            );
          })}
        </div>
      </div>

      <div className="flex min-w-0 flex-wrap items-center justify-start gap-2 lg:justify-end">
        <OnboardingNavButton />
        {onLogout ? (
          <button
            type="button"
            onClick={onLogout}
            className="flex items-center rounded-lg px-3 py-2 text-sm font-medium text-zinc-500 transition-colors hover:bg-zinc-100 hover:text-zinc-900"
          >
            <LogOut size={15} className="mr-1.5" /> 退出登录
          </button>
        ) : null}
      </div>
    </nav>
  );
}
