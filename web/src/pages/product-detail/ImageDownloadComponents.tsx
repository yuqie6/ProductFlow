import { Download } from "lucide-react";

import { formatDateTime } from "../../lib/format";
import type { DownloadableImage } from "../../lib/image-downloads";
import type { PosterVariant, ProductDetail, SourceAsset } from "../../lib/types";
import { buildPosterDownload, buildSourceImageDownload } from "./imageDownloads";

export function DownloadLink({
  image,
  variant = "button",
}: {
  image: DownloadableImage;
  variant?: "button" | "overlay";
}) {
  const className =
    variant === "overlay"
      ? "absolute bottom-2 right-2 inline-flex items-center rounded bg-white/95 px-2 py-1 text-[10px] font-medium text-zinc-700 shadow-sm ring-1 ring-zinc-200 hover:bg-white"
      : "inline-flex items-center rounded border border-zinc-200 bg-white px-2 py-1 text-[10px] font-medium text-zinc-600 hover:border-zinc-300 hover:bg-zinc-50";
  return (
    <a
      data-node-action
      href={image.downloadUrl}
      download={image.filename}
      onClick={(event) => event.stopPropagation()}
      target="_blank"
      rel="noreferrer"
      className={className}
      title={`下载 ${image.filename}`}
      aria-label={`下载 ${image.filename}`}
    >
      <Download size={11} className="mr-1" /> 下载
    </a>
  );
}

export function PosterThumb({
  poster,
  productName,
  onPreview,
  onUseAsReference,
  useAsReferenceDisabled = false,
  useAsReferenceBusy = false,
}: {
  poster: PosterVariant;
  productName: string;
  onPreview?: (image: DownloadableImage) => void;
  onUseAsReference?: () => void;
  useAsReferenceDisabled?: boolean;
  useAsReferenceBusy?: boolean;
}) {
  const image = buildPosterDownload(productName, poster);
  const thumbnailImage = buildPosterDownload(productName, poster, poster.thumbnail_url);
  return (
    <div className="group overflow-hidden rounded-md border border-zinc-200 bg-white">
      <button
        type="button"
        onClick={() => onPreview?.(image)}
        className="block w-full"
        aria-label={`预览 ${image.alt}`}
      >
        <div className="aspect-square bg-zinc-100">
          <img
            src={thumbnailImage.previewUrl}
            alt={image.alt}
            className="h-full w-full object-cover transition-transform group-hover:scale-[1.02]"
          />
        </div>
      </button>
      <div className="flex items-center justify-between gap-2 border-t border-zinc-100 px-2 py-1 text-[10px] text-zinc-500">
        <span className="min-w-0 truncate">
          {poster.kind === "main_image" ? "主图" : "促销"} ·{" "}
          {formatDateTime(poster.created_at)}
        </span>
        <div className="flex shrink-0 items-center gap-1">
          {onUseAsReference ? (
            <button
              data-node-action
              type="button"
              onClick={(event) => {
                event.stopPropagation();
                onUseAsReference();
              }}
              disabled={useAsReferenceDisabled || useAsReferenceBusy}
              className="inline-flex items-center rounded border border-blue-200 bg-blue-50 px-2 py-1 text-[10px] font-medium text-blue-700 hover:border-blue-300 hover:bg-blue-100 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {useAsReferenceBusy ? "填充中" : "填充"}
            </button>
          ) : null}
          <DownloadLink image={image} />
        </div>
      </div>
    </div>
  );
}

export function SourceAssetThumb({
  asset,
  product,
  onPreview,
  onUseAsReference,
  useAsReferenceDisabled = false,
  useAsReferenceBusy = false,
}: {
  asset: SourceAsset;
  product: ProductDetail;
  onPreview?: (image: DownloadableImage) => void;
  onUseAsReference?: () => void;
  useAsReferenceDisabled?: boolean;
  useAsReferenceBusy?: boolean;
}) {
  const image = buildSourceImageDownload(
    product,
    asset,
    asset.kind === "original_image" ? "主图" : "参考图",
  );
  const thumbnailImage = buildSourceImageDownload(
    product,
    asset,
    asset.kind === "original_image" ? "主图" : "参考图",
    asset.thumbnail_url,
  );
  return (
    <div className="group overflow-hidden rounded-md border border-zinc-200 bg-white">
      <button
        type="button"
        onClick={() => onPreview?.(image)}
        className="block w-full"
        aria-label={`预览 ${image.alt}`}
      >
        <div className="flex aspect-square items-center justify-center bg-zinc-100 p-2">
          <img
            src={thumbnailImage.previewUrl}
            alt={image.alt}
            className="h-full w-full object-contain transition-transform group-hover:scale-[1.02]"
          />
        </div>
      </button>
      <div className="flex items-center justify-between gap-2 border-t border-zinc-100 px-2 py-1 text-[10px] text-zinc-500">
        <span className="min-w-0 truncate">
          参考图 · {formatDateTime(asset.created_at)}
        </span>
        <div className="flex shrink-0 items-center gap-1">
          {onUseAsReference ? (
            <button
              data-node-action
              type="button"
              onClick={(event) => {
                event.stopPropagation();
                onUseAsReference();
              }}
              disabled={useAsReferenceDisabled || useAsReferenceBusy}
              className="inline-flex items-center rounded border border-blue-200 bg-blue-50 px-2 py-1 text-[10px] font-medium text-blue-700 hover:border-blue-300 hover:bg-blue-100 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {useAsReferenceBusy ? "填充中" : "填充"}
            </button>
          ) : null}
          <DownloadLink image={image} />
        </div>
      </div>
    </div>
  );
}
