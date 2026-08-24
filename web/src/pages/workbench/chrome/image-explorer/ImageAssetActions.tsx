import { ArrowUpLeft, Download, Eye, MoreHorizontal, MoveRight, Pencil, PencilLine, RefreshCw } from "lucide-react";

import { api } from "../../../../lib/api";
import { useI18n } from "../../../../lib/preferences";
import type { GalleryAsset } from "../../../../lib/types";
import type { LocalImageEditOpenRequest } from "../../local-edit/LocalImageEditController";
import { assetCanReadMedia } from "./explorerState";

interface ImageAssetActionsProps {
  asset: GalleryAsset;
  onPreview: (asset: GalleryAsset) => void;
  onRename: (asset: GalleryAsset) => void;
  onMove: (asset: GalleryAsset) => void;
  onUseAsReference?: (asset: GalleryAsset) => void;
  onViewSource?: (asset: GalleryAsset) => void;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
  referenceBusy?: boolean;
  sourceBusy?: boolean;
}

export function ImageAssetActions({
  asset,
  onPreview,
  onRename,
  onMove,
  onUseAsReference,
  onViewSource,
  onOpenLocalEdit,
  referenceBusy = false,
  sourceBusy = false,
}: ImageAssetActionsProps) {
  const { t } = useI18n();
  const readable = assetCanReadMedia(asset);
  return (
    <details className="relative">
      <summary
        className="flex h-11 w-11 cursor-pointer list-none items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-950 lg:h-7 lg:w-7 [&::-webkit-details-marker]:hidden dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
        title={t("detail.library.moreActions")}
        aria-label={t("detail.library.moreActions")}
      >
        <MoreHorizontal size={15} />
      </summary>
      <div className="absolute right-0 top-8 z-30 w-44 overflow-hidden rounded-md border border-slate-200 bg-white p-1 shadow-xl dark:border-slate-700 dark:bg-slate-950">
        <ActionButton icon={<Eye size={13} />} label={t("detail.library.preview")} onClick={() => onPreview(asset)} disabled={!readable} />
        {readable ? (
          <a
            href={api.toApiUrl(asset.download_url)}
            download={asset.original_filename}
            target="_blank"
            rel="noreferrer"
            className="flex h-8 w-full items-center gap-2 rounded px-2 text-xs text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800"
          >
            <Download size={13} /> {t("detail.library.download")}
          </a>
        ) : null}
        <ActionButton icon={<Pencil size={13} />} label={t("detail.library.renameAsset")} onClick={() => onRename(asset)} />
        {onOpenLocalEdit && readable ? (
          <ActionButton
            icon={<PencilLine size={13} />}
            label={t("localEdit.open")}
            onClick={() => onOpenLocalEdit({ sourceAssetId: asset.id, targetNodeId: null })}
          />
        ) : null}
        <ActionButton icon={<MoveRight size={13} />} label={t("detail.library.move")} onClick={() => onMove(asset)} />
        {asset.rendition && onViewSource ? (
          <ActionButton
            icon={<ArrowUpLeft size={13} />}
            label={t("detail.library.viewSource")}
            onClick={() => onViewSource(asset)}
            disabled={sourceBusy}
          />
        ) : null}
        {onUseAsReference ? (
          <ActionButton
            icon={<RefreshCw size={13} />}
            label={t("detail.library.useAsReference")}
            onClick={() => onUseAsReference(asset)}
            disabled={referenceBusy || !readable}
          />
        ) : null}
      </div>
    </details>
  );
}

function ActionButton({
  icon,
  label,
  onClick,
  disabled = false,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="flex h-8 w-full items-center gap-2 rounded px-2 text-left text-xs text-slate-700 hover:bg-slate-100 disabled:cursor-not-allowed disabled:opacity-40 dark:text-slate-200 dark:hover:bg-slate-800"
    >
      {icon} {label}
    </button>
  );
}
