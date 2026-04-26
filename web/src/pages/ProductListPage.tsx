import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowRight,
  Image as ImageIcon,
  Loader2,
  Plus,
  Trash2,
} from "lucide-react";
import { useNavigate } from "react-router-dom";

import { OnboardingGuideCard } from "../components/OnboardingGuide";
import { StatusPill } from "../components/StatusPill";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { formatPrice, formatShortDate } from "../lib/format";
import type { ProductSummary } from "../lib/types";

const PAGE_SIZE = 12;

export function ProductListPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [deleteError, setDeleteError] = useState("");
  const productsQuery = useQuery({
    queryKey: ["products", page, PAGE_SIZE],
    queryFn: () => api.listProducts({ page, page_size: PAGE_SIZE }),
  });
  const products = productsQuery.data?.items ?? [];
  const total = productsQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
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
      await queryClient.invalidateQueries({ queryKey: ["products"] });
      if (products.length === 1 && page > 1) {
        setPage((current) => Math.max(1, current - 1));
      }
    },
    onError: (mutationError) => {
      setDeleteError(mutationError instanceof ApiError ? mutationError.detail : "删除商品失败");
    },
  });

  const handleDeleteProduct = (productId: string, productName: string) => {
    if (!window.confirm(`确定删除「${productName}」吗？此操作不可恢复。`)) {
      return;
    }
    deleteProductMutation.mutate(productId);
  };

  return (
    <div className="flex min-h-screen flex-col bg-slate-50">
      <TopNav
        onHome={() => navigate("/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="mx-auto flex w-full max-w-6xl flex-1 px-6 py-8 lg:py-10">
        <div className="w-full space-y-6">
          <section className="overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm shadow-slate-200/60">
            <div className="grid gap-8 p-6 md:grid-cols-[1.35fr_1fr] lg:p-7">
              <div>
                <div className="mb-3 text-[11px] font-semibold uppercase tracking-[0.2em] text-zinc-400">
                  ProductFlow Workbench
                </div>
                <h1 className="text-2xl font-semibold tracking-tight text-zinc-900">商品创作工作台</h1>
                <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-500">
                  从真实商品开始，整理资料、生成文案与图片。
                </p>
                <div className="mt-5 flex flex-wrap items-center gap-3">
                  <button
                    type="button"
                    onClick={() => navigate("/products/new")}
                    className="inline-flex items-center rounded-xl bg-indigo-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-indigo-600/20 transition-colors hover:bg-indigo-500"
                  >
                    <Plus size={16} className="mr-1.5" /> 新建商品
                  </button>
                </div>
              </div>
              <div className="grid grid-cols-3 gap-3 self-end">
                <MetricCard label="商品总数" value={total} />
                <MetricCard label="当前页文案就绪" value={copyReadyCount} />
                <MetricCard label="当前页图片就绪" value={posterReadyCount} />
              </div>
            </div>
          </section>

          <OnboardingGuideCard page="products" />

          <div className="flex items-end justify-between gap-4 rounded-2xl border border-slate-200 bg-white px-5 py-4 shadow-sm shadow-slate-200/50">
            <div>
              <h2 className="text-base font-semibold text-zinc-900">商品列表</h2>
              <p className="mt-1 text-sm text-zinc-500">
                第 {page} / {totalPages} 页 · 共 {total} 个商品
              </p>
            </div>
            <Pagination page={page} totalPages={totalPages} onPageChange={setPage} disabled={productsQuery.isFetching} />
          </div>

          {deleteError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {deleteError}
            </div>
          ) : null}

          {productsQuery.isLoading ? (
            <div className="flex justify-center rounded-xl border border-zinc-200 bg-white py-20 text-zinc-400">
              <Loader2 size={20} className="animate-spin" />
            </div>
          ) : productsQuery.isError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              商品列表加载失败，请确认后端已启动。
            </div>
          ) : products.length ? (
            <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm shadow-slate-200/50">
              <table className="w-full border-collapse text-left text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50/70">
                    <th className="w-[45%] px-5 py-3 font-medium text-zinc-500">商品素材</th>
                    <th className="px-5 py-3 font-medium text-zinc-500">流程状态</th>
                    <th className="px-5 py-3 font-medium text-zinc-500">最后更新</th>
                    <th className="px-5 py-3 text-right font-medium text-zinc-500">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-100">
                  {products.map((product) => {
                    return (
                      <tr key={product.id} className="group transition-colors hover:bg-indigo-50/30">
                        <td className="px-5 py-4">
                          <div className="flex items-center space-x-3">
                            <ProductThumbnail product={product} />
                            <div className="min-w-0">
                              <button
                                type="button"
                                onClick={() => navigate(`/products/${product.id}`)}
                                className="truncate text-left font-medium text-slate-950 transition-colors hover:text-indigo-700"
                              >
                                {product.name}
                              </button>
                              <div className="mt-0.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-zinc-500">
                                {product.category ? <span>{product.category}</span> : null}
                                {product.price ? <span>{formatPrice(product.price)}</span> : null}
                                {product.source_image_filename ? <span>{product.source_image_filename}</span> : null}
                              </div>
                            </div>
                          </div>
                        </td>
                        <td className="px-5 py-4">
                          <StatusPill status={product.workflow_state} />
                        </td>
                        <td className="px-5 py-4 font-mono text-xs text-zinc-500">
                          {formatShortDate(product.updated_at)}
                        </td>
                        <td className="px-5 py-4 text-right">
                          <div className="flex items-center justify-end gap-3 opacity-0 transition-opacity group-hover:opacity-100">
                            <button
                              type="button"
                              onClick={() => handleDeleteProduct(product.id, product.name)}
                              disabled={deleteProductMutation.isPending}
                              className="inline-flex items-center text-sm font-medium text-red-500 transition-colors hover:text-red-700 disabled:opacity-50"
                            >
                              <Trash2 size={14} className="mr-1" /> 删除
                            </button>
                            <button
                              type="button"
                              onClick={() => navigate(`/products/${product.id}`)}
                              className="inline-flex items-center text-sm font-medium text-zinc-600 transition-colors hover:text-zinc-900"
                            >
                              打开 <ArrowRight size={14} className="ml-1" />
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="rounded-xl border border-dashed border-zinc-300 bg-white px-6 py-16 text-center">
              <ImageIcon className="mx-auto mb-3 text-zinc-300" size={32} />
              <div className="font-medium text-zinc-900">还没有商品</div>
              <p className="mt-1 text-sm text-zinc-500">上传第一张商品图后，就可以进入工作流生成图片。</p>
              <button
                type="button"
                onClick={() => navigate("/products/new")}
                className="mt-5 inline-flex items-center rounded-xl bg-indigo-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-indigo-600/20 hover:bg-indigo-500"
              >
                <Plus size={16} className="mr-1.5" /> 新建商品
              </button>
            </div>
          )}

          {products.length ? (
            <div className="flex justify-end">
              <Pagination page={page} totalPages={totalPages} onPageChange={setPage} disabled={productsQuery.isFetching} />
            </div>
          ) : null}
        </div>
      </main>
    </div>
  );
}

function MetricCard({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">
      <div className="text-2xl font-semibold tracking-tight text-slate-950">{value}</div>
      <div className="mt-1 text-[11px] font-semibold uppercase tracking-wider text-slate-400">{label}</div>
    </div>
  );
}

function ProductThumbnail({ product }: { product: ProductSummary }) {
  const [failed, setFailed] = useState(false);
  const thumbUrl = product.source_image_thumbnail_url ?? product.source_image_preview_url;
  const shouldShowImage = Boolean(thumbUrl) && !failed;

  return (
    <div className="flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-xl border border-slate-200 bg-slate-100 text-slate-400 shadow-sm">
      {shouldShowImage && thumbUrl ? (
        <img
          src={api.toApiUrl(thumbUrl)}
          alt={product.source_image_filename ?? product.name}
          className="h-full w-full object-cover"
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
}: {
  page: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  disabled: boolean;
}) {
  return (
    <div className="inline-flex items-center gap-2 rounded-lg border border-zinc-200 bg-white p-1 shadow-sm">
      <button
        type="button"
        onClick={() => onPageChange(Math.max(1, page - 1))}
        disabled={disabled || page <= 1}
        className="inline-flex items-center rounded-md px-2.5 py-1.5 text-xs font-medium text-zinc-600 hover:bg-zinc-50 disabled:opacity-40"
      >
        <ArrowLeft size={13} className="mr-1" /> 上一页
      </button>
      <span className="px-2 text-xs tabular-nums text-zinc-500">
        {page} / {totalPages}
      </span>
      <button
        type="button"
        onClick={() => onPageChange(Math.min(totalPages, page + 1))}
        disabled={disabled || page >= totalPages}
        className="inline-flex items-center rounded-md px-2.5 py-1.5 text-xs font-medium text-zinc-600 hover:bg-zinc-50 disabled:opacity-40"
      >
        下一页 <ArrowRight size={13} className="ml-1" />
      </button>
    </div>
  );
}
