import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("delivery export API", () => {
  it("posts the complete job selection to the encoded v3 export route and returns its blob", async () => {
    const blob = new Blob(["zip"], { type: "application/zip" });
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, blob: async () => blob });
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.downloadDeliveryExport("product / 1", ["job-1", "job-2"])).resolves.toBe(blob);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v3/products/product%20%2F%201/delivery-exports",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ rendition_job_ids: ["job-1", "job-2"], allow_partial: false }),
      }),
    );
  });

  it("preserves the server ApiError for a rejected export", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ detail: "A rendition is not exportable" }),
      { status: 409, statusText: "Conflict" },
    ));
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.downloadDeliveryExport("product-1", ["job-1"])).rejects.toEqual(
      new ApiError(409, "A rendition is not exportable"),
    );
  });
});
