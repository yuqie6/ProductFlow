import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import { getAccountGeneration, isCurrentAccountGeneration } from "./accountBoundary";
import type { AccountPreferences, AccountProfile, SessionState } from "./types";

import {
  getLoadedLocale,
  DEFAULT_LOCALE,
  LOCALE_STORAGE_KEY,
  type Locale,
  type TranslationKey,
  type TranslationParams,
  resolveLocale,
  translate,
} from "./i18n";
import { loadLocale } from "./localeResources";
import { LocaleLoading } from "./LocaleLoading";
import {
  DEFAULT_THEME_PREFERENCE,
  THEME_STORAGE_KEY,
  type ResolvedTheme,
  type ThemePreference,
  applyThemeToRoot,
  resolveTheme,
  resolveThemePreference,
} from "./theme";

export type TranslateFunction = ((key: TranslationKey, params?: TranslationParams) => string) & { locale?: Locale };

interface PreferencesContextValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: TranslateFunction;
  themePreference: ThemePreference;
  setThemePreference: (theme: ThemePreference) => void;
  resolvedTheme: ResolvedTheme;
  saving: boolean;
  saveFailed: boolean;
  saved: boolean;
  retrySave: () => void;
}

const PreferencesContext = createContext<PreferencesContextValue | null>(null);

function readStorage(key: string): string | null {
  if (typeof window === "undefined") {
    return null;
  }
  return window.localStorage.getItem(key);
}

function getSystemPrefersDark(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) {
    return false;
  }
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function PreferencesProvider({ children }: { children: ReactNode }) {
  const [anonymousLocale, setLocaleState] = useState<Locale>(() => resolveLocale(readStorage(LOCALE_STORAGE_KEY)));
  const [anonymousTheme, setThemePreferenceState] = useState<ThemePreference>(() =>
    resolveThemePreference(readStorage(THEME_STORAGE_KEY)),
  );
  const [systemPrefersDark, setSystemPrefersDark] = useState(getSystemPrefersDark);
  const queryClient = useQueryClient();
  const session = useQuery({ queryKey: ["session"], queryFn: api.getSessionState, retry: false });
  const userId = session.data?.authenticated ? session.data.user.id : null;
  const targetLocale = session.data?.authenticated ? session.data.preferences.locale : anonymousLocale;
  const language = useQuery({ queryKey: ["locale", targetLocale], queryFn: () => loadLocale(targetLocale), enabled: !session.isPending, retry: false, staleTime: Infinity, initialData: () => getLoadedLocale(targetLocale) });
  const ready = !session.isPending && Boolean(language.data);
  // Keep mounted routes alive so their existing account-boundary effects still run.
  const lastLocale = useRef<Locale | null>(null);
  if (ready) lastLocale.current = targetLocale;
  const locale = ready ? targetLocale : lastLocale.current ?? targetLocale;
  const sequence = useRef(0);
  const themePreference = session.data?.authenticated ? session.data.preferences.theme : anonymousTheme;
  type Operation = { userId: string | null; generation: number; sequence: number; input: Partial<AccountPreferences> };
  const isCurrent = (operation: Operation) => {
    const current = queryClient.getQueryData<SessionState>(["session"]);
    const currentId = current?.authenticated ? current.user.id : null;
    return operation.sequence === sequence.current && isCurrentAccountGeneration(operation.generation) && currentId === operation.userId;
  };
  const save = useMutation({
    mutationFn: async (operation: Operation) => {
      if (operation.input.locale) await loadLocale(operation.input.locale);
      // A language download may finish after logout or another account has signed in.
      if (!isCurrent(operation)) return null;
      return operation.userId ? api.updateAccountPreferences(operation.input) : null;
    },
    onSuccess: (preferences, operation) => {
      if (!isCurrent(operation)) return;
      if (!operation.userId) {
        if (operation.input.locale) setLocaleState(operation.input.locale);
        if (operation.input.theme) setThemePreferenceState(operation.input.theme);
      } else if (preferences) {
        queryClient.setQueryData<SessionState>(["session"], (current) => current?.authenticated ? { ...current, preferences } : current);
        queryClient.setQueryData<AccountProfile>(["account", operation.userId], (current) => current ? { ...current, preferences } : current);
      }
    },
  });
  const currentSave = save.variables && isCurrent(save.variables);
  const change = (input: Partial<AccountPreferences>) => {
    if (session.isPending || session.isError) return;
    if (!userId && input.theme) {
      // Theme is synchronous locally; it must not supersede an in-flight language choice.
      setThemePreferenceState(input.theme);
      return;
    }
    if (userId && currentSave && save.isPending) return;
    save.mutate({ userId, generation: getAccountGeneration(), sequence: ++sequence.current, input });
  };

  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) {
      return;
    }
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const handleChange = () => setSystemPrefersDark(media.matches);
    handleChange();
    media.addEventListener("change", handleChange);
    return () => media.removeEventListener("change", handleChange);
  }, []);

  const resolvedTheme = resolveTheme(themePreference, systemPrefersDark);

  useEffect(() => {
    document.documentElement.lang = targetLocale;
  }, [targetLocale]);

  useEffect(() => {
    applyThemeToRoot(document.documentElement, resolvedTheme, themePreference);
  }, [resolvedTheme, themePreference]);

  useEffect(() => { window.localStorage.setItem(LOCALE_STORAGE_KEY, anonymousLocale); }, [anonymousLocale]);
  useEffect(() => { window.localStorage.setItem(THEME_STORAGE_KEY, anonymousTheme); }, [anonymousTheme]);

  const t = useMemo<TranslateFunction>(() => {
    const translateCurrent: TranslateFunction = (key, params) => translate(locale, key, params);
    translateCurrent.locale = locale;
    return translateCurrent;
  }, [locale]);
  const value: PreferencesContextValue = {
    locale, t, themePreference, resolvedTheme,
    setLocale: (locale) => change({ locale }),
    setThemePreference: (theme) => change({ theme }),
    saving: session.isPending || session.isError || Boolean(currentSave && save.isPending),
    saveFailed: Boolean(currentSave && save.isError),
    saved: Boolean(currentSave && save.isSuccess),
    retrySave: () => { if (currentSave && save.variables) change(save.variables.input); },
  };

  return <PreferencesContext.Provider value={value}>
    {!ready && <LocaleLoading locale={targetLocale} failed={language.isError} retry={() => { void language.refetch(); }} sessionPending={session.isPending} />}
    {lastLocale.current && <div style={{ display: ready ? "contents" : "none" }} aria-hidden={!ready || undefined}>{children}</div>}
  </PreferencesContext.Provider>;
}

export function usePreferences(): PreferencesContextValue {
  const context = useContext(PreferencesContext);
  if (!context) {
    const t: TranslateFunction = (key, params) => translate(DEFAULT_LOCALE, key, params);
    t.locale = DEFAULT_LOCALE;
    return {
      saving: false, saveFailed: false, saved: false, retrySave: () => undefined,
      locale: DEFAULT_LOCALE,
      setLocale: () => undefined,
      t,
      themePreference: DEFAULT_THEME_PREFERENCE,
      setThemePreference: () => undefined,
      resolvedTheme: resolveTheme(DEFAULT_THEME_PREFERENCE, false),
    };
  }
  return context;
}

export function useI18n() {
  const { locale, setLocale, t } = usePreferences();
  return { locale, setLocale, t };
}
