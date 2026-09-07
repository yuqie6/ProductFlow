import type { QuotaPriceVersion } from "./types";

/** Catalog entry codes aligned with go/internal/quota.Entry* (display lookup only). */
export const QUOTA_ENTRY_IMAGE_SESSION_GENERATE = "image_session.generate";

export type QuotaEntryPriceLookup =
  | {
      status: "ok";
      priceVersionId: string;
      entryCode: string;
      unitPrice: number;
      estimatedUnits: number;
      currency: string;
    }
  | { status: "invalid_version" }
  | { status: "missing_entry" }
  | { status: "invalid_price" };

/**
 * Resolve unit price / estimated debit for one paid action from a price-version DTO.
 * Missing version, missing entry, or non-positive unit_price → explicit failure (never 0).
 */
export function resolveQuotaEntryPrice(
  version: QuotaPriceVersion | null | undefined,
  entryCode: string,
): QuotaEntryPriceLookup {
  const code = entryCode.trim();
  const versionId = version?.price_version_id?.trim() ?? "";
  if (!version || !versionId || !code) {
    return { status: "invalid_version" };
  }
  const entry = (version.entries ?? []).find((item) => item.entry_code === code);
  if (!entry) {
    return { status: "missing_entry" };
  }
  const unitPrice = entry.unit_price;
  if (!Number.isFinite(unitPrice) || unitPrice <= 0) {
    return { status: "invalid_price" };
  }
  return {
    status: "ok",
    priceVersionId: versionId,
    entryCode: code,
    unitPrice,
    estimatedUnits: unitPrice,
    currency: version.currency?.trim() || "iu",
  };
}

export function isQuotaEntryPriceReady(lookup: QuotaEntryPriceLookup): boolean {
  return lookup.status === "ok";
}
