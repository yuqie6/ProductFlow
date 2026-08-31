import { Check, FileQuestion } from "lucide-react";
import type { DragEvent } from "react";

import { api } from "../../../../lib/api";
import { formatDateTime } from "../../../../lib/format";
import { useI18n } from "../../../../lib/preferences";
import type { GalleryAsset } from "../../../../lib/types";
import type { LocalImageEditOpenRequest } from "../../local-edit/LocalImageEditController";
import { ImageAssetActions } from "./ImageAssetActions";
import {
  assetCanReadMedia,
  assetPixelSize,
  encodeAssetDragPayload,
  formatAssetByteSize,
  IMAGE_EXPLORER_DRAG_MIME,
} from "./explorerState";

interface ImageAssetCollectionProps {
  assets: GalleryAsset[];
  selectedIds: Set<string>;
  onToggleSelected: (assetId: string) => void;
  onPreview: (asset: GalleryAsset) => void;
  onRename: (asset: GalleryAsset) => void;
  onMove: (asset: GalleryAsset) => void;
  onUseAsReference?: (asset: GalleryAsset) => void;
  onViewSource?: (asset: GalleryAsset) => void;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
  referenceBusy?: boolean;
  sourceBusy?: boolean;
}

export function ImageAssetGrid(props: ImageAssetCollectionProps) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(112px,1fr))] gap-2">
      {props.assets.map((asset) => (
        <ImageAssetCard key={asset.id} asset={asset} {...props} />
      ))}
    </div>
  );
}

export function ImageAssetList(props: ImageAssetCollectionProps) {
  const { t } = useI18n();
  return (
    <div className="divide-y divide-border-l2 rounded-panel border border-border-l1">
      {props.assets.map((asset) => {
        const readable = assetCanReadMedia(asset);
        const selected = props.selectedIds.has(asset.id);
        return (
          <div
            key={asset.id}
            data-gallery-asset-id={asset.id}
            data-gallery-origin-type={asset.origin_type}
            draggable
            onDragStart={(event) => startAssetDrag(event, asset, props.selectedIds)}
            className={`flex min-h-[58px] min-w-0 items-center gap-2 px-2 py-1.5 ${selected ? "bg-accent-soft" : "bg-surface-raised"}`}
          >
            <SelectionButton asset={asset} selected={selected} onToggle={props.onToggleSelected} />
            <button
              type="button"
              onClick={() => readable && props.onPreview(asset)}
              disabled={!readable}
              className="flex h-11 w-11 shrink-0 items-center justify-center overflow-hidden rounded-control bg-surface-subtle"
              aria-label={t("detail.previewImage", { alt: asset.display_name })}
            >
              {readable ? (
                <img src={api.toApiUrl(asset.thumbnail_url)} alt="" className="h-full w-full object-cover" />
              ) : (
                <FileQuestion size={17} className="text-text-muted" />
              )}
            </button>
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-1.5">
                <span className="min-w-0 flex-1 truncate text-xs font-medium text-text-primary" title={asset.display_name}>
                  {asset.display_name}
                </span>
                {asset.rendition ? (
                  <span className="shrink-0 rounded-control bg-surface-subtle px-1 py-0.5 text-[9px] font-semibold text-text-muted">
                    {asset.rendition.delivery_spec.format.toUpperCase()}
                  </span>
                ) : null}
              </div>
              <div className="mt-1 truncate text-[10px] text-text-muted">
                {assetPixelSize(asset)} · {formatAssetByteSize(asset.byte_size)} · {formatDateTime(asset.created_at, t.locale)}
              </div>
            </div>
            <ImageAssetActions
              asset={asset}
              onPreview={props.onPreview}
              onRename={props.onRename}
              onMove={props.onMove}
              onUseAsReference={props.onUseAsReference}
              onViewSource={props.onViewSource}
              onOpenLocalEdit={props.onOpenLocalEdit}
              referenceBusy={props.referenceBusy}
              sourceBusy={props.sourceBusy}
            />
          </div>
        );
      })}
    </div>
  );
}

