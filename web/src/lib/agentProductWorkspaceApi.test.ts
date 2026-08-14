import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("Agent product workspace API", () => {
  it("loads the backend-owned options catalog and read-only workbench bootstrap", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.getAgentProductWorkspaceOptions();
    await api.getAgentWorkbench("product/1");
    await api.getActiveProductWorkflowV2("product-1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/agent-product-workspaces/options",
      "/api/v2/products/product%2F1/agent-workbench",
      "/api/v2/products/product-1/workflow",
    ]);
    expect(fetchMock.mock.calls.map(([url]) => url)).not.toContain(
      "/api/products/product-1/workflow",
    );
    expect(fetchMock.mock.calls.map(([url]) => url)).not.toContain(
      "/api/workflow/canvas-templates",
    );
  });

  it("owns the multipart field names and keeps one stable idempotency header", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({ product: { id: "product-1" } }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const front = new File(["front"], "front.png", { type: "image/png" });
    const detail = new File(["detail"], "detail.webp", { type: "image/webp" });

    await api.createAgentProductWorkspace({
      name: "硬质刀具收纳套装",
      selection: {
        schema_version: 1,
        image_types: [
          { key: "scene", quantity: 3, order: 0 },
          { key: "hero", quantity: 2, order: 1 },
        ],
      },
      images: [front, detail],
      idempotency_key: "agent-create-1",
    });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v2/agent-product-workspaces");
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("include");
    expect(init.headers).toEqual({ "Idempotency-Key": "agent-create-1" });
    expect(init.body).toBeInstanceOf(FormData);
    const formData = init.body as FormData;
    expect(formData.get("name")).toBe("硬质刀具收纳套装");
    expect(JSON.parse(String(formData.get("selection")))).toEqual({
      schema_version: 1,
      image_types: [
        { key: "scene", quantity: 3, order: 0 },
        { key: "hero", quantity: 2, order: 1 },
      ],
    });
    expect(formData.getAll("images")).toEqual([front, detail]);
    expect(fetchMock.mock.calls.map(([requestUrl]) => requestUrl)).not.toContain(
      "/api/workflow/canvas-templates",
    );
  });
});
