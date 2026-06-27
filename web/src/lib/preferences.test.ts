import { describe, expect, it } from "vitest";

import {
  DEFAULT_LOCALE,
  LOCALES,
  LOCALE_LABEL_KEYS,
  interpolate,
  resolveLocale,
  translate,
  translations,
} from "./i18n";
import { resolveTheme, resolveThemePreference } from "./theme";

describe("i18n helpers", () => {
  it("resolves supported locales and falls back to Chinese", () => {
    expect(resolveLocale("en-US")).toBe("en-US");
    expect(resolveLocale("zh-CN")).toBe("zh-CN");
    expect(resolveLocale("vi-VN")).toBe("vi-VN");
    expect(resolveLocale("fr-FR")).toBe(DEFAULT_LOCALE);
    expect(resolveLocale(null)).toBe(DEFAULT_LOCALE);
  });

  it("keeps locale selector metadata aligned with supported locales", () => {
    const defaultKeys = Object.keys(translations[DEFAULT_LOCALE]).sort();

    expect(Object.keys(LOCALE_LABEL_KEYS).sort()).toEqual([...LOCALES].sort());
    for (const locale of LOCALES) {
      expect(Object.keys(translations[locale]).sort()).toEqual(defaultKeys);
    }
    expect(translate("vi-VN", "locale.viVN")).toBe("Tiếng Việt");
    expect(translate("vi-VN", "nav.language")).toBe("Ngôn ngữ");
    expect(translate("vi-VN", "detail.runWorkflow")).toBe(
      "Lưu cấu hình hiện tại rồi chạy toàn bộ quy trình trên canvas",
    );
  });

  it("translates keys with interpolation", () => {
    expect(translate("zh-CN", "products.paginationSummary", { page: 2, totalPages: 5, total: 48 })).toBe(
      "第 2 / 5 页 · 共 48 个商品",
    );
    expect(translate("en-US", "products.paginationSummary", { page: 2, totalPages: 5, total: 48 })).toBe(
      "Page 2 / 5 · 48 products",
    );
    expect(interpolate("Hello {name}, {missing}", { name: "Ada" })).toBe("Hello Ada, {missing}");
  });
});

describe("theme helpers", () => {
  it("resolves persisted theme preference values", () => {
    expect(resolveThemePreference("dark")).toBe("dark");
    expect(resolveThemePreference("light")).toBe("light");
    expect(resolveThemePreference("system")).toBe("system");
    expect(resolveThemePreference("sepia")).toBe("system");
  });

  it("resolves system mode from prefers-color-scheme", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
    expect(resolveTheme("dark", false)).toBe("dark");
    expect(resolveTheme("light", true)).toBe("light");
  });
});
