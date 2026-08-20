import { api } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { sanitizeFilenamePart } from "../../../lib/image-downloads";
import type { WorkflowNodeV2 } from "../../../lib/types";

export function workflowNodeAssetId(node: WorkflowNodeV2): string | null {
  if (node.bound_image_asset_id) {
    return node.bound_image_asset_id;
  }
  const resultAssetId = node.output_json?.result_asset_id;
  return typeof resultAssetId === "string" && resultAssetId ? resultAssetId : null;
}

export function workflowAssetThumbnailUrl(assetId: string): string {
  return api.getProductImageAssetMediaUrl(assetId, "thumbnail");
}

export function workflowNodeDownloadableImage(
  node: WorkflowNodeV2,
  previewVariant: "thumbnail" | "preview" = "preview",
): DownloadableImage | null {
  const assetId = workflowNodeAssetId(node);
  if (!assetId) {
    return null;
  }
  return {
    previewUrl: api.getProductImageAssetMediaUrl(assetId, previewVariant),
    downloadUrl: api.getProductImageAssetMediaUrl(assetId),
    filename: `${sanitizeFilenamePart(node.title, node.key || "workflow-image")}.png`,
    alt: node.title,
  };
}
