import { afterEach, describe, expect, it, vi } from "vitest";

import type { DeliveryAdoptionCreateInput } from "./types";
import { ApiError, api } from "./api";

afterEach(() => vi.unstubAllGlobals());

const body: DeliveryAdoptionCreateInput = {
  acknowledge_quality_warnings: false,
  slots: [{
    slot_key: "node-1",
    sort_order: 0,
    source_asset_id: "asset-1",
    source_node_id: "node-1",
    delivery_spec: { width: 800, height: 800, format: "png", fit: "contain" },
    quality_status: "fail",
    quality_detail: "图位文字溢出",
    text_overflow: true,
  }],
  graph_id: "graph-1",
  graph_revision: 4,
};

describe("delivery adoption API", () => {
  it("posts the quality acknowledgement with the typed adoption snapshot", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}", { status: 201 }));
    vi.stubGlobal("fetch", fetchMock);

    await api.createDeliveryAdoption("product / 1", body);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v3/products/product%20%2F%201/delivery-adoptions",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }),
    );
  });

  it("preserves the server error code used for quality confirmation", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(
      JSON.stringify({
        code: "adoption_quality_confirmation_required",
        detail: "图位 node-1：图片质量尚未确认",
      }),
      { status: 409, statusText: "Conflict" },
    ));
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.createDeliveryAdoption("product-1", body)).rejects.toEqual(
      new ApiError(
        409,
        "图位 node-1：图片质量尚未确认",
        null,
        "adoption_quality_confirmation_required",
      ),
    );
  });
});
