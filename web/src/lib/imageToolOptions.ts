import type { ImageToolOptionKey, ImageToolOptions } from "./types";

export const IMAGE_TOOL_FIELD_KEYS: readonly ImageToolOptionKey[] = [
  "model",
  "quality",
  "output_format",
  "output_compression",
  "background",
  "moderation",
  "action",
  "input_fidelity",
  "partial_images",
  "n",
];

export const DEFAULT_IMAGE_TOOL_ALLOWED_FIELDS = IMAGE_TOOL_FIELD_KEYS.filter((key) => key !== "background");

function imageToolAllowedSet(allowedFields: readonly ImageToolOptionKey[]): Set<ImageToolOptionKey> {
  return new Set(allowedFields);
}

function compactString(value: string | null | undefined): string | undefined {
  const normalized = value?.trim();
  return normalized || undefined;
}

function clampOptionalInteger(
  value: number | null | undefined,
  minimum: number,
  maximum: number,
): number | undefined {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return undefined;
  }
  return Math.min(maximum, Math.max(minimum, Math.round(value)));
}

export function compactImageToolOptions(
  options: ImageToolOptions,
  allowedFields: readonly ImageToolOptionKey[] = DEFAULT_IMAGE_TOOL_ALLOWED_FIELDS,
): ImageToolOptions | undefined {
  const allowed = imageToolAllowedSet(allowedFields);
  const compacted: ImageToolOptions = {
    model: compactString(options.model),
    quality: options.quality || undefined,
    output_format: options.output_format || undefined,
    output_compression: clampOptionalInteger(options.output_compression, 0, 100),
    background: options.background || undefined,
    moderation: options.moderation || undefined,
    action: options.action || undefined,
    input_fidelity: options.input_fidelity || undefined,
    partial_images: clampOptionalInteger(options.partial_images, 0, 3),
    n: clampOptionalInteger(options.n, 1, 10),
  };
  const entries = Object.entries(compacted).filter(
    ([key, value]) => allowed.has(key as ImageToolOptionKey) && value !== undefined,
  );
  return entries.length ? (Object.fromEntries(entries) as ImageToolOptions) : undefined;
}

function stringOption(value: unknown): string | null {
  return typeof value === "string" ? value : null;
}

function numberOption(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

export function imageToolOptionsFromUnknown(
  value: unknown,
  allowedFields: readonly ImageToolOptionKey[] = IMAGE_TOOL_FIELD_KEYS,
): ImageToolOptions {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  const record = value as Record<string, unknown>;
  const allowed = imageToolAllowedSet(allowedFields);
  const parsed: ImageToolOptions = {
    model: stringOption(record.model),
    quality: stringOption(record.quality) as ImageToolOptions["quality"],
    output_format: stringOption(record.output_format) as ImageToolOptions["output_format"],
    output_compression: numberOption(record.output_compression),
    background: stringOption(record.background) as ImageToolOptions["background"],
    moderation: stringOption(record.moderation) as ImageToolOptions["moderation"],
    action: stringOption(record.action) as ImageToolOptions["action"],
    input_fidelity: stringOption(record.input_fidelity) as ImageToolOptions["input_fidelity"],
    partial_images: numberOption(record.partial_images),
    n: numberOption(record.n),
  };
  const entries = Object.entries(parsed).filter(([key]) => allowed.has(key as ImageToolOptionKey));
  return Object.fromEntries(entries) as ImageToolOptions;
}
