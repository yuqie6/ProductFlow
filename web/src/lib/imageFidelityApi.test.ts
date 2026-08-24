import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, api } from "./api";

afterEach(() => vi.unstubAllGlobals());

function checkPayload() {
  return {
    id: "check-1",
    product_id: "product-1",
    asset_id: "asset-1",
    version: 1,
    shape_fidelity: "pass",
    color_material_fidelity: "fail",
    logo_text_legibility: "not_applicable",
    text_policy_compliance: "pass",
    notes: "保留细节",
    checked_by: "administrator",
    idempotency_key: "fidelity-key-1",
    request_hash: "a".repeat(64),
    created_at: "2026-08-24T10:00:00Z",
  };
}

function okResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("image fidelity API", () => {
  it("uses the v3 scoped path and canonical POST body", async () => {
    const record = checkPayload();
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(okResponse({
        product_id: "product-1",
        asset_id: "asset-1",
        latest_version: 1,
        items: [record],
      }))
      .mockResolvedValueOnce(okResponse(record, 201));
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.listProductImageFidelityChecks("product / one", "asset/one")).resolves.toMatchObject({
      latest_version: 1,
    });
    const input = {
      expected_latest_version: 1,
      idempotency_key: "fidelity-key-1",
      shape_fidelity: "pass" as const,
      color_material_fidelity: "fail" as const,
      logo_text_legibility: "not_applicable" as const,
      text_policy_compliance: "pass" as const,
      notes: "保留细节",
    };
    await expect(api.createProductImageFidelityCheck("product / one", "asset/one", input)).resolves.toMatchObject(record);

    expect(fetchMock.mock.calls.map(([url, init]) => [url, init?.method ?? "GET"])).toEqual([
      ["/api/v3/products/product%20%2F%20one/image-assets/asset%2Fone/fidelity-checks", "GET"],
      ["/api/v3/products/product%20%2F%20one/image-assets/asset%2Fone/fidelity-checks", "POST"],
    ]);
    expect((fetchMock.mock.calls[1]?.[1] as RequestInit).body).toBe(JSON.stringify(input));
  });

  it("rejects malformed responses and preserves strict conflict status", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(okResponse({ items: [] })));
    await expect(api.listProductImageFidelityChecks("product-1", "asset-1")).rejects.toMatchObject({ status: 502 });

    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(okResponse({ detail: "版本已变化" }, 409)));
    const conflictRequest = api.createProductImageFidelityCheck("product-1", "asset-1", {
      expected_latest_version: 0,
      idempotency_key: "key-1",
      shape_fidelity: "pass",
      color_material_fidelity: "pass",
      logo_text_legibility: "pass",
      text_policy_compliance: "pass",
      notes: null,
    });
    await expect(conflictRequest).rejects.toBeInstanceOf(ApiError);
    await expect(conflictRequest).rejects.toMatchObject({ status: 409, detail: "版本已变化" });
  });
});
