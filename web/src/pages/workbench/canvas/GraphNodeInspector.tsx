import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Images,
  Link2,
  Loader2,
  Plus,
  PencilLine,
  Play,
  RotateCcw,
  Save,
  Search,
  Trash2,
  Undo2,
  XCircle,
} from "lucide-react";
import { useCallback, useContext, useEffect, useId, useMemo, useRef, useState, type ReactNode, createContext } from "react";

import { api, ApiError } from "../../../lib/api";
import { formatDateTime } from "../../../lib/format";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { sanitizeFilenamePart } from "../../../lib/image-downloads";
import type { TranslationKey } from "../../../lib/i18n";
import { useI18n } from "../../../lib/preferences";
import type {
  CanonicalProductDetail,
  GraphConfigStatus,
  GraphEdgeRole,
  GraphNodeCatalog,
  GraphProductFactSet,
  GraphNode,
  GraphNodeRun,
  GraphProjection,
  ProductFactsResponse,
  WorkflowDeliverySpec,
  WorkflowNodeStatus,
} from "../../../lib/types";
import { parseAspectRatio } from "../../../components/ImageRatioFrame";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "../chrome/constants";
import { parseWorkflowGenerationSpec } from "./generationSpec";
import { DownloadLink } from "../chrome/ImageDownloadComponents";
import { SaveStatusBadge, type SaveStatus } from "../chrome/SaveStatusBadge";
import { TextArea } from "../chrome/TextArea";
import { statusClass } from "../chrome/utils";
import { workflowNodeKindTheme } from "../chrome/WorkflowNodeCard";
import { CatalogConfigFields } from "./CatalogConfigFields";
import {
  catalogConfigForSave,
  catalogNodeDraft,
  normalizeCatalogDraft,
  validateCatalogDraft,
  type CatalogNodeDraft,
} from "./catalogConfig";
import { DeliveryRenditionPanel } from "./DeliveryRenditionPanel";
import { replaceDeliverySpec } from "./deliveryRenditions";
import { graphEdgeRoleLabelKey, graphNodeConfigFields } from "./graphCatalog";
import { graphNodeTitleKey } from "./graphLayout";
import { graphContextEntries, graphIncomingSourceEntries, graphNodeRunPresentations, graphRunInputTraceEntries, graphRunsAreLive } from "./graphRunDisplay";
import {
  graphProductSourceConfig,
  graphProductSourceDraft,
  normalizeProductFactsDraft,
  productFactsDraft,
  productFactsPayload,
  validateProductFactsDraft,
  graphTitleDraft,
  validateGraphTitle,
  type GraphProductSourceDraft,
  type ProductFactsDraft,
  type ProductFactRowDraft,
  type GraphTitleDraft,
} from "./graphNodeEditorDrafts";
import { useNodeDraftAutosave, type NodeDraftAutosave } from "./useNodeDraftAutosave";
import type { LocalImageEditOpenRequest } from "../local-edit/LocalImageEditController";
import { ImageFidelityCheckController } from "../fidelity/ImageFidelityCheckController";

const ACTIVE_RUN_STATUSES = new Set(["queued", "running"]);
type InspectorFlush = () => Promise<unknown>;
type RegisterInspectorFlush = (id: string, flush: InspectorFlush) => () => void;
const InspectorFlushContext = createContext<RegisterInspectorFlush>(() => () => undefined);

