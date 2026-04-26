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

export const IMAGE_SIZE_PATTERN = /^\d+x\d+$/;
export const IMAGE_GENERATION_MAX_DIMENSION = 3840;
export const IMAGE_GENERATION_MAX_PIXELS = 3840 * 3840;

export const DEFAULT_IMAGE_SIZE_OPTIONS: ImageSizeOption[] = [
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

const PRESET_LABELS = new Map(DEFAULT_IMAGE_SIZE_OPTIONS.map((option) => [option.value, option.label]));

export function normalizeImageSizeValue(value: string): string | null {
  const normalized = value.trim().toLowerCase();
  return IMAGE_SIZE_PATTERN.test(normalized) ? normalizeImageSizeDimensions(normalized) : null;
}

export function parseImageSizeValue(value: string): { width: number; height: number } | null {
  const normalized = normalizeImageSizeValue(value);
  if (!normalized) {
    return null;
  }
  const [width, height] = normalized.split("x", 2).map(Number);
  if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height) || width <= 0 || height <= 0) {
    return null;
  }
  return { width, height };
}

export function resolveImageSize(width: number, height: number): ImageSizeResolution | null {
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return null;
  }
  const requestedWidth = Math.round(width);
  const requestedHeight = Math.round(height);
  if (requestedWidth <= 0 || requestedHeight <= 0) {
    return null;
  }

  let scale = Math.min(1, IMAGE_GENERATION_MAX_DIMENSION / requestedWidth, IMAGE_GENERATION_MAX_DIMENSION / requestedHeight);
  const dimensionCalibrated = scale < 1;
  let resolvedWidth = Math.min(IMAGE_GENERATION_MAX_DIMENSION, Math.max(1, Math.round(requestedWidth * scale)));
  let resolvedHeight = Math.min(IMAGE_GENERATION_MAX_DIMENSION, Math.max(1, Math.round(requestedHeight * scale)));

  const resolvedPixels = resolvedWidth * resolvedHeight;
  let pixelCalibrated = false;
  if (resolvedPixels > IMAGE_GENERATION_MAX_PIXELS) {
    scale = Math.sqrt(IMAGE_GENERATION_MAX_PIXELS / resolvedPixels);
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

export function normalizeImageSizeDimensions(value: string): string | null {
  const [widthRaw, heightRaw] = value.trim().toLowerCase().split("x", 2);
  if (!/^\d+$/.test(widthRaw) || !/^\d+$/.test(heightRaw)) {
    return null;
  }
  const resolution = resolveImageSize(Number(widthRaw), Number(heightRaw));
  return resolution?.value ?? null;
}

export function formatImageSizeValue(value: string): string {
  return value.replace("x", "×");
}

export function labelForImageSize(value: string): string {
  return PRESET_LABELS.get(value) ?? `自定义 · ${formatImageSizeValue(value)}`;
}
