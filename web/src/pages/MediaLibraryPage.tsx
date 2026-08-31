import { useMutation, useQuery, useQueryClient, useInfiniteQuery } from "@tanstack/react-query";
import {
  Archive,
  ArchiveRestore,
  Check,
  FolderInput,
  FolderPlus,
  Grid2X2,
  Images,
  List,
  Loader2,
  Maximize2,
  Pencil,
  Plus,
  Search,
  Tag,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { GalleryImagePreviewDialog } from "../components/GalleryImagePreviewDialog";
import { TopNav } from "../components/TopNav";
import { Button } from "../components/ui/button";
import { Dialog, DialogContent } from "../components/ui/dialog";
import { Input } from "../components/ui/field";
import { Select } from "../components/ui/select";
import { useRegisterAgentPageContext } from "../lib/agentPageContext";
import { api, ApiError } from "../lib/api";
import { formatDateTime } from "../lib/format";
import { useI18n } from "../lib/preferences";
import type {
  MediaLibraryAsset,
  MediaLibraryFolder,
  MediaLibrarySourceType,
  MediaLibraryTag,
} from "../lib/types";

type LibraryView = "grid" | "list";
type LibraryDialog =
  | { kind: "create-folder" }
  | { kind: "rename-folder"; folder: MediaLibraryFolder }
  | { kind: "delete-folder"; folder: MediaLibraryFolder }
  | { kind: "create-tag" }
  | { kind: "rename-tag"; tag: MediaLibraryTag }
  | { kind: "delete-tag"; tag: MediaLibraryTag }
  | { kind: "move" }
  | { kind: "tags" }
  | { kind: "archive"; asset: MediaLibraryAsset }
  | { kind: "restore"; asset: MediaLibraryAsset }
  | { kind: "batch-archive"; assets: MediaLibraryAsset[] }
  | { kind: "batch-restore"; assets: MediaLibraryAsset[] }
  | null;

interface UploadErrorItem {
  filename: string;
  reason: string;
}

interface UploadProgressState {
  total: number;
  completed: number;
  success: number;
  failed: number;
  currentFileName?: string;
  errors: UploadErrorItem[];
}

const MAX_SINGLE_IMAGE_BYTES = 10 * 1024 * 1024; // 10MB
const MAX_BATCH_FILES = 20;
const ALLOWED_MIME_TYPES = new Set(["image/png", "image/jpeg", "image/webp", "image/jpg"]);

export function MediaLibraryPage() {
  const { locale, t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const uploadInputRef = useRef<HTMLInputElement>(null);
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [includeArchived, setIncludeArchived] = useState(false);
  const [sourceType, setSourceType] = useState<MediaLibrarySourceType | "">("");
  const [folderId, setFolderId] = useState<string | null>(null);
  const [tag, setTag] = useState<string | null>(null);
  const [view, setView] = useState<LibraryView>("grid");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [lastSelectedAssetId, setLastSelectedAssetId] = useState<string | null>(null);
  const [dialog, setDialog] = useState<LibraryDialog>(null);
  const [previewAsset, setPreviewAsset] = useState<MediaLibraryAsset | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [uploadProgress, setUploadProgress] = useState<UploadProgressState | null>(null);
  const [uploadSummaryErrors, setUploadSummaryErrors] = useState<UploadErrorItem[] | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(searchInput.trim()), 220);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  useEffect(() => {
    setSelectedIds(new Set());
    setLastSelectedAssetId(null);
  }, [folderId, includeArchived, search, sourceType, tag]);

  const bootstrapQuery = useQuery({
    queryKey: ["media-library-bootstrap"],
    queryFn: api.getMediaLibraryBootstrap,
  });
  const assetsQuery = useInfiniteQuery({
    queryKey: ["media-library-assets", search, includeArchived, sourceType, folderId, tag],
    queryFn: ({ pageParam }) => api.listMediaLibraryAssets({
      cursor: pageParam,
      includeArchived,
      sourceType: sourceType || null,
      folderId,
      tag,
      q: search,
      limit: 48,
    }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
  });
  const assets = useMemo(
    () => assetsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [assetsQuery.data],
  );
  const selectedAssets = useMemo(
    () => assets.filter((asset) => selectedIds.has(asset.id)),
    [assets, selectedIds],
  );
  const bootstrap = bootstrapQuery.data;
  const agentPageContext = useMemo(() => ({
    route: `${location.pathname}${location.search}`,
    page_type: "media_library",
    product_id: null,
    workflow_id: null,
    selected_asset_ids: [...selectedIds].sort().slice(0, 100),
    visible_asset_ids: assets.map((asset) => asset.id).slice(0, 100),
    filters: {
      ...(search ? { search } : {}),
      ...(sourceType ? { source_type: sourceType } : {}),
      ...(folderId ? { folder_id: folderId } : {}),
      ...(tag ? { tag } : {}),
      ...(includeArchived ? { include_archived: "true" } : {}),
    },
    workflow_revision: null,
    library_revision: null,
    captured_at: new Date().toISOString(),
  }), [assets, folderId, includeArchived, location.pathname, location.search, search, selectedIds, sourceType, tag]);
  useRegisterAgentPageContext(agentPageContext);

  const invalidate = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["media-library-bootstrap"] }),
      queryClient.invalidateQueries({ queryKey: ["media-library-assets"] }),
      queryClient.invalidateQueries({ queryKey: ["workflow-media-library"] }),
    ]);
  };

  const handleUploadFiles = async (rawFiles: File[]) => {
    if (!rawFiles.length) return;

    if (rawFiles.length > MAX_BATCH_FILES) {
      setUploadSummaryErrors([
        {
          filename: t("mediaLibrary.upload"),
          reason: t("mediaLibrary.uploadBatchExceeded", { max: MAX_BATCH_FILES }),
        },
      ]);
      return;
    }

    const validFiles: File[] = [];
    const initialErrors: UploadErrorItem[] = [];

    for (const file of rawFiles) {
      const isImage = file.type.startsWith("image/") || /\.(png|jpe?g|webp)$/i.test(file.name);
      if (!isImage || (file.type && !ALLOWED_MIME_TYPES.has(file.type.toLowerCase()))) {
        initialErrors.push({
          filename: file.name,
          reason: t("mediaLibrary.uploadHint"),
        });
        continue;
      }
      if (file.size > MAX_SINGLE_IMAGE_BYTES) {
        initialErrors.push({
          filename: file.name,
          reason: t("mediaLibrary.uploadHint"),
        });
        continue;
      }
      if (file.size === 0) {
        initialErrors.push({
          filename: file.name,
          reason: "文件内容不能为空",
        });
        continue;
      }
      validFiles.push(file);
    }

    if (validFiles.length === 0) {
      if (initialErrors.length > 0) {
        setUploadSummaryErrors(initialErrors);
      }
      return;
    }

    setUploadProgress({
      total: rawFiles.length,
      completed: initialErrors.length,
      success: 0,
      failed: initialErrors.length,
      errors: [...initialErrors],
    });

    const CONCURRENCY = 3;
    let successCount = 0;
    let failedCount = initialErrors.length;
    const errors: UploadErrorItem[] = [...initialErrors];
    let cursor = 0;

    const worker = async () => {
      while (cursor < validFiles.length) {
        const index = cursor++;
        const file = validFiles[index];
        setUploadProgress((prev) => (prev ? { ...prev, currentFileName: file.name } : null));
        try {
          await api.uploadMediaLibraryAsset(file, folderId);
          successCount++;
        } catch (err: unknown) {
          failedCount++;
          const message =
            err instanceof ApiError ? err.detail : err instanceof Error ? err.message : "上传失败";
          errors.push({ filename: file.name, reason: message });
        }
        setUploadProgress((prev) =>
          prev
            ? {
              ...prev,
              completed: successCount + failedCount,
              success: successCount,
              failed: failedCount,
              errors,
            }
            : null,
        );
      }
    };

    const pool = Array.from({ length: Math.min(CONCURRENCY, validFiles.length) }, () => worker());
    await Promise.all(pool);

    await invalidate();
    setUploadProgress(null);

    if (errors.length > 0) {
      setUploadSummaryErrors(errors);
    }
  };

  const operationMutation = useMutation({
    mutationFn: async (operation: {
      kind: "move" | "tags" | "archive" | "restore" | "batch-archive" | "batch-restore";
      folderId?: string | null;
      tagNames?: string[];
      asset?: MediaLibraryAsset;
      assets?: MediaLibraryAsset[];
    }) => {
      if (operation.kind === "move") {
        return api.moveMediaLibraryAssets({
          assetIds: selectedAssets.map((asset) => asset.id),
          folderId: operation.folderId ?? null,
          expectedRevisions: Object.fromEntries(selectedAssets.map((asset) => [asset.id, asset.revision])),
        });
      }
      if (operation.kind === "tags") {
        return api.setMediaLibraryAssetTags({
          assetIds: selectedAssets.map((asset) => asset.id),
          tagNames: operation.tagNames ?? [],
          expectedRevisions: Object.fromEntries(selectedAssets.map((asset) => [asset.id, asset.revision])),
        });
      }
      if (operation.kind === "batch-archive") {
        const targetAssets = operation.assets ?? selectedAssets;
        return Promise.all(
          targetAssets.map((asset) => api.archiveMediaLibraryAsset(asset.id, asset.revision)),
        );
      }
      if (operation.kind === "batch-restore") {
        const targetAssets = operation.assets ?? selectedAssets;
        return Promise.all(
          targetAssets.map((asset) => api.restoreMediaLibraryAsset(asset.id, asset.revision)),
        );
      }
      const asset = operation.asset;
      if (!asset) throw new Error(t("mediaLibrary.operationFailed"));
      return operation.kind === "archive"
        ? api.archiveMediaLibraryAsset(asset.id, asset.revision)
        : api.restoreMediaLibraryAsset(asset.id, asset.revision);
    },
    onSuccess: async () => {
      setSelectedIds(new Set());
      setLastSelectedAssetId(null);
      setDialog(null);
      await invalidate();
    },
  });

  const createFolderMutation = useMutation({
    mutationFn: (name: string) => api.createMediaLibraryFolder(name),
    onSuccess: async (folder) => {
      setDialog(null);
      setFolderId(folder.id);
      await invalidate();
    },
  });

  const renameFolderMutation = useMutation({
    mutationFn: ({ folderId, expectedName, name }: { folderId: string; expectedName: string; name: string }) =>
      api.renameMediaLibraryFolder(folderId, expectedName, name),
    onSuccess: async () => {
      setDialog(null);
      await invalidate();
    },
  });

  const deleteFolderMutation = useMutation({
    mutationFn: (targetFolderId: string) => api.deleteMediaLibraryFolder(targetFolderId),
    onSuccess: async (_, deletedFolderId) => {
      setDialog(null);
      if (folderId === deletedFolderId) setFolderId(null);
      await invalidate();
    },
  });

  const createTagMutation = useMutation({
    mutationFn: (name: string) => api.createMediaLibraryTag(name),
    onSuccess: async (createdTag) => {
      setDialog(null);
      setTag(createdTag.name);
      await invalidate();
    },
  });

  const renameTagMutation = useMutation({
    mutationFn: ({ tagId, expectedName, name }: { tagId: string; expectedName: string; name: string }) =>
      api.renameMediaLibraryTag(tagId, expectedName, name),
    onSuccess: async ({ name }, { expectedName }) => {
      setDialog(null);
      if (tag === expectedName) setTag(name);
      await invalidate();
    },
  });

  const deleteTagMutation = useMutation({
    mutationFn: (tagId: string) => api.deleteMediaLibraryTag(tagId),
    onSuccess: async () => {
      setDialog(null);
      setTag(null);
      await invalidate();
    },
  });

  const operationError = [
    assetsQuery.error,
    operationMutation.error,
    createFolderMutation.error,
    renameFolderMutation.error,
    deleteFolderMutation.error,
    createTagMutation.error,
    renameTagMutation.error,
    deleteTagMutation.error,
  ].find((error): error is Error => error instanceof Error) ?? null;

  const busy =
    Boolean(uploadProgress) ||
    operationMutation.isPending ||
    createFolderMutation.isPending ||
    renameFolderMutation.isPending ||
    deleteFolderMutation.isPending ||
    createTagMutation.isPending ||
    renameTagMutation.isPending ||
    deleteTagMutation.isPending;

  const toggleSelected = (assetId: string, event?: React.MouseEvent) => {
    if (event?.shiftKey && lastSelectedAssetId && lastSelectedAssetId !== assetId) {
      const lastIndex = assets.findIndex((a) => a.id === lastSelectedAssetId);
      const currentIndex = assets.findIndex((a) => a.id === assetId);
      if (lastIndex !== -1 && currentIndex !== -1) {
        const start = Math.min(lastIndex, currentIndex);
        const end = Math.max(lastIndex, currentIndex);
        const rangeIds = assets.slice(start, end + 1).map((a) => a.id);
        setSelectedIds((current) => {
          const next = new Set(current);
          rangeIds.forEach((id) => next.add(id));
          return next;
        });
        setLastSelectedAssetId(assetId);
        return;
      }
    }

    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(assetId)) next.delete(assetId);
      else next.add(assetId);
      return next;
    });
    setLastSelectedAssetId(assetId);
  };

  const isAllLoadedSelected = assets.length > 0 && assets.every((asset) => selectedIds.has(asset.id));
  const toggleSelectAll = () => {
    if (isAllLoadedSelected) {
      setSelectedIds(new Set());
      setLastSelectedAssetId(null);
    } else {
      setSelectedIds(new Set(assets.map((asset) => asset.id)));
      setLastSelectedAssetId(null);
    }
  };

  const clearFilters = () => {
    setSearchInput("");
    setSearch("");
    setIncludeArchived(false);
    setSourceType("");
    setFolderId(null);
    setTag(null);
  };

  const hasActiveSelected = selectedAssets.some((a) => !a.is_archived);
  const hasArchivedSelected = selectedAssets.some((a) => a.is_archived);

  // 连续大图预览控制
  const previewIndex = previewAsset ? assets.findIndex((a) => a.id === previewAsset.id) : -1;
  const hasPrev = previewIndex > 0;
  const hasNext = previewIndex >= 0 && previewIndex < assets.length - 1;
  const onPrev = () => {
    if (hasPrev) setPreviewAsset(assets[previewIndex - 1]);
  };
  const onNext = () => {
    if (hasNext) setPreviewAsset(assets[previewIndex + 1]);
  };
  const counterText = previewIndex >= 0 ? `${previewIndex + 1} / ${assets.length}` : undefined;

  const handleFileInputChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []);
    if (files.length) {
      void handleUploadFiles(files);
    }
    event.target.value = "";
  };

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!isDragging) setIsDragging(true);
  };

  const handleDragLeave = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.currentTarget === e.target) setIsDragging(false);
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(false);
    const files = Array.from(e.dataTransfer.files);
    if (files.length > 0) {
      void handleUploadFiles(files);
    }
  };

  return (
    <div
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      className="relative min-h-screen bg-slate-50 text-slate-950 dark:bg-[#060a12] dark:text-slate-100"
    >
      <input
        ref={uploadInputRef}
        type="file"
        multiple
        accept="image/png,image/jpeg,image/webp"
        onChange={handleFileInputChange}
        className="hidden"
      />

      {/* 上传进度悬浮指示器 */}
      {uploadProgress ? (
        <div className="fixed bottom-6 right-6 z-[160] flex w-80 flex-col gap-2 rounded-xl border border-slate-200 bg-white/95 p-3.5 shadow-2xl backdrop-blur-md dark:border-slate-700 dark:bg-slate-900/95">
          <div className="flex items-center justify-between text-xs font-semibold text-slate-800 dark:text-slate-100">
            <span className="flex items-center gap-1.5">
              <Loader2 size={13} className="animate-spin text-indigo-600 dark:text-violet-400" />
              {t("mediaLibrary.uploadProgress", {
                current: uploadProgress.completed,
                total: uploadProgress.total,
              })}
            </span>
            <span className="text-[11px] tabular-nums text-slate-400">
              {Math.round((uploadProgress.completed / uploadProgress.total) * 100)}%
            </span>
          </div>
          {uploadProgress.currentFileName ? (
            <div className="truncate text-[11px] text-slate-400">
              {uploadProgress.currentFileName}
            </div>
          ) : null}
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
            <div
              className="h-full bg-indigo-600 transition-all duration-200 dark:bg-violet-500"
              style={{ width: `${(uploadProgress.completed / uploadProgress.total) * 100}%` }}
            />
          </div>
        </div>
      ) : null}

      {/* 上传结果/错误汇总弹窗 */}
      <Dialog open={Boolean(uploadSummaryErrors)} onOpenChange={(open) => !open && setUploadSummaryErrors(null)}>
        {uploadSummaryErrors ? (
          <DialogContent
            title={t("mediaLibrary.upload")}
            closeLabel={t("common.cancel")}
            onClose={() => setUploadSummaryErrors(null)}
            footer={
              <Button type="button" onClick={() => setUploadSummaryErrors(null)}>
                {t("workbench.dialog.confirm")}
              </Button>
            }
          >
            <div className="max-h-60 space-y-2 overflow-y-auto pr-1">
              {uploadSummaryErrors.map((item, idx) => (
                <div
                  key={idx}
                  className="rounded-control border border-state-error/30 bg-state-error/10 p-2.5 text-xs"
                >
                  <div className="truncate font-semibold text-state-error">{item.filename}</div>
                  <div className="mt-0.5 text-state-error">{item.reason}</div>
                </div>
              ))}
            </div>
          </DialogContent>
        ) : null}
      </Dialog>

      {isDragging ? (
        <div className="pointer-events-none fixed inset-0 z-[150] flex flex-col items-center justify-center bg-indigo-950/60 p-6 backdrop-blur-md dark:bg-violet-950/60">
          <div className="flex flex-col items-center rounded-2xl border-2 border-dashed border-white/60 bg-slate-900/60 px-8 py-10 text-center text-white shadow-2xl">
            <Upload size={44} className="animate-bounce text-indigo-300 dark:text-violet-300" />
            <h2 className="mt-4 text-lg font-bold">{t("mediaLibrary.dropToUpload")}</h2>
            <p className="mt-1 text-xs text-white/70">{t("mediaLibrary.uploadHint")}</p>
          </div>
        </div>
      ) : null}

      <TopNav breadcrumbs={t("mediaLibrary.title")} onHome={() => navigate("/home")} />
      <main className="mx-auto flex min-h-[calc(100svh-4.5rem)] max-w-[1600px] flex-col px-3 py-3 sm:px-5 lg:px-7 lg:py-5">
        <header className="flex flex-wrap items-start gap-4 border-b border-slate-200 pb-4 dark:border-slate-800">
          <div className="flex min-w-0 flex-1 items-start gap-3">
            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-sm dark:bg-violet-500">
              <Images size={19} />
            </span>
            <div className="min-w-0">
              <h1 className="truncate text-xl font-bold tracking-tight text-slate-950 dark:text-white sm:text-2xl">
                {t("mediaLibrary.title")}
              </h1>
              <p className="mt-1 max-w-2xl text-sm text-slate-500 dark:text-slate-400">{t("mediaLibrary.subtitle")}</p>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2 text-xs font-semibold text-slate-500 dark:text-slate-400">
            <Metric label={t("mediaLibrary.activeCount")} value={bootstrap?.active_count ?? 0} />
            <Metric label={t("mediaLibrary.archivedCount")} value={bootstrap?.archived_count ?? 0} muted />
            <button
              type="button"
              onClick={() => uploadInputRef.current?.click()}
              disabled={Boolean(uploadProgress)}
              title={t("mediaLibrary.uploadHint")}
              className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-indigo-600 px-3 text-xs font-semibold text-white shadow-sm hover:bg-indigo-500 disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400"
            >
              {uploadProgress ? <Loader2 size={14} className="animate-spin" /> : <Upload size={14} />}
              {uploadProgress ? t("mediaLibrary.uploading") : t("mediaLibrary.upload")}
            </button>
          </div>
        </header>

        <div className="mt-4 flex min-h-0 flex-1 flex-col gap-4 lg:grid lg:grid-cols-[230px_minmax(0,1fr)]">
          <aside className="min-w-0 rounded-xl border border-slate-200 bg-white p-3 shadow-sm dark:border-slate-800 dark:bg-[#0d131e]">
            <div className="flex items-center justify-between px-2 pb-2">
              <h2 className="text-xs font-bold uppercase tracking-[0.14em] text-slate-500 dark:text-slate-400">{t("mediaLibrary.scope")}</h2>
              {(search || sourceType || folderId || tag || includeArchived) ? (
                <button type="button" onClick={clearFilters} className="text-[11px] font-semibold text-indigo-600 hover:text-indigo-800 dark:text-violet-300">
                  {t("mediaLibrary.clearFilters")}
                </button>
              ) : null}
            </div>
            <FilterButton active={!folderId && !tag && !sourceType && !includeArchived} label={t("mediaLibrary.allAssets")} count={bootstrap?.active_count ?? 0} onClick={clearFilters} />
            <FilterButton active={Boolean(!folderId && !tag && !sourceType && includeArchived)} label={t("mediaLibrary.archived")} count={bootstrap?.archived_count ?? 0} onClick={() => { clearFilters(); setIncludeArchived(true); }} />

            <div className="mt-4 border-t border-slate-100 pt-3 dark:border-slate-800">
              <div className="flex items-center justify-between px-2 pb-1.5">
                <div className="text-[11px] font-semibold text-slate-400">{t("mediaLibrary.folders")}</div>
                <button type="button" onClick={() => setDialog({ kind: "create-folder" })} className="inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-indigo-700 dark:hover:bg-slate-800 dark:hover:text-violet-200" aria-label={t("mediaLibrary.createFolder")} title={t("mediaLibrary.createFolder")}>
                  <FolderPlus size={14} />
                </button>
              </div>
              {bootstrap?.folders.map((folder) => (
                <SidebarItem
                  key={folder.id}
                  active={folderId === folder.id}
                  label={folder.name}
                  count={folder.count}
                  onClick={() => { setFolderId(folder.id); setTag(null); }}
                  onRename={() => setDialog({ kind: "rename-folder", folder })}
                  onDelete={() => setDialog({ kind: "delete-folder", folder })}
                />
              ))}
              {bootstrap?.folders.length === 0 ? <div className="px-2 py-2 text-[11px] text-slate-400">{t("mediaLibrary.noFolders")}</div> : null}
            </div>

            <div className="mt-4 border-t border-slate-100 pt-3 dark:border-slate-800">
              <div className="flex items-center justify-between px-2 pb-1.5">
                <div className="text-[11px] font-semibold text-slate-400">{t("mediaLibrary.tags")}</div>
                <button type="button" onClick={() => setDialog({ kind: "create-tag" })} className="inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-indigo-700 dark:hover:bg-slate-800 dark:hover:text-violet-200" aria-label={t("mediaLibrary.createTag")} title={t("mediaLibrary.createTag")}>
                  <Plus size={14} />
                </button>
              </div>
              {bootstrap?.tags.map((item) => (
                <SidebarItem
                  key={item.id}
                  active={tag === item.name}
                  label={item.name}
                  count={item.count}
                  onClick={() => { setTag(item.name); setFolderId(null); }}
                  onRename={() => setDialog({ kind: "rename-tag", tag: item })}
                  onDelete={() => setDialog({ kind: "delete-tag", tag: item })}
                />
              ))}
              {bootstrap?.tags.length === 0 ? <div className="px-2 py-2 text-[11px] text-slate-400">{t("mediaLibrary.noTags")}</div> : null}
            </div>
          </aside>

          <section className="min-w-0">
            <div className="flex flex-wrap items-center gap-2 rounded-xl border border-slate-200 bg-white p-2 shadow-sm dark:border-slate-800 dark:bg-[#0d131e]">
              <label className="relative min-w-[190px] flex-1">
                <span className="sr-only">{t("mediaLibrary.search")}</span>
                <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
                <input value={searchInput} onChange={(event) => setSearchInput(event.target.value)} type="search" placeholder={t("mediaLibrary.search")} className="h-9 w-full rounded-lg border border-slate-200 bg-slate-50 pl-9 pr-3 text-sm outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 dark:border-slate-700 dark:bg-slate-950/50 dark:text-white dark:focus:border-violet-400 dark:focus:ring-violet-500/15" />
              </label>
              <select value={sourceType} onChange={(event) => setSourceType(event.target.value as MediaLibrarySourceType | "")} className="h-9 max-w-[170px] rounded-lg border border-slate-200 bg-white px-2.5 text-xs font-medium text-slate-700 outline-none focus:border-indigo-400 dark:border-slate-700 dark:bg-slate-950/60 dark:text-slate-200 dark:focus:border-violet-400">
                <option value="">{t("mediaLibrary.allSources")}</option>
                <option value="direct_upload">{t("mediaLibrary.source.upload")}</option>
                <option value="product_asset">{t("mediaLibrary.source.product")}</option>
                <option value="image_session_generated">{t("mediaLibrary.source.session")}</option>
              </select>
              <label className="inline-flex h-9 items-center gap-2 rounded-lg border border-slate-200 px-2.5 text-xs font-medium text-slate-600 dark:border-slate-700 dark:text-slate-300">
                <input type="checkbox" checked={includeArchived} onChange={(event) => setIncludeArchived(event.target.checked)} className="h-3.5 w-3.5 accent-indigo-600" />
                {t("mediaLibrary.showArchived")}
              </label>
              <div className="flex h-9 overflow-hidden rounded-lg border border-slate-200 dark:border-slate-700">
                <ViewButton active={view === "grid"} label={t("detail.library.gridView")} onClick={() => setView("grid")}><Grid2X2 size={15} /></ViewButton>
                <ViewButton active={view === "list"} label={t("detail.library.listView")} onClick={() => setView("list")}><List size={15} /></ViewButton>
              </div>
            </div>

            {selectedAssets.length ? (
              <div className="mt-3 flex flex-wrap items-center gap-2 rounded-lg border border-indigo-200 bg-indigo-50 px-3 py-2 text-xs text-indigo-800 shadow-sm dark:border-violet-400/30 dark:bg-violet-500/10 dark:text-violet-100">
                <span className="mr-auto font-semibold">{t("mediaLibrary.selected", { count: selectedAssets.length })}</span>
                <button type="button" onClick={() => setDialog({ kind: "move" })} className="inline-flex h-8 items-center gap-1.5 rounded-md bg-white px-2.5 font-semibold text-slate-700 shadow-sm hover:bg-slate-50 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800">
                  <FolderInput size={13} />{t("detail.library.move")}
                </button>
                <button type="button" onClick={() => setDialog({ kind: "tags" })} className="inline-flex h-8 items-center gap-1.5 rounded-md bg-white px-2.5 font-semibold text-slate-700 shadow-sm hover:bg-slate-50 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800">
                  <Tag size={13} />{t("mediaLibrary.editTags")}
                </button>
                {hasActiveSelected ? (
                  <button type="button" onClick={() => setDialog({ kind: "batch-archive", assets: selectedAssets.filter((a) => !a.is_archived) })} className="inline-flex h-8 items-center gap-1.5 rounded-md bg-white px-2.5 font-semibold text-slate-700 shadow-sm hover:bg-red-50 hover:text-red-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-red-900/30 dark:hover:text-red-300">
                    <Archive size={13} />{t("mediaLibrary.batchArchive")}
                  </button>
                ) : null}
                {hasArchivedSelected ? (
                  <button type="button" onClick={() => setDialog({ kind: "batch-restore", assets: selectedAssets.filter((a) => a.is_archived) })} className="inline-flex h-8 items-center gap-1.5 rounded-md bg-white px-2.5 font-semibold text-slate-700 shadow-sm hover:bg-indigo-50 hover:text-indigo-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-violet-900/30 dark:hover:text-violet-300">
                    <ArchiveRestore size={13} />{t("mediaLibrary.batchRestore")}
                  </button>
                ) : null}
                <button type="button" onClick={() => { setSelectedIds(new Set()); setLastSelectedAssetId(null); }} className="inline-flex h-8 w-8 items-center justify-center rounded-md hover:bg-white dark:hover:bg-slate-900" aria-label={t("mediaLibrary.clearSelection")} title={t("mediaLibrary.clearSelection")}>
                  <X size={14} />
                </button>
              </div>
            ) : null}

            {operationError ? <div role="alert" className="mt-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">{operationError instanceof ApiError ? operationError.detail : operationError.message}</div> : null}

            <div className="mt-3 flex items-center justify-between px-1 text-xs text-slate-500 dark:text-slate-400">
              <span>{t("mediaLibrary.resultCount", { count: assets.length })}</span>
              {assets.length ? (
                <button type="button" onClick={toggleSelectAll} className="font-semibold text-indigo-600 hover:text-indigo-800 dark:text-violet-300 dark:hover:text-violet-200">
                  {isAllLoadedSelected ? t("mediaLibrary.deselectLoaded") : t("mediaLibrary.selectLoaded")}
                </button>
              ) : null}
            </div>

            {bootstrapQuery.isLoading || assetsQuery.isLoading ? (
              <LibraryState icon={<Loader2 size={18} className="animate-spin" />} text={t("mediaLibrary.loading")} />
            ) : bootstrapQuery.isError || assetsQuery.isError ? (
              <LibraryState text={t("mediaLibrary.loadFailed")} action={t("detail.library.retry")} onAction={() => { void bootstrapQuery.refetch(); void assetsQuery.refetch(); }} />
            ) : assets.length === 0 ? (
              <LibraryState
                icon={<Images size={22} />}
                text={t("mediaLibrary.empty")}
                action={search || folderId || tag || sourceType ? t("mediaLibrary.clearFilters") : t("mediaLibrary.upload")}
                onAction={search || folderId || tag || sourceType ? clearFilters : () => uploadInputRef.current?.click()}
              />
            ) : view === "grid" ? (
              <div className="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
                {assets.map((asset) => (
                  <MediaLibraryCard
                    key={asset.id}
                    asset={asset}
                    selected={selectedIds.has(asset.id)}
                    busy={busy}
                    onToggle={(event) => toggleSelected(asset.id, event)}
                    onPreview={() => setPreviewAsset(asset)}
                    onArchive={() => setDialog({ kind: asset.is_archived ? "restore" : "archive", asset })}
                  />
                ))}
              </div>
            ) : (
              <div className="mt-2 divide-y divide-slate-100 overflow-hidden rounded-xl border border-slate-200 bg-white dark:divide-slate-800 dark:border-slate-800 dark:bg-[#0d131e]">
                {assets.map((asset) => (
                  <MediaLibraryRow
                    key={asset.id}
                    asset={asset}
                    selected={selectedIds.has(asset.id)}
                    busy={busy}
                    locale={locale}
                    onToggle={(event) => toggleSelected(asset.id, event)}
                    onPreview={() => setPreviewAsset(asset)}
                    onArchive={() => setDialog({ kind: asset.is_archived ? "restore" : "archive", asset })}
                  />
                ))}
              </div>
            )}
            {assetsQuery.hasNextPage ? <button type="button" onClick={() => assetsQuery.fetchNextPage()} disabled={assetsQuery.isFetchingNextPage} className="mt-3 inline-flex h-9 w-full items-center justify-center rounded-lg border border-slate-200 bg-white text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-950/50 dark:text-slate-200">{assetsQuery.isFetchingNextPage ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : null}{t("detail.library.loadMore")}</button> : null}
          </section>
        </div>
      </main>

      <NameDialog
        open={dialog?.kind === "create-folder" || dialog?.kind === "create-tag"}
        title={dialog?.kind === "create-tag" ? t("mediaLibrary.createTag") : t("mediaLibrary.createFolder")}
        label={dialog?.kind === "create-tag" ? t("mediaLibrary.tagName") : t("mediaLibrary.folderName")}
        busy={createFolderMutation.isPending || createTagMutation.isPending}
        onClose={() => setDialog(null)}
        onSubmit={(name) => dialog?.kind === "create-tag" ? createTagMutation.mutate(name) : createFolderMutation.mutate(name)}
      />

      <NameDialog
        open={dialog?.kind === "rename-folder" || dialog?.kind === "rename-tag"}
        title={dialog?.kind === "rename-folder" ? t("mediaLibrary.renameFolder") : t("mediaLibrary.renameTag")}
        label={dialog?.kind === "rename-folder" ? t("mediaLibrary.folderName") : t("mediaLibrary.tagName")}
        initialValue={dialog?.kind === "rename-folder" ? dialog.folder.name : dialog?.kind === "rename-tag" ? dialog.tag.name : ""}
        busy={renameFolderMutation.isPending || renameTagMutation.isPending}
        onClose={() => setDialog(null)}
        onSubmit={(name) => {
          if (dialog?.kind === "rename-folder") {
            renameFolderMutation.mutate({ folderId: dialog.folder.id, expectedName: dialog.folder.name, name });
          } else if (dialog?.kind === "rename-tag") {
            renameTagMutation.mutate({ tagId: dialog.tag.id, expectedName: dialog.tag.name, name });
          }
        }}
      />

      <ConfirmDialog
        open={dialog?.kind === "delete-folder" || dialog?.kind === "delete-tag"}
        title={dialog?.kind === "delete-folder" ? t("mediaLibrary.deleteFolder") : t("mediaLibrary.deleteTag")}
        description={
          dialog?.kind === "delete-folder"
            ? t("mediaLibrary.deleteFolderConfirm", { name: dialog.folder.name })
            : dialog?.kind === "delete-tag"
              ? t("mediaLibrary.deleteTagConfirm", { name: dialog.tag.name })
              : ""
        }
        confirmLabel={dialog?.kind === "delete-folder" ? t("mediaLibrary.deleteFolder") : t("mediaLibrary.deleteTag")}
        cancelLabel={t("common.cancel")}
        busy={deleteFolderMutation.isPending || deleteTagMutation.isPending}
        destructive
        onClose={() => setDialog(null)}
        onConfirm={() => {
          if (dialog?.kind === "delete-folder") {
            deleteFolderMutation.mutate(dialog.folder.id);
          } else if (dialog?.kind === "delete-tag") {
            deleteTagMutation.mutate(dialog.tag.id);
          }
        }}
      />

      <MoveDialog open={dialog?.kind === "move"} folders={bootstrap?.folders ?? []} busy={operationMutation.isPending} onClose={() => setDialog(null)} onMove={(nextFolderId) => operationMutation.mutate({ kind: "move", folderId: nextFolderId })} />
      <NameDialog open={dialog?.kind === "tags"} title={t("mediaLibrary.editTags")} label={t("mediaLibrary.tagsInput")} placeholder={t("mediaLibrary.tagsPlaceholder")} busy={operationMutation.isPending} onClose={() => setDialog(null)} onSubmit={(value) => operationMutation.mutate({ kind: "tags", tagNames: value.split(",").map((item) => item.trim()).filter(Boolean) })} />
      <ConfirmDialog
        open={dialog?.kind === "archive" || dialog?.kind === "restore"}
        title={dialog?.kind === "restore" ? t("mediaLibrary.restoreTitle") : t("mediaLibrary.archiveTitle")}
        description={dialog?.kind === "archive" || dialog?.kind === "restore" ? t(dialog.kind === "restore" ? "mediaLibrary.restoreDescription" : "mediaLibrary.archiveDescription", { name: dialog.asset.display_name }) : ""}
        confirmLabel={dialog?.kind === "restore" ? t("mediaLibrary.restore") : t("mediaLibrary.archive")}
        cancelLabel={t("common.cancel")}
        busy={operationMutation.isPending}
        destructive={dialog?.kind !== "restore"}
        onClose={() => setDialog(null)}
        onConfirm={() => {
          if (dialog?.kind === "archive" || dialog?.kind === "restore") {
            operationMutation.mutate({ kind: dialog.kind, asset: dialog.asset });
          }
        }}
      />
      <ConfirmDialog
        open={dialog?.kind === "batch-archive" || dialog?.kind === "batch-restore"}
        title={dialog?.kind === "batch-restore" ? t("mediaLibrary.batchRestoreTitle") : t("mediaLibrary.batchArchiveTitle")}
        description={dialog?.kind === "batch-archive" || dialog?.kind === "batch-restore" ? t(dialog.kind === "batch-restore" ? "mediaLibrary.batchRestoreDescription" : "mediaLibrary.batchArchiveDescription", { count: dialog.assets.length }) : ""}
        confirmLabel={dialog?.kind === "batch-restore" ? t("mediaLibrary.batchRestore") : t("mediaLibrary.batchArchive")}
        cancelLabel={t("common.cancel")}
        busy={operationMutation.isPending}
        destructive={dialog?.kind !== "batch-restore"}
        onClose={() => setDialog(null)}
        onConfirm={() => {
          if (dialog?.kind === "batch-archive" || dialog?.kind === "batch-restore") {
            operationMutation.mutate({ kind: dialog.kind, assets: dialog.assets });
          }
        }}
      />
      {previewAsset ? (
        <GalleryImagePreviewDialog
          ariaLabel={t("mediaLibrary.previewLabel")}
          imageUrl={api.toApiUrl(previewAsset.preview_url)}
          imageAlt={previewAsset.display_name}
          title={previewAsset.display_name}
          subtitle={previewAsset.original_filename}
          body={previewAsset.folder_name ?? t("mediaLibrary.unorganized")}
          metadataRows={[
            { label: t("mediaLibrary.source"), value: sourceLabel(previewAsset.source_type, t) },
            { label: t("mediaLibrary.dimensions"), value: previewAsset.width && previewAsset.height ? `${previewAsset.width} x ${previewAsset.height}` : t("common.unknown") },
            { label: t("mediaLibrary.createdAt"), value: formatDateTime(previewAsset.created_at, locale) },
            { label: t("mediaLibrary.status"), value: previewAsset.is_archived ? t("mediaLibrary.archived") : t("mediaLibrary.active") },
          ]}
          providerNotes={previewAsset.tags.map((item) => item.name)}
          providerNotesTitle={t("mediaLibrary.tags")}
          downloadUrl={previewAsset.download_url}
          downloadLabel={t("detail.library.download")}
          closeLabel={t("detail.library.close")}
          hasPrev={hasPrev}
          hasNext={hasNext}
          onPrev={onPrev}
          onNext={onNext}
          prevLabel={t("mediaLibrary.previewPrev")}
          nextLabel={t("mediaLibrary.previewNext")}
          counterText={counterText}
          onClose={() => setPreviewAsset(null)}
        />
      ) : null}
    </div>
  );
}