export function GraphNodeInspector({
  graph,
  node,
  busy,
  catalog: catalogProp,
  catalogError,
  onRetryCatalog,
  onCommit,
  onBind,
  onJump,
  onPreviewImage,
  onOpenLocalEdit,
  onRegisterFlush,
  onOpenAdd,
  onOpenLibrary,
}: {
  graph: GraphProjection;
  node: GraphNode | null;
  product?: CanonicalProductDetail | null;
  busy: boolean;
  catalog?: GraphNodeCatalog | null;
  catalogError?: string | null;
  onRetryCatalog?: () => void;
  onCommit: (input: {
    title: string;
    config: Record<string, unknown>;
    boundAssetId: string | null;
  }) => Promise<GraphProjection | void> | GraphProjection | void;
  onBind?: () => void;
  onJump?: (nodeId: string) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
  onRegisterFlush?: (flush: () => Promise<void>) => void;
  onOpenAdd?: () => void;
  onOpenLibrary?: () => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const catalogQuery = useQuery({
    queryKey: ["graph-node-catalog"],
    queryFn: () => api.getGraphNodeCatalog(),
    staleTime: 60_000,
    enabled: catalogProp === undefined,
  });
  const catalog = catalogProp ?? catalogQuery.data ?? null;
  const catalogLoadError = catalogError ?? (
    catalogQuery.error ? errorMessage(catalogQuery.error, t("graph.inspector.catalogLoadFailed")) : null
  );
  const retryCatalog = useCallback(() => {
    if (onRetryCatalog) {
      onRetryCatalog();
      return;
    }
    void catalogQuery.refetch();
  }, [catalogQuery.refetch, onRetryCatalog]);
  const [saveState, setSaveState] = useState<{ status: SaveStatus; error: string | null }>({
    status: "idle",
    error: null,
  });
  const runsQueryKey = ["graph-runs", graph.product_id, graph.id] as const;
  const runsQuery = useQuery({
    queryKey: runsQueryKey,
    queryFn: () => api.listGraphRuns(graph.product_id, graph.id),
    refetchInterval: (query) => graphRunsAreLive(query.state.data?.items) ? 1200 : false,
  });
  const presentations = useMemo(
    () => graphNodeRunPresentations(runsQuery.data?.items ?? []),
    [runsQuery.data],
  );
  const graphRunIsBusy = graphRunsAreLive(runsQuery.data?.items);
  const presentation = node ? presentations[node.id] : undefined;
  const activeRun = node
    ? runsQuery.data?.items.find((run) => run.status === "running" && run.node_runs.some((item) => (
      item.node_id === node.id && ACTIVE_RUN_STATUSES.has(item.status)
    ))) ?? null
    : null;
  const nodeStatus: WorkflowNodeStatus = presentation?.status ?? "idle";
  const runMutation = useMutation({
    mutationFn: (input: { scope: "graph" | "node" | "to_node"; node_id?: string }) =>
      api.submitGraphRun(graph.product_id, graph.id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: runsQueryKey });
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", graph.product_id] });
    },
  });
  const cancelMutation = useMutation({
    mutationFn: (runId: string) => api.cancelGraphRun(graph.product_id, graph.id, runId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: runsQueryKey }),
  });
  const retryMutation = useMutation({
    mutationFn: (runId: string) => api.retryGraphRun(graph.product_id, graph.id, runId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: runsQueryKey });
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", graph.product_id] });
    },
  });

  useEffect(() => {
    setSaveState({ status: "idle", error: null });
  }, [node?.id]);

  const persist = useCallback(async (input: {
    title: string;
    config: Record<string, unknown>;
    boundAssetId: string | null;
  }) => {
    const next = await onCommit(input);
    return { edit_version: next?.revision ?? graph.revision };
  }, [graph.revision, onCommit]);

  const flushesRef = useRef(new Map<string, InspectorFlush>());
  const registerFlush = useCallback<RegisterInspectorFlush>((id, flush) => {
    flushesRef.current.set(id, flush);
    return () => {
      flushesRef.current.delete(id);
    };
  }, []);
  const flushInspector = useCallback(async () => {
    for (const flush of [...flushesRef.current.values()]) {
      await flush();
    }
  }, []);
  useEffect(() => {
    onRegisterFlush?.(flushInspector);
  }, [flushInspector, onRegisterFlush]);

  const retryRun = useCallback(async () => {
    const runId = presentation?.runId;
    if (!runId) return;
    try {
      await flushInspector();
      retryMutation.mutate(runId);
    } catch {
      // 编辑器继续展示校验或保存错误
    }
  }, [flushInspector, presentation?.runId, retryMutation]);

  if (!node) {
    return (
      <GraphInspectorDashboard
        graph={graph}
        busy={busy || runMutation.isPending || graphRunIsBusy}
        onRunGraph={() => {
          void flushInspector()
            .then(() => runMutation.mutate({ scope: "graph" }))
            .catch(() => undefined);
        }}
        onOpenAdd={onOpenAdd}
        onOpenLibrary={onOpenLibrary}
      />
    );
  }

  const theme = workflowNodeKindTheme(node.node_type);
  const Icon = theme.icon;
  const image = nodePreview(node);
  const missingPrompt = node.node_type === "image_generation"
    && !node.incoming.some((edge) => edge.role === "prompt");
  const missingReference = node.node_type === "image_generation"
    && !node.incoming.some((edge) => edge.role === "reference");
  const runBlocked = missingPrompt || missingReference;
  const canRun = node.node_type === "creative_brief"
    || node.node_type === "visual_system"
    || node.node_type === "prompt_generation"
    || node.node_type === "image_generation";
  const mutationError = runMutation.error ?? cancelMutation.error ?? retryMutation.error;
  const incoming = node.incoming.map((edge) => ({
    edge,
    related: graph.nodes.find((item) => item.id === edge.node_id) ?? null,
  }));
  const outgoing = node.outgoing.map((edge) => ({
    edge,
    related: graph.nodes.find((item) => item.id === edge.node_id) ?? null,
  }));
  const lastNodeRun = runsQuery.data?.items
    .flatMap((run) => run.node_runs)
    .find((item) => item.node_id === node.id && item.compiled_context) ?? null;

  return (
    <InspectorFlushContext.Provider value={registerFlush}>
      <div className="space-y-3 pb-4" data-graph-node-inspector>
        <section className="config-bubble rounded-2xl p-4 shadow-sm">
          <div className="flex items-start gap-3">
            <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border shadow-sm ${theme.iconBox}`}>
              <Icon size={16} />
            </span>
            <div className="min-w-0 flex-1">
              <h3 className="truncate text-base font-semibold text-zinc-950 dark:text-white">{node.title}</h3>
              <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${theme.badge}`}>
                  {t(graphNodeTitleKey(node.node_type))}
                </span>
                <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(nodeStatus)}`}>
                  {ACTIVE_RUN_STATUSES.has(nodeStatus) ? <Loader2 size={10} className="mr-1 animate-spin" /> : null}
                  {t(`detail.nodeStatus.${nodeStatus}`)}
                </span>
                <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${node.config_status === "ready"
                  ? "border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300"
                  : "border-slate-200 bg-slate-100 text-slate-700 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200"
                  }`}>
                  {t(configStatusKey(node.config_status))}
                </span>
                <SaveStatusBadge status={saveState.status} />
              </div>
            </div>
          </div>

          {node.unused && node.bound_asset_id ? (
            <div className="mt-3 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
              {t("graph.inspector.unused")}
            </div>
          ) : null}
          {node.node_type === "image_asset" && !node.bound_asset_id ? (
            <div className="mt-3 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
              {t("graph.inspector.unbound")}
            </div>
          ) : null}
          {missingPrompt ? (
            <div className="mt-3 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
              {t("graph.inspector.missingPrompt")}
            </div>
          ) : null}
          {missingReference ? (
            <div className="mt-3 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
              {t("graph.inspector.missingReference")}
            </div>
          ) : null}
          {presentation?.failureReason && !activeRun ? (
            <div className="mt-3 rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
              <div className="font-semibold">{t("graph.inspector.lastFailed")}</div>
              <p className="mt-1">{presentation.failureReason}</p>
              {presentation.lastRunAt ? (
                <p className="mt-1 text-[10px] text-red-600/80 dark:text-red-300/80">
                  {t("graph.inspector.lastRun", { time: formatDateTime(presentation.lastRunAt, t.locale) })}
                </p>
              ) : null}
            </div>
          ) : null}
          {activeRun ? (
            <div className="mt-3 flex items-start gap-2 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-xs text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200">
              <Loader2 size={14} className="mt-0.5 shrink-0 animate-spin" />
              <div className="font-semibold">{t(`detail.nodeStatus.${nodeStatus}`)}</div>
            </div>
          ) : null}

          {canRun || activeRun || presentation?.retryable ? (
            <div className="mt-4 space-y-2">
              {canRun ? (
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    data-graph-inspector-run-node
                    onClick={() => {
                      void flushInspector()
                        .then(() => runMutation.mutate({ scope: "node", node_id: node.id }))
                        .catch(() => undefined);
                    }}
                    disabled={Boolean(activeRun) || graphRunIsBusy || runMutation.isPending || busy || runBlocked}
                    className="inline-flex h-10 items-center justify-center rounded-xl bg-slate-900 px-3 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
                  >
                    {runMutation.isPending && runMutation.variables?.scope === "node" ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Play size={14} className="mr-1.5" />}
                    {t("graph.runs.scope.node")}
                  </button>
                  <button
                    type="button"
                    data-graph-inspector-run-to-node
                    onClick={() => {
                      void flushInspector()
                        .then(() => runMutation.mutate({ scope: "to_node", node_id: node.id }))
                        .catch(() => undefined);
                    }}
                    disabled={Boolean(activeRun) || graphRunIsBusy || runMutation.isPending || busy || runBlocked}
                    className="inline-flex h-10 items-center justify-center rounded-xl border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                  >
                    {runMutation.isPending && runMutation.variables?.scope === "to_node" ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Play size={14} className="mr-1.5" />}
                    {t("graph.runs.scope.toNode")}
                  </button>
                </div>
              ) : null}
              {activeRun ? (
                <button
                  type="button"
                  onClick={() => cancelMutation.mutate(activeRun.id)}
                  disabled={cancelMutation.isPending}
                  className="inline-flex h-10 w-full items-center justify-center rounded-xl border border-red-200 bg-white px-3 text-xs font-semibold text-red-700 hover:bg-red-50 disabled:opacity-50 dark:border-red-900/50 dark:bg-transparent dark:text-red-300 dark:hover:bg-red-950/30"
                >
                  {cancelMutation.isPending ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <XCircle size={14} className="mr-1.5" />}
                  {t("detail.cancel")}
                </button>
              ) : null}
              {presentation?.retryable && presentation.runId && !activeRun ? (
                <button
                  type="button"
                  onClick={() => void retryRun()}
                  disabled={retryMutation.isPending || graphRunIsBusy || busy}
                  className="inline-flex h-10 w-full items-center justify-center rounded-xl border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                >
                  {retryMutation.isPending ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <RotateCcw size={14} className="mr-1.5" />}
                  {t("graph.inspector.retryRun")}
                </button>
              ) : null}
            </div>
          ) : null}
        </section>

        {saveState.error || mutationError ? (
          <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
            <AlertCircle size={13} className="mr-1.5 inline" />
            {saveState.error ?? t("workbench.error.structure")}
          </div>
        ) : null}

        {node.node_type === "prompt_generation" ? (
          <PromptResult payload={node.current_artifact_payload} />
        ) : null}

        {node.node_type === "product_source" ? (
          <ProductSourceEditor
            key={node.id}
            node={node}
            graphProductId={graph.product_id}
            busy={busy}
            graphRevision={graph.revision}
            onSave={persist}
            onSaveStateChange={(status, error) => setSaveState({ status, error })}
          />
        ) : !catalog && catalogLoadError ? (
          <CatalogLoadError
            message={catalogLoadError}
            busy={busy}
            retrying={catalogQuery.isFetching}
            onRetry={retryCatalog}
          />
        ) : !catalog ? (
          <p className="px-1 text-xs text-zinc-500 dark:text-slate-400">{t("app.loading")}</p>
        ) : node.node_type === "image_asset" ? (
          <ImageAssetEditor
            key={node.id}
            node={node}
            catalog={catalog}
            image={image}
            busy={busy}
            graphRevision={graph.revision}
            onBind={onBind}
            onSave={persist}
            onSaveStateChange={(status, error) => setSaveState({ status, error })}
            onPreviewImage={onPreviewImage}
          />
        ) : (
          <CatalogNodeEditor
            key={node.id}
            node={node}
            catalog={catalog}
            busy={busy}
            graphRevision={graph.revision}
            productId={graph.product_id}
            sourceAssetId={node.preview_asset_id}
            onPreviewImage={onPreviewImage}
            header={image && onPreviewImage ? (
              <div className="space-y-3">
                <NodeImagePreview
                  image={image}
                  onPreview={onPreviewImage}
                  aspectRatio={requestedAspectRatio(node)}
                />
                {node.node_type === "image_generation" && node.preview_asset_id && onOpenLocalEdit ? (
                  <button
                    type="button"
                    data-graph-node-local-edit
                    onClick={() => onOpenLocalEdit({ sourceAssetId: node.preview_asset_id!, targetNodeId: node.id })}
                    className="inline-flex h-9 items-center justify-center gap-1.5 rounded-lg border border-border-l1 px-3 text-xs font-semibold text-text-secondary hover:bg-surface-subtle hover:text-text-primary"
                    title={t("localEdit.open")}
                  >
                    <PencilLine size={14} aria-hidden="true" />
                    <span>{t("localEdit.open")}</span>
                  </button>
                ) : null}
                {node.node_type === "image_generation" ? <MeasuredOutputStrip node={node} /> : null}
              </div>
            ) : node.node_type === "image_generation" ? (
              <MeasuredOutputStrip node={node} />
            ) : null}
            onSave={persist}
            onSaveStateChange={(status, error) => setSaveState({ status, error })}
          />
        )}

        {node.node_type === "image_generation" && node.preview_asset_id ? (
          <ImageFidelityCheckController
            key={`${node.id}:${node.preview_asset_id}`}
            productId={graph.product_id}
            assetId={node.preview_asset_id}
            locale={t.locale ?? "zh-CN"}
          />
        ) : null}

        <EdgeList heading={t("graph.inspector.inputs")} empty={t("graph.inspector.inputsEmpty")} items={incoming} onJump={onJump} />
        <RuntimeInputList node={node} graph={graph} lastRun={lastNodeRun} />
        <EdgeList heading={t("graph.inspector.outputs")} empty={t("graph.inspector.outputsEmpty")} items={outgoing} onJump={onJump} />
      </div>
    </InspectorFlushContext.Provider>
  );
}

