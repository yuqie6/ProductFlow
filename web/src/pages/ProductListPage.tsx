import { useEffect, useMemo, useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, useNavigate, useSearchParams } from "react-router-dom";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { TopNav } from "../components/TopNav";
import { MerchantOverview } from "./product-list/MerchantOverview";
import { getAccountGeneration, ownMerchantId } from "../lib/accountBoundary";
import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/preferences";
import type { ProductListSort, ProductSummary, SessionState } from "../lib/types";
import { ProductListSurface } from "./product-list/ProductListSurface";
import { parseProductListSearchParams, patchProductListSearchParams } from "./product-list/model";

const PAGE_SIZE = 12;
const PRODUCT_LIST_STALE_TIME_MS = 60_000;
const RUNTIME_CONFIG_STALE_TIME_MS = 5 * 60_000;
const SEARCH_COMMIT_DELAY_MS = 300;

export function ProductListPage() {
  const session = useQuery({ queryKey: ["session"], queryFn: api.getSessionState });
  const merchantId = ownMerchantId(session.data);
  if (session.data?.authenticated && !merchantId) return <Navigate to={session.data.user.is_operator ? "/ops" : "/account"} replace />;
  return <ProductDirectory key={`${session.data?.user?.id}:${merchantId}`} merchantId={merchantId} />;
}

function ProductDirectory({ merchantId }: { merchantId: string }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const userId = queryClient.getQueryData<SessionState>(["session"])?.user?.id;
  const currentAccount = (generation: number) => generation === getAccountGeneration() && userId === queryClient.getQueryData<SessionState>(["session"])?.user?.id;
  const [searchParams, setSearchParams] = useSearchParams();
  const queryState = useMemo(() => parseProductListSearchParams(searchParams), [searchParams]);
  const [searchDraft, setSearchDraft] = useState(queryState.q);
  const [deleteError, setDeleteError] = useState("");
  const [pendingDeleteProduct, setPendingDeleteProduct] = useState<ProductSummary | null>(null);

  const productsQuery = useQuery({
    queryKey: ["products", merchantId, queryState.page, PAGE_SIZE, queryState.q, queryState.sort],
    queryFn: () =>
      api.listProducts({
        page: queryState.page,
        page_size: PAGE_SIZE,
        q: queryState.q,
        sort: queryState.sort,
      }),
    enabled: Boolean(merchantId),
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

  const logoutMutation = useMutation<Awaited<ReturnType<typeof api.destroySession>>, Error, number>({
    mutationFn: api.destroySession,
    onSuccess: async (_, generation) => {
      if (!currentAccount(generation)) return;
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      if (!currentAccount(generation)) return;
      navigate("/login", { replace: true });
    },
  });

  const deleteProductMutation = useMutation({
    mutationFn: ({ productId }: { productId: string; generation: number }) => api.deleteProduct(productId),
    onSuccess: async (_, { generation }) => {
      if (!currentAccount(generation)) return;
      setDeleteError("");
      setPendingDeleteProduct(null);
      if (products.length === 1 && queryState.page > 1) {
        setSearchParams(
          (current) => patchProductListSearchParams(current, { page: queryState.page - 1 }),
          { replace: true },
        );
      }
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["products"] }), queryClient.invalidateQueries({ queryKey: ["merchant-overview", merchantId] })]);
    },
    onError: (mutationError, { generation }) => {
      if (!currentAccount(generation)) return;
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
    <div className="flex min-h-screen flex-col bg-surface-base dark:bg-surface-base">
      <TopNav
        onHome={() => navigate("/home")}
        onLogout={() => logoutMutation.mutate(getAccountGeneration())}
      />

      <main className="mx-auto w-full max-w-[1328px] flex-1 px-3 pt-4 pb-[calc(7rem+env(safe-area-inset-bottom))] sm:px-4 sm:pt-6 lg:px-6 lg:pt-[26px] lg:pb-16">
        <ProductListSurface
          overview={<MerchantOverview merchantId={merchantId} />}
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
            deleteProductMutation.mutate({ productId: pendingDeleteProduct.id, generation: getAccountGeneration() });
          }
        }}
      />
    </div>
  );
}
