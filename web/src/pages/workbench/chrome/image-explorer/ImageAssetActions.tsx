import { ArrowUpLeft, Download, Eye, MoreHorizontal, MoveRight, Pencil, PencilLine, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";

import { DropdownMenu, DropdownMenuItem } from "../../../../components/ui/dropdown-menu";
import { IconButton } from "../../../../components/ui/icon-button";
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
    <DropdownMenu
      trigger={(
        <IconButton
          label={t("detail.library.moreActions")}
          variant="ghost"
          size="toolbar"
          tooltip={false}
        >
          <MoreHorizontal size={15} />
        </IconButton>
      )}
    >
      <DropdownMenuItem onSelect={() => onPreview(asset)} disabled={!readable}>
        <ActionLabel icon={<Eye size={13} />} label={t("detail.library.preview")} />
      </DropdownMenuItem>
      {readable ? (
        <a
          href={api.toApiUrl(asset.download_url)}
          download={asset.original_filename}
          target="_blank"
          rel="noreferrer"
          className="flex items-center rounded-control px-2.5 py-1.5 text-xs font-medium text-text-primary outline-none hover:bg-surface-subtle focus-visible:ring-2 focus-visible:ring-focus-ring"
        >
          <ActionLabel icon={<Download size={13} />} label={t("detail.library.download")} />
        </a>
      ) : null}
      <DropdownMenuItem onSelect={() => onRename(asset)}>
        <ActionLabel icon={<Pencil size={13} />} label={t("detail.library.renameAsset")} />
      </DropdownMenuItem>
      {onOpenLocalEdit && readable ? (
        <DropdownMenuItem onSelect={() => onOpenLocalEdit({ sourceAssetId: asset.id, targetNodeId: null })}>
          <ActionLabel icon={<PencilLine size={13} />} label={t("localEdit.open")} />
        </DropdownMenuItem>
      ) : null}
      <DropdownMenuItem onSelect={() => onMove(asset)}>
        <ActionLabel icon={<MoveRight size={13} />} label={t("detail.library.move")} />
      </DropdownMenuItem>
      {asset.rendition && onViewSource ? (
        <DropdownMenuItem onSelect={() => onViewSource(asset)} disabled={sourceBusy}>
          <ActionLabel icon={<ArrowUpLeft size={13} />} label={t("detail.library.viewSource")} />
        </DropdownMenuItem>
      ) : null}
      {onUseAsReference ? (
        <DropdownMenuItem onSelect={() => onUseAsReference(asset)} disabled={referenceBusy || !readable}>
          <ActionLabel icon={<RefreshCw size={13} />} label={t("detail.library.useAsReference")} />
        </DropdownMenuItem>
      ) : null}
    </DropdownMenu>
  );
}

function ActionLabel({ icon, label }: { icon: ReactNode; label: string }) {
  return (
    <span className="flex items-center gap-2">
      {icon} {label}
    </span>
  );
}
