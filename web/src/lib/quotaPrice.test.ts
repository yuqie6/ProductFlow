import { describe, expect, it } from "vitest";

import type { QuotaPriceVersion } from "./types";
import {
  QUOTA_ENTRY_IMAGE_SESSION_GENERATE,
  isQuotaEntryPriceReady,
  resolveQuotaEntryPrice,
} from "./quotaPrice";

function version(overrides: Partial<QuotaPriceVersion> = {}): QuotaPriceVersion {
  return {
    price_version_id: "pv-placeholder-v0",
    label: "默认内部单价 v0",
    currency: "iu",
    is_default: true,
    entries: [
      { entry_code: QUOTA_ENTRY_IMAGE_SESSION_GENERATE, unit_price: 1 },
      { entry_code: "graph.image_generation", unit_price: 2 },
    ],
    ...overrides,
  };
}

describe("resolveQuotaEntryPrice", () => {
  it("reads catalog unit price for image_session.generate (no hardcode)", () => {
    const lookup = resolveQuotaEntryPrice(version({ entries: [{ entry_code: QUOTA_ENTRY_IMAGE_SESSION_GENERATE, unit_price: 7 }] }), QUOTA_ENTRY_IMAGE_SESSION_GENERATE);
    expect(lookup).toEqual({
      status: "ok",
      priceVersionId: "pv-placeholder-v0",
      entryCode: QUOTA_ENTRY_IMAGE_SESSION_GENERATE,
      unitPrice: 7,
      estimatedUnits: 7,
      currency: "iu",
    });
    expect(isQuotaEntryPriceReady(lookup)).toBe(true);
  });

  it("fails explicitly when version id is missing", () => {
    expect(resolveQuotaEntryPrice(version({ price_version_id: "  " }), QUOTA_ENTRY_IMAGE_SESSION_GENERATE)).toEqual({
      status: "invalid_version",
    });
    expect(resolveQuotaEntryPrice(null, QUOTA_ENTRY_IMAGE_SESSION_GENERATE).status).toBe("invalid_version");
    expect(resolveQuotaEntryPrice(undefined, QUOTA_ENTRY_IMAGE_SESSION_GENERATE).status).toBe("invalid_version");
  });

  it("fails explicitly when entry is missing (not silent 0)", () => {
    const lookup = resolveQuotaEntryPrice(version({ entries: [{ entry_code: "other", unit_price: 1 }] }), QUOTA_ENTRY_IMAGE_SESSION_GENERATE);
    expect(lookup).toEqual({ status: "missing_entry" });
    expect(isQuotaEntryPriceReady(lookup)).toBe(false);
  });

  it("fails explicitly when unit_price is zero or invalid", () => {
    expect(
      resolveQuotaEntryPrice(
        version({ entries: [{ entry_code: QUOTA_ENTRY_IMAGE_SESSION_GENERATE, unit_price: 0 }] }),
        QUOTA_ENTRY_IMAGE_SESSION_GENERATE,
      ),
    ).toEqual({ status: "invalid_price" });
    expect(
      resolveQuotaEntryPrice(
        version({ entries: [{ entry_code: QUOTA_ENTRY_IMAGE_SESSION_GENERATE, unit_price: -3 }] }),
        QUOTA_ENTRY_IMAGE_SESSION_GENERATE,
      ),
    ).toEqual({ status: "invalid_price" });
    expect(
      resolveQuotaEntryPrice(
        version({ entries: [{ entry_code: QUOTA_ENTRY_IMAGE_SESSION_GENERATE, unit_price: Number.NaN }] }),
        QUOTA_ENTRY_IMAGE_SESSION_GENERATE,
      ),
    ).toEqual({ status: "invalid_price" });
  });
});
