import { Check, ImagePlus, Loader2, Trash2 } from "lucide-react";

import { ImageDropZone } from "../../components/ImageDropZone";
import { Select as SelectField } from "../../components/ui/select";
import { api } from "../../lib/api";
import { formatImageSizeValue } from "../../lib/imageSizes";
import type { ImageSessionAsset, ImageSessionRound, ProductSummary } from "../../lib/types";
import type { ImageChatTranslate } from "./display";

interface SessionReferencePanelProps {
  assets: ImageSessionAsset[];
  selectedAssetIds: string[];
  maxSelectedCount: number;
  uploadBusy: boolean;
  deletingAssetId: string | null;
  disabled: boolean;
  onFiles: (files: File[]) => void;
  onToggle: (assetId: string, checked: boolean) => void;
  onDelete: (assetId: string) => void;
  t: ImageChatTranslate;
}

export function SessionReferencePanel({
  assets,
  selectedAssetIds,
  maxSelectedCount,
  uploadBusy,
  deletingAssetId,
  disabled,
  onFiles,
  onToggle,
  onDelete,
  t,
}: SessionReferencePanelProps) {
  return (
    <div className="border-t border-border-l1 pt-4">
      <div className="mb-2 text-sm font-semibold text-text-primary dark:text-white">{t("chat.sessionReferences")}</div>
      <ImageDropZone
        ariaLabel={t("chat.uploadSessionReference")}
        multiple
        disabled={disabled || uploadBusy}
        className="flex cursor-pointer items-center justify-center rounded-xl border border-dashed border-border-l3 bg-surface-base px-4 py-4 text-sm text-text-secondary transition-colors hover:border-accent hover:bg-accent-soft/40 dark:border-border-l3/80 dark:bg-surface-panel dark:text-text-secondary dark:hover:border-accent/55 dark:hover:bg-accent/10"
        onFiles={onFiles}
      >
        {({ isDragging }) => (
          <>
            {uploadBusy ? <Loader2 size={16} className="mr-2 animate-spin" /> : <ImagePlus size={16} className="mr-2" />}
            {isDragging ? t("chat.dropUpload") : t("chat.uploadReference")}
          </>
        )}
      </ImageDropZone>
      <div className="mt-2 text-xs leading-5 text-text-muted dark:text-text-muted">
        {t("chat.selectedReferences", { selected: selectedAssetIds.length, max: maxSelectedCount })}
      </div>
      {assets.length ? (
        <div className="mt-3 grid grid-cols-4 gap-2">
          {assets.map((asset) => {
            const deleting = deletingAssetId === asset.id;
            const selected = selectedAssetIds.includes(asset.id);
            const selectionLimitReached = !selected && selectedAssetIds.length >= maxSelectedCount;
            return (
              <div
                key={asset.id}
                className={`group relative overflow-hidden rounded-xl border bg-surface-base dark:bg-surface-panel ${
                  selected
                    ? "border-accent ring-2 ring-accent dark:border-accent dark:ring-accent/45"
                    : "border-border-l1 dark:border-border-l1"
                }`}
              >
                <a href={api.toApiUrl(asset.preview_url)} target="_blank" rel="noreferrer" title={asset.original_filename}>
                  <img
                    src={api.toApiUrl(asset.thumbnail_url)}
                    alt={asset.original_filename}
                    loading="lazy"
                    decoding="async"
                    className="h-20 w-full object-cover"
                  />
                </a>
                <label className="absolute bottom-1 left-1 inline-flex h-6 w-6 items-center justify-center rounded-md bg-surface-raised/95 text-text-secondary shadow-sm ring-1 ring-border-l1 dark:bg-surface-base/90 dark:text-accent dark:ring-accent/35">
                  <input
                    type="checkbox"
                    checked={selected}
                    disabled={selectionLimitReached}
                    onChange={(event) => onToggle(asset.id, event.target.checked)}
                    aria-label={t("chat.useReference")}
                    className="h-3 w-3 rounded border-border-l3 text-accent focus:ring-accent"
                  />
                  <span className="sr-only">{t("chat.useReference")}</span>
                </label>
                <button
                  type="button"
                  aria-label={t("chat.deleteSessionReference")}
                  onClick={() => onDelete(asset.id)}
                  disabled={deleting}
                  className="absolute right-1 top-1 inline-flex h-7 w-7 items-center justify-center rounded-lg bg-surface-raised/90 text-text-muted opacity-100 shadow-sm ring-1 ring-border-l1 transition-colors hover:text-state-error disabled:opacity-60 dark:bg-surface-base/90 dark:text-text-secondary dark:ring-border-l1 dark:hover:text-state-error md:opacity-0 md:group-hover:opacity-100"
                >
                  {deleting ? <Loader2 size={13} className="animate-spin" /> : <Trash2 size={13} />}
                </button>
              </div>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

interface ProductAssociationPanelProps {
  products: ProductSummary[];
  targetProductId: string;
  selectedRound: ImageSessionRound | null;
  attachBusy: boolean;
  onTargetProductChange: (value: string) => void;
  onAttach: () => void;
  t: ImageChatTranslate;
}

export function ProductAssociationPanel({
  products,
  targetProductId,
  selectedRound,
  attachBusy,
  onTargetProductChange,
  onAttach,
  t,
}: ProductAssociationPanelProps) {
  const saveDisabled = attachBusy || !selectedRound || !targetProductId;

  return (
    <div className="border-t border-border-l1 pt-4">
      <div className="mb-3 text-sm font-semibold text-text-primary dark:text-white">{t("chat.saveToProduct")}</div>
      <label className="block">
        <span className="mb-1.5 block text-xs font-semibold text-text-secondary dark:text-text-primary">{t("chat.targetProduct")}</span>
        <SelectField
          value={targetProductId}
          options={
            products.length
              ? [
                  { value: "", label: t("chat.selectTargetProduct") },
                  ...products.map((item) => ({ value: item.id, label: item.name })),
                ]
              : [{ value: "", label: t("chat.noProducts"), disabled: true }]
          }
          onChange={onTargetProductChange}
        />
      </label>

      <div className="mt-4 border-t border-border-l1 pt-3 dark:border-border-l2">
        {selectedRound ? (
          <div className="mb-2 text-[11px] leading-5 text-text-muted dark:text-text-muted">
            {t("chat.selectedCandidate", { size: formatImageSizeValue(selectedRound.size) })}
          </div>
        ) : (
          <div className="mb-2 rounded-xl border border-dashed border-border-l1 bg-surface-raised px-3 py-2 text-center text-sm text-text-muted dark:border-border-l1 dark:bg-surface-base/45 dark:text-text-muted">
            {t("chat.selectHistoryFirst")}
          </div>
        )}
        <button
          type="button"
          onClick={onAttach}
          disabled={saveDisabled}
          className="inline-flex w-full items-center justify-center rounded-xl bg-text-muted px-3 py-2 text-sm font-semibold text-accent-fg transition-colors hover:bg-text-muted disabled:opacity-60 dark:bg-accent/20 dark:text-accent dark:ring-1 dark:ring-accent/35 dark:hover:bg-accent/30"
        >
          {attachBusy ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Check size={14} className="mr-2" />}
          {t("chat.saveToProduct")}
        </button>
      </div>
    </div>
  );
}
