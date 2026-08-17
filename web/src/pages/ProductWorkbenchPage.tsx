import { useQuery } from "@tanstack/react-query";
import { Loader2, RotateCw } from "lucide-react";
import { lazy } from "react";
import { Link, Navigate, useParams, useSearchParams } from "react-router-dom";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/preferences";
import {
  agentProductIntakeResumePath,
  productWorkbenchRouteTarget,
} from "./agent-workbench/productWorkbenchRoute";

const AgentProductWorkbenchPage = lazy(() =>
  import("./agent-workbench/AgentProductWorkbenchPage").then((module) => ({
    default: module.AgentProductWorkbenchPage,
  })),
);

export function ProductWorkbenchPage() {
  const { productId = "" } = useParams();
  const [searchParams] = useSearchParams();
  const agentSessionId = searchParams.get("agent_session_id");
  const agentTaskId = searchParams.get("agent_task_id");
  const query = useQuery({
    queryKey: ["agent-workbench", productId, agentSessionId, agentTaskId],
    queryFn: () => api.getAgentWorkbench(productId, agentSessionId, agentTaskId),
    enabled: Boolean(productId),
    retry: (failureCount, error) => {
      if (error instanceof ApiError && error.status === 409) return false;
      return failureCount < 2;
    },
  });

  if (query.isLoading || !query.data) {
    return <WorkbenchRouteState productId={productId} error={query.error} onRetry={() => void query.refetch()} />;
  }

  const target = productWorkbenchRouteTarget(query.data);
  if (target === "agent_intake") {
    return <Navigate to={agentProductIntakeResumePath(query.data.conversation.id)} replace />;
  }
  return (
    <AgentProductWorkbenchPage
      key={query.data.product.id}
      bootstrap={query.data}
      agentTaskId={agentTaskId}
      onRefetchBootstrap={() => query.refetch()}
    />
  );
}

function WorkbenchRouteState({
  productId,
  error,
  onRetry,
}: {
  productId: string;
  error: unknown;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  const legacyMigrationRequired = error instanceof ApiError && error.status === 409;
  return (
    <div className="flex min-h-screen items-center justify-center bg-white p-6 text-zinc-500 dark:bg-[#060a12] dark:text-slate-400">
      {error ? (
        <div className="flex max-w-md flex-col items-center gap-3 text-center">
          {legacyMigrationRequired ? (
            <>
              <h1 className="text-base font-semibold text-zinc-900 dark:text-white">
                {t("productWorkbench.migration.title")}
              </h1>
              <p role="alert" className="text-sm text-zinc-600 dark:text-slate-300">
                {t("productWorkbench.migration.description")}
              </p>
              <Link
                to={`/history?product_id=${encodeURIComponent(productId)}`}
                className="inline-flex h-10 items-center rounded-md bg-indigo-600 px-4 text-sm font-semibold text-white hover:bg-indigo-700"
              >
                {t("productWorkbench.migration.history")}
              </Link>
            </>
          ) : (
            <>
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
            </>
          )}
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

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}
