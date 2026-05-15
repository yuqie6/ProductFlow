import { describe, expect, it } from "vitest";

import { getHelpDocsForLocale, getHelpNavGroupsForLocale, getMissingHelpDocTranslations } from "./HelpPage";

describe("HelpPage locale documents", () => {
  it("serves Japanese help documents instead of falling back to English", () => {
    const pages = getHelpDocsForLocale("ja-JP");
    const groups = getHelpNavGroupsForLocale("ja-JP");

    expect(pages[0].title).toBe("ProductFlow ドキュメント概要");
    expect(pages[0].category).toBe("はじめに");
    expect(groups[0].title).toBe("はじめに");
    expect(pages.some((page) => page.title === "ProductFlow Docs Overview")).toBe(false);
  });

  it("has Japanese translations for every built-in help document string", () => {
    expect(getMissingHelpDocTranslations()).toEqual([]);
  });
});
