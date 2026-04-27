export interface ImageSizeOption {
  value: string;
  label: string;
  description: string;
  aspect: string;
}

export interface ImageSizeResolution {
  width: number;
  height: number;
  value: string;
  calibrated: boolean;
}

export interface ImageSizePresetDisplay {
  aspectLabel: string;
  tierLabel: string;
  dimensionLabel: string;
}

export const IMAGE_SIZE_PATTERN = /^\d+x\d+$/;
export const DEFAULT_IMAGE_GENERATION_MAX_DIMENSION = 3840;
export const IMAGE_GENERATION_MIN_MAX_DIMENSION = 512;
export const IMAGE_GENERATION_MAX_MAX_DIMENSION = 8192;
export const IMAGE_GENERATION_MAX_DIMENSION = DEFAULT_IMAGE_GENERATION_MAX_DIMENSION;
export const IMAGE_GENERATION_MAX_PIXELS = DEFAULT_IMAGE_GENERATION_MAX_DIMENSION * DEFAULT_IMAGE_GENERATION_MAX_DIMENSION;

const BUILT_IN_IMAGE_SIZE_OPTIONS: ImageSizeOption[] = [
  { label: "方图 · 1K", description: "1:1 · 1024×1024", aspect: "1:1", value: "1024x1024" },
  { label: "竖图 · 1K", description: "2:3 · 1024×1536", aspect: "2:3", value: "1024x1536" },
  { label: "横图 · 1K", description: "3:2 · 1536×1024", aspect: "3:2", value: "1536x1024" },
  { label: "方图 · 2K", description: "1:1 · 2048×2048", aspect: "1:1", value: "2048x2048" },
  { label: "竖图 · 2K", description: "2:3 · 2048×3072", aspect: "2:3", value: "2048x3072" },
  { label: "横图 · 2K", description: "3:2 · 3072×2048", aspect: "3:2", value: "3072x2048" },
  { label: "方图 · 4K", description: "1:1 · 3840×3840", aspect: "1:1", value: "3840x3840" },
  { label: "竖图 · 4K", description: "9:16 · 2160×3840", aspect: "9:16", value: "2160x3840" },
  { label: "横图 · 4K", description: "16:9 · 3840×2160", aspect: "16:9", value: "3840x2160" },
];

const PRESET_LABELS = new Map(BUILT_IN_IMAGE_SIZE_OPTIONS.map((option) => [option.value, option.label]));

function normalizeMaxDimension(maxDimension?: number): number {
  if (!Number.isFinite(maxDimension)) {
    return DEFAULT_IMAGE_GENERATION_MAX_DIMENSION;
  }
  const rounded = Math.round(maxDimension ?? DEFAULT_IMAGE_GENERATION_MAX_DIMENSION);
  return Math.min(IMAGE_GENERATION_MAX_MAX_DIMENSION, Math.max(IMAGE_GENERATION_MIN_MAX_DIMENSION, rounded));
}

export function normalizeImageSizeValue(value: string, maxDimension?: number): string | null {
  const normalized = value.trim().toLowerCase();
  return IMAGE_SIZE_PATTERN.test(normalized) ? normalizeImageSizeDimensions(normalized, maxDimension) : null;
}

export function parseImageSizeValue(value: string, maxDimension?: number): { width: number; height: number } | null {
  const normalized = normalizeImageSizeValue(value, maxDimension);
  if (!normalized) {
    return null;
  }
  const [width, height] = normalized.split("x", 2).map(Number);
  if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height) || width <= 0 || height <= 0) {
    return null;
  }
  return { width, height };
}

export function resolveImageSize(width: number, height: number, maxDimension?: number): ImageSizeResolution | null {
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return null;
  }
  const requestedWidth = Math.round(width);
  const requestedHeight = Math.round(height);
  if (requestedWidth <= 0 || requestedHeight <= 0) {
    return null;
  }
  const resolvedMaxDimension = normalizeMaxDimension(maxDimension);
  const maxPixels = resolvedMaxDimension * resolvedMaxDimension;

  let scale = Math.min(1, resolvedMaxDimension / requestedWidth, resolvedMaxDimension / requestedHeight);
  const dimensionCalibrated = scale < 1;
  let resolvedWidth = Math.min(resolvedMaxDimension, Math.max(1, Math.round(requestedWidth * scale)));
  let resolvedHeight = Math.min(resolvedMaxDimension, Math.max(1, Math.round(requestedHeight * scale)));

  const resolvedPixels = resolvedWidth * resolvedHeight;
  let pixelCalibrated = false;
  if (resolvedPixels > maxPixels) {
    scale = Math.sqrt(maxPixels / resolvedPixels);
    resolvedWidth = Math.max(1, Math.floor(resolvedWidth * scale));
    resolvedHeight = Math.max(1, Math.floor(resolvedHeight * scale));
    pixelCalibrated = true;
  }

  const value = `${resolvedWidth}x${resolvedHeight}`;
  return {
    width: resolvedWidth,
    height: resolvedHeight,
    value,
    calibrated: dimensionCalibrated || pixelCalibrated || value !== `${requestedWidth}x${requestedHeight}`,
  };
}

export function normalizeImageSizeDimensions(value: string, maxDimension?: number): string | null {
  const [widthRaw, heightRaw] = value.trim().toLowerCase().split("x", 2);
  if (!/^\d+$/.test(widthRaw) || !/^\d+$/.test(heightRaw)) {
    return null;
  }
  const resolution = resolveImageSize(Number(widthRaw), Number(heightRaw), maxDimension);
  return resolution?.value ?? null;
}

export function buildImageSizeOptions(maxDimension?: number): ImageSizeOption[] {
  return BUILT_IN_IMAGE_SIZE_OPTIONS.filter((option) => {
    const parsed = parseImageSizeValue(option.value, DEFAULT_IMAGE_GENERATION_MAX_DIMENSION);
    if (!parsed) {
      return false;
    }
    const resolved = resolveImageSize(parsed.width, parsed.height, maxDimension);
    return resolved?.value === option.value && !resolved.calibrated;
  });
}

export const DEFAULT_IMAGE_SIZE_OPTIONS: ImageSizeOption[] = buildImageSizeOptions(DEFAULT_IMAGE_GENERATION_MAX_DIMENSION);

export function formatImageSizeValue(value: string): string {
  return value.replace("x", "×");
}

export function labelForImageSize(value: string): string {
  return PRESET_LABELS.get(value) ?? `自定义 · ${formatImageSizeValue(value)}`;
}

export function getImageSizePresetDisplay(option: ImageSizeOption): ImageSizePresetDisplay {
  const [, tier] = option.label.split("·", 2);
  return {
    aspectLabel: option.aspect,
    tierLabel: tier?.trim() || option.label,
    dimensionLabel: formatImageSizeValue(option.value),
  };
}
