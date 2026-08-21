import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Play, Redo2, Undo2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { api, ApiError } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { GraphChangeSet, GraphNodeCatalog, GraphNodeType, GraphProjection } from "../../../lib/types";
import { ProductWorkbenchCanvasChromeToggle } from "../chrome/ProductWorkbenchCanvasChromeToggle";
import { getWorkflowKeyboardShortcut, type WorkflowKeyboardShortcut } from "../chrome/shortcuts";
import type { CanvasInteractionMode } from "../chrome/workflowCanvasInteraction";
import {
  readStoredWorkflowCanvasViewport,
  writeStoredWorkflowCanvasViewport,
  type WorkflowCanvasViewport,
} from "./canvasState";
import {
  buildCreateAndConnectOperations,
  buildReuseConnectOperations,
  resolveGraphAssetDrop,
  type GraphAssetDropPlan,
} from "./graphAssetDrop";
import { GraphWorkflowCanvas } from "./GraphWorkflowCanvas";
import {
  buildDeleteNodeOperations,
  buildDuplicateGraphOperations,
  buildGraphAutoLayoutPositions,
  buildRenameGroupOperations,
  createdGraphNodeIds,
  defaultGraphNodeConfig,
  graphChangeSetClientRef,
  graphNodeTitleKey,
  graphViewportCenterPosition,
} from "./graphLayout";
import { graphNodeRunPresentations } from "./graphRunDisplay";

export interface GraphCanvasActions {
  createNode: (nodeType: GraphNodeType) => void;
  duplicateSelected: () => void;
  groupSelected: () => void;
  dissolveSelected: () => void;
  commitNode: (input: {
    nodeId: string;
    title?: string;
    config?: Record<string, unknown>;
    boundAssetId?: string | null;
  }) => Promise<GraphProjection | void>;
}

export function graphHistoryShortcutAction(
  shortcut: WorkflowKeyboardShortcut,
): "undo" | "redo" | null {
  if (shortcut === "undo" || shortcut === "redo") return shortcut;
  return null;
}

function compactWorkbench(): boolean {
  return typeof window !== "undefined"
    && typeof window.matchMedia === "function"
    && window.matchMedia("(max-width: 1023px)").matches;
}

