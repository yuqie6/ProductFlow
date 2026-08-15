import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArrowLeft,
  Bot,
  Boxes,
  Check,
  ChevronRight,
  CircleAlert,
  Download,
  FileJson2,
  FolderClock,
  Image as ImageIcon,
  LayoutTemplate,
  Loader2,
  MessagesSquare,
  Package,
  RotateCw,
  Search,
  UserRound,
  Workflow,
  X,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import type { DownloadableImage } from "../lib/image-downloads";
import { formatDateTime } from "../lib/format";
import type { TranslationKey } from "../lib/i18n";
import { useI18n } from "../lib/preferences";
import type {
  LegacyArchiveAsset,
  LegacyArchiveDetail,
  LegacyArchiveKind,
  LegacyArchiveListItem,
  ProductSummary,
} from "../lib/types";
import { ImagePreviewModal } from "./legacy-history/ImagePreviewModal";
import {
  boundedJson,
  flattenLegacyArchivePages,
  isLegacyArchiveKind,
  isLegacyArchiveMediaAvailable,
  legacyArchiveDetailPath,
  legacyArchiveExportFilename,
  payloadRecord,
  payloadRecords,
  recordText,
} from "./legacy-history/model";

const ARCHIVE_PAGE_SIZE = 30;
const SEARCH_COMMIT_DELAY_MS = 300;

export type LegacyArchiveRebuildTarget =
  | { mode: "select_product" }
  | { mode: "direct"; targetProductId: string }
  | { mode: "unavailable" };

export function resolveLegacyArchiveRebuildTarget(item: LegacyArchiveListItem): LegacyArchiveRebuildTarget {
  if (item.kind === "user_template") {
    return { mode: "select_product" };
  }
  return item.product_id
    ? { mode: "direct", targetProductId: item.product_id }
    : { mode: "unavailable" };
}

export function getOrCreateLegacyArchiveRebuildKey(
  keys: Map<string, string>,
  archive: Pick<LegacyArchiveListItem, "kind" | "id">,
  targetProductId: string,
  createId: () => string = () => globalThis.crypto.randomUUID(),
): string {
  const identity = `${archive.kind}:${archive.id}:${targetProductId}`;
  const existing = keys.get(identity);
  if (existing) {
    return existing;
  }
  const created = `legacy-rebuild:${createId()}`;
  keys.set(identity, created);
  return created;
}

const KIND_OPTIONS: Array<{
  kind: LegacyArchiveKind | null;
  icon: LucideIcon;
  labelKey: TranslationKey;
}> = [
  { kind: null, icon: Boxes, labelKey: "history.kind.all" },
  { kind: "workflow", icon: Workflow, labelKey: "history.kind.workflow" },
  { kind: "canvas_agent_thread", icon: MessagesSquare, labelKey: "history.kind.canvasAgent" },
  { kind: "user_template", icon: LayoutTemplate, labelKey: "history.kind.userTemplate" },
];

const COUNT_LABEL_KEYS: Record<string, TranslationKey> = {
  nodes: "history.count.nodes",
  edges: "history.count.edges",
  runs: "history.count.runs",
  node_runs: "history.count.nodeRuns",
  assets: "history.count.assets",
  messages: "history.count.messages",
  tool_events: "history.count.toolEvents",
  plans: "history.count.plans",
  task_plans: "history.count.taskPlans",
  timeline_events: "history.count.timelineEvents",
  visible_events: "history.count.visibleEvents",
  technical_events: "history.count.technicalEvents",
};

const STATUS_LABEL_KEYS: Record<string, TranslationKey> = {
  active: "history.status.active",
  archived: "history.status.archived",
  cancelled: "history.status.cancelled",
  completed: "history.status.completed",
  damaged: "history.status.damaged",
  failed: "history.status.failed",
};

