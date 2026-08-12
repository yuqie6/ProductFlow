import type { GalleryAsset, GalleryAssetPage, GalleryAssetSort, GalleryDirectorySelection } from "../../../lib/types";

export type ImageExplorerView = "grid" | "list";

export const IMAGE_EXPLORER_WIDE_MIN_PX = 440;
export const IMAGE_EXPLORER_PAGE_SIZE = 50;
export const IMAGE_EXPLORER_MAX_SELECTION = 100;
export const IMAGE_EXPLORER_DRAG_MIME = "application/x-productflow-image-asset-ids";

export const DEFAULT_GALLERY_DIRECTORY: GalleryDirectorySelection = { kind: "all", key: null };

export function imageExplorerViewStorageKey(productId: string): string {
  return `productflow.product.${productId}.imageExplorer.view`;
}

export function readImageExplorerView(productId: string): ImageExplorerView {
  if (typeof window === "undefined") {
    return "grid";
  }
  return window.localStorage.getItem(imageExplorerViewStorageKey(productId)) === "list" ? "list" : "grid";
}

export function writeImageExplorerView(productId: string, view: ImageExplorerView): void {
  if (typeof window !== "undefined") {
    window.localStorage.setItem(imageExplorerViewStorageKey(productId), view);
  }
}

export function isWideImageExplorer(width: number): boolean {
  return width >= IMAGE_EXPLORER_WIDE_MIN_PX;
}

export function toggleAssetSelection(selected: Set<string>, assetId: string): Set<string> {
  const next = new Set(selected);
  if (next.has(assetId)) {
    next.delete(assetId);
  } else if (next.size < IMAGE_EXPLORER_MAX_SELECTION) {
    next.add(assetId);
  }
  return next;
}

export function selectLoadedAssets(assets: GalleryAsset[]): Set<string> {
  return new Set(assets.slice(0, IMAGE_EXPLORER_MAX_SELECTION).map((asset) => asset.id));
}

export function formatAssetByteSize(value: number | null): string {
  if (value === null || value < 0) {
    return "--";
  }
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(value < 10 * 1024 ? 1 : 0)} KiB`;
  }
  return `${(value / (1024 * 1024)).toFixed(value < 10 * 1024 * 1024 ? 1 : 0)} MiB`;
}

export function assetPixelSize(asset: GalleryAsset): string {
  return asset.width && asset.height ? `${asset.width} x ${asset.height}` : "--";
}

export function assetCanReadMedia(asset: GalleryAsset): boolean {
  return asset.verification_status === "verified";
}

export function galleryDirectoryEquals(
  left: GalleryDirectorySelection,
  right: GalleryDirectorySelection,
): boolean {
  return left.kind === right.kind && left.key === right.key;
}

export function encodeAssetDragPayload(assetIds: string[]): string {
  return JSON.stringify(assetIds.slice(0, IMAGE_EXPLORER_MAX_SELECTION));
}

export function decodeAssetDragPayload(value: string): string[] {
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed) || parsed.length === 0 || parsed.length > IMAGE_EXPLORER_MAX_SELECTION) {
      return [];
    }
    const ids = parsed.filter((item): item is string => typeof item === "string" && item.trim().length > 0);
    return ids.length === parsed.length && new Set(ids).size === ids.length ? ids : [];
  } catch {
    return [];
  }
}

export function flattenGalleryAssetPages(pages: GalleryAssetPage[] | undefined): GalleryAsset[] {
  return pages?.flatMap((page) => page.items) ?? [];
}

export function galleryAssetsQueryKey(
  productId: string,
  directory: GalleryDirectorySelection,
  query: string,
  sort: GalleryAssetSort,
) {
  return ["product-image-library-assets", productId, directory.kind, directory.key, query, sort] as const;
}
