import {
  BookOpen,
  GalleryHorizontalEnd,
  Languages,
  LayoutGrid,
  LogOut,
  MessagesSquare,
  Monitor,
  Moon,
  Settings,
  Sun,
  Wand2,
} from "lucide-react";
import { Link, useLocation } from "react-router-dom";

import { LOCALES, type Locale, type TranslationKey } from "../lib/i18n";
import { usePreferences } from "../lib/preferences";
import { THEME_PREFERENCES, type ThemePreference } from "../lib/theme";

interface TopNavProps {
  breadcrumbs?: string;
  onHome?: () => void;
  onLogout?: () => void;
}

const navItems = [
  {
    labelKey: "nav.products",
    to: "/products",
    icon: LayoutGrid,
    match: (pathname: string) => pathname.startsWith("/products") && !pathname.endsWith("/image-chat"),
  },
  {
    labelKey: "nav.imageChat",
    to: "/image-chat",
    icon: MessagesSquare,
    match: (pathname: string) => pathname.includes("image-chat"),
  },
  {
    labelKey: "nav.gallery",
    to: "/gallery",
    icon: GalleryHorizontalEnd,
    match: (pathname: string) => pathname.startsWith("/gallery"),
  },
  {
    labelKey: "nav.help",
    to: "/help",
    icon: BookOpen,
    match: (pathname: string) => pathname.startsWith("/help"),
  },
  {
    labelKey: "nav.settings",
    to: "/settings",
    icon: Settings,
    match: (pathname: string) => pathname.startsWith("/settings"),
  },
] as const;

const themeIcons: Record<ThemePreference, typeof Sun> = {
  light: Sun,
  dark: Moon,
  system: Monitor,
};

const localeLabelKey: Record<Locale, TranslationKey> = {
  "zh-CN": "locale.zhCN",
  "en-US": "locale.enUS",
  "ja-JP": "locale.jaJP",
};

function navItemClassName(active: boolean) {
  return [
    "inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-sm font-semibold transition-colors sm:w-auto sm:px-4 lg:px-5",
    active
      ? "bg-white text-indigo-700 shadow-sm ring-1 ring-indigo-100 dark:bg-slate-800 dark:text-indigo-300 dark:ring-slate-700"
      : "text-slate-500 hover:bg-white/70 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800/80 dark:hover:text-slate-100",
  ].join(" ");
}

