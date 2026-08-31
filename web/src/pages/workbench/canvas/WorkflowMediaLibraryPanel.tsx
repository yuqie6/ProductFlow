import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, FolderPlus, Images, Link2, Maximize2, Search, Unlink } from "lucide-react";
import { useEffect, useState } from "react";

import { api, ApiError } from "../../../lib/api";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent } from "../../../components/ui/dialog";
import { EmptyState as PanelState } from "../../../components/ui/empty-state";
import { PanelSkeleton } from "../../../components/ui/skeleton";
import { Input } from "../../../components/ui/field";
import { IconButton } from "../../../components/ui/icon-button";
import { Tooltip } from "../../../components/ui/tooltip";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { MediaLibraryAsset, WorkflowMediaLibraryAsset } from "../../../lib/types";
import { IMAGE_EXPLORER_DRAG_MIME, encodeAssetDragPayload } from "../chrome/image-explorer/explorerState";
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
      await queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
    },
  });
  const removeMutation = useMutation({
    mutationFn: (assetId: string) => api.removeWorkflowMediaLibraryAsset(productId, workflowId, assetId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["workflow-media-library", productId, workflowId] }),
  });
  const referenceMutation = useMutation({
    mutationFn: (asset: WorkflowMediaLibraryAsset) => {
      if (!referenceTarget?.bindAsset || !asset.product_image_asset_id) {
        throw new Error(t("workbench.reference.unavailable"));
      }
      return referenceTarget.bindAsset({ id: asset.product_image_asset_id });
    },
    onSuccess: async () => {
      referenceTarget?.onBound?.();
      await queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
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
      <div className="flex shrink-0 items-center gap-2 border-b border-border-l1 px-3 py-2">
        <div className="min-w-0 flex-1">
          <div className="text-xs font-semibold text-text-primary">{t("workbench.mediaLibrary.title")}</div>
          <div className="mt-0.5 text-[10px] text-text-muted">{t("workbench.mediaLibrary.count", { count: linked.length })}</div>
        </div>
        <Button variant="primary" size="sm" onClick={() => setPickerOpen(true)}>
          <FolderPlus size={13} aria-hidden="true" />
          {t("workbench.mediaLibrary.add")}
        </Button>
      </div>

      {error ? (
        <div role="alert" className="m-3 rounded-md border border-state-error/30 bg-state-error-soft px-2.5 py-2 text-xs text-state-error">
          {error instanceof ApiError ? error.detail : error.message}
        </div>
      ) : null}
      {linkedQuery.isLoading ? (
        <PanelSkeleton rows={4} label={t("workbench.mediaLibrary.loading")} />
      ) : linked.length === 0 ? (
        <PanelState icon={<Images size={20} />} text={t("workbench.mediaLibrary.empty")} action={t("workbench.mediaLibrary.add")} onAction={() => setPickerOpen(true)} />
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto p-3">
          <div className="grid grid-cols-2 gap-2">
            {linked.map((item) => (
              <LinkedAssetCard
                key={item.asset.id}
                item={item}
                busy={busy}
                canReference={Boolean(referenceTarget?.bindAsset && item.product_image_asset_id)}
                onPreview={() => preview(item.asset)}
                onRemove={() => removeMutation.mutate(item.asset.id)}
                onUseAsReference={() => referenceMutation.mutate(item)}
              />
            ))}
          </div>
        </div>
      )}

      <Dialog
        open={pickerOpen}
        onOpenChange={(open) => {
          if (!open && !syncMutation.isPending) setPickerOpen(false);
        }}
      >
        <DialogContent
          title={t("workbench.mediaLibrary.pickerTitle")}
          description={t("workbench.mediaLibrary.pickerSelected", { count: selectedIds.size })}
          size="xl"
          className="flex max-h-[min(720px,calc(100svh-1.5rem))] flex-col"
          bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden p-0"
          closeLabel={t("detail.library.close")}
          footer={(
            <>
              <span className="mr-auto text-xs text-text-muted">{t("workbench.mediaLibrary.pickerHint")}</span>
              <Button variant="secondary" onClick={() => setPickerOpen(false)} disabled={syncMutation.isPending}>{t("common.cancel")}</Button>
              <Button
                variant="primary"
                busy={syncMutation.isPending}
                disabled={selectedIds.size === 0}
                onClick={() => syncMutation.mutate([...selectedIds])}
              >
                {t("workbench.mediaLibrary.confirmAdd")}
              </Button>
            </>
          )}
        >
          <div className="shrink-0 border-b border-border-l1 p-3">
            <div className="relative">
              <Search size={14} className="pointer-events-none absolute inset-y-0 left-2.5 my-auto text-text-muted" aria-hidden="true" />
              <Input
                autoFocus
                value={pickerSearch}
                onChange={(event) => setPickerSearch(event.target.value)}
                placeholder={t("mediaLibrary.search")}
                aria-label={t("mediaLibrary.search")}
                className="pl-8"
              />
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-3">
            {pickerQuery.isLoading ? (
              <PanelSkeleton compact rows={4} label={t("mediaLibrary.loading")} />
            ) : (pickerQuery.data?.items ?? []).filter((asset) => !linkedById.has(asset.id)).length === 0 ? (
              <PanelState text={t("workbench.mediaLibrary.pickerEmpty")} />
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
        </DialogContent>
      </Dialog>
    </div>
  );
}

function LinkedAssetCard({ item, busy, canReference, onPreview, onRemove, onUseAsReference }: { item: WorkflowMediaLibraryAsset; busy: boolean; canReference: boolean; onPreview: () => void; onRemove: () => void; onUseAsReference: () => void }) {
  const { t } = useI18n();
  const readable = item.asset.verification_status === "verified";
  const productAssetId = item.product_image_asset_id;
  return (
    <article
      className="group min-w-0 overflow-hidden rounded-lg border border-border-l1 bg-surface-raised"
      draggable={Boolean(readable && productAssetId)}
      onDragStart={(event) => {
        if (!productAssetId) return;
        event.dataTransfer.setData(IMAGE_EXPLORER_DRAG_MIME, encodeAssetDragPayload([productAssetId]));
        event.dataTransfer.effectAllowed = "copyMove";
      }}
    >
      <button type="button" onClick={onPreview} disabled={!readable} className="relative block aspect-square w-full overflow-hidden bg-surface-subtle disabled:cursor-not-allowed">
        <img src={api.toApiUrl(item.asset.thumbnail_url)} alt={item.asset.display_name} className={`h-full w-full object-cover transition-transform duration-200 group-hover:scale-[1.02] ${readable ? "" : "opacity-50 grayscale"}`} />
        {!readable ? <span className="absolute inset-x-1 bottom-1 rounded bg-surface-inverse/70 px-1 py-1 text-[9px] text-surface-raised">{t("detail.library.mediaPending")}</span> : null}
      </button>
      <div className="min-w-0 p-2">
        <Tooltip content={item.asset.display_name}>
          <div className="truncate text-[11px] font-semibold text-text-primary">
            {item.asset.display_name}
          </div>
        </Tooltip>
        <div className="mt-1 truncate text-[9px] text-text-muted">
          {item.asset.folder_name ?? t("mediaLibrary.unorganized")}
        </div>
        <div className="mt-2 flex items-center justify-end gap-1">
          <IconButton
            variant="danger"
            size="sm"
            label={t("workbench.mediaLibrary.remove")}
            onClick={onRemove}
            disabled={busy}
          >
            <Unlink size={13} aria-hidden="true" />
          </IconButton>
          {canReference ? (
            <Button variant="secondary" size="sm" onClick={onUseAsReference} disabled={busy}>
              <Link2 size={11} aria-hidden="true" />
              {t("workbench.mediaLibrary.use")}
            </Button>
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
      className={`group relative flex cursor-pointer flex-col overflow-hidden rounded-lg border bg-surface-raised shadow-sm transition-colors select-none hover:shadow-md focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-1 ${selected
          ? "border-accent ring-2 ring-accent/20"
          : "border-border-l1 hover:border-border-l3"
        }`}
    >
      <div className="relative aspect-square w-full bg-surface-subtle">
        <img
          src={api.toApiUrl(asset.thumbnail_url)}
          alt={asset.display_name}
          className={`h-full w-full object-cover transition-transform duration-200 group-hover:scale-[1.02] ${readable ? "" : "opacity-50 grayscale"
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
          className={`absolute left-2 top-2 z-10 flex h-6 w-6 items-center justify-center rounded-md border shadow-md backdrop-blur-md transition-colors ${selected
              ? "border-accent bg-accent text-accent-fg opacity-100"
              : "border-surface-raised/40 bg-surface-inverse/40 text-transparent opacity-75 group-hover:opacity-100 hover:border-surface-raised hover:bg-surface-inverse/70 hover:text-surface-raised/80"
            }`}
        >
          <Check size={13} strokeWidth={selected ? 2.5 : 2} />
        </button>

        {/* 悬浮预览按钮 */}
        {readable ? (
          <IconButton
            variant="ghost"
            size="sm"
            label={t("mediaLibrary.previewLabel")}
            onClick={(e) => {
              e.stopPropagation();
              onPreview();
            }}
            className="absolute right-2 top-2 z-10 border-surface-raised/30 bg-surface-inverse/50 text-surface-raised opacity-0 shadow-md backdrop-blur-md hover:bg-surface-inverse/80 hover:text-surface-raised focus:opacity-100 group-hover:opacity-100"
          >
            <Maximize2 size={12} aria-hidden="true" />
          </IconButton>
        ) : null}
      </div>

      <Tooltip content={asset.display_name}>
        <div className="truncate px-2 py-1.5 text-[11px] font-semibold text-text-primary">
          {asset.display_name}
        </div>
      </Tooltip>
    </article>
  );
}
