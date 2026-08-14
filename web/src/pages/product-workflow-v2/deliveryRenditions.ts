import type { WorkflowDeliverySpec } from "../../lib/types";

const MAX_TOTAL_PIXELS = 64 * 1024 * 1024;
const FORMATS = new Set<WorkflowDeliverySpec["format"]>(["png", "jpeg", "webp"]);
const FITS = new Set<WorkflowDeliverySpec["fit"]>(["contain", "cover"]);
const ANCHORS = new Set<NonNullable<WorkflowDeliverySpec["crop_anchor"]>>([
  "center",
  "top",
  "bottom",
  "left",
  "right",
]);

export function parseWorkflowDeliverySpec(value: unknown): WorkflowDeliverySpec | null {
  if (!isRecord(value)) return null;
  const width = value.width;
  const height = value.height;
  const format = value.format;
  const fit = value.fit;
  const maxByteSize = value.max_byte_size;
  const backgroundColor = value.background_color;
  const cropAnchor = value.crop_anchor;
  if (
    !Number.isInteger(width)
    || !Number.isInteger(height)
    || (width as number) < 1
    || (height as number) < 1
    || (width as number) > 16384
    || (height as number) > 16384
    || (width as number) * (height as number) > MAX_TOTAL_PIXELS
    || typeof format !== "string"
    || !FORMATS.has(format as WorkflowDeliverySpec["format"])
    || typeof fit !== "string"
    || !FITS.has(fit as WorkflowDeliverySpec["fit"])
  ) {
    return null;
  }
  if (
    maxByteSize !== undefined
    && maxByteSize !== null
    && (typeof maxByteSize !== "number" || !Number.isInteger(maxByteSize) || maxByteSize < 1)
  ) {
    return null;
  }
  if (
    backgroundColor !== undefined
    && backgroundColor !== null
    && (typeof backgroundColor !== "string" || !/^#[0-9A-Fa-f]{6}$/.test(backgroundColor))
  ) {
    return null;
  }
  if (
    cropAnchor !== undefined
    && cropAnchor !== null
    && (typeof cropAnchor !== "string" || !ANCHORS.has(cropAnchor as NonNullable<WorkflowDeliverySpec["crop_anchor"]>))
  ) {
    return null;
  }
  if (fit === "contain" && cropAnchor != null) return null;
  if (fit === "cover" && backgroundColor != null) return null;
  return {
    width: width as number,
    height: height as number,
    format: format as WorkflowDeliverySpec["format"],
    max_byte_size: typeof maxByteSize === "number" ? maxByteSize : null,
    fit: fit as WorkflowDeliverySpec["fit"],
    background_color: typeof backgroundColor === "string" ? backgroundColor : null,
    crop_anchor: typeof cropAnchor === "string"
      ? cropAnchor as NonNullable<WorkflowDeliverySpec["crop_anchor"]>
      : null,
  };
}

export function deliverySpecLabel(spec: WorkflowDeliverySpec): string {
  return `${spec.width} x ${spec.height} ${spec.format.toUpperCase()}`;
}

export function deliverySpecKey(spec: WorkflowDeliverySpec): string {
  return JSON.stringify({
    width: spec.width,
    height: spec.height,
    format: spec.format,
    max_byte_size: spec.max_byte_size ?? null,
    fit: spec.fit,
    background_color: spec.background_color ?? null,
    crop_anchor: spec.crop_anchor ?? null,
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