function Metric({ label, value, muted = false }: { label: string; value: number; muted?: boolean }) {
  return (
    <div className={`rounded-lg border px-2.5 py-1.5 ${muted ? "border-slate-200/70 dark:border-slate-700/70" : "border-indigo-100 bg-indigo-50 dark:border-violet-400/20 dark:bg-violet-500/10"}`}>
      <div className="text-[10px] font-medium">{label}</div>
      <div className="mt-0.5 text-sm font-bold tabular-nums text-slate-800 dark:text-slate-200">{value}</div>
    </div>
  );
}

function FilterButton({ active, label, count, onClick }: { active: boolean; label: string; count: number; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-xs font-medium transition-colors ${active
          ? "bg-indigo-50 text-indigo-700 dark:bg-violet-500/15 dark:text-violet-200"
          : "text-slate-600 hover:bg-slate-50 dark:text-slate-300 dark:hover:bg-slate-800/70"
        }`}
    >
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="shrink-0 text-[10px] tabular-nums text-slate-400">{count}</span>
    </button>
  );
}

function SidebarItem({
  active,
  label,
  count,
  onClick,
  onRename,
  onDelete,
}: {
  active: boolean;
  label: string;
  count: number;
  onClick: () => void;
  onRename: () => void;
  onDelete: () => void;
}) {
  return (
    <div
      onClick={onClick}
      className={`group flex h-8 w-full cursor-pointer items-center gap-1.5 rounded-md px-2 text-xs font-medium transition-colors select-none ${active
          ? "bg-indigo-50 text-indigo-700 dark:bg-violet-500/15 dark:text-violet-200"
          : "text-slate-600 hover:bg-slate-50 dark:text-slate-300 dark:hover:bg-slate-800/70"
        }`}
    >
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <div className="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onRename();
          }}
          className="inline-flex h-5 w-5 items-center justify-center rounded text-slate-400 hover:bg-slate-200 hover:text-slate-700 dark:hover:bg-slate-700 dark:hover:text-slate-200"
          title="Rename"
        >
          <Pencil size={11} />
        </button>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onDelete();
          }}
          className="inline-flex h-5 w-5 items-center justify-center rounded text-slate-400 hover:bg-red-100 hover:text-red-600 dark:hover:bg-red-950 dark:hover:text-red-400"
          title="Delete"
        >
          <Trash2 size={11} />
        </button>
      </div>
      <span className="shrink-0 text-[10px] tabular-nums text-slate-400">{count}</span>
    </div>
  );
}

function ViewButton({ active, label, onClick, children }: { active: boolean; label: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      aria-pressed={active}
      className={`inline-flex h-full w-9 items-center justify-center transition-colors ${active
          ? "bg-indigo-50 text-indigo-700 dark:bg-violet-500/20 dark:text-violet-100"
          : "text-slate-400 hover:text-slate-800 dark:text-slate-500 dark:hover:text-white"
        }`}
    >
      {children}
    </button>
  );
}

function MediaLibraryCard({
  asset,
  selected,
  busy,
  onToggle,
  onPreview,
  onArchive,
}: {
  asset: MediaLibraryAsset;
  selected: boolean;
  busy: boolean;
  onToggle: (event: React.MouseEvent) => void;
  onPreview: () => void;
  onArchive: () => void;
}) {
  const { t } = useI18n();
  return (
    <article
      onClick={(e) => onToggle(e)}
      className={`group relative flex cursor-pointer flex-col overflow-hidden rounded-xl border bg-white shadow-sm transition-all select-none hover:shadow-md dark:bg-[#0d131e] ${selected
          ? "border-indigo-500 ring-2 ring-indigo-500/20 dark:border-violet-400 dark:ring-violet-500/25"
          : "border-slate-200 hover:border-slate-300 dark:border-slate-800 dark:hover:border-slate-700"
        }`}
    >
      <div className="relative aspect-square overflow-hidden bg-slate-100 dark:bg-slate-900">
        <img
          src={api.toApiUrl(asset.thumbnail_url)}
          alt={asset.display_name}
          loading="lazy"
          className={`h-full w-full object-cover transition duration-200 group-hover:scale-[1.02] ${asset.is_archived ? "opacity-55 grayscale" : ""
            }`}
        />

        {/* 复选框 - 高对比度毛玻璃，hover 时显现，选中时高亮常驻 */}
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onToggle(e);
          }}
          aria-pressed={selected}
          aria-label={asset.display_name}
          className={`absolute left-2.5 top-2.5 z-10 flex h-6 w-6 items-center justify-center rounded-md border shadow-md backdrop-blur-md transition-all ${selected
              ? "border-indigo-600 bg-indigo-600 text-white opacity-100 dark:border-violet-500 dark:bg-violet-500"
              : "border-white/40 bg-slate-950/40 text-transparent opacity-75 group-hover:opacity-100 hover:border-white hover:bg-slate-950/70 hover:text-white/80 dark:border-white/30 dark:bg-slate-900/60"
            }`}
        >
          <Check size={13} strokeWidth={selected ? 2.5 : 2} />
        </button>

        {/* 悬浮快速预览按钮 */}
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onPreview();
          }}
          className="absolute right-2.5 top-2.5 z-10 flex h-7 w-7 items-center justify-center rounded-md border border-white/30 bg-slate-950/50 text-white opacity-0 shadow-md backdrop-blur-md transition-all group-hover:opacity-100 hover:scale-105 hover:bg-slate-950/80 focus:opacity-100"
          aria-label={t("mediaLibrary.previewLabel")}
          title={t("mediaLibrary.previewLabel")}
        >
          <Maximize2 size={13} />
        </button>

        {asset.is_archived ? (
          <span className="absolute bottom-2 left-2.5 rounded-md bg-slate-950/80 px-2 py-0.5 text-[10px] font-semibold text-white backdrop-blur-sm">
            {t("mediaLibrary.archived")}
          </span>
        ) : null}
      </div>

      <div className="flex min-w-0 items-center gap-2 p-2.5">
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs font-semibold text-slate-800 dark:text-slate-100" title={asset.display_name}>
            {asset.display_name}
          </div>
          <div className="mt-0.5 truncate text-[10px] text-slate-400">
            {asset.folder_name ?? t("mediaLibrary.unorganized")} · {sourceLabel(asset.source_type, t)}
          </div>
        </div>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onArchive();
          }}
          disabled={busy}
          className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-indigo-700 disabled:opacity-40 dark:hover:bg-slate-800 dark:hover:text-violet-200"
          aria-label={asset.is_archived ? t("mediaLibrary.restore") : t("mediaLibrary.archive")}
          title={asset.is_archived ? t("mediaLibrary.restore") : t("mediaLibrary.archive")}
        >
          {asset.is_archived ? <ArchiveRestore size={14} /> : <Archive size={14} />}
        </button>
      </div>
    </article>
  );
}

function MediaLibraryRow({
  asset,
  selected,
  busy,
  locale,
  onToggle,
  onPreview,
  onArchive,
}: {
  asset: MediaLibraryAsset;
  selected: boolean;
  busy: boolean;
  locale: ReturnType<typeof useI18n>["locale"];
  onToggle: (event: React.MouseEvent) => void;
  onPreview: () => void;
  onArchive: () => void;
}) {
  const { t } = useI18n();
  return (
    <div
      onClick={(e) => onToggle(e)}
      className={`group flex min-w-0 cursor-pointer items-center gap-3 px-3 py-2.5 transition-colors select-none ${selected ? "bg-indigo-50/70 dark:bg-violet-500/10" : "hover:bg-slate-50/70 dark:hover:bg-slate-900/50"
        }`}
    >
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onToggle(e);
        }}
        aria-pressed={selected}
        aria-label={asset.display_name}
        className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-md border transition-all ${selected
            ? "border-indigo-600 bg-indigo-600 text-white dark:border-violet-400 dark:bg-violet-500"
            : "border-slate-300 bg-white text-transparent group-hover:border-slate-400 dark:border-slate-600 dark:bg-slate-950"
          }`}
      >
        <Check size={13} strokeWidth={selected ? 2.5 : 2} />
      </button>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onPreview();
        }}
        className="group/img relative h-12 w-12 shrink-0 overflow-hidden rounded-md bg-slate-100 dark:bg-slate-900"
        aria-label={t("mediaLibrary.previewLabel")}
      >
        <img
          src={api.toApiUrl(asset.thumbnail_url)}
          alt=""
          loading="lazy"
          className={`h-full w-full object-cover transition-transform duration-150 group-hover/img:scale-105 ${asset.is_archived ? "opacity-55 grayscale" : ""
            }`}
        />
        <span className="absolute inset-0 flex items-center justify-center bg-slate-950/40 text-white opacity-0 transition-opacity group-hover/img:opacity-100">
          <Maximize2 size={13} />
        </span>
      </button>
      <div className="min-w-0 flex-1">
        <div className="truncate text-xs font-semibold text-slate-800 dark:text-slate-100">{asset.display_name}</div>
        <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[10px] text-slate-400">
          <span>{asset.folder_name ?? t("mediaLibrary.unorganized")}</span>
          <span>{sourceLabel(asset.source_type, t)}</span>
          <span>{formatDateTime(asset.created_at, locale)}</span>
        </div>
      </div>
      <div className="hidden max-w-[220px] flex-wrap justify-end gap-1 sm:flex">
        {asset.tags.slice(0, 3).map((item) => (
          <span key={item.id} className="rounded bg-slate-100 px-1.5 py-1 text-[10px] text-slate-500 dark:bg-slate-800 dark:text-slate-300">
            {item.name}
          </span>
        ))}
      </div>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onArchive();
        }}
        disabled={busy}
        className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-indigo-700 disabled:opacity-40 dark:hover:bg-slate-800 dark:hover:text-violet-200"
        aria-label={asset.is_archived ? t("mediaLibrary.restore") : t("mediaLibrary.archive")}
        title={asset.is_archived ? t("mediaLibrary.restore") : t("mediaLibrary.archive")}
      >
        {asset.is_archived ? <ArchiveRestore size={15} /> : <Archive size={15} />}
      </button>
    </div>
  );
}

