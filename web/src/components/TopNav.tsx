import { useQuery } from "@tanstack/react-query";
import {
  BookOpen,
  Check,
  ChevronDown,
  Images,
  House,
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
import type { FocusEvent, MouseEvent } from "react";
import { Link, useLocation } from "react-router-dom";

import { api } from "../lib/api";
import { LOCALES, LOCALE_LABEL_KEYS, type Locale } from "../lib/i18n";
import { canAccessOpsSettings } from "../lib/opsAccess";
import { usePreferences } from "../lib/preferences";
import { THEME_PREFERENCES, type ThemePreference } from "../lib/theme";

interface TopNavProps {
  breadcrumbs?: string;
  onHome?: () => void;
  onLogout?: () => void;
}

const navItems = [
  {
    labelKey: "nav.home",
    to: "/home",
    icon: House,
    match: (pathname: string) => pathname === "/home",
  },
  {
    labelKey: "nav.products",
    to: "/products",
    icon: LayoutGrid,
    match: (pathname: string) => pathname.startsWith("/products"),
  },
  {
    labelKey: "nav.imageChat",
    to: "/image-chat",
    icon: MessagesSquare,
    match: (pathname: string) => pathname.includes("image-chat"),
  },
  {
    labelKey: "nav.mediaLibrary",
    to: "/media-library",
    icon: Images,
    match: (pathname: string) => pathname.startsWith("/media-library"),
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

function navItemClassName(active: boolean) {
  return [
    "inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-sm font-semibold transition-colors sm:w-auto sm:px-4 lg:w-10 lg:px-0 xl:w-auto xl:px-3 2xl:px-4",
    active
      ? "bg-surface-raised text-accent shadow-sm ring-1 ring-accent dark:bg-surface-panel dark:text-accent dark:ring-border-l1"
      : "text-text-muted hover:bg-surface-raised/70 hover:text-text-primary dark:text-text-muted dark:hover:bg-surface-panel/80 dark:hover:text-text-primary",
  ].join(" ");
}

function closeDetails(event: MouseEvent<HTMLButtonElement>) {
  event.currentTarget.closest("details")?.removeAttribute("open");
}

function closeDetailsOnBlur(event: FocusEvent<HTMLElement>) {
  const nextTarget = event.relatedTarget;
  if (!nextTarget || !(nextTarget instanceof Node) || !event.currentTarget.contains(nextTarget)) {
    event.currentTarget.removeAttribute("open");
  }
}

function LanguagePicker({ compact = false }: { compact?: boolean }) {
  const { locale, setLocale, t } = usePreferences();
  const selectedLabel = t(LOCALE_LABEL_KEYS[locale]);
  const selectLocale = (event: MouseEvent<HTMLButtonElement>, nextLocale: Locale) => {
    setLocale(nextLocale);
    closeDetails(event);
  };

  return (
    <details
      onBlur={closeDetailsOnBlur}
      className={[
        "group relative inline-flex shrink-0 text-text-secondary dark:text-text-secondary",
        compact ? "w-[7.35rem]" : "w-36",
      ].join(" ")}
    >
      <summary
        aria-label={`${t("nav.language")}: ${selectedLabel}`}
        title={selectedLabel}
        className={[
          "flex cursor-pointer list-none items-center rounded-xl border border-border-l1 bg-surface-raised shadow-sm transition-colors select-none marker:hidden hover:border-accent hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent group-open:border-accent group-open:text-accent dark:border-border-l1 dark:bg-surface-base/80 dark:hover:border-accent/55 dark:hover:text-accent dark:focus-visible:ring-accent dark:group-open:border-accent/55 dark:group-open:text-accent [&::-webkit-details-marker]:hidden",
          compact ? "h-11 w-[7.35rem] px-2.5" : "h-9 w-36 px-2.5",
        ].join(" ")}
      >
        <Languages size={14} className="shrink-0 text-text-muted" aria-hidden="true" />
        <span className="ml-2 min-w-0 flex-1 truncate text-left text-xs font-semibold">{selectedLabel}</span>
        <ChevronDown
          size={13}
          className="ml-1 shrink-0 text-text-muted transition-transform group-open:rotate-180"
          aria-hidden="true"
        />
      </summary>
      <div className="absolute right-0 top-full z-[70] mt-2 w-44 overflow-hidden rounded-2xl border border-border-l1 bg-surface-raised p-1.5 shadow-xl dark:border-border-l1 dark:bg-surface-base dark:shadow-black/40">
        {LOCALES.map((item) => {
          const active = locale === item;
          return (
            <button
              key={item}
              type="button"
              onClick={(event) => selectLocale(event, item)}
              aria-current={active ? "true" : undefined}
              className={`flex w-full items-center gap-2 rounded-xl px-3 py-2 text-left text-sm font-medium transition-colors ${
                active
                  ? "bg-accent-soft text-accent dark:bg-accent/18 dark:text-accent"
                  : "text-text-secondary hover:bg-surface-subtle hover:text-text-primary dark:text-text-secondary dark:hover:bg-surface-base dark:hover:text-text-primary"
              }`}
            >
              <span className="min-w-0 flex-1 truncate">{t(LOCALE_LABEL_KEYS[item])}</span>
              {active ? <Check size={14} className="shrink-0" aria-hidden="true" /> : null}
            </button>
          );
        })}
      </div>
    </details>
  );
}

export function TopNav({ breadcrumbs, onHome, onLogout }: TopNavProps) {
  const location = useLocation();
  const { t, themePreference, setThemePreference } = usePreferences();
  const sessionQuery = useQuery({
    queryKey: ["session"],
    queryFn: api.getSessionState,
    retry: false,
  });
  const showSettings = canAccessOpsSettings(sessionQuery.data);
  const visibleNavItems = navItems.filter((item) => item.to !== "/settings" || showSettings);
  const CurrentThemeIcon = themeIcons[themePreference];
  const nextThemePreference =
    THEME_PREFERENCES[(THEME_PREFERENCES.indexOf(themePreference) + 1) % THEME_PREFERENCES.length];

  return (
    <>
      <nav className="z-50 flex flex-col gap-3 overflow-visible border-b border-border-l1 bg-surface-raised/95 px-3 py-3 shadow-[0_1px_0_rgba(15,23,42,0.03)] backdrop-blur dark:border-border-l2 dark:bg-surface-base/92 sm:px-4 lg:grid lg:min-h-14 lg:grid-cols-[minmax(180px,1fr)_auto_minmax(180px,1fr)] lg:items-center lg:gap-4 lg:px-6">
        <div className="flex min-w-0 items-center justify-between gap-2 text-sm">
          <div className="flex min-w-0 max-w-[calc(100%-10.75rem)] items-center space-x-2 overflow-hidden lg:max-w-none">
            <button
              type="button"
              className="flex min-w-0 shrink-0 items-center text-base font-semibold text-text-primary transition-colors hover:text-accent dark:text-text-primary dark:hover:text-accent"
              onClick={onHome}
            >
              <span className="mr-2 inline-flex h-8 w-8 items-center justify-center rounded-xl bg-accent text-accent-fg shadow-sm">
                <Wand2 size={17} />
              </span>
              <span className="hidden sm:inline">ProductFlow</span>
            </button>
            {breadcrumbs ? (
              <>
                <span className="text-text-muted dark:text-text-muted">/</span>
                <span className="truncate font-medium text-text-secondary dark:text-text-muted">{breadcrumbs}</span>
              </>
            ) : null}
          </div>
          <div className="flex shrink-0 items-center gap-1 lg:hidden">
            <LanguagePicker compact />
            <button
              type="button"
              onClick={() => setThemePreference(nextThemePreference)}
              aria-label={`${t("nav.theme")}: ${t(`theme.${themePreference}`)}`}
              title={t(`theme.${themePreference}`)}
              className="inline-flex h-11 w-11 items-center justify-center rounded-xl border border-border-l1 bg-surface-raised text-text-secondary shadow-sm transition-colors active:scale-[0.98] hover:border-accent hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:border-border-l1 dark:bg-surface-base/80 dark:text-text-secondary dark:hover:border-accent/55 dark:hover:text-accent"
            >
              <CurrentThemeIcon size={16} />
            </button>
          </div>
        </div>

        <div className="hidden min-w-0 justify-start overflow-x-auto lg:flex lg:justify-center">
          <div className="flex min-w-max items-center gap-1 rounded-xl border border-border-l1 bg-surface-subtle/80 p-1 shadow-inner dark:border-border-l2 dark:bg-surface-base/80 dark:shadow-none">
            {visibleNavItems.map((item) => {
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
                  <Icon size={16} className="sm:mr-2 lg:mr-0 xl:mr-2" />
                  <span className="hidden sm:inline lg:hidden xl:inline">{label}</span>
                </Link>
              );
            })}
          </div>
        </div>

        <div className="hidden min-w-0 items-center justify-end gap-2 lg:flex xl:hidden">
          <LanguagePicker compact />
          <button
            type="button"
            onClick={() => setThemePreference(nextThemePreference)}
            aria-label={`${t("nav.theme")}: ${t(`theme.${themePreference}`)}`}
            title={t(`theme.${themePreference}`)}
            className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-border-l1 bg-surface-raised text-text-secondary transition-colors hover:border-accent hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:border-border-l1 dark:bg-surface-base/80 dark:text-text-secondary dark:hover:border-accent/55 dark:hover:text-accent dark:focus-visible:ring-accent"
          >
            <CurrentThemeIcon size={15} />
          </button>
          {onLogout ? (
            <button
              type="button"
              onClick={onLogout}
              aria-label={t("nav.logout")}
              title={t("nav.logout")}
              className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-text-muted transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:text-text-muted dark:hover:bg-surface-panel dark:hover:text-text-primary dark:focus-visible:ring-accent"
            >
              <LogOut size={15} />
            </button>
          ) : null}
        </div>

        <div className="hidden min-w-0 flex-nowrap items-center justify-start gap-2 xl:flex xl:justify-end">
          <LanguagePicker compact />
          <div className="inline-flex items-center gap-1 rounded-lg border border-border-l1 bg-surface-base p-1 dark:border-border-l2 dark:bg-surface-base">
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
                      ? "bg-surface-raised text-accent shadow-sm dark:bg-surface-panel dark:text-accent"
                      : "text-text-muted hover:text-text-primary dark:text-text-muted dark:hover:text-text-primary"
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
              className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-text-muted transition-colors hover:bg-surface-subtle hover:text-text-primary dark:text-text-muted dark:hover:bg-surface-panel dark:hover:text-text-primary"
            >
              <LogOut size={15} />
            </button>
          ) : null}
        </div>
      </nav>

      <div
        aria-label={t("nav.mobile")}
        className="fixed inset-x-0 bottom-0 z-50 border-t border-border-l1 bg-surface-raised/96 px-2 pt-1.5 pb-[calc(env(safe-area-inset-bottom)+0.4rem)] shadow-[0_-10px_30px_rgba(15,23,42,0.12)] backdrop-blur dark:border-border-l2 dark:bg-surface-base/94 dark:shadow-[0_-18px_40px_rgba(0,0,0,0.35)] lg:hidden"
      >
        <div
          className="mx-auto grid w-full max-w-xl gap-1"
          style={{ gridTemplateColumns: `repeat(${visibleNavItems.length}, minmax(0, 1fr))` }}
        >
          {visibleNavItems.map((item) => {
            const Icon = item.icon;
            const active = item.match(location.pathname);
            const label = t(item.labelKey);
            return (
              <Link
                key={item.to}
                to={item.to}
                aria-current={active ? "page" : undefined}
                aria-label={label}
                className={`flex min-h-12 min-w-0 flex-col items-center justify-center rounded-xl px-0.5 text-[10px] font-semibold transition-colors active:scale-[0.98] focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:focus-visible:ring-accent ${
                  active
                    ? "bg-accent-soft text-accent dark:bg-accent/18 dark:text-accent"
                    : "text-text-muted hover:bg-surface-subtle hover:text-text-primary dark:text-text-muted dark:hover:bg-surface-base dark:hover:text-text-primary"
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
