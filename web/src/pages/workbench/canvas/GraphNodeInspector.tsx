import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  ArrowDown,
  ArrowUp,
  Images,
  Link2,
  Loader2,
  Pin,
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
  GraphRunSubmitInput,
  ProductFactsResponse,
  WorkflowDeliverySpec,
  WorkflowNodeDisplayStatus,
} from "../../../lib/types";
import { parseAspectRatio } from "../../../components/ImageRatioFrame";
import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { Button } from "../../../components/ui/button";
import { Field, Input, TextArea } from "../../../components/ui/field";
import { IconButton } from "../../../components/ui/icon-button";
import { StatusBadge } from "../../../components/ui/status-badge";
import { Tooltip } from "../../../components/ui/tooltip";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "../chrome/constants";
import { parseWorkflowGenerationSpec } from "./generationSpec";
import { graphDocumentOrigin, isContentGraphNodeType } from "./graphDocument";
import { DownloadLink } from "../chrome/ImageDownloadComponents";
import { SaveStatusBadge, type SaveStatus } from "../chrome/SaveStatusBadge";
import { workflowNodeKindTheme } from "../chrome/WorkflowNodeCard";
import { CatalogConfigFields } from "./CatalogConfigFields";
import { DocumentCandidateReview } from "./DocumentCandidateReview";
import {
  catalogConfigForSave,
  catalogNodeDraft,
  normalizeCatalogDraft,
  validateCatalogDraft,
  type CatalogNodeDraft,
} from "./catalogConfig";
import { documentCandidateCatalogFields } from "./documentCandidateView";
import { DeliveryRenditionPanel } from "./DeliveryRenditionPanel";
import { replaceDeliverySpec } from "./deliveryRenditions";
import { graphCatalogNode, graphEdgeRoleLabelKey, graphNodeConfigFields, graphRunBlock, graphRunBlockMessage, missingRequiredRunRoles } from "./graphCatalog";
import { graphNodeHasPinnableOutput, graphNodeTitleKey } from "./graphLayout";
import { graphArtifactTypeLabelKey, graphContextEntries, graphIncomingSourceEntries, graphNodeRunPresentations, graphOutputActionLabelKey, graphOutputQualityLabelKey, graphProgressPhaseLabelKey, graphRunInputTraceEntries, LIVE_RUN_STATUSES } from "./graphRunDisplay";
import { withGraphRunSubmit } from "./graphRunLock";
import { runPreviewPointerHandlers } from "./graphRunPreview";
import { displayNodeState } from "./graphOperationalState";
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
  onPinAsset,
  onJump,
  onPreviewImage,
  onOpenLocalEdit,
  onRegisterFlush,
  onOpenAdd,
  onOpenLibrary,
  onPreviewRun,
  onHideRunPreview,
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
  onPinAsset?: () => void;
  onJump?: (nodeId: string) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
  onRegisterFlush?: (flush: () => Promise<void>) => void;
  onOpenAdd?: () => void;
  onOpenLibrary?: () => void;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
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
  const [replaceConfirmOpen, setReplaceConfirmOpen] = useState(false);
  const [selectedCandidateSections, setSelectedCandidateSections] = useState<string[]>([]);
  const [technicalDetailsNodeId, setTechnicalDetailsNodeId] = useState<string | null>(null);
  const runsQueryKey = ["graph-runs", graph.product_id, graph.id] as const;
  const runsQuery = useQuery({
    queryKey: runsQueryKey,
    queryFn: () => api.listGraphRuns(graph.product_id, graph.id),
    staleTime: 30_000,
    // GraphCanvasPanel owns the shared run SSE and updates this query cache.
  });
  const presentations = useMemo(
    () => graphNodeRunPresentations(runsQuery.data?.items ?? []),
    [runsQuery.data],
  );
  const lastNodeRunRef = useMemo(() => {
    if (!node) return null;
    for (const run of runsQuery.data?.items ?? []) {
      const nodeRun = run.node_runs.find((item) => item.node_id === node.id);
      if (nodeRun) return { runId: run.id, nodeRunId: nodeRun.id };
    }
    return null;
  }, [node?.id, runsQuery.data]);
  const technicalDetailsOpen = Boolean(node && technicalDetailsNodeId === node.id);
  const lastNodeRunQuery = useQuery({
    queryKey: ["graph-run", graph.product_id, graph.id, lastNodeRunRef?.runId],
    queryFn: () => api.getGraphRun(graph.product_id, graph.id, lastNodeRunRef!.runId),
    enabled: Boolean(lastNodeRunRef && technicalDetailsOpen),
  });
  const presentation = node ? presentations[node.id] : undefined;
  const activeRun = node
    ? runsQuery.data?.items.find((run) => run.status === "running" && run.node_runs.some((item) => (
      item.node_id === node.id && LIVE_RUN_STATUSES.has(item.status)
    ))) ?? null
    : null;
  const nodeStatus: WorkflowNodeDisplayStatus = presentation?.status ?? "idle";
  const inspectorDisplayState = node ? displayNodeState(node, nodeStatus) : null;
  const runBlock = useMemo(() => graphRunBlock(graph, catalog), [catalog, graph]);
  const graphRunBlocked = runBlock != null;
  const graphRunBlockedReason = useMemo(() => graphRunBlockMessage(runBlock, t, (role) => {
    const key = graphEdgeRoleLabelKey(role);
    return t("graph.missingRunInput", { role: key ? t(key) : role });
  }), [runBlock, t]);
  const runMutation = useMutation({
    mutationFn: (input: GraphRunSubmitInput) =>
      api.submitGraphRun(graph.product_id, graph.id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: runsQueryKey });
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", graph.product_id] });
    },
  });
  const reorderMutation = useMutation({
    mutationFn: (input: { nodeId: string; role: string; edgeRefs: string[] }) =>
      api.applyWorkflowChangeSet(graph.product_id, graph.id, {
        base_graph_revision: graph.revision,
        summary: "调整连线顺序",
        operations: [{
          op: "reorder_edges",
          node_ref: input.nodeId,
          role: input.role,
          edge_refs: input.edgeRefs,
        }],
      }),
    onSuccess: () => {
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
  const candidateQueryKey = ["graph-document-candidate", graph.product_id, graph.id, node?.id] as const;
  const candidateQuery = useQuery({
    queryKey: candidateQueryKey,
    queryFn: () => api.getGraphDocumentCandidate(graph.product_id, graph.id, node!.id),
    enabled: Boolean(node?.pending_candidate_artifact_id),
  });
  const candidateMutation = useMutation({
    mutationFn: (input: { kind: "apply"; sectionKeys?: string[] } | { kind: "discard" }) => {
      if (!node || !candidateQuery.data) throw new Error(t("graph.candidate.missing"));
      if (input.kind === "discard") {
        return api.discardGraphDocumentCandidate(graph.product_id, graph.id, node.id, candidateQuery.data.artifact_id);
      }
      return api.applyGraphDocumentCandidate(graph.product_id, graph.id, node.id, {
        artifact_id: candidateQuery.data.artifact_id,
        base_graph_revision: graph.revision,
        section_keys: input.sectionKeys,
      });
    },
    onSuccess: () => {
      setSelectedCandidateSections([]);
      queryClient.removeQueries({ queryKey: candidateQueryKey });
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", graph.product_id] });
    },
  });

  useEffect(() => {
    setSaveState({ status: "idle", error: null });
    setReplaceConfirmOpen(false);
    setSelectedCandidateSections([]);
  }, [node?.id]);

  useEffect(() => {
    const candidate = candidateQuery.data;
    if (!candidate) return;
    setSelectedCandidateSections(candidate.sections.filter((section) => section.changed).map((section) => section.key));
  }, [candidateQuery.data?.artifact_id]);

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
  const submitInspectorRun = useCallback((input: GraphRunSubmitInput) => {
    void flushInspector()
      .then(() => withGraphRunSubmit(() => runMutation.mutateAsync(input)))
      .catch(() => undefined);
  }, [flushInspector, runMutation]);

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
        busy={busy || runMutation.isPending}
        runBlocked={graphRunBlocked}
        runBlockedReason={graphRunBlockedReason}
        onRunGraph={() => {
          onHideRunPreview?.();
          submitInspectorRun({ scope: "graph" });
        }}
        onPreviewGraph={() => onPreviewRun?.({ scope: "graph" })}
        onHideRunPreview={onHideRunPreview}
        onOpenAdd={onOpenAdd}
        onOpenLibrary={onOpenLibrary}
      />
    );
  }

  const theme = workflowNodeKindTheme(node.node_type);
  const Icon = theme.icon;
  const image = nodePreview(node);
  const missingRoles = missingRequiredRunRoles(node, catalog);
  const runBlocked = missingRoles.length > 0;
  const origin = graphDocumentOrigin(node);
  const contentNode = isContentGraphNodeType(node.node_type);
  const frozenDocument = contentNode && (origin === "authored" || origin === "generated" || origin === "collaborative");
  const seedDocument = contentNode && origin === "seed";
  const promptSource = node.node_type === "image_generation"
    ? graph.nodes.find((item) => item.id === node.incoming.find((edge) => edge.role === "prompt")?.node_id)
    : null;
  const seedPromptForImage = Boolean(promptSource && graphDocumentOrigin(promptSource) === "seed");
  const canRun = node.node_type === "creative_brief"
    || node.node_type === "visual_system"
    || node.node_type === "image_prompt"
    || node.node_type === "image_generation";
  const mutationError = runMutation.error ?? cancelMutation.error ?? retryMutation.error ?? candidateMutation.error;
  const incoming = node.incoming.map((edge) => ({
    edge,
    related: graph.nodes.find((item) => item.id === edge.node_id) ?? null,
  }));
  const outgoing = node.outgoing.map((edge) => ({
    edge,
    related: graph.nodes.find((item) => item.id === edge.node_id) ?? null,
  }));
  const lastNodeRun = lastNodeRunQuery.data?.node_runs.find(
    (item) => item.id === lastNodeRunRef?.nodeRunId,
  ) ?? null;

  return (
    <InspectorFlushContext.Provider value={registerFlush}>
      <div key={node.id} className="space-y-4 pb-4 motion-safe:animate-node-reveal" data-graph-node-inspector>
        <section className="border-b border-border-l1 pb-4">
          <div className="flex min-w-0 items-center gap-2">
            <Tooltip content={t(graphNodeTitleKey(node.node_type))}>
              <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-control border ${theme.iconBox}`}>
                <Icon size={15} aria-hidden="true" />
              </span>
            </Tooltip>
            <h3 className="min-w-0 flex-1 truncate text-sm font-semibold text-text-primary">{node.title}</h3>
            <StatusBadge status={inspectorDisplayState?.status ?? nodeStatus} spinning={LIVE_RUN_STATUSES.has(nodeStatus)}>
              {inspectorDisplayState ? t(inspectorDisplayState.labelKey) : t(`detail.nodeStatus.${nodeStatus}`)}
            </StatusBadge>
            <Tooltip content={t(configStatusKey(node.config_status))}>
              <span data-graph-config-status={node.config_status} className={`h-2.5 w-2.5 shrink-0 rounded-full ring-2 ring-surface-raised ${node.config_status === "ready" ? "bg-state-success" : node.config_status === "stale" ? "bg-state-warning" : "bg-text-muted"}`}>
                <span className="sr-only">{t(configStatusKey(node.config_status))}</span>
              </span>
            </Tooltip>
            <Tooltip content={saveState.error ?? t(saveStatusKey(saveState.status))}>
              <span data-graph-save-status={saveState.status} className={`h-2.5 w-2.5 shrink-0 rounded-full ring-2 ring-surface-raised ${saveState.status === "failed" ? "bg-state-error" : saveState.status === "saving" ? "bg-accent" : "bg-state-success"}`}>
                <span className="sr-only"><SaveStatusBadge status={saveState.status} /></span>
              </span>
            </Tooltip>
          </div>

          {node.unused && node.bound_asset_id ? (
            <div className="mt-3 rounded-xl border border-border-l1 bg-surface-subtle px-3 py-2 text-xs text-text-secondary">
              {t("graph.inspector.unused")}
            </div>
          ) : null}
          {node.node_type === "image_asset" && !node.bound_asset_id ? (
            <div className="mt-3 rounded-xl border border-border-l1 bg-surface-subtle px-3 py-2 text-xs text-text-secondary">
              {t("graph.inspector.unbound")}
            </div>
          ) : null}
          {missingRoles.length ? (
            <div className="mt-3 rounded-xl border border-state-error/30 bg-state-error-soft px-3 py-2 text-xs text-state-error">
              {missingRoles.map((role) => {
                const key = graphEdgeRoleLabelKey(role);
                return t("graph.missingRunInput", { role: key ? t(key) : t("graph.edgeRole.unknown") });
              }).join(" · ")}
            </div>
          ) : null}
          {seedDocument || seedPromptForImage ? (
            <div className="mt-3 rounded-xl border border-border-l1 bg-surface-subtle px-3 py-2 text-xs text-text-secondary">
              {t("graph.inspector.seedDocument")}
            </div>
          ) : null}
          {presentation?.failureReason && !activeRun ? (
            <div className="mt-3 rounded-xl border border-state-error/30 bg-state-error-soft px-3 py-2.5 text-xs leading-5 text-state-error">
              <div className="font-semibold">{t("graph.inspector.lastFailed")}</div>
              <p className="mt-1">{presentation.failureReason}</p>
              {presentation.lastRunAt ? (
                <p className="mt-1 text-[10px] text-state-error/80">
                  {t("graph.inspector.lastRun", { time: formatDateTime(presentation.lastRunAt, t.locale) })}
                </p>
              ) : null}
            </div>
          ) : null}
          {activeRun ? (
            <div className="mt-3 flex items-start gap-2 rounded-xl border border-accent/30 bg-accent-soft px-3 py-2.5 text-xs text-text-primary">
              <Loader2 size={14} className="mt-0.5 shrink-0 animate-spin" />
              <div>
                <div className="font-semibold">{t(`detail.nodeStatus.${nodeStatus}`)}</div>
                {graphProgressPhaseLabelKey(presentation?.progressPhase) ? (
                  <div className="mt-0.5 text-[10px]">{t(graphProgressPhaseLabelKey(presentation?.progressPhase)!)}</div>
                ) : null}
                {presentation?.elapsedLabel ? (
                  <div className="mt-0.5 text-[10px]">{presentation.elapsedLabel}</div>
                ) : null}
              </div>
            </div>
          ) : null}

          {canRun || activeRun || presentation?.retryable || (graphNodeHasPinnableOutput(node) && onPinAsset) ? (
            <div className="mt-3 flex flex-wrap items-center gap-1.5" data-graph-inspector-actions>
              {canRun ? (
                <>
                  <Button
                    data-graph-inspector-run-node
                    variant="primary"
                    size="toolbar"
                    onClick={() => {
                      onHideRunPreview?.();
                      submitInspectorRun({ scope: "node", node_id: node.id });
                    }}
                    disabled={runMutation.isPending || busy || runBlocked}
                    {...runPreviewPointerHandlers(
                      { scope: "node", node_id: node.id },
                      onPreviewRun,
                      onHideRunPreview,
                    )}
                  >
                    {runMutation.isPending && runMutation.variables?.scope === "node" ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Play size={14} className="mr-1.5" />}
                    {t("graph.runs.scope.node")}
                  </Button>
                  <IconButton
                    label={t("graph.runs.scope.toNode")}
                    data-graph-inspector-run-to-node
                    onClick={() => {
                      onHideRunPreview?.();
                      submitInspectorRun({ scope: "to_node", node_id: node.id });
                    }}
                    disabled={runMutation.isPending || busy || runBlocked}
                    {...runPreviewPointerHandlers(
                      { scope: "to_node", node_id: node.id },
                      onPreviewRun,
                      onHideRunPreview,
                    )}
                  >
                    {runMutation.isPending && runMutation.variables?.scope === "to_node" ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />}
                  </IconButton>
                </>
              ) : null}
              {frozenDocument ? (
                <>
                  <IconButton
                    label={t("graph.inspector.complete")}
                    data-graph-inspector-complete
                    onClick={() => {
                      onHideRunPreview?.();
                      submitInspectorRun({
                        scope: "node",
                        node_id: node.id,
                        force: true,
                        document_action: "complete",
                      });
                    }}
                    disabled={runMutation.isPending || busy}
                    {...runPreviewPointerHandlers(
                      { scope: "node", node_id: node.id, force: true, document_action: "complete" },
                      onPreviewRun,
                      onHideRunPreview,
                    )}
                  >
                    {runMutation.isPending && runMutation.variables?.document_action === "complete" ? <Loader2 size={14} className="animate-spin" /> : <Plus size={14} />}
                  </IconButton>
                  <IconButton
                    label={t("graph.inspector.rewrite")}
                    data-graph-inspector-rewrite
                    onClick={() => {
                      onHideRunPreview?.();
                      submitInspectorRun({
                        scope: "node",
                        node_id: node.id,
                        force: true,
                        document_action: "rewrite",
                      });
                    }}
                    disabled={runMutation.isPending || busy}
                    {...runPreviewPointerHandlers(
                      { scope: "node", node_id: node.id, force: true, document_action: "rewrite" },
                      onPreviewRun,
                      onHideRunPreview,
                    )}
                  >
                    {runMutation.isPending && runMutation.variables?.document_action === "rewrite" ? <Loader2 size={14} className="animate-spin" /> : <PencilLine size={14} />}
                  </IconButton>
                  <IconButton
                    label={t("graph.inspector.replace")}
                    data-graph-inspector-replace
                    onClick={() => setReplaceConfirmOpen(true)}
                    disabled={runMutation.isPending || busy}
                    {...runPreviewPointerHandlers(
                      { scope: "node", node_id: node.id, force: true, document_action: "replace" },
                      onPreviewRun,
                      onHideRunPreview,
                    )}
                  >
                    {runMutation.isPending && runMutation.variables?.document_action === "replace" ? <Loader2 size={14} className="animate-spin" /> : <RotateCcw size={14} />}
                  </IconButton>
                </>
              ) : null}
              {activeRun ? (
                <IconButton
                  label={t("detail.cancel")}
                  variant="danger"
                  onClick={() => cancelMutation.mutate(activeRun.id)}
                  disabled={cancelMutation.isPending}
                  busy={cancelMutation.isPending}
                >
                  <XCircle size={14} aria-hidden="true" />
                </IconButton>
              ) : null}
              {presentation?.retryable && presentation.runId && !activeRun ? (
                <IconButton
                  label={t("graph.inspector.retryRun")}
                  onClick={() => void retryRun()}
                  disabled={retryMutation.isPending || busy}
                  busy={retryMutation.isPending}
                >
                  <RotateCcw size={14} />
                </IconButton>
              ) : null}
              {graphNodeHasPinnableOutput(node) && onPinAsset ? (
                <IconButton label={t("graph.canvas.pinAsset")} data-graph-pin-asset onClick={onPinAsset} disabled={busy}>
                  <Pin size={14} aria-hidden="true" />
                </IconButton>
              ) : null}
            </div>
          ) : null}
        </section>

        {saveState.error || mutationError ? (
          <div role="alert" className="rounded-panel border border-state-error/30 bg-state-error-soft px-3 py-2.5 text-xs leading-5 text-state-error">
            <AlertCircle size={13} className="mr-1.5 inline" />
            {saveState.error ?? t("workbench.error.structure")}
          </div>
        ) : null}

        {node.pending_candidate_artifact_id ? (
          <DocumentCandidateReview
            candidate={candidateQuery.data ?? null}
            loading={candidateQuery.isLoading}
            error={candidateQuery.error ? errorMessage(candidateQuery.error, t("graph.candidate.loadFailed")) : null}
            fields={documentCandidateCatalogFields(catalog, node.node_type)}
            selectedKeys={selectedCandidateSections}
            busy={busy || candidateMutation.isPending}
            onToggle={(key) => setSelectedCandidateSections((current) => (
              current.includes(key) ? current.filter((item) => item !== key) : [...current, key]
            ))}
            onApplySelected={() => candidateMutation.mutate({ kind: "apply", sectionKeys: selectedCandidateSections })}
            onApplyAll={() => candidateMutation.mutate({ kind: "apply" })}
            onDiscard={() => candidateMutation.mutate({ kind: "discard" })}
            onRetry={() => void candidateQuery.refetch()}
          />
        ) : null}

        {node.node_type === "image_prompt" ? (
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
        ) : catalog && !graphCatalogNode(catalog, node.node_type) ? (
          <CatalogLoadError
            message={t("graph.catalog.unknownNodes")}
            busy={busy}
            retrying={catalogQuery.isFetching}
            onRetry={retryCatalog}
          />
        ) : !catalog ? (
          <p className="px-1 text-xs text-text-muted">{t("app.loading")}</p>
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
                  <IconButton
                    label={t("localEdit.open")}
                    data-graph-node-local-edit
                    onClick={() => onOpenLocalEdit({ sourceAssetId: node.preview_asset_id!, targetNodeId: node.id })}
                  >
                    <PencilLine size={14} aria-hidden="true" />
                  </IconButton>
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

        <IncomingEdgeList
          heading={t("graph.inspector.inputs")}
          empty={t("graph.inspector.inputsEmpty")}
          items={incoming}
          lastRun={lastNodeRun}
          busy={busy || reorderMutation.isPending}
          onJump={onJump}
          onReorder={(role, edgeRefs) => reorderMutation.mutate({ nodeId: node.id, role, edgeRefs })}
        />
        <EdgeList heading={t("graph.inspector.outputs")} empty={t("graph.inspector.outputsEmpty")} items={outgoing} onJump={onJump} />
        <GraphTechnicalDetails
          node={node}
          graph={graph}
          lastRun={lastNodeRun}
          open={technicalDetailsOpen}
          onOpenChange={(open) => setTechnicalDetailsNodeId(open ? node.id : null)}
        />
      </div>
      <ConfirmDialog
        open={replaceConfirmOpen}
        title={t("graph.inspector.replace")}
        description={t("graph.inspector.replaceConfirm")}
        confirmLabel={t("graph.inspector.replace")}
        cancelLabel={t("common.cancel")}
        busy={runMutation.isPending}
        onConfirm={() => {
          setReplaceConfirmOpen(false);
          submitInspectorRun({
            scope: "node",
            node_id: node.id,
            force: true,
            document_action: "replace",
          });
        }}
        onClose={() => setReplaceConfirmOpen(false)}
      />
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
    <section className="border-b border-border-l1 pb-4">
      <h4 className="text-xs font-semibold text-text-primary">{t("graph.inspector.lastPrompt")}</h4>
      <p className="mt-2 text-xs leading-5 text-text-secondary">{goal}</p>
      {background ? (
        <p className="mt-1 text-[11px] leading-4 text-text-muted">{background}</p>
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
      className="flex items-start gap-2 rounded-xl border border-state-error/30 bg-state-error-soft px-3 py-2.5 text-xs leading-5 text-state-error"
    >
      <AlertCircle size={14} className="mt-0.5 shrink-0" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p>{message || t("graph.inspector.catalogLoadFailed")}</p>
        <Button
          variant="dangerSoft"
          size="sm"
          className="mt-2"
          onClick={onRetry}
          disabled={busy || retrying}
          busy={retrying}
          aria-label={t("workbench.retry")}
        >
          {retrying ? null : <RotateCcw size={12} aria-hidden="true" />}
          {t("workbench.retry")}
        </Button>
      </div>
    </div>
  );
}

function GraphInspectorDashboard({
  graph,
  busy,
  runBlocked,
  runBlockedReason,
  onRunGraph,
  onPreviewGraph,
  onHideRunPreview,
  onOpenAdd,
  onOpenLibrary,
}: {
  graph: GraphProjection;
  busy: boolean;
  runBlocked: boolean;
  runBlockedReason?: string;
  onRunGraph: () => void;
  onPreviewGraph?: () => void;
  onHideRunPreview?: () => void;
  onOpenAdd?: () => void;
  onOpenLibrary?: () => void;
}) {
  const { t } = useI18n();
  return (
    <div key={graph.id} className="px-1 py-2 motion-safe:animate-node-reveal" data-graph-node-inspector>
      <section>
        <h3 className="text-sm font-semibold text-text-primary">{graph.title}</h3>
        <p className="mt-1.5 text-xs leading-5 text-text-muted">{t("graph.inspector.selectHint")}</p>
        <div className="mt-5 grid gap-2">
          {onOpenAdd ? (
            <Button variant="secondary" size="lg" className="w-full" onClick={onOpenAdd}>
              <Plus size={14} aria-hidden="true" />
              {t("graph.inspector.openAdd")}
            </Button>
          ) : null}
          {onOpenLibrary ? (
            <Button variant="secondary" size="lg" className="w-full" onClick={onOpenLibrary}>
              <Images size={14} aria-hidden="true" />
              {t("graph.inspector.openLibrary")}
            </Button>
          ) : null}
          <Tooltip content={runBlocked ? runBlockedReason : undefined}>
            <span className="block w-full">
              <Button
                variant="primary"
                size="lg"
                className="w-full min-h-11"
                onClick={onRunGraph}
                disabled={busy || runBlocked}
                title={runBlocked ? runBlockedReason : undefined}
                busy={busy}
                {...runPreviewPointerHandlers(
                  { scope: "graph" },
                  onPreviewGraph ? () => onPreviewGraph() : undefined,
                  onHideRunPreview,
                )}
              >
                {busy ? null : <Play size={14} aria-hidden="true" />}
                {t("graph.inspector.runGraph")}
              </Button>
            </span>
          </Tooltip>
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
  const searchInputId = useId();
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
    versionConflictMessage: t("graph.inspector.draftConflict"),
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
      <p className="text-[11px] leading-5 text-text-muted">{t("graph.inspector.productSourceHint")}</p>
      {!sourceProductId ? (
        <div className="rounded-xl border border-state-warning/35 bg-state-warning-soft px-3 py-2.5 text-xs leading-5 text-state-warning">
          {t("graph.inspector.productSourceUnbound")}
        </div>
      ) : null}

      <div className="space-y-2">
        <Field label={t("graph.inspector.productSourceSearch")} htmlFor={searchInputId}>
          <span className="relative block">
            <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-muted" aria-hidden="true" />
            <Input
              id={searchInputId}
              value={search}
              disabled={busy}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t("graph.inspector.productSourceSearchPlaceholder")}
              className="pl-9 pr-3"
            />
          </span>
        </Field>
        <div className="max-h-44 space-y-1 overflow-y-auto rounded-xl border border-border-l1 p-1">
          {productsQuery.isPending ? <p className="px-2 py-2 text-xs text-text-muted">{t("app.loading")}</p> : null}
          {!productsQuery.isPending && pickerItems.length === 0 ? <p className="px-2 py-2 text-xs text-text-muted">{t("graph.inspector.productSourceNoProducts")}</p> : null}
          {pickerItems.map((item) => (
            <button
              key={item.id}
              type="button"
              disabled={busy || item.id === sourceProductId}
              onClick={() => void saveBinding(item.id, item.name)}
              className="flex w-full min-w-0 items-start justify-between gap-2 rounded-lg px-2.5 py-2 text-left text-xs hover:bg-surface-subtle disabled:cursor-default disabled:opacity-60"
            >
              <span className="min-w-0">
                <span className="block truncate font-semibold text-text-primary">{item.name}</span>
                <span className="mt-0.5 block truncate text-[10px] text-text-muted">{item.category || t("agentWorkbench.nodeEditor.noValue")}</span>
              </span>
              {item.id === sourceProductId ? <span className="shrink-0 text-[10px] text-state-success">{t("graph.inspector.productSourceSelected")}</span> : null}
            </button>
          ))}
        </div>
        {sourceProductId ? (
          <Button
            variant="secondary"
            size="md"
            className="w-full"
            disabled={busy}
            onClick={() => void saveBinding(null)}
          >
            {t("graph.inspector.productSourceClear")}
          </Button>
        ) : null}
      </div>

      {factsQuery.error ? (
        <div role="alert" className="rounded-xl border border-state-error/30 bg-state-error-soft px-3 py-2.5 text-xs leading-5 text-state-error">
          <AlertCircle size={13} className="mr-1.5 inline" />
          {errorMessage(factsQuery.error, t("graph.inspector.productFactsLoadFailed"))}
        </div>
      ) : null}
      {sourceProductId && factsQuery.isPending ? <p className="text-xs text-text-muted">{t("graph.inspector.productFactsLoading")}</p> : null}
      {sourceProductId && factsForm ? (
        <div className="space-y-3 border-t border-border-l1 pt-4">
          <div className="flex items-center justify-between gap-2">
            <SectionTitle title={t("graph.inspector.productFacts")} />
            {factSet ? <span className="text-[10px] text-text-muted">{t("graph.inspector.productFactsVersion", { version: factSet.version })}</span> : null}
          </div>
          <TextInput label={t("detail.inspector.productName")} value={factsForm.name} maxLength={255} disabled={busy} onChange={(name) => setFactsForm({ ...factsForm, name })} />
          <TextInput label={t("detail.inspector.category")} value={factsForm.category} maxLength={255} disabled={busy} onChange={(category) => setFactsForm({ ...factsForm, category })} />
          <TextInput label={t("detail.inspector.price")} value={factsForm.price} maxLength={120} disabled={busy} onChange={(price) => setFactsForm({ ...factsForm, price })} />
          <TextArea label={t("detail.inspector.productDescription")} value={factsForm.source_note} onChange={(source_note) => setFactsForm({ ...factsForm, source_note })} minRows={2} maxRows={8} disabled={busy} />
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-2">
              <span className="text-[10px] font-semibold text-text-muted">{t("graph.inspector.productFacts")}</span>
              <Button
                variant="secondary"
                size="sm"
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
              >
                <Plus size={13} aria-hidden="true" />
                {t("graph.inspector.productFactAdd")}
              </Button>
            </div>
            {factsForm.facts.map((fact, index) => (
              <div key={fact.id} className="grid grid-cols-[minmax(0,0.85fr)_minmax(0,1fr)_44px] gap-1.5 lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1fr)_32px]">
                <Input
                  value={fact.key}
                  disabled={busy}
                  aria-label={`${t("graph.inspector.productFactKey")} ${index + 1}`}
                  onChange={(event) => updateFactRow(setFactsForm, factsForm, fact.id, { key: event.target.value })}
                  placeholder={t("graph.inspector.productFactKey")}
                  className="min-w-0 px-2"
                />
                <Input
                  value={fact.value}
                  disabled={busy}
                  aria-label={`${t("graph.inspector.productFactValue")} ${index + 1}`}
                  onChange={(event) => updateFactRow(setFactsForm, factsForm, fact.id, { value: event.target.value })}
                  placeholder={t("graph.inspector.productFactValue")}
                  className="min-w-0 px-2"
                />
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-11 w-11 px-0 text-text-muted hover:text-state-error lg:h-9 lg:w-8"
                  disabled={busy}
                  aria-label={t("graph.inspector.productFactRemove")}
                  onClick={() => setFactsForm({ ...factsForm, facts: factsForm.facts.filter((item) => item.id !== fact.id) })}
                >
                  <Trash2 size={14} aria-hidden="true" />
                </Button>
              </div>
            ))}
          </div>
          {validateProductFactsDraft(factsForm) ? (
            <p role="alert" className="text-[11px] leading-5 text-state-error">{productFactsValidationMessage(validateProductFactsDraft(factsForm), t)}</p>
          ) : null}
          {factsSaveState.error ? <p role="alert" className="text-[11px] leading-5 text-state-error">{factsSaveState.error}</p> : null}
          <Button
            variant="primary"
            size="lg"
            className="w-full"
            onClick={() => void saveFacts()}
            disabled={busy || factsSaveState.status === "saving" || Boolean(validateProductFactsDraft(factsForm))}
            busy={factsSaveState.status === "saving"}
          >
            {factsSaveState.status === "saving" ? null : <Save size={14} />}
            {factsSaveState.status === "saving" ? t("graph.inspector.productFactsSaving") : t("graph.inspector.productFactsSave")}
          </Button>
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
    versionConflictMessage: t("graph.inspector.draftConflict"),
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
      {!image ? <p className="text-xs text-text-muted">{t("graph.inspector.noPreview")}</p> : null}
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} disabled={busy} onChange={(title) => editor.update({ ...editor.draft, title })} />
      <CatalogConfigFields
        fields={fields}
        value={editor.draft.config}
        onChange={(config) => editor.update({ ...editor.draft, config })}
        disabled={busy}
      />
      {onBind ? (
        <Button
          variant="secondary"
          size="lg"
          className="w-full"
          onClick={() => void onBind()}
          disabled={busy}
        >
          <Link2 size={14} aria-hidden="true" />
          {node.bound_asset_id ? t("graph.inspector.rebind") : t("graph.inspector.bind")}
        </Button>
      ) : null}
      {node.bound_asset_id ? (
        <Button
          variant="secondary"
          size="lg"
          className="w-full"
          disabled={busy}
          onClick={() => void unbind()}
        >
          {t("graph.inspector.unbind")}
        </Button>
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
    versionConflictMessage: t("graph.inspector.draftConflict"),
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
      className="space-y-4 border-b border-border-l1 pb-4"
      onSubmit={(event) => {
        event.preventDefault();
        void editor.flush(true).catch(() => undefined);
      }}
    >
      {children}
      {editor.dirty || editor.status === "failed" ? (
        <div className="grid grid-cols-2 gap-2 border-t border-border-l1 pt-4">
          <Button type="button" variant="secondary" size="lg" onClick={editor.discard} disabled={busy}>
            <Undo2 size={14} />
            {t("settings.discard")}
          </Button>
          <Button type="submit" variant="primary" size="lg" disabled={busy} busy={editor.status === "saving"}>
            {busy || editor.status === "saving" ? null : <Save size={14} />}
            {t("detail.save")}
          </Button>
        </div>
      ) : null}
    </form>
  );
}

function GraphTechnicalDetails({
  node,
  graph,
  lastRun,
  open,
  onOpenChange,
}: {
  node: GraphNode;
  graph: GraphProjection;
  lastRun: GraphNodeRun | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const currentSources = graphIncomingSourceEntries(node, graph);
  const historicalSources = lastRun ? graphRunInputTraceEntries(lastRun) : [];
  const technical = graphContextEntries(lastRun?.compiled_context ?? null);
  const identifiers = [
    { label: t("graph.inspector.nodeId"), value: node.id },
    ...(lastRun ? [{ label: t("graph.inspector.nodeRunId"), value: lastRun.id }] : []),
    ...historicalSources.flatMap((item) => [
      item.artifactId ? { label: t("graph.inspector.artifact"), value: item.artifactId } : null,
      item.versionId ? { label: t("graph.inspector.version"), value: item.versionId } : null,
      item.assetId ? { label: t("graph.inspector.asset"), value: item.assetId } : null,
      item.sourceNodeId ? { label: t("graph.inspector.sourceNodeId"), value: item.sourceNodeId } : null,
      item.artifactType ? { label: t("graph.inspector.artifactType"), value: graphArtifactTypeLabelKey(item.artifactType) ? t(graphArtifactTypeLabelKey(item.artifactType)!) : t("graph.artifactType.unknown") } : null,
    ].filter((item): item is { label: string; value: string } => Boolean(item))),
  ];
  return (
    <details
      className="border-b border-border-l1 py-3"
      data-graph-technical-details
      open={open}
      onToggle={(event) => onOpenChange(event.currentTarget.open)}
    >
      <summary className="cursor-pointer text-xs font-semibold text-text-primary">
        {t("graph.inspector.runtimeInputsTechnical")}
      </summary>
      <dl className="mt-3 space-y-1 border-t border-border-l2 pt-2">
        {identifiers.map((item, index) => (
          <ReadOnlyRow key={`${item.label}:${item.value}:${index}`} label={item.label} value={item.value} mono />
        ))}
        {technical.map((item) => (
          <ReadOnlyRow key={item.key} label={t(item.labelKey)} value={item.value} mono />
        ))}
        {!currentSources.length && !historicalSources.length && !technical.length ? (
          <div className="py-2">
            <dt className="sr-only">{t("graph.inspector.runtimeInputsTechnical")}</dt>
            <dd className="text-xs text-text-muted">{t("graph.inspector.runtimeInputsEmpty")}</dd>
          </div>
        ) : null}
      </dl>
    </details>
  );
}

function IncomingEdgeList({
  heading,
  empty,
  items,
  lastRun,
  busy,
  onJump,
  onReorder,
}: {
  heading: string;
  empty: string;
  items: Array<{ edge: GraphNode["incoming"][number]; related: GraphNode | null }>;
  lastRun: GraphNodeRun | null;
  busy: boolean;
  onJump?: (nodeId: string) => void;
  onReorder: (role: string, edgeRefs: string[]) => void;
}) {
  const { t } = useI18n();
  const [draggingEdgeId, setDraggingEdgeId] = useState<string | null>(null);
  const actualEdgeIds = new Set((lastRun?.input_trace ?? []).map((item) => item.edge_id));
  const grouped = new Map<string, typeof items>();
  for (const item of items) {
    const group = grouped.get(item.edge.role) ?? [];
    group.push(item);
    grouped.set(item.edge.role, group);
  }
  return (
    <section className="border-b border-border-l1 pb-4">
      <h4 className="text-xs font-semibold text-text-primary">{heading}</h4>
      {items.length === 0 ? <p className="mt-2 text-xs text-text-muted">{empty}</p> : null}
      <div className="mt-2 space-y-3">
        {[...grouped.entries()].map(([role, group]) => {
          const roleKey = graphEdgeRoleLabelKey(role);
          const ordered = group.slice().sort((left, right) => left.edge.order - right.edge.order || left.edge.id.localeCompare(right.edge.id));
          return (
            <div key={role}>
              <div className="mb-1 text-[10px] font-semibold text-text-muted">
                {roleKey ? t(roleKey) : t("graph.edgeRole.unknown")}
              </div>
              <ul className="space-y-1">
                {ordered.map((item, index) => {
                  const assetId = item.related?.preview_asset_id ?? item.related?.bound_asset_id;
                  const primaryBrief = role === "brief" && index === 0;
                  const RelatedIcon = item.related ? workflowNodeKindTheme(item.related.node_type).icon : Link2;
                  const usedInLastRun = actualEdgeIds.has(item.edge.id);
                  return (
                    <li
                      key={item.edge.id}
                      draggable={!busy && ordered.length > 1}
                      aria-grabbed={draggingEdgeId === item.edge.id}
                      onDragStart={(event) => {
                        if (busy || ordered.length < 2) return;
                        setDraggingEdgeId(item.edge.id);
                        event.dataTransfer.effectAllowed = "move";
                        event.dataTransfer.setData("text/plain", item.edge.id);
                      }}
                      onDragEnd={() => setDraggingEdgeId(null)}
                      onDragOver={(event) => {
                        if (draggingEdgeId && draggingEdgeId !== item.edge.id) event.preventDefault();
                      }}
                      onDrop={(event) => {
                        event.preventDefault();
                        const sourceId = draggingEdgeId ?? event.dataTransfer.getData("text/plain");
                        if (!sourceId || sourceId === item.edge.id || busy) {
                          setDraggingEdgeId(null);
                          return;
                        }
                        const refs = ordered.map((entry) => entry.edge.id);
                        const sourceIndex = refs.indexOf(sourceId);
                        const targetIndex = refs.indexOf(item.edge.id);
                        if (sourceIndex < 0 || targetIndex < 0) {
                          setDraggingEdgeId(null);
                          return;
                        }
                        const [moved] = refs.splice(sourceIndex, 1);
                        if (!moved) {
                          setDraggingEdgeId(null);
                          return;
                        }
                        refs.splice(targetIndex, 0, moved);
                        setDraggingEdgeId(null);
                        onReorder(role, refs);
                      }}
                      className={`flex items-center gap-2 rounded-xl px-1 py-1 ${draggingEdgeId === item.edge.id ? "bg-surface-subtle" : ""}`}
                    >
                      {assetId ? (
                        <img
                          src={api.getProductImageAssetMediaUrl(assetId, "thumbnail")}
                          alt=""
                          className="h-8 w-8 shrink-0 rounded-md object-cover"
                        />
                      ) : (
                        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-surface-subtle text-text-muted">
                          <RelatedIcon size={13} aria-hidden="true" />
                        </span>
                      )}
                      <button
                        type="button"
                        disabled={!item.related || !onJump}
                        onClick={() => item.related && onJump?.(item.related.id)}
                        className="min-w-0 flex-1 truncate text-left text-xs font-medium text-text-primary hover:underline disabled:text-text-muted"
                      >
                        {item.related?.title ?? t("graph.runs.deletedNode")}
                        {primaryBrief ? ` · ${t("graph.inspector.primaryBrief")}` : ""}
                      </button>
                      {usedInLastRun ? (
                        <Tooltip content={t("graph.inspector.runtimeInputs")}>
                          <span data-graph-runtime-input-used className="h-2.5 w-2.5 shrink-0 rounded-full bg-state-success ring-2 ring-surface-raised">
                            <span className="sr-only">{t("graph.inspector.runtimeInputs")}</span>
                          </span>
                        </Tooltip>
                      ) : null}
                      {ordered.length > 1 ? (
                        <span className="flex shrink-0 gap-0.5">
                          <button
                            type="button"
                            disabled={busy || index === 0}
                            aria-label={t("graph.inspector.moveUp")}
                            onClick={() => {
                              const refs = ordered.map((entry) => entry.edge.id);
                              const swap = refs[index - 1];
                              refs[index - 1] = refs[index];
                              refs[index] = swap;
                              onReorder(role, refs);
                            }}
                            className="rounded p-1 text-text-muted hover:bg-surface-subtle disabled:opacity-30"
                          >
                            <ArrowUp size={12} />
                          </button>
                          <button
                            type="button"
                            disabled={busy || index === ordered.length - 1}
                            aria-label={t("graph.inspector.moveDown")}
                            onClick={() => {
                              const refs = ordered.map((entry) => entry.edge.id);
                              const swap = refs[index + 1];
                              refs[index + 1] = refs[index];
                              refs[index] = swap;
                              onReorder(role, refs);
                            }}
                            className="rounded p-1 text-text-muted hover:bg-surface-subtle disabled:opacity-30"
                          >
                            <ArrowDown size={12} />
                          </button>
                        </span>
                      ) : null}
                    </li>
                  );
                })}
              </ul>
            </div>
          );
        })}
      </div>
    </section>
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
    <section className="border-b border-border-l1 pb-4">
      <h4 className="text-xs font-semibold text-text-primary">{heading}</h4>
      {items.length === 0 ? <p className="mt-2 text-xs text-text-muted">{empty}</p> : null}
      <ul className="mt-2 space-y-1">
        {items.map(({ edge, related }) => (
          <li key={edge.id}>
            <button
              type="button"
              disabled={!related || !onJump}
              onClick={() => related && onJump?.(related.id)}
              className="flex w-full min-w-0 items-center justify-between gap-2 rounded-xl px-2.5 py-2 text-left hover:bg-surface-subtle disabled:text-text-muted"
            >
              <span className="min-w-0 truncate text-xs font-medium text-text-primary">
                {related?.title ?? t("graph.runs.deletedNode")}
              </span>
              <span className="shrink-0 rounded-full bg-surface-subtle px-2 py-0.5 text-[10px] font-medium text-text-muted">
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
      className="border-t border-border-l1 pt-3"
    >
      <h4 className="text-xs font-semibold text-text-primary">{t("graph.inspector.measuredOutput")}</h4>
      {aspectMatched ? null : (
        <p role="status" className="mt-2 text-[11px] leading-4 text-state-warning">
          {t("graph.inspector.aspectMismatch", {
            requested: requestedAspect || "—",
            size: typeof measuredWidth === "number" && typeof measuredHeight === "number"
              ? `${measuredWidth}×${measuredHeight}`
              : "—",
          })}
        </p>
      )}
      <dl className="mt-2 space-y-1 text-[11px] leading-4 text-text-secondary">
        {requestedAspect ? (
          <div className="flex justify-between gap-3">
            <dt>{t("graph.inspector.requestedAspect")}</dt>
            <dd className="font-medium text-text-primary">{requestedAspect}</dd>
          </div>
        ) : null}
        {typeof measuredWidth === "number" && typeof measuredHeight === "number" ? (
          <div className="flex justify-between gap-3">
            <dt>{t("graph.inspector.measuredSize")}</dt>
            <dd className="font-medium text-text-primary">{measuredWidth}×{measuredHeight}</dd>
          </div>
        ) : null}
        {quality ? (
          <div className="flex justify-between gap-3">
            <dt>{t("graph.inspector.measuredQuality")}</dt>
            <dd className="font-medium text-text-primary">{graphOutputQualityLabelKey(quality) ? t(graphOutputQualityLabelKey(quality)!) : t("graph.output.quality.unknown")}</dd>
          </div>
        ) : null}
        {action ? (
          <div className="flex justify-between gap-3">
            <dt>{t("graph.inspector.measuredAction")}</dt>
            <dd className="font-medium text-text-primary">{graphOutputActionLabelKey(action) ? t(graphOutputActionLabelKey(action)!) : t("graph.output.action.unknown")}</dd>
          </div>
        ) : null}
      </dl>
      {fallback && typeof fallback.message === "string" ? (
        <p className="mt-2 text-[11px] leading-4 text-state-warning">{t("graph.inspector.generationFallback")}: {fallback.message}</p>
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
    <div className="relative overflow-hidden rounded-xl border border-border-l1">
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
  return <h4 className="text-xs font-semibold text-text-primary">{title}</h4>;
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
    <Input
      label={label}
      value={value}
      maxLength={maxLength}
      disabled={disabled}
      onChange={(event) => onChange(event.target.value)}
    />
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
    <div className="grid grid-cols-[112px_minmax(0,1fr)] gap-3 border-b border-border-l2 py-2 last:border-0">
      <dt className="text-text-muted">{label}</dt>
      <dd className={`min-w-0 break-all text-text-primary ${mono ? "font-mono text-[10px]" : "font-medium"}`}>{value}</dd>
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

function saveStatusKey(status: SaveStatus): TranslationKey {
  const keys: Record<SaveStatus, TranslationKey> = {
    idle: "detail.inspector.saveIdle",
    saving: "detail.inspector.saving",
    saved: "detail.inspector.saved",
    failed: "detail.inspector.saveFailed",
  };
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
