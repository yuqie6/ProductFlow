import { useEffect, useMemo, useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router-dom";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/preferences";
import type { ProductListSort, ProductSummary } from "../lib/types";
import { ProductListSurface } from "./product-list/ProductListSurface";
import { parseProductListSearchParams, patchProductListSearchParams } from "./product-list/model";

const PAGE_SIZE = 12;
const PRODUCT_LIST_STALE_TIME_MS = 60_000;
const RUNTIME_CONFIG_STALE_TIME_MS = 5 * 60_000;
const SEARCH_COMMIT_DELAY_MS = 300;

export function ProductListPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryState = useMemo(() => parseProductListSearchParams(searchParams), [searchParams]);
  const [searchDraft, setSearchDraft] = useState(queryState.q);
  const [deleteError, setDeleteError] = useState("");
  const [pendingDeleteProduct, setPendingDeleteProduct] = useState<ProductSummary | null>(null);

  const productsQuery = useQuery({
    queryKey: ["products", queryState.page, PAGE_SIZE, queryState.q, queryState.sort],
    queryFn: () =>
      api.listProducts({
        page: queryState.page,
        page_size: PAGE_SIZE,
        q: queryState.q,
        sort: queryState.sort,
      }),
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

  useEffect(() => {
    const canonicalParams = patchProductListSearchParams(searchParams, {
      page: queryState.page,
      q: queryState.q,
      sort: queryState.sort,
    });
    if (canonicalParams.toString() !== searchParams.toString()) {
      setSearchParams(canonicalParams, { replace: true });
    }
  }, [queryState.page, queryState.q, queryState.sort, searchParams, setSearchParams]);

  useEffect(() => {
    setSearchDraft(queryState.q);
  }, [queryState.q]);

  useEffect(() => {
    const committedQuery = searchDraft.trim();
    if (committedQuery === queryState.q) {
      return;
    }
    const timer = window.setTimeout(() => {
      setSearchParams(
        (current) => patchProductListSearchParams(current, { q: committedQuery, resetPage: true }),
        { replace: true },
      );
    }, SEARCH_COMMIT_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [queryState.q, searchDraft, setSearchParams]);

  useEffect(() => {
    if (productsQuery.data && !productsQuery.isPlaceholderData && queryState.page > totalPages) {
      setSearchParams(
        (current) => patchProductListSearchParams(current, { page: totalPages }),
        { replace: true },
      );
    }
  }, [productsQuery.data, productsQuery.isPlaceholderData, queryState.page, setSearchParams, totalPages]);

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
      if (products.length === 1 && queryState.page > 1) {
        setSearchParams(
          (current) => patchProductListSearchParams(current, { page: queryState.page - 1 }),
          { replace: true },
        );
      }
      await queryClient.invalidateQueries({ queryKey: ["products"] });
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
    setDeleteError("");
    setPendingDeleteProduct(product);
  };

  const handleSortChange = (sort: ProductListSort) => {
    setSearchParams((current) => patchProductListSearchParams(current, { sort, resetPage: true }));
  };

  const handleClearSearch = () => {
    setSearchDraft("");
    setSearchParams((current) => patchProductListSearchParams(current, { q: "", resetPage: true }));
  };

  return (
    <div className="flex min-h-screen flex-col bg-slate-50 dark:!bg-[#0b0c0e]">
      <TopNav
        onHome={() => navigate("/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="mx-auto w-full max-w-[1328px] flex-1 px-3 pt-4 pb-[calc(7rem+env(safe-area-inset-bottom))] sm:px-4 sm:pt-6 lg:px-6 lg:pt-[26px] lg:pb-16">
        <ProductListSurface
          products={products}
          total={total}
          page={queryState.page}
          pageSize={PAGE_SIZE}
          totalPages={totalPages}
          sort={queryState.sort}
          searchDraft={searchDraft}
          isLoading={productsQuery.isLoading}
          isError={productsQuery.isError}
          isRefreshing={productsQuery.isFetching && !productsQuery.isLoading}
          deletionEnabled={deletionEnabled}
          isDeleting={deleteProductMutation.isPending}
          deleteError={deleteError}
          onSearchDraftChange={setSearchDraft}
          onSortChange={handleSortChange}
          onPageChange={(page) => {
            setSearchParams((current) => patchProductListSearchParams(current, { page }));
          }}
          onClearSearch={handleClearSearch}
          onRetry={() => void productsQuery.refetch()}
          onDelete={handleDeleteProduct}
        />
      </main>

      <ConfirmDialog
        open={Boolean(pendingDeleteProduct)}
        title={t("products.deleteConfirmTitle")}
        description={pendingDeleteProduct ? t("products.deleteConfirm", { name: pendingDeleteProduct.name }) : ""}
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
