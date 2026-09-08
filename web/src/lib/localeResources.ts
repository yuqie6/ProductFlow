import { getLoadedLocale, installLocaleDictionary, type Locale, type TranslationDictionary } from "./i18n";

// Vite emits content-hashed asset URLs. Loading one resource does not preload the others.
export const localeResourceUrls = {
  "zh-CN": new URL("./locales/zh-CN.json", import.meta.url).href,
  "en-US": new URL("./locales/en-US.json", import.meta.url).href,
  "ja-JP": new URL("./locales/ja-JP.json", import.meta.url).href,
  "vi-VN": new URL("./locales/vi-VN.json", import.meta.url).href,
} satisfies Record<Locale, string>;
const pending = new Map<Locale, Promise<TranslationDictionary>>();

export function loadLocale(locale: Locale): Promise<TranslationDictionary> {
  const loaded = getLoadedLocale(locale);
  if (loaded) return Promise.resolve(loaded);
  const existing = pending.get(locale);
  if (existing) return existing;
  const request = fetch(localeResourceUrls[locale])
    .then(async (response) => {
      if (!response.ok) throw new Error(`Language request failed: ${response.status}`);
      return installLocaleDictionary(locale, await response.json());
    })
    .finally(() => pending.delete(locale));
  pending.set(locale, request);
  return request;
}
