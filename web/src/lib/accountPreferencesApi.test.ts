import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("本人偏好与商家资料 wire", () => {
  it("patches only the selected preference without a target identity", async () => {
    const preferences = { locale: "ja-JP", theme: "dark" };
    const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify(preferences)));
    vi.stubGlobal("fetch", fetch);
    await expect(api.updateAccountPreferences({ locale: "ja-JP" })).resolves.toEqual(preferences);
    expect(fetch.mock.calls[0][0]).toContain("/api/account/preferences");
    expect(fetch.mock.calls[0][1].method).toBe("PATCH");
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ locale: "ja-JP" });
    await api.updateAccountPreferences({ theme: "system" });
    expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({ theme: "system" });
  });
  it("patches the directly owned merchant and preserves a rejection", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ detail: "Merchant suspended" }), { status: 403 }));
    vi.stubGlobal("fetch", fetch);
    await expect(api.updateAccountMerchant({ name: "Studio" })).rejects.toMatchObject({ status: 403 });
    expect(fetch.mock.calls[0][0]).toContain("/api/account/merchant");
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ name: "Studio" });
  });
});