export function TopNav({ breadcrumbs, onHome, onLogout }: TopNavProps) {
  const location = useLocation();
  const { locale, setLocale, t, themePreference, setThemePreference } = usePreferences();
  const CurrentThemeIcon = themeIcons[themePreference];
  const nextThemePreference =
    THEME_PREFERENCES[(THEME_PREFERENCES.indexOf(themePreference) + 1) % THEME_PREFERENCES.length];
  const nextLocale = LOCALES[(LOCALES.indexOf(locale) + 1) % LOCALES.length];

  return (
    <>
      <nav className="z-50 flex flex-col gap-3 overflow-x-hidden border-b border-slate-200 bg-white/95 px-3 py-3 shadow-[0_1px_0_rgba(15,23,42,0.03)] backdrop-blur dark:border-slate-800 dark:bg-slate-950/92 sm:px-4 lg:grid lg:min-h-14 lg:grid-cols-[minmax(180px,1fr)_auto_minmax(180px,1fr)] lg:items-center lg:gap-4 lg:px-6">
        <div className="flex min-w-0 items-center justify-between gap-2 text-sm">
          <div className="flex min-w-0 max-w-[calc(100%-6.5rem)] items-center space-x-2 overflow-hidden">
            <button
              type="button"
              className="flex min-w-0 shrink-0 items-center text-base font-semibold text-slate-950 transition-colors hover:text-indigo-700 dark:text-slate-100 dark:hover:text-indigo-300"
              onClick={onHome}
            >
              <span className="mr-2 inline-flex h-8 w-8 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-sm shadow-indigo-600/20">
                <Wand2 size={17} />
              </span>
              <span className="hidden sm:inline">ProductFlow</span>
            </button>
            {breadcrumbs ? (
              <>
                <span className="text-slate-300 dark:text-slate-700">/</span>
                <span className="truncate font-medium text-slate-600 dark:text-slate-400">{breadcrumbs}</span>
              </>
            ) : null}
          </div>
          <div className="flex shrink-0 items-center gap-1 lg:hidden">
            <button
              type="button"
              onClick={() => setLocale(nextLocale)}
              aria-label={`${t("nav.language")}: ${t(localeLabelKey[locale])}`}
              title={t(localeLabelKey[locale])}
              className="inline-flex h-11 w-11 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-600 shadow-sm transition-colors active:scale-[0.98] hover:border-indigo-200 hover:text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:border-slate-700 dark:bg-slate-950/80 dark:text-slate-300 dark:hover:border-violet-400/55 dark:hover:text-violet-100"
            >
              <Languages size={16} />
            </button>
            <button
              type="button"
              onClick={() => setThemePreference(nextThemePreference)}
              aria-label={`${t("nav.theme")}: ${t(`theme.${themePreference}`)}`}
              title={t(`theme.${themePreference}`)}
              className="inline-flex h-11 w-11 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-600 shadow-sm transition-colors active:scale-[0.98] hover:border-indigo-200 hover:text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:border-slate-700 dark:bg-slate-950/80 dark:text-slate-300 dark:hover:border-violet-400/55 dark:hover:text-violet-100"
            >
              <CurrentThemeIcon size={16} />
            </button>
          </div>
        </div>

        <div className="hidden min-w-0 justify-start overflow-x-auto lg:flex lg:justify-center">
          <div className="flex min-w-max items-center gap-1 rounded-xl border border-slate-200 bg-slate-100/80 p-1 shadow-inner shadow-slate-200/40 dark:border-slate-800 dark:bg-slate-900/80 dark:shadow-none">
            {navItems.map((item) => {
              const Icon = item.icon;
              const active = item.match(location.pathname);
              const label = t(item.labelKey);
              return (
                <Link
                  key={item.to}
                  to={item.to}
                  aria-current={active ? "page" : undefined}
                  aria-label={label}
                  className={navItemClassName(active)}
                  title={label}
                >
                  <Icon size={16} className="sm:mr-2" />
                  <span className="hidden sm:inline">{label}</span>
                </Link>
              );
            })}
          </div>
        </div>

        <div className="hidden min-w-0 flex-wrap items-center justify-start gap-2 lg:flex lg:justify-end">
          <div className="inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-slate-50 p-1 dark:border-slate-800 dark:bg-slate-900">
            <Languages size={14} className="ml-1 text-slate-400" aria-hidden="true" />
            {LOCALES.map((item) => (
              <button
                key={item}
                type="button"
                onClick={() => setLocale(item)}
                aria-label={`${t("nav.language")}: ${t(localeLabelKey[item])}`}
                className={`h-7 rounded-md px-2 text-xs font-semibold transition-colors ${
                  locale === item
                    ? "bg-white text-indigo-700 shadow-sm dark:bg-slate-800 dark:text-indigo-300"
                    : "text-slate-500 hover:text-slate-950 dark:text-slate-400 dark:hover:text-slate-100"
                }`}
              >
                {t(localeLabelKey[item])}
              </button>
            ))}
          </div>
          <div className="inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-slate-50 p-1 dark:border-slate-800 dark:bg-slate-900">
            {THEME_PREFERENCES.map((item) => {
              const Icon = themeIcons[item];
              return (
                <button
                  key={item}
                  type="button"
                  onClick={() => setThemePreference(item)}
                  aria-label={`${t("nav.theme")}: ${t(`theme.${item}`)}`}
                  title={t(`theme.${item}`)}
                  className={`inline-flex h-7 w-7 items-center justify-center rounded-md transition-colors ${
                    themePreference === item
                      ? "bg-white text-indigo-700 shadow-sm dark:bg-slate-800 dark:text-indigo-300"
                      : "text-slate-500 hover:text-slate-950 dark:text-slate-400 dark:hover:text-slate-100"
                  }`}
                >
                  <Icon size={14} />
                </button>
              );
            })}
          </div>
          {onLogout ? (
            <button
              type="button"
              onClick={onLogout}
              aria-label={t("nav.logout")}
              title={t("nav.logout")}
              className="flex items-center rounded-lg px-3 py-2 text-sm font-medium text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100"
            >
              <LogOut size={15} className="sm:mr-1.5" /> <span className="hidden sm:inline">{t("nav.logout")}</span>
            </button>
          ) : null}
        </div>
      </nav>

      <div
        aria-label={t("nav.mobile")}
        className="fixed inset-x-0 bottom-0 z-50 border-t border-slate-200 bg-white/96 px-2 pt-1.5 pb-[calc(env(safe-area-inset-bottom)+0.4rem)] shadow-[0_-10px_30px_rgba(15,23,42,0.12)] backdrop-blur dark:border-slate-800 dark:bg-slate-950/94 dark:shadow-[0_-18px_40px_rgba(0,0,0,0.35)] lg:hidden"
      >
        <div className="mx-auto grid w-full max-w-md grid-cols-5 gap-1">
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = item.match(location.pathname);
            const label = t(item.labelKey);
            return (
              <Link
                key={item.to}
                to={item.to}
                aria-current={active ? "page" : undefined}
                aria-label={label}
                className={`flex min-h-12 min-w-0 flex-col items-center justify-center rounded-xl px-0.5 text-[10px] font-semibold transition-colors active:scale-[0.98] focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-violet-400 ${
                  active
                    ? "bg-indigo-50 text-indigo-700 dark:bg-violet-500/18 dark:text-violet-100"
                    : "text-slate-500 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-900 dark:hover:text-slate-100"
                }`}
              >
                <Icon size={18} aria-hidden="true" />
                <span className="mt-0.5 truncate">{label}</span>
              </Link>
            );
          })}
        </div>
      </div>
    </>
  );
}
