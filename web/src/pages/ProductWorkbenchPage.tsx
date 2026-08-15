import { useQuery } from "@tanstack/react-query";
import { Archive, ArrowLeft, FolderClock, Loader2, RotateCw } from "lucide-react";
import { lazy } from "react";
import { useNavigate, useParams } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { formatPrice, formatShortDate } from "../lib/format";
import { useI18n } from "../lib/preferences";
import type { CanonicalProductDetail } from "../lib/types";
import { productWorkbenchRouteTarget } from "./agent-workbench/productWorkbenchRoute";

const AgentProductWorkbenchPage = lazy(() =>
  import("./agent-workbench/AgentProductWorkbenchPage").then((module) => ({
    default: module.AgentProductWorkbenchPage,
  })),
);
const LegacyProductDetailPage = lazy(() =>
  import("./ProductDetailPage").then((module) => ({ default: module.ProductDetailPage })),
);

export function ProductWorkbenchPage() {
  const { productId = "" } = useParams();
  const query = useQuery({
    queryKey: ["agent-workbench", productId],
    queryFn: () => api.getAgentWorkbench(productId),
    enabled: Boolean(productId),
  });

  if (query.isLoading || !query.data) {
    return <WorkbenchRouteState error={query.error} onRetry={() => void query.refetch()} />;
  }

  const target = productWorkbenchRouteTarget(query.data);
  if (target === "agent_v2" && query.data.mode === "agent_v2") {
    return (
      <AgentProductWorkbenchPage
        key={query.data.product.id}
        bootstrap={query.data}
        onRefetchBootstrap={() => query.refetch()}
      />
    );
  }
  if (target === "legacy_v1") {
    return <LegacyProductDetailPage />;
  }
  return <LegacyEmptyProductState product={query.data.product} />;
}

function WorkbenchRouteState({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="flex min-h-screen items-center justify-center bg-white p-6 text-zinc-500 dark:bg-[#060a12] dark:text-slate-400">
      {error ? (
        <div className="flex max-w-md flex-col items-center gap-3 text-center">
          <p role="alert" className="text-sm text-red-700 dark:text-red-200">
            {errorDetail(error, t("productWorkbench.loadFailed"))}
          </p>
          <button
            type="button"
            onClick={onRetry}
            className="inline-flex h-10 items-center gap-2 rounded-md border border-zinc-300 px-3 text-sm font-semibold text-zinc-700 hover:border-zinc-500 dark:border-slate-700 dark:text-slate-200 dark:hover:border-slate-500"
          >
            <RotateCw size={15} />
            {t("productWorkbench.retry")}
          </button>
        </div>
      ) : (
        <>
          <Loader2 size={22} className="animate-spin" />
          <span className="sr-only">{t("productWorkbench.loading")}</span>
        </>
      )}
    </div>
  );
}

function LegacyEmptyProductState({ product }: { product: CanonicalProductDetail }) {
  const navigate = useNavigate();
  const { t } = useI18n();
  const details = [
    product.category,
    formatPrice(product.price),
    t("productWorkbench.legacy.createdAt", { date: formatShortDate(product.created_at, t.locale) }),
  ].filter(Boolean);

  return (
    <div className="flex min-h-screen flex-col bg-white text-zinc-950 dark:bg-[#060a12] dark:text-slate-100">
      <TopNav
        breadcrumbs={`${product.name} / ${t("productWorkbench.legacy.breadcrumb")}`}
        onHome={() => navigate("/products")}
      />
      <main className="flex min-h-0 flex-1 items-center px-5 py-12 sm:px-8 lg:px-12">
        <div className="mx-auto w-full max-w-3xl border-y border-zinc-200 py-10 dark:border-slate-800 sm:py-14">
          <Archive size={28} className="text-zinc-400 dark:text-slate-500" aria-hidden="true" />
          <h1 className="mt-5 text-2xl font-semibold text-zinc-950 dark:text-white">
            {t("productWorkbench.legacy.title")}
          </h1>
          <p className="mt-3 max-w-2xl text-sm leading-6 text-zinc-600 dark:text-slate-300">
            {t("productWorkbench.legacy.description")}
          </p>
          <div className="mt-6 text-lg font-semibold text-zinc-900 dark:text-slate-100">{product.name}</div>
          {details.length ? (
            <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm text-zinc-500 dark:text-slate-400">
              {details.map((detail) => <span key={detail}>{detail}</span>)}
            </div>
          ) : null}
          <div className="mt-8 flex flex-wrap gap-2">
            <button
              type="button"
              onClick={() => navigate("/products")}
              className="inline-flex h-11 items-center gap-2 rounded-md bg-zinc-950 px-4 text-sm font-semibold text-white hover:bg-blue-700 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300"
            >
              <ArrowLeft size={16} />
              {t("productWorkbench.legacy.back")}
            </button>
            <button
              type="button"
              onClick={() => navigate(`/history?${new URLSearchParams({ product_id: product.id })}`)}
              className="inline-flex h-11 items-center gap-2 rounded-md border border-zinc-300 bg-white px-4 text-sm font-semibold text-zinc-700 hover:border-blue-400 hover:text-blue-700 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200 dark:hover:border-cyan-400 dark:hover:text-cyan-200"
            >
              <FolderClock size={16} />
              {t("productWorkbench.legacy.history")}
            </button>
          </div>
        </div>
      </main>
    </div>
  );
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}