function PromptResult({ payload }: { payload: Record<string, unknown> | null | undefined }) {
  const { t } = useI18n();
  const goal = typeof payload?.design_goal === "string" ? payload.design_goal.trim() : "";
  if (!goal) return null;
  const content = payload?.content && typeof payload.content === "object"
    ? payload.content as Record<string, unknown>
    : null;
  const background = typeof content?.background === "string" ? content.background.trim() : "";
  return (
    <section className="config-bubble rounded-2xl p-4 shadow-sm">
      <h4 className="text-xs font-semibold text-zinc-950 dark:text-white">{t("graph.inspector.lastPrompt")}</h4>
      <p className="mt-2 text-xs leading-5 text-zinc-700 dark:text-slate-200">{goal}</p>
      {background ? (
        <p className="mt-1 text-[11px] leading-4 text-zinc-500 dark:text-slate-400">{background}</p>
      ) : null}
    </section>
  );
}

function CatalogLoadError({
  message,
  busy,
  retrying,
  onRetry,
}: {
  message: string;
  busy: boolean;
  retrying: boolean;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  return (
    <div
      role="alert"
      className="flex items-start gap-2 rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200"
    >
      <AlertCircle size={14} className="mt-0.5 shrink-0" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p>{message || t("graph.inspector.catalogLoadFailed")}</p>
        <button
          type="button"
          onClick={onRetry}
          disabled={busy || retrying}
          aria-label={t("workbench.retry")}
          title={t("workbench.retry")}
          className="mt-2 inline-flex h-8 items-center gap-1.5 rounded-lg border border-red-200 bg-white px-2.5 text-[11px] font-semibold text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-red-400/30 dark:bg-transparent dark:text-red-200 dark:hover:bg-red-950/30"
        >
          {retrying ? <Loader2 size={12} className="animate-spin" aria-hidden="true" /> : <RotateCcw size={12} aria-hidden="true" />}
          {t("workbench.retry")}
        </button>
      </div>
    </div>
  );
}

function GraphInspectorDashboard({
  graph,
  busy,
  onRunGraph,
  onOpenAdd,
  onOpenLibrary,
}: {
  graph: GraphProjection;
  busy: boolean;
  onRunGraph: () => void;
  onOpenAdd?: () => void;
  onOpenLibrary?: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-3 p-3.5 pb-6" data-graph-node-inspector>
      <section className="config-bubble rounded-2xl p-4 shadow-sm">
        <h3 className="text-sm font-semibold text-zinc-950 dark:text-white">{graph.title}</h3>
        <p className="mt-2 text-xs leading-5 text-zinc-600 dark:text-slate-300">{t("graph.inspector.selectHint")}</p>
        <div className="mt-4 grid gap-2">
          {onOpenAdd ? (
            <button
              type="button"
              onClick={onOpenAdd}
              className="inline-flex h-10 items-center justify-center gap-1.5 rounded-xl border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
            >
              <Plus size={14} aria-hidden="true" />
              {t("graph.inspector.openAdd")}
            </button>
          ) : null}
          {onOpenLibrary ? (
            <button
              type="button"
              onClick={onOpenLibrary}
              className="inline-flex h-10 items-center justify-center gap-1.5 rounded-xl border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
            >
              <Images size={14} aria-hidden="true" />
              {t("graph.inspector.openLibrary")}
            </button>
          ) : null}
          <button
            type="button"
            onClick={onRunGraph}
            disabled={busy}
            className="inline-flex h-10 items-center justify-center gap-1.5 rounded-xl bg-slate-900 px-3 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
          >
            {busy ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Play size={14} aria-hidden="true" />}
            {t("graph.inspector.runGraph")}
          </button>
        </div>
      </section>
    </div>
  );
}

function ProductSourceEditor({
  node,
  graphProductId,
  busy,
  graphRevision,
  onSave,
  onSaveStateChange,
}: {
  node: GraphNode;
  graphProductId: string;
  busy: boolean;
  graphRevision: number;
  onSave: (input: { title: string; config: Record<string, unknown>; boundAssetId: string | null }) => Promise<{ edit_version: number }>;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const sourceDraft = graphProductSourceDraft(node);
  const sourceProductId = sourceDraft.source_product_id;
  const [search, setSearch] = useState("");
  const [factsForm, setFactsForm] = useState<ProductFactsDraft | null>(null);
  const [factsSaveState, setFactsSaveState] = useState<{ status: SaveStatus; error: string | null }>({
    status: "idle",
    error: null,
  });
  const productsQuery = useQuery({
    queryKey: ["product-source-picker", search],
    queryFn: () => api.listProducts({ q: search, page: 1, page_size: 20 }),
    staleTime: 10_000,
  });
  const factsQuery = useQuery<ProductFactsResponse>({
    queryKey: ["product-facts", sourceProductId],
    queryFn: () => api.getProductFacts(sourceProductId as string),
    enabled: Boolean(sourceProductId),
  });
  const editor = useNodeDraftAutosave<GraphTitleDraft>({
    serverValue: graphTitleDraft(node),
    serverEditVersion: graphRevision,
    disabled: busy,
    normalize: (draft) => ({ title: draft.title.trim() }),
    validate: (draft) => validateGraphTitle(draft.title, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft) => onSave({ title: draft.title, config: node.config, boundAssetId: node.bound_asset_id }),
    onStateChange: onSaveStateChange,
  });

  useEffect(() => {
    setSearch("");
    setFactsForm(null);
    setFactsSaveState({ status: "idle", error: null });
  }, [sourceProductId]);

  useEffect(() => {
    if (!sourceProductId) {
      setFactsForm(null);
      return;
    }
    if (factsQuery.data) {
      setFactsForm(productFactsDraft(factsQuery.data.product, factsQuery.data.fact_set));
    }
  }, [factsQuery.data, sourceProductId]);

  const saveBinding = useCallback(async (nextProductId: string | null, productName?: string) => {
    const nextTitle = node.title === t("graph.node.productSource") && productName ? productName : node.title;
    const nextDraft: GraphProductSourceDraft = {
      title: nextTitle,
      source_product_id: nextProductId,
      fact_set_version_id: nextProductId === sourceProductId ? sourceDraft.fact_set_version_id : null,
    };
    try {
      await onSave({
        title: nextTitle,
        config: graphProductSourceConfig(node, nextDraft),
        boundAssetId: node.bound_asset_id,
      });
      onSaveStateChange("saved", null);
    } catch (error) {
      onSaveStateChange("failed", errorMessage(error, t("workbench.error.structure")));
    }
  }, [node, onSave, onSaveStateChange, sourceDraft.fact_set_version_id, sourceProductId, t]);

  const saveFacts = async () => {
    if (!sourceProductId || !factsForm || factsQuery.isPending) return;
    const validationError = validateProductFactsDraft(factsForm);
    if (validationError) {
      setFactsSaveState({ status: "failed", error: productFactsValidationMessage(validationError, t) });
      return;
    }
    const normalized = normalizeProductFactsDraft(factsForm);
    setFactsSaveState({ status: "saving", error: null });
    try {
      const response = await api.updateProductFacts(sourceProductId, {
        expected_fact_version: factsQuery.data?.fact_set?.version ?? node.product_fact_set?.version ?? null,
        name: normalized.name,
        category: normalized.category || null,
        price: normalized.price || null,
        source_note: normalized.source_note || null,
        facts: productFactsPayload(normalized),
      });
      queryClient.setQueryData(["product-facts", sourceProductId], response);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["product-facts", sourceProductId] }),
        queryClient.invalidateQueries({ queryKey: ["product", sourceProductId] }),
        queryClient.invalidateQueries({ queryKey: ["products"] }),
        queryClient.invalidateQueries({ queryKey: ["workflow-graph", graphProductId] }),
      ]);
      setFactsForm(productFactsDraft(response.product, response.fact_set));
      setFactsSaveState({ status: "saved", error: null });
    } catch (error) {
      setFactsSaveState({
        status: "failed",
        error: errorMessage(
          error,
          error instanceof ApiError && error.status === 409
            ? t("graph.inspector.productFactsConflict")
            : t("graph.inspector.productFactsSaveFailed"),
        ),
      });
    }
  };

  const sourceProduct = factsQuery.data?.product ?? node.source_product ?? null;
  const factSet: GraphProductFactSet | null = factsQuery.data
    ? factsQuery.data.fact_set
    : node.product_fact_set ?? null;
  const pickerItems = productsQuery.data?.items ?? [];

  return (
    <AutosaveForm editor={editor} busy={busy}>
      <div className="flex items-center justify-between gap-3">
        <SectionTitle title={t("graph.inspector.productInfo")} />
        <SaveStatusBadge status={factsSaveState.status} />
      </div>
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} disabled={busy} onChange={(title) => editor.update({ title })} />
      <p className="text-[11px] leading-5 text-zinc-500 dark:text-slate-400">{t("graph.inspector.productSourceHint")}</p>
      {!sourceProductId ? (
        <div className="rounded-xl border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs leading-5 text-amber-800 dark:border-amber-400/30 dark:bg-amber-500/10 dark:text-amber-100">
          {t("graph.inspector.productSourceUnbound")}
        </div>
      ) : null}

      <div className="space-y-2">
        <label className="block">
          <span className="mb-1.5 block text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{t("graph.inspector.productSourceSearch")}</span>
          <span className="relative block">
            <Search size={14} className="pointer-events-none absolute left-3 top-3 text-zinc-400" aria-hidden="true" />
            <input
              value={search}
              disabled={busy}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t("graph.inspector.productSourceSearchPlaceholder")}
              className="input-premium h-10 w-full pl-9 pr-3 text-xs outline-none"
            />
          </span>
        </label>
        <div className="max-h-44 space-y-1 overflow-y-auto rounded-xl border border-zinc-200 p-1 dark:border-slate-700">
          {productsQuery.isPending ? <p className="px-2 py-2 text-xs text-zinc-500 dark:text-slate-400">{t("app.loading")}</p> : null}
          {!productsQuery.isPending && pickerItems.length === 0 ? <p className="px-2 py-2 text-xs text-zinc-500 dark:text-slate-400">{t("graph.inspector.productSourceNoProducts")}</p> : null}
          {pickerItems.map((item) => (
            <button
              key={item.id}
              type="button"
              disabled={busy || item.id === sourceProductId}
              onClick={() => void saveBinding(item.id, item.name)}
              className="flex w-full min-w-0 items-start justify-between gap-2 rounded-lg px-2.5 py-2 text-left text-xs hover:bg-zinc-50 disabled:cursor-default disabled:opacity-60 dark:hover:bg-slate-800"
            >
              <span className="min-w-0">
                <span className="block truncate font-semibold text-zinc-900 dark:text-slate-100">{item.name}</span>
                <span className="mt-0.5 block truncate text-[10px] text-zinc-500 dark:text-slate-400">{item.category || t("agentWorkbench.nodeEditor.noValue")}</span>
              </span>
              {item.id === sourceProductId ? <span className="shrink-0 text-[10px] text-emerald-600">{t("graph.inspector.productSourceSelected")}</span> : null}
            </button>
          ))}
        </div>
        {sourceProductId ? (
          <button
            type="button"
            disabled={busy}
            onClick={() => void saveBinding(null)}
            className="inline-flex h-9 w-full items-center justify-center rounded-xl border border-slate-200 px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
          >
            {t("graph.inspector.productSourceClear")}
          </button>
        ) : null}
      </div>

      {factsQuery.error ? (
        <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          <AlertCircle size={13} className="mr-1.5 inline" />
          {errorMessage(factsQuery.error, t("graph.inspector.productFactsLoadFailed"))}
        </div>
      ) : null}
      {sourceProductId && factsQuery.isPending ? <p className="text-xs text-zinc-500 dark:text-slate-400">{t("graph.inspector.productFactsLoading")}</p> : null}
      {sourceProductId && factsForm ? (
        <div className="space-y-3 border-t border-zinc-200 pt-4 dark:border-slate-700">
          <div className="flex items-center justify-between gap-2">
            <SectionTitle title={t("graph.inspector.productFacts")} />
            {factSet ? <span className="text-[10px] text-zinc-500 dark:text-slate-400">{t("graph.inspector.productFactsVersion", { version: factSet.version })}</span> : null}
          </div>
          <TextInput label={t("detail.inspector.productName")} value={factsForm.name} maxLength={255} disabled={busy} onChange={(name) => setFactsForm({ ...factsForm, name })} />
          <TextInput label={t("detail.inspector.category")} value={factsForm.category} maxLength={255} disabled={busy} onChange={(category) => setFactsForm({ ...factsForm, category })} />
          <TextInput label={t("detail.inspector.price")} value={factsForm.price} maxLength={120} disabled={busy} onChange={(price) => setFactsForm({ ...factsForm, price })} />
          <TextArea label={t("detail.inspector.productDescription")} value={factsForm.source_note} onChange={(source_note) => setFactsForm({ ...factsForm, source_note })} minRows={2} maxRows={8} disabled={busy} />
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-2">
              <span className="text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{t("graph.inspector.productFacts")}</span>
              <button
                type="button"
                disabled={busy}
                onClick={() => setFactsForm({
                  ...factsForm,
                  facts: [...factsForm.facts, {
                    id: nextFactRowId(factsForm.facts),
                    key: "",
                    value: "",
                    original: { key: "", value: "" },
                  }],
                })}
                className="inline-flex h-8 items-center gap-1 rounded-lg border border-slate-200 px-2 text-[10px] font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
              >
                <Plus size={13} aria-hidden="true" />
                {t("graph.inspector.productFactAdd")}
              </button>
            </div>
            {factsForm.facts.map((fact, index) => (
              <div key={fact.id} className="grid grid-cols-[minmax(0,0.85fr)_minmax(0,1fr)_32px] gap-1.5">
                <input
                  value={fact.key}
                  disabled={busy}
                  aria-label={`${t("graph.inspector.productFactKey")} ${index + 1}`}
                  onChange={(event) => updateFactRow(setFactsForm, factsForm, fact.id, { key: event.target.value })}
                  placeholder={t("graph.inspector.productFactKey")}
                  className="input-premium h-9 min-w-0 px-2 text-xs outline-none"
                />
                <input
                  value={fact.value}
                  disabled={busy}
                  aria-label={`${t("graph.inspector.productFactValue")} ${index + 1}`}
                  onChange={(event) => updateFactRow(setFactsForm, factsForm, fact.id, { value: event.target.value })}
                  placeholder={t("graph.inspector.productFactValue")}
                  className="input-premium h-9 min-w-0 px-2 text-xs outline-none"
                />
                <button
                  type="button"
                  disabled={busy}
                  aria-label={t("graph.inspector.productFactRemove")}
                  title={t("graph.inspector.productFactRemove")}
                  onClick={() => setFactsForm({ ...factsForm, facts: factsForm.facts.filter((item) => item.id !== fact.id) })}
                  className="inline-flex h-9 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:border-red-200 hover:text-red-600 dark:border-slate-700 dark:text-slate-400"
                >
                  <Trash2 size={14} aria-hidden="true" />
                </button>
              </div>
            ))}
          </div>
          {validateProductFactsDraft(factsForm) ? (
            <p role="alert" className="text-[11px] leading-5 text-red-600 dark:text-red-300">{productFactsValidationMessage(validateProductFactsDraft(factsForm), t)}</p>
          ) : null}
          {factsSaveState.error ? <p role="alert" className="text-[11px] leading-5 text-red-600 dark:text-red-300">{factsSaveState.error}</p> : null}
          <button
            type="button"
            onClick={() => void saveFacts()}
            disabled={busy || factsSaveState.status === "saving" || Boolean(validateProductFactsDraft(factsForm))}
            className="inline-flex h-10 w-full items-center justify-center gap-1.5 rounded-xl bg-slate-900 px-3 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
          >
            {factsSaveState.status === "saving" ? <Loader2 size={14} className="animate-spin" /> : <Save size={14} />}
            {factsSaveState.status === "saving" ? t("graph.inspector.productFactsSaving") : t("graph.inspector.productFactsSave")}
          </button>
        </div>
      ) : null}
      {sourceProduct && !factsForm && !factsQuery.isPending ? (
        <ReadOnlyRow label={t("graph.inspector.productSourceSelected")} value={sourceProduct.name} />
      ) : null}
    </AutosaveForm>
  );
}

