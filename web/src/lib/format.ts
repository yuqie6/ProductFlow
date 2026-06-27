import { DEFAULT_LOCALE, type Locale } from "./i18n";

export function formatShortDate(value: string | null | undefined, locale: Locale = DEFAULT_LOCALE): string {
  if (!value) {
    return "--";
  }
  return new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date(value));
}

export function formatDateTime(value: string | null | undefined, locale: Locale = DEFAULT_LOCALE): string {
  if (!value) {
    return "--";
  }
  return new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

export function formatPrice(value: string | null | undefined): string {
  if (!value) {
    return "";
  }
  return `¥${value}`;
}

