import { Download } from "lucide-react";

import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";

export function DownloadLink({
  image,
  variant = "button",
}: {
  image: DownloadableImage;
  variant?: "button" | "overlay";
}) {
  const { t } = useI18n();
  const className =
    variant === "overlay"
      ? "nodrag nopan nowheel absolute bottom-2 right-2 inline-flex items-center rounded bg-white/95 px-2 py-1 text-[10px] font-medium text-zinc-700 shadow-sm ring-1 ring-zinc-200 hover:bg-white dark:bg-slate-950/88 dark:text-slate-100 dark:ring-slate-700 dark:hover:bg-slate-900"
      : "nodrag nopan nowheel inline-flex items-center rounded border border-zinc-200 bg-white px-2 py-1 text-[10px] font-medium text-zinc-600 hover:border-zinc-300 hover:bg-zinc-50 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:border-violet-400/50 dark:hover:bg-violet-500/12 dark:hover:text-white";
  return (
    <a
      data-node-action
      href={image.downloadUrl}
      download={image.filename}
      onClick={(event) => event.stopPropagation()}
      target="_blank"
      rel="noreferrer"
      className={className}
      title={t("detail.downloadImage", { filename: image.filename })}
      aria-label={t("detail.downloadImage", { filename: image.filename })}
    >
      <Download size={11} className="mr-1" /> {t("detail.download")}
    </a>
  );
}