function ImageAssetEditor({
  node,
  catalog,
  image,
  busy,
  graphRevision,
  onBind,
  onSave,
  onSaveStateChange,
  onPreviewImage,
}: {
  node: GraphNode;
  catalog: GraphNodeCatalog;
  image: DownloadableImage | null;
  busy: boolean;
  graphRevision: number;
  onBind?: () => void;
  onSave: (input: { title: string; config: Record<string, unknown>; boundAssetId: string | null }) => Promise<{ edit_version: number }>;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const fields = graphNodeConfigFields(catalog, node.node_type);
  const editor = useNodeDraftAutosave<CatalogNodeDraft>({
    serverValue: catalogNodeDraft(node, fields),
    serverEditVersion: graphRevision,
    disabled: busy,
    normalize: (draft) => normalizeCatalogDraft(draft, fields),
    validate: (draft) => validateCatalogDraft(draft, fields, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft) => onSave({
      title: draft.title,
      config: catalogConfigForSave(fields, draft.config),
      boundAssetId: node.bound_asset_id,
    }),
    onStateChange: onSaveStateChange,
  });
  const unbind = useCallback(async () => {
    try {
      await editor.flush(true);
      await onSave({
        title: editor.draft.title.trim() || node.title,
        config: catalogConfigForSave(fields, editor.draft.config),
        boundAssetId: null,
      });
      onSaveStateChange("saved", null);
    } catch (error) {
      onSaveStateChange("failed", errorMessage(error, t("workbench.error.structure")));
    }
  }, [editor, fields, node.title, onSave, onSaveStateChange, t]);
  return (
    <AutosaveForm editor={editor} busy={busy}>
      {image && onPreviewImage ? <NodeImagePreview image={image} onPreview={onPreviewImage} aspectRatio={requestedAspectRatio(node)} /> : null}
      {!image ? <p className="text-xs text-zinc-500 dark:text-slate-400">{t("graph.inspector.noPreview")}</p> : null}
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} disabled={busy} onChange={(title) => editor.update({ ...editor.draft, title })} />
      <CatalogConfigFields
        fields={fields}
        value={editor.draft.config}
        onChange={(config) => editor.update({ ...editor.draft, config })}
        disabled={busy}
      />
      {onBind ? (
        <button
          type="button"
          onClick={() => void onBind()}
          disabled={busy}
          className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
        >
          <Link2 size={14} aria-hidden="true" />
          {node.bound_asset_id ? t("graph.inspector.rebind") : t("graph.inspector.bind")}
        </button>
      ) : null}
      {node.bound_asset_id ? (
        <button
          type="button"
          disabled={busy}
          onClick={() => void unbind()}
          className="inline-flex h-10 w-full items-center justify-center rounded-xl border border-slate-200 px-3 text-xs font-semibold text-slate-700 dark:border-slate-700 dark:text-slate-200"
        >
          {t("graph.inspector.unbind")}
        </button>
      ) : null}
    </AutosaveForm>
  );
}

