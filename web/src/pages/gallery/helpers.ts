import type { GalleryEntry } from "../../lib/types";

const DEFAULT_TILE_ASPECT_RATIO = 4 / 5;
const MIN_TILE_ASPECT_RATIO = 0.72;
const MAX_TILE_ASPECT_RATIO = 1.85;
const FEATURED_TILE_THRESHOLD = 24;
const GRID_ROW_UNIT_PX = 8;
const GRID_GAP_PX = 16;
const LG_GRID_COLUMNS = 12;
const GRID_CONTENT_WIDTH_PX = 1280;

export interface GalleryTileLayout {
  aspectRatio: string;
  className: string;
  rowSpan: number;
}

export function galleryEntrySizeLabel(entry: GalleryEntry): string {
  if (entry.actual_size && entry.size && entry.actual_size !== entry.size) {
    return `实际 ${entry.actual_size} · 请求 ${entry.size}`;
  }
  return entry.actual_size ?? entry.size ?? "尺寸未知";
}

export function selectGalleryEntry(entries: GalleryEntry[], selectedId: string | null): GalleryEntry | null {
  if (!entries.length) {
    return null;
  }
  return entries.find((entry) => entry.id === selectedId) ?? entries[0];
}

function parseImageSize(value: string | null): { width: number; height: number } | null {
  const match = value?.trim().match(/^(\d+)\s*x\s*(\d+)$/i);
  if (!match) {
    return null;
  }

  const width = Number(match[1]);
  const height = Number(match[2]);
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return null;
  }
  return { width, height };
}

function clampAspectRatio(value: number): number {
  return Math.min(MAX_TILE_ASPECT_RATIO, Math.max(MIN_TILE_ASPECT_RATIO, value));
}

export function galleryEntryAspectRatio(entry: Pick<GalleryEntry, "actual_size" | "size">): number {
  const parsedSize = parseImageSize(entry.actual_size) ?? parseImageSize(entry.size);
  if (!parsedSize) {
    return DEFAULT_TILE_ASPECT_RATIO;
  }
  return clampAspectRatio(parsedSize.width / parsedSize.height);
}

function stableTileScore(entry: Pick<GalleryEntry, "id">, index: number): number {
  let hash = 2166136261;
  for (const char of `${entry.id}:${index}`) {
    hash ^= char.charCodeAt(0);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0) % 100;
}

function tileWidthForColumnSpan(columnSpan: number, contentWidth: number): number {
  const usableContentWidth = Math.max(0, contentWidth - GRID_GAP_PX * (LG_GRID_COLUMNS - 1));
  const columnWidth = usableContentWidth / LG_GRID_COLUMNS;
  return columnWidth * columnSpan + GRID_GAP_PX * (columnSpan - 1);
}

function renderedGridHeight(rowSpan: number): number {
  return rowSpan * GRID_ROW_UNIT_PX + (rowSpan - 1) * GRID_GAP_PX;
}

function rowSpanForTileHeight(tileHeight: number): number {
  const exactRowSpan = (tileHeight + GRID_GAP_PX) / (GRID_ROW_UNIT_PX + GRID_GAP_PX);
  const lowerRowSpan = Math.max(8, Math.floor(exactRowSpan));
  const upperRowSpan = Math.max(8, Math.ceil(exactRowSpan));
  const lowerDelta = Math.abs(renderedGridHeight(lowerRowSpan) - tileHeight);
  const upperDelta = Math.abs(renderedGridHeight(upperRowSpan) - tileHeight);
  return lowerDelta <= upperDelta ? lowerRowSpan : upperRowSpan;
}

export function galleryTileLayout(
  entry: GalleryEntry,
  index: number,
  contentWidth: number = GRID_CONTENT_WIDTH_PX,
): GalleryTileLayout {
  const aspectRatio = galleryEntryAspectRatio(entry);
  const isFeatured = stableTileScore(entry, index) < FEATURED_TILE_THRESHOLD;
  const isPortrait = aspectRatio < 0.9;
  const columnSpan = isFeatured ? (isPortrait ? 3 : 4) : isPortrait ? 2 : 3;
  const baseClassName = isPortrait ? "lg:col-span-2" : "lg:col-span-3";
  const featuredClassName = isPortrait ? "sm:col-span-2 lg:col-span-3" : "sm:col-span-2 lg:col-span-4";
  const estimatedTileWidth = tileWidthForColumnSpan(columnSpan, contentWidth);
  const estimatedTileHeight = estimatedTileWidth / aspectRatio;

  return {
    aspectRatio: aspectRatio.toFixed(4),
    className: isFeatured ? featuredClassName : baseClassName,
    rowSpan: rowSpanForTileHeight(estimatedTileHeight),
  };
}
