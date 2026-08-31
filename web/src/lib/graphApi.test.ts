import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

describe("v3 graph API helpers", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("posts direct create, changesets, and runs to v3 paths", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      calls.push(`${init?.method ?? "GET"} ${new URL(url, "http://localhost").pathname}`);
      return new Response(JSON.stringify({ id: "ok", schema_version: 3, revision: 1, nodes: [], edges: [], groups: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    await api.getGraphNodeCatalog();
    await api.createEmptyWorkflowGraph("product/1");
    await api.getCurrentWorkflowGraph("product/1");
    await api.applyWorkflowChangeSet("product/1", "graph/1", {
      base_graph_revision: 1,
      summary: "move",
      operations: [{ op: "move_nodes", nodes: [["n1", 1, 2]] }],
    });
    await api.submitGraphRun("product/1", "graph/1", { scope: "graph" });
    await api.undoWorkflowChangeSet("product/1", "graph/1");
    await api.redoWorkflowChangeSet("product/1", "graph/1");

    expect(calls).toEqual([
      "GET /api/v3/node-catalog",
      "POST /api/v3/products/product%2F1/workflows",
      "GET /api/v3/products/product%2F1/workflows/current",
      "POST /api/v3/products/product%2F1/workflows/graph%2F1/changesets",
      "POST /api/v3/products/product%2F1/workflows/graph%2F1/runs",
      "POST /api/v3/products/product%2F1/workflows/graph%2F1/undo",
      "POST /api/v3/products/product%2F1/workflows/graph%2F1/redo",
    ]);
  });

  it("uses typed product facts endpoints and preserves the optimistic version contract", async () => {
    const requests: Array<{ method: string; path: string; body: string }> = [];
    vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
      requests.push({
        method: init?.method ?? "GET",
        path: new URL(String(input), "http://localhost").pathname,
        body: typeof init?.body === "string" ? init.body : "",
      });
      return new Response(JSON.stringify({
        product: {
          id: "product-2",
          name: "商品二",
          category: null,
          price: null,
          source_note: null,
          cover_image_asset_id: null,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
        fact_set: { id: "facts-2", version: 2, facts: [{ key: "material", value: "steel" }] },
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    await api.getProductFacts("product/2");
    await api.updateProductFacts("product/2", {
      expected_fact_version: 2,
      name: "商品二",
      category: "工具",
      price: "99",
      source_note: "手工维护",
      facts: [{ key: "material", value: "steel" }],
    });

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /api/v3/products/product%2F2/facts",
      "PUT /api/v3/products/product%2F2/facts",
    ]);
    expect(JSON.parse(requests[1].body)).toMatchObject({ expected_fact_version: 2, facts: [{ key: "material", value: "steel" }] });
  });

  it("posts direct-create intake as source_note and generation_spec form fields", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({ product: { id: "product-1" }, graph: { id: "graph-1" } }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const image = new File(["ref"], "ref.png", { type: "image/png" });

    await api.createProductDirect({
      name: "带字海报商品",
      images: [image],
      imageTypes: [
        { key: "hero", quantity: 1, aspect_ratio: "3:4" },
        { key: "detail", quantity: 1, aspect_ratio: "1:1" },
      ],
      sourceNote: "无线洗地机，面向都市白领",
      generationSpec: {
        aspect_ratio: "3:4",
        resolution_tier: "high",
        quality_intent: "high",
        reference_fidelity: "high",
        background_intent: "auto",
        text_policy: "required",
        text_language: "zh-CN",
      },
    });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v3/products");
    expect(init.method).toBe("POST");
    expect(init.body).toBeInstanceOf(FormData);
    const formData = init.body as FormData;
    expect(formData.get("name")).toBe("带字海报商品");
    expect(formData.get("source_note")).toBe("无线洗地机，面向都市白领");
    expect(formData.get("delivery_preset_key")).toBeNull();
    expect(JSON.parse(String(formData.get("image_types")))).toEqual([
      { key: "hero", quantity: 1, aspect_ratio: "3:4" },
      { key: "detail", quantity: 1, aspect_ratio: "1:1" },
    ]);
    expect(JSON.parse(String(formData.get("generation_spec")))).toMatchObject({
      text_policy: "required",
      text_language: "zh-CN",
    });
    expect(formData.getAll("images")).toEqual([image]);
  });

  it("adds the direct-create delivery preset field only for an explicit selection", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({ product: { id: "product-1" }, graph: { id: "graph-1" } }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.createProductDirect({
      name: "平台商品",
      images: [],
      imageTypes: [{ key: "hero", quantity: 1 }],
      deliveryPresetKey: "jd_hero",
    });

    const formData = (fetchMock.mock.calls[0] as [string, RequestInit])[1].body as FormData;
    expect(formData.get("delivery_preset_key")).toBe("jd_hero");
  });

  it("posts create-page source-note generate as multipart images and optional drafts", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        visible: "厚壁玻璃密封瓶",
        fields: [
          { label: "材质", value: "玻璃" },
          { label: "容量", value: "" },
        ],
      }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const image = new File(["ref"], "ref.png", { type: "image/png" });

    const got = await api.generateProductSourceNote({
      images: [image],
      productName: "密封瓶",
      currentNote: "手填外形",
    });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v2/product-source-notes/generate");
    expect(init.method).toBe("POST");
    expect(init.body).toBeInstanceOf(FormData);
    const formData = init.body as FormData;
    expect(formData.get("product_name")).toBe("密封瓶");
    expect(formData.get("current_note")).toBe("手填外形");
    expect(formData.getAll("images")).toEqual([image]);
    expect(got.visible).toBe("厚壁玻璃密封瓶");
    expect(got.fields).toEqual([
      { label: "材质", value: "玻璃" },
      { label: "容量", value: "" },
    ]);
  });
});
