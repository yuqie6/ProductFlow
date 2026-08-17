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
  Plus,
  Search,
  Tag,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { GalleryImagePreviewDialog } from "../components/GalleryImagePreviewDialog";
import { TopNav } from "../components/TopNav";
import { useRegisterAgentPageContext } from "../lib/agentPageContext";
import { api, ApiError } from "../lib/api";
import { formatDateTime } from "../lib/format";
import { useI18n } from "../lib/preferences";
import type {
  MediaLibraryAsset,
  MediaLibrarySourceType,
} from "../lib/types";

type LibraryView = "grid" | "list";
type LibraryDialog =
  | { kind: "create-folder" }
  | { kind: "create-tag" }
  | { kind: "move" }
  | { kind: "tags" }
  | { kind: "archive"; asset: MediaLibraryAsset }
  | { kind: "restore"; asset: MediaLibraryAsset }
  | null;

export function MediaLibraryPage() {
  const { locale, t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [includeArchived, setIncludeArchived] = useState(false);
  const [sourceType, setSourceType] = useState<MediaLibrarySourceType | "">("");
  const [folderId, setFolderId] = useState<string | null>(null);
  const [tag, setTag] = useState<string | null>(null);
  const [view, setView] = useState<LibraryView>("grid");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<LibraryDialog>(null);
  const [previewAsset, setPreviewAsset] = useState<MediaLibraryAsset | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(searchInput.trim()), 220);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  useEffect(() => {
    setSelectedIds(new Set());
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
  const operationMutation = useMutation({
    mutationFn: async (operation: {
      kind: "move" | "tags" | "archive" | "restore";
      folderId?: string | null;
      tagNames?: string[];
      asset?: MediaLibraryAsset;
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
      const asset = operation.asset;
      if (!asset) throw new Error(t("mediaLibrary.operationFailed"));
      return operation.kind === "archive"
        ? api.archiveMediaLibraryAsset(asset.id, asset.revision)
        : api.restoreMediaLibraryAsset(asset.id, asset.revision);
    },
    onSuccess: async () => {
      setSelectedIds(new Set());
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
  const createTagMutation = useMutation({
    mutationFn: (name: string) => api.createMediaLibraryTag(name),
    onSuccess: async (createdTag) => {
      setDialog(null);
      setTag(createdTag.name);
      await invalidate();
    },
  });
  const operationError = [
    assetsQuery.error,
    operationMutation.error,
    createFolderMutation.error,
    createTagMutation.error,
  ].find((error): error is Error => error instanceof Error) ?? null;
  const busy = operationMutation.isPending || createFolderMutation.isPending || createTagMutation.isPending;

  const toggleSelected = (assetId: string) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(assetId)) next.delete(assetId);
      else next.add(assetId);
      return next;
    });
  };
  const selectAllLoaded = () => setSelectedIds(new Set(assets.map((asset) => asset.id)));
  const clearFilters = () => {
    setSearchInput("");
    setSearch("");
    setIncludeArchived(false);
    setSourceType("");
    setFolderId(null);
    setTag(null);
  };

  return (
    <div className="min-h-screen bg-slate-50 text-slate-950 dark:bg-[#060a12] dark:text-slate-100">
      <TopNav breadcrumbs={t("mediaLibrary.title")} onHome={() => navigate("/products")} />
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
          </div>
        </header>

        <div className="mt-4 flex min-h-0 flex-1 flex-col gap-4 lg:grid lg:grid-cols-[220px_minmax(0,1fr)]">
          <aside className="min-w-0 rounded-xl border border-slate-200 bg-white p-3 shadow-sm dark:border-slate-800 dark:bg-[#0d131e]">
            <div className="flex items-center justify-between px-2 pb-2">
              <h2 className="text-xs font-bold uppercase tracking-[0.14em] text-slate-500 dark:text-slate-400">{t("mediaLibrary.scope")}</h2>
              {(search || sourceType || folderId || tag || includeArchived) ? (
                <button type="button" onClick={clearFilters} className="text-[11px] font-semibold text-indigo-600 hover:text-indigo-800 dark:text-violet-300">
                  {t("mediaLibrary.clearFilters")}
                </button>
              ) : null}
            </div>
            <FilterButton active={!folderId && !tag && !sourceType} label={t("mediaLibrary.allAssets")} count={bootstrap?.active_count ?? 0} onClick={clearFilters} />
            <FilterButton active={Boolean(!folderId && !tag && !sourceType && includeArchived)} label={t("mediaLibrary.archived")} count={bootstrap?.archived_count ?? 0} onClick={() => { clearFilters(); setIncludeArchived(true); }} />

            <div className="mt-4 border-t border-slate-100 pt-3 dark:border-slate-800">
              <div className="flex items-center justify-between px-2 pb-1.5">
                <div className="text-[11px] font-semibold text-slate-400">{t("mediaLibrary.folders")}</div>
                <button type="button" onClick={() => setDialog({ kind: "create-folder" })} className="inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-indigo-700 dark:hover:bg-slate-800 dark:hover:text-violet-200" aria-label={t("mediaLibrary.createFolder")} title={t("mediaLibrary.createFolder")}>
                  <FolderPlus size={14} />
                </button>
              </div>
              {bootstrap?.folders.map((folder) => (
                <FilterButton key={folder.id} active={folderId === folder.id} label={folder.name} count={folder.count} onClick={() => { setFolderId(folder.id); setTag(null); }} />
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
                <FilterButton key={item.id} active={tag === item.name} label={item.name} count={item.count} onClick={() => { setTag(item.name); setFolderId(null); }} />
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
                <option value="product_asset">{t("mediaLibrary.source.product")}</option>
                <option value="image_session_generated">{t("mediaLibrary.source.session")}</option>
                <option value="legacy_gallery">{t("mediaLibrary.source.legacy")}</option>
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
              <div className="mt-3 flex flex-wrap items-center gap-2 rounded-lg border border-indigo-200 bg-indigo-50 px-3 py-2 text-xs text-indigo-800 dark:border-violet-400/30 dark:bg-violet-500/10 dark:text-violet-100">
                <span className="mr-auto font-semibold">{t("mediaLibrary.selected", { count: selectedAssets.length })}</span>
                <button type="button" onClick={() => setDialog({ kind: "move" })} className="inline-flex h-8 items-center gap-1.5 rounded-md bg-white px-2.5 font-semibold shadow-sm dark:bg-slate-950/70"><FolderInput size={13} />{t("detail.library.move")}</button>
                <button type="button" onClick={() => setDialog({ kind: "tags" })} className="inline-flex h-8 items-center gap-1.5 rounded-md bg-white px-2.5 font-semibold shadow-sm dark:bg-slate-950/70"><Tag size={13} />{t("mediaLibrary.editTags")}</button>
                <button type="button" onClick={() => setSelectedIds(new Set())} className="inline-flex h-8 w-8 items-center justify-center rounded-md hover:bg-white dark:hover:bg-slate-950/70" aria-label={t("mediaLibrary.clearSelection")} title={t("mediaLibrary.clearSelection")}><X size={14} /></button>
              </div>
            ) : null}

            {operationError ? <div role="alert" className="mt-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">{operationError instanceof ApiError ? operationError.detail : operationError.message}</div> : null}

            <div className="mt-3 flex items-center justify-between px-1 text-xs text-slate-500 dark:text-slate-400">
              <span>{t("mediaLibrary.resultCount", { count: assets.length })}</span>
              {assets.length ? <button type="button" onClick={selectAllLoaded} className="font-semibold text-indigo-600 hover:text-indigo-800 dark:text-violet-300">{t("mediaLibrary.selectLoaded")}</button> : null}
            </div>

            {bootstrapQuery.isLoading || assetsQuery.isLoading ? (
              <LibraryState icon={<Loader2 size={18} className="animate-spin" />} text={t("mediaLibrary.loading")} />
            ) : bootstrapQuery.isError || assetsQuery.isError ? (
              <LibraryState text={t("mediaLibrary.loadFailed")} action={t("detail.library.retry")} onAction={() => { void bootstrapQuery.refetch(); void assetsQuery.refetch(); }} />
            ) : assets.length === 0 ? (
              <LibraryState icon={<Images size={22} />} text={t("mediaLibrary.empty")} action={search || folderId || tag || sourceType ? t("mediaLibrary.clearFilters") : undefined} onAction={clearFilters} />
            ) : view === "grid" ? (
              <div className="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
                {assets.map((asset) => <MediaLibraryCard key={asset.id} asset={asset} selected={selectedIds.has(asset.id)} busy={busy} onToggle={() => toggleSelected(asset.id)} onPreview={() => setPreviewAsset(asset)} onArchive={() => setDialog({ kind: asset.is_archived ? "restore" : "archive", asset })} />)}
              </div>
            ) : (
              <div className="mt-2 divide-y divide-slate-100 overflow-hidden rounded-xl border border-slate-200 bg-white dark:divide-slate-800 dark:border-slate-800 dark:bg-[#0d131e]">
                {assets.map((asset) => <MediaLibraryRow key={asset.id} asset={asset} selected={selectedIds.has(asset.id)} busy={busy} onToggle={() => toggleSelected(asset.id)} onPreview={() => setPreviewAsset(asset)} onArchive={() => setDialog({ kind: asset.is_archived ? "restore" : "archive", asset })} />)}
              </div>
            )}
            {assetsQuery.hasNextPage ? <button type="button" onClick={() => assetsQuery.fetchNextPage()} disabled={assetsQuery.isFetchingNextPage} className="mt-3 inline-flex h-9 w-full items-center justify-center rounded-lg border border-slate-200 bg-white text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-950/50 dark:text-slate-200">{assetsQuery.isFetchingNextPage ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : null}{t("detail.library.loadMore")}</button> : null}
          </section>
        </div>
      </main>

      <NameDialog open={dialog?.kind === "create-folder" || dialog?.kind === "create-tag"} title={dialog?.kind === "create-tag" ? t("mediaLibrary.createTag") : t("mediaLibrary.createFolder")} label={dialog?.kind === "create-tag" ? t("mediaLibrary.tagName") : t("mediaLibrary.folderName")} busy={createFolderMutation.isPending || createTagMutation.isPending} onClose={() => setDialog(null)} onSubmit={(name) => dialog?.kind === "create-tag" ? createTagMutation.mutate(name) : createFolderMutation.mutate(name)} />
      <MoveDialog open={dialog?.kind === "move"} folders={bootstrap?.folders ?? []} busy={operationMutation.isPending} onClose={() => setDialog(null)} onMove={(nextFolderId) => operationMutation.mutate({ kind: "move", folderId: nextFolderId })} />
      <NameDialog open={dialog?.kind === "tags"} title={t("mediaLibrary.editTags")} label={t("mediaLibrary.tagsInput")} placeholder={t("mediaLibrary.tagsPlaceholder")} busy={operationMutation.isPending} onClose={() => setDialog(null)} onSubmit={(value) => operationMutation.mutate({ kind: "tags", tagNames: value.split(",").map((item) => item.trim()).filter(Boolean) })} />
      <ConfirmDialog open={dialog?.kind === "archive" || dialog?.kind === "restore"} title={dialog?.kind === "restore" ? t("mediaLibrary.restoreTitle") : t("mediaLibrary.archiveTitle")} description={dialog?.kind === "archive" || dialog?.kind === "restore" ? t(dialog.kind === "restore" ? "mediaLibrary.restoreDescription" : "mediaLibrary.archiveDescription", { name: dialog.asset.display_name }) : ""} confirmLabel={dialog?.kind === "restore" ? t("mediaLibrary.restore") : t("mediaLibrary.archive")} cancelLabel={t("common.cancel")} busy={operationMutation.isPending} destructive={dialog?.kind !== "restore"} onClose={() => setDialog(null)} onConfirm={() => { if (dialog?.kind === "archive" || dialog?.kind === "restore") operationMutation.mutate({ kind: dialog.kind, asset: dialog.asset }); }} />
      {previewAsset ? <GalleryImagePreviewDialog ariaLabel={t("mediaLibrary.previewLabel")} imageUrl={api.toApiUrl(previewAsset.preview_url)} imageAlt={previewAsset.display_name} title={previewAsset.display_name} subtitle={previewAsset.original_filename} body={previewAsset.folder_name ?? t("mediaLibrary.unorganized")} metadataRows={[{ label: t("mediaLibrary.source"), value: sourceLabel(previewAsset.source_type, t) }, { label: t("mediaLibrary.dimensions"), value: previewAsset.width && previewAsset.height ? `${previewAsset.width} x ${previewAsset.height}` : t("common.unknown") }, { label: t("mediaLibrary.createdAt"), value: formatDateTime(previewAsset.created_at, locale) }, { label: t("mediaLibrary.status"), value: previewAsset.is_archived ? t("mediaLibrary.archived") : t("mediaLibrary.active") }]} providerNotes={previewAsset.tags.map((item) => item.name)} providerNotesTitle={t("mediaLibrary.tags")} downloadUrl={previewAsset.download_url} downloadLabel={t("detail.library.download")} closeLabel={t("detail.library.close")} onClose={() => setPreviewAsset(null)} /> : null}
    </div>
  );
}

function Metric({ label, value, muted = false }: { label: string; value: number; muted?: boolean }) {
  return <div className={`rounded-lg border px-2.5 py-1.5 ${muted ? "border-slate-200/70 dark:border-slate-700/70" : "border-indigo-100 bg-indigo-50 dark:border-violet-400/20 dark:bg-violet-500/10"}`}><div className="text-[10px] font-medium">{label}</div><div className="mt-0.5 text-sm font-bold tabular-nums text-slate-800 dark:text-slate-200">{value}</div></div>;
}

function FilterButton({ active, label, count, onClick }: { active: boolean; label: string; count: number; onClick: () => void }) {
  return <button type="button" onClick={onClick} className={`flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-xs font-medium transition-colors ${active ? "bg-indigo-50 text-indigo-700 dark:bg-violet-500/15 dark:text-violet-200" : "text-slate-600 hover:bg-slate-50 dark:text-slate-300 dark:hover:bg-slate-800/70"}`}><span className="min-w-0 flex-1 truncate">{label}</span><span className="shrink-0 text-[10px] tabular-nums text-slate-400">{count}</span></button>;
}

function ViewButton({ active, label, onClick, children }: { active: boolean; label: string; onClick: () => void; children: React.ReactNode }) {
  return <button type="button" onClick={onClick} aria-label={label} title={label} aria-pressed={active} className={`inline-flex h-full w-9 items-center justify-center ${active ? "bg-indigo-50 text-indigo-700 dark:bg-violet-500/20 dark:text-violet-100" : "text-slate-400 hover:text-slate-800 dark:text-slate-500 dark:hover:text-white"}`}>{children}</button>;
}

function MediaLibraryCard({ asset, selected, busy, onToggle, onPreview, onArchive }: { asset: MediaLibraryAsset; selected: boolean; busy: boolean; onToggle: () => void; onPreview: () => void; onArchive: () => void }) {
  const { t } = useI18n();
  return <article className={`group relative overflow-hidden rounded-lg border bg-white shadow-sm transition-shadow hover:shadow-md dark:bg-[#0d131e] ${selected ? "border-indigo-400 ring-2 ring-indigo-100 dark:border-violet-400 dark:ring-violet-500/15" : "border-slate-200 dark:border-slate-800"}`}>
    <div className="relative aspect-square overflow-hidden bg-slate-100 dark:bg-slate-900"><button type="button" onClick={onPreview} className="h-full w-full"><img src={api.toApiUrl(asset.thumbnail_url)} alt={asset.display_name} className={`h-full w-full object-cover transition duration-200 group-hover:scale-[1.02] ${asset.is_archived ? "opacity-55 grayscale" : ""}`} /></button><button type="button" onClick={onToggle} aria-pressed={selected} aria-label={asset.display_name} className={`absolute left-2 top-2 flex h-6 w-6 items-center justify-center rounded-md border shadow-sm ${selected ? "border-indigo-600 bg-indigo-600 text-white dark:border-violet-400 dark:bg-violet-500" : "border-white/80 bg-white/90 text-transparent hover:text-slate-400"}`}><Check size={13} /></button>{asset.is_archived ? <span className="absolute right-2 top-2 rounded-md bg-slate-950/75 px-1.5 py-1 text-[10px] font-semibold text-white">{t("mediaLibrary.archived")}</span> : null}</div>
    <div className="flex min-w-0 items-center gap-2 px-2.5 py-2"><div className="min-w-0 flex-1"><div className="truncate text-xs font-semibold text-slate-800 dark:text-slate-100" title={asset.display_name}>{asset.display_name}</div><div className="mt-0.5 truncate text-[10px] text-slate-400">{asset.folder_name ?? t("mediaLibrary.unorganized")} · {sourceLabel(asset.source_type, t)}</div></div><button type="button" onClick={onArchive} disabled={busy} className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-indigo-700 disabled:opacity-40 dark:hover:bg-slate-800 dark:hover:text-violet-200" aria-label={asset.is_archived ? t("mediaLibrary.restore") : t("mediaLibrary.archive")} title={asset.is_archived ? t("mediaLibrary.restore") : t("mediaLibrary.archive")}>{asset.is_archived ? <ArchiveRestore size={14} /> : <Archive size={14} />}</button></div>
  </article>;
}

function MediaLibraryRow({ asset, selected, busy, onToggle, onPreview, onArchive }: { asset: MediaLibraryAsset; selected: boolean; busy: boolean; onToggle: () => void; onPreview: () => void; onArchive: () => void }) {
  const { t } = useI18n();
  return <div className={`flex min-w-0 items-center gap-3 px-3 py-2.5 ${selected ? "bg-indigo-50/70 dark:bg-violet-500/10" : ""}`}><button type="button" onClick={onToggle} aria-pressed={selected} aria-label={asset.display_name} className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-md border ${selected ? "border-indigo-600 bg-indigo-600 text-white dark:border-violet-400 dark:bg-violet-500" : "border-slate-300 text-transparent dark:border-slate-600"}`}><Check size={13} /></button><button type="button" onClick={onPreview} className="h-12 w-12 shrink-0 overflow-hidden rounded-md bg-slate-100 dark:bg-slate-900"><img src={api.toApiUrl(asset.thumbnail_url)} alt="" className={`h-full w-full object-cover ${asset.is_archived ? "opacity-55 grayscale" : ""}`} /></button><div className="min-w-0 flex-1"><div className="truncate text-xs font-semibold text-slate-800 dark:text-slate-100">{asset.display_name}</div><div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[10px] text-slate-400"><span>{asset.folder_name ?? t("mediaLibrary.unorganized")}</span><span>{sourceLabel(asset.source_type, t)}</span><span>{formatDateTime(asset.created_at, t.locale)}</span></div></div><div className="hidden max-w-[220px] flex-wrap justify-end gap-1 sm:flex">{asset.tags.slice(0, 3).map((item) => <span key={item.id} className="rounded bg-slate-100 px-1.5 py-1 text-[10px] text-slate-500 dark:bg-slate-800 dark:text-slate-300">{item.name}</span>)}</div><button type="button" onClick={onArchive} disabled={busy} className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-indigo-700 disabled:opacity-40 dark:hover:bg-slate-800 dark:hover:text-violet-200" aria-label={asset.is_archived ? t("mediaLibrary.restore") : t("mediaLibrary.archive")} title={asset.is_archived ? t("mediaLibrary.restore") : t("mediaLibrary.archive")}>{asset.is_archived ? <ArchiveRestore size={15} /> : <Archive size={15} />}</button></div>;
}

function sourceLabel(source: MediaLibrarySourceType, t: ReturnType<typeof useI18n>["t"]): string {
  if (source === "product_asset") return t("mediaLibrary.source.product");
  if (source === "image_session_generated") return t("mediaLibrary.source.session");
  return t("mediaLibrary.source.legacy");
}

function LibraryState({ icon, text, action, onAction }: { icon?: React.ReactNode; text: string; action?: string; onAction?: () => void }) {
  return <div className="mt-3 flex min-h-[260px] flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-slate-200 bg-white px-5 text-center text-sm text-slate-500 dark:border-slate-800 dark:bg-[#0d131e] dark:text-slate-400">{icon ? <span className="text-slate-400">{icon}</span> : null}<span>{text}</span>{action && onAction ? <button type="button" onClick={onAction} className="font-semibold text-indigo-600 hover:text-indigo-800 dark:text-violet-300">{action}</button> : null}</div>;
}

function NameDialog({ open, title, label, placeholder, busy, onClose, onSubmit }: { open: boolean; title: string; label: string; placeholder?: string; busy: boolean; onClose: () => void; onSubmit: (value: string) => void }) {
  const { t } = useI18n();
  const [value, setValue] = useState("");
  useEffect(() => { if (open) setValue(""); }, [open]);
  if (!open) return null;
  return <div className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-950/55 p-4" onMouseDown={(event) => event.target === event.currentTarget && !busy && onClose()}><form className="w-full max-w-sm rounded-xl border border-slate-200 bg-white p-4 shadow-2xl dark:border-slate-700 dark:bg-slate-950" onSubmit={(event) => { event.preventDefault(); if (value.trim()) onSubmit(value.trim()); }}><h2 className="text-sm font-semibold text-slate-950 dark:text-white">{title}</h2><label className="mt-4 block text-xs font-medium text-slate-600 dark:text-slate-300">{label}<input autoFocus value={value} onChange={(event) => setValue(event.target.value)} placeholder={placeholder} maxLength={120} className="mt-1.5 h-10 w-full rounded-lg border border-slate-200 px-3 text-sm outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 dark:border-slate-700 dark:bg-slate-900 dark:text-white dark:focus:border-violet-400" /></label><div className="mt-5 flex justify-end gap-2"><button type="button" onClick={onClose} disabled={busy} className="h-9 rounded-lg border border-slate-200 px-3 text-xs font-medium text-slate-600 dark:border-slate-700 dark:text-slate-300">{t("common.cancel")}</button><button type="submit" disabled={busy || !value.trim()} className="inline-flex h-9 items-center rounded-lg bg-indigo-600 px-3 text-xs font-semibold text-white disabled:opacity-50 dark:bg-violet-500">{busy ? <Loader2 size={13} className="mr-1.5 animate-spin" /> : null}{t("detail.library.save")}</button></div></form></div>;
}

function MoveDialog({ open, folders, busy, onClose, onMove }: { open: boolean; folders: Array<{ id: string; name: string }>; busy: boolean; onClose: () => void; onMove: (folderId: string | null) => void }) {
  const { t } = useI18n();
  const [folderId, setFolderId] = useState("");
  useEffect(() => { if (open) setFolderId(""); }, [open]);
  if (!open) return null;
  return <div className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-950/55 p-4" onMouseDown={(event) => event.target === event.currentTarget && !busy && onClose()}><div role="dialog" aria-modal="true" className="w-full max-w-sm rounded-xl border border-slate-200 bg-white p-4 shadow-2xl dark:border-slate-700 dark:bg-slate-950"><h2 className="text-sm font-semibold text-slate-950 dark:text-white">{t("detail.library.moveTitle")}</h2><select value={folderId} onChange={(event) => setFolderId(event.target.value)} className="mt-4 h-10 w-full rounded-lg border border-slate-200 bg-white px-3 text-sm text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-white"><option value="">{t("detail.library.moveUnorganized")}</option>{folders.map((folder) => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</select><div className="mt-5 flex justify-end gap-2"><button type="button" onClick={onClose} disabled={busy} className="h-9 rounded-lg border border-slate-200 px-3 text-xs font-medium text-slate-600 dark:border-slate-700 dark:text-slate-300">{t("common.cancel")}</button><button type="button" onClick={() => onMove(folderId || null)} disabled={busy} className="inline-flex h-9 items-center rounded-lg bg-indigo-600 px-3 text-xs font-semibold text-white disabled:opacity-50 dark:bg-violet-500">{busy ? <Loader2 size={13} className="mr-1.5 animate-spin" /> : null}{t("detail.library.move")}</button></div></div></div>;
}
