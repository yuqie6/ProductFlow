import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleAlert, Hand, Loader2, MousePointer2, Move } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { ConfirmDialog } from "../../components/ConfirmDialog";
import { api, ApiError } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import type {
  ActiveProductWorkflowV2,
  CreateReferenceWorkflowNodeV2Input,
  ProductWorkflowV2,
  WorkflowCanvasMutationResult,
  WorkflowEdgeV2,
  WorkflowNodeV2,
  WorkflowRunListV2Response,
} from "../../lib/types";
import { ProductWorkbenchCanvasChromeToggle } from "../product-detail/ProductWorkbenchCanvasChromeToggle";
import {
  WorkflowCanvasMobileModeTabs,
  type WorkflowCanvasMobileModeItem,
} from "../product-detail/WorkflowCanvasChrome";
import type { CanvasInteractionMode } from "../product-detail/types";
import { getWorkflowKeyboardShortcut } from "../product-detail/shortcuts";
import {
  emptyWorkflowCanvasState,
  parseWorkflowCanvasState,
  reconcileWorkflowCanvasState,
  workflowCanvasStateStorageKey,
  type WorkflowCanvasStateV1,
  type WorkflowCanvasViewport,
} from "./canvasState";
import { visibleRealNodeIds } from "./graph";
import {
  V2WorkflowCanvas,
  type WorkflowRevealVisibility,
} from "./V2WorkflowCanvas";
import type { RecipeSourceSelection } from "./recipeSource";
import { V2WorkflowCommandBar } from "./V2WorkflowCommandBar";
import {
  EMPTY_V2_WORKFLOW_HISTORY,
  captureV2WorkflowPositions,
  findAddedV2RootNodeId,
  findV2WorkflowEdgeId,
  shouldClearV2WorkflowHistory,
  type V2WorkflowHistoryAction,
} from "./v2WorkflowHistory";
import { WorkflowReferenceNodeDialog, WorkflowTextDialog } from "./WorkflowDialogs";

type CanvasStateUpdate = WorkflowCanvasStateV1 | ((current: WorkflowCanvasStateV1) => WorkflowCanvasStateV1);
type StructureOperation = (expectedEditVersion: number) => Promise<WorkflowCanvasMutationResult>;
const COMPACT_WORKFLOW_CANVAS_MEDIA_QUERY = "(max-width: 1023px)";
type TextDialogState =
  | { kind: "create-folder" }
  | { kind: "rename-folder"; folderId: string; initialValue: string }
  | null;

export interface ProductWorkflowV2CanvasContext {
  openFolderId: string | null;
  selectedNodeIds: string[];
}

export type V2CreateReferenceRequest = () => void;
export interface V2CreateReferenceControl {
  request: V2CreateReferenceRequest;
  busy: boolean;
}

interface ProductWorkflowV2CanvasPanelProps {
  productId: string;
  workflow: ProductWorkflowV2;
  revealVisibility: WorkflowRevealVisibility | null;
  activityLabel: string | null;
  interactionLocked: boolean;
  externalError: string | null;
  topChromeCollapsed: boolean;
  onToggleTopChrome: () => void;
  onRefetchWorkflow: () => Promise<unknown>;
  onCanvasContextChange: (context: ProductWorkflowV2CanvasContext) => void;
  onOpenSidebarTool: (tool: "details" | "runs" | "library") => Promise<boolean>;
  onBeforeWorkflowAction: () => Promise<number | null>;
  onReferenceNodeChange: (nodeId: string | null) => void;
  onCreateReferenceRegistration?: (control: V2CreateReferenceControl | null) => void;
  onSaveRecipe: (source: RecipeSourceSelection) => void | Promise<void>;
}

