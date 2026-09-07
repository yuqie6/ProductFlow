/**
 * 懒加载成果集成：切换器与成果层同块，主壳只保留 flow 默认与选择同步入口。
 */

import { Suspense, useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ListChecks } from "lucide-react";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { ApiError, api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type {
  DeliveryAdoptionCreateInput,
  GraphNodeCatalog,
  GraphPlannedAction,
  GraphProjection,
  GraphRunLike,
  GraphRunSubmitInput,
} from "../../../lib/types";
import type { LocalImageEditOpenRequest } from "../local-edit/LocalImageEditController";
import { graphEdgeRoleLabelKey, missingRequiredRunNodes, missingRunNodesSummary } from "./graphCatalog";
import {
  adoptedSlotByNodeId,
  assessAdoptionQuality,
  assessDeliveryAdoptionFreshness,
  buildAdoptionSlotsReplacingNode,
} from "./deliveryAdoption";
import { projectGraphResults, type GraphResultItem } from "./resultProjection";
import { GraphResultsView } from "./GraphResultsView";

const EMPTY_RUNS: GraphRunLike[] = [];

export function graphHasResultWorkbenchItems(graph: GraphProjection): boolean {
  return graph.nodes.some((node) => (
    node.node_type === "image_generation"
    || (node.node_type === "image_asset" && node.config?.role === "evidence")
  ));
}

export function WorkbenchResultsViewSwitcher({
  mainView,
  onMainViewChange,
}: {
  mainView: "flow" | "results";
  onMainViewChange: (view: "flow" | "results") => void;
}) {
  const { t } = useI18n();
  const showFlowView = mainView === "flow";
  return (
    <div
      role="group"
      aria-label={t("graph.canvas.viewSwitcher")}
      data-graph-main-view-switcher
      className="flex items-center rounded-control border border-border-l1 bg-surface-raised p-0.5"
    >
      <button
        type="button"
        aria-label={t("graph.canvas.viewFlow")}
        aria-pressed={showFlowView}
        data-graph-main-view="flow"
        className={`h-8 rounded-[calc(var(--radius-control)-2px)] px-2 text-[11px] font-semibold outline-none focus-visible:ring-2 focus-visible:ring-focus-ring ${
          showFlowView ? "bg-surface-subtle text-text-primary" : "text-text-secondary hover:text-text-primary"
        }`}
        onClick={() => onMainViewChange("flow")}
      >
        {t("graph.canvas.viewFlow")}
      </button>
      <button
        type="button"
        aria-label={t("graph.canvas.viewResults")}
        aria-pressed={!showFlowView}
        data-graph-main-view="results"
        className={`h-8 rounded-[calc(var(--radius-control)-2px)] px-2 text-[11px] font-semibold outline-none focus-visible:ring-2 focus-visible:ring-focus-ring ${
          !showFlowView ? "bg-surface-subtle text-text-primary" : "text-text-secondary hover:text-text-primary"
        }`}
        onClick={() => onMainViewChange("results")}
      >
        {t("graph.canvas.viewResults")}
      </button>
    </div>
  );
}

export function WorkbenchResultsLayer({
  productId,
  graph,
  catalog,
  runs,
  runsLoading,
  runsFetching,
  runsError,
  onRetryRuns,
  busy,
  runningNodeId,
  selectedNodeIds,
  plannedActions,
  onSelect,
  onRequestFlowFocus,
  onSubmitNodeRun,
  onHideRunPreview,
  onPreviewRun,
  onOpenLocalEdit,
  onPreviewImage,
  onBindNode,
  headerActions,
}: {
  productId: string;
  graph: GraphProjection;
  catalog: GraphNodeCatalog | null;
  runs: readonly GraphRunLike[] | undefined;
  runsLoading?: boolean;
  runsFetching?: boolean;
  runsError?: unknown;
  onRetryRuns?: () => void;
  busy: boolean;
  runningNodeId: string | null;
  selectedNodeIds: readonly string[];
  plannedActions?: Readonly<Record<string, GraphPlannedAction>>;
  onSelect: (nodeIds: string[]) => void | Promise<void>;
  onRequestFlowFocus: (nodeId: string, groupId: string | null) => void;
  onSubmitNodeRun: (nodeId: string) => void;
  onHideRunPreview?: () => void;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
  onPreviewImage?: (assetId: string, alt: string) => void;
  onBindNode?: (nodeId: string) => void;
  headerActions?: ReactNode;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [operationError, setOperationError] = useState<unknown>(null);
  const [qualityConfirmation, setQualityConfirmation] = useState<{
    productId: string;
    item: GraphResultItem;
    body: DeliveryAdoptionCreateInput;
    detail: string;
  } | null>(null);
  useEffect(() => {
    setQualityConfirmation(null);
    setOperationError(null);
  }, [productId]);
  const sections = useMemo(() => projectGraphResults(graph, runs ?? EMPTY_RUNS), [graph, runs]);
  const adoptionQuery = useQuery({
    queryKey: ["delivery-adoption-current", productId],
    queryFn: async () => {
      try {
        return await api.getCurrentDeliveryAdoption(productId);
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
  });
  const deliveryAdoptionFreshness = useMemo(() => {
    const currentAssetByNodeId = new Map<string, string | null>();
    for (const section of sections) {
      for (const item of section.items) currentAssetByNodeId.set(item.nodeId, item.currentAssetId);
    }
    return assessDeliveryAdoptionFreshness({
      graph,
      current: adoptionQuery.data ?? null,
      currentAssetByNodeId,
    });
  }, [adoptionQuery.data, graph, sections]);
  const adoptedSlotMap = useMemo(
    () => adoptedSlotByNodeId(adoptionQuery.data ?? null),
    [adoptionQuery.data],
  );
  const adoptionQualityByNodeId = useMemo(() => {
    const map = new Map<string, ReturnType<typeof assessAdoptionQuality>>();
    for (const node of graph.nodes) {
      if (node.node_type !== "image_generation") continue;
      map.set(node.id, assessAdoptionQuality(
        node.current_artifact_payload as Record<string, unknown> | null | undefined,
      ));
    }
    return map;
  }, [graph.nodes]);
  const blockedReasons = useMemo(() => {
    const reasons: Record<string, string> = {};
    for (const node of graph.nodes) {
      if (node.node_type !== "image_generation") continue;
      const missing = missingRequiredRunNodes(graph, catalog, new Set([node.id]));
      if (!missing.length) continue;
      reasons[node.id] = missingRunNodesSummary(missing, (role) => {
        const key = graphEdgeRoleLabelKey(role);
        return t("graph.missingRunInput", { role: key ? t(key) : t("graph.edgeRole.unknown") });
      });
    }
    return reasons;
  }, [catalog, graph, t]);

  const selectItem = useCallback((item: GraphResultItem) => {
    void onSelect([item.nodeId]);
  }, [onSelect]);
  const locateItem = useCallback((item: GraphResultItem) => {
    onRequestFlowFocus(item.nodeId, item.groupId);
  }, [onRequestFlowFocus]);
  const openHistory = useCallback((item: GraphResultItem) => {
    void onSelect([item.nodeId]);
  }, [onSelect]);
  const runItem = useCallback((item: GraphResultItem) => {
    if (!item.runnable) return;
    onSubmitNodeRun(item.nodeId);
  }, [onSubmitNodeRun]);
  const openLocalEdit = useCallback((item: GraphResultItem) => {
    if (!item.currentAssetId || !onOpenLocalEdit) return;
    onOpenLocalEdit({
      sourceAssetId: item.currentAssetId,
      targetNodeId: item.kind === "generation" ? item.nodeId : null,
    });
  }, [onOpenLocalEdit]);
  const previewImage = useCallback((item: GraphResultItem) => {
    if (!item.currentAssetId || !onPreviewImage) return;
    onPreviewImage(item.currentAssetId, item.title);
  }, [onPreviewImage]);
  const bindEvidence = useCallback((item: GraphResultItem) => {
    if (item.kind !== "evidence") return;
    onBindNode?.(item.nodeId);
  }, [onBindNode]);

  const adoptMutation = useMutation({
    mutationFn: async ({ productId: requestProductId, body }: {
      productId: string;
      item: GraphResultItem;
      body: DeliveryAdoptionCreateInput;
    }) => (
      api.createDeliveryAdoption(requestProductId, body)
    ),
    onSuccess: async (_data, variables) => {
      if (variables.productId !== productId) return;
      setOperationError(null);
      setQualityConfirmation(null);
      await queryClient.invalidateQueries({ queryKey: ["delivery-adoption-current", productId] });
    },
    onError: (error, variables) => {
      if (variables.productId !== productId) return;
      if (
        error instanceof ApiError
        && error.status === 409
        && error.code === "adoption_quality_confirmation_required"
      ) {
        setOperationError(null);
        setQualityConfirmation({
          productId: variables.productId,
          item: variables.item,
          body: variables.body,
          detail: error.detail,
        });
        return;
      }
      setQualityConfirmation(null);
      setOperationError(error);
    },
  });

  const adoptItem = useCallback((item: GraphResultItem) => {
    if (!item.currentAssetId) {
      setOperationError(new ApiError(400, t("graph.results.adoptNeedImage")));
      return;
    }
    const slots = buildAdoptionSlotsReplacingNode({
      graph,
      current: adoptionQuery.data ?? null,
      nodeId: item.nodeId,
      sourceAssetId: item.currentAssetId,
    });
    if ("error" in slots) {
      setOperationError(new ApiError(
        400,
        slots.error === "missing_delivery_spec"
          ? t("graph.results.adoptNeedSpec")
          : t("graph.results.adoptNeedImage"),
      ));
      return;
    }
    setOperationError(null);
    adoptMutation.mutate({
      productId,
      item,
      body: {
        acknowledge_quality_warnings: false,
        slots,
        graph_id: graph.id,
        graph_revision: graph.revision,
      },
    });
  }, [adoptMutation, adoptionQuery.data, graph, productId, t]);

  const confirmQualityWarning = useCallback(() => {
    if (!qualityConfirmation || qualityConfirmation.productId !== productId) {
      setQualityConfirmation(null);
      return;
    }
    adoptMutation.mutate({
      productId: qualityConfirmation.productId,
      item: qualityConfirmation.item,
      body: {
        ...qualityConfirmation.body,
        acknowledge_quality_warnings: true,
      },
    });
  }, [adoptMutation, productId, qualityConfirmation]);

  const exportMutation = useMutation({
    mutationFn: async () => {
      const current = adoptionQuery.data;
      if (!current) throw new ApiError(404, t("graph.results.exportNeedAdoption"));
      await api.ensureDeliveryAdoptionRenditions(productId, current.id);
      const preview = await api.previewDeliveryAdoption(productId, current.id);
      if (!preview.export_ready) {
        const first = preview.issues[0]?.message ?? t("graph.results.exportNotReady");
        throw new ApiError(409, first);
      }
      return api.downloadDeliveryAdoptionExport(productId, current.id);
    },
    onSuccess: async (blob) => {
      setOperationError(null);
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = "delivery-adoption.zip";
      anchor.click();
      URL.revokeObjectURL(url);
      await queryClient.invalidateQueries({ queryKey: ["delivery-adoption-current", productId] });
    },
    onError: (error) => setOperationError(error),
  });

  return (
    <>
      <Suspense
      fallback={(
        <div className="flex h-full items-center justify-center text-text-muted" aria-label={t("app.loading")}>
          <ListChecks size={18} className="animate-pulse motion-reduce:animate-none" aria-hidden="true" />
        </div>
      )}
    >
      <GraphResultsView
        sections={sections}
        runsLoading={runsLoading}
        runsFetching={runsFetching}
        runsError={runsError}
        operationError={operationError ?? adoptionQuery.error}
        onRetryRuns={onRetryRuns}
        busy={busy || adoptMutation.isPending || exportMutation.isPending || Boolean(qualityConfirmation)}
        runningNodeId={runningNodeId}
        selectedNodeIds={selectedNodeIds}
        plannedActions={plannedActions}
        blockedReasons={blockedReasons}
        onSelectItem={selectItem}
        onLocateItem={locateItem}
        onOpenHistory={openHistory}
        onRunItem={runItem}
        onPreviewRun={onPreviewRun}
        onHideRunPreview={onHideRunPreview}
        onOpenLocalEdit={onOpenLocalEdit ? openLocalEdit : undefined}
        onPreviewImage={onPreviewImage ? previewImage : undefined}
        onBindEvidence={onBindNode ? bindEvidence : undefined}
        adoptedSlotByNodeId={adoptedSlotMap}
        deliveryAdoptionFreshness={deliveryAdoptionFreshness}
        adoptionQualityByNodeId={adoptionQualityByNodeId}
        adoptingNodeId={adoptMutation.isPending && adoptMutation.variables?.productId === productId
          ? adoptMutation.variables.item.nodeId
          : null}
        exportingAdoption={exportMutation.isPending}
        onAdoptItem={adoptItem}
        onExportAdoption={adoptionQuery.data ? () => exportMutation.mutate() : undefined}
        headerActions={headerActions}
      />
      </Suspense>
      <ConfirmDialog
      open={qualityConfirmation?.productId === productId}
      title={t("graph.results.qualityConfirmationTitle")}
      description={t("graph.results.qualityConfirmationDescription")}
      body={qualityConfirmation ? (
        <QualityConfirmationBody detail={qualityConfirmation.detail} />
      ) : null}
      confirmLabel={t("graph.results.qualityConfirmationConfirm")}
      cancelLabel={t("common.cancel")}
      busy={adoptMutation.isPending}
      destructive={false}
      onConfirm={confirmQualityWarning}
      onClose={() => {
        if (!adoptMutation.isPending) setQualityConfirmation(null);
      }}
      />
    </>
  );
}

function QualityConfirmationBody({ detail }: { detail: string }) {
  const { t } = useI18n();
  const lines = detail.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  return (
    <div data-delivery-adoption-quality-confirmation className="space-y-3">
      <p className="text-xs leading-5 text-text-secondary">
        {t("graph.results.qualityConfirmationDetails")}
      </p>
      {lines.length ? (
        <ul className="list-disc space-y-1 pl-5 text-xs leading-5 text-state-warning">
          {lines.map((line, index) => <li key={`${line}-${index}`}>{line}</li>)}
        </ul>
      ) : null}
    </div>
  );
}