export function GraphCanvasPanel({
  productId,
  graph,
  catalog,
  selectedNodeIds,
  onSelect,
  onGraphChange,
  onRegisterActions,
  onBindNode,
  onBusyChange,
  onBeforeRun,
  chromeCollapsed = false,
  onToggleChrome,
}: {
  productId: string;
  graph: GraphProjection;
  catalog: GraphNodeCatalog | null;
  selectedNodeIds: string[];
  onSelect: (nodeIds: string[]) => void;
  onGraphChange: (next: GraphProjection) => void;
  onRegisterActions?: (actions: GraphCanvasActions) => void;
  onBindNode?: (nodeId: string) => void;
  onBusyChange?: (busy: boolean) => void;
  onBeforeRun?: () => Promise<void>;
  chromeCollapsed?: boolean;
  onToggleChrome?: () => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const graphRef = useRef(graph);
  const catalogRef = useRef(catalog);
  const selectedRef = useRef(selectedNodeIds);
  const clipboardRef = useRef<string[]>([]);
  const [viewport, setViewport] = useState<WorkflowCanvasViewport | null>(
    () => readStoredWorkflowCanvasViewport(graph.id),
  );
  const viewportRef = useRef(viewport);
  const [compact, setCompact] = useState(compactWorkbench);
  const [mobileMode, setMobileMode] = useState<CanvasInteractionMode>("edit");
  const [reusePrompt, setReusePrompt] = useState<Extract<GraphAssetDropPlan, { kind: "choose_reuse" }> | null>(null);
  const [pendingDeleteIds, setPendingDeleteIds] = useState<string[] | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const noticeTimerRef = useRef<number | null>(null);
  graphRef.current = graph;
  catalogRef.current = catalog;
  selectedRef.current = selectedNodeIds;
  viewportRef.current = viewport;

  useEffect(() => {
    setViewport(readStoredWorkflowCanvasViewport(graph.id));
  }, [graph.id]);

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") return;
    const media = window.matchMedia("(max-width: 1023px)");
    const update = () => setCompact(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  const showNotice = useCallback((message: string) => {
    setNotice(message);
    if (noticeTimerRef.current) window.clearTimeout(noticeTimerRef.current);
    noticeTimerRef.current = window.setTimeout(() => setNotice(null), 2200);
  }, []);

  useEffect(() => () => {
    if (noticeTimerRef.current) window.clearTimeout(noticeTimerRef.current);
  }, []);

  const applyMutation = useMutation({
    mutationFn: (changeSet: GraphChangeSet) => api.applyWorkflowChangeSet(productId, graph.id, changeSet),
    onSuccess: (next) => {
      onGraphChange(next);
      queryClient.setQueryData(["workflow-graph", productId], next);
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        const next = await api.getWorkflowGraph(productId, graph.id);
        onGraphChange(next);
      }
    },
  });

  const undoMutation = useMutation({
    mutationFn: () => api.undoWorkflowChangeSet(productId, graph.id),
    onSuccess: (next) => {
      onGraphChange(next);
      queryClient.setQueryData(["workflow-graph", productId], next);
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        const next = await api.getWorkflowGraph(productId, graph.id);
        onGraphChange(next);
      }
    },
  });

  const redoMutation = useMutation({
    mutationFn: () => api.redoWorkflowChangeSet(productId, graph.id),
    onSuccess: (next) => {
      onGraphChange(next);
      queryClient.setQueryData(["workflow-graph", productId], next);
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        const next = await api.getWorkflowGraph(productId, graph.id);
        onGraphChange(next);
      }
    },
  });

  const runMutation = useMutation({
    mutationFn: (input: { scope: "graph" | "node" | "to_node"; node_id?: string }) =>
      api.submitGraphRun(productId, graph.id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["graph-runs", productId, graph.id] });
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
    },
  });

  const runsQuery = useQuery({
    queryKey: ["graph-runs", productId, graph.id],
    queryFn: () => api.listGraphRuns(productId, graph.id),
    refetchInterval: (query) => query.state.data?.items.some((run) => run.status === "running") ? 1200 : false,
  });

  const nodePresentations = useMemo(
    () => graphNodeRunPresentations(runsQuery.data?.items ?? []),
    [runsQuery.data],
  );
  const nodeStatuses = useMemo(() => {
    const statuses: Record<string, (typeof nodePresentations)[string]["status"]> = {};
    for (const [nodeId, presentation] of Object.entries(nodePresentations)) {
      statuses[nodeId] = presentation.status;
    }
    return statuses;
  }, [nodePresentations]);
  const runningNodeId = runsQuery.data?.items.find((run) => run.status === "running")?.node_runs
    .find((nodeRun) => nodeRun.status === "running")?.node_id ?? null;

  const applyAsync = useCallback(async (summary: string, operations: GraphChangeSet["operations"]) => {
    if (!operations.length || applyMutation.isPending) return null;
    return applyMutation.mutateAsync({
      base_graph_revision: graphRef.current.revision,
      summary,
      operations,
    });
  }, [applyMutation]);

  const apply = useCallback((summary: string, operations: GraphChangeSet["operations"]) => {
    void applyAsync(summary, operations);
  }, [applyAsync]);

  const selectCreatedNodes = useCallback((before: GraphProjection, after: GraphProjection) => {
    const created = createdGraphNodeIds(before, after);
    if (created.length) onSelect(created);
    return created;
  }, [onSelect]);

  const handleAssetDrop = useCallback((input: Parameters<typeof resolveGraphAssetDrop>[1]) => {
    const plan = resolveGraphAssetDrop(graphRef.current, input, catalogRef.current);
    if (plan.kind === "ignored") return;
    if (plan.kind === "choose_reuse") {
      setReusePrompt(plan);
      return;
    }
    apply(plan.summary, plan.operations);
  }, [apply]);

  const createNode = useCallback((nodeType: GraphNodeType) => {
    const before = graphRef.current;
    const position = graphViewportCenterPosition(viewportRef.current);
    void applyAsync("创建节点", [{
      op: "create_node",
      client_ref: graphChangeSetClientRef("node"),
      node_type: nodeType,
      title: t(graphNodeTitleKey(nodeType)),
      position_x: position.position_x,
      position_y: position.position_y,
      config: defaultGraphNodeConfig(nodeType),
    }]).then((next) => {
      if (next) selectCreatedNodes(before, next);
    });
  }, [applyAsync, selectCreatedNodes, t]);

  const duplicateSelected = useCallback((nodeIds = selectedRef.current, noticeKey?: "pasted" | "duplicated") => {
    const { operations } = buildDuplicateGraphOperations(graphRef.current, nodeIds);
    if (!operations.length) return;
    const before = graphRef.current;
    void applyAsync("复制节点", operations).then((next) => {
      if (!next) return;
      const created = selectCreatedNodes(before, next);
      if (!created.length) return;
      if (noticeKey === "pasted") {
        showNotice(t("detail.notice.pastedNodes", { count: created.length }));
      } else if (noticeKey === "duplicated") {
        showNotice(t("detail.notice.duplicatedNodes", { count: created.length }));
      }
    });
  }, [applyAsync, selectCreatedNodes, showNotice, t]);

  const groupSelected = useCallback(() => {
    const nodeIds = selectedRef.current;
    if (nodeIds.length < 2) return;
    apply("创建分组", [{
      op: "create_group",
      client_ref: graphChangeSetClientRef("group"),
      title: t("graph.canvas.group"),
      member_refs: nodeIds,
    }]);
  }, [apply, t]);

  const dissolveSelected = useCallback(() => {
    const current = graphRef.current;
    const groupIds = [...new Set(
      current.nodes
        .filter((node) => selectedRef.current.includes(node.id) && node.group_id)
        .map((node) => node.group_id as string),
    )];
    if (!groupIds.length) return;
    apply("解散分组", groupIds.map((groupId) => ({ op: "dissolve_group", group_ref: groupId })));
  }, [apply]);

  const renameGroup = useCallback((groupId: string, title: string) => {
    apply("重命名分组", buildRenameGroupOperations(groupId, title));
  }, [apply]);

  const dissolveGroup = useCallback((groupId: string) => {
    apply("解散分组", [{ op: "dissolve_group", group_ref: groupId }]);
  }, [apply]);

  const requestDeleteNodes = useCallback((nodeIds: string[]) => {
    if (!nodeIds.length) return;
    setPendingDeleteIds(nodeIds);
  }, []);

  const confirmDeleteNodes = useCallback(() => {
    if (!pendingDeleteIds?.length) return;
    apply("删除节点", buildDeleteNodeOperations(pendingDeleteIds));
    setPendingDeleteIds(null);
    onSelect([]);
  }, [apply, onSelect, pendingDeleteIds]);

  const commitNode = useCallback(async (input: {
    nodeId: string;
    title?: string;
    config?: Record<string, unknown>;
    boundAssetId?: string | null;
  }) => {
    const current = graphRef.current.nodes.find((node) => node.id === input.nodeId);
    if (!current) return graphRef.current;
    const operations: GraphChangeSet["operations"] = [];
    if (input.title && input.title !== current.title) {
      operations.push({ op: "rename_node", node_ref: input.nodeId, title: input.title });
    }
    if (input.config || input.boundAssetId !== undefined) {
      operations.push({
        op: "update_node_config",
        node_ref: input.nodeId,
        config: input.config ?? current.config,
        bound_asset_id: input.boundAssetId === undefined ? current.bound_asset_id : input.boundAssetId,
      });
    }
    if (!operations.length) return graphRef.current;
    return applyMutation.mutateAsync({
      base_graph_revision: graphRef.current.revision,
      summary: "更新节点",
      operations,
    });
  }, [applyMutation]);

  const submitRun = useCallback(async (input: { scope: "graph" | "node" | "to_node"; node_id?: string }) => {
    try {
      await onBeforeRun?.();
    } catch {
      return;
    }
    runMutation.mutate(input);
  }, [onBeforeRun, runMutation]);

  const handleViewportChange = useCallback((next: WorkflowCanvasViewport) => {
    setViewport(next);
    writeStoredWorkflowCanvasViewport(graph.id, next);
  }, [graph.id]);

  const actionsRef = useRef<GraphCanvasActions>({
    createNode,
    duplicateSelected,
    groupSelected,
    dissolveSelected,
    commitNode,
  });
  actionsRef.current = {
    createNode,
    duplicateSelected,
    groupSelected,
    dissolveSelected,
    commitNode,
  };
  useEffect(() => {
    onRegisterActions?.({
      createNode: (nodeType) => actionsRef.current.createNode(nodeType),
      duplicateSelected: () => actionsRef.current.duplicateSelected(),
      groupSelected: () => actionsRef.current.groupSelected(),
      dissolveSelected: () => actionsRef.current.dissolveSelected(),
      commitNode: (input) => actionsRef.current.commitNode(input),
    });
  }, [onRegisterActions]);

  useEffect(() => {
    const handleShortcut = (event: KeyboardEvent) => {
      const shortcut = getWorkflowKeyboardShortcut(event);
      if (!shortcut || applyMutation.isPending || undoMutation.isPending || redoMutation.isPending) return;
      const selected = selectedRef.current;
      if (shortcut === "copy") {
        if (!selected.length) return;
        event.preventDefault();
        clipboardRef.current = selected;
        showNotice(t("detail.notice.copiedNodes", { count: selected.length }));
        return;
      }
      if (shortcut === "paste") {
        if (!clipboardRef.current.length) return;
        event.preventDefault();
        duplicateSelected(clipboardRef.current, "pasted");
        return;
      }
      if (shortcut === "duplicate") {
        if (!selected.length) return;
        event.preventDefault();
        duplicateSelected(selected, "duplicated");
        return;
      }
      if (shortcut === "delete") {
        if (!selected.length) return;
        event.preventDefault();
        requestDeleteNodes(selected);
        return;
      }
      if (graphHistoryShortcutAction(shortcut) === "undo") {
        if (!graphRef.current.can_undo) return;
        event.preventDefault();
        undoMutation.mutate();
        return;
      }
      if (graphHistoryShortcutAction(shortcut) === "redo") {
        if (!graphRef.current.can_redo) return;
        event.preventDefault();
        redoMutation.mutate();
      }
    };
    window.addEventListener("keydown", handleShortcut);
    return () => window.removeEventListener("keydown", handleShortcut);
  }, [applyMutation.isPending, duplicateSelected, redoMutation, requestDeleteNodes, showNotice, t, undoMutation]);

  const error = applyMutation.error ?? runMutation.error ?? undoMutation.error ?? redoMutation.error;
  const busy = applyMutation.isPending || runMutation.isPending || undoMutation.isPending || redoMutation.isPending;

  useEffect(() => {
    onBusyChange?.(busy);
  }, [busy, onBusyChange]);
  useEffect(() => () => onBusyChange?.(false), [onBusyChange]);

  const pendingDeleteTitle = pendingDeleteIds?.length === 1
    ? graph.nodes.find((node) => node.id === pendingDeleteIds[0])?.title ?? ""
    : "";

  return (
    <div
      data-graph-canvas-panel
      className="relative flex h-full min-h-0 flex-col overflow-hidden bg-zinc-50 text-zinc-950 dark:bg-[#080c12] dark:text-slate-100"
    >
      <div
        className={`absolute z-20 flex items-center gap-1 rounded-xl border border-border-l1 bg-surface-raised/95 p-1 shadow-sm backdrop-blur ${compact ? "right-3 top-[4.75rem]" : "right-4 top-4"
          }`}
      >
        <button
          type="button"
          disabled={busy || !graph.can_undo}
          onClick={() => undoMutation.mutate()}
          className="inline-flex h-11 w-11 items-center justify-center rounded-lg text-text-secondary hover:bg-surface-subtle hover:text-text-primary disabled:opacity-45 lg:h-9 lg:w-9"
          aria-label={t("graph.canvas.undo")}
          title={t("graph.canvas.undo")}
        >
          <Undo2 size={16} aria-hidden="true" />
        </button>
        <button
          type="button"
          disabled={busy || !graph.can_redo}
          onClick={() => redoMutation.mutate()}
          className="inline-flex h-11 w-11 items-center justify-center rounded-lg text-text-secondary hover:bg-surface-subtle hover:text-text-primary disabled:opacity-45 lg:h-9 lg:w-9"
          aria-label={t("graph.canvas.redo")}
          title={t("graph.canvas.redo")}
        >
          <Redo2 size={16} aria-hidden="true" />
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => void submitRun({ scope: "graph" })}
          className="inline-flex h-11 w-11 items-center justify-center rounded-lg bg-accent text-accent-fg hover:bg-accent-strong disabled:opacity-45 lg:h-9 lg:w-9"
          aria-label={t("graph.canvas.run")}
          title={t("graph.canvas.run")}
        >
          <Play size={16} aria-hidden="true" />
        </button>
        {onToggleChrome ? (
          <ProductWorkbenchCanvasChromeToggle
            embedded
            collapsed={chromeCollapsed}
            maximizeLabel={t("detail.maximizeCanvas")}
            restoreLabel={t("detail.restoreCanvas")}
            onToggle={onToggleChrome}
          />
        ) : null}
      </div>
      {error ? (
        <div role="alert" className="absolute left-4 top-4 z-10 max-w-sm rounded bg-red-50 px-3 py-2 text-xs text-red-700">
          {error instanceof ApiError ? error.detail : t("workbench.error.structure")}
        </div>
      ) : notice ? (
        <div role="status" className="absolute left-4 top-4 z-10 max-w-sm rounded-lg border border-slate-200 bg-white/95 px-3 py-2 text-xs font-medium text-slate-700 shadow-sm dark:border-slate-700 dark:bg-[#111a2b] dark:text-slate-200">
          {notice}
        </div>
      ) : null}
      <div className="relative min-h-0 flex-1 overflow-hidden">
        <GraphWorkflowCanvas
          graph={graph}
          catalog={catalog}
          selectedNodeIds={selectedNodeIds}
          busy={busy}
          nodeStatuses={nodeStatuses}
          nodePresentations={nodePresentations}
          runningNodeId={runningNodeId}
          viewport={viewport}
          onViewportChange={handleViewportChange}
          compact={compact}
          mobileInteractionMode={mobileMode}
          onMobileInteractionModeChange={setMobileMode}
          onSelect={onSelect}
          onConnect={(source, target) => apply("连接节点", [{
            op: "connect_nodes",
            client_ref: graphChangeSetClientRef("edge"),
            source_ref: source,
            target_ref: target,
          }])}
          onMove={(nodes) => apply("移动节点", [{
            op: "move_nodes",
            nodes: nodes.map((item) => [item.node_id, item.position_x, item.position_y]),
          }])}
          onTranslateGroup={(groupId, deltaX, deltaY) => {
            const members = graph.nodes.filter((node) => node.group_id === groupId);
            if (!members.length) return;
            apply("移动分组", [{
              op: "move_nodes",
              nodes: members.map((node) => [node.id, node.position_x + deltaX, node.position_y + deltaY]),
            }]);
          }}
          onDeleteNode={(nodeId) => requestDeleteNodes([nodeId])}
          onDeleteEdge={(edgeId) => apply("断开连线", [{ op: "disconnect_edge", edge_ref: edgeId }])}
          onRunNode={(nodeId) => void submitRun({ scope: "node", node_id: nodeId })}
          onRunToNode={(nodeId) => void submitRun({ scope: "to_node", node_id: nodeId })}
          onBindNode={(nodeId) => onBindNode?.(nodeId)}
          onDuplicateNode={(nodeIds) => duplicateSelected(nodeIds, "duplicated")}
          onAssetDrop={handleAssetDrop}
          onRenameGroup={renameGroup}
          onDissolveGroup={dissolveGroup}
          onAutoLayout={() => {
            const positions = buildGraphAutoLayoutPositions(graph);
            if (!positions.length) return;
            apply("自动布局", [{
              op: "move_nodes",
              nodes: positions.map((item) => [item.node_id, item.position_x, item.position_y]),
            }]);
          }}
        />
      </div>
      {reusePrompt ? (
        <div className="absolute inset-0 z-30 flex items-center justify-center bg-slate-950/40 p-4">
          <div role="dialog" aria-modal="true" className="w-full max-w-sm rounded-2xl bg-white p-4 shadow-xl dark:bg-[#0f1726]">
            <h3 className="text-sm font-semibold text-zinc-950 dark:text-white">{t("graph.drop.reuseTitle")}</h3>
            <p className="mt-1 text-xs leading-5 text-zinc-500 dark:text-slate-400">{t("graph.drop.reuseDescription")}</p>
            <div className="mt-3 space-y-2">
              {reusePrompt.existing.map((node) => (
                <button
                  key={node.id}
                  type="button"
                  onClick={() => {
                    apply("连接参考图", buildReuseConnectOperations(node.id, reusePrompt.targetNodeId));
                    setReusePrompt(null);
                  }}
                  className="flex h-10 w-full items-center rounded-xl border border-zinc-200 px-3 text-left text-xs font-semibold text-zinc-800 dark:border-slate-700 dark:text-slate-100"
                >
                  {t("graph.drop.reuseNode", { title: node.title })}
                </button>
              ))}
              <button
                type="button"
                onClick={() => {
                  apply(
                    "添加参考图",
                    buildCreateAndConnectOperations(
                      [reusePrompt.assetId],
                      reusePrompt.targetNodeId,
                      reusePrompt.position,
                      () => t("graph.node.imageAsset"),
                    ),
                  );
                  setReusePrompt(null);
                }}
                className="flex h-10 w-full items-center rounded-xl bg-slate-900 px-3 text-xs font-semibold text-white dark:bg-slate-100 dark:text-slate-900"
              >
                {t("graph.drop.createAndConnect")}
              </button>
              <button
                type="button"
                onClick={() => setReusePrompt(null)}
                className="flex h-10 w-full items-center rounded-xl px-3 text-xs font-semibold text-zinc-500"
              >
                {t("common.cancel")}
              </button>
            </div>
          </div>
        </div>
      ) : null}
      <ConfirmDialog
        open={Boolean(pendingDeleteIds?.length)}
        title={pendingDeleteIds?.length === 1
          ? t("detail.confirm.deleteNodeTitle")
          : t("detail.confirm.deleteSelectedNodesTitle")}
        description={pendingDeleteIds?.length === 1
          ? t("detail.confirm.deleteNode", { title: pendingDeleteTitle })
          : t("detail.confirm.deleteSelectedNodes", { count: pendingDeleteIds?.length ?? 0 })}
        confirmLabel={t("graph.canvas.delete")}
        cancelLabel={t("common.cancel")}
        busy={busy}
        onConfirm={confirmDeleteNodes}
        onClose={() => setPendingDeleteIds(null)}
      />
    </div>
  );
}
