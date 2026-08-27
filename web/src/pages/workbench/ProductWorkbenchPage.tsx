/**
 * 商品工作台路由：Agent 采集、Agent 画布，或 live 图。
 *
 * live 图是编辑权威。无图 bootstrap 会把查询缓存预置为 null，避免 Agent 面闪 404 重试。
 */

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, RotateCw } from "lucide-react";
import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";

import { api, ApiError } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import { AgentProductWorkbenchPage } from "./agent/AgentProductWorkbenchPage";
import {
  isHttpErrorStatus,
  loadProductWorkbenchAgent,
  readWorkflowGraphOrNull,
  resolveProductWorkbenchSurface,
} from "./agent/productWorkbenchRoute";
import { GraphAgentPanel, GraphWorkbenchPage } from "./GraphWorkbenchPage";

export function ProductWorkbenchPage() {
  const { productId = "" } = useParams();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const agentSessionId = searchParams.get("agent_session_id");
  const agentTaskId = searchParams.get("agent_task_id");
  const agentQuery = useQuery({
    queryKey: ["agent-workbench", productId, agentSessionId, agentTaskId],
    queryFn: () => loadProductWorkbenchAgent(api, productId, agentSessionId, agentTaskId),
    enabled: Boolean(productId),
    retry: (failureCount, error) =>
      !isHttpErrorStatus(error, 404) && !isHttpErrorStatus(error, 409) && failureCount < 2,
  });
  const graphQuery = useQuery({
    queryKey: ["workflow-graph", productId],
    queryFn: () => readWorkflowGraphOrNull(() => api.getCurrentWorkflowGraph(productId)),
    enabled: shouldReadCurrentWorkflowGraph(productId, agentQuery.error),
    retry: (failureCount, error) => !isHttpErrorStatus(error, 404) && failureCount < 2,
  });
  const [missingGraphPrimedProductId, setMissingGraphPrimedProductId] = useState<string | null>(null);
  useEffect(() => {
    const bootstrap = agentQuery.data;
    if (!bootstrap) return;
    const queryKey = ["workflow-graph", productId] as const;
    if (bootstrap.graph) {
      queryClient.setQueryData(queryKey, bootstrap.graph);
      return;
    }
    queryClient.setQueryDefaults(queryKey, { staleTime: Infinity });
    queryClient.setQueryData(queryKey, null);
    setMissingGraphPrimedProductId(productId);
  }, [agentQuery.data, productId, queryClient]);
  const surface = resolveProductWorkbenchSurface({
    graph: graphQuery.data ?? undefined,
    graphPending: graphQuery.isPending && graphQuery.isFetching,
    graphError: graphQuery.error,
    agent: agentQuery.data,
    agentPending: agentQuery.isPending,
    agentError: agentQuery.error,
  });
  const productQuery = useQuery({
    queryKey: ["product", productId],
    queryFn: () => api.getProduct(productId),
    enabled: Boolean(productId) && surface.kind === "graph",
  });
  const missingGraphNeedsPrime = surface.kind === "agent"
    && !surface.bootstrap.graph
    && missingGraphPrimedProductId !== productId;

  if (surface.kind === "loading" || missingGraphNeedsPrime || (surface.kind === "graph" && (productQuery.isLoading || !productQuery.data))) {
    return (
      <WorkbenchRouteState
        error={surface.kind === "graph" ? productQuery.error : null}
        onRetry={() => {
          if (shouldReadCurrentWorkflowGraph(productId, agentQuery.error)) void graphQuery.refetch();
          void agentQuery.refetch();
          if (surface.kind === "graph") void productQuery.refetch();
        }}
      />
    );
  }
  if (surface.kind === "error") {
    return (
      <WorkbenchRouteState
        error={surface.error}
        onRetry={() => {
          if (shouldReadCurrentWorkflowGraph(productId, agentQuery.error)) void graphQuery.refetch();
          void agentQuery.refetch();
        }}
      />
    );
  }
  if (surface.kind === "graph") {
    if (!productQuery.data) {
      return (
        <WorkbenchRouteState
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
            error={agentQuery.error}
            onRetry={() => void agentQuery.refetch()}
            onOpenConversation={() => {
              void api.ensureAgentWorkbench(productId).then(() => agentQuery.refetch());
            }}
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
      preferConversation={Boolean(agentSessionId)}
      onRefetchBootstrap={() => agentQuery.refetch()}
    />
  );
}

/** 图画布不依赖 Agent 工作台是否存在。 */
export function shouldReadCurrentWorkflowGraph(productId: string, _agentError?: unknown): boolean {
  void _agentError;
  return Boolean(productId);
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

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}