function sourceLabel(source: MediaLibrarySourceType, t: ReturnType<typeof useI18n>["t"]): string {
  if (source === "direct_upload") return t("mediaLibrary.source.upload");
  if (source === "product_asset") return t("mediaLibrary.source.product");
  if (source === "image_session_generated") return t("mediaLibrary.source.session");
  return t("mediaLibrary.source.upload");
}

function LibraryState({ icon, text, action, onAction }: { icon?: React.ReactNode; text: string; action?: string; onAction?: () => void }) {
  return (
    <div className="mt-3 flex min-h-[260px] flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-slate-200 bg-white px-5 text-center text-sm text-slate-500 dark:border-slate-800 dark:bg-[#0d131e] dark:text-slate-400">
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

function NameDialog({
  open,
  title,
  label,
  placeholder,
  initialValue = "",
  busy,
  onClose,
  onSubmit,
}: {
  open: boolean;
  title: string;
  label: string;
  placeholder?: string;
  initialValue?: string;
  busy: boolean;
  onClose: () => void;
  onSubmit: (value: string) => void;
}) {
  const { t } = useI18n();
  const [value, setValue] = useState(initialValue);
  useEffect(() => {
    if (open) setValue(initialValue);
  }, [open, initialValue]);
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && !busy && onClose()}>
      <DialogContent
        title={title}
        size="sm"
        hideClose={busy}
        footer={
          <>
            <Button type="button" variant="secondary" onClick={onClose} disabled={busy}>{t("common.cancel")}</Button>
            <Button type="submit" form="media-library-name-form" disabled={busy || !value.trim()} busy={busy}>{t("detail.library.save")}</Button>
          </>
        }
      >
        <form id="media-library-name-form" onSubmit={(event) => { event.preventDefault(); if (value.trim()) onSubmit(value.trim()); }}>
          <Input autoFocus label={label} value={value} onChange={(event) => setValue(event.target.value)} placeholder={placeholder} maxLength={120} />
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MoveDialog({ open, folders, busy, onClose, onMove }: { open: boolean; folders: Array<{ id: string; name: string }>; busy: boolean; onClose: () => void; onMove: (folderId: string | null) => void }) {
  const { t } = useI18n();
  const [folderId, setFolderId] = useState("");
  useEffect(() => { if (open) setFolderId(""); }, [open]);
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && !busy && onClose()}>
      <DialogContent
        title={t("detail.library.moveTitle")}
        size="sm"
        hideClose={busy}
        footer={
          <>
            <Button type="button" variant="secondary" onClick={onClose} disabled={busy}>{t("common.cancel")}</Button>
            <Button type="button" onClick={() => onMove(folderId || null)} disabled={busy} busy={busy}>{t("detail.library.move")}</Button>
          </>
        }
      >
        <Select
          value={folderId}
          onChange={setFolderId}
          ariaLabel={t("detail.library.moveTitle")}
          options={[
            { value: "", label: t("detail.library.moveUnorganized") },
            ...folders.map((folder) => ({ value: folder.id, label: folder.name })),
          ]}
        />
      </DialogContent>
    </Dialog>
  );
}
