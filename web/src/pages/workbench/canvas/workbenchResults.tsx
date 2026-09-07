/**
 * 懒加载成果集成：切换器与成果层同块，主壳只保留 flow 默认与选择同步入口。
 */

import { Suspense, useCallback, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ListChecks } from "lucide-react";

import { ApiError, api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type {
  GraphNodeCatalog,
  GraphPlannedAction,
  GraphProjection,
  GraphRunLike,
  GraphRunSubmitInput,
} from "../../../lib/types";
import type { LocalImageEditOpenRequest } from "../local-edit/LocalImageEditController";
import { graphEdgeRoleLabelKey, missingRequiredRunNodes, missingRunNodesSummary } from "./graphCatalog";
import {
  adoptedAssetBySlot,
  adoptionGateMessageKey,
  buildAdoptionSlotsReplacingNode,
  evaluateAdoptionGate,
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
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [operationError, setOperationError] = useState<unknown>(null);
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
  const adoptedMap = useMemo(
    () => adoptedAssetBySlot(adoptionQuery.data ?? null),
    [adoptionQuery.data],
  );
  const adoptionBlockByNodeId = useMemo(() => {
    const map = new Map<string, string>();
    for (const node of graph.nodes) {
      if (node.node_type !== "image_generation") continue;
      const gate = evaluateAdoptionGate(
        node.current_artifact_payload as Record<string, unknown> | null | undefined,
      );
      if (!gate.ok) {
        map.set(node.id, t(adoptionGateMessageKey(gate.code)));
      }
    }
    return map;
  }, [graph.nodes, t]);
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
    mutationFn: async (item: GraphResultItem) => {
      if (!item.currentAssetId) throw new ApiError(400, t("graph.results.adoptNeedImage"));
      const slots = buildAdoptionSlotsReplacingNode({
        graph,
        current: adoptionQuery.data ?? null,
        nodeId: item.nodeId,
        sourceAssetId: item.currentAssetId,
        qualityStatus: "unchecked",
      });
      if ("error" in slots) {
        if (slots.error === "missing_delivery_spec") {
          throw new ApiError(400, t("graph.results.adoptNeedSpec"));
        }
        if (slots.error === "text_unqualified" || slots.error === "route_unqualified") {
          throw new ApiError(400, t(adoptionGateMessageKey(slots.error)));
        }
        throw new ApiError(400, t("graph.results.adoptNeedImage"));
      }
      return api.createDeliveryAdoption(productId, {
        slots,
        graph_id: graph.id,
        graph_revision: graph.revision,
      });
    },
    onSuccess: async () => {
      setOperationError(null);
      await queryClient.invalidateQueries({ queryKey: ["delivery-adoption-current", productId] });
    },
    onError: (error) => setOperationError(error),
  });

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
        busy={busy || adoptMutation.isPending || exportMutation.isPending}
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
        adoptedAssetBySlot={adoptedMap}
        adoptionBlockByNodeId={adoptionBlockByNodeId}
        adoptingNodeId={adoptMutation.isPending ? adoptMutation.variables?.nodeId ?? null : null}
        exportingAdoption={exportMutation.isPending}
        onAdoptItem={(item) => adoptMutation.mutate(item)}
        onExportAdoption={adoptionQuery.data ? () => exportMutation.mutate() : undefined}
      />
    </Suspense>
  );
}