function CatalogNodeEditor({
  node,
  catalog,
  busy,
  graphRevision,
  productId,
  sourceAssetId,
  onPreviewImage,
  header,
  onSave,
  onSaveStateChange,
}: {
  node: GraphNode;
  catalog: GraphNodeCatalog;
  busy: boolean;
  graphRevision: number;
  productId: string;
  sourceAssetId: string | null;
  onPreviewImage?: (image: DownloadableImage) => void;
  header?: ReactNode;
  onSave: (input: { title: string; config: Record<string, unknown>; boundAssetId: string | null }) => Promise<{ edit_version: number }>;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
}) {
  const { t } = useI18n();
  const fields = graphNodeConfigFields(catalog, node.node_type);
  const editor = useNodeDraftAutosave<CatalogNodeDraft>({
    serverValue: catalogNodeDraft(node, fields),
    serverEditVersion: graphRevision,
    disabled: busy,
    normalize: (draft) => normalizeCatalogDraft(draft, fields),
    validate: (draft) => validateCatalogDraft(draft, fields, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft) => onSave({
      title: draft.title,
      config: catalogConfigForSave(fields, draft.config),
      boundAssetId: node.bound_asset_id,
    }),
    onStateChange: onSaveStateChange,
  });
  const applyDeliverySpec = useCallback(async (deliverySpec: WorkflowDeliverySpec) => {
    editor.update(applyDeliveryPresetToCatalogDraft(editor.draft, deliverySpec));
    await editor.flush(true);
  }, [editor]);
  return (
    <AutosaveForm editor={editor} busy={busy}>
      {header}
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} disabled={busy} onChange={(title) => editor.update({ ...editor.draft, title })} />
      <CatalogConfigFields
        fields={fields}
        value={editor.draft.config}
        onChange={(config) => editor.update({ ...editor.draft, config })}
        disabled={busy}
      />
      {node.node_type === "image_generation" && onPreviewImage ? (
        <DeliveryRenditionPanel
          productId={productId}
          sourceAssetId={sourceAssetId}
          deliverySpec={editor.draft.config.delivery_spec}
          onPreviewImage={onPreviewImage}
          onApplyDeliverySpec={applyDeliverySpec}
          applyDisabled={busy}
        />
      ) : null}
    </AutosaveForm>
  );
}