function ImageAssetCard({
  asset,
  selectedIds,
  onToggleSelected,
  onPreview,
  onRename,
  onMove,
  onUseAsReference,
  onViewSource,
  onOpenLocalEdit,
  referenceBusy,
  sourceBusy,
}: ImageAssetCollectionProps & { asset: GalleryAsset }) {
  const { t } = useI18n();
  const readable = assetCanReadMedia(asset);
  const selected = selectedIds.has(asset.id);
  return (
    <article
      data-gallery-asset-id={asset.id}
      data-gallery-origin-type={asset.origin_type}
      draggable
      onDragStart={(event) => startAssetDrag(event, asset, selectedIds)}
      className={`group min-w-0 overflow-visible rounded-panel border bg-surface-raised shadow-sm ${
        selected
          ? "border-accent ring-2 ring-accent/20"
          : "border-border-l1"
      }`}
    >
      <div className="relative aspect-square overflow-hidden rounded-t-panel bg-surface-subtle">
        <button
          type="button"
          onClick={() => readable && onPreview(asset)}
          disabled={!readable}
          className="flex h-full w-full items-center justify-center"
          aria-label={t("detail.previewImage", { alt: asset.display_name })}
        >
          {readable ? (
            <img
              src={api.toApiUrl(asset.thumbnail_url)}
              alt={asset.display_name}
              className="h-full w-full object-cover transition-transform duration-200 group-hover:scale-[1.02]"
            />
          ) : (
            <span className="flex flex-col items-center gap-1 text-[10px] text-text-muted">
              <FileQuestion size={20} />
              {asset.verification_status === "missing"
                ? t("detail.library.mediaMissing")
                : t("detail.library.mediaPending")}
            </span>
          )}
        </button>
        <div className="absolute left-1.5 top-1.5">
          <SelectionButton asset={asset} selected={selected} onToggle={onToggleSelected} />
        </div>
        {asset.rendition ? (
          <span className="absolute bottom-1.5 right-1.5 max-w-[calc(100%-0.75rem)] truncate rounded-control bg-surface-inverse/80 px-1.5 py-1 text-[9px] font-semibold text-surface-raised backdrop-blur">
            {asset.rendition.delivery_spec.width} x {asset.rendition.delivery_spec.height} {asset.rendition.delivery_spec.format.toUpperCase()}
          </span>
        ) : null}
      </div>
      <div className="flex h-[52px] min-w-0 items-center gap-1 px-2 py-1.5">
        <div className="min-w-0 flex-1">
          <div className="truncate text-[11px] font-medium text-text-primary" title={asset.display_name}>
            {asset.display_name}
          </div>
          <div className="mt-0.5 truncate text-[9px] text-text-muted">
            {asset.image_type_title ?? assetPixelSize(asset)}
          </div>
        </div>
        <ImageAssetActions
          asset={asset}
          onPreview={onPreview}
          onRename={onRename}
          onMove={onMove}
          onUseAsReference={onUseAsReference}
          onViewSource={onViewSource}
          onOpenLocalEdit={onOpenLocalEdit}
          referenceBusy={referenceBusy}
          sourceBusy={sourceBusy}
        />
      </div>
    </article>
  );
}

function SelectionButton({
  asset,
  selected,
  onToggle,
}: {
  asset: GalleryAsset;
  selected: boolean;
  onToggle: (assetId: string) => void;
}) {
  return (
    <button
      type="button"
      onClick={(event) => {
        event.stopPropagation();
        onToggle(asset.id);
      }}
      className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-control border shadow-sm ${
        selected
          ? "border-accent bg-accent text-accent-fg"
          : "border-border-l3 bg-surface-raised/95 text-transparent hover:text-text-muted"
      }`}
      aria-label={asset.display_name}
      aria-pressed={selected}
    >
      <Check size={13} />
    </button>
  );
}

function startAssetDrag(event: DragEvent<HTMLElement>, asset: GalleryAsset, selectedIds: Set<string>) {
  const ids = selectedIds.has(asset.id) ? [...selectedIds] : [asset.id];
  event.dataTransfer.setData(IMAGE_EXPLORER_DRAG_MIME, encodeAssetDragPayload(ids));
  event.dataTransfer.effectAllowed = "copyMove";
}
