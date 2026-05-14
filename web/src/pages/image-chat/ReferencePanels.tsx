import { Check, Image as ImageIcon, ImagePlus, Loader2, Trash2 } from "lucide-react";

import { ImageDropZone } from "../../components/ImageDropZone";
import { SelectField } from "../../components/SelectField";
import { api } from "../../lib/api";
import { formatImageSizeValue } from "../../lib/imageSizes";
import type { ImageSessionAsset, ImageSessionRound, ProductDetail, ProductSummary, SourceAsset } from "../../lib/types";
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
    <div className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-700/80 dark:bg-[#151f33]">
      <div className="mb-2 text-sm font-semibold text-slate-950 dark:text-white">{t("chat.sessionReferences")}</div>
      <ImageDropZone
        ariaLabel={t("chat.uploadSessionReference")}
        multiple
        disabled={disabled || uploadBusy}
        className="flex cursor-pointer items-center justify-center rounded-xl border border-dashed border-slate-300 bg-slate-50 px-4 py-4 text-sm text-slate-600 transition-colors hover:border-indigo-300 hover:bg-indigo-50/40 dark:border-slate-600/80 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:border-violet-400/55 dark:hover:bg-violet-500/10"
        onFiles={onFiles}
      >
        {({ isDragging }) => (
          <>
            {uploadBusy ? <Loader2 size={16} className="mr-2 animate-spin" /> : <ImagePlus size={16} className="mr-2" />}
            {isDragging ? t("chat.dropUpload") : t("chat.uploadReference")}
          </>
        )}
      </ImageDropZone>
      <div className="mt-2 text-xs leading-5 text-slate-500 dark:text-slate-400">
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
                className={`group relative overflow-hidden rounded-xl border bg-slate-50 dark:bg-[#0b1220] ${
                  selected
                    ? "border-indigo-500 ring-2 ring-indigo-100 dark:border-violet-400 dark:ring-violet-400/45"
                    : "border-slate-200 dark:border-slate-700"
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
                <label className="absolute bottom-1 left-1 inline-flex h-6 w-6 items-center justify-center rounded-md bg-white/95 text-slate-700 shadow-sm ring-1 ring-slate-200 dark:bg-slate-950/90 dark:text-violet-100 dark:ring-violet-400/35">
                  <input
                    type="checkbox"
                    checked={selected}
                    disabled={selectionLimitReached}
                    onChange={(event) => onToggle(asset.id, event.target.checked)}
                    aria-label={t("chat.useReference")}
                    className="h-3 w-3 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                  />
                  <span className="sr-only">{t("chat.useReference")}</span>
                </label>
                <button
                  type="button"
                  aria-label={t("chat.deleteSessionReference")}
                  onClick={() => onDelete(asset.id)}
                  disabled={deleting}
                  className="absolute right-1 top-1 inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/90 text-slate-500 opacity-100 shadow-sm ring-1 ring-slate-200 transition-colors hover:text-red-600 disabled:opacity-60 dark:bg-slate-950/90 dark:text-slate-300 dark:ring-slate-700 dark:hover:text-red-300 md:opacity-0 md:group-hover:opacity-100"
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
  isProductMode: boolean;
  product: ProductDetail | undefined;
  products: ProductSummary[];
  targetProductId: string;
  sourceImage: SourceAsset | null;
  referenceImages: SourceAsset[];
  selectedRound: ImageSessionRound | null;
  attachBusy: boolean;
  deletingReferenceAssetId: string | null;
  onTargetProductChange: (value: string) => void;
  onDeleteReference: (assetId: string) => void;
  onAttach: (target: "reference" | "main_source") => void;
  t: ImageChatTranslate;
}

export function ProductAssociationPanel({
  isProductMode,
  product,
  products,
  targetProductId,
  sourceImage,
  referenceImages,
  selectedRound,
  attachBusy,
  deletingReferenceAssetId,
  onTargetProductChange,
  onDeleteReference,
  onAttach,
  t,
}: ProductAssociationPanelProps) {
  const saveDisabled = attachBusy || !selectedRound || (!isProductMode && !targetProductId);

  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 dark:border-slate-700/80 dark:bg-[#151f33]">
      <div className="mb-3 text-sm font-semibold text-zinc-900 dark:text-white">{t("chat.saveToProduct")}</div>
      {isProductMode ? (
        product ? (
          <div className="grid grid-cols-[88px_minmax(0,1fr)] gap-3">
            <ProductThumbnail sourceImage={sourceImage} alt={product.name} />
            <div className="min-w-0 self-center">
              <div className="truncate text-sm font-medium text-zinc-900 dark:text-slate-100">{product.name}</div>
              <div className="mt-1 text-xs text-zinc-500 dark:text-slate-400">{t("chat.productReferenceCount", { count: referenceImages.length })}</div>
            </div>
          </div>
        ) : (
          <div className="flex justify-center py-6 text-zinc-400">
            <Loader2 size={16} className="animate-spin" />
          </div>
        )
      ) : (
        <label className="block">
          <span className="mb-1.5 block text-xs font-semibold text-slate-700 dark:text-slate-200">{t("chat.targetProduct")}</span>
          <SelectField
            value={targetProductId}
            options={
              products.length
                ? products.map((item) => ({ value: item.id, label: item.name }))
                : [{ value: "", label: t("chat.noProducts"), disabled: true }]
            }
            onChange={onTargetProductChange}
          />
        </label>
      )}

      {referenceImages.length ? (
        <div className="mt-3 grid grid-cols-4 gap-2">
          {referenceImages.slice(0, 4).map((asset) => {
            const deleting = deletingReferenceAssetId === asset.id;
            return (
              <div key={asset.id} className="group relative overflow-hidden rounded-md border border-zinc-200 bg-white dark:border-slate-700 dark:bg-slate-950/70">
                <a href={api.toApiUrl(asset.preview_url)} target="_blank" rel="noreferrer" title={asset.original_filename}>
                  <img
                    src={api.toApiUrl(asset.thumbnail_url)}
                    alt={asset.original_filename}
                    loading="lazy"
                    decoding="async"
                    className="h-16 w-full object-cover"
                  />
                </a>
                <button
                  type="button"
                  aria-label={t("chat.deleteProductReference")}
                  onClick={() => onDeleteReference(asset.id)}
                  disabled={deleting}
                  className="absolute right-1 top-1 inline-flex h-6 w-6 items-center justify-center rounded bg-white/90 text-zinc-500 opacity-100 shadow-sm ring-1 ring-zinc-200 transition-colors hover:text-red-600 disabled:opacity-60 dark:bg-slate-950/90 dark:text-slate-300 dark:ring-slate-700 dark:hover:text-red-300 md:opacity-0 md:group-hover:opacity-100"
                >
                  {deleting ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
                </button>
              </div>
            );
          })}
        </div>
      ) : null}

      <div className="mt-4 border-t border-slate-200 pt-3 dark:border-slate-800">
        {selectedRound ? (
          <div className="mb-2 text-[11px] leading-5 text-slate-500 dark:text-slate-400">
            {t("chat.selectedCandidate", { size: formatImageSizeValue(selectedRound.size) })}
          </div>
        ) : (
          <div className="mb-2 rounded-xl border border-dashed border-slate-200 bg-white px-3 py-2 text-center text-sm text-slate-400 dark:border-slate-700 dark:bg-slate-950/45 dark:text-slate-500">
            {t("chat.selectHistoryFirst")}
          </div>
        )}
        <div className="grid gap-2">
          <button
            type="button"
            onClick={() => onAttach("reference")}
            disabled={saveDisabled}
            className="inline-flex items-center justify-center rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-950 disabled:opacity-60 dark:border-slate-700 dark:bg-slate-950/70 dark:text-slate-200 dark:hover:border-violet-400/55 dark:hover:text-violet-100"
          >
            {attachBusy ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Check size={14} className="mr-2" />}
            {isProductMode ? t("chat.addReference") : t("chat.saveAsReference")}
          </button>
          {isProductMode ? (
            <button
              type="button"
              onClick={() => onAttach("main_source")}
              disabled={saveDisabled}
              className="inline-flex items-center justify-center rounded-xl bg-slate-900 px-3 py-2 text-sm font-semibold text-white transition-colors hover:bg-slate-800 disabled:opacity-60 dark:bg-violet-500/20 dark:text-violet-100 dark:ring-1 dark:ring-violet-400/35 dark:hover:bg-violet-500/30"
            >
              {attachBusy ? <Loader2 size={14} className="mr-2 animate-spin" /> : <ImageIcon size={14} className="mr-2" />}
              {t("chat.setMainSource")}
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function ProductThumbnail({ sourceImage, alt }: { sourceImage: SourceAsset | null; alt: string }) {
  return (
    <div className="overflow-hidden rounded-lg border border-zinc-200 bg-white dark:border-slate-700 dark:bg-slate-950/70">
      {sourceImage ? (
        <img src={api.toApiUrl(sourceImage.thumbnail_url)} alt={alt} decoding="async" className="h-24 w-full object-cover" />
      ) : (
        <div className="flex h-24 items-center justify-center text-zinc-300 dark:text-slate-500">
          <ImageIcon size={20} />
        </div>
      )}
    </div>
  );
}
