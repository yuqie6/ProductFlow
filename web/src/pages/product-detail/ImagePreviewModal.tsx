import { X } from "lucide-react";

import type { DownloadableImage } from "../../lib/image-downloads";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "./constants";
import { DownloadLink } from "./ImageDownloadComponents";

interface ImagePreviewModalProps {
  image: DownloadableImage;
  onClose: () => void;
}

export function ImagePreviewModal({ image, onClose }: ImagePreviewModalProps) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-zinc-950/70 p-6"
      role="dialog"
      aria-modal="true"
      aria-label={image.alt}
      onClick={onClose}
    >
      <div
        className="flex max-h-full w-full max-w-5xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-center justify-between gap-3 border-b border-zinc-200 px-4 py-3">
          <div className="min-w-0 truncate text-sm font-medium text-zinc-800">
            {image.alt}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <DownloadLink image={image} />
            <button
              type="button"
              onClick={onClose}
              className="inline-flex h-8 w-8 items-center justify-center rounded-full border border-zinc-200 text-zinc-500 hover:bg-zinc-50 hover:text-zinc-800"
              aria-label="关闭预览"
            >
              <X size={16} />
            </button>
          </div>
        </div>
        <div className={`flex min-h-0 flex-1 items-center justify-center p-4 ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}>
          <img
            src={image.previewUrl}
            alt={image.alt}
            className="max-h-[calc(100vh-11rem)] max-w-full object-contain"
          />
        </div>
      </div>
    </div>
  );
}
