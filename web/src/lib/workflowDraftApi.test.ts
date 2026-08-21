import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("workflow draft API contract", () => {
  it("confirms the exact draft revision currently under review", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: "draft-1", current_version: 4 }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await api.confirmWorkflowDraft("product-1", "draft-1", 4);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v2/products/product-1/workflow-drafts/draft-1/confirm",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ expected_draft_version: 4 }),
      }),
    );
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
