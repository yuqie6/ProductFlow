/**
 * 保真检查线上辅助。四项结论独立记录；一项 pass 不代表其余项也通过。
 */

import type {
  CreateProductImageFidelityCheckInput,
  ProductImageFidelityCheck,
  ProductImageFidelityCheckListResponse,
  ProductImageFidelityOutcome,
} from "./types";

const OUTCOMES = new Set<ProductImageFidelityOutcome>(["pass", "fail", "not_applicable"]);
const CHECK_KEYS = new Set([
  "id",
  "product_id",
  "asset_id",
  "version",
  "shape_fidelity",
  "color_material_fidelity",
  "logo_text_legibility",
  "text_policy_compliance",
  "notes",
  "checked_by",
  "idempotency_key",
  "request_hash",
  "created_at",
]);
const LIST_KEYS = new Set(["product_id", "asset_id", "latest_version", "items"]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExactKeys(value: Record<string, unknown>, expected: Set<string>): boolean {
  const actual = Object.keys(value);
  return actual.length === expected.size && actual.every((key) => expected.has(key));
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function isNonNegativeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

function isPositiveInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 1;
}

function isTimestamp(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0 && !Number.isNaN(Date.parse(value));
}

function isOutcome(value: unknown): value is ProductImageFidelityOutcome {
  return typeof value === "string" && OUTCOMES.has(value as ProductImageFidelityOutcome);
}

export function parseProductImageFidelityCheck(value: unknown): ProductImageFidelityCheck | null {
  if (!isRecord(value) || !hasExactKeys(value, CHECK_KEYS)) return null;
  if (
    !isNonEmptyString(value.id)
    || !isNonEmptyString(value.product_id)
    || !isNonEmptyString(value.asset_id)
    || !isPositiveInteger(value.version)
    || !isOutcome(value.shape_fidelity)
    || !isOutcome(value.color_material_fidelity)
    || !isOutcome(value.logo_text_legibility)
    || !isOutcome(value.text_policy_compliance)
    || (value.notes !== null && typeof value.notes !== "string")
    || !isNonEmptyString(value.checked_by)
    || !isNonEmptyString(value.idempotency_key)
    || value.idempotency_key.length > 120
    || typeof value.request_hash !== "string"
    || !/^[a-f0-9]{64}$/.test(value.request_hash)
    || !isTimestamp(value.created_at)
  ) {
    return null;
  }
  return {
    id: value.id,
    product_id: value.product_id,
    asset_id: value.asset_id,
    version: value.version,
    shape_fidelity: value.shape_fidelity,
    color_material_fidelity: value.color_material_fidelity,
    logo_text_legibility: value.logo_text_legibility,
    text_policy_compliance: value.text_policy_compliance,
    notes: value.notes as string | null,
    checked_by: value.checked_by,
    idempotency_key: value.idempotency_key,
    request_hash: value.request_hash,
    created_at: value.created_at,
  };
}

export function parseProductImageFidelityCheckList(
  value: unknown,
): ProductImageFidelityCheckListResponse | null {
  if (!isRecord(value) || !hasExactKeys(value, LIST_KEYS) || !Array.isArray(value.items)) return null;
  if (!isNonEmptyString(value.product_id) || !isNonEmptyString(value.asset_id) || !isNonNegativeInteger(value.latest_version)) {
    return null;
  }
  const items = value.items.map(parseProductImageFidelityCheck);
  if (items.some((item): item is null => item === null)) return null;
  const parsedItems = items as ProductImageFidelityCheck[];
  if (
    parsedItems.some((item) => item.product_id !== value.product_id || item.asset_id !== value.asset_id)
    || new Set(parsedItems.map((item) => item.version)).size !== parsedItems.length
    || parsedItems.some((item, index) => index > 0 && parsedItems[index - 1]!.version <= item.version)
    || (parsedItems.length > 0 && parsedItems[0]!.version !== value.latest_version)
    || (parsedItems.length === 0 && value.latest_version !== 0)
  ) {
    return null;
  }
  return {
    product_id: value.product_id,
    asset_id: value.asset_id,
    latest_version: value.latest_version,
    items: parsedItems,
  };
}

export function normalizeImageFidelityNotes(notes: string | null | undefined): string {
  return notes?.trim() ?? "";
}

function hashString(value: string, seed: number): string {
  let hash = seed >>> 0;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(16).padStart(8, "0");
}

export function imageFidelityRequestFingerprint(
  assetId: string,
  input: Pick<
    CreateProductImageFidelityCheckInput,
    | "expected_latest_version"
    | "shape_fidelity"
    | "color_material_fidelity"
    | "logo_text_legibility"
    | "text_policy_compliance"
    | "notes"
  >,
): string {
  return JSON.stringify([
    assetId,
    input.expected_latest_version,
    input.shape_fidelity,
    input.color_material_fidelity,
    input.logo_text_legibility,
    input.text_policy_compliance,
    normalizeImageFidelityNotes(input.notes),
  ]);
}

export function createImageFidelityIdempotencyKey(
  assetId: string,
  input: Pick<
    CreateProductImageFidelityCheckInput,
    | "expected_latest_version"
    | "shape_fidelity"
    | "color_material_fidelity"
    | "logo_text_legibility"
    | "text_policy_compliance"
    | "notes"
  >,
): string {
  const fingerprint = imageFidelityRequestFingerprint(assetId, input);
  return `fidelity-${hashString(fingerprint, 0x811c9dc5)}${hashString(fingerprint, 0x9e3779b9)}`;
}