export function ProductWorkflowV2CanvasPanel({
  productId,
  workflow,
  revealVisibility,
  activityLabel,
  interactionLocked,
  externalError,
  topChromeCollapsed,
  onToggleTopChrome,
  onRefetchWorkflow,
  onCanvasContextChange,
  onOpenSidebarTool,
  onBeforeWorkflowAction,
  onReferenceNodeChange,
  onCreateReferenceRegistration,
  onSaveRecipe,
}: ProductWorkflowV2CanvasPanelProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [canvasState, setCanvasState] = useState<WorkflowCanvasStateV1>(emptyWorkflowCanvasState);
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([]);
  const [textDialog, setTextDialog] = useState<TextDialogState>(null);
  const [dissolveFolder, setDissolveFolder] = useState<{ id: string; title: string } | null>(null);
  const [createReferenceOpen, setCreateReferenceOpen] = useState(false);
  const [deleteNode, setDeleteNode] = useState<WorkflowNodeV2 | null>(null);
  const [deleteEdge, setDeleteEdge] = useState<WorkflowEdgeV2 | null>(null);
  const [history, setHistory] = useState(EMPTY_V2_WORKFLOW_HISTORY);
  const [currentRun, setCurrentRun] = useState<{ id: string; nodeId: string } | null>(null);
  const [operationError, setOperationError] = useState<string | null>(null);
  const [canvasResetVersion, setCanvasResetVersion] = useState(0);
  const [canvasRenderable, setCanvasRenderable] = useState(false);
  const [mobileCanvasMode, setMobileCanvasMode] = useState<CanvasInteractionMode>("browse");
  const [mobileCanvasControlsActive, setMobileCanvasControlsActive] = useState(() => (
    typeof window !== "undefined"
      && typeof window.matchMedia === "function"
      && window.matchMedia(COMPACT_WORKFLOW_CANVAS_MEDIA_QUERY).matches
  ));
  const loadedCanvasWorkflowRef = useRef<string | null>(null);
  const workflowRef = useRef(workflow);
  const observedWorkflowVersionRef = useRef({
    workflowId: workflow.id,
    editVersion: workflow.edit_version,
  });
  const locallyAcceptedEditVersionRef = useRef<number | null>(null);
  const selectionTransitionSequenceRef = useRef(0);
  const canvasSurfaceRef = useRef<HTMLDivElement | null>(null);
  const previousActiveWorkflowRunIdsRef = useRef<Set<string>>(new Set());

  workflowRef.current = workflow;

  const openFolder = workflow.folders.find((folder) => folder.id === canvasState.open_folder_id) ?? null;
  const mobileCanvasModeItems: WorkflowCanvasMobileModeItem[] = [
    {
      key: "browse",
      label: t("detail.mobileCanvasBrowse"),
      description: t("detail.mobileCanvasBrowseHint"),
      icon: <Hand size={15} />,
    },
    {
      key: "edit",
      label: t("detail.mobileCanvasEdit"),
      description: t("detail.mobileCanvasEditHint"),
      icon: <Move size={15} />,
    },
    {
      key: "select",
      label: t("detail.mobileCanvasSelect"),
      description: t("detail.mobileCanvasSelectHint"),
      icon: <MousePointer2 size={15} />,
    },
  ];

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const media = window.matchMedia(COMPACT_WORKFLOW_CANVAS_MEDIA_QUERY);
    const update = () => setMobileCanvasControlsActive(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  const persistCanvasState = useCallback((update: CanvasStateUpdate) => {
    setCanvasState((current) => {
      const next = typeof update === "function" ? update(current) : update;
      if (next === current) {
        return current;
      }
      if (typeof window !== "undefined") {
        window.localStorage.setItem(workflowCanvasStateStorageKey(workflow.id), JSON.stringify(next));
      }
      return next;
    });
  }, [workflow.id]);

  useEffect(() => {
    if (loadedCanvasWorkflowRef.current !== workflow.id) {
      loadedCanvasWorkflowRef.current = workflow.id;
      const raw = typeof window === "undefined"
        ? null
        : window.localStorage.getItem(workflowCanvasStateStorageKey(workflow.id));
      setCanvasState(
        reconcileWorkflowCanvasState(
          parseWorkflowCanvasState(raw),
          workflow.folders.map((folder) => folder.id),
        ),
      );
      setSelectedNodeIds([]);
      setHistory(EMPTY_V2_WORKFLOW_HISTORY);
      onReferenceNodeChange(null);
      return;
    }
    setCanvasState((current) =>
      reconcileWorkflowCanvasState(current, workflow.folders.map((folder) => folder.id)),
    );
    setSelectedNodeIds((current) => {
      const visibleNodeIds = new Set(visibleRealNodeIds(workflow, canvasState.open_folder_id));
      return current.filter((nodeId) => visibleNodeIds.has(nodeId));
    });
  }, [canvasState.open_folder_id, onReferenceNodeChange, workflow]);

  useEffect(() => {
    const next = { workflowId: workflow.id, editVersion: workflow.edit_version };
    const previous = observedWorkflowVersionRef.current;
    const locallyAcceptedEditVersion = locallyAcceptedEditVersionRef.current;
    observedWorkflowVersionRef.current = next;
    locallyAcceptedEditVersionRef.current = null;
    if (shouldClearV2WorkflowHistory(previous, next, locallyAcceptedEditVersion)) {
      setHistory(EMPTY_V2_WORKFLOW_HISTORY);
    }
  }, [workflow.edit_version, workflow.id]);

  useEffect(() => {
    onCanvasContextChange({
      openFolderId: openFolder?.id ?? null,
      selectedNodeIds,
    });
  }, [onCanvasContextChange, openFolder?.id, selectedNodeIds]);

  useEffect(() => {
    const surface = canvasSurfaceRef.current;
    if (!surface) {
      return;
    }
    const update = () => {
      const bounds = surface.getBoundingClientRect();
      setCanvasRenderable(bounds.width > 0 && bounds.height > 0);
    };
    update();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", update);
      return () => window.removeEventListener("resize", update);
    }
    const observer = new ResizeObserver(update);
    observer.observe(surface);
    return () => observer.disconnect();
  }, [workflow.id]);

  const runQuery = useQuery({
    queryKey: ["workflow-node-run-v2", currentRun?.id],
    queryFn: () => api.getWorkflowNodeRunV2(currentRun!.id),
    enabled: Boolean(currentRun),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return !status || status === "queued" || status === "running" ? 1_000 : false;
    },
  });
  const workflowRunsQueryKey = ["v2-workflow-runs", productId, workflow.id] as const;
  const workflowRunsQuery = useQuery({
    queryKey: workflowRunsQueryKey,
    queryFn: () => api.listWorkflowRunsV2(productId, workflow.id),
    refetchInterval: (query) => query.state.data?.items.some((run) => run.status === "running")
      ? 1_200
      : false,
  });
  const activeWorkflowRun = workflowRunsQuery.data?.items.find((run) => run.status === "running") ?? null;

  useEffect(() => {
    const result = workflowRunsQuery.data;
    if (!result) {
      return;
    }
    queryClient.setQueryData<ActiveProductWorkflowV2>(
      ["active-product-workflow-v2", productId],
      { latest_revision: result.workflow.revision, workflow: result.workflow },
    );
    const activeIds = new Set(
      result.items.filter((run) => run.status === "running").map((run) => run.id),
    );
    if (previousActiveWorkflowRunIdsRef.current.size > 0 && activeIds.size === 0) {
      void onRefetchWorkflow();
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
        queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
        queryClient.invalidateQueries({ queryKey: ["product", productId] }),
        queryClient.invalidateQueries({ queryKey: ["products"] }),
      ]);
    }
    previousActiveWorkflowRunIdsRef.current = activeIds;
  }, [onRefetchWorkflow, productId, queryClient, workflowRunsQuery.data]);

  useEffect(() => {
    const status = runQuery.data?.status;
    if (!currentRun || !status || status === "queued" || status === "running") {
      return;
    }
    setCurrentRun(null);
    void onRefetchWorkflow();
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
      queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
      queryClient.invalidateQueries({ queryKey: ["product", productId] }),
      queryClient.invalidateQueries({ queryKey: ["products"] }),
    ]);
  }, [currentRun, onRefetchWorkflow, productId, queryClient, runQuery.data?.status]);

  const acceptCanvasMutation = useCallback((result: WorkflowCanvasMutationResult) => {
    locallyAcceptedEditVersionRef.current = result.workflow.edit_version;
    workflowRef.current = result.workflow;
    queryClient.setQueryData<ActiveProductWorkflowV2>(
      ["active-product-workflow-v2", productId],
      { latest_revision: result.workflow.revision, workflow: result.workflow },
    );
    setOperationError(null);
  }, [productId, queryClient]);

  const structureMutation = useMutation({
    mutationFn: async (operation: StructureOperation) => {
      const flushedEditVersion = await onBeforeWorkflowAction();
      return operation(flushedEditVersion ?? workflowRef.current.edit_version);
    },
    onSuccess: acceptCanvasMutation,
    onError: async (error) => {
      setOperationError(errorDetail(error, t("workflowV2.error.structure")));
      setCanvasResetVersion((version) => version + 1);
      if (error instanceof ApiError && error.status === 409) {
        await onRefetchWorkflow();
      }
    },
  });
  const pushHistory = useCallback((action: V2WorkflowHistoryAction) => {
    setHistory((current) => ({ undo: [...current.undo, action], redo: [] }));
  }, []);
  const clearHistory = useCallback(() => {
    setHistory(EMPTY_V2_WORKFLOW_HISTORY);
  }, []);

  const commitTrackedLayout = useCallback(async (
    positions: Array<{ node_id: string; position_x: number; position_y: number }>,
  ) => {
    const beforeWorkflow = workflowRef.current;
    const before = captureV2WorkflowPositions(beforeWorkflow, positions.map((position) => position.node_id));
    try {
      const result = await structureMutation.mutateAsync((expectedEditVersion) => api.updateWorkflowNodeLayoutV2(
        productId,
        beforeWorkflow.id,
        { positions, expected_edit_version: expectedEditVersion },
      ));
      if (result.changed) {
        const after = captureV2WorkflowPositions(result.workflow, before.map((position) => position.node_id));
        pushHistory({ kind: "layout", before, after });
      }
    } catch {
      // The mutation owns the visible error and conflict refresh.
    }
  }, [productId, pushHistory, structureMutation]);

  const translateFolderTracked = useCallback(async (folderId: string, deltaX: number, deltaY: number) => {
    const beforeWorkflow = workflowRef.current;
    const memberIds = beforeWorkflow.nodes
      .filter((node) => node.folder_id === folderId)
      .map((node) => node.id);
    const before = captureV2WorkflowPositions(beforeWorkflow, memberIds);
    try {
      const result = await structureMutation.mutateAsync((expectedEditVersion) => api.translateWorkflowFolder(
        productId,
        beforeWorkflow.id,
        folderId,
        { delta_x: deltaX, delta_y: deltaY, expected_edit_version: expectedEditVersion },
      ));
      if (result.changed) {
        pushHistory({
          kind: "layout",
          before,
          after: captureV2WorkflowPositions(result.workflow, memberIds),
        });
      }
    } catch {
      // The mutation owns the visible error and conflict refresh.
    }
  }, [productId, pushHistory, structureMutation]);

  const createReferenceNode = useCallback(async (values: { title: string; role: string; label: string }) => {
    const beforeWorkflow = workflowRef.current;
    const folderId = canvasState.open_folder_id;
    const visibleNodes = beforeWorkflow.nodes.filter((node) => (
      folderId ? node.folder_id === folderId : node.folder_id === null
    ));
    const positionX = visibleNodes.length ? Math.min(...visibleNodes.map((node) => node.position_x)) : 120;
    const positionY = visibleNodes.length ? Math.max(...visibleNodes.map((node) => node.position_y)) + 288 : 120;
    const input: Omit<CreateReferenceWorkflowNodeV2Input, "expected_edit_version"> = {
      ...values,
      position_x: positionX,
      position_y: positionY,
      folder_id: folderId,
    };
    try {
      const result = await structureMutation.mutateAsync((expectedEditVersion) => api.createWorkflowReferenceNodeV2(
        productId,
        beforeWorkflow.id,
        { ...input, expected_edit_version: expectedEditVersion },
      ));
      const nodeId = findAddedV2RootNodeId(beforeWorkflow, result.workflow, "reference_image");
      pushHistory({ kind: "reference-created", nodeId, input });
      setCreateReferenceOpen(false);
      setSelectedNodeIds([nodeId]);
    } catch {
      // Keep the dialog open so the API error remains actionable.
    }
  }, [canvasState.open_folder_id, productId, pushHistory, structureMutation]);

  const duplicateNodeTracked = useCallback(async (node: WorkflowNodeV2) => {
    const beforeWorkflow = workflowRef.current;
    try {
      const result = await structureMutation.mutateAsync((expectedEditVersion) => api.duplicateWorkflowNodeV2(
        productId,
        beforeWorkflow.id,
        node.id,
        expectedEditVersion,
      ));
      const createdRootNodeId = findAddedV2RootNodeId(beforeWorkflow, result.workflow, node.node_type);
      pushHistory({
        kind: "node-duplicated",
        sourceNodeId: node.id,
        sourceNodeType: node.node_type,
        createdRootNodeId,
      });
      setSelectedNodeIds([createdRootNodeId]);
    } catch {
      // The mutation owns the visible error and conflict refresh.
    }
  }, [productId, pushHistory, structureMutation]);

  const createEdgeTracked = useCallback(async (sourceNodeId: string, targetNodeId: string) => {
    const beforeWorkflow = workflowRef.current;
    try {
      const result = await structureMutation.mutateAsync((expectedEditVersion) => api.createWorkflowEdgeV2(
        productId,
        beforeWorkflow.id,
        {
          source_node_id: sourceNodeId,
          target_node_id: targetNodeId,
          expected_edit_version: expectedEditVersion,
        },
      ));
      pushHistory({
        kind: "edge",
        initialOperation: "created",
        sourceNodeId,
        targetNodeId,
        edgeId: findV2WorkflowEdgeId(result.workflow, sourceNodeId, targetNodeId),
      });
    } catch {
      // The mutation owns the visible error and conflict refresh.
    }
  }, [productId, pushHistory, structureMutation]);

  const executeHistoryAction = useCallback(async (
    action: V2WorkflowHistoryAction,
    direction: "undo" | "redo",
  ): Promise<V2WorkflowHistoryAction> => {
    const currentWorkflow = workflowRef.current;
    if (action.kind === "layout") {
      await structureMutation.mutateAsync((expectedEditVersion) => api.updateWorkflowNodeLayoutV2(
        productId,
        currentWorkflow.id,
        {
          positions: direction === "undo" ? action.before : action.after,
          expected_edit_version: expectedEditVersion,
        },
      ));
      return action;
    }
    if (action.kind === "reference-created") {
      if (direction === "undo") {
        await structureMutation.mutateAsync((expectedEditVersion) => api.deleteWorkflowNodeV2(
          productId,
          currentWorkflow.id,
          action.nodeId,
          expectedEditVersion,
        ));
        return action;
      }
      const result = await structureMutation.mutateAsync((expectedEditVersion) => api.createWorkflowReferenceNodeV2(
        productId,
        currentWorkflow.id,
        { ...action.input, expected_edit_version: expectedEditVersion },
      ));
      return {
        ...action,
        nodeId: findAddedV2RootNodeId(currentWorkflow, result.workflow, "reference_image"),
      };
    }
    if (action.kind === "node-duplicated") {
      if (direction === "undo") {
        await structureMutation.mutateAsync((expectedEditVersion) => api.deleteWorkflowNodeV2(
          productId,
          currentWorkflow.id,
          action.createdRootNodeId,
          expectedEditVersion,
        ));
        return action;
      }
      const result = await structureMutation.mutateAsync((expectedEditVersion) => api.duplicateWorkflowNodeV2(
        productId,
        currentWorkflow.id,
        action.sourceNodeId,
        expectedEditVersion,
      ));
      return {
        ...action,
        createdRootNodeId: findAddedV2RootNodeId(currentWorkflow, result.workflow, action.sourceNodeType),
      };
    }

    const shouldCreate = action.initialOperation === "created"
      ? direction === "redo"
      : direction === "undo";
    if (shouldCreate) {
      const result = await structureMutation.mutateAsync((expectedEditVersion) => api.createWorkflowEdgeV2(
        productId,
        currentWorkflow.id,
        {
          source_node_id: action.sourceNodeId,
          target_node_id: action.targetNodeId,
          expected_edit_version: expectedEditVersion,
        },
      ));
      return {
        ...action,
        edgeId: findV2WorkflowEdgeId(result.workflow, action.sourceNodeId, action.targetNodeId),
      };
    }
    await structureMutation.mutateAsync((expectedEditVersion) => api.deleteWorkflowEdgeV2(
      productId,
      currentWorkflow.id,
      action.edgeId,
      expectedEditVersion,
    ));
    return action;
  }, [productId, structureMutation]);

  const applyHistory = useCallback(async (direction: "undo" | "redo") => {
    const source = direction === "undo" ? history.undo : history.redo;
    const action = source[source.length - 1];
    if (!action || structureMutation.isPending) return;
    try {
      const updated = await executeHistoryAction(action, direction);
      setHistory((current) => {
        const from = direction === "undo" ? current.undo : current.redo;
        if (from[from.length - 1] !== action) return current;
        return direction === "undo"
          ? { undo: current.undo.slice(0, -1), redo: [...current.redo, updated] }
          : { undo: [...current.undo, updated], redo: current.redo.slice(0, -1) };
      });
    } catch {
      // The mutation owns the visible error and conflict refresh.
    }
  }, [executeHistoryAction, history.redo, history.undo, structureMutation.isPending]);
  const workflowRunMutation = useMutation({
    mutationFn: async () => {
      await onBeforeWorkflowAction();
      return api.runWorkflowV2(productId, workflow.id);
    },
    onMutate: () => setOperationError(null),
    onSuccess: async (result) => {
      queryClient.setQueryData<WorkflowRunListV2Response>(workflowRunsQueryKey, (current) => ({
        workflow: result.workflow,
        items: [
          result.workflow_run,
          ...(current?.items ?? []).filter((run) => run.id !== result.workflow_run.id),
        ],
      }));
      queryClient.setQueryData<ActiveProductWorkflowV2>(
        ["active-product-workflow-v2", productId],
        { latest_revision: result.workflow.revision, workflow: result.workflow },
      );
      await onOpenSidebarTool("runs");
      await onRefetchWorkflow();
    },
    onError: (error) => {
      setOperationError(errorDetail(error, t("workflowV2.error.runWorkflow")));
    },
  });
  const nodeRunMutation = useMutation({
    mutationFn: async (node: WorkflowNodeV2) => {
      await onBeforeWorkflowAction();
      return api.runWorkflowNodeV2(node.id);
    },
    onMutate: () => {
      setOperationError(null);
      setCurrentRun(null);
    },
    onSuccess: async (result) => {
      setCurrentRun({ id: result.node_run.id, nodeId: result.node_run.node_id });
      await onRefetchWorkflow();
    },
    onError: (error) => {
      setCurrentRun(null);
      setOperationError(errorDetail(error, t("workflowV2.error.run")));
    },
  });

  const moveSelection = (folderId: string | null) => {
    if (selectedNodeIds.length === 0) {
      return;
    }
    if (folderId) {
      const targetIds = workflow.nodes
        .filter((node) => node.folder_id === folderId)
        .map((node) => node.id);
      const nodeIds = Array.from(new Set([...targetIds, ...selectedNodeIds]));
      structureMutation.mutate((expectedEditVersion) => api.setWorkflowFolderMembers(productId, workflow.id, folderId, {
        node_ids: nodeIds,
        expected_edit_version: expectedEditVersion,
      }), { onSuccess: clearHistory });
      return;
    }
    if (!openFolder) {
      return;
    }
    const selected = new Set(selectedNodeIds);
    const remainingIds = workflow.nodes
      .filter((node) => node.folder_id === openFolder.id && !selected.has(node.id))
      .map((node) => node.id);
    structureMutation.mutate((expectedEditVersion) => api.setWorkflowFolderMembers(productId, workflow.id, openFolder.id, {
      node_ids: remainingIds,
      expected_edit_version: expectedEditVersion,
    }), { onSuccess: () => { clearHistory(); setSelectedNodeIds([]); } });
  };

  const viewportFolderId = openFolder?.id ?? null;
  const updateViewport = useCallback((viewport: WorkflowCanvasViewport) => {
    persistCanvasState((current) => viewportFolderId ? {
      ...current,
      folder_viewports: { ...current.folder_viewports, [viewportFolderId]: viewport },
    } : {
      ...current,
      global_viewport: viewport,
    });
  }, [persistCanvasState, viewportFolderId]);

  const openCanvasFolder = useCallback((folderId: string | null) => {
    const sequence = ++selectionTransitionSequenceRef.current;
    void (async () => {
      try {
        await onBeforeWorkflowAction();
      } catch {
        return;
      }
      if (sequence !== selectionTransitionSequenceRef.current) {
        return;
      }
      persistCanvasState((current) => ({ ...current, open_folder_id: folderId }));
      setSelectedNodeIds([]);
      onReferenceNodeChange(null);
    })();
  }, [onBeforeWorkflowAction, onReferenceNodeChange, persistCanvasState]);

  const selectNodes = useCallback((nodeIds: string[]) => {
    if (
      selectedNodeIds.length === nodeIds.length
      && selectedNodeIds.every((nodeId, index) => nodeId === nodeIds[index])
    ) {
      return;
    }
    const sequence = ++selectionTransitionSequenceRef.current;
    void (async () => {
      const keepCanvasVisible = mobileCanvasControlsActive && mobileCanvasMode === "select";
      try {
        if (!keepCanvasVisible && nodeIds.length === 1) {
          const accepted = await onOpenSidebarTool("details");
          if (!accepted) {
            return;
          }
        } else {
          await onBeforeWorkflowAction();
        }
      } catch {
        return;
      }
      if (sequence !== selectionTransitionSequenceRef.current) {
        return;
      }
      onReferenceNodeChange(null);
      setSelectedNodeIds(nodeIds);
    })();
  }, [
    mobileCanvasControlsActive,
    mobileCanvasMode,
    onBeforeWorkflowAction,
    onOpenSidebarTool,
    onReferenceNodeChange,
    selectedNodeIds,
  ]);

  const deleteNodeConfirmed = useCallback(async () => {
    if (!deleteNode) return;
    const currentWorkflow = workflowRef.current;
    try {
      await structureMutation.mutateAsync((expectedEditVersion) => api.deleteWorkflowNodeV2(
        productId,
        currentWorkflow.id,
        deleteNode.id,
        expectedEditVersion,
      ));
      clearHistory();
      setDeleteNode(null);
      setSelectedNodeIds([]);
    } catch {
      // Keep the confirmation open so the API error remains visible.
    }
  }, [clearHistory, deleteNode, productId, structureMutation]);

  const deleteEdgeConfirmed = useCallback(async () => {
    if (!deleteEdge) return;
    const currentWorkflow = workflowRef.current;
    try {
      await structureMutation.mutateAsync((expectedEditVersion) => api.deleteWorkflowEdgeV2(
        productId,
        currentWorkflow.id,
        deleteEdge.id,
        expectedEditVersion,
      ));
      pushHistory({
        kind: "edge",
        initialOperation: "deleted",
        sourceNodeId: deleteEdge.source_node_id,
        targetNodeId: deleteEdge.target_node_id,
        edgeId: deleteEdge.id,
      });
      setDeleteEdge(null);
    } catch {
      // Keep the confirmation open so the API error remains visible.
    }
  }, [deleteEdge, productId, pushHistory, structureMutation]);

  useEffect(() => {
    const handleShortcut = (event: KeyboardEvent) => {
      const shortcut = getWorkflowKeyboardShortcut(event);
      if (!shortcut || interactionLocked || structureMutation.isPending) return;
      if (shortcut === "undo" || shortcut === "redo") {
        event.preventDefault();
        void applyHistory(shortcut);
        return;
      }
      if (selectedNodeIds.length !== 1) return;
      const selected = workflowRef.current.nodes.find((node) => node.id === selectedNodeIds[0]);
      if (!selected || selected.node_type === "product_context") return;
      if (shortcut === "duplicate") {
        event.preventDefault();
        void duplicateNodeTracked(selected);
      } else if (shortcut === "delete") {
        event.preventDefault();
        setDeleteNode(selected);
      }
    };
    window.addEventListener("keydown", handleShortcut);
    return () => window.removeEventListener("keydown", handleShortcut);
  }, [applyHistory, duplicateNodeTracked, interactionLocked, selectedNodeIds, structureMutation.isPending]);

  const runningNodeId = currentRun?.nodeId ?? (
    nodeRunMutation.isPending ? nodeRunMutation.variables?.id ?? null : null
  );
  const workflowRunBusy = workflowRunMutation.isPending || Boolean(activeWorkflowRun);
  const busy = structureMutation.isPending
    || interactionLocked
    || workflowRunBusy
    || nodeRunMutation.isPending
    || Boolean(currentRun);
  const requestCreateReference = useCallback(() => {
    setOperationError(null);
    setCreateReferenceOpen(true);
  }, []);

  useEffect(() => {
    if (!onCreateReferenceRegistration) return;
    onCreateReferenceRegistration({ request: requestCreateReference, busy });
    return () => onCreateReferenceRegistration(null);
  }, [busy, onCreateReferenceRegistration, requestCreateReference]);

  const deleteNodeDescription = deleteNode
    ? deleteNode.node_type === "reference_image"
      ? t("workflowV2.node.deleteReferenceConfirm", { title: deleteNode.title })
      : deleteNode.node_type === "prompt_generation"
        ? t("workflowV2.node.deletePromptConfirm", { title: deleteNode.title })
        : workflow.nodes.filter((node) => (
            node.node_type === "image_generation"
            && node.config_json.prompt_plan_key === deleteNode.config_json.prompt_plan_key
          )).length === 1
          ? t("workflowV2.node.deleteLastImageConfirm", { title: deleteNode.title })
          : t("workflowV2.node.deleteImageConfirm", { title: deleteNode.title })
    : "";

  return (
    <>
      <section
        data-product-workflow-v2-canvas
        data-agent-workflow-canvas
        className="relative flex h-full min-h-0 flex-col overflow-hidden bg-zinc-50 text-zinc-950 dark:bg-[#080c12] dark:text-slate-100"
      >
        {operationError || externalError ? (
          <div role="alert" className="flex shrink-0 items-start gap-2 border-b border-red-200 bg-red-50 px-3 py-2 text-xs leading-5 text-red-700 dark:border-red-400/25 dark:bg-red-500/10 dark:text-red-200">
            <CircleAlert size={14} className="mt-0.5 shrink-0" />
            <span className="min-w-0 flex-1">{operationError ?? externalError}</span>
            {operationError ? (
              <button type="button" onClick={() => setOperationError(null)} className="font-semibold hover:underline">
                {t("workflowV2.dialog.close")}
              </button>
            ) : null}
          </div>
        ) : null}

        <div
          ref={canvasSurfaceRef}
          className="relative min-h-0 flex-1 overflow-hidden"
          aria-label={t("workflowV2.canvas.ariaLabel")}
        >
          <ProductWorkbenchCanvasChromeToggle
            collapsed={topChromeCollapsed}
            maximizeLabel={t("detail.maximizeCanvas")}
            restoreLabel={t("detail.restoreCanvas")}
            onToggle={onToggleTopChrome}
          />
          {canvasRenderable ? (
            <V2WorkflowCanvas
              workflow={workflow}
              resetVersion={canvasResetVersion}
              revealVisibility={revealVisibility ?? undefined}
              openFolderId={openFolder?.id ?? null}
              viewport={openFolder
                ? canvasState.folder_viewports[openFolder.id] ?? null
                : canvasState.global_viewport}
              structureBusy={busy}
              runningNodeId={runningNodeId}
              selectedNodeIds={selectedNodeIds}
              mobileInteractionMode={mobileCanvasControlsActive ? mobileCanvasMode : "edit"}
              mobileCanvasControlsActive={mobileCanvasControlsActive}
              onOpenFolder={openCanvasFolder}
              onRunNode={(node) => nodeRunMutation.mutate(node)}
              onBindReference={(node) => {
                void (async () => {
                  if (await onOpenSidebarTool("library")) {
                    onReferenceNodeChange(node.id);
                  }
                })();
              }}
              onDuplicateNode={(node) => void duplicateNodeTracked(node)}
              onDeleteNode={setDeleteNode}
              onConnectionCreate={(sourceNodeId, targetNodeId) => {
                void createEdgeTracked(sourceNodeId, targetNodeId);
              }}
              onEdgeDelete={(edgeId) => {
                const edge = workflowRef.current.edges.find((candidate) => candidate.id === edgeId);
                if (edge) setDeleteEdge(edge);
              }}
              onSelectionChange={selectNodes}
              onLayoutCommit={(positions) => {
                void commitTrackedLayout(positions);
              }}
              onFolderTranslate={(folderId, deltaX, deltaY) => {
                void translateFolderTracked(folderId, deltaX, deltaY);
              }}
              onViewportChange={updateViewport}
            />
          ) : null}

          <V2WorkflowCommandBar
            workflow={workflow}
            openFolderId={openFolder?.id ?? null}
            selectedNodeIds={selectedNodeIds}
            structureBusy={busy}
            workflowRunBusy={workflowRunBusy}
            variant="overlay"
            activityLabel={activityLabel}
            onOpenFolder={openCanvasFolder}
            onCreateFolder={() => setTextDialog({ kind: "create-folder" })}
            onMoveSelection={moveSelection}
            onSaveRecipe={(source) => void onSaveRecipe(source)}
            onRenameFolder={() => {
              if (openFolder) {
                setTextDialog({
                  kind: "rename-folder",
                  folderId: openFolder.id,
                  initialValue: openFolder.title,
                });
              }
            }}
            onDissolveFolder={() => {
              if (openFolder) {
                setDissolveFolder({ id: openFolder.id, title: openFolder.title });
              }
            }}
            onRunWorkflow={() => workflowRunMutation.mutate()}
            canUndo={history.undo.length > 0}
            canRedo={history.redo.length > 0}
            onUndo={() => void applyHistory("undo")}
            onRedo={() => void applyHistory("redo")}
          />

          <div
            data-canvas-control
            className="pointer-events-none absolute inset-x-3 bottom-3 z-30 lg:hidden"
          >
            <div className="pointer-events-auto mx-auto max-w-[28rem] rounded-2xl border border-slate-200 bg-white p-1.5 shadow-[0_-6px_18px_rgba(15,23,42,0.12)] dark:border-slate-700 dark:bg-slate-950 dark:shadow-[0_-12px_28px_rgba(0,0,0,0.30)]">
              <WorkflowCanvasMobileModeTabs
                value={mobileCanvasMode}
                items={mobileCanvasModeItems}
                onChange={setMobileCanvasMode}
              />
            </div>
          </div>

          {structureMutation.isPending ? (
            <div className="pointer-events-none absolute bottom-20 left-3 z-20 inline-flex items-center gap-1.5 rounded-md border border-zinc-200 bg-white/95 px-2.5 py-1.5 text-[11px] font-semibold text-zinc-600 shadow-sm backdrop-blur dark:border-slate-700 dark:bg-slate-900/95 dark:text-slate-200 lg:bottom-3">
              <Loader2 size={12} className="animate-spin" />
              {t("workflowV2.canvas.saving")}
            </div>
          ) : null}
        </div>
      </section>

      <WorkflowTextDialog
        open={textDialog !== null}
        title={textDialog?.kind === "rename-folder" ? t("workflowV2.folder.rename") : t("workflowV2.folder.create")}
        label={t("workflowV2.folder.name")}
        initialValue={textDialog?.kind === "rename-folder" ? textDialog.initialValue : ""}
        busy={structureMutation.isPending}
        error={structureMutation.isError ? operationError : null}
        onClose={() => setTextDialog(null)}
        onSubmit={(title) => {
          if (!textDialog) {
            return;
          }
          if (textDialog.kind === "create-folder") {
            structureMutation.mutate((expectedEditVersion) => api.createWorkflowFolder(productId, workflow.id, {
              title,
              node_ids: selectedNodeIds,
              expected_edit_version: expectedEditVersion,
            }), { onSuccess: () => { clearHistory(); setTextDialog(null); setSelectedNodeIds([]); } });
            return;
          }
          structureMutation.mutate((expectedEditVersion) => api.renameWorkflowFolder(productId, workflow.id, textDialog.folderId, {
            title,
            expected_edit_version: expectedEditVersion,
          }), { onSuccess: () => { clearHistory(); setTextDialog(null); } });
        }}
      />

      <WorkflowReferenceNodeDialog
        open={createReferenceOpen}
        busy={structureMutation.isPending}
        error={structureMutation.isError ? operationError : null}
        onClose={() => setCreateReferenceOpen(false)}
        onSubmit={(input) => void createReferenceNode(input)}
      />

      <ConfirmDialog
        open={deleteNode !== null}
        title={t("workflowV2.node.deleteTitle")}
        description={deleteNodeDescription}
        confirmLabel={t("detail.delete")}
        cancelLabel={t("common.cancel")}
        busy={structureMutation.isPending}
        destructive
        onClose={() => setDeleteNode(null)}
        onConfirm={() => void deleteNodeConfirmed()}
      />

      <ConfirmDialog
        open={deleteEdge !== null}
        title={t("detail.deleteEdge")}
        description={t("workflowV2.edge.deleteConfirm")}
        confirmLabel={t("detail.deleteEdge")}
        cancelLabel={t("common.cancel")}
        busy={structureMutation.isPending}
        destructive
        onClose={() => setDeleteEdge(null)}
        onConfirm={() => void deleteEdgeConfirmed()}
      />

      <ConfirmDialog
        open={dissolveFolder !== null}
        title={t("workflowV2.folder.dissolve")}
        description={t("workflowV2.folder.dissolveConfirm", { title: dissolveFolder?.title ?? "" })}
        confirmLabel={t("workflowV2.folder.dissolve")}
        cancelLabel={t("common.cancel")}
        busy={structureMutation.isPending}
        destructive
        onClose={() => setDissolveFolder(null)}
        onConfirm={() => {
          if (!dissolveFolder) {
            return;
          }
          structureMutation.mutate(
            (expectedEditVersion) => api.dissolveWorkflowFolder(
              productId,
              workflow.id,
              dissolveFolder.id,
              expectedEditVersion,
            ),
            {
              onSuccess: () => {
                clearHistory();
                setDissolveFolder(null);
                persistCanvasState((current) => ({ ...current, open_folder_id: null }));
                setSelectedNodeIds([]);
              },
            },
          );
        }}
      />
    </>
  );
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return error instanceof Error ? error.message : fallback;
}
