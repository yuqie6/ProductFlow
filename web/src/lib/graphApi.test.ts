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
    await api.getCurrentWorkflowGraph("product/1");
    await api.applyWorkflowChangeSet("product/1", "graph/1", {
      base_graph_revision: 1,
      summary: "move",
      operations: [{ op: "move_nodes", nodes: [["n1", 1, 2]] }],
    });
    await api.submitGraphRun("product/1", "graph/1", { scope: "graph" });
    await api.undoWorkflowChangeSet("product/1", "graph/1");
    await api.persistConfirmedDraftGraph("product/1", "draft/1", 3);

    expect(calls).toEqual([
      "GET /api/v3/node-catalog",
      "GET /api/v3/products/product%2F1/workflows/current",
      "POST /api/v3/products/product%2F1/workflows/graph%2F1/changesets",
      "POST /api/v3/products/product%2F1/workflows/graph%2F1/runs",
      "POST /api/v3/products/product%2F1/workflows/graph%2F1/undo",
      "POST /api/v3/products/product%2F1/workflow-drafts/draft%2F1/graphs",
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
});
