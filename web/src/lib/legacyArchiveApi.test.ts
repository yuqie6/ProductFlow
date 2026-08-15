import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("legacy archive API", () => {
  it("encodes archive filters and cursor", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ items: [], next_cursor: null, total: 0, kind_counts: {} }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.listLegacyArchives({
      kind: "workflow",
      product_id: "product/1",
      q: "橙色 & blue",
      after: "cursor=1/+",
      limit: 20,
    });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    const parsed = new URL(url, "http://localhost");
    expect(parsed.pathname).toBe("/api/v2/legacy-archives");
    expect(Object.fromEntries(parsed.searchParams)).toEqual({
      limit: "20",
      kind: "workflow",
      product_id: "product/1",
      q: "橙色 & blue",
      after: "cursor=1/+",
    });
  });

  it("encodes detail ids and downloads the deterministic export as a credentialed blob", async () => {
    const blob = new Blob(["{}"], { type: "application/json" });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ item: {} }) })
      .mockResolvedValueOnce({ ok: true, status: 200, blob: async () => blob });
    vi.stubGlobal("fetch", fetchMock);

    await api.getLegacyArchive("canvas_agent_thread", "archive/1");
    await expect(api.downloadLegacyArchive("canvas_agent_thread", "archive/1")).resolves.toBe(blob);

    expect(fetchMock.mock.calls[0][0]).toBe(
      "/api/v2/legacy-archives/canvas_agent_thread/archive%2F1",
    );
    expect(fetchMock.mock.calls[1]).toEqual([
      "/api/v2/legacy-archives/canvas_agent_thread/archive%2F1/export",
      { credentials: "include" },
    ]);
  });

  it("creates an idempotent Agent rebuild for one explicit target product", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({ created: true }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.createLegacyArchiveAgentRebuild("user_template", "archive/1", {
      target_product_id: "product/1",
      idempotency_key: "legacy-rebuild-1",
    });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v2/legacy-archives/user_template/archive%2F1/agent-rebuilds");
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("include");
    expect(JSON.parse(String(init.body))).toEqual({
      target_product_id: "product/1",
      idempotency_key: "legacy-rebuild-1",
    });
  });
});
