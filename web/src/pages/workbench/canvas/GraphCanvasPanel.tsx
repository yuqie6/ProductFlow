/**
 * live schema-v3 图的画布命令面。
 *
 * 变更是 Graph Command change set。撤销/重做是服务端 operation group，不是本地历史。
 * `onBeforeRun` 会 flush inspector 草稿。
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, ChevronRight, Loader2, Play, Redo2, Undo2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { api, ApiError } from "../../../lib/api";
import { AGENT_IMAGE_TYPE_TRANSLATIONS } from "../../product-create/imageTypeSelection";
import { useI18n } from "../../../lib/preferences";
import type { AgentProductImageTypeKey, GraphChangeSet, GraphNodeCatalog, GraphNodeType, GraphPlannedAction, GraphProjection, GraphRunListResponse, GraphRunPreviewResponse, GraphRunSubmitInput } from "../../../lib/types";
import { ProductWorkbenchCanvasChromeToggle } from "../chrome/ProductWorkbenchCanvasChromeToggle";
import { getWorkflowKeyboardShortcut, type WorkflowKeyboardShortcut } from "../chrome/shortcuts";
import type { CanvasInteractionMode } from "../chrome/workflowCanvasInteraction";
import {
  isWorkflowCanvasViewportScopeActive,
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
  resolveRecipeSaveRequest,
  type RecipeSaveKind,
  type RecipeSaveRequest,
} from "./recipeSave";
import { WorkflowRecipeDialog } from "./WorkflowDialogs";
import {
  buildDeleteNodeOperations,
  buildDuplicateGraphOperations,
  buildGraphAutoLayoutPositions,
  buildPinImageAssetOperations,
  buildRenameGroupOperations,
  createdGraphNodeIds,
  graphCanvasView,
  graphChangeSetClientRef,
  graphNodeTitleKey,
  graphViewportCenterPosition,
  selectionInsideGroup,
} from "./graphLayout";
import { graphNodeRunPresentations, graphQueuedRuns, graphRunningRuns } from "./graphRunDisplay";
import { applyGraphRunEvent, subscribeGraphRunEvents } from "./graphRunEvents";
import { withGraphRunSubmit } from "./graphRunLock";
import { isMoveNodesOnly } from "./graphChangeSetQueue";
import { graphEdgeRoleLabelKey, graphHasRunnableProcessingNode, missingRequiredRunNodes, missingRunNodesSummary } from "./graphCatalog";
import {
  buildCreateShotOperations,
  shotRunRequest,
} from "./shotChangeSet";
import { GraphShotList } from "./GraphShotList";
import type { LocalImageEditOpenRequest } from "../local-edit/LocalImageEditController";
import { graphHasImageGenerationGroups, projectGraphShots } from "./shotProjection";

export interface GraphCanvasActions {
  createNode: (nodeType: GraphNodeType) => void;
  createShot: (imageTypeKey: AgentProductImageTypeKey) => void;
  duplicateSelected: () => void;
  groupSelected: () => void;
  dissolveSelected: () => void;
  saveRecipe: (kind: RecipeSaveKind) => void;
  appendRecipe: (recipe: {
    id: string;
    version: number;
    title: string;
    description: string | null;
  }) => void;
  commitNode: (input: {
    nodeId: string;
    title?: string;
    config?: Record<string, unknown>;
    boundAssetId?: string | null;
  }) => Promise<GraphProjection | void>;
  pinCurrentOutput: (nodeId: string) => void;
}

export function graphHistoryShortcutAction(
  shortcut: WorkflowKeyboardShortcut,
): "undo" | "redo" | null {
  if (shortcut === "undo" || shortcut === "redo") return shortcut;
  return null;
}

export function GraphCanvasNotice({ notice }: { notice: string | null }) {
  if (!notice) return null;
  return (
    <div
      role="status"
      data-graph-canvas-notice
      className="rounded-lg border border-slate-200 bg-white/95 px-3 py-2 text-xs font-medium text-slate-700 shadow-sm dark:border-slate-700 dark:bg-[#111a2b] dark:text-slate-200"
    >
      {notice}
    </div>
  );
}

function compactWorkbench(): boolean {
  return typeof window !== "undefined"
    && typeof window.matchMedia === "function"
    && window.matchMedia("(max-width: 1023px)").matches;
}

interface PendingGraphApply {
  summary: string;
  operations: GraphChangeSet["operations"];
  resolve: (next: GraphProjection | null) => void;
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
  onOpenLocalEdit,
  chromeCollapsed = false,
  onToggleChrome,
  agentEditing = false,
}: {
  productId: string;
  graph: GraphProjection;
  catalog: GraphNodeCatalog | null;
  selectedNodeIds: string[];
  onSelect: (nodeIds: string[]) => void | Promise<void>;
  onGraphChange: (next: GraphProjection) => void;
  onRegisterActions?: (actions: GraphCanvasActions) => void;
  onBindNode?: (nodeId: string) => void;
  onBusyChange?: (busy: boolean) => void;
  onBeforeRun?: () => Promise<void>;
  onOpenLocalEdit?: (request: LocalImageEditOpenRequest) => void;
  chromeCollapsed?: boolean;
  onToggleChrome?: () => void;
  agentEditing?: boolean;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const graphRef = useRef(graph);
  const catalogRef = useRef(catalog);
  const selectedRef = useRef(selectedNodeIds);
  const clipboardRef = useRef<string[]>([]);
  const commitLiveGraph = useCallback((next: GraphProjection) => {
    void queryClient.cancelQueries({ queryKey: ["workflow-graph", productId] });
    graphRef.current = next;
    onGraphChange(next);
    queryClient.setQueryData(["workflow-graph", productId], next);
  }, [onGraphChange, productId, queryClient]);
  const [enteredGroupId, setEnteredGroupId] = useState<string | null>(null);
  const enteredGroupIdRef = useRef<string | null>(null);
  const [viewport, setViewport] = useState<WorkflowCanvasViewport | null>(
    () => readStoredWorkflowCanvasViewport(graph.id),
  );
  const viewportRef = useRef(viewport);
  const [canvasSyncVersion, setCanvasSyncVersion] = useState(0);
  const applyInFlightRef = useRef(false);
  const pendingApplyRef = useRef<PendingGraphApply | null>(null);
  const applyPumpRef = useRef(false);
  const historyInFlightRef = useRef(false);
  const mutationPreparationRef = useRef(false);
  const [compact, setCompact] = useState(compactWorkbench);
  const [mobileMode, setMobileMode] = useState<CanvasInteractionMode>("edit");
  const [reusePrompt, setReusePrompt] = useState<Extract<GraphAssetDropPlan, { kind: "choose_reuse" }> | null>(null);
  const [pendingDeleteIds, setPendingDeleteIds] = useState<string[] | null>(null);
  const [recipeDialog, setRecipeDialog] = useState<{
    request: RecipeSaveRequest;
    heading: string;
    sourceLabel: string;
    initialTitle: string;
    initialDescription: string | null;
    appendRecipeId?: string;
    expectedRecipeVersion?: number;
  } | null>(null);
  const [recipeError, setRecipeError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [mainView, setMainView] = useState<"shots" | "canvas">(
    () => graphHasImageGenerationGroups(graph) ? "shots" : "canvas",
  );
  const mainViewGraphIdRef = useRef(graph.id);
  const [runningShotGroupId, setRunningShotGroupId] = useState<string | null>(null);
  const runningShotGroupRef = useRef<string | null>(null);
  const [runPreview, setRunPreview] = useState<GraphRunPreviewResponse | null>(null);
  const [runEventsFallback, setRunEventsFallback] = useState(false);
  const previewTimerRef = useRef<number | null>(null);
  const noticeTimerRef = useRef<number | null>(null);
  const runEventCursorRef = useRef<Record<string, number>>({});
  graphRef.current = graph;
  catalogRef.current = catalog;
  selectedRef.current = selectedNodeIds;
  viewportRef.current = viewport;
  enteredGroupIdRef.current = enteredGroupId;

  useEffect(() => {
    setEnteredGroupId(null);
    setViewport(readStoredWorkflowCanvasViewport(graph.id));
  }, [graph.id]);

  useEffect(() => {
    if (previewTimerRef.current) window.clearTimeout(previewTimerRef.current);
    previewTimerRef.current = null;
    setRunPreview(null);
  }, [graph.id, graph.revision]);

  useEffect(() => {
    if (!enteredGroupId) return;
    if (graph.groups.some((group) => group.id === enteredGroupId)) return;
    setEnteredGroupId(null);
    setViewport(readStoredWorkflowCanvasViewport(graph.id));
  }, [enteredGroupId, graph.groups, graph.id]);

  useEffect(() => {
    if (!enteredGroupId) return;
    const inside = selectionInsideGroup(graph, enteredGroupId, selectedNodeIds);
    if (inside.length === selectedNodeIds.length) return;
    setEnteredGroupId(null);
    setViewport(readStoredWorkflowCanvasViewport(graph.id));
  }, [enteredGroupId, graph, selectedNodeIds]);

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
    if (previewTimerRef.current) window.clearTimeout(previewTimerRef.current);
    pendingApplyRef.current?.resolve(null);
    pendingApplyRef.current = null;
  }, []);

  // 409 先刷新 live 图；只有纯位置操作可以在新 revision 上安全重放一次。
  const applyMutation = useMutation({
    mutationFn: (changeSet: GraphChangeSet) => api.applyWorkflowChangeSet(productId, graph.id, changeSet),
    onSuccess: (next) => {
      commitLiveGraph(next);
    },
    onError: () => {
      setCanvasSyncVersion((current) => current + 1);
    },
  });

  const undoMutation = useMutation({
    mutationFn: () => api.undoWorkflowChangeSet(productId, graph.id),
    onSuccess: (next) => {
      commitLiveGraph(next);
    },
    onError: async (error) => {
      setCanvasSyncVersion((current) => current + 1);
      if (error instanceof ApiError && error.status === 409) {
        const next = await api.getWorkflowGraph(productId, graph.id);
        commitLiveGraph(next);
      }
    },
  });

  const redoMutation = useMutation({
    mutationFn: () => api.redoWorkflowChangeSet(productId, graph.id),
    onSuccess: (next) => {
      commitLiveGraph(next);
    },
    onError: async (error) => {
      setCanvasSyncVersion((current) => current + 1);
      if (error instanceof ApiError && error.status === 409) {
        const next = await api.getWorkflowGraph(productId, graph.id);
        commitLiveGraph(next);
      }
    },
  });

  const runMutation = useMutation({
    mutationFn: (input: GraphRunSubmitInput) =>
      api.submitGraphRun(productId, graph.id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["graph-runs", productId, graph.id] });
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
    },
  });

  const recipeMutation = useMutation({
    mutationFn: (input: {
      request: RecipeSaveRequest;
      title: string;
      description: string | null;
      appendRecipeId?: string;
      expectedRecipeVersion?: number;
    }) => {
      const body = {
        source_type: input.request.source_type,
        group_id: input.request.group_id,
        node_ids: input.request.node_ids,
        expected_graph_revision: input.request.expected_graph_revision,
        title: input.title,
        description: input.description,
      };
      if (input.appendRecipeId && input.expectedRecipeVersion != null) {
        return api.appendWorkflowRecipeVersion(productId, graph.id, input.appendRecipeId, {
          ...body,
          expected_recipe_version: input.expectedRecipeVersion,
        });
      }
      return api.createWorkflowRecipe(productId, graph.id, body);
    },
    onSuccess: () => {
      setRecipeDialog(null);
      setRecipeError(null);
      void queryClient.invalidateQueries({ queryKey: ["workflow-recipes"] });
      showNotice(t("workbench.recipe.saved"));
    },
    onError: (error) => {
      setRecipeError(error instanceof ApiError ? error.detail : t("workbench.error.recipe"));
    },
  });

  const runsQuery = useQuery({
    queryKey: ["graph-runs", productId, graph.id],
    queryFn: () => api.listGraphRuns(productId, graph.id),
    refetchInterval: runEventsFallback ? 1200 : false,
  });
  const cancelRunMutation = useMutation({
    mutationFn: (runId: string) => api.cancelGraphRun(productId, graph.id, runId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["graph-runs", productId, graph.id] });
    },
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
  const shotProjections = useMemo(
    () => projectGraphShots(graph, runsQuery.data?.items ?? []),
    [graph, runsQuery.data?.items],
  );
  const hasShotGroups = shotProjections.length > 0;
  const queuedRuns = graphQueuedRuns(runsQuery.data?.items);
  const runningRuns = graphRunningRuns(runsQuery.data?.items);
  const liveRun = runningRuns[0] ?? queuedRuns[0] ?? null;
  const liveRunId = liveRun?.id ?? null;
  const missingRunNodes = useMemo(
    () => missingRequiredRunNodes(graph, catalog),
    [catalog, graph],
  );
  const graphRunBlocked = !graphHasRunnableProcessingNode(graph, catalog);
  const graphRunBlockedReason = useMemo(() => {
    if (!graphRunBlocked) return undefined;
    const missing = missingRunNodesSummary(missingRunNodes, (role) => {
      const key = graphEdgeRoleLabelKey(role);
      return t("graph.missingRunInput", { role: key ? t(key) : role });
    });
    return missing || t("graph.runs.noRunnableNodes");
  }, [graphRunBlocked, missingRunNodes, t]);
  const blockedShotReasons = useMemo(() => {
    const reasons: Record<string, string> = {};
    for (const group of graph.groups) {
      const missing = missingRequiredRunNodes(graph, catalog, new Set(group.member_ids));
      if (!missing.length) continue;
      reasons[group.id] = missingRunNodesSummary(missing, (role) => {
        const key = graphEdgeRoleLabelKey(role);
        return t("graph.missingRunInput", { role: key ? t(key) : role });
      });
    }
    return reasons;
  }, [catalog, graph, t]);
  const plannedActions = useMemo(() => {
    const next: Record<string, GraphPlannedAction> = {};
    for (const node of runPreview?.nodes ?? []) next[node.node_id] = node.planned_action;
    return next;
  }, [runPreview]);
  useEffect(() => {
    if (!liveRunId) {
      setRunEventsFallback(false);
      return;
    }
    return subscribeGraphRunEvents(api.graphRunEventsUrl(productId, graph.id, liveRunId), (event) => {
      runEventCursorRef.current[event.run_id] = event.sequence;
      queryClient.setQueryData<GraphRunListResponse | undefined>(["graph-runs", productId, graph.id], (previous) => applyGraphRunEvent(previous, event));
      if (event.kind === "run.started"
        || event.kind === "run.completed"
        || event.kind === "run.failed"
        || event.kind === "run.cancelled"
        || event.kind === "run.unknown") {
        // A queued run has no node_runs until activation. Terminal refreshes
        // also pick up attempt_count, retryability, and a promoted queue item.
        void queryClient.invalidateQueries({ queryKey: ["graph-runs", productId, graph.id] });
      }
      if (event.kind === "node.succeeded" || event.kind === "node.skipped" || event.kind === "run.completed") {
        void queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
        void queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] });
        void queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] });
      }
    }, {
      after: runEventCursorRef.current[liveRunId] ?? 0,
      onOpen: () => setRunEventsFallback(false),
      onError: () => {
        setRunEventsFallback(true);
        showNotice(t("graph.runs.liveConnectionFailed"));
      },
    });
  }, [graph.id, liveRunId, productId, queryClient, showNotice, t]);

  useEffect(() => {
    if (mainViewGraphIdRef.current !== graph.id) {
      mainViewGraphIdRef.current = graph.id;
      setMainView(hasShotGroups && !graph.pending_proposal ? "shots" : "canvas");
      return;
    }
    if (graph.pending_proposal) {
      setMainView("canvas");
      return;
    }
    if (!hasShotGroups) setMainView("canvas");
  }, [graph.id, graph.pending_proposal, hasShotGroups]);

  const executeApply = useCallback(async (summary: string, operations: GraphChangeSet["operations"]) => {
    mutationPreparationRef.current = true;
    try {
      try {
        await onBeforeRun?.();
      } catch {
        setCanvasSyncVersion((current) => current + 1);
        return null;
      }
      if (historyInFlightRef.current) {
        setCanvasSyncVersion((current) => current + 1);
        return null;
      }
      applyInFlightRef.current = true;
      const changeSet = () => ({
        base_graph_revision: graphRef.current.revision,
        summary,
        operations,
      });
      try {
        return await applyMutation.mutateAsync(changeSet());
      } catch (error) {
        if (!(error instanceof ApiError) || error.status !== 409) {
          throw error;
        }
        const latest = await api.getWorkflowGraph(productId, graph.id);
        commitLiveGraph(latest);
        if (!isMoveNodesOnly(operations)) {
          showNotice(t("graph.canvas.revisionConflict"));
          setCanvasSyncVersion((current) => current + 1);
          return null;
        }
        return await applyMutation.mutateAsync({
          ...changeSet(),
          base_graph_revision: latest.revision,
        });
      }
    } catch {
      setCanvasSyncVersion((current) => current + 1);
      return null;
    } finally {
      applyInFlightRef.current = false;
      mutationPreparationRef.current = false;
    }
  }, [applyMutation, commitLiveGraph, graph.id, onBeforeRun, showNotice, t]);

  const pumpApplyQueue = useCallback(() => {
    if (applyPumpRef.current) return;
    applyPumpRef.current = true;
    void (async () => {
      try {
        while (pendingApplyRef.current) {
          const pending = pendingApplyRef.current;
          pendingApplyRef.current = null;
          const next = await executeApply(pending.summary, pending.operations);
          pending.resolve(next);
        }
      } finally {
        applyPumpRef.current = false;
        if (pendingApplyRef.current) pumpApplyQueue();
      }
    })();
  }, [executeApply]);

  const applyAsync = useCallback((summary: string, operations: GraphChangeSet["operations"]) => {
    if (!operations.length) return Promise.resolve(null);
    if (applyInFlightRef.current || historyInFlightRef.current || mutationPreparationRef.current || applyPumpRef.current) {
      if (pendingApplyRef.current) {
        showNotice(t("graph.canvas.changeConflict"));
        return Promise.resolve(null);
      }
      const queued = new Promise<GraphProjection | null>((resolve) => {
        pendingApplyRef.current = { summary, operations, resolve };
      });
      showNotice(t("graph.canvas.changeQueued"));
      if (!applyInFlightRef.current && !historyInFlightRef.current && !mutationPreparationRef.current) {
        pumpApplyQueue();
      }
      return queued;
    }
    return executeApply(summary, operations).finally(pumpApplyQueue);
  }, [executeApply, pumpApplyQueue, showNotice, t]);

  const apply = useCallback((summary: string, operations: GraphChangeSet["operations"]) => {
    void applyAsync(summary, operations);
  }, [applyAsync]);

  const runHistoryMutation = useCallback(async (action: "undo" | "redo") => {
    if (
      applyInFlightRef.current
      || historyInFlightRef.current
      || mutationPreparationRef.current
      || undoMutation.isPending
      || redoMutation.isPending
    ) {
      return;
    }
    mutationPreparationRef.current = true;
    try {
      try {
        await onBeforeRun?.();
      } catch {
        return;
      }
      if (applyInFlightRef.current || applyMutation.isPending) return;
      historyInFlightRef.current = true;
      if (action === "undo") {
        await undoMutation.mutateAsync();
      } else {
        await redoMutation.mutateAsync();
      }
    } catch {
      return;
    } finally {
      historyInFlightRef.current = false;
      mutationPreparationRef.current = false;
      pumpApplyQueue();
    }
  }, [applyMutation, onBeforeRun, pumpApplyQueue, redoMutation, undoMutation]);

  const selectCreatedNodes = useCallback((before: GraphProjection, after: GraphProjection) => {
    const created = createdGraphNodeIds(before, after);
    if (created.length) onSelect(created);
    return created;
  }, [onSelect]);

  const handleAssetDrop = useCallback((input: Parameters<typeof resolveGraphAssetDrop>[1]) => {
    const plan = resolveGraphAssetDrop(graphRef.current, input, catalogRef.current);
    if (plan.kind === "ignored") return;
    if (plan.kind === "rejected") {
      showNotice(t(plan.reasonKey));
      return;
    }
    if (plan.kind === "choose_reuse") {
      setReusePrompt(plan);
      return;
    }
    apply(plan.summary, plan.operations);
  }, [apply, showNotice, t]);

  const createNode = useCallback((nodeType: GraphNodeType) => {
    const before = graphRef.current;
    const position = graphViewportCenterPosition(viewportRef.current);
    const groupRef = enteredGroupIdRef.current;
    void applyAsync("创建节点", [{
      op: "create_node",
      client_ref: graphChangeSetClientRef("node"),
      node_type: nodeType,
      title: t(graphNodeTitleKey(nodeType)),
      position_x: position.position_x,
      position_y: position.position_y,
      config: {},
      ...(groupRef ? { group_ref: groupRef } : {}),
    }]).then((next) => {
      if (next) selectCreatedNodes(before, next);
    });
  }, [applyAsync, selectCreatedNodes, t]);

  const createShot = useCallback((imageTypeKey: AgentProductImageTypeKey) => {
    const before = graphRef.current;
    const position = graphViewportCenterPosition(viewportRef.current);
    const translations = AGENT_IMAGE_TYPE_TRANSLATIONS[imageTypeKey];
    const operations = buildCreateShotOperations({
      imageTypeKey,
      title: translations ? t(translations.title) : imageTypeKey,
      position: { x: position.position_x, y: position.position_y },
      graph: before,
      selectedNodeIds: selectedRef.current,
    });
    if (!operations.length) return;
    void applyAsync("添加场景", operations).then((next) => {
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

  const enterGroup = useCallback((groupId: string) => {
    void (async () => {
      if (!graphRef.current.groups.some((group) => group.id === groupId)) return;
      try {
        await onBeforeRun?.();
      } catch {
        return;
      }
      const liveGraph = graphRef.current;
      const inside = selectionInsideGroup(liveGraph, groupId, selectedRef.current);
      if (inside.length !== selectedRef.current.length) {
        try {
          await onSelect(inside);
        } catch {
          return;
        }
      }
      if (!graphRef.current.groups.some((group) => group.id === groupId)) return;
      setEnteredGroupId(groupId);
      setViewport(readStoredWorkflowCanvasViewport(graphRef.current.id, groupId));
    })();
  }, [onBeforeRun, onSelect]);

  const exitGroup = useCallback(() => {
    void (async () => {
      if (!enteredGroupIdRef.current) return;
      try {
        await onBeforeRun?.();
      } catch {
        return;
      }
      setEnteredGroupId(null);
      setViewport(readStoredWorkflowCanvasViewport(graphRef.current.id));
    })();
  }, [onBeforeRun]);

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
    if (applyInFlightRef.current || historyInFlightRef.current || applyMutation.isPending) {
      throw new Error("图正在保存，请稍后重试");
    }
    applyInFlightRef.current = true;
    try {
      return await applyMutation.mutateAsync({
        base_graph_revision: graphRef.current.revision,
        summary: "更新节点",
        operations,
      });
    } finally {
      applyInFlightRef.current = false;
      pumpApplyQueue();
    }
  }, [applyMutation, pumpApplyQueue]);

  const pinCurrentOutput = useCallback((nodeId: string) => {
    const before = graphRef.current;
    const operations = buildPinImageAssetOperations(before, nodeId, t("graph.node.imageAsset"));
    if (!operations.length) return;
    void applyAsync("固定为图片素材", operations).then((next) => {
      if (next) selectCreatedNodes(before, next);
    });
  }, [applyAsync, selectCreatedNodes, t]);

  const submitRun = useCallback(async (input: GraphRunSubmitInput) => {
    await withGraphRunSubmit(async () => {
      try {
        await onBeforeRun?.();
      } catch {
        return;
      }
      await runMutation.mutateAsync(input);
    });
  }, [onBeforeRun, runMutation]);

  const runShot = useCallback(async (groupId: string) => {
    const request = shotRunRequest(graphRef.current, groupId);
    if (!request) return;
    await submitRun(request);
    void queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] });
    void queryClient.invalidateQueries({ queryKey: ["graph-runs", productId, graphRef.current.id] });
    void queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] });
    void queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] });
  }, [productId, queryClient, submitRun]);

  const showGraphRunPreview = useCallback(() => {
    if (previewTimerRef.current) window.clearTimeout(previewTimerRef.current);
    const previewGraphId = graphRef.current.id;
    const previewRevision = graphRef.current.revision;
    previewTimerRef.current = window.setTimeout(() => {
      void api.previewGraphRun(productId, previewGraphId, { scope: "graph" })
        .then((preview) => {
          const current = graphRef.current;
          if (current.id !== previewGraphId || current.revision !== previewRevision) return;
          setRunPreview(preview);
        })
        .catch(() => undefined);
    }, 160);
  }, [productId]);
  const hideGraphRunPreview = useCallback(() => {
    if (previewTimerRef.current) window.clearTimeout(previewTimerRef.current);
    previewTimerRef.current = null;
    setRunPreview(null);
  }, []);

  const runShotWithBusy = useCallback(async (groupId: string) => {
    if (runningShotGroupRef.current !== null) return;
    runningShotGroupRef.current = groupId;
    setRunningShotGroupId(groupId);
    try {
      await runShot(groupId);
    } finally {
      if (runningShotGroupRef.current === groupId) {
        runningShotGroupRef.current = null;
        setRunningShotGroupId(null);
      }
    }
  }, [runShot]);

  const handleShotRun = useCallback((groupId: string) => {
    void runShotWithBusy(groupId).catch((runError: unknown) => {
      showNotice(runError instanceof ApiError && runError.detail ? runError.detail : t("workbench.error.run"));
    });
  }, [runShotWithBusy, showNotice, t]);

  const openShotNode = useCallback((nodeId: string) => {
    void (async () => {
      try {
        await onSelect([nodeId]);
      } catch {
        return;
      }
    })();
  }, [onSelect]);

  const handleViewportChange = useCallback((next: WorkflowCanvasViewport, groupId: string | null) => {
    if (!isWorkflowCanvasViewportScopeActive(enteredGroupIdRef.current, groupId)) return;
    setViewport(next);
    writeStoredWorkflowCanvasViewport(graph.id, next, groupId);
  }, [graph.id]);

  const openRecipeSave = useCallback((
    kind: RecipeSaveKind,
    explicitNodeIds?: string[],
    append?: { id: string; version: number; title: string; description: string | null },
  ) => {
    void (async () => {
      try {
        await onBeforeRun?.();
      } catch {
        return;
      }
      const live = graphRef.current;
      const request = resolveRecipeSaveRequest(
        live,
        selectedRef.current,
        enteredGroupIdRef.current,
        kind,
        explicitNodeIds,
      );
      if (!request) {
        showNotice(t("workbench.recipe.saveUnavailable"));
        return;
      }
      const groupTitle = request.group_id
        ? live.groups.find((group) => group.id === request.group_id)?.title ?? ""
        : "";
      const heading = append
        ? t("workbench.recipe.append")
        : kind === "workflow"
          ? t("workbench.recipe.saveFull")
          : kind === "group"
            ? t("workbench.recipe.saveGroup")
            : t("workbench.recipe.saveSelection");
      const sourceLabel = kind === "workflow"
        ? t("workbench.recipe.sourceFull")
        : kind === "group"
          ? t("workbench.recipe.sourceGroup", { title: groupTitle })
          : t("workbench.recipe.sourceSelection", { count: request.node_ids?.length ?? 0 });
      setRecipeError(null);
      setRecipeDialog({
        request,
        heading,
        sourceLabel,
        initialTitle: append?.title ?? live.title,
        initialDescription: append?.description ?? null,
        appendRecipeId: append?.id,
        expectedRecipeVersion: append?.version,
      });
    })();
  }, [onBeforeRun, showNotice, t]);

  const actionsRef = useRef<GraphCanvasActions>({
    createNode,
    createShot,
    duplicateSelected,
    groupSelected,
    dissolveSelected,
    saveRecipe: (kind) => openRecipeSave(kind),
    appendRecipe: (recipe) => openRecipeSave("workflow", undefined, recipe),
    commitNode,
    pinCurrentOutput,
  });
  actionsRef.current = {
    createNode,
    createShot,
    duplicateSelected,
    groupSelected,
    dissolveSelected,
    saveRecipe: (kind) => openRecipeSave(kind),
    appendRecipe: (recipe) => openRecipeSave("workflow", undefined, recipe),
    commitNode,
    pinCurrentOutput,
  };
  useEffect(() => {
    onRegisterActions?.({
      createNode: (nodeType) => actionsRef.current.createNode(nodeType),
      createShot: (imageTypeKey) => actionsRef.current.createShot(imageTypeKey),
      duplicateSelected: () => actionsRef.current.duplicateSelected(),
      groupSelected: () => actionsRef.current.groupSelected(),
      dissolveSelected: () => actionsRef.current.dissolveSelected(),
      saveRecipe: (kind) => actionsRef.current.saveRecipe(kind),
      appendRecipe: (recipe) => actionsRef.current.appendRecipe(recipe),
      commitNode: (input) => actionsRef.current.commitNode(input),
      pinCurrentOutput: (nodeId) => actionsRef.current.pinCurrentOutput(nodeId),
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
        void runHistoryMutation("undo");
        return;
      }
      if (graphHistoryShortcutAction(shortcut) === "redo") {
        if (!graphRef.current.can_redo) return;
        event.preventDefault();
        void runHistoryMutation("redo");
      }
    };
    window.addEventListener("keydown", handleShortcut);
    return () => window.removeEventListener("keydown", handleShortcut);
  }, [applyMutation.isPending, duplicateSelected, requestDeleteNodes, runHistoryMutation, showNotice, t]);

  const proposalMutation = useMutation({
    mutationFn: async (kind: "confirm" | "discard") => {
      const proposal = graph.pending_proposal;
      if (!proposal) throw new Error(t("graph.proposal.missing"));
      return kind === "confirm"
        ? api.confirmGraphProposal(productId, graph.id, proposal.id)
        : api.discardGraphProposal(productId, graph.id, proposal.id);
    },
    onSuccess: (next) => {
      onGraphChange(next);
    },
  });
  const error = applyMutation.error ?? runMutation.error ?? undoMutation.error ?? redoMutation.error ?? proposalMutation.error;
  const structureBusy = applyMutation.isPending || undoMutation.isPending || redoMutation.isPending || proposalMutation.isPending;
  const runBusy = runMutation.isPending;
  const busy = structureBusy || runBusy;
  const runControlsBusy = structureBusy || runningShotGroupId !== null;

  useEffect(() => {
    onBusyChange?.(structureBusy);
  }, [onBusyChange, structureBusy]);
  useEffect(() => () => onBusyChange?.(false), [onBusyChange]);

  const pendingDeleteTitle = pendingDeleteIds?.length === 1
    ? graph.nodes.find((node) => node.id === pendingDeleteIds[0])?.title ?? ""
    : "";
  const enteredGroup = enteredGroupId
    ? graph.groups.find((group) => group.id === enteredGroupId) ?? null
    : null;

  return (
    <div
      data-graph-canvas-panel
      className="relative flex h-full min-h-0 flex-col overflow-hidden bg-zinc-50 text-zinc-950 dark:bg-[#080c12] dark:text-slate-100"
    >
      <div className="flex shrink-0 items-center justify-between gap-3 border-b border-border-l1 bg-surface-raised px-3 py-2 sm:px-4">
        <div
          role="tablist"
          aria-label={t("graph.canvas.ariaLabel")}
          data-graph-view-switcher
          className="inline-flex min-h-9 max-w-full items-center rounded-lg border border-border-l1 bg-surface-subtle p-0.5"
        >
          <button
            type="button"
            role="tab"
            data-graph-view="shots"
            aria-selected={mainView === "shots"}
            disabled={!hasShotGroups}
            onClick={() => setMainView("shots")}
            className="min-h-8 rounded-md px-3 text-xs font-semibold text-text-secondary hover:text-text-primary aria-selected:bg-surface-raised aria-selected:text-text-primary disabled:cursor-not-allowed disabled:opacity-45 sm:px-4"
          >
            {t("graph.canvas.shotsView")}
          </button>
          <button
            type="button"
            role="tab"
            data-graph-view="canvas"
            aria-selected={mainView === "canvas"}
            onClick={() => setMainView("canvas")}
            className="min-h-8 rounded-md px-3 text-xs font-semibold text-text-secondary hover:text-text-primary aria-selected:bg-surface-raised aria-selected:text-text-primary sm:px-4"
          >
            {t("agentWorkbench.canvas")}
          </button>
        </div>
        <div className="flex min-w-0 items-center gap-2">
          {agentEditing ? (
            <span
              role="status"
              data-agent-canvas-presence
              className="inline-flex min-h-8 min-w-0 items-center gap-1.5 rounded-md border border-accent/30 bg-accent-soft px-2 text-[11px] font-medium text-accent-strong"
            >
              <Bot size={13} className="shrink-0" aria-hidden="true" />
              <span className="truncate">{t("graph.canvas.agentEditing")}</span>
            </span>
          ) : null}
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
      </div>
      {mainView === "canvas" ? <div
        className={`absolute z-20 flex items-center gap-1 rounded-xl border border-border-l1 bg-surface-raised/95 p-1 shadow-sm backdrop-blur ${compact ? "right-3 top-[4.75rem]" : "right-4 top-16"
          }`}
      >
        <button
          type="button"
          disabled={structureBusy || !graph.can_undo}
          onClick={() => void runHistoryMutation("undo")}
          className="inline-flex h-11 w-11 items-center justify-center rounded-lg text-text-secondary hover:bg-surface-subtle hover:text-text-primary disabled:opacity-45 lg:h-9 lg:w-9"
          aria-label={t("graph.canvas.undo")}
          title={t("graph.canvas.undo")}
        >
          <Undo2 size={16} aria-hidden="true" />
        </button>
        <button
          type="button"
          disabled={structureBusy || !graph.can_redo}
          onClick={() => void runHistoryMutation("redo")}
          className="inline-flex h-11 w-11 items-center justify-center rounded-lg text-text-secondary hover:bg-surface-subtle hover:text-text-primary disabled:opacity-45 lg:h-9 lg:w-9"
          aria-label={t("graph.canvas.redo")}
          title={t("graph.canvas.redo")}
        >
          <Redo2 size={16} aria-hidden="true" />
        </button>
        <button
          type="button"
          data-graph-run-all
          disabled={structureBusy || graphRunBlocked}
          onClick={() => {
            hideGraphRunPreview();
            void submitRun({ scope: "graph" }).catch(() => undefined);
          }}
          onMouseEnter={showGraphRunPreview}
          onMouseLeave={hideGraphRunPreview}
          onFocus={showGraphRunPreview}
          onBlur={hideGraphRunPreview}
          className="inline-flex h-11 w-11 items-center justify-center rounded-lg bg-accent text-accent-fg hover:bg-accent-strong disabled:opacity-45 lg:h-9 lg:w-9"
          aria-label={t("graph.canvas.run")}
          title={graphRunBlocked ? graphRunBlockedReason : t("graph.canvas.run")}
        >
          {runBusy ? <Loader2 size={16} className="animate-spin" aria-hidden="true" /> : <Play size={16} aria-hidden="true" />}
        </button>
        {runningRuns.length || queuedRuns.length ? (
          <div
            data-graph-run-queue
            className="ml-1 flex max-w-[14rem] items-center gap-1 rounded-lg border border-border-l1 bg-surface-subtle px-2 py-1 text-[10px] font-semibold text-text-secondary"
          >
            <span>
              {t("graph.runs.queue", { running: runningRuns.length, queued: queuedRuns.length })}
            </span>
            {queuedRuns.map((run) => (
              <button
                key={run.id}
                type="button"
                disabled={cancelRunMutation.isPending}
                onClick={() => cancelRunMutation.mutate(run.id)}
                className="rounded px-1 text-[10px] text-red-700 hover:bg-red-50 dark:text-red-300"
                title={t("graph.runs.cancelQueued")}
              >
                {t("graph.runs.cancelQueued")}
              </button>
            ))}
          </div>
        ) : null}
      </div> : null}
      {graph.pending_proposal ? (
        <div
          data-graph-proposal-banner
          className={`absolute z-20 ${compact ? "left-3 right-3 top-[8.25rem]" : "left-4 top-16 max-w-md"}`}
        >
          <div className="rounded-xl border border-accent/30 bg-surface-raised/95 p-3 text-xs shadow-sm">
            <div className="font-semibold text-accent">{t("graph.proposal.title")}</div>
            <p className="mt-1 leading-5 text-text-secondary">
              {graph.pending_proposal.stale ? t("graph.proposal.stale") : graph.pending_proposal.summary}
            </p>
            <div className="mt-2 flex gap-2">
              <button
                type="button"
                disabled={busy || graph.pending_proposal.stale}
                onClick={() => {
                  void proposalMutation.mutateAsync("confirm").catch(() => undefined);
                }}
                className="inline-flex h-8 items-center rounded-lg bg-accent px-3 text-[11px] font-semibold text-accent-fg disabled:opacity-45"
              >
                {t("graph.proposal.confirm")}
              </button>
              <button
                type="button"
                disabled={busy}
                onClick={() => {
                  void proposalMutation.mutateAsync("discard").catch(() => undefined);
                }}
                className="inline-flex h-8 items-center rounded-lg border border-border-l1 px-3 text-[11px] font-semibold text-text-primary disabled:opacity-45"
              >
                {t("graph.proposal.discard")}
              </button>
            </div>
          </div>
        </div>
      ) : null}
      {mainView === "canvas" && (enteredGroup || error || notice) ? (
        <div
          className={`absolute z-20 flex flex-col gap-2 ${compact ? "left-3 right-[16.5rem] top-[4.75rem]" : "left-4 top-16 max-w-sm"
            }`}
        >
          {enteredGroup ? (
            <nav
              data-graph-group-breadcrumb
              aria-label={t("workbench.breadcrumb")}
              className="flex min-w-0 items-center gap-1 rounded-xl border border-border-l1 bg-surface-raised/95 px-2 py-1 text-xs shadow-sm backdrop-blur"
            >
              <button
                type="button"
                onClick={exitGroup}
                className="min-h-9 shrink-0 rounded-lg px-2 font-semibold text-text-secondary hover:bg-surface-subtle hover:text-text-primary lg:min-h-7"
                aria-label={t("workbench.canvas.back")}
                title={t("workbench.canvas.back")}
              >
                {t("workbench.breadcrumb")}
              </button>
              <ChevronRight size={12} className="shrink-0 text-text-muted" aria-hidden="true" />
              <span className="min-w-0 truncate px-1 font-semibold text-text-primary">{enteredGroup.title}</span>
            </nav>
          ) : null}
          {error ? (
            <div role="alert" className="rounded bg-red-50 px-3 py-2 text-xs text-red-700">
              {error instanceof ApiError ? error.detail : t("workbench.error.structure")}
            </div>
          ) : (
            <GraphCanvasNotice notice={notice} />
          )}
        </div>
      ) : null}
      <div className="relative min-h-0 flex-1 overflow-hidden">
        <div
          className={`absolute inset-0 min-h-0 overflow-hidden ${mainView === "canvas" ? "" : "pointer-events-none invisible"}`}
          aria-hidden={mainView !== "canvas"}
        >
          <GraphWorkflowCanvas
            graph={graph}
            catalog={catalog}
            selectedNodeIds={selectedNodeIds}
            busy={structureBusy}
            runDisabled={runControlsBusy}
            nodeStatuses={nodeStatuses}
            nodePresentations={nodePresentations}
            plannedActions={plannedActions}
            runningNodeId={runningNodeId}
            canvasSyncVersion={canvasSyncVersion}
            viewport={viewport}
            onViewportChange={handleViewportChange}
            compact={compact}
            enteredGroupId={enteredGroupId}
            mobileInteractionMode={mobileMode}
            onMobileInteractionModeChange={setMobileMode}
            onSelect={onSelect}
            onConnect={(source, target) => apply("连接节点", [{
              op: "connect_nodes",
              client_ref: graphChangeSetClientRef("edge"),
              source_ref: source,
              target_ref: target,
            }])}
            onReconnect={(edgeId, source, target, order) => apply("重连", [
              { op: "disconnect_edge", edge_ref: edgeId },
              {
                op: "connect_nodes",
                client_ref: graphChangeSetClientRef("edge"),
                source_ref: source,
                target_ref: target,
                order,
              },
            ])}
            onConnectionRejected={(reasonKey) => {
              if (reasonKey) showNotice(t(reasonKey));
            }}
            onGroupSelected={groupSelected}
            onDeleteSelected={() => requestDeleteNodes(selectedRef.current)}
            onSaveSelection={() => openRecipeSave("selection")}
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
            onRunNode={(nodeId) => {
              void submitRun({ scope: "node", node_id: nodeId }).catch(() => undefined);
            }}
            onRunToNode={(nodeId) => {
              void submitRun({ scope: "to_node", node_id: nodeId }).catch(() => undefined);
            }}
            onRunShot={handleShotRun}
            onBindNode={(nodeId) => onBindNode?.(nodeId)}
            onPinNode={pinCurrentOutput}
            onDuplicateNode={(nodeIds) => duplicateSelected(nodeIds, "duplicated")}
            onSaveRecipeNode={(nodeId) => openRecipeSave("selection", [nodeId])}
            onAssetDrop={handleAssetDrop}
            onRenameGroup={renameGroup}
            onDissolveGroup={dissolveGroup}
            onEnterGroup={enterGroup}
            onAutoLayout={() => {
              const positions = buildGraphAutoLayoutPositions(graphCanvasView(graph, enteredGroupId));
              if (!positions.length) return;
              apply("自动布局", [{
                op: "move_nodes",
                nodes: positions.map((item) => [item.node_id, item.position_x, item.position_y]),
              }]);
            }}
          />
        </div>
        <div
          className={`absolute inset-0 min-h-0 overflow-hidden ${mainView === "shots" ? "" : "pointer-events-none invisible"}`}
          aria-hidden={mainView !== "shots"}
        >
          <GraphShotList
            shots={shotProjections}
            runsLoading={runsQuery.isLoading}
            runsFetching={runsQuery.isFetching}
            runsError={runsQuery.error}
            operationError={error}
            notice={notice}
            onRetryRuns={() => void runsQuery.refetch()}
            busy={runControlsBusy}
            runningGroupId={runningShotGroupId}
            onOpenNode={openShotNode}
            onOpenLocalEdit={onOpenLocalEdit}
            onRunShot={handleShotRun}
            onRunAll={() => {
              void submitRun({ scope: "graph" }).catch(() => undefined);
            }}
            runAllDisabled={graphRunBlocked}
            blockedReasons={blockedShotReasons}
            runAllBlockedReason={graphRunBlockedReason}
          />
        </div>
      </div>
      {reusePrompt ? (
        <div className="absolute inset-0 z-30 flex items-center justify-center bg-slate-950/40 p-4">
          <div role="dialog" aria-modal="true" className="w-full max-w-sm rounded-2xl bg-white p-4 shadow-xl dark:bg-[#0f1726]">
            <h3 className="text-sm font-semibold text-text-primary">{t("graph.drop.reuseTitle")}</h3>
            <p className="mt-1 text-xs leading-5 text-text-muted">{t("graph.drop.reuseDescription")}</p>
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
                      enteredGroupId,
                    ),
                  );
                  setReusePrompt(null);
                }}
                className="flex h-10 w-full items-center rounded-xl bg-slate-900 px-3 text-xs font-semibold text-white "
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
      <WorkflowRecipeDialog
        open={Boolean(recipeDialog)}
        heading={recipeDialog?.heading ?? ""}
        sourceLabel={recipeDialog?.sourceLabel ?? ""}
        initialTitle={recipeDialog?.initialTitle}
        initialDescription={recipeDialog?.initialDescription}
        busy={recipeMutation.isPending}
        error={recipeError}
        onClose={() => {
          if (recipeMutation.isPending) return;
          setRecipeDialog(null);
          setRecipeError(null);
        }}
        onSubmit={(title, description) => {
          if (!recipeDialog) return;
          recipeMutation.mutate({
            request: recipeDialog.request,
            title,
            description,
            appendRecipeId: recipeDialog.appendRecipeId,
            expectedRecipeVersion: recipeDialog.expectedRecipeVersion,
          });
        }}
      />
    </div>
  );
}
