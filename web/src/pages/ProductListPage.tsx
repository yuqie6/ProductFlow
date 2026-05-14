import { useEffect, useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowRight,
  Image as ImageIcon,
  Loader2,
  MoreHorizontal,
  Plus,
  Trash2,
} from "lucide-react";
import { useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { StatusPill } from "../components/StatusPill";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { formatPrice, formatShortDate } from "../lib/format";
import { useI18n } from "../lib/preferences";
import type { ProductSummary } from "../lib/types";

const PAGE_SIZE = 12;
const PRODUCT_LIST_STALE_TIME_MS = 60_000;
const RUNTIME_CONFIG_STALE_TIME_MS = 5 * 60_000;

export function ProductListPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [deleteError, setDeleteError] = useState("");
  const [pendingDeleteProduct, setPendingDeleteProduct] = useState<ProductSummary | null>(null);
  const productsQuery = useQuery({
    queryKey: ["products", page, PAGE_SIZE],
    queryFn: () => api.listProducts({ page, page_size: PAGE_SIZE }),
    placeholderData: keepPreviousData,
    staleTime: PRODUCT_LIST_STALE_TIME_MS,
  });
  const runtimeConfigQuery = useQuery({
    queryKey: ["runtime-config"],
    queryFn: api.getRuntimeConfig,
    staleTime: RUNTIME_CONFIG_STALE_TIME_MS,
  });
  const products = productsQuery.data?.items ?? [];
  const total = productsQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const deletionEnabled = runtimeConfigQuery.data?.deletion_enabled ?? false;
  const posterReadyCount = products.filter((product) => product.workflow_state === "poster_ready").length;
  const copyReadyCount = products.filter(
    (product) => product.workflow_state === "copy_ready" || product.workflow_state === "poster_ready",
  ).length;

  useEffect(() => {
    if (productsQuery.data && page > totalPages) {
      setPage(totalPages);
    }
  }, [page, productsQuery.data, totalPages]);

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  const deleteProductMutation = useMutation({
    mutationFn: (productId: string) => api.deleteProduct(productId),
    onSuccess: async () => {
      setDeleteError("");
      setPendingDeleteProduct(null);
      await queryClient.invalidateQueries({ queryKey: ["products"] });
      if (products.length === 1 && page > 1) {
        setPage((current) => Math.max(1, current - 1));
      }
    },
    onError: (mutationError) => {
      setPendingDeleteProduct(null);
      setDeleteError(mutationError instanceof ApiError ? mutationError.detail : t("products.deleteFailed"));
    },
  });

  const handleDeleteProduct = (product: ProductSummary) => {
    if (!deletionEnabled) {
      setDeleteError(t("products.deleteDisabled"));
      return;
    }
    setPendingDeleteProduct(product);
  };

  return (
    <div className="flex min-h-screen flex-col bg-slate-50 dark:bg-[#060a12]">
      <TopNav
        onHome={() => navigate("/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="mx-auto flex w-full max-w-6xl flex-1 px-4 pt-4 pb-40 sm:px-6 lg:py-10">
        <div className="w-full space-y-4 lg:space-y-6">
          <section className="rounded-2xl border border-slate-200 bg-white px-4 py-4 shadow-sm shadow-slate-200/50 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_16px_44px_rgba(0,0,0,0.24)] lg:hidden">
            <div className="flex items-center justify-between gap-3">
              <div className="min-w-0">
                <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-zinc-400 dark:text-slate-500">
                  {t("products.heroEyebrow")}
                </div>
                <h1 className="mt-1 truncate text-xl font-semibold tracking-tight text-zinc-900 dark:text-white">
                  {t("products.listTitle")}
                </h1>
                <p className="mt-1 text-xs text-zinc-500 dark:text-slate-400">
                  {t("products.paginationSummary", { page, totalPages, total })}
                </p>
              </div>
              <button
                type="button"
                onClick={() => navigate("/products/new")}
                aria-label={t("products.new")}
                className="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-sm shadow-indigo-600/20 transition-colors active:scale-[0.98] hover:bg-indigo-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 dark:bg-gradient-to-r dark:from-indigo-500 dark:to-violet-500 dark:shadow-violet-900/35 dark:ring-1 dark:ring-violet-300/35 dark:focus-visible:ring-violet-400 dark:focus-visible:ring-offset-slate-950"
              >
                <Plus size={18} aria-hidden="true" />
              </button>
            </div>
          </section>

          <section className="hidden overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm shadow-slate-200/60 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_20px_70px_rgba(0,0,0,0.28)] lg:block">
            <div className="grid gap-8 p-6 md:grid-cols-[1.35fr_1fr] lg:p-7">
              <div>
                <div className="mb-3 text-[11px] font-semibold uppercase tracking-[0.2em] text-zinc-400 dark:text-slate-500">
                  {t("products.heroEyebrow")}
                </div>
                <h1 className="text-2xl font-semibold tracking-tight text-zinc-900 dark:text-white">{t("products.title")}</h1>
                <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-500 dark:text-slate-400">
                  {t("products.description")}
                </p>
                <div className="mt-5 flex flex-wrap items-center gap-3">
                  <button
                    type="button"
                    onClick={() => navigate("/products/new")}
                    className="inline-flex items-center rounded-xl bg-indigo-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-indigo-600/20 transition-colors hover:bg-indigo-500 dark:bg-gradient-to-r dark:from-indigo-500 dark:to-violet-500 dark:shadow-violet-900/35 dark:ring-1 dark:ring-violet-300/35"
                  >
                    <Plus size={16} className="mr-1.5" /> {t("products.new")}
                  </button>
                </div>
              </div>
              <div className="grid grid-cols-3 gap-3 self-end">
                <MetricCard label={t("products.totalMetric")} value={total} />
                <MetricCard label={t("products.copyReadyMetric")} value={copyReadyCount} />
                <MetricCard label={t("products.posterReadyMetric")} value={posterReadyCount} />
              </div>
            </div>
          </section>

          <div className="hidden items-end justify-between gap-4 rounded-2xl border border-slate-200 bg-white px-5 py-4 shadow-sm shadow-slate-200/50 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_16px_48px_rgba(0,0,0,0.22)] lg:flex">
            <div>
              <h2 className="text-base font-semibold text-zinc-900 dark:text-white">{t("products.listTitle")}</h2>
              <p className="mt-1 text-sm text-zinc-500 dark:text-slate-400">
                {t("products.paginationSummary", { page, totalPages, total })}
              </p>
            </div>
            <Pagination page={page} totalPages={totalPages} onPageChange={setPage} disabled={productsQuery.isFetching} />
          </div>

          {deleteError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
              {deleteError}
            </div>
          ) : null}

          {productsQuery.isLoading ? (
            <div className="flex justify-center rounded-xl border border-zinc-200 bg-white py-20 text-zinc-400 dark:border-slate-700/80 dark:bg-[#111b2d] dark:text-slate-500">
              <Loader2 size={20} className="animate-spin" />
            </div>
          ) : productsQuery.isError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
              {t("products.loadFailed")}
            </div>
          ) : products.length ? (
            <>
              <div className="space-y-3 lg:hidden">
                {products.map((product) => (
                  <ProductMobileCard
                    key={product.id}
                    product={product}
                    deletionEnabled={deletionEnabled}
                    isDeleting={deleteProductMutation.isPending}
                    onOpen={() => navigate(`/products/${product.id}`)}
                    onDelete={() => handleDeleteProduct(product)}
                  />
                ))}
              </div>

              <div className="hidden overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm shadow-slate-200/50 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_18px_60px_rgba(0,0,0,0.24)] lg:block">
                <table className="w-full table-fixed border-collapse text-left text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50/70 dark:border-slate-700/80 dark:bg-[#151f33]">
                      <th className="w-[45%] px-5 py-3 font-medium text-zinc-500 dark:text-slate-300">{t("products.table.product")}</th>
                      <th className="w-[18%] px-5 py-3 font-medium text-zinc-500 dark:text-slate-300">{t("products.table.state")}</th>
                      <th className="w-[18%] px-5 py-3 font-medium text-zinc-500 dark:text-slate-300">{t("products.table.updated")}</th>
                      <th className="w-[19%] px-5 py-3 text-right font-medium text-zinc-500 dark:text-slate-300">{t("products.table.actions")}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-100 dark:divide-slate-800">
                    {products.map((product) => {
                      return (
                        <tr key={product.id} className="group transition-colors hover:bg-indigo-50/30 dark:hover:bg-violet-500/10">
                          <td className="px-5 py-4">
                            <div className="flex min-w-0 items-center gap-3">
                              <ProductThumbnail product={product} />
                              <div className="min-w-0 flex-1">
                                <button
                                  type="button"
                                  onClick={() => navigate(`/products/${product.id}`)}
                                  className="block max-w-full truncate text-left font-medium text-slate-950 transition-colors hover:text-indigo-700 dark:text-slate-100 dark:hover:text-violet-200"
                                  title={product.name}
                                >
                                  {product.name}
                                </button>
                                <div className="mt-0.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-zinc-500 dark:text-slate-400">
                                  {product.category ? (
                                    <span className="min-w-0 max-w-full truncate">{product.category}</span>
                                  ) : null}
                                  {product.price ? <span className="shrink-0">{formatPrice(product.price)}</span> : null}
                                  {product.source_image_filename ? (
                                    <span className="min-w-0 max-w-full truncate" title={product.source_image_filename}>
                                      {product.source_image_filename}
                                    </span>
                                  ) : null}
                                </div>
                              </div>
                            </div>
                          </td>
                          <td className="px-5 py-4">
                            <StatusPill status={product.workflow_state} />
                          </td>
                          <td className="px-5 py-4 font-mono text-xs text-zinc-500 dark:text-slate-400">
                            {formatShortDate(product.updated_at)}
                          </td>
                          <td className="px-5 py-4 text-right">
                            <div className="flex items-center justify-end gap-3 opacity-0 transition-opacity group-hover:opacity-100">
                              <button
                                type="button"
                                onClick={() => handleDeleteProduct(product)}
                                disabled={deleteProductMutation.isPending || !deletionEnabled}
                                title={deletionEnabled ? t("products.delete") : t("products.deleteDisabled")}
                                className="inline-flex items-center text-sm font-medium text-red-500 transition-colors hover:text-red-700 disabled:opacity-50"
                              >
                                <Trash2 size={14} className="mr-1" /> {t("products.delete")}
                              </button>
                              <button
                                type="button"
                                onClick={() => navigate(`/products/${product.id}`)}
                                className="inline-flex items-center text-sm font-medium text-zinc-600 transition-colors hover:text-zinc-900 dark:text-slate-300 dark:hover:text-white"
                              >
                                {t("products.open")} <ArrowRight size={14} className="ml-1" />
                              </button>
                            </div>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </>
          ) : (
            <div className="rounded-xl border border-dashed border-zinc-300 bg-white px-6 py-16 text-center dark:border-slate-700/80 dark:bg-[#0f1726]">
              <ImageIcon className="mx-auto mb-3 text-zinc-300 dark:text-slate-500" size={32} />
              <div className="font-medium text-zinc-900 dark:text-white">{t("products.emptyTitle")}</div>
              <p className="mt-1 text-sm text-zinc-500 dark:text-slate-400">{t("products.emptyDescription")}</p>
              <button
                type="button"
                onClick={() => navigate("/products/new")}
                className="mt-5 inline-flex items-center rounded-xl bg-indigo-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-indigo-600/20 hover:bg-indigo-500 dark:bg-gradient-to-r dark:from-indigo-500 dark:to-violet-500 dark:shadow-violet-900/35"
              >
                <Plus size={16} className="mr-1.5" /> {t("products.new")}
              </button>
            </div>
          )}

          {products.length ? (
            <div className="hidden justify-end lg:flex">
              <Pagination page={page} totalPages={totalPages} onPageChange={setPage} disabled={productsQuery.isFetching} />
            </div>
          ) : null}
        </div>
      </main>
      {products.length ? (
        <div className="fixed inset-x-0 bottom-[calc(env(safe-area-inset-bottom)+4.75rem)] z-40 flex justify-center px-4 lg:hidden">
          <Pagination page={page} totalPages={totalPages} onPageChange={setPage} disabled={productsQuery.isFetching} floating />
        </div>
      ) : null}
      <ConfirmDialog
        open={Boolean(pendingDeleteProduct)}
        title={t("products.deleteConfirmTitle")}
        description={
          pendingDeleteProduct ? t("products.deleteConfirm", { name: pendingDeleteProduct.name }) : ""
        }
        confirmLabel={t("confirm.delete.confirm")}
        cancelLabel={t("common.cancel")}
        busy={deleteProductMutation.isPending}
        onClose={() => setPendingDeleteProduct(null)}
        onConfirm={() => {
          if (pendingDeleteProduct) {
            deleteProductMutation.mutate(pendingDeleteProduct.id);
          }
        }}
      />
    </div>
  );
}

function ProductMobileCard({
  product,
  deletionEnabled,
  isDeleting,
  onOpen,
  onDelete,
}: {
  product: ProductSummary;
  deletionEnabled: boolean;
  isDeleting: boolean;
  onOpen: () => void;
  onDelete: () => void;
}) {
  const { t } = useI18n();
  const metadata = [
    product.category,
    product.price ? formatPrice(product.price) : null,
    product.source_image_filename,
  ].filter(Boolean);
  const metadataText = metadata.join(" / ");

  return (
    <article className="rounded-2xl border border-slate-200 bg-white p-3 shadow-sm shadow-slate-200/50 [contain-intrinsic-size:144px] [content-visibility:auto] dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_14px_38px_rgba(0,0,0,0.22)]">
      <div className="flex min-w-0 gap-3">
        <ProductThumbnail product={product} compact />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-start justify-between gap-2">
            <button
              type="button"
              onClick={onOpen}
              aria-label={t("products.openProduct", { name: product.name })}
              className="min-h-11 min-w-0 flex-1 rounded-lg pr-1 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-violet-400"
            >
              <span className="block truncate text-sm font-semibold text-slate-950 dark:text-slate-100" title={product.name}>
                {product.name}
              </span>
              <span className="mt-1 flex items-center gap-1.5 text-xs text-zinc-500 dark:text-slate-400">
                <span>{t("products.table.updated")}</span>
                <span className="font-mono tabular-nums">{formatShortDate(product.updated_at)}</span>
              </span>
            </button>
            <StatusPill status={product.workflow_state} />
          </div>

          {metadata.length ? (
            <div className="mt-1.5 flex items-center gap-1.5 text-xs text-zinc-500 dark:text-slate-400">
              <MoreHorizontal size={14} className="shrink-0 text-zinc-300 dark:text-slate-600" aria-hidden="true" />
              <span className="min-w-0 truncate" title={metadataText}>
                {metadataText}
              </span>
            </div>
          ) : null}
        </div>
      </div>

      <div className="mt-3 grid grid-cols-[minmax(0,1fr)_auto] gap-2">
        <button
          type="button"
          onClick={onOpen}
          className="inline-flex min-h-11 min-w-0 items-center justify-center rounded-xl bg-slate-950 px-3 text-sm font-semibold text-white transition-colors active:scale-[0.98] hover:bg-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 dark:bg-slate-100 dark:text-slate-950 dark:hover:bg-white dark:focus-visible:ring-violet-400 dark:focus-visible:ring-offset-slate-950"
        >
          <span className="truncate">{t("products.open")}</span>
          <ArrowRight size={15} className="ml-1.5 shrink-0" aria-hidden="true" />
        </button>
        <button
          type="button"
          onClick={onDelete}
          disabled={isDeleting || !deletionEnabled}
          aria-label={
            deletionEnabled
              ? t("products.deleteProduct", { name: product.name })
              : t("products.deleteDisabled")
          }
          title={deletionEnabled ? t("products.delete") : t("products.deleteDisabled")}
          className="inline-flex min-h-11 min-w-[5.75rem] items-center justify-center rounded-xl border border-red-200 bg-red-50 px-3 text-sm font-semibold text-red-600 transition-colors active:scale-[0.98] hover:border-red-300 hover:bg-red-100 disabled:opacity-45 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200 dark:hover:border-red-300/50 dark:hover:bg-red-500/18"
        >
          <Trash2 size={16} className="mr-1.5 shrink-0" aria-hidden="true" />
          <span className="whitespace-nowrap">{t("products.delete")}</span>
        </button>
      </div>
    </article>
  );
}

function MetricCard({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 dark:border-slate-700/80 dark:bg-[#151f33]">
      <div className="text-2xl font-semibold tracking-tight text-slate-950 dark:text-white">{value}</div>
      <div className="mt-1 text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">{label}</div>
    </div>
  );
}

function ProductThumbnail({ product, compact = false }: { product: ProductSummary; compact?: boolean }) {
  const [failed, setFailed] = useState(false);
  const thumbUrl = product.source_image_thumbnail_url ?? product.source_image_preview_url;
  const shouldShowImage = Boolean(thumbUrl) && !failed;

  return (
    <div
      className={`flex shrink-0 items-center justify-center overflow-hidden rounded-xl border border-slate-200 bg-slate-100 text-slate-400 shadow-sm dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-500 ${
        compact ? "h-20 w-20" : "h-16 w-16"
      }`}
    >
      {shouldShowImage && thumbUrl ? (
        <img
          src={api.toApiUrl(thumbUrl)}
          alt={product.source_image_filename ?? product.name}
          className="h-full w-full object-cover"
          decoding="async"
          loading="lazy"
          onError={() => setFailed(true)}
        />
      ) : (
        <ImageIcon size={18} strokeWidth={1.5} />
      )}
    </div>
  );
}

function Pagination({
  page,
  totalPages,
  onPageChange,
  disabled,
  floating = false,
}: {
  page: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  disabled: boolean;
  floating?: boolean;
}) {
  const { t } = useI18n();
  return (
    <div
      className={`inline-flex items-center gap-2 border p-1 shadow-sm ${
        floating
          ? "rounded-2xl border-slate-200/85 bg-white/95 shadow-[0_16px_44px_rgba(15,23,42,0.18)] backdrop-blur dark:border-slate-700/90 dark:bg-slate-950/94 dark:shadow-[0_18px_46px_rgba(0,0,0,0.42)]"
          : "rounded-lg border-zinc-200 bg-white dark:border-slate-700/80 dark:bg-[#151f33] dark:shadow-black/20"
      }`}
    >
      <button
        type="button"
        onClick={() => onPageChange(Math.max(1, page - 1))}
        disabled={disabled || page <= 1}
        className="inline-flex min-h-11 items-center rounded-md px-3 py-2 text-xs font-medium text-zinc-600 hover:bg-zinc-50 disabled:opacity-40 dark:text-slate-300 dark:hover:bg-violet-500/15 dark:hover:text-white lg:min-h-0 lg:px-2.5 lg:py-1.5"
      >
        <ArrowLeft size={13} className="mr-1" /> {t("pagination.previous")}
      </button>
      <span className="px-2 text-xs tabular-nums text-zinc-500 dark:text-slate-400">
        {page} / {totalPages}
      </span>
      <button
        type="button"
        onClick={() => onPageChange(Math.min(totalPages, page + 1))}
        disabled={disabled || page >= totalPages}
        className="inline-flex min-h-11 items-center rounded-md px-3 py-2 text-xs font-medium text-zinc-600 hover:bg-zinc-50 disabled:opacity-40 dark:text-slate-300 dark:hover:bg-violet-500/15 dark:hover:text-white lg:min-h-0 lg:px-2.5 lg:py-1.5"
      >
        {t("pagination.next")} <ArrowRight size={13} className="ml-1" />
      </button>
    </div>
  );
}
