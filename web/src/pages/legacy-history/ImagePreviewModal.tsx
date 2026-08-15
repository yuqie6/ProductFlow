import { Download, X } from "lucide-react";

import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";

type ImagePreviewModalProps = {
  image: DownloadableImage;
  onClose: () => void;
};

export function ImagePreviewModal({ image, onClose }: ImagePreviewModalProps) {
  const { t } = useI18n();
  const downloadLabel = t("history.downloadAsset", { name: image.alt });
  const closeLabel = t("history.closePreview");

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/75 p-4"
      role="dialog"
      aria-modal="true"
      aria-label={image.alt}
      onClick={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <div className="relative flex max-h-full max-w-5xl flex-col overflow-hidden rounded-lg bg-white shadow-2xl dark:bg-[#101621]">
        <div className="flex items-center justify-end gap-2 border-b border-slate-200 px-3 py-2 dark:border-slate-800">
          <a
            href={image.downloadUrl}
            download={image.filename}
            className="inline-flex h-8 w-8 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-950 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
            aria-label={downloadLabel}
            title={downloadLabel}
          >
            <Download size={16} aria-hidden="true" />
          </a>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-8 w-8 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-950 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
            aria-label={closeLabel}
            title={closeLabel}
          >
            <X size={17} aria-hidden="true" />
          </button>
        </div>
        <div className="flex min-h-0 items-center justify-center p-3 sm:p-6">
          <img
            src={image.previewUrl}
            alt={image.alt}
            className="max-h-[calc(100dvh-8rem)] max-w-full object-contain"
          />
        </div>
      </div>
    </div>
  );
}
