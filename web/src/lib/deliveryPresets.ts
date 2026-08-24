/**
 * DeliverySpec 与市场预设解析。
 *
 * 交付是确定性派生。预设不能代替 GenerationSpec，也不会触发新的图像模型调用。
 */

import type { DeliveryPreset, DeliveryPresetCatalog, WorkflowDeliverySpec } from "./types";

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
const DELIVERY_SPEC_KEYS = new Set([
  "width",
  "height",
  "format",
  "max_byte_size",
  "fit",
  "background_color",
  "crop_anchor",
]);
const DELIVERY_PRESET_KEYS = [
  "taobao_tmall_hero",
  "jd_hero",
  "amazon_hero",
  "detail_portrait",
  "scene_landscape",
] as const;
const DELIVERY_PRESET_FIELDS = new Set([
  "key",
  "title",
  "aspect_ratio",
  "applicable_image_type",
  "reviewed_at",
  "source",
  "disclaimer",
  "delivery_spec",
]);
const DELIVERY_PRESET_CATALOG_FIELDS = new Set(["supports_custom", "items"]);

export function parseWorkflowDeliverySpec(value: unknown): WorkflowDeliverySpec | null {
  if (!isRecord(value)) return null;
  if (!hasNoUnknownKeys(value, DELIVERY_SPEC_KEYS)) return null;
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

export function parseDeliveryPresetCatalog(value: unknown): DeliveryPresetCatalog | null {
  if (!isRecord(value) || !hasOnlyKeys(value, DELIVERY_PRESET_CATALOG_FIELDS)) return null;
  if (value.supports_custom !== true || !Array.isArray(value.items)) return null;
  if (value.items.length !== DELIVERY_PRESET_KEYS.length) return null;

  const allowedKeys = new Set<string>(DELIVERY_PRESET_KEYS);
  const seenKeys = new Set<string>();
  const items: DeliveryPreset[] = [];
  for (const [index, item] of value.items.entries()) {
    if (!isRecord(item) || !hasOnlyKeys(item, DELIVERY_PRESET_FIELDS)) return null;
    const key = item.key;
    const title = item.title;
    const aspectRatio = item.aspect_ratio;
    const applicableImageType = item.applicable_image_type;
    const reviewedAt = item.reviewed_at;
    const source = item.source;
    const disclaimer = item.disclaimer;
    const deliverySpec = parseWorkflowDeliverySpec(item.delivery_spec);
    if (
      typeof key !== "string"
      || key !== DELIVERY_PRESET_KEYS[index]
      || !allowedKeys.has(key)
      || seenKeys.has(key)
      || typeof title !== "string"
      || !title.trim()
      || typeof aspectRatio !== "string"
      || typeof applicableImageType !== "string"
      || !applicableImageType.trim()
      || typeof reviewedAt !== "string"
      || !/^\d{4}-\d{2}-\d{2}$/.test(reviewedAt)
      || typeof source !== "string"
      || !source.trim()
      || typeof disclaimer !== "string"
      || !disclaimer.trim()
      || !deliverySpec
      || reducedAspectRatio(deliverySpec.width, deliverySpec.height) !== aspectRatio
    ) {
      return null;
    }
    seenKeys.add(key);
    items.push({
      key,
      title,
      aspect_ratio: aspectRatio,
      applicable_image_type: applicableImageType,
      reviewed_at: reviewedAt,
      source,
      disclaimer,
      delivery_spec: deliverySpec,
    });
  }
  if (seenKeys.size !== allowedKeys.size) return null;
  return { supports_custom: true, items };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasOnlyKeys(value: Record<string, unknown>, allowed: Set<string>): boolean {
  const keys = Object.keys(value);
  return keys.length === allowed.size && keys.every((key) => allowed.has(key));
}

function hasNoUnknownKeys(value: Record<string, unknown>, allowed: Set<string>): boolean {
  return Object.keys(value).every((key) => allowed.has(key));
}

function reducedAspectRatio(width: number, height: number): string {
  let left = width;
  let right = height;
  while (right !== 0) {
    const remainder = left % right;
    left = right;
    right = remainder;
  }
  return `${width / left}:${height / left}`;
}
