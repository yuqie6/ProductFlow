import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, RotateCw } from "lucide-react";
import { lazy } from "react";
import { Link, Navigate, useParams, useSearchParams } from "react-router-dom";

import { api, ApiError } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import {
  agentProductIntakeResumePath,
  isAgentWorkbenchMissing,
  resolveProductWorkbenchSurface,
} from "./agent/productWorkbenchRoute";
import { GraphAgentPanel, GraphWorkbenchPage } from "./GraphWorkbenchPage";

const AgentProductWorkbenchPage = lazy(() =>
  import("./agent/AgentProductWorkbenchPage").then((module) => ({
    default: module.AgentProductWorkbenchPage,
  })),
);

export function ProductWorkbenchPage() {
  const { productId = "" } = useParams();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const agentSessionId = searchParams.get("agent_session_id");
  const agentTaskId = searchParams.get("agent_task_id");
  const graphQuery = useQuery({
    queryKey: ["workflow-graph", productId],
    queryFn: () => api.getCurrentWorkflowGraph(productId),
    enabled: Boolean(productId),
    retry: (failureCount, error) => {
      if (error instanceof ApiError && error.status === 404) return false;
      return failureCount < 2;
    },
  });
  const agentQuery = useQuery({
    queryKey: ["agent-workbench", productId, agentSessionId, agentTaskId],
    queryFn: () => api.getAgentWorkbench(productId, agentSessionId, agentTaskId),
    enabled: Boolean(productId),
    retry: (failureCount, error) => {
      if (error instanceof ApiError && error.status === 409) return false;
      return failureCount < 2;
    },
  });
  const missingAgent = !agentQuery.isPending && isAgentWorkbenchMissing(agentQuery.error);
  const shouldEnsureAgent = Boolean(productId) && missingAgent && Boolean(graphQuery.data) && !agentTaskId;
  const ensureQuery = useQuery({
    queryKey: ["agent-workbench", productId, agentSessionId, "ensure"],
    queryFn: async () => {
      const bootstrap = await api.ensureAgentWorkbench(productId, agentSessionId);
      queryClient.setQueryData(
        ["agent-workbench", productId, agentSessionId, agentTaskId],
        bootstrap,
      );
      return bootstrap;
    },
    enabled: shouldEnsureAgent,
    retry: false,
  });
  const surface = resolveProductWorkbenchSurface({
    graph: graphQuery.data,
    graphPending: graphQuery.isPending,
    graphError: graphQuery.error,
    agent: agentQuery.data ?? ensureQuery.data,
    agentPending: agentQuery.isPending || (shouldEnsureAgent && ensureQuery.isPending),
    agentError: ensureQuery.error ?? agentQuery.error,
  });
  const productQuery = useQuery({
    queryKey: ["product", productId],
    queryFn: () => api.getProduct(productId),
    enabled: Boolean(productId) && surface.kind === "graph",
  });

  if (surface.kind === "loading" || (surface.kind === "graph" && (productQuery.isLoading || !productQuery.data))) {
    return (
      <WorkbenchRouteState
        productId={productId}
        error={surface.kind === "graph" ? productQuery.error : null}
        onRetry={() => {
          void graphQuery.refetch();
          void agentQuery.refetch();
          if (surface.kind === "graph") void productQuery.refetch();
        }}
      />
    );
  }
  if (surface.kind === "error") {
    return (
      <WorkbenchRouteState
        productId={productId}
        error={surface.error}
        onRetry={() => {
          void graphQuery.refetch();
          void agentQuery.refetch();
          void ensureQuery.refetch();
        }}
      />
    );
  }
  if (surface.kind === "intake") {
    return (
      <Navigate
        to={agentProductIntakeResumePath(
          surface.bootstrap.conversation.id,
          surface.bootstrap.conversation.session_id,
          agentTaskId,
        )}
        replace
      />
    );
  }
  if (surface.kind === "graph") {
    if (!productQuery.data) {
      return (
        <WorkbenchRouteState
          productId={productId}
          error={productQuery.error}
          onRetry={() => void productQuery.refetch()}
        />
      );
    }
    return (
      <GraphWorkbenchPage
        product={productQuery.data}
        initialGraph={surface.graph}
        agentContent={(
          <GraphAgentPanel
            error={ensureQuery.error}
            onRetry={() => void ensureQuery.refetch()}
          />
        )}
      />
    );
  }
  return (
    <AgentProductWorkbenchPage
      key={surface.bootstrap.product.id}
      bootstrap={surface.bootstrap}
      agentTaskId={agentTaskId}
      onRefetchBootstrap={() => agentQuery.refetch()}
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
