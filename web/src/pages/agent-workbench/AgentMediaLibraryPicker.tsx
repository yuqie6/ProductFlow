import { Check, ImagePlus, Loader2, Search, Upload, X } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";

import { api } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import type { AgentAttachment, MediaLibraryAsset } from "../../lib/types";
import { AGENT_COMPOSER_MAX_ASSETS } from "./AgentComposer";

interface AgentMediaLibraryPickerProps {
  selectedAssets: readonly MediaLibraryAsset[];
  onClose: () => void;
  onConfirm: (assets: MediaLibraryAsset[]) => void;
  onPreview: (asset: AgentAttachment) => void;
}

export function AgentMediaLibraryPicker({
  selectedAssets,
  onClose,
  onConfirm,
  onPreview,
}: AgentMediaLibraryPickerProps) {
  const { t } = useI18n();
  const uploadInputId = useId();
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [selectedById, setSelectedById] = useState<Map<string, MediaLibraryAsset>>(
    () => new Map(selectedAssets.map((asset) => [asset.id, asset])),
  );
  const [selectionError, setSelectionError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);

  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(searchInput.trim()), 220);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  const assetsQuery = useInfiniteQuery({
    queryKey: ["agent-global-asset-picker", search],
    queryFn: ({ pageParam }) => api.listMediaLibraryAssets({ cursor: pageParam, q: search, limit: 48 }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
  });
  const assets = useMemo(
    () => assetsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [assetsQuery.data],
  );
  const selectedCount = selectedById.size;
  const errorMessage = selectionError
    ?? (assetsQuery.error instanceof Error ? assetsQuery.error.message : null);

  const toggleAsset = (asset: MediaLibraryAsset) => {
    setSelectedById((current) => {
      const next = new Map(current);
      if (next.has(asset.id)) {
        next.delete(asset.id);
        setSelectionError(null);
        return next;
      }
      if (next.size >= AGENT_COMPOSER_MAX_ASSETS) {
        setSelectionError(t("agentWorkbench.assetLimit", { maximum: AGENT_COMPOSER_MAX_ASSETS }));
        return current;
      }
      if (asset.verification_status === "missing") {
        return current;
      }
      next.set(asset.id, asset);
      setSelectionError(null);
      return next;
    });
  };

  const uploadFiles = async (files: File[]) => {
    if (!files.length) {
      return;
    }
    const remaining = AGENT_COMPOSER_MAX_ASSETS - selectedCount;
    if (remaining <= 0) {
      setSelectionError(t("agentWorkbench.assetLimit", { maximum: AGENT_COMPOSER_MAX_ASSETS }));
      return;
    }
    const filesToUpload = files.slice(0, remaining);
    setUploading(true);
    setSelectionError(null);
    try {
      const uploaded = await api.uploadMediaLibraryAssets(filesToUpload);
      setSelectedById((current) => {
        const next = new Map(current);
        for (const asset of uploaded) {
          next.set(asset.id, asset);
        }
        return next;
      });
      if (files.length > filesToUpload.length) {
        setSelectionError(t("agentWorkbench.assetLimit", { maximum: AGENT_COMPOSER_MAX_ASSETS }));
      }
      await assetsQuery.refetch();
    } catch (error) {
      setSelectionError(error instanceof Error ? error.message : t("globalAgent.assetPicker.uploadFailed"));
    } finally {
      setUploading(false);
    }
  };

  return (
    <div
      data-global-agent-modal
      role="dialog"
      aria-modal="true"
      aria-label={t("globalAgent.assetPicker")}
      className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/70 p-2 backdrop-blur-sm sm:p-4"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <div className="flex h-[min(760px,calc(100dvh-1rem))] w-full max-w-4xl min-h-0 flex-col overflow-hidden rounded-md bg-surface-raised shadow-2xl sm:h-[min(760px,calc(100dvh-2rem))]">
        <header className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-border-l1 px-4">
          <div className="flex min-w-0 items-center gap-2">
            <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent">
              <ImagePlus size={16} aria-hidden="true" />
            </span>
            <h2 className="truncate text-sm font-semibold text-text-primary">{t("globalAgent.assetPicker")}</h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={t("agentWorkbench.closeAssetSelector")}
            title={t("agentWorkbench.closeAssetSelector")}
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-text-muted transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <X size={17} />
          </button>
        </header>

        <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border-l1 px-3 py-3 sm:px-4">
          <label className="flex h-9 min-w-[12rem] flex-1 items-center gap-2 rounded-md border border-border-l2 bg-surface-subtle px-2.5 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/15">
            <Search size={14} className="shrink-0 text-text-muted" aria-hidden="true" />
            <span className="sr-only">{t("globalAgent.assetPicker.searchPlaceholder")}</span>
            <input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder={t("globalAgent.assetPicker.searchPlaceholder")}
              className="min-w-0 flex-1 border-0 bg-transparent text-xs text-text-primary outline-none placeholder:text-text-muted"
            />
          </label>
          <input
            id={uploadInputId}
            type="file"
            accept="image/png,image/jpeg,image/webp"
            multiple
            disabled={uploading}
            className="sr-only"
            onChange={(event) => {
              void uploadFiles(Array.from(event.target.files ?? []));
              event.currentTarget.value = "";
            }}
          />
          <label
            htmlFor={uploadInputId}
            aria-disabled={uploading}
            className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-md border border-border-l2 bg-surface-raised px-3 text-xs font-medium text-text-secondary transition-colors hover:border-accent/50 hover:text-text-primary focus-within:ring-2 focus-within:ring-accent disabled:cursor-wait disabled:opacity-50 aria-disabled:cursor-wait aria-disabled:opacity-50"
          >
            {uploading ? <Loader2 size={14} className="animate-spin motion-reduce:animate-none" /> : <Upload size={14} />}
            {uploading ? t("globalAgent.assetPicker.uploading") : t("globalAgent.assetPicker.upload")}
          </label>
        </div>

        {errorMessage ? (
          <p role="alert" className="shrink-0 border-b border-state-error/20 bg-state-error/10 px-4 py-2 text-xs leading-5 text-state-error">
            {errorMessage}
          </p>
        ) : null}

        <div className="min-h-0 flex-1 overflow-y-auto px-3 py-3 sm:px-4 sm:py-4">
          {assetsQuery.isLoading ? (
            <div className="flex min-h-40 items-center justify-center gap-2 text-sm text-text-secondary">
              <Loader2 size={17} className="animate-spin motion-reduce:animate-none" />
              {t("globalAgent.assetPicker.loading")}
            </div>
          ) : assets.length ? (
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-6">
              {assets.map((asset) => {
                const selected = selectedById.has(asset.id);
                const readable = asset.verification_status !== "missing";
                return (
                  <article
                    key={asset.id}
                    className={`group overflow-hidden rounded-md border bg-surface-raised transition-colors ${
                      selected ? "border-accent ring-2 ring-accent/15" : "border-border-l2"
                    }`}
                  >
                    <div className="relative aspect-square bg-surface-subtle">
                      <button
                        type="button"
                        onClick={() => readable && onPreview(asset)}
                        disabled={!readable}
                        aria-label={asset.display_name}
                        className="h-full w-full disabled:cursor-not-allowed"
                      >
                        {readable ? (
                          <img
                            src={api.toApiUrl(asset.thumbnail_url)}
                            alt=""
                            className="h-full w-full object-cover transition-transform group-hover:scale-[1.02]"
                          />
                        ) : (
                          <span className="flex h-full items-center justify-center px-2 text-center text-[11px] text-text-muted">
                            {t("detail.library.mediaMissing")}
                          </span>
                        )}
                      </button>
                      <button
                        type="button"
                        onClick={() => toggleAsset(asset)}
                        disabled={!readable}
                        aria-label={asset.display_name}
                        aria-pressed={selected}
                        className={`absolute left-1.5 top-1.5 flex h-6 w-6 items-center justify-center rounded border shadow-sm transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-50 ${
                          selected
                            ? "border-accent bg-accent text-white"
                            : "border-white/90 bg-slate-950/45 text-transparent hover:text-white"
                        }`}
                      >
                        <Check size={13} />
                      </button>
                    </div>
                    <div className="truncate px-2 py-2 text-[11px] font-medium text-text-secondary" title={asset.display_name}>
                      {asset.display_name}
                    </div>
                  </article>
                );
              })}
            </div>
          ) : (
            <div className="flex min-h-40 flex-col items-center justify-center gap-2 text-center text-sm text-text-secondary">
              <ImagePlus size={22} className="text-text-muted" />
              {t("globalAgent.assetPicker.empty")}
            </div>
          )}
          {assetsQuery.hasNextPage ? (
            <button
              type="button"
              onClick={() => void assetsQuery.fetchNextPage()}
              disabled={assetsQuery.isFetchingNextPage}
              className="mx-auto mt-4 flex h-9 items-center gap-2 rounded-md border border-border-l2 bg-surface-raised px-3 text-xs font-medium text-text-secondary hover:border-accent/50 hover:text-text-primary disabled:cursor-wait disabled:opacity-50"
            >
              {assetsQuery.isFetchingNextPage ? <Loader2 size={14} className="animate-spin motion-reduce:animate-none" /> : null}
              {t("globalAgent.assetPicker.loadMore")}
            </button>
          ) : null}
        </div>

        <footer className="flex shrink-0 items-center justify-between gap-3 border-t border-border-l1 px-4 py-3">
          <span className="text-xs text-text-secondary">
            {t("globalAgent.assetPicker.selected", { count: selectedCount, maximum: AGENT_COMPOSER_MAX_ASSETS })}
          </span>
          <button
            type="button"
            onClick={() => onConfirm([...selectedById.values()])}
            className="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3 text-xs font-semibold text-white transition-colors hover:bg-accent-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <Check size={14} />
            {t("globalAgent.assetPicker.confirm")}
          </button>
        </footer>
      </div>
    </div>
  );
}
