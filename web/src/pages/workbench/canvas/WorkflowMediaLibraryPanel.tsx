import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, FolderPlus, Images, Link2, Loader2, Maximize2, Search, Unlink, X } from "lucide-react";
import { useEffect, useState } from "react";

import { api, ApiError } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { MediaLibraryAsset, WorkflowMediaLibraryAsset } from "../../../lib/types";
import type { ImageExplorerReferenceTarget } from "../chrome/image-explorer/ProductImageExplorer";

interface WorkflowMediaLibraryPanelProps {
  productId: string;
  workflowId: string;
  referenceTarget?: ImageExplorerReferenceTarget;
  onPreviewImage: (image: DownloadableImage) => void;
}

export function WorkflowMediaLibraryPanel({
  productId,
  workflowId,
  referenceTarget,
  onPreviewImage,
}: WorkflowMediaLibraryPanelProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerSearch, setPickerSearch] = useState("");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const linkedQuery = useQuery({
    queryKey: ["workflow-media-library", productId, workflowId],
    queryFn: () => api.listWorkflowMediaLibraryAssets(productId, workflowId),
    enabled: Boolean(productId && workflowId),
  });
  const pickerQuery = useQuery({
    queryKey: ["workflow-media-library-picker", pickerSearch],
    queryFn: () => api.listMediaLibraryAssets({ q: pickerSearch, limit: 60 }),
    enabled: pickerOpen,
  });
  const linked = linkedQuery.data?.items ?? [];
  const linkedById = new Set(linked.map((item) => item.asset.id));
  const syncMutation = useMutation({
    mutationFn: (assetIds: string[]) => api.syncWorkflowMediaLibraryAssets(productId, workflowId, assetIds),
    onSuccess: async () => {
      setSelectedIds(new Set());
      setPickerOpen(false);
      await queryClient.invalidateQueries({ queryKey: ["workflow-media-library", productId, workflowId] });
      await queryClient.invalidateQueries({ queryKey: ["active-product-workflow-v2", productId] });
    },
  });
  const removeMutation = useMutation({
    mutationFn: (assetId: string) => api.removeWorkflowMediaLibraryAsset(productId, workflowId, assetId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["workflow-media-library", productId, workflowId] }),
  });
  const referenceMutation = useMutation({
    mutationFn: (asset: WorkflowMediaLibraryAsset) => {
      if (!referenceTarget || !asset.product_image_asset_id) {
        throw new Error(t("workflowV2.reference.unavailable"));
      }
      return api.bindWorkflowReferenceAsset(productId, workflowId, referenceTarget.nodeId, {
        asset_id: asset.product_image_asset_id,
        expected_workflow_revision: referenceTarget.expectedWorkflowRevision,
        expected_bound_asset_id: referenceTarget.expectedBoundAssetId,
      });
    },
    onSuccess: async (result) => {
      referenceTarget?.onBound?.(result);
      await queryClient.invalidateQueries({ queryKey: ["active-product-workflow-v2", productId] });
    },
  });
  const error = [linkedQuery.error, pickerQuery.error, syncMutation.error, removeMutation.error, referenceMutation.error]
    .find((item): item is Error => item instanceof Error) ?? null;
  const busy = syncMutation.isPending || removeMutation.isPending || referenceMutation.isPending;

  useEffect(() => {
    if (!pickerOpen) {
      setSelectedIds(new Set());
    }
  }, [pickerOpen]);

  const togglePickerAsset = (assetId: string) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(assetId)) next.delete(assetId);
      else next.add(assetId);
      return next;
    });
  };
  const preview = (asset: MediaLibraryAsset) => {
    if (asset.verification_status !== "verified") return;
    onPreviewImage({
      previewUrl: api.toApiUrl(asset.preview_url),
      downloadUrl: api.toApiUrl(asset.download_url),
      filename: asset.original_filename,
      alt: asset.display_name,
    });
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-slate-200 px-3 py-2 dark:border-slate-800">
        <div className="min-w-0 flex-1">
          <div className="text-xs font-semibold text-slate-800 dark:text-slate-100">{t("workflowV2.mediaLibrary.title")}</div>
          <div className="mt-0.5 text-[10px] text-slate-400">{t("workflowV2.mediaLibrary.count", { count: linked.length })}</div>
        </div>
        <button type="button" onClick={() => setPickerOpen(true)} className="inline-flex h-8 items-center gap-1.5 rounded-md bg-indigo-600 px-2.5 text-[11px] font-semibold text-white hover:bg-indigo-500 dark:bg-violet-500 dark:hover:bg-violet-400" title={t("workflowV2.mediaLibrary.add")}>
          <FolderPlus size={13} />{t("workflowV2.mediaLibrary.add")}
        </button>
      </div>

      {error ? <div role="alert" className="m-3 rounded-md border border-red-200 bg-red-50 px-2.5 py-2 text-xs text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">{error instanceof ApiError ? error.detail : error.message}</div> : null}
      {linkedQuery.isLoading ? <PanelState icon={<Loader2 size={17} className="animate-spin" />} text={t("workflowV2.mediaLibrary.loading")} /> : linked.length === 0 ? <PanelState icon={<Images size={20} />} text={t("workflowV2.mediaLibrary.empty")} action={t("workflowV2.mediaLibrary.add")} onAction={() => setPickerOpen(true)} /> : <div className="min-h-0 flex-1 overflow-y-auto p-3"><div className="grid grid-cols-2 gap-2">{linked.map((item) => <LinkedAssetCard key={item.asset.id} item={item} busy={busy} canReference={Boolean(referenceTarget?.nodeId && item.product_image_asset_id)} onPreview={() => preview(item.asset)} onRemove={() => removeMutation.mutate(item.asset.id)} onUseAsReference={() => referenceMutation.mutate(item)} />)}</div></div>}

      {pickerOpen ? (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/55 p-3 sm:p-5" onMouseDown={(event) => event.target === event.currentTarget && !syncMutation.isPending && setPickerOpen(false)}>
          <div role="dialog" aria-modal="true" className="flex h-[min(720px,calc(100svh-1.5rem))] w-full max-w-3xl min-h-0 flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-[#0d131e]">
            <header className="flex shrink-0 items-center gap-3 border-b border-slate-200 px-4 py-3 dark:border-slate-800">
              <div className="min-w-0 flex-1">
                <h2 className="text-sm font-semibold text-slate-950 dark:text-white">{t("workflowV2.mediaLibrary.pickerTitle")}</h2>
                <p className="mt-0.5 text-[11px] text-slate-400">{t("workflowV2.mediaLibrary.pickerSelected", { count: selectedIds.size })}</p>
              </div>
              <button type="button" onClick={() => setPickerOpen(false)} disabled={syncMutation.isPending} className="inline-flex h-8 w-8 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800" aria-label={t("detail.library.close")}>
                <X size={16} />
              </button>
            </header>
            <div className="shrink-0 border-b border-slate-200 p-3 dark:border-slate-800">
              <label className="relative block">
                <span className="sr-only">{t("mediaLibrary.search")}</span>
                <Search size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-slate-400" />
                <input autoFocus value={pickerSearch} onChange={(event) => setPickerSearch(event.target.value)} placeholder={t("mediaLibrary.search")} className="h-9 w-full rounded-lg border border-slate-200 bg-slate-50 pl-8 pr-3 text-xs outline-none focus:border-indigo-400 dark:border-slate-700 dark:bg-slate-950/50 dark:text-white dark:focus:border-violet-400" />
              </label>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-3">
              {pickerQuery.isLoading ? (
                <PanelState icon={<Loader2 size={17} className="animate-spin" />} text={t("mediaLibrary.loading")} />
              ) : (pickerQuery.data?.items ?? []).filter((asset) => !linkedById.has(asset.id)).length === 0 ? (
                <PanelState text={t("workflowV2.mediaLibrary.pickerEmpty")} />
              ) : (
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
                  {(pickerQuery.data?.items ?? [])
                    .filter((asset) => !linkedById.has(asset.id))
                    .map((asset) => (
                      <PickerAssetCard
                        key={asset.id}
                        asset={asset}
                        selected={selectedIds.has(asset.id)}
                        onToggle={() => togglePickerAsset(asset.id)}
                        onPreview={() => preview(asset)}
                      />
                    ))}
                </div>
              )}
            </div>
            <footer className="flex shrink-0 items-center gap-2 border-t border-slate-200 bg-slate-50 px-4 py-3 dark:border-slate-800 dark:bg-slate-950/35">
              <span className="mr-auto text-xs text-slate-500 dark:text-slate-400">{t("workflowV2.mediaLibrary.pickerHint")}</span>
              <button type="button" onClick={() => setPickerOpen(false)} disabled={syncMutation.isPending} className="h-9 rounded-md border border-slate-200 px-3 text-xs font-semibold text-slate-600 dark:border-slate-700 dark:text-slate-300">
                {t("common.cancel")}
              </button>
              <button type="button" onClick={() => syncMutation.mutate([...selectedIds])} disabled={syncMutation.isPending || selectedIds.size === 0} className="inline-flex h-9 items-center gap-1.5 rounded-md bg-indigo-600 px-3 text-xs font-semibold text-white disabled:opacity-50 dark:bg-violet-500">
                {syncMutation.isPending ? <Loader2 size={13} className="animate-spin" /> : <Link2 size={13} />}
                {t("workflowV2.mediaLibrary.confirmAdd")}
              </button>
            </footer>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function LinkedAssetCard({ item, busy, canReference, onPreview, onRemove, onUseAsReference }: { item: WorkflowMediaLibraryAsset; busy: boolean; canReference: boolean; onPreview: () => void; onRemove: () => void; onUseAsReference: () => void }) {
  const { t } = useI18n();
  const readable = item.asset.verification_status === "verified";
  return (
    <article className="group min-w-0 overflow-hidden rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-950/35">
      <button type="button" onClick={onPreview} disabled={!readable} className="relative block aspect-square w-full overflow-hidden bg-slate-100 disabled:cursor-not-allowed dark:bg-slate-900">
        <img src={api.toApiUrl(item.asset.thumbnail_url)} alt={item.asset.display_name} className={`h-full w-full object-cover transition-transform duration-200 group-hover:scale-[1.02] ${readable ? "" : "opacity-50 grayscale"}`} />
        {!readable ? <span className="absolute inset-x-1 bottom-1 rounded bg-slate-950/70 px-1 py-1 text-[9px] text-white">{t("detail.library.mediaPending")}</span> : null}
      </button>
      <div className="min-w-0 p-2">
        <div className="truncate text-[11px] font-semibold text-slate-800 dark:text-slate-100" title={item.asset.display_name}>
          {item.asset.display_name}
        </div>
        <div className="mt-1 truncate text-[9px] text-slate-400">
          {item.asset.folder_name ?? t("mediaLibrary.unorganized")}
        </div>
        <div className="mt-2 flex items-center justify-end gap-1">
          <button type="button" onClick={onRemove} disabled={busy} className="inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-400 hover:bg-red-50 hover:text-red-600 disabled:opacity-40 dark:hover:bg-red-500/10 dark:hover:text-red-300" aria-label={t("workflowV2.mediaLibrary.remove")} title={t("workflowV2.mediaLibrary.remove")}>
            <Unlink size={13} />
          </button>
          {canReference ? (
            <button type="button" onClick={onUseAsReference} disabled={busy} className="inline-flex h-7 items-center gap-1 rounded-md border border-indigo-100 px-2 text-[10px] font-semibold text-indigo-700 hover:bg-indigo-50 disabled:opacity-40 dark:border-violet-400/25 dark:text-violet-200 dark:hover:bg-violet-500/10">
              <Link2 size={11} />{t("workflowV2.mediaLibrary.use")}
            </button>
          ) : null}
        </div>
      </div>
    </article>
  );
}

function PickerAssetCard({ asset, selected, onToggle, onPreview }: { asset: MediaLibraryAsset; selected: boolean; onToggle: () => void; onPreview: () => void }) {
  const { t } = useI18n();
  const readable = asset.verification_status === "verified";
  return (
    <article
      role="button"
      aria-pressed={selected}
      aria-disabled={!readable}
      aria-label={asset.display_name}
      tabIndex={readable ? 0 : -1}
      onClick={() => readable && onToggle()}
      onKeyDown={(event) => {
        if (!readable) return;
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onToggle();
        }
      }}
      className={`group relative flex cursor-pointer flex-col overflow-hidden rounded-lg border bg-white shadow-sm transition-all select-none hover:shadow-md focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-1 dark:bg-[#0d131e] ${
        selected
          ? "border-indigo-500 ring-2 ring-indigo-500/20 dark:border-violet-400 dark:ring-violet-500/25"
          : "border-slate-200 hover:border-slate-300 dark:border-slate-800 dark:hover:border-slate-700"
      }`}
    >
      <div className="relative aspect-square w-full bg-slate-100 dark:bg-slate-900">
        <img
          src={api.toApiUrl(asset.thumbnail_url)}
          alt={asset.display_name}
          className={`h-full w-full object-cover transition-transform duration-200 group-hover:scale-[1.02] ${
            readable ? "" : "opacity-50 grayscale"
          }`}
        />

        {/* 选框（视觉层；键盘/语义由卡片 role=button 承载） */}
        <button
          type="button"
          tabIndex={-1}
          onClick={(e) => {
            e.stopPropagation();
            if (readable) onToggle();
          }}
          disabled={!readable}
          aria-pressed={selected}
          aria-label={asset.display_name}
          className={`absolute left-2 top-2 z-10 flex h-6 w-6 items-center justify-center rounded-md border shadow-md backdrop-blur-md transition-all ${
            selected
              ? "border-indigo-600 bg-indigo-600 text-white opacity-100 dark:border-violet-500 dark:bg-violet-500"
              : "border-white/40 bg-slate-950/40 text-transparent opacity-75 group-hover:opacity-100 hover:border-white hover:bg-slate-950/70 hover:text-white/80 dark:border-white/30 dark:bg-slate-900/60"
          }`}
        >
          <Check size={13} strokeWidth={selected ? 2.5 : 2} />
        </button>

        {/* 悬浮预览按钮 */}
        {readable ? (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onPreview();
            }}
            className="absolute right-2 top-2 z-10 flex h-6 w-6 items-center justify-center rounded-md border border-white/30 bg-slate-950/50 text-white opacity-0 shadow-md backdrop-blur-md transition-all group-hover:opacity-100 hover:scale-105 hover:bg-slate-950/80 focus:opacity-100"
            aria-label={t("mediaLibrary.previewLabel")}
            title={t("mediaLibrary.previewLabel")}
          >
            <Maximize2 size={12} />
          </button>
        ) : null}
      </div>

      <div className="truncate px-2 py-1.5 text-[11px] font-semibold text-slate-800 dark:text-slate-200" title={asset.display_name}>
        {asset.display_name}
      </div>
    </article>
  );
}

function PanelState({ icon, text, action, onAction }: { icon?: React.ReactNode; text: string; action?: string; onAction?: () => void }) {
  return (
    <div className="flex min-h-[220px] flex-col items-center justify-center gap-2 px-5 text-center text-xs text-slate-500 dark:text-slate-400">
      {icon ? <span className="text-slate-400">{icon}</span> : null}
      <span>{text}</span>
      {action && onAction ? (
        <button type="button" onClick={onAction} className="font-semibold text-indigo-600 hover:text-indigo-800 dark:text-violet-300">
          {action}
        </button>
      ) : null}
    </div>
  );
}