export function applyDeliveryPresetToCatalogDraft(
  draft: CatalogNodeDraft,
  deliverySpec: WorkflowDeliverySpec,
): CatalogNodeDraft {
  return {
    ...draft,
    config: replaceDeliverySpec(draft.config, deliverySpec),
  };
}

function AutosaveForm<T>({
  children,
  busy,
  editor,
}: {
  children: ReactNode;
  busy: boolean;
  editor: NodeDraftAutosave<T>;
}) {
  const { t } = useI18n();
  const flushId = useId();
  const registerFlush = useContext(InspectorFlushContext);
  useEffect(() => registerFlush(flushId, () => editor.flush(true)), [editor.flush, flushId, registerFlush]);
  return (
    <form
      className="config-bubble space-y-4 rounded-2xl p-4 shadow-sm"
      onSubmit={(event) => {
        event.preventDefault();
        void editor.flush(true).catch(() => undefined);
      }}
    >
      {children}
      {editor.dirty || editor.status === "failed" ? (
        <div className="grid grid-cols-2 gap-2 border-t border-zinc-200 pt-4 dark:border-slate-700">
          <button type="button" onClick={editor.discard} disabled={busy} className="inline-flex h-10 items-center justify-center rounded-xl border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200">
            <Undo2 size={14} className="mr-1.5" />
            {t("settings.discard")}
          </button>
          <button type="submit" disabled={busy} className="inline-flex h-10 items-center justify-center rounded-xl bg-slate-900 px-3 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900">
            {busy || editor.status === "saving" ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Save size={14} className="mr-1.5" />}
            {t("detail.save")}
          </button>
        </div>
      ) : null}
    </form>
  );
}

