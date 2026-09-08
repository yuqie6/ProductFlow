import { afterEach, describe, expect, it, vi } from "vitest";
import enUS from "./locales/en-US.json";
import jaJP from "./locales/ja-JP.json";

afterEach(() => { vi.unstubAllGlobals(); vi.resetModules(); });
describe("locale resources", () => {
  it("requests only the selected resource, deduplicates pending loads and caches success", async () => {
    vi.resetModules();
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(enUS)));
    vi.stubGlobal("fetch", fetcher);
    const { loadLocale, localeResourceUrls } = await import("./localeResources");
    const first = loadLocale("en-US");
    expect(loadLocale("en-US")).toBe(first);
    await first;
    await loadLocale("en-US");
    expect(fetcher).toHaveBeenCalledExactlyOnceWith(localeResourceUrls["en-US"]);
    const { getLoadedLocale, translate } = await import("./i18n");
    expect(getLoadedLocale("zh-CN")).toBeUndefined();
    expect(translate("en-US", "nav.language")).toBe(enUS["nav.language"]);
  });
  it("retries a failed URL instead of retaining its rejected promise", async () => {
    vi.resetModules();
    const fetcher = vi.fn().mockResolvedValueOnce(new Response("", { status: 503 })).mockResolvedValueOnce(new Response(JSON.stringify(jaJP)));
    vi.stubGlobal("fetch", fetcher);
    const { loadLocale } = await import("./localeResources");
    await expect(loadLocale("ja-JP")).rejects.toThrow("503");
    await expect(loadLocale("ja-JP")).resolves.toEqual(jaJP);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
  it.each(["missing", "extra", "non-string", "malformed"])("rejects %s resources without installing a partial dictionary", async (kind) => {
    vi.resetModules();
    const invalid: Record<string, unknown> = { ...enUS };
    if (kind === "missing") delete invalid["nav.language"];
    if (kind === "extra") invalid.unexpected = "extra";
    if (kind === "non-string") invalid["nav.language"] = 123;
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(kind === "malformed" ? "{" : JSON.stringify(invalid))));
    const { loadLocale } = await import("./localeResources");
    await expect(loadLocale("en-US")).rejects.toThrow();
    const { getLoadedLocale, translate } = await import("./i18n");
    expect(getLoadedLocale("en-US")).toBeUndefined();
    expect(() => translate("en-US", "nav.language")).toThrow("not loaded");
  });
});
