import type { PosterVariant, ProductDetail, SourceAsset, WorkflowNode } from "../../lib/types";
import type { DownloadableImage } from "../../lib/image-downloads";
import {
  compactDateTime,
  getExtensionFromFilename,
  getExtensionFromMime,
  sanitizeFilenamePart,
  toImageUrl,
} from "../../lib/image-downloads";
import { outputStringArray } from "./utils";

export function getSourceImageAsset(product: ProductDetail): SourceAsset | null {
  return (
    product.source_assets.find((asset) => asset.kind === "original_image") ??
    null
  );
}

export function buildSourceImageDownload(
  product: ProductDetail,
  asset: SourceAsset,
  label: string,
  previewUrl?: string,
): DownloadableImage {
  const productName = sanitizeFilenamePart(product.name, "商品");
  const imageLabel = sanitizeFilenamePart(label, "图片");
  const extension = getExtensionFromFilename(
    asset.original_filename,
    asset.mime_type,
  );
  return {
    previewUrl: toImageUrl(previewUrl, asset.preview_url, asset.download_url),
    downloadUrl: toImageUrl(asset.download_url, asset.preview_url),
    filename: `${productName}-${imageLabel}-${compactDateTime(asset.created_at)}${extension}`,
    alt: `${product.name} ${label}`,
  };
}

export function buildPosterDownload(
  productName: string,
  poster: PosterVariant,
  previewUrl?: string,
): DownloadableImage {
  const productLabel = sanitizeFilenamePart(productName, "商品");
  const posterLabel = poster.kind === "main_image" ? "主图" : "海报";
  const extension = getExtensionFromMime(poster.mime_type);
  return {
    previewUrl: toImageUrl(previewUrl, poster.preview_url, poster.download_url),
    downloadUrl: toImageUrl(poster.download_url, poster.preview_url),
    filename: `${productLabel}-${posterLabel}-${compactDateTime(poster.created_at)}${extension}`,
    alt: `${productName} ${posterLabel}`,
  };
}

export function getSourceImageDownload(
  product: ProductDetail,
): DownloadableImage | null {
  const sourceAsset = getSourceImageAsset(product);
  return sourceAsset
    ? buildSourceImageDownload(product, sourceAsset, "主图")
    : null;
}

export function getNodeImageDownload(
  node: WorkflowNode,
  product: ProductDetail,
): DownloadableImage | null {
  if (node.node_type === "product_context") {
    return getSourceImageDownload(product);
  }
  if (node.node_type === "reference_image") {
    const ids = outputStringArray(node, "source_asset_ids");
    const asset = ids
      .map((id) =>
        product.source_assets.find((item: SourceAsset) => item.id === id),
      )
      .find((item): item is SourceAsset => Boolean(item));
    return asset
      ? buildSourceImageDownload(
          product,
          asset,
          node.title || "参考图",
          asset.thumbnail_url,
        )
      : null;
  }
  return null;
}