function RuntimeInputList({
  node,
  graph,
  lastRun,
}: {
  node: GraphNode;
  graph: GraphProjection;
  lastRun: GraphNodeRun | null;
}) {
  const { t } = useI18n();
  const currentSources = graphIncomingSourceEntries(node, graph);
  const historicalSources = lastRun ? graphRunInputTraceEntries(lastRun) : [];
  const technical = graphContextEntries(lastRun?.compiled_context ?? null);
  return (
    <section className="config-bubble rounded-2xl p-4 shadow-sm" data-graph-runtime-inputs>
      <h4 className="text-xs font-semibold text-zinc-800 dark:text-slate-100">{t("graph.inspector.currentWiring")}</h4>
      {currentSources.length === 0 ? (
        <p className="mt-2 text-xs text-zinc-500 dark:text-slate-400">{t("graph.inspector.currentWiringEmpty")}</p>
      ) : (
        <ul data-graph-current-wiring className="mt-2 space-y-1">
          {currentSources.map((item) => <RuntimeInputRow key={item.id} item={item} />)}
        </ul>
      )}
      <h4 className="mt-4 text-xs font-semibold text-zinc-800 dark:text-slate-100">{t("graph.inspector.runtimeInputs")}</h4>
      {historicalSources.length === 0 ? (
        <p className="mt-2 text-xs text-zinc-500 dark:text-slate-400">{t("graph.inspector.runtimeInputsEmpty")}</p>
      ) : (
        <ul data-graph-run-inputs className="mt-2 space-y-1">
          {historicalSources.map((item) => <RuntimeInputRow key={item.id} item={item} />)}
        </ul>
      )}
      {technical.length ? (
        <details data-graph-runtime-inputs-technical className="mt-3 border-t border-zinc-100 pt-2 dark:border-slate-800">
          <summary className="cursor-pointer text-[10px] font-semibold text-zinc-500 dark:text-slate-400">
            {t("graph.inspector.runtimeInputsTechnical")}
          </summary>
          <dl className="mt-2 space-y-1">
            {technical.map((item) => (
              <div key={item.key} className="flex min-w-0 items-baseline justify-between gap-3 px-2.5 py-1.5">
                <dt className="shrink-0 text-[10px] text-zinc-500 dark:text-slate-400">{t(item.labelKey)}</dt>
                <dd className="min-w-0 truncate text-right text-xs font-medium text-zinc-800 dark:text-slate-100">{item.value}</dd>
              </div>
            ))}
          </dl>
        </details>
      ) : null}
    </section>
  );
}

function RuntimeInputRow({ item }: { item: ReturnType<typeof graphIncomingSourceEntries>[number] }) {
  const { t } = useI18n();
  const roleKey = graphEdgeRoleLabelKey(item.role);
  const sourceLabel = item.title || (item.sourceNodeId
    ? t("graph.inspector.sourceNode", { id: item.sourceNodeId })
    : t("graph.runs.deletedNode"));
  return (
    <li data-graph-runtime-input-entry={item.id} className="min-w-0 px-2.5 py-1.5">
      <div className="flex min-w-0 items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-xs font-medium text-zinc-800 dark:text-slate-100">{sourceLabel}</div>
          <div className="mt-0.5 flex min-w-0 flex-wrap gap-x-2 text-[10px] text-zinc-500 dark:text-slate-400">
            {item.sourceNodeId ? (
              <span data-graph-source-node-id={item.sourceNodeId} className="truncate">
                {t("graph.inspector.sourceNode", { id: item.sourceNodeId })}
              </span>
            ) : null}
            <span data-graph-input-order={item.order}>{t("graph.inspector.inputOrder", { order: item.order })}</span>
          </div>
        </div>
        <span className="shrink-0 rounded-full bg-zinc-100 px-2 py-0.5 text-[10px] font-medium text-zinc-500 dark:bg-slate-800 dark:text-slate-300">
          {roleKey ? t(roleKey) : item.role}
        </span>
      </div>
      <InputArtifactSummary item={item} />
    </li>
  );
}

function InputArtifactSummary({ item }: { item: ReturnType<typeof graphIncomingSourceEntries>[number] }) {
  const { t } = useI18n();
  return (
    <div data-graph-input-artifact className="mt-1 truncate text-[10px] text-zinc-500 dark:text-slate-400">
      <span>{t("graph.inspector.artifact")}: {item.artifactId ?? t("graph.inspector.artifactUnavailable")}</span>
      {item.artifactType ? <span> · {item.artifactType}</span> : null}
      {item.assetId ? <span> · {t("graph.inspector.asset")}: {item.assetId}</span> : null}
      {item.versionId ? <span> · {t("graph.inspector.version")}: {item.versionId}</span> : null}
    </div>
  );
}

