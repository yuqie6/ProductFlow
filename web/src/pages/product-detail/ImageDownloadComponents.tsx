import { Download } from "lucide-react";

import { api } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type { DownloadableImage } from "../../lib/image-downloads";
import type { PosterVariant } from "../../lib/types";
import { buildPosterDownload } from "./imageDownloads";

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
}: {
  poster: PosterVariant;
  productName: string;
}) {
  const image = buildPosterDownload(productName, poster, poster.thumbnail_url);
  return (
    <div className="group overflow-hidden rounded-md border border-zinc-200 bg-white">
      <a
        href={api.toApiUrl(poster.preview_url)}
        target="_blank"
        rel="noreferrer"
        className="block"
      >
        <div className="aspect-square bg-zinc-100">
          <img
            src={image.previewUrl}
            alt={image.alt}
            className="h-full w-full object-cover transition-transform group-hover:scale-[1.02]"
          />
        </div>
      </a>
      <div className="flex items-center justify-between gap-2 border-t border-zinc-100 px-2 py-1 text-[10px] text-zinc-500">
        <span className="min-w-0 truncate">
          {poster.kind === "main_image" ? "主图" : "促销"} ·{" "}
          {formatDateTime(poster.created_at)}
        </span>
        <DownloadLink image={image} />
      </div>
    </div>
  );
}
