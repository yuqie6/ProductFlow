import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Download, FolderInput, Loader2, Package, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { ConfirmDialog } from "../../../../components/ConfirmDialog";
import { Button } from "../../../../components/ui/button";
import { Dialog, DialogContent } from "../../../../components/ui/dialog";
import { Input } from "../../../../components/ui/field";
import { IconButton } from "../../../../components/ui/icon-button";
import { Select } from "../../../../components/ui/select";
import { api, ApiError } from "../../../../lib/api";
import type { DownloadableImage } from "../../../../lib/image-downloads";
import { sanitizeFilenamePart } from "../../../../lib/image-downloads";
import { useI18n } from "../../../../lib/preferences";
import type {
  GalleryAsset,
  GalleryDirectorySelection,
  GalleryFolder,
} from "../../../../lib/types";
import type { LocalImageEditOpenRequest } from "../../local-edit/LocalImageEditController";
import { ImageAssetGrid, ImageAssetList } from "./ImageAssetGrid";
import { ImageDirectoryTree } from "./ImageDirectoryTree";
import { ImageExplorerToolbar } from "./ImageExplorerToolbar";
import {
  evaluateDeliveryExportSelection,
  type DeliveryExportEligibility,
  type DeliveryExportEligibilityReason,
} from "./deliveryExport";
import { assetCanReadMedia, isWideImageExplorer } from "./explorerState";
import { toggleImageExplorerTargetAsset } from "./selectionTarget";
import { useProductImageExplorer } from "./useProductImageExplorer";

const UNORGANIZED_FOLDER_VALUE = "__none__";

export interface ImageExplorerReferenceTarget {
  bindAsset: (asset: Pick<GalleryAsset, "id">) => Promise<unknown>;
  onBound?: () => void;
}

export interface ImageExplorerSelectionTarget {
  selectedAssets: readonly GalleryAsset[];
  maxSelected: number;
  confirmLabel: string;
  selectionLabel: (count: number, maximum: number) => string;
  limitMessage: string;
  onConfirm: (assets: GalleryAsset[]) => void;
}

interface ProductImageExplorerProps {
  productId: string;
  productName: string;
  onPreviewImage: (image: DownloadableImage) => void;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
  referenceTarget?: ImageExplorerReferenceTarget;
  selectionTarget?: ImageExplorerSelectionTarget;
}

type ExplorerDialog =
  | { kind: "create-folder" }
  | { kind: "rename-folder"; folder: GalleryFolder }
  | { kind: "delete-folder"; folder: GalleryFolder }
  | { kind: "rename-asset"; asset: GalleryAsset }
  | { kind: "move-assets"; assets: GalleryAsset[] }
  | null;

interface DeliveryExportButtonProps {
  eligibility: DeliveryExportEligibility;
  busy: boolean;
  exporting: boolean;
  label: string;
  exportingLabel: string;
  reasonLabel: string | null;
  onExport: (jobIds: readonly string[]) => void;
}

export function DeliveryExportButton({
  eligibility,
  busy,
  exporting,
  label,
  exportingLabel,
  reasonLabel,
  onExport,
}: DeliveryExportButtonProps) {
  const reasonId = "delivery-export-selection-reason";
  return (
    <>
      <Button
        type="button"
        size="sm"
        className="h-7 px-2"
        onClick={() => {
          if (eligibility.eligible) {
            onExport(eligibility.renditionJobIds);
          }
        }}
        disabled={!eligibility.eligible || busy}
        busy={exporting}
        aria-describedby={reasonLabel ? reasonId : undefined}
        title={!eligibility.eligible ? (reasonLabel ?? undefined) : undefined}
        data-testid="delivery-export-button"
      >
        {exporting ? null : <Package size={13} />}
        {exporting ? exportingLabel : label}
      </Button>
      {reasonLabel ? (
        <span id={reasonId} data-testid="delivery-export-reason" className="basis-full text-[10px] text-state-warning">
          {reasonLabel}
        </span>
      ) : null}
    </>
  );
}

