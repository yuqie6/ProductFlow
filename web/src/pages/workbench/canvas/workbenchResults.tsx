/**
 * 懒加载成果集成：切换器与成果层同块，主壳只保留 flow 默认与选择同步入口。
 */

import { Suspense, useCallback, useMemo } from "react";
import { ListChecks } from "lucide-react";

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
  const sections = useMemo(() => projectGraphResults(graph, runs ?? EMPTY_RUNS), [graph, runs]);
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
        onRetryRuns={onRetryRuns}
        busy={busy}
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
      />
    </Suspense>
  );
}
