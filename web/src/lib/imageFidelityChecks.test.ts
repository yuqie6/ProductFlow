import { describe, expect, it } from "vitest";

import type { ProductImageFidelityCheck } from "./types";
import {
  createImageFidelityIdempotencyKey,
  imageFidelityRequestFingerprint,
  normalizeImageFidelityNotes,
  parseProductImageFidelityCheck,
  parseProductImageFidelityCheckList,
} from "./imageFidelityChecks";

function check(overrides: Partial<ProductImageFidelityCheck> = {}): ProductImageFidelityCheck {
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
    ...overrides,
  };
}

describe("image fidelity response contracts", () => {
  it("accepts the exact record and newest-first list shape", () => {
    const newest = check({ version: 2, id: "check-2" });
    const oldest = check({ version: 1 });
    const payload = {
      product_id: "product-1",
      asset_id: "asset-1",
      latest_version: 2,
      items: [newest, oldest],
    };

    expect(parseProductImageFidelityCheck(newest)).toEqual(newest);
    expect(parseProductImageFidelityCheckList(payload)).toEqual(payload);
    expect(parseProductImageFidelityCheckList({
      product_id: "product-1",
      asset_id: "asset-1",
      latest_version: 0,
      items: [],
    })).toEqual({
      product_id: "product-1",
      asset_id: "asset-1",
      latest_version: 0,
      items: [],
    });
  });

  it("fails closed on unknown fields, wrong scope, invalid outcomes, and non-newest lists", () => {
    expect(parseProductImageFidelityCheck({ ...check(), unexpected: true })).toBeNull();
    expect(parseProductImageFidelityCheck({ ...check(), shape_fidelity: "unknown" })).toBeNull();
    expect(parseProductImageFidelityCheck({ ...check(), version: 0 })).toBeNull();

    const newest = check({ version: 2, id: "check-2" });
    expect(parseProductImageFidelityCheckList({
      product_id: "product-1",
      asset_id: "asset-1",
      latest_version: 2,
      items: [{ ...newest, product_id: "other-product" }, check()],
    })).toBeNull();
    expect(parseProductImageFidelityCheckList({
      product_id: "product-1",
      asset_id: "asset-1",
      latest_version: 2,
      items: [check(), newest],
    })).toBeNull();
  });
});

describe("image fidelity idempotency identity", () => {
  const input = {
    expected_latest_version: 3,
    shape_fidelity: "pass" as const,
    color_material_fidelity: "fail" as const,
    logo_text_legibility: "not_applicable" as const,
    text_policy_compliance: "pass" as const,
    notes: "  保留细节  ",
  };

  it("normalizes notes and reuses the same semantic key", () => {
    const equivalent = { ...input, notes: "保留细节" };
    expect(normalizeImageFidelityNotes(input.notes)).toBe("保留细节");
    expect(imageFidelityRequestFingerprint("asset-1", input)).toBe(
      imageFidelityRequestFingerprint("asset-1", equivalent),
    );
    expect(createImageFidelityIdempotencyKey("asset-1", input)).toMatch(/^fidelity-[0-9a-f]{16}$/);
    expect(createImageFidelityIdempotencyKey("asset-1", input)).toBe(
      createImageFidelityIdempotencyKey("asset-1", equivalent),
    );
  });

  it("changes identity when asset, version, outcome, or notes changes", () => {
    const key = createImageFidelityIdempotencyKey("asset-1", input);
    expect(createImageFidelityIdempotencyKey("asset-2", input)).not.toBe(key);
    expect(createImageFidelityIdempotencyKey("asset-1", { ...input, expected_latest_version: 4 })).not.toBe(key);
    expect(createImageFidelityIdempotencyKey("asset-1", { ...input, shape_fidelity: "fail" })).not.toBe(key);
    expect(createImageFidelityIdempotencyKey("asset-1", { ...input, notes: "不同备注" })).not.toBe(key);
  });
});
