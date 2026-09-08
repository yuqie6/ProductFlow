import { afterEach, expect, it, vi } from "vitest";
import { getMerchantOverview } from "./merchantOverviewApi";
import { api } from "./api";
afterEach(() => vi.unstubAllGlobals());
it("sends only frozen self-merchant overview filters through the shared HTTP boundary", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ detail: "Unavailable" }), { status: 503 }));
  vi.stubGlobal("fetch", fetch);
  await expect(getMerchantOverview({ days: 7, kind: "image_session", state: "waiting", page: 2, page_size: 10 })).rejects.toMatchObject({ status: 503 });
  const url = new URL(fetch.mock.calls[0][0], "http://localhost");
  expect(url.pathname).toBe("/api/v2/products/overview");
  expect(Object.fromEntries(url.searchParams)).toEqual({ days: "7", kind: "image_session", state: "waiting", page: "2", page_size: "10" });
});
it("encodes an explicit image session id before reading its detail", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response("{}"));
  vi.stubGlobal("fetch", fetch);
  await api.getImageSession("id/a?b#c");
  expect(fetch.mock.calls[0][0]).toContain("/api/image-sessions/id%2Fa%3Fb%23c");
});
