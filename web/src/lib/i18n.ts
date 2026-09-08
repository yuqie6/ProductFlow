import { TRANSLATION_KEYS } from "./translationKeys";

export const LOCALES = ["zh-CN", "en-US", "ja-JP", "vi-VN"] as const;
export type Locale = (typeof LOCALES)[number];
export type TranslationKey = (typeof TRANSLATION_KEYS)[number];
export type TranslationParams = Record<string, string | number>;
export type TranslationDictionary = Record<TranslationKey, string>;
export const DEFAULT_LOCALE: Locale = "zh-CN";
export const LOCALE_STORAGE_KEY = "productflow.locale";
export const LOCALE_LABEL_KEYS = {
  "zh-CN": "locale.zhCN", "en-US": "locale.enUS", "ja-JP": "locale.jaJP", "vi-VN": "locale.viVN",
} satisfies Record<Locale, TranslationKey>;

const keySet: ReadonlySet<string> = new Set(TRANSLATION_KEYS);
const dictionaries = new Map<Locale, TranslationDictionary>();

export function isLocale(value: string | null | undefined): value is Locale {
  return LOCALES.includes(value as Locale);
}
export function resolveLocale(value: string | null | undefined): Locale {
  return isLocale(value) ? value : DEFAULT_LOCALE;
}
export function isTranslationKey(value: string | null | undefined): value is TranslationKey {
  return typeof value === "string" && keySet.has(value);
}

/** Every resource must have exactly the supported keys and string values. */
export function installLocaleDictionary(locale: Locale, input: unknown): TranslationDictionary {
  if (!input || typeof input !== "object" || Array.isArray(input)
    || Object.keys(input).length !== TRANSLATION_KEYS.length
    || !TRANSLATION_KEYS.every((key) => Object.hasOwn(input, key) && typeof Reflect.get(input, key) === "string")) {
    throw new Error(`Invalid language resource: ${locale}`);
  }
  const dictionary = input as TranslationDictionary;
  dictionaries.set(locale, dictionary);
  return dictionary;
}
export function getLoadedLocale(locale: Locale): TranslationDictionary | undefined {
  return dictionaries.get(locale);
}
export function interpolate(template: string, params: TranslationParams = {}): string {
  return template.replace(/\{(\w+)\}/g, (match, key: string) => {
    const value = params[key];
    return value === undefined ? match : String(value);
  });
}
export function translate(locale: Locale, key: TranslationKey, params?: TranslationParams): string {
  const dictionary = getLoadedLocale(locale);
  if (!dictionary) throw new Error(`Language is not loaded: ${locale}`);
  return interpolate(dictionary[key], params);
}
