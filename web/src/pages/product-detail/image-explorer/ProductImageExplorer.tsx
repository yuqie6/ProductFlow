import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Download, FolderInput, Loader2, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { api, ApiError } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { sanitizeFilenamePart } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type {
  GalleryAsset,
  GalleryDirectorySelection,
  GalleryFolder,
  WorkflowReferenceBindingResult,
} from "../../../lib/types";
import { ImageAssetGrid, ImageAssetList } from "./ImageAssetGrid";
import { ImageDirectoryTree } from "./ImageDirectoryTree";
import { ImageExplorerToolbar } from "./ImageExplorerToolbar";
import { assetCanReadMedia, isWideImageExplorer } from "./explorerState";
import { toggleImageExplorerTargetAsset } from "./selectionTarget";
import { useProductImageExplorer } from "./useProductImageExplorer";

export interface ImageExplorerReferenceTarget {
  workflowId: string;
  nodeId: string;
  expectedWorkflowRevision: number;
  expectedBoundAssetId: string | null;
  onBound?: (result: WorkflowReferenceBindingResult) => void;
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

export function ProductImageExplorer({
  productId,
  productName,
  onPreviewImage,
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
      return api.bindWorkflowReferenceAsset(
        productId,
        referenceTarget.workflowId,
        referenceTarget.nodeId,
        {
          asset_id: asset.id,
          expected_workflow_revision: referenceTarget.expectedWorkflowRevision,
          expected_bound_asset_id: referenceTarget.expectedBoundAssetId,
        },
      );
    },
    onSuccess: async (result) => {
      referenceTarget?.onBound?.(result);
      await queryClient.invalidateQueries({ queryKey: ["active-product-workflow-v2", productId] });
    },
  });
  const sourceMutation = useMutation({
    mutationFn: (asset: GalleryAsset) => {
      if (!asset.rendition) throw new Error(t("detail.library.sourceUnavailable"));
      return api.getGalleryAsset(productId, asset.rendition.source_asset_id);
    },
    onSuccess: (asset) => onPreviewImage(toDownloadableImage(asset)),
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
  ].some((mutation) => mutation.isPending);
  const operationError = explorer.operationError
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
      // The mutation error is rendered beside the current directory.
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
          <div className={wide ? "min-w-0 border-r border-slate-200 pr-2 dark:border-slate-800" : "mb-3 border-b border-slate-200 pb-3 dark:border-slate-800"}>
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
            <div role="alert" className="mb-2 rounded-md border border-red-200 bg-red-50 px-2.5 py-2 text-xs text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
              {operationError instanceof ApiError ? operationError.detail : operationError.message}
            </div>
          ) : null}

          {selectionTarget && targetAssetsById.size ? (
            <div className="mb-2 flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-blue-200 bg-blue-50 px-2 py-1.5 text-xs text-blue-800 dark:border-cyan-400/35 dark:bg-cyan-400/10 dark:text-cyan-100">
              <span className="mr-auto font-semibold">
                {selectionTarget.selectionLabel(targetAssetsById.size, selectionTarget.maxSelected)}
              </span>
              <button
                type="button"
                onClick={() => selectionTarget.onConfirm(targetSelectedAssets)}
                className="inline-flex h-8 items-center rounded-md bg-blue-600 px-3 font-semibold text-white hover:bg-blue-700 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300"
              >
                {selectionTarget.confirmLabel}
              </button>
              <button
                type="button"
                onClick={() => {
                  setTargetAssetsById(new Map());
                  setSelectionError(null);
                }}
                className="inline-flex h-8 w-8 items-center justify-center rounded-md hover:bg-white dark:hover:bg-slate-950/70"
                aria-label={t("detail.library.clearSelection")}
                title={t("detail.library.clearSelection")}
              >
                <X size={13} />
              </button>
            </div>
          ) : explorer.selectedIds.size ? (
            <div className="mb-2 flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-indigo-200 bg-indigo-50 px-2 py-1.5 text-xs text-indigo-800 dark:border-violet-400/35 dark:bg-violet-500/10 dark:text-violet-100">
              <span className="mr-auto font-semibold">{t("detail.library.selected", { count: explorer.selectedIds.size })}</span>
              <button type="button" onClick={() => setDialog({ kind: "move-assets", assets: explorer.selectedAssets })} className="inline-flex h-7 items-center gap-1 rounded bg-white px-2 font-medium shadow-sm dark:bg-slate-950/70">
                <FolderInput size={13} /> {t("detail.library.move")}
              </button>
              <button type="button" onClick={() => void downloadSelected()} disabled={!selectionCanDownload || explorer.archiveMutation.isPending} className="inline-flex h-7 items-center gap-1 rounded bg-white px-2 font-medium shadow-sm disabled:opacity-40 dark:bg-slate-950/70">
                {explorer.archiveMutation.isPending ? <Loader2 size={13} className="animate-spin" /> : <Download size={13} />}
                {t("detail.library.downloadZip")}
              </button>
              <button type="button" onClick={explorer.clearSelection} className="inline-flex h-7 w-7 items-center justify-center rounded hover:bg-white dark:hover:bg-slate-950/70" aria-label={t("detail.library.clearSelection")} title={t("detail.library.clearSelection")}>
                <X size={13} />
              </button>
            </div>
          ) : explorer.assets.length && !selectionTarget ? (
            <button type="button" onClick={explorer.selectAllLoaded} className="mb-2 text-[10px] font-medium text-slate-500 hover:text-indigo-700 dark:text-slate-400 dark:hover:text-violet-300">
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
              onRename={(asset) => setDialog({ kind: "rename-asset", asset })}
              onMove={(asset) => setDialog({ kind: "move-assets", assets: [asset] })}
              onUseAsReference={referenceTarget ? (asset) => referenceMutation.mutate(asset) : undefined}
              onViewSource={(asset) => sourceMutation.mutate(asset)}
              referenceBusy={referenceMutation.isPending}
              sourceBusy={sourceMutation.isPending}
            />
          )}

          {explorer.assetsQuery.hasNextPage ? (
            <button
              type="button"
              onClick={() => explorer.assetsQuery.fetchNextPage()}
              disabled={explorer.assetsQuery.isFetchingNextPage}
              className="mt-3 inline-flex h-9 w-full items-center justify-center rounded-md border border-slate-200 bg-white text-xs font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-950/50 dark:text-slate-200 dark:hover:bg-slate-900"
            >
              {explorer.assetsQuery.isFetchingNextPage ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : null}
              {t("detail.library.loadMore")}
            </button>
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
      return t("detail.library.source.legacy");
    }
    case "user_folder": return bootstrap.user_folders.find((folder) => folder.id === directory.key)?.name ?? t("detail.library.folders");
  }
}

