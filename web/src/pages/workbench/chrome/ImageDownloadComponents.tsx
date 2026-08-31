import { Download } from "lucide-react";

import { Tooltip } from "../../../components/ui/tooltip";
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
      ? "nodrag nopan nowheel absolute bottom-2 right-2 inline-flex items-center rounded-control bg-surface-raised/95 px-2 py-1 text-[10px] font-medium text-text-secondary shadow-elev-1 ring-1 ring-border-l1 hover:bg-surface-raised"
      : "nodrag nopan nowheel inline-flex items-center rounded-control border border-border-l1 bg-surface-raised px-2 py-1 text-[10px] font-medium text-text-secondary hover:border-border-l3 hover:bg-surface-subtle";
  const label = t("detail.downloadImage", { filename: image.filename });
  return (
    <Tooltip content={label}>
      <a
        data-node-action
        href={image.downloadUrl}
        download={image.filename}
        onClick={(event) => event.stopPropagation()}
        target="_blank"
        rel="noreferrer"
        className={className}
        aria-label={label}
      >
        <Download size={11} className="mr-1" /> {t("detail.download")}
      </a>
    </Tooltip>
  );
}