function deliveryExportReasonKey(reason: DeliveryExportEligibilityReason):
  | "detail.library.deliveryExportReasonEmpty"
  | "detail.library.deliveryExportReasonNotRendition"
  | "detail.library.deliveryExportReasonNotSucceeded"
  | "detail.library.deliveryExportReasonMissingJob"
  | "detail.library.deliveryExportReasonDuplicateJob" {
  switch (reason) {
    case "empty":
      return "detail.library.deliveryExportReasonEmpty";
    case "not_rendition":
      return "detail.library.deliveryExportReasonNotRendition";
    case "not_succeeded":
      return "detail.library.deliveryExportReasonNotSucceeded";
    case "missing_job_id":
      return "detail.library.deliveryExportReasonMissingJob";
    case "duplicate_job_id":
      return "detail.library.deliveryExportReasonDuplicateJob";
  }
}

export function ProductImageExplorer({
  productId,
  productName,
  onPreviewImage,
  onOpenLocalEdit,
  referenceTarget,
  selectionTarget,
}: ProductImageExplorerProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const rootRef = useRef<HTMLDivElement | null>(null);
  const [wide, setWide] = useState(false);
  const [directoryOpen, setDirectoryOpen] = useState(false);
  const [dialog, setDialog] = useState<ExplorerDialog>(null);
  const [targetAssetsById, setTargetAssetsById] = useState<Map<string, GalleryAsset>>(
    () => new Map(selectionTarget?.selectedAssets.map((asset) => [asset.id, asset]) ?? []),
  );
  const [selectionError, setSelectionError] = useState<string | null>(null);
  const explorer = useProductImageExplorer(productId);
  const bootstrap = explorer.bootstrapQuery.data ?? null;

  useEffect(() => {
    const element = rootRef.current;
    if (!element || typeof ResizeObserver === "undefined") {
      return;
    }
    const observer = new ResizeObserver(([entry]) => {
      setWide(isWideImageExplorer(entry.contentRect.width));
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [bootstrap]);

  const referenceMutation = useMutation({
    mutationFn: (asset: GalleryAsset) => {
      if (!referenceTarget) {
        throw new Error("reference target is required");
      }
      return referenceTarget.bindAsset(asset);
    },
    onSuccess: async () => {
      referenceTarget?.onBound?.();
      await queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
    },
  });
  const sourceMutation = useMutation({
    mutationFn: (asset: GalleryAsset) => {
      if (!asset.rendition) throw new Error(t("detail.library.sourceUnavailable"));
      return api.getGalleryAsset(productId, asset.rendition.source_asset_id);
    },
    onSuccess: (asset) => onPreviewImage(toDownloadableImage(asset)),
  });
  const deliveryExportMutation = useMutation({
    mutationFn: (renditionJobIds: string[]) => api.downloadDeliveryExport(productId, renditionJobIds, false),
  });

  const loadedAssetsById = useMemo(
    () => new Map(explorer.assets.map((asset) => [asset.id, asset])),
    [explorer.assets],
  );
  const targetSelectedAssets = useMemo(
    () => [...targetAssetsById.values()],
    [targetAssetsById],
  );
  const visibleSelectedIds = selectionTarget
    ? new Set(targetAssetsById.keys())
    : explorer.selectedIds;
  const toggleVisibleSelection = (assetId: string) => {
    if (!selectionTarget) {
      explorer.toggleSelected(assetId);
      return;
    }
    setTargetAssetsById((current) => {
      const next = new Map(current);
      if (next.has(assetId)) {
        next.delete(assetId);
        setSelectionError(null);
        return next;
      }
      const asset = loadedAssetsById.get(assetId);
      if (!asset) {
        return current;
      }
      const result = toggleImageExplorerTargetAsset(
        [...next.values()],
        asset,
        selectionTarget.maxSelected,
      );
      if (result.limitExceeded) {
        setSelectionError(selectionTarget.limitMessage);
        return current;
      }
      setSelectionError(null);
      return new Map(result.assets.map((item) => [item.id, item]));
    });
  };
  const operationBusy = [
    explorer.uploadMutation,
    explorer.createFolderMutation,
    explorer.renameFolderMutation,
    explorer.deleteFolderMutation,
    explorer.renameAssetMutation,
    explorer.moveAssetsMutation,
    explorer.archiveMutation,
    referenceMutation,
    sourceMutation,
    deliveryExportMutation,
  ].some((mutation) => mutation.isPending);
  const operationError = (deliveryExportMutation.error instanceof Error ? deliveryExportMutation.error : null)
    ?? explorer.operationError
    ?? (referenceMutation.error instanceof Error ? referenceMutation.error : null)
    ?? (sourceMutation.error instanceof Error ? sourceMutation.error : null)
    ?? (selectionError ? new Error(selectionError) : null);

  const currentDirectoryLabel = bootstrap
    ? directoryLabel(bootstrap, explorer.directory, t)
    : t("detail.library.all");
  const preview = (asset: GalleryAsset) => {
    if (!assetCanReadMedia(asset)) {
      return;
    }
    onPreviewImage({
      previewUrl: api.toApiUrl(asset.preview_url),
      downloadUrl: api.toApiUrl(asset.download_url),
      filename: asset.original_filename,
      alt: asset.display_name,
    });
  };
  const moveDroppedAssets = (assetIds: string[], folderId: string | null) => {
    const assets = assetIds.map((id) => loadedAssetsById.get(id)).filter((asset): asset is GalleryAsset => Boolean(asset));
    if (assets.length === assetIds.length) {
      explorer.moveAssetsMutation.mutate({ assets, folderId });
    }
  };
  const downloadSelected = async () => {
    try {
      const blob = await explorer.archiveMutation.mutateAsync(explorer.selectedAssets.map((asset) => asset.id));
      const url = window.URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `${sanitizeFilenamePart(productName, "product")}-images.zip`;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      window.URL.revokeObjectURL(url);
    } catch {
      // mutation 错误展示在当前目录旁
    }
  };
  const deliveryExportEligibility = useMemo(
    () => evaluateDeliveryExportSelection(explorer.selectedAssets),
    [explorer.selectedAssets],
  );
  const deliveryExportReason = deliveryExportEligibility.reason
    ? t(deliveryExportReasonKey(deliveryExportEligibility.reason))
    : null;
  const exportDeliveryPackage = async (renditionJobIds: readonly string[]) => {
    try {
      const blob = await deliveryExportMutation.mutateAsync([...renditionJobIds]);
      const url = window.URL.createObjectURL(blob);
      try {
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = `${sanitizeFilenamePart(productName, "product")}-delivery-export.zip`;
        document.body.append(anchor);
        anchor.click();
        anchor.remove();
      } finally {
        window.URL.revokeObjectURL(url);
      }
    } catch {
      // mutation 错误仍显示在现有操作提示里
    }
  };
  const selectionCanDownload =
    explorer.selectedAssets.length > 0 && explorer.selectedAssets.every(assetCanReadMedia);
  const dialogs = (
    <>
      <TextDialog
        open={dialog?.kind === "create-folder" || dialog?.kind === "rename-folder" || dialog?.kind === "rename-asset"}
        title={dialog?.kind === "create-folder"
          ? t("detail.library.createFolder")
          : dialog?.kind === "rename-folder"
            ? t("detail.library.renameFolder")
            : t("detail.library.renameAsset")}
        label={dialog?.kind === "rename-asset" ? t("detail.library.assetName") : t("detail.library.folderName")}
        initialValue={dialog?.kind === "rename-folder" ? dialog.folder.name : dialog?.kind === "rename-asset" ? dialog.asset.display_name : ""}
        maxLength={dialog?.kind === "rename-asset" ? 255 : 120}
        busy={operationBusy}
        onClose={() => setDialog(null)}
        onSubmit={(value) => {
          if (dialog?.kind === "create-folder") {
            explorer.createFolderMutation.mutate(value, { onSuccess: () => setDialog(null) });
          } else if (dialog?.kind === "rename-folder") {
            explorer.renameFolderMutation.mutate(
              { id: dialog.folder.id, expectedName: dialog.folder.name, name: value },
              { onSuccess: () => setDialog(null) },
            );
          } else if (dialog?.kind === "rename-asset") {
            explorer.renameAssetMutation.mutate(
              { asset: dialog.asset, displayName: value },
              { onSuccess: () => setDialog(null) },
            );
          }
        }}
      />
      <MoveAssetsDialog
        open={dialog?.kind === "move-assets"}
        folders={bootstrap?.user_folders ?? []}
        busy={explorer.moveAssetsMutation.isPending}
        onClose={() => setDialog(null)}
        onMove={(folderId) => {
          if (dialog?.kind === "move-assets") {
            explorer.moveAssetsMutation.mutate(
              { assets: dialog.assets, folderId },
              { onSuccess: () => setDialog(null) },
            );
          }
        }}
      />
      <ConfirmDialog
        open={dialog?.kind === "delete-folder"}
        title={t("detail.library.deleteFolder")}
        description={dialog?.kind === "delete-folder"
          ? t("detail.library.deleteFolderDescription", { name: dialog.folder.name, count: dialog.folder.count })
          : ""}
        confirmLabel={t("confirm.delete.confirm")}
        cancelLabel={t("common.cancel")}
        busy={explorer.deleteFolderMutation.isPending}
        onClose={() => setDialog(null)}
        onConfirm={() => {
          if (dialog?.kind === "delete-folder") {
            explorer.deleteFolderMutation.mutate(
              { id: dialog.folder.id, expectedName: dialog.folder.name },
              { onSuccess: () => setDialog(null) },
            );
          }
        }}
      />
    </>
  );

  if (explorer.bootstrapQuery.isLoading) {
    return <ExplorerState icon={<Loader2 size={17} className="animate-spin" />} text={t("detail.library.loading")} />;
  }
  if (explorer.bootstrapQuery.isError || !bootstrap) {
    return (
      <ExplorerState
        text={explorer.bootstrapQuery.error instanceof ApiError ? explorer.bootstrapQuery.error.detail : t("detail.library.operationFailed")}
        action={t("detail.library.retry")}
        onAction={() => explorer.bootstrapQuery.refetch()}
      />
    );
  }

  return (
    <div ref={rootRef} className="min-w-0" data-image-explorer-wide={wide ? "true" : "false"}>
      <ImageExplorerToolbar
        directoryLabel={currentDirectoryLabel}
        search={explorer.searchInput}
        sort={explorer.sort}
        view={explorer.view}
        directoryOpen={directoryOpen}
        showDirectoryToggle={!wide}
        busy={operationBusy}
        onSearchChange={explorer.setSearchInput}
        onSortChange={explorer.setSort}
        onViewChange={explorer.setView}
        onToggleDirectory={() => setDirectoryOpen((value) => !value)}
        onCreateFolder={() => setDialog({ kind: "create-folder" })}
        onUpload={(files) => explorer.uploadMutation.mutate(files)}
      />

      <div className={wide ? "grid grid-cols-[148px_minmax(0,1fr)] gap-3 pt-3" : "pt-3"}>
        {wide || directoryOpen ? (
          <div className={wide ? "min-w-0 border-r border-border-l1 pr-2" : "mb-3 border-b border-border-l1 pb-3"}>
            <ImageDirectoryTree
              bootstrap={bootstrap}
              directory={explorer.directory}
              onSelect={(next) => {
                explorer.setDirectory(next);
                if (!wide) {
                  setDirectoryOpen(false);
                }
              }}
              onCreateFolder={() => setDialog({ kind: "create-folder" })}
              onRenameFolder={(folder) => setDialog({ kind: "rename-folder", folder })}
              onDeleteFolder={(folder) => setDialog({ kind: "delete-folder", folder })}
              onDropAssets={moveDroppedAssets}
              busy={operationBusy}
            />
          </div>
        ) : null}

        <div className="min-w-0">
          {operationError ? (
            <div role="alert" className="mb-2 rounded-control border border-state-error/30 bg-state-error-soft px-2.5 py-2 text-xs text-state-error">
              {operationError instanceof ApiError ? operationError.detail : operationError.message}
            </div>
          ) : null}

          {selectionTarget && targetAssetsById.size ? (
            <div className="mb-2 flex min-h-9 flex-wrap items-center gap-1.5 rounded-control border border-accent/30 bg-accent-soft px-2 py-1.5 text-xs text-accent">
              <span className="mr-auto font-semibold">
                {selectionTarget.selectionLabel(targetAssetsById.size, selectionTarget.maxSelected)}
              </span>
              <Button
                type="button"
                size="sm"
                variant="primary"
                onClick={() => selectionTarget.onConfirm(targetSelectedAssets)}
              >
                {selectionTarget.confirmLabel}
              </Button>
              <IconButton
                label={t("detail.library.clearSelection")}
                size="sm"
                className="h-11 w-11 lg:h-8 lg:w-8"
                onClick={() => {
                  setTargetAssetsById(new Map());
                  setSelectionError(null);
                }}
              >
                <X size={13} />
              </IconButton>
            </div>
          ) : explorer.selectedIds.size ? (
            <div className="mb-2 flex min-h-9 flex-wrap items-center gap-1.5 rounded-control border border-accent/30 bg-accent-soft px-2 py-1.5 text-xs text-accent">
              <span className="mr-auto font-semibold">{t("detail.library.selected", { count: explorer.selectedIds.size })}</span>
              <Button type="button" size="sm" className="px-2" onClick={() => setDialog({ kind: "move-assets", assets: explorer.selectedAssets })}>
                <FolderInput size={13} /> {t("detail.library.move")}
              </Button>
              <Button
                type="button"
                size="sm"
                className="h-7 px-2"
                onClick={() => void downloadSelected()}
                disabled={!selectionCanDownload || explorer.archiveMutation.isPending}
                busy={explorer.archiveMutation.isPending}
              >
                {explorer.archiveMutation.isPending ? null : <Download size={13} />}
                {t("detail.library.downloadZip")}
              </Button>
              <DeliveryExportButton
                eligibility={deliveryExportEligibility}
                busy={operationBusy}
                exporting={deliveryExportMutation.isPending}
                label={t("detail.library.deliveryExport")}
                exportingLabel={t("detail.library.deliveryExporting")}
                reasonLabel={deliveryExportReason}
                onExport={(jobIds) => void exportDeliveryPackage(jobIds)}
              />
              <IconButton
                label={t("detail.library.clearSelection")}
                size="sm"
                className="h-11 w-11 lg:h-7 lg:w-7"
                onClick={explorer.clearSelection}
              >
                <X size={13} />
              </IconButton>
            </div>
          ) : explorer.assets.length && !selectionTarget ? (
            <button type="button" onClick={explorer.selectAllLoaded} className="mb-2 text-[10px] font-medium text-text-muted hover:text-accent">
              {t("detail.library.selectAll")}
            </button>
          ) : null}

          {explorer.assetsQuery.isLoading ? (
            <ExplorerState icon={<Loader2 size={17} className="animate-spin" />} text={t("detail.library.loading")} />
          ) : explorer.assetsQuery.isError ? (
            <ExplorerState
              text={explorer.assetsQuery.error instanceof ApiError ? explorer.assetsQuery.error.detail : t("detail.library.operationFailed")}
              action={t("detail.library.retry")}
              onAction={() => explorer.assetsQuery.refetch()}
            />
          ) : explorer.assets.length === 0 ? (
            <ExplorerState text={t("detail.library.empty")} />
          ) : explorer.view === "grid" ? (
            <ImageAssetGrid
              assets={explorer.assets}
              selectedIds={visibleSelectedIds}
              onToggleSelected={toggleVisibleSelection}
              onPreview={preview}
              onOpenLocalEdit={onOpenLocalEdit}
              onRename={(asset) => setDialog({ kind: "rename-asset", asset })}
              onMove={(asset) => setDialog({ kind: "move-assets", assets: [asset] })}
              onUseAsReference={referenceTarget ? (asset) => referenceMutation.mutate(asset) : undefined}
              onViewSource={(asset) => sourceMutation.mutate(asset)}
              referenceBusy={referenceMutation.isPending}
              sourceBusy={sourceMutation.isPending}
            />
          ) : (
            <ImageAssetList
              assets={explorer.assets}
              selectedIds={visibleSelectedIds}
              onToggleSelected={toggleVisibleSelection}
              onPreview={preview}
              onOpenLocalEdit={onOpenLocalEdit}
              onRename={(asset) => setDialog({ kind: "rename-asset", asset })}
              onMove={(asset) => setDialog({ kind: "move-assets", assets: [asset] })}
              onUseAsReference={referenceTarget ? (asset) => referenceMutation.mutate(asset) : undefined}
              onViewSource={(asset) => sourceMutation.mutate(asset)}
              referenceBusy={referenceMutation.isPending}
              sourceBusy={sourceMutation.isPending}
            />
          )}

          {explorer.assetsQuery.hasNextPage ? (
            <Button
              type="button"
              variant="secondary"
              onClick={() => explorer.assetsQuery.fetchNextPage()}
              disabled={explorer.assetsQuery.isFetchingNextPage}
              busy={explorer.assetsQuery.isFetchingNextPage}
              className="mt-3 w-full"
            >
              {t("detail.library.loadMore")}
            </Button>
          ) : null}
        </div>
      </div>

      {typeof document === "undefined" ? dialogs : createPortal(dialogs, document.body)}
    </div>
  );
}

function toDownloadableImage(asset: GalleryAsset): DownloadableImage {
  return {
    previewUrl: api.toApiUrl(asset.preview_url),
    downloadUrl: api.toApiUrl(asset.download_url),
    filename: asset.original_filename,
    alt: asset.display_name,
  };
}

function directoryLabel(
  bootstrap: NonNullable<ReturnType<typeof useProductImageExplorer>["bootstrapQuery"]["data"]>,
  directory: GalleryDirectorySelection,
  t: ReturnType<typeof useI18n>["t"],
): string {
  switch (directory.kind) {
    case "all": return t("detail.library.all");
    case "recent_generated": return t("detail.library.recent");
    case "uploads": return t("detail.library.uploads");
    case "generated": return t("detail.library.generated");
    case "unorganized": return t("detail.library.unorganized");
    case "image_type": return bootstrap.image_types.find((item) => item.directory_key === directory.key)?.title ?? t("detail.library.types");
    case "source": {
      const key = bootstrap.origins.find((item) => item.origin_type === directory.key)?.origin_type;
      if (key === "upload") return t("detail.library.source.upload");
      if (key === "workflow_generation") return t("detail.library.source.workflow");
      if (key === "image_session_attach") return t("detail.library.source.session");
      return t("detail.library.sources");
    }
    case "user_folder": return bootstrap.user_folders.find((folder) => folder.id === directory.key)?.name ?? t("detail.library.folders");
  }
}

function ExplorerState({ icon, text, action, onAction }: { icon?: React.ReactNode; text: string; action?: string; onAction?: () => void }) {
  return (
    <div className="flex min-h-[160px] flex-col items-center justify-center gap-2 rounded-panel border border-dashed border-border-l1 px-4 text-center text-xs text-text-muted">
      {icon}
      <span>{text}</span>
      {action && onAction ? (
        <Button type="button" variant="ghost" size="sm" onClick={onAction} className="mt-1 font-semibold text-accent hover:text-accent-strong">
          {action}
        </Button>
      ) : null}
    </div>
  );
}

function TextDialog({
  open,
  title,
  label,
  initialValue,
  maxLength,
  busy,
  onClose,
  onSubmit,
}: {
  open: boolean;
  title: string;
  label: string;
  initialValue: string;
  maxLength: number;
  busy: boolean;
  onClose: () => void;
  onSubmit: (value: string) => void;
}) {
  const { t } = useI18n();
  const [value, setValue] = useState(initialValue);
  useEffect(() => {
    if (open) setValue(initialValue);
  }, [initialValue, open]);
  const normalized = value.trim();
  const submit = () => {
    if (normalized && !busy) onSubmit(normalized);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !busy) onClose();
      }}
    >
      <DialogContent
        title={title}
        size="sm"
        closeLabel={t("workbench.dialog.close")}
        footer={(
          <>
            <Button variant="secondary" onClick={onClose} disabled={busy}>{t("common.cancel")}</Button>
            <Button variant="primary" onClick={submit} disabled={!normalized} busy={busy}>
              {t("detail.library.save")}
            </Button>
          </>
        )}
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            submit();
          }}
        >
          <Input
            label={label}
            value={value}
            onChange={(event) => setValue(event.target.value)}
            maxLength={maxLength}
            autoFocus
            disabled={busy}
          />
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MoveAssetsDialog({
  open,
  folders,
  busy,
  onClose,
  onMove,
}: {
  open: boolean;
  folders: GalleryFolder[];
  busy: boolean;
  onClose: () => void;
  onMove: (folderId: string | null) => void;
}) {
  const { t } = useI18n();
  const [folderId, setFolderId] = useState(UNORGANIZED_FOLDER_VALUE);
  useEffect(() => {
    if (open) setFolderId(UNORGANIZED_FOLDER_VALUE);
  }, [open]);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !busy) onClose();
      }}
    >
      <DialogContent
        title={t("detail.library.moveTitle")}
        size="sm"
        closeLabel={t("workbench.dialog.close")}
        footer={(
          <>
            <Button variant="secondary" onClick={onClose} disabled={busy}>{t("common.cancel")}</Button>
            <Button
              variant="primary"
              busy={busy}
              onClick={() => onMove(folderId === UNORGANIZED_FOLDER_VALUE ? null : folderId)}
            >
              {t("detail.library.move")}
            </Button>
          </>
        )}
      >
        <Select
          value={folderId}
          onChange={setFolderId}
          disabled={busy}
          ariaLabel={t("detail.library.moveTitle")}
          options={[
            { value: UNORGANIZED_FOLDER_VALUE, label: t("detail.library.moveUnorganized") },
            ...folders.map((folder) => ({ value: folder.id, label: folder.name })),
          ]}
        />
      </DialogContent>
    </Dialog>
  );
}