function ExplorerState({ icon, text, action, onAction }: { icon?: React.ReactNode; text: string; action?: string; onAction?: () => void }) {
  return (
    <div className="flex min-h-[160px] flex-col items-center justify-center gap-2 rounded-md border border-dashed border-slate-200 px-4 text-center text-xs text-slate-500 dark:border-slate-700 dark:text-slate-400">
      {icon}
      <span>{text}</span>
      {action && onAction ? <button type="button" onClick={onAction} className="mt-1 font-semibold text-indigo-600 dark:text-violet-300">{action}</button> : null}
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
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-950/55 p-4" onMouseDown={(event) => event.target === event.currentTarget && !busy && onClose()}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const normalized = value.trim();
          if (normalized) onSubmit(normalized);
        }}
        className="w-full max-w-sm rounded-lg border border-slate-200 bg-white p-4 shadow-2xl dark:border-slate-700 dark:bg-slate-950"
      >
        <h2 className="text-sm font-semibold text-slate-950 dark:text-white">{title}</h2>
        <label className="mt-4 block text-xs font-medium text-slate-600 dark:text-slate-300">
          {label}
          <input autoFocus value={value} onChange={(event) => setValue(event.target.value)} maxLength={maxLength} className="mt-1.5 h-10 w-full rounded-md border border-slate-200 px-3 text-sm outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 dark:border-slate-700 dark:bg-slate-900 dark:text-white dark:focus:border-violet-400 dark:focus:ring-violet-500/15" />
        </label>
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" onClick={onClose} disabled={busy} className="h-9 rounded-md border border-slate-200 px-3 text-xs font-medium text-slate-600 dark:border-slate-700 dark:text-slate-300">{t("common.cancel")}</button>
          <button type="submit" disabled={busy || !value.trim()} className="inline-flex h-9 items-center rounded-md bg-indigo-600 px-3 text-xs font-semibold text-white disabled:opacity-50 dark:bg-violet-500">
            {busy ? <Loader2 size={13} className="mr-1.5 animate-spin" /> : null}{t("detail.library.save")}
          </button>
        </div>
      </form>
    </div>
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
  const [folderId, setFolderId] = useState("");
  useEffect(() => {
    if (open) setFolderId("");
  }, [open]);
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-950/55 p-4" onMouseDown={(event) => event.target === event.currentTarget && !busy && onClose()}>
      <div role="dialog" aria-modal="true" className="w-full max-w-sm rounded-lg border border-slate-200 bg-white p-4 shadow-2xl dark:border-slate-700 dark:bg-slate-950">
        <h2 className="text-sm font-semibold text-slate-950 dark:text-white">{t("detail.library.moveTitle")}</h2>
        <select value={folderId} onChange={(event) => setFolderId(event.target.value)} className="mt-4 h-10 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-white">
          <option value="">{t("detail.library.moveUnorganized")}</option>
          {folders.map((folder) => <option key={folder.id} value={folder.id}>{folder.name}</option>)}
        </select>
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" onClick={onClose} disabled={busy} className="h-9 rounded-md border border-slate-200 px-3 text-xs font-medium text-slate-600 dark:border-slate-700 dark:text-slate-300">{t("common.cancel")}</button>
          <button type="button" onClick={() => onMove(folderId || null)} disabled={busy} className="inline-flex h-9 items-center rounded-md bg-indigo-600 px-3 text-xs font-semibold text-white disabled:opacity-50 dark:bg-violet-500">
            {busy ? <Loader2 size={13} className="mr-1.5 animate-spin" /> : null}{t("detail.library.move")}
          </button>
        </div>
      </div>
    </div>
  );
}
