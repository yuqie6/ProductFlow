import { GalleryHorizontalEnd, LayoutGrid, LogOut, MessagesSquare, Settings, Wand2 } from "lucide-react";
import { Link, useLocation } from "react-router-dom";

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
    label: "文/图生图",
    to: "/image-chat",
    icon: MessagesSquare,
    match: (pathname: string) => pathname.includes("image-chat"),
  },
  {
    label: "画廊",
    to: "/gallery",
    icon: GalleryHorizontalEnd,
    match: (pathname: string) => pathname.startsWith("/gallery"),
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
    "inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-sm font-semibold transition-colors sm:w-auto sm:px-4 lg:px-5",
    active
      ? "bg-white text-indigo-700 shadow-sm ring-1 ring-indigo-100"
      : "text-slate-500 hover:bg-white/70 hover:text-slate-900",
  ].join(" ");
}

export function TopNav({ breadcrumbs, onHome, onLogout }: TopNavProps) {
  const location = useLocation();

  return (
    <nav className="z-50 flex flex-col gap-3 border-b border-slate-200 bg-white/95 px-3 py-3 shadow-[0_1px_0_rgba(15,23,42,0.03)] backdrop-blur sm:px-4 lg:grid lg:min-h-14 lg:grid-cols-[minmax(180px,1fr)_auto_minmax(180px,1fr)] lg:items-center lg:gap-4 lg:px-6">
      <div className="flex min-w-0 items-center space-x-2 text-sm">
        <button
          type="button"
          className="flex min-w-0 shrink-0 items-center text-base font-semibold text-slate-950 transition-colors hover:text-indigo-700"
          onClick={onHome}
        >
          <span className="mr-2 inline-flex h-8 w-8 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-sm shadow-indigo-600/20">
            <Wand2 size={17} />
          </span>
          ProductFlow
        </button>
        {breadcrumbs ? (
          <>
            <span className="text-slate-300">/</span>
            <span className="truncate font-medium text-slate-600">{breadcrumbs}</span>
          </>
        ) : null}
      </div>

      <div className="flex min-w-0 justify-start overflow-x-auto lg:justify-center">
        <div className="flex min-w-max items-center gap-1 rounded-xl border border-slate-200 bg-slate-100/80 p-1 shadow-inner shadow-slate-200/40">
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = item.match(location.pathname);
            return (
              <Link
                key={item.to}
                to={item.to}
                aria-current={active ? "page" : undefined}
                aria-label={item.label}
                className={navItemClassName(active)}
                title={item.label}
              >
                <Icon size={16} className="sm:mr-2" />
                <span className="hidden sm:inline">{item.label}</span>
              </Link>
            );
          })}
        </div>
      </div>

      <div className="flex min-w-0 flex-wrap items-center justify-start gap-2 lg:justify-end">
        {onLogout ? (
          <button
            type="button"
            onClick={onLogout}
            aria-label="退出登录"
            title="退出登录"
            className="flex items-center rounded-lg px-3 py-2 text-sm font-medium text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900"
          >
            <LogOut size={15} className="sm:mr-1.5" /> <span className="hidden sm:inline">退出登录</span>
          </button>
        ) : null}
      </div>
    </nav>
  );
}
