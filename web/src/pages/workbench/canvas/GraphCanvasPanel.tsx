import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Play, Undo2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { api, ApiError } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { GraphChangeSet, GraphNodeCatalog, GraphNodeType, GraphProjection, WorkflowNodeStatus } from "../../../lib/types";
import { getWorkflowKeyboardShortcut, type WorkflowKeyboardShortcut } from "../chrome/shortcuts";
import type { CanvasInteractionMode } from "../chrome/workflowCanvasInteraction";
import type { WorkflowCanvasViewport } from "./canvasState";
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
  defaultGraphNodeConfig,
  graphChangeSetClientRef,
  graphNodeTitleKey,
} from "./graphLayout";

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
): "undo" | null {
  return shortcut === "undo" ? "undo" : null;
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
}: {
  productId: string;
  graph: GraphProjection;
  catalog: GraphNodeCatalog | null;
  selectedNodeIds: string[];
  onSelect: (nodeIds: string[]) => void;
  onGraphChange: (next: GraphProjection) => void;
  onRegisterActions?: (actions: GraphCanvasActions) => void;
  onBindNode?: (nodeId: string) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const graphRef = useRef(graph);
  const catalogRef = useRef(catalog);
  const selectedRef = useRef(selectedNodeIds);
  const clipboardRef = useRef<string[]>([]);
  const [viewport, setViewport] = useState<WorkflowCanvasViewport | null>(null);
  const [compact, setCompact] = useState(compactWorkbench);
  const [mobileMode, setMobileMode] = useState<CanvasInteractionMode>("edit");
  const [reusePrompt, setReusePrompt] = useState<Extract<GraphAssetDropPlan, { kind: "choose_reuse" }> | null>(null);
  graphRef.current = graph;
  catalogRef.current = catalog;
  selectedRef.current = selectedNodeIds;

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") return;
    const media = window.matchMedia("(max-width: 1023px)");
    const update = () => setCompact(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
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

  const nodeStatuses = useMemo(() => {
    const statuses: Record<string, WorkflowNodeStatus> = {};
    for (const run of runsQuery.data?.items ?? []) {
      for (const nodeRun of run.node_runs) {
        if (!nodeRun.node_id) continue;
        if (!statuses[nodeRun.node_id] || run.status === "running") {
          statuses[nodeRun.node_id] = nodeRun.status;
        }
      }
    }
    return statuses;
  }, [runsQuery.data]);
  const runningNodeId = runsQuery.data?.items.find((run) => run.status === "running")?.node_runs
    .find((nodeRun) => nodeRun.status === "running")?.node_id ?? null;

  const apply = useCallback((summary: string, operations: GraphChangeSet["operations"]) => {
    if (!operations.length || applyMutation.isPending) return;
    applyMutation.mutate({
      base_graph_revision: graphRef.current.revision,
      summary,
      operations,
    });
  }, [applyMutation]);

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
    apply("创建节点", [{
      op: "create_node",
      client_ref: graphChangeSetClientRef("node"),
      node_type: nodeType,
      title: t(graphNodeTitleKey(nodeType)),
      position_x: 120 + graphRef.current.nodes.length * 24,
      position_y: 120,
      config: defaultGraphNodeConfig(nodeType),
    }]);
  }, [apply, t]);

  const duplicateSelected = useCallback((nodeIds = selectedRef.current) => {
    const { operations } = buildDuplicateGraphOperations(graphRef.current, nodeIds);
    apply("复制节点", operations);
  }, [apply]);

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
      if (!shortcut || applyMutation.isPending || undoMutation.isPending) return;
      const selected = selectedRef.current;
      if (shortcut === "copy") {
        if (!selected.length) return;
        event.preventDefault();
        clipboardRef.current = selected;
        return;
      }
      if (shortcut === "paste") {
        if (!clipboardRef.current.length) return;
        event.preventDefault();
        duplicateSelected(clipboardRef.current);
        return;
      }
      if (shortcut === "duplicate") {
        if (!selected.length) return;
        event.preventDefault();
        duplicateSelected(selected);
        return;
      }
      if (shortcut === "delete") {
        if (!selected.length) return;
        event.preventDefault();
        apply("删除选区", buildDeleteNodeOperations(selected));
        return;
      }
      if (graphHistoryShortcutAction(shortcut) === "undo") {
        if (!graphRef.current.last_operation_group_id) return;
        event.preventDefault();
        undoMutation.mutate();
      }
    };
    window.addEventListener("keydown", handleShortcut);
    return () => window.removeEventListener("keydown", handleShortcut);
  }, [apply, applyMutation.isPending, duplicateSelected, undoMutation]);

  const error = applyMutation.error ?? runMutation.error ?? undoMutation.error;
  const busy = applyMutation.isPending || runMutation.isPending || undoMutation.isPending;

  return (
    <div
      data-graph-canvas-panel
      className="relative flex h-full min-h-0 flex-col overflow-hidden bg-zinc-50 text-zinc-950 dark:bg-[#080c12] dark:text-slate-100"
    >
      <div className="absolute right-4 top-4 z-10 flex gap-2">
        <button
          type="button"
          disabled={busy || !graph.last_operation_group_id}
          onClick={() => undoMutation.mutate()}
          className="inline-flex items-center gap-1 rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700"
        >
          <Undo2 size={12} />
          {t("graph.canvas.undo")}
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => runMutation.mutate({ scope: "graph" })}
          className="inline-flex items-center gap-1 rounded-full bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white dark:bg-slate-100 dark:text-slate-900"
        >
          <Play size={12} />
          {t("graph.canvas.run")}
        </button>
      </div>
      {error ? (
        <div role="alert" className="absolute left-4 top-4 z-10 max-w-sm rounded bg-red-50 px-3 py-2 text-xs text-red-700">
          {error instanceof ApiError ? error.detail : t("workflowV2.error.structure")}
        </div>
      ) : null}
      <div className="relative min-h-0 flex-1 overflow-hidden">
        <GraphWorkflowCanvas
          graph={graph}
          catalog={catalog}
          selectedNodeIds={selectedNodeIds}
          busy={busy}
          nodeStatuses={nodeStatuses}
          runningNodeId={runningNodeId}
          viewport={viewport}
          onViewportChange={setViewport}
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
          onDeleteNode={(nodeId) => apply("删除节点", [{ op: "delete_node", node_ref: nodeId }])}
          onDeleteEdge={(edgeId) => apply("断开连线", [{ op: "disconnect_edge", edge_ref: edgeId }])}
          onRunNode={(nodeId) => runMutation.mutate({ scope: "node", node_id: nodeId })}
          onBindNode={(nodeId) => onBindNode?.(nodeId)}
          onDuplicateNode={duplicateSelected}
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
    </div>
  );
}
