import type { GalleryAsset } from "../../../../lib/types";

export interface ImageExplorerTargetSelectionResult {
  assets: GalleryAsset[];
  limitExceeded: boolean;
}

export function toggleImageExplorerTargetAsset(
  current: readonly GalleryAsset[],
  asset: GalleryAsset,
  maximum: number,
): ImageExplorerTargetSelectionResult {
  if (current.some((item) => item.id === asset.id)) {
    return {
      assets: current.filter((item) => item.id !== asset.id),
      limitExceeded: false,
    };
  }
  if (current.length >= maximum) {
    return { assets: [...current], limitExceeded: true };
  }
  return { assets: [...current, asset], limitExceeded: false };
}