function EdgeList({
  heading,
  empty,
  items,
  onJump,
}: {
  heading: string;
  empty: string;
  items: Array<{ edge: GraphNode["incoming"][number]; related: GraphNode | null }>;
  onJump?: (nodeId: string) => void;
}) {
  const { t } = useI18n();
  return (
    <section className="config-bubble rounded-2xl p-4 shadow-sm">
      <h4 className="text-xs font-semibold text-zinc-800 dark:text-slate-100">{heading}</h4>
      {items.length === 0 ? <p className="mt-2 text-xs text-zinc-500 dark:text-slate-400">{empty}</p> : null}
      <ul className="mt-2 space-y-1">
        {items.map(({ edge, related }) => (
          <li key={edge.id}>
            <button
              type="button"
              disabled={!related || !onJump}
              onClick={() => related && onJump?.(related.id)}
              className="flex w-full min-w-0 items-center justify-between gap-2 rounded-xl px-2.5 py-2 text-left hover:bg-zinc-50 disabled:text-zinc-400 dark:hover:bg-slate-900/60"
            >
              <span className="min-w-0 truncate text-xs font-medium text-zinc-800 dark:text-slate-100">
                {related?.title ?? t("graph.runs.deletedNode")}
              </span>
              <span className="shrink-0 rounded-full bg-zinc-100 px-2 py-0.5 text-[10px] font-medium text-zinc-500 dark:bg-slate-800 dark:text-slate-300">
                {t(edgeRoleKey(edge.role))}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

function requestedAspectRatio(node: GraphNode): string {
  const spec = parseWorkflowGenerationSpec(node.config.generation_spec);
  if (spec?.aspect_ratio) return spec.aspect_ratio;
  const raw = node.config.generation_spec;
  if (raw && typeof raw === "object" && "aspect_ratio" in raw && typeof raw.aspect_ratio === "string") {
    return raw.aspect_ratio;
  }
  return "";
}

function MeasuredOutputStrip({ node }: { node: GraphNode }) {
  const { t } = useI18n();
  const payload = node.current_artifact_payload;
  const measured = payload && typeof payload.measured_output === "object" && payload.measured_output !== null
    ? payload.measured_output as Record<string, unknown>
    : null;
  if (!measured) return null;
  const requestedAspect = typeof measured.requested_aspect_ratio === "string"
    ? measured.requested_aspect_ratio
    : requestedAspectRatio(node);
  const measuredWidth = measured.measured_width;
  const measuredHeight = measured.measured_height;
  const parameters = measured.effective_parameters && typeof measured.effective_parameters === "object"
    ? measured.effective_parameters as Record<string, unknown>
    : {};
  const quality = typeof measured.requested_quality === "string"
    ? measured.requested_quality
    : typeof parameters.quality === "string" ? parameters.quality : "";
  const action = typeof parameters.action === "string" ? parameters.action : "";
  const notes = Array.isArray(parameters.notes)
    ? parameters.notes.filter((note): note is Record<string, unknown> => Boolean(note) && typeof note === "object")
    : [];
  const fallback = notes.find((note) => note.kind === "fallback" || note.kind === "parameter_not_sent");
  const aspectMatched = measured.aspect_matched !== false;
  return (
    <section
      data-measured-output=""
      data-aspect-matched={aspectMatched ? "true" : "false"}
      className="config-bubble rounded-2xl p-4 shadow-sm"
    >
      <h4 className="text-xs font-semibold text-zinc-950 dark:text-white">{t("graph.inspector.measuredOutput")}</h4>
      {aspectMatched ? null : (
        <p role="status" className="mt-2 text-[11px] leading-4 text-amber-700 dark:text-amber-300">
          {t("graph.inspector.aspectMismatch", {
            requested: requestedAspect || "—",
            size: typeof measuredWidth === "number" && typeof measuredHeight === "number"
              ? `${measuredWidth}×${measuredHeight}`
              : "—",
          })}
        </p>
      )}
      <dl className="mt-2 space-y-1 text-[11px] leading-4 text-zinc-600 dark:text-slate-300">
        {requestedAspect ? (
          <div className="flex justify-between gap-3">
            <dt>{t("graph.inspector.requestedAspect")}</dt>
            <dd className="font-medium text-zinc-800 dark:text-slate-100">{requestedAspect}</dd>
          </div>
        ) : null}
        {typeof measuredWidth === "number" && typeof measuredHeight === "number" ? (
          <div className="flex justify-between gap-3">
            <dt>{t("graph.inspector.measuredSize")}</dt>
            <dd className="font-medium text-zinc-800 dark:text-slate-100">{measuredWidth}×{measuredHeight}</dd>
          </div>
        ) : null}
        {quality ? (
          <div className="flex justify-between gap-3">
            <dt>{t("graph.inspector.measuredQuality")}</dt>
            <dd className="font-medium text-zinc-800 dark:text-slate-100">{quality}</dd>
          </div>
        ) : null}
        {action ? (
          <div className="flex justify-between gap-3">
            <dt>{t("graph.inspector.measuredAction")}</dt>
            <dd className="font-medium text-zinc-800 dark:text-slate-100">{action}</dd>
          </div>
        ) : null}
      </dl>
      {fallback && typeof fallback.message === "string" ? (
        <p className="mt-2 text-[11px] leading-4 text-amber-700 dark:text-amber-300">{t("graph.inspector.generationFallback")}: {fallback.message}</p>
      ) : null}
    </section>
  );
}

function NodeImagePreview({
  image,
  onPreview,
  aspectRatio,
}: {
  image: DownloadableImage;
  onPreview: (image: DownloadableImage) => void;
  aspectRatio: string;
}) {
  const { t } = useI18n();
  const parsed = parseAspectRatio(aspectRatio);
  return (
    <div className="relative overflow-hidden rounded-xl border border-zinc-200 dark:border-slate-700">
      <button
        type="button"
        onClick={() => onPreview(image)}
        className={`block w-full ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
        style={parsed ? { aspectRatio: `${parsed.width} / ${parsed.height}` } : undefined}
        aria-label={t("detail.previewImage", { alt: image.alt })}
        data-preview-aspect={parsed ? `${parsed.width}:${parsed.height}` : undefined}
      >
        <img src={image.previewUrl} alt={image.alt} className="h-full w-full object-contain" />
      </button>
      <DownloadLink image={image} variant="overlay" />
    </div>
  );
}

function SectionTitle({ title }: { title: string }) {
  return <h4 className="text-xs font-semibold text-zinc-800 dark:text-slate-100">{title}</h4>;
}

function TextInput({
  label,
  value,
  maxLength,
  disabled = false,
  onChange,
}: {
  label: string;
  value: string;
  maxLength?: number;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{label}</span>
      <input disabled={disabled} value={value} maxLength={maxLength} onChange={(event) => onChange(event.target.value)} className="input-premium h-10 w-full px-3 text-xs outline-none disabled:cursor-not-allowed disabled:opacity-60" />
    </label>
  );
}

function updateFactRow(
  setFactsForm: (next: ProductFactsDraft) => void,
  form: ProductFactsDraft,
  id: string,
  patch: Partial<ProductFactRowDraft>,
) {
  setFactsForm({
    ...form,
    facts: form.facts.map((fact) => fact.id === id ? { ...fact, ...patch } : fact),
  });
}

function nextFactRowId(rows: ProductFactRowDraft[]): string {
  const used = new Set(rows.map((row) => row.id));
  let index = rows.length;
  while (used.has(`fact-${index}`)) index += 1;
  return `fact-${index}`;
}

function productFactsValidationMessage(
  error: ReturnType<typeof validateProductFactsDraft>,
  t: ReturnType<typeof useI18n>["t"],
): string {
  switch (error) {
    case "empty_key":
      return t("graph.inspector.productFactsEmptyKey");
    case "duplicate_key":
      return t("graph.inspector.productFactsDuplicateKey");
    case "empty_value":
      return t("graph.inspector.productFactsEmptyValue");
    default:
      return "";
  }
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.detail) return error.detail;
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

function ReadOnlyRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[112px_minmax(0,1fr)] gap-3 border-b border-zinc-100 py-2 last:border-0 dark:border-slate-800">
      <dt className="text-zinc-500 dark:text-slate-400">{label}</dt>
      <dd className={`min-w-0 break-all text-zinc-800 dark:text-slate-100 ${mono ? "font-mono text-[10px]" : "font-medium"}`}>{value}</dd>
    </div>
  );
}

function edgeRoleKey(role: GraphEdgeRole): TranslationKey {
  const keys = {
    facts: "graph.inspector.role.facts",
    reference: "graph.inspector.role.reference",
    brief: "graph.inspector.role.brief",
    visual_guidance: "graph.inspector.role.visual_guidance",
    prompt: "graph.inspector.role.prompt",
  } as const;
  return keys[role];
}

function configStatusKey(status: GraphConfigStatus): TranslationKey {
  const keys = {
    incomplete: "graph.inspector.config.incomplete",
    ready: "graph.inspector.config.ready",
    stale: "graph.inspector.config.stale",
  } as const;
  return keys[status];
}

function nodePreview(node: GraphNode): DownloadableImage | null {
  if (!node.preview_asset_id && !node.bound_asset_id) return null;
  const assetId = node.preview_asset_id ?? node.bound_asset_id;
  if (!assetId) return null;
  return {
    previewUrl: api.getProductImageAssetMediaUrl(assetId, "thumbnail"),
    downloadUrl: api.getProductImageAssetMediaUrl(assetId),
    filename: `${sanitizeFilenamePart(node.title, "workflow-image")}.png`,
    alt: node.title,
  };
}
