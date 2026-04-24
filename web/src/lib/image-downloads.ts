import { api } from "./api";

export type DownloadableImage = {
  previewUrl: string;
  downloadUrl: string;
  filename: string;
  alt: string;
};

export function sanitizeFilenamePart(
  value: string | null | undefined,
  fallback: string,
): string {
  const cleaned = (value ?? fallback)
    .trim()
    .replace(/[\u0000-\u001f\u007f]+/g, "")
    .replace(/[\\/:*?"<>|]+/g, "-")
    .replace(/\s+/g, "-")
    .replace(/\.+/g, ".")
    .replace(/^-+|-+$/g, "");
  return (cleaned && cleaned !== "." && cleaned !== ".." ? cleaned : fallback)
    .slice(0, 80)
    .replace(/\.+$/g, "");
}

export function toImageUrl(...paths: Array<string | null | undefined>): string {
  const path = paths.find((item) => typeof item === "string" && item.trim());
  return api.toApiUrl(path ?? "/");
}

export function getExtensionFromMime(mimeType: string | null | undefined): string {
  if (mimeType === "image/jpeg") {
    return ".jpg";
  }
  if (mimeType === "image/webp") {
    return ".webp";
  }
  return ".png";
}

export function getExtensionFromFilename(
  filename: string | null | undefined,
  mimeType?: string | null,
): string {
  const match = filename?.match(/\.[a-z0-9]+$/i);
  return match ? match[0].toLowerCase() : getExtensionFromMime(mimeType);
}

export function compactDateTime(value: string): string {
  return value.replace(/[^0-9]/g, "").slice(0, 14) || "image";
}
