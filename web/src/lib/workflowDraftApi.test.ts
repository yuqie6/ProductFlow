import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("workflow recipe and rendition API contract", () => {
  it("uses recipe v3 routes and sends the confirmed preview fence unchanged", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.listWorkflowRecipes(false);
    await api.getWorkflowRecipe("recipe/1");
    await api.archiveWorkflowRecipe("recipe/1", 3);
    await api.applyWorkflowRecipe("product/1", "recipe/1", {
      expected_recipe_version: 3,
      expected_graph_revision: 9,
      preview_digest: "a".repeat(64),
      idempotency_key: "recipe-apply-1",
    });

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v3/workflow-recipes?include_archived=false",
      "/api/v3/workflow-recipes/recipe%2F1",
      "/api/v3/workflow-recipes/recipe%2F1?expected_recipe_version=3",
      "/api/v3/products/product%2F1/workflow-recipes/recipe%2F1/apply",
    ]);
    expect(fetchMock.mock.calls[3]?.[1]).toEqual(expect.objectContaining({
      method: "POST",
      credentials: "include",
      body: JSON.stringify({
        expected_recipe_version: 3,
        expected_graph_revision: 9,
        preview_digest: "a".repeat(64),
        idempotency_key: "recipe-apply-1",
      }),
    }));
  });

  it("creates, lists, reads, and retries delivery renditions through stable asset and job ids", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      json: async () => ({ id: "job-1", status: "queued" }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const spec = { width: 1600, height: 900, format: "webp" as const, fit: "cover" as const };

    await api.createDeliveryRendition("source/1", spec);
    await api.listDeliveryRenditions("source/1");
    await api.getDeliveryRenditionJob("job/1");
    await api.retryDeliveryRenditionJob("job/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v2/product-image-assets/source%2F1/renditions",
      "/api/v2/product-image-assets/source%2F1/renditions",
      "/api/v2/delivery-rendition-jobs/job%2F1",
      "/api/v2/delivery-rendition-jobs/job%2F1/retry",
    ]);
    expect(fetchMock.mock.calls[0]?.[1]).toEqual(expect.objectContaining({
      method: "POST",
      credentials: "include",
      body: JSON.stringify(spec),
    }));
    expect(fetchMock.mock.calls[3]?.[1]).toEqual(expect.objectContaining({
      method: "POST",
      credentials: "include",
    }));
  });
});