export function LegacyHistoryPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { archiveKind: archiveKindParam, archiveId } = useParams<{
    archiveKind?: string;
    archiveId?: string;
  }>();
  const [searchParams] = useSearchParams();
  const selectedKind = isLegacyArchiveKind(searchParams.get("kind"))
    ? searchParams.get("kind") as LegacyArchiveKind
    : null;
  const productId = searchParams.get("product_id")?.trim() || null;
  const committedQuery = searchParams.get("q")?.trim() || "";
  const detailKind = isLegacyArchiveKind(archiveKindParam) ? archiveKindParam : null;
  const detailSelected = Boolean(detailKind && archiveId);
  const [searchDraft, setSearchDraft] = useState(committedQuery);
  const [previewAsset, setPreviewAsset] = useState<LegacyArchiveAsset | null>(null);
  const [templateRebuildDetail, setTemplateRebuildDetail] = useState<LegacyArchiveDetail | null>(null);
  const rebuildKeysRef = useRef(new Map<string, string>());

  useEffect(() => {
    setSearchDraft(committedQuery);
  }, [committedQuery]);

  useEffect(() => {
    if (archiveKindParam && !detailKind) {
      navigate(historyListPath(searchParams), { replace: true });
    }
  }, [archiveKindParam, detailKind, navigate, searchParams]);

  useEffect(() => {
    const normalized = searchDraft.trim();
    if (normalized === committedQuery) {
      return;
    }
    const timer = window.setTimeout(() => {
      const next = new URLSearchParams(searchParams);
      if (normalized) {
        next.set("q", normalized);
      } else {
        next.delete("q");
      }
      navigate(historyListPath(next), { replace: true });
    }, SEARCH_COMMIT_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [committedQuery, navigate, searchDraft, searchParams]);

  const archivesQuery = useInfiniteQuery({
    queryKey: ["legacy-archives", selectedKind, productId, committedQuery],
    queryFn: ({ pageParam }) =>
      api.listLegacyArchives({
        kind: selectedKind ?? undefined,
        product_id: productId ?? undefined,
        q: committedQuery,
        after: pageParam,
        limit: ARCHIVE_PAGE_SIZE,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
  });
  const items = useMemo(
    () => flattenLegacyArchivePages(archivesQuery.data?.pages),
    [archivesQuery.data?.pages],
  );
  const pageSummary = archivesQuery.data?.pages[0];
  const total = pageSummary?.total ?? 0;
  const kindCounts = pageSummary?.kind_counts ?? {
    workflow: 0,
    canvas_agent_thread: 0,
    user_template: 0,
  };
  const productName = items.find((item) => item.product_id === productId)?.product_name ?? productId;

  const detailQuery = useQuery({
    queryKey: ["legacy-archive", detailKind, archiveId],
    queryFn: () => api.getLegacyArchive(detailKind!, archiveId!),
    enabled: detailSelected,
  });
  const exportMutation = useMutation({
    mutationFn: async (detail: LegacyArchiveDetail) => ({
      blob: await api.downloadLegacyArchive(detail.item.kind, detail.item.id),
      item: detail.item,
    }),
    onSuccess: ({ blob, item }) => downloadBlob(blob, legacyArchiveExportFilename(item)),
  });
  const rebuildMutation = useMutation({
    mutationFn: ({ detail, targetProductId }: { detail: LegacyArchiveDetail; targetProductId: string }) => {
      const idempotencyKey = getOrCreateLegacyArchiveRebuildKey(
        rebuildKeysRef.current,
        detail.item,
        targetProductId,
      );
      return api.createLegacyArchiveAgentRebuild(detail.item.kind, detail.item.id, {
        target_product_id: targetProductId,
        idempotency_key: idempotencyKey,
      });
    },
    onSuccess: async (result) => {
      setTemplateRebuildDetail(null);
      await queryClient.invalidateQueries({ queryKey: ["agent-workbench", result.target_product_id] });
      navigate(`/products/${encodeURIComponent(result.target_product_id)}`);
    },
  });
  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  const selectKind = (kind: LegacyArchiveKind | null) => {
    const next = new URLSearchParams(searchParams);
    if (kind) {
      next.set("kind", kind);
    } else {
      next.delete("kind");
    }
    navigate(historyListPath(next));
  };
  const clearProductFilter = () => {
    const next = new URLSearchParams(searchParams);
    next.delete("product_id");
    navigate(historyListPath(next));
  };
  const rebuildFromArchive = (detail: LegacyArchiveDetail) => {
    rebuildMutation.reset();
    const target = resolveLegacyArchiveRebuildTarget(detail.item);
    if (target.mode === "select_product") {
      setTemplateRebuildDetail(detail);
      return;
    }
    if (target.mode === "direct") {
      rebuildMutation.mutate({ detail, targetProductId: target.targetProductId });
    }
  };

  return (
    <div className="flex h-[100dvh] flex-col overflow-hidden bg-white text-slate-950 dark:bg-[#060a12] dark:text-slate-100">
      <TopNav
        breadcrumbs={t("history.breadcrumb")}
        onHome={() => navigate("/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="flex min-h-0 flex-1 flex-col pb-[calc(4rem+env(safe-area-inset-bottom))] lg:pb-0">
        <header className={`${detailSelected ? "hidden lg:block" : "block"} shrink-0 border-b border-slate-200 bg-white px-4 py-4 dark:border-slate-800 dark:bg-[#0b0f17] sm:px-6`}>
          <div className="mx-auto flex w-full max-w-[1600px] flex-col gap-3 xl:flex-row xl:items-center">
            <div className="min-w-0 xl:mr-auto">
              <div className="flex items-center gap-2">
                <FolderClock size={20} className="shrink-0 text-blue-600 dark:text-cyan-300" aria-hidden="true" />
                <h1 className="truncate text-lg font-semibold text-slate-950 dark:text-white">
                  {t("history.title")}
                </h1>
                <span className="text-xs tabular-nums text-slate-400 dark:text-slate-500">
                  {t("history.total", { count: total })}
                </span>
              </div>
              {productId ? (
                <div className="mt-1 flex min-w-0 items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                  <Package size={13} className="shrink-0" aria-hidden="true" />
                  <span className="truncate">{productName}</span>
                  <button
                    type="button"
                    onClick={clearProductFilter}
                    className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md hover:bg-slate-100 hover:text-slate-900 dark:hover:bg-slate-800 dark:hover:text-white"
                    aria-label={t("history.clearProductFilter")}
                    title={t("history.clearProductFilter")}
                  >
                    <X size={13} />
                  </button>
                </div>
              ) : null}
            </div>

            <label className="relative block w-full xl:w-[25rem]">
              <Search
                size={16}
                className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"
                aria-hidden="true"
              />
              <input
                type="search"
                value={searchDraft}
                onChange={(event) => setSearchDraft(event.target.value)}
                placeholder={t("history.search")}
                aria-label={t("history.search")}
                className="h-10 w-full rounded-md border border-slate-300 bg-white pl-9 pr-9 text-sm outline-none transition-colors placeholder:text-slate-400 focus:border-blue-500 focus:ring-2 focus:ring-blue-500/15 dark:border-slate-700 dark:bg-[#101621] dark:text-slate-100 dark:focus:border-cyan-400 dark:focus:ring-cyan-400/15"
              />
              {searchDraft ? (
                <button
                  type="button"
                  onClick={() => setSearchDraft("")}
                  className="absolute right-1.5 top-1/2 inline-flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-900 dark:hover:bg-slate-800 dark:hover:text-white"
                  aria-label={t("history.clearSearch")}
                  title={t("history.clearSearch")}
                >
                  <X size={14} />
                </button>
              ) : null}
            </label>
          </div>

          <div className="mx-auto mt-3 flex w-full max-w-[1600px] gap-1 overflow-x-auto lg:hidden">
            <ArchiveKindFilters
              selectedKind={selectedKind}
              kindCounts={kindCounts}
              total={selectedKind === null ? total : Object.values(kindCounts).reduce((sum, count) => sum + count, 0)}
              compact
              onSelect={selectKind}
            />
          </div>
        </header>

        <div className="mx-auto grid min-h-0 w-full max-w-[1600px] flex-1 lg:grid-cols-[220px_minmax(0,1fr)] xl:grid-cols-[220px_380px_minmax(0,1fr)]">
          <aside className="hidden min-h-0 flex-col border-r border-slate-200 bg-slate-50/70 p-3 dark:border-slate-800 dark:bg-[#080c13] lg:flex">
            <div className="px-2 pb-2 pt-1 text-[11px] font-semibold uppercase text-slate-400 dark:text-slate-500">
              {t("history.categories")}
            </div>
            <nav className="space-y-1" aria-label={t("history.categories") }>
              <ArchiveKindFilters
                selectedKind={selectedKind}
                kindCounts={kindCounts}
                total={selectedKind === null ? total : Object.values(kindCounts).reduce((sum, count) => sum + count, 0)}
                onSelect={selectKind}
              />
            </nav>
            <div className="mt-auto border-t border-slate-200 px-2 pt-3 text-xs leading-5 text-slate-500 dark:border-slate-800 dark:text-slate-400">
              <Archive size={14} className="mb-1" aria-hidden="true" />
              {t("history.readOnly")}
            </div>
          </aside>

          <section
            className={`${detailSelected ? "hidden xl:flex" : "flex"} min-h-0 flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-[#0b0f17]`}
            aria-label={t("history.results")}
          >
            <ArchiveList
              items={items}
              selectedId={archiveId ?? null}
              searchParams={searchParams}
              isLoading={archivesQuery.isLoading}
              error={archivesQuery.error}
              hasSearch={Boolean(committedQuery)}
              hasNextPage={archivesQuery.hasNextPage}
              isFetchingNextPage={archivesQuery.isFetchingNextPage}
              onRetry={() => void archivesQuery.refetch()}
              onLoadMore={() => void archivesQuery.fetchNextPage()}
            />
          </section>

          <section
            className={`${detailSelected ? "flex" : "hidden xl:flex"} min-h-0 min-w-0 flex-col bg-slate-50 dark:bg-[#090d14] lg:col-start-2 xl:col-start-3`}
            aria-label={t("history.detail")}
          >
            {detailSelected ? (
              <ArchiveDetailPanel
                detail={detailQuery.data ?? null}
                error={detailQuery.error}
                isLoading={detailQuery.isLoading}
                isExporting={exportMutation.isPending}
                exportError={exportMutation.error}
                isRebuilding={rebuildMutation.isPending}
                rebuildError={
                  rebuildMutation.variables?.detail.item.id === detailQuery.data?.item.id
                    ? rebuildMutation.error
                    : null
                }
                onBack={() => navigate(historyListPath(searchParams))}
                onRetry={() => void detailQuery.refetch()}
                onExport={() => {
                  if (detailQuery.data) {
                    exportMutation.mutate(detailQuery.data);
                  }
                }}
                onRebuild={() => {
                  if (detailQuery.data) {
                    rebuildFromArchive(detailQuery.data);
                  }
                }}
                onPreviewAsset={setPreviewAsset}
              />
            ) : (
              <div className="flex min-h-0 flex-1 items-center justify-center p-8 text-center">
                <div className="max-w-xs text-slate-400 dark:text-slate-500">
                  <FileJson2 size={28} className="mx-auto" aria-hidden="true" />
                  <p className="mt-3 text-sm leading-6">{t("history.selectItem")}</p>
                </div>
              </div>
            )}
          </section>
        </div>
      </main>

      {previewAsset ? (
        <ImagePreviewModal image={archiveAssetImage(previewAsset)} onClose={() => setPreviewAsset(null)} />
      ) : null}
      {templateRebuildDetail ? (
        <RebuildTargetDialog
          key={templateRebuildDetail.item.id}
          archive={templateRebuildDetail.item}
          busy={rebuildMutation.isPending}
          error={rebuildMutation.error}
          onClose={() => {
            if (!rebuildMutation.isPending) {
              setTemplateRebuildDetail(null);
              rebuildMutation.reset();
            }
          }}
          onSubmit={(targetProductId) => {
            rebuildMutation.mutate({ detail: templateRebuildDetail, targetProductId });
          }}
        />
      ) : null}
    </div>
  );
}

function ArchiveKindFilters({
  selectedKind,
  kindCounts,
  total,
  compact = false,
  onSelect,
}: {
  selectedKind: LegacyArchiveKind | null;
  kindCounts: Record<LegacyArchiveKind, number>;
  total: number;
  compact?: boolean;
  onSelect: (kind: LegacyArchiveKind | null) => void;
}) {
  const { t } = useI18n();
  return KIND_OPTIONS.map((option) => {
    const Icon = option.icon;
    const active = selectedKind === option.kind;
    const count = option.kind === null ? total : kindCounts[option.kind];
    return (
      <button
        key={option.kind ?? "all"}
        type="button"
        onClick={() => onSelect(option.kind)}
        aria-current={active ? "page" : undefined}
        className={[
          "flex shrink-0 items-center text-left text-sm font-medium transition-colors",
          compact ? "h-9 rounded-md px-3" : "h-10 w-full rounded-md px-2.5",
          active
            ? "bg-blue-50 text-blue-700 dark:bg-cyan-400/10 dark:text-cyan-200"
            : "text-slate-600 hover:bg-white hover:text-slate-950 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white",
        ].join(" ")}
      >
        <Icon size={15} className="shrink-0" aria-hidden="true" />
        <span className="ml-2 whitespace-nowrap">{t(option.labelKey)}</span>
        <span className="ml-auto pl-3 text-xs tabular-nums text-slate-400 dark:text-slate-500">{count}</span>
      </button>
    );
  });
}

function ArchiveList({
  items,
  selectedId,
  searchParams,
  isLoading,
  error,
  hasSearch,
  hasNextPage,
  isFetchingNextPage,
  onRetry,
  onLoadMore,
}: {
  items: LegacyArchiveListItem[];
  selectedId: string | null;
  searchParams: URLSearchParams;
  isLoading: boolean;
  error: unknown;
  hasSearch: boolean;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onRetry: () => void;
  onLoadMore: () => void;
}) {
  const { locale, t } = useI18n();
  if (isLoading) {
    return <CenteredState icon={<Loader2 size={22} className="animate-spin" />} label={t("history.loading")} />;
  }
  if (error) {
    return (
      <CenteredState
        icon={<CircleAlert size={22} />}
        label={errorDetail(error, t("history.loadFailed"))}
        actionLabel={t("history.retry")}
        onAction={onRetry}
      />
    );
  }
  if (!items.length) {
    return (
      <CenteredState
        icon={<Archive size={24} />}
        label={hasSearch ? t("history.emptySearch") : t("history.empty")}
      />
    );
  }
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="divide-y divide-slate-200 dark:divide-slate-800">
        {items.map((item) => {
          const active = selectedId === item.id;
          return (
            <Link
              key={`${item.kind}:${item.id}`}
              to={legacyArchiveDetailPath(item.kind, item.id, searchParams)}
              aria-current={active ? "true" : undefined}
              className={`group flex min-h-[102px] items-start gap-3 px-4 py-3.5 transition-colors ${
                active
                  ? "bg-blue-50/80 dark:bg-cyan-400/8"
                  : "hover:bg-slate-50 dark:hover:bg-slate-900/65"
              }`}
            >
              <KindIcon kind={item.kind} className={active ? "text-blue-600 dark:text-cyan-300" : "text-slate-400"} />
              <span className="min-w-0 flex-1">
                <span className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-sm font-semibold text-slate-900 dark:text-slate-100">{item.title}</span>
                  {item.source_status ? (
                    <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold text-slate-500 ring-1 ring-slate-200 dark:text-slate-400 dark:ring-slate-700">
                      {archiveStatusLabel(item.source_status, t)}
                    </span>
                  ) : null}
                </span>
                <span className="mt-1 flex items-center gap-1.5 truncate text-xs text-slate-500 dark:text-slate-400">
                  {item.product_name ? <Package size={12} className="shrink-0" aria-hidden="true" /> : null}
                  {item.product_name ?? item.source_key ?? t(kindLabelKey(item.kind))}
                </span>
                <span className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-slate-400 dark:text-slate-500">
                  <span>{formatDateTime(item.created_at, locale)}</span>
                  <span>{archiveCountSummary(item, t)}</span>
                </span>
              </span>
              <ChevronRight
                size={16}
                className="mt-1 shrink-0 text-slate-300 transition-transform group-hover:translate-x-0.5 group-hover:text-slate-500 dark:text-slate-700"
                aria-hidden="true"
              />
            </Link>
          );
        })}
      </div>
      {hasNextPage ? (
        <div className="border-t border-slate-200 p-3 dark:border-slate-800">
          <button
            type="button"
            onClick={onLoadMore}
            disabled={isFetchingNextPage}
            className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md border border-slate-300 bg-white text-sm font-semibold text-slate-700 hover:border-blue-400 hover:text-blue-700 disabled:opacity-60 dark:border-slate-700 dark:bg-[#101621] dark:text-slate-200 dark:hover:border-cyan-400 dark:hover:text-cyan-200"
          >
            {isFetchingNextPage ? <Loader2 size={15} className="animate-spin" /> : null}
            {isFetchingNextPage ? t("history.loadingMore") : t("history.loadMore")}
          </button>
        </div>
      ) : null}
    </div>
  );
}

function ArchiveDetailPanel({
  detail,
  error,
  isLoading,
  isExporting,
  exportError,
  isRebuilding,
  rebuildError,
  onBack,
  onRetry,
  onExport,
  onRebuild,
  onPreviewAsset,
}: {
  detail: LegacyArchiveDetail | null;
  error: unknown;
  isLoading: boolean;
  isExporting: boolean;
  exportError: unknown;
  isRebuilding: boolean;
  rebuildError: unknown;
  onBack: () => void;
  onRetry: () => void;
  onExport: () => void;
  onRebuild: () => void;
  onPreviewAsset: (asset: LegacyArchiveAsset) => void;
}) {
  const { locale, t } = useI18n();
  if (isLoading) {
    return <CenteredState icon={<Loader2 size={22} className="animate-spin" />} label={t("history.detailLoading")} />;
  }
  if (error || !detail) {
    return (
      <CenteredState
        icon={<CircleAlert size={22} />}
        label={errorDetail(error, t("history.detailLoadFailed"))}
        actionLabel={t("history.retry")}
        onAction={onRetry}
      />
    );
  }
  const item = detail.item;
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-slate-200 bg-white/96 px-3 py-2.5 backdrop-blur dark:border-slate-800 dark:bg-[#0b0f17]/96 sm:px-5">
        <button
          type="button"
          onClick={onBack}
          className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-950 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white xl:hidden"
          aria-label={t("history.backToList")}
          title={t("history.backToList")}
        >
          <ArrowLeft size={17} />
        </button>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-slate-950 dark:text-white">{item.title}</div>
          <div className="mt-0.5 text-[11px] text-slate-400 dark:text-slate-500">{t(kindLabelKey(item.kind))}</div>
        </div>
        <button
          type="button"
          onClick={onRebuild}
          disabled={isRebuilding || (item.kind !== "user_template" && !item.product_id)}
          className="inline-flex h-9 shrink-0 items-center gap-2 rounded-md bg-blue-600 px-3 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-60 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300"
          title={t("history.rebuild")}
        >
          {isRebuilding ? <Loader2 size={14} className="animate-spin" /> : <Bot size={14} />}
          <span className="hidden sm:inline">
            {isRebuilding ? t("history.rebuilding") : t("history.rebuild")}
          </span>
        </button>
        <button
          type="button"
          onClick={onExport}
          disabled={isExporting}
          className="inline-flex h-9 shrink-0 items-center gap-2 rounded-md bg-slate-950 px-3 text-xs font-semibold text-white hover:bg-blue-700 disabled:opacity-60 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300"
          title={t("history.export")}
        >
          {isExporting ? <Loader2 size={14} className="animate-spin" /> : <Download size={14} />}
          <span className="hidden sm:inline">{isExporting ? t("history.exporting") : t("history.export")}</span>
        </button>
      </div>

      {exportError ? (
        <div role="alert" className="border-b border-red-200 bg-red-50 px-5 py-2 text-xs text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          {errorDetail(exportError, t("history.exportFailed"))}
        </div>
      ) : null}
      {rebuildError ? (
        <div role="alert" className="border-b border-red-200 bg-red-50 px-5 py-2 text-xs text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          {errorDetail(rebuildError, t("history.rebuildFailed"))}
        </div>
      ) : null}

      <section className="bg-white px-4 py-5 dark:bg-[#0b0f17] sm:px-6">
        <div className="flex items-start gap-3">
          <KindIcon kind={item.kind} className="text-blue-600 dark:text-cyan-300" large />
          <div className="min-w-0">
            <h2 className="text-xl font-semibold text-slate-950 dark:text-white">{item.title}</h2>
            {item.description ? (
              <p className="mt-1.5 text-sm leading-6 text-slate-600 dark:text-slate-300">{item.description}</p>
            ) : null}
            {item.product_id ? (
              <Link
                to={`/products/${encodeURIComponent(item.product_id)}`}
                className="mt-2 inline-flex items-center gap-1.5 text-xs font-semibold text-blue-700 hover:underline dark:text-cyan-300"
              >
                <Package size={13} aria-hidden="true" />
                {item.product_name ?? item.product_id}
              </Link>
            ) : null}
          </div>
        </div>

        <dl className="mt-5 grid grid-cols-1 gap-x-6 gap-y-3 border-t border-slate-200 pt-4 text-xs dark:border-slate-800 sm:grid-cols-2">
          <Metadata label={t("history.meta.sourceId")} value={item.source_id} mono />
          {item.source_key ? <Metadata label={t("history.meta.sourceKey")} value={item.source_key} mono /> : null}
          <Metadata label={t("history.meta.archivedAt")} value={formatDateTime(item.created_at, locale)} />
          <Metadata label={t("history.meta.sourceUpdatedAt")} value={formatDateTime(item.source_updated_at, locale)} />
          <Metadata label={t("history.meta.sourceProfile")} value={detail.source_profile} mono />
          <Metadata label={t("history.meta.schemaVersion")} value={`v${item.archive_schema_version}`} />
          <Metadata label={t("history.meta.payloadHash")} value={item.payload_sha256} mono />
        </dl>

        {Object.keys(item.counts).length ? (
          <div className="mt-5 flex flex-wrap gap-2 border-t border-slate-200 pt-4 dark:border-slate-800">
            {Object.entries(item.counts).map(([key, count]) => (
              <span
                key={key}
                className="inline-flex items-center gap-1.5 rounded-md bg-slate-100 px-2 py-1 text-xs text-slate-600 dark:bg-slate-800 dark:text-slate-300"
              >
                <span className="font-semibold tabular-nums text-slate-950 dark:text-white">{count}</span>
                {t(COUNT_LABEL_KEYS[key] ?? "history.count.records")}
              </span>
            ))}
          </div>
        ) : null}
      </section>

      {detail.diagnostics.length ? (
        <section className="border-t border-amber-200 bg-amber-50 px-4 py-4 dark:border-amber-400/25 dark:bg-amber-500/8 sm:px-6">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-amber-900 dark:text-amber-100">
            <CircleAlert size={15} /> {t("history.diagnostics")}
          </h3>
          <div className="mt-2 space-y-2">
            {detail.diagnostics.map((diagnostic, index) => (
              <pre key={index} className="whitespace-pre-wrap break-words text-xs leading-5 text-amber-800 dark:text-amber-200">
                {boundedJson(diagnostic, 2_000)}
              </pre>
            ))}
          </div>
        </section>
      ) : null}

      {detail.assets.length ? (
        <ArchiveAssets assets={detail.assets} onPreview={onPreviewAsset} />
      ) : null}

      <ArchivePayload detail={detail} />
    </div>
  );
}

function ArchiveAssets({
  assets,
  onPreview,
}: {
  assets: LegacyArchiveAsset[];
  onPreview: (asset: LegacyArchiveAsset) => void;
}) {
  const { t } = useI18n();
  return (
    <section className="border-t border-slate-200 bg-white px-4 py-5 dark:border-slate-800 dark:bg-[#0b0f17] sm:px-6">
      <h3 className="flex items-center gap-2 text-sm font-semibold text-slate-950 dark:text-white">
        <ImageIcon size={15} /> {t("history.assets")}
        <span className="text-xs font-normal tabular-nums text-slate-400">{assets.length}</span>
      </h3>
      <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3 2xl:grid-cols-4">
        {assets.map((asset) => {
          const available = isLegacyArchiveMediaAvailable(asset.verification_status);
          return (
            <div key={`${asset.product_image_asset_id}:${asset.role}:${asset.legacy_source_id}`} className="min-w-0 overflow-hidden rounded-md border border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-900">
              <button
                type="button"
                onClick={() => onPreview(asset)}
                disabled={!available}
                className="flex aspect-square w-full items-center justify-center overflow-hidden bg-slate-100 disabled:cursor-not-allowed dark:bg-slate-950"
                aria-label={t("history.previewAsset", { name: asset.display_name })}
                title={available ? t("history.previewAsset", { name: asset.display_name }) : t("history.assetMissing")}
              >
                {available ? (
                  <img
                    src={api.toApiUrl(asset.thumbnail_url)}
                    alt={asset.display_name}
                    loading="lazy"
                    decoding="async"
                    className="h-full w-full object-contain transition-transform duration-200 hover:scale-[1.02]"
                  />
                ) : (
                  <CircleAlert size={22} className="text-slate-400" />
                )}
              </button>
              <div className="flex items-center gap-2 border-t border-slate-200 px-2.5 py-2 dark:border-slate-700">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-xs font-semibold text-slate-800 dark:text-slate-200">{asset.display_name}</div>
                  <div className="mt-0.5 truncate text-[10px] text-slate-400">{asset.role}</div>
                </div>
                {available ? (
                  <a
                    href={api.toApiUrl(asset.download_url)}
                    download={asset.original_filename}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-slate-500 hover:bg-white hover:text-blue-700 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-cyan-300"
                    aria-label={t("history.downloadAsset", { name: asset.display_name })}
                    title={t("history.downloadAsset", { name: asset.display_name })}
                  >
                    <Download size={14} />
                  </a>
                ) : null}
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}

function ArchivePayload({ detail }: { detail: LegacyArchiveDetail }) {
  if (detail.item.kind === "canvas_agent_thread") {
    return <CanvasAgentSnapshot payload={detail.payload} />;
  }
  if (detail.item.kind === "workflow") {
    return <WorkflowSnapshot payload={detail.payload} />;
  }
  return <TemplateSnapshot payload={detail.payload} />;
}

function WorkflowSnapshot({ payload }: { payload: Record<string, unknown> }) {
  const { t } = useI18n();
  const nodes = payloadRecords(payload, "nodes");
  const copySets = payloadRecords(payload, "copy_sets");
  const briefs = payloadRecords(payload, "creative_briefs");
  const runs = payloadRecords(payload, "runs");
  return (
    <div className="border-t border-slate-200 bg-white dark:border-slate-800 dark:bg-[#0b0f17]">
      <ArchiveRecordSection
        title={t("history.snapshot.nodes")}
        records={nodes}
        titleForRecord={(record, index) => recordText(record, "title") ?? recordText(record, "node_key") ?? `${t("history.snapshot.node")} ${index + 1}`}
        metaForRecord={(record) => [recordText(record, "node_type"), recordText(record, "status")].filter(Boolean).join(" · ")}
      />
      <ArchiveRecordSection
        title={t("history.snapshot.copySets")}
        records={copySets}
        titleForRecord={(record, index) => recordText(record, "status") ?? `${t("history.snapshot.copySet")} ${index + 1}`}
      />
      <ArchiveRecordSection
        title={t("history.snapshot.briefs")}
        records={briefs}
        titleForRecord={(_, index) => `${t("history.snapshot.brief")} ${index + 1}`}
      />
      <ArchiveRecordSection
        title={t("history.snapshot.runs")}
        records={runs}
        titleForRecord={(record, index) => recordText(record, "status") ?? `${t("history.snapshot.run")} ${index + 1}`}
        metaForRecord={(record) => recordText(record, "started_at") ?? ""}
      />
    </div>
  );
}

function CanvasAgentSnapshot({ payload }: { payload: Record<string, unknown> }) {
  const { t } = useI18n();
  const messages = payloadRecords(payload, "messages");
  const plans = payloadRecords(payload, "plans");
  const taskPlans = payloadRecords(payload, "task_plans");
  const timeline = payloadRecords(payload, "visible_timeline");
  return (
    <div className="border-t border-slate-200 bg-white dark:border-slate-800 dark:bg-[#0b0f17]">
      {messages.length ? (
        <section className="px-4 py-5 sm:px-6">
          <h3 className="text-sm font-semibold text-slate-950 dark:text-white">{t("history.snapshot.messages")}</h3>
          <div className="mt-4 space-y-4">
            {messages.slice(0, 100).map((message, index) => {
              const role = recordText(message, "role") ?? "assistant";
              const user = role === "user";
              return (
                <div key={recordText(message, "id") ?? index} className="flex items-start gap-3">
                  <span className={`inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md ${user ? "bg-blue-100 text-blue-700 dark:bg-blue-500/15 dark:text-blue-200" : "bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-200"}`}>
                    {user ? <UserRound size={14} /> : <Bot size={14} />}
                  </span>
                  <div className="min-w-0 flex-1 border-b border-slate-100 pb-4 dark:border-slate-800">
                    <div className="text-[11px] font-semibold text-slate-400">{user ? t("history.snapshot.user") : t("history.snapshot.agent")}</div>
                    <p className="mt-1 whitespace-pre-wrap break-words text-sm leading-6 text-slate-700 dark:text-slate-300">
                      {(recordText(message, "content") ?? t("history.snapshot.emptyContent")).slice(0, 12_000)}
                    </p>
                  </div>
                </div>
              );
            })}
          </div>
        </section>
      ) : null}
      <ArchiveRecordSection
        title={t("history.snapshot.plans")}
        records={plans}
        titleForRecord={(record, index) => recordText(record, "status") ?? `${t("history.snapshot.plan")} ${index + 1}`}
      />
      <ArchiveRecordSection
        title={t("history.snapshot.taskPlans")}
        records={taskPlans}
        titleForRecord={(record, index) => recordText(record, "status") ?? `${t("history.snapshot.taskPlan")} ${index + 1}`}
      />
      <ArchiveRecordSection
        title={t("history.snapshot.timeline")}
        records={timeline}
        titleForRecord={(record, index) => recordText(record, "type") ?? `${t("history.snapshot.event")} ${index + 1}`}
      />
    </div>
  );
}

function TemplateSnapshot({ payload }: { payload: Record<string, unknown> }) {
  const { t } = useI18n();
  const template = payloadRecord(payload.template_json) ?? payloadRecord(payload.template) ?? payload;
  const nodes = payloadRecords(template, "nodes");
  const edges = payloadRecords(template, "edges");
  return (
    <div className="border-t border-slate-200 bg-white dark:border-slate-800 dark:bg-[#0b0f17]">
      <ArchiveRecordSection
        title={t("history.snapshot.nodes")}
        records={nodes}
        titleForRecord={(record, index) => recordText(record, "title") ?? recordText(record, "node_key") ?? `${t("history.snapshot.node")} ${index + 1}`}
      />
      <ArchiveRecordSection
        title={t("history.snapshot.edges")}
        records={edges}
        titleForRecord={(record, index) => recordText(record, "edge_key") ?? `${t("history.snapshot.edge")} ${index + 1}`}
      />
      <section className="border-t border-slate-200 px-4 py-5 dark:border-slate-800 sm:px-6">
        <details>
          <summary className="cursor-pointer text-sm font-semibold text-slate-950 hover:text-blue-700 dark:text-white dark:hover:text-cyan-300">
            {t("history.snapshot.templateData")}
          </summary>
          <pre className="mt-3 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-md bg-slate-950 p-3 text-[11px] leading-5 text-slate-200">
            {boundedJson(template, 12_000)}
          </pre>
        </details>
      </section>
    </div>
  );
}

function ArchiveRecordSection({
  title,
  records,
  titleForRecord,
  metaForRecord,
}: {
  title: string;
  records: Array<Record<string, unknown>>;
  titleForRecord: (record: Record<string, unknown>, index: number) => string;
  metaForRecord?: (record: Record<string, unknown>, index: number) => string;
}) {
  if (!records.length) {
    return null;
  }
  return (
    <section className="border-t border-slate-200 px-4 py-5 first:border-t-0 dark:border-slate-800 sm:px-6">
      <h3 className="text-sm font-semibold text-slate-950 dark:text-white">
        {title} <span className="ml-1 text-xs font-normal tabular-nums text-slate-400">{records.length}</span>
      </h3>
      <div className="mt-3 divide-y divide-slate-200 border-y border-slate-200 dark:divide-slate-800 dark:border-slate-800">
        {records.slice(0, 100).map((record, index) => (
          <details key={recordText(record, "id") ?? index} className="group py-2.5">
            <summary className="flex cursor-pointer list-none items-center gap-2 text-xs font-semibold text-slate-700 marker:hidden hover:text-blue-700 dark:text-slate-300 dark:hover:text-cyan-300 [&::-webkit-details-marker]:hidden">
              <ChevronRight size={13} className="shrink-0 transition-transform group-open:rotate-90" />
              <span className="min-w-0 flex-1 truncate">{titleForRecord(record, index)}</span>
              {metaForRecord ? (
                <span className="shrink-0 font-normal text-slate-400">{metaForRecord(record, index)}</span>
              ) : null}
            </summary>
            <pre className="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-md bg-slate-950 p-3 text-[11px] leading-5 text-slate-200">
              {boundedJson(record)}
            </pre>
          </details>
        ))}
      </div>
    </section>
  );
}

function RebuildTargetDialog({
  archive,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  archive: LegacyArchiveListItem;
  busy: boolean;
  error: unknown;
  onClose: () => void;
  onSubmit: (targetProductId: string) => void;
}) {
  const { t } = useI18n();
  const headingId = useId();
  const [searchDraft, setSearchDraft] = useState("");
  const [committedSearch, setCommittedSearch] = useState("");
  const [selectedProductId, setSelectedProductId] = useState<string | null>(null);
  const productsQuery = useQuery({
    queryKey: ["products", "legacy-archive-rebuild-targets", committedSearch],
    queryFn: () => api.listProducts({ q: committedSearch, page_size: 20 }),
  });
  const products = productsQuery.data?.items ?? [];

  useEffect(() => {
    const timer = window.setTimeout(() => setCommittedSearch(searchDraft.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [searchDraft]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) {
        onClose();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [busy, onClose]);

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/60 p-3 backdrop-blur-sm sm:p-6"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !busy) {
          onClose();
        }
      }}
    >
      <form
        role="dialog"
        aria-modal="true"
        aria-labelledby={headingId}
        className="flex max-h-[min(680px,calc(100dvh-1.5rem))] w-full max-w-xl flex-col overflow-hidden rounded-lg border border-slate-200 bg-white shadow-2xl shadow-slate-950/25 dark:border-slate-700 dark:bg-[#0f151f]"
        onSubmit={(event) => {
          event.preventDefault();
          if (selectedProductId) {
            onSubmit(selectedProductId);
          }
        }}
      >
        <header className="flex items-start gap-3 border-b border-slate-200 px-4 py-4 dark:border-slate-800 sm:px-5">
          <span className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-blue-50 text-blue-600 dark:bg-cyan-400/10 dark:text-cyan-300">
            <LayoutTemplate size={17} aria-hidden="true" />
          </span>
          <div className="min-w-0 flex-1">
            <h2 id={headingId} className="text-base font-semibold text-slate-950 dark:text-white">
              {t("history.rebuildTargetTitle")}
            </h2>
            <p className="mt-1 truncate text-xs text-slate-500 dark:text-slate-400">{archive.title}</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-950 disabled:opacity-50 dark:hover:bg-slate-800 dark:hover:text-white"
            aria-label={t("workflowV2.dialog.close")}
            title={t("workflowV2.dialog.close")}
          >
            <X size={16} />
          </button>
        </header>

        <div className="border-b border-slate-200 p-3 dark:border-slate-800 sm:px-5">
          <label className="relative block">
            <Search
              size={15}
              className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"
              aria-hidden="true"
            />
            <input
              autoFocus
              type="search"
              value={searchDraft}
              onChange={(event) => setSearchDraft(event.target.value)}
              placeholder={t("history.rebuildTargetSearch")}
              aria-label={t("history.rebuildTargetSearch")}
              className="h-10 w-full rounded-md border border-slate-300 bg-white pl-9 pr-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/15 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100 dark:focus:border-cyan-400 dark:focus:ring-cyan-400/15"
            />
          </label>
        </div>

        <div className="flex min-h-[260px] flex-1 flex-col overflow-y-auto">
          {productsQuery.isLoading ? (
            <CenteredState
              icon={<Loader2 size={20} className="animate-spin" />}
              label={t("history.rebuildTargetLoading")}
            />
          ) : productsQuery.error ? (
            <CenteredState
              icon={<CircleAlert size={20} />}
              label={errorDetail(productsQuery.error, t("history.rebuildTargetLoadFailed"))}
              actionLabel={t("history.retry")}
              onAction={() => void productsQuery.refetch()}
            />
          ) : products.length ? (
            <div className="divide-y divide-slate-100 dark:divide-slate-800">
              {products.map((product) => (
                <RebuildTargetProductRow
                  key={product.id}
                  product={product}
                  selected={selectedProductId === product.id}
                  disabled={busy}
                  onSelect={() => setSelectedProductId(product.id)}
                />
              ))}
            </div>
          ) : (
            <CenteredState icon={<Package size={21} />} label={t("history.rebuildTargetEmpty")} />
          )}
        </div>

        {error ? (
          <div role="alert" className="border-t border-red-200 bg-red-50 px-5 py-2 text-xs text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
            {errorDetail(error, t("history.rebuildFailed"))}
          </div>
        ) : null}

        <footer className="flex justify-end gap-2 border-t border-slate-200 bg-slate-50 px-4 py-3 dark:border-slate-800 dark:bg-slate-950/45 sm:px-5">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="h-9 rounded-md border border-slate-300 bg-white px-3 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
          >
            {t("common.cancel")}
          </button>
          <button
            type="submit"
            disabled={!selectedProductId || busy}
            className="inline-flex h-9 min-w-[112px] items-center justify-center gap-2 rounded-md bg-blue-600 px-3 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300"
          >
            {busy ? <Loader2 size={15} className="animate-spin" /> : <Bot size={15} />}
            {busy ? t("history.rebuilding") : t("history.rebuild")}
          </button>
        </footer>
      </form>
    </div>
  );
}

function RebuildTargetProductRow({
  product,
  selected,
  disabled,
  onSelect,
}: {
  product: ProductSummary;
  selected: boolean;
  disabled: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      disabled={disabled}
      aria-pressed={selected}
      className={`flex min-h-[68px] w-full items-center gap-3 px-4 py-2.5 text-left transition-colors disabled:opacity-60 sm:px-5 ${
        selected
          ? "bg-blue-50 text-blue-950 dark:bg-cyan-400/10 dark:text-cyan-50"
          : "hover:bg-slate-50 dark:hover:bg-slate-900"
      }`}
    >
      <span className="flex h-11 w-11 shrink-0 items-center justify-center overflow-hidden rounded-md border border-slate-200 bg-slate-100 dark:border-slate-700 dark:bg-slate-800">
        {product.cover_image_thumbnail_url ? (
          <img
            src={api.toApiUrl(product.cover_image_thumbnail_url)}
            alt=""
            className="h-full w-full object-cover"
          />
        ) : (
          <Package size={17} className="text-slate-400" aria-hidden="true" />
        )}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-semibold">{product.name}</span>
        <span className="mt-0.5 block truncate text-xs text-slate-500 dark:text-slate-400">
          {product.category ?? product.id}
        </span>
      </span>
      <span
        className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full border ${
          selected
            ? "border-blue-600 bg-blue-600 text-white dark:border-cyan-300 dark:bg-cyan-300 dark:text-slate-950"
            : "border-slate-300 text-transparent dark:border-slate-600"
        }`}
        aria-hidden="true"
      >
        <Check size={13} />
      </span>
    </button>
  );
}

function Metadata({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-slate-400 dark:text-slate-500">{label}</dt>
      <dd className={`mt-1 break-all text-slate-700 dark:text-slate-300 ${mono ? "font-mono text-[11px]" : ""}`}>{value}</dd>
    </div>
  );
}

function KindIcon({
  kind,
  className = "",
  large = false,
}: {
  kind: LegacyArchiveKind;
  className?: string;
  large?: boolean;
}) {
  const Icon = kind === "workflow" ? Workflow : kind === "canvas_agent_thread" ? MessagesSquare : LayoutTemplate;
  return (
    <span className={`inline-flex ${large ? "h-10 w-10" : "mt-0.5 h-8 w-8"} shrink-0 items-center justify-center rounded-md bg-slate-100 dark:bg-slate-800 ${className}`}>
      <Icon size={large ? 19 : 15} aria-hidden="true" />
    </span>
  );
}

function CenteredState({
  icon,
  label,
  actionLabel,
  onAction,
}: {
  icon: React.ReactNode;
  label: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center p-8 text-center text-slate-400 dark:text-slate-500">
      <div className="max-w-sm">
        <div className="flex justify-center">{icon}</div>
        <p className="mt-3 text-sm leading-6">{label}</p>
        {actionLabel && onAction ? (
          <button
            type="button"
            onClick={onAction}
            className="mt-4 inline-flex h-9 items-center gap-2 rounded-md border border-slate-300 px-3 text-xs font-semibold text-slate-700 hover:border-blue-400 hover:text-blue-700 dark:border-slate-700 dark:text-slate-200 dark:hover:border-cyan-400 dark:hover:text-cyan-200"
          >
            <RotateCw size={14} /> {actionLabel}
          </button>
        ) : null}
      </div>
    </div>
  );
}

function archiveCountSummary(
  item: LegacyArchiveListItem,
  t: ReturnType<typeof useI18n>["t"],
): string {
  const entries = Object.entries(item.counts).filter(([, count]) => count > 0).slice(0, 2);
  if (!entries.length) {
    return t("history.count.noRecords");
  }
  return entries.map(([key, count]) => `${count} ${t(COUNT_LABEL_KEYS[key] ?? "history.count.records")}`).join(" · ");
}

function kindLabelKey(kind: LegacyArchiveKind): TranslationKey {
  if (kind === "workflow") return "history.kind.workflow";
  if (kind === "canvas_agent_thread") return "history.kind.canvasAgent";
  return "history.kind.userTemplate";
}

function archiveStatusLabel(status: string, t: ReturnType<typeof useI18n>["t"]): string {
  const key = STATUS_LABEL_KEYS[status.toLowerCase()];
  return key ? t(key) : status;
}

function archiveAssetImage(asset: LegacyArchiveAsset): DownloadableImage {
  return {
    previewUrl: api.toApiUrl(asset.preview_url),
    downloadUrl: api.toApiUrl(asset.download_url),
    filename: asset.original_filename,
    alt: asset.display_name,
  };
}

function historyListPath(searchParams: URLSearchParams): string {
  const query = searchParams.toString();
  return query ? `/history?${query}` : "/history";
}

function downloadBlob(blob: Blob, filename: string): void {
  const url = window.URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.URL.revokeObjectURL(url);
}

function errorDetail(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.detail : fallback;
}
