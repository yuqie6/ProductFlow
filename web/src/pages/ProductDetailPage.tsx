import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent, ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  ChevronLeft,
  ChevronRight,
  CircleDot,
  Check,
  Eye,
  FileText,
  Hand,
  Image as ImageIcon,
  ImagePlus,
  Layers3,
  Loader2,
  Maximize2,
  Minimize2,
  MousePointer2,
  Move,
  Play,
  Plus,
  Settings2,
  Sparkles,
  X,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";
import { Drawer } from "vaul";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { DEFAULT_IMAGE_TOOL_ALLOWED_FIELDS } from "../lib/imageToolOptions";
import { DEFAULT_IMAGE_GENERATION_MAX_DIMENSION, buildImageSizeOptions } from "../lib/imageSizes";
import { useI18n } from "../lib/preferences";
import type {
  CanvasTemplateSummary,
  ProductWorkflow,
  ProductWorkflowStatus,
  WorkflowNode,
  WorkflowNodeType,
} from "../lib/types";
import {
  ADD_NODE_OPTIONS,
  MAX_INSPECTOR_WIDTH,
  MIN_INSPECTOR_WIDTH,
} from "./product-detail/constants";
import { ImagePreviewModal } from "./product-detail/ImagePreviewModal";
import { ImagesPanel } from "./product-detail/ImagesPanel";
import { InspectorPanel } from "./product-detail/InspectorPanel";
import { RunsPanel } from "./product-detail/RunsPanel";
import { SidebarTabButton } from "./product-detail/SidebarTabButton";
import { TemplateGroupsPanel } from "./product-detail/TemplateGroupsPanel";
import { WorkflowCanvas } from "./product-detail/WorkflowCanvas";
import type { NodePositionCommitInput, WorkflowCanvasHandle } from "./product-detail/WorkflowCanvas";
import {
  buildPosterSourceAssetMap,
  getVisibleReferenceAssets,
} from "./product-detail/galleryImages";
import { getNodeImageDownload, getSourceImageDownload } from "./product-detail/imageDownloads";
import {
  clearSelectedNodeGroup,
  deleteNodeFromSelection,
  focusSelectedNodeGroup,
  reconcileSelectedNodeIds,
  replaceSelectedNodeIdsFromBox,
  toggleSelectedNodeId,
} from "./product-detail/selection";
import {
  getSelectedWorkflowShortcutNodeIds,
  getWorkflowKeyboardShortcut,
} from "./product-detail/shortcuts";
import {
  createRestoreEdgesStep,
  createRestoreNodesStep,
  getWorkflowStructureSignature,
  getInternalWorkflowEdges,
  workflowHistoryStepRequiresConfirmation,
  workflowEdgeToRestorableEdge,
} from "./product-detail/workflowHistory";
import type { WorkflowHistoryStep } from "./product-detail/workflowHistory";
import { connectionDescription, localizedWorkflowNodeTypeLabel } from "./product-detail/nodeDisplay";
import type { CanvasInteractionMode, NodeConfigDraft, SaveStatus } from "./product-detail/types";
import {
  buildWorkflowCanvasActionItems,
  getWorkflowCanvasActionTargetForNodeToolbar,
  getWorkflowCanvasActionTargetNodeIds,
} from "./product-detail/workflowActions";
import type {
  WorkflowCanvasActionId,
  WorkflowCanvasActionItem,
  WorkflowCanvasActionToolbar,
  WorkflowCanvasActionTarget,
} from "./product-detail/workflowActions";
import {
  clamp,
  getWorkflowNodeCancelableRun,
  getWorkflowNodeRunActionState,
  hasActiveWorkflow,
  isProductWorkflowStatusActive,
  mergeProductWorkflowStatusIntoDetail,
  outputText,
  readStoredNumber,
  shouldRefreshProductWorkflowDetailFromStatus,
} from "./product-detail/utils";
import {
  defaultConfigForType,
  defaultTitleForType,
  draftFromNode,
  nodeConfigFromDraft,
} from "./product-detail/workflowConfig";
import type { DownloadableImage } from "../lib/image-downloads";
import { normalizeWorkflowZoom } from "./product-detail/reactFlowAdapters";

type SidebarTab = "singleNode" | "templates" | "details" | "runs" | "images";

type PendingDeleteAction =
  | { kind: "node"; node: WorkflowNode }
  | { kind: "selectedNodes"; nodeIds: string[]; count: number }
  | { kind: "template"; templateId: string; title: string };

type PendingHistoryAction = {
  direction: "undo" | "redo";
  step: WorkflowHistoryStep;
};

type WorkflowClipboard = {
  nodeIds: string[];
};

export function ProductDetailPage() {
  const { locale, t } = useI18n();
  const { productId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const previousBodyUserSelectRef = useRef<string | null>(null);
  const workflowCanvasRef = useRef<WorkflowCanvasHandle | null>(null);
  const wasWorkflowActiveRef = useRef(false);
  const workflowHistorySignatureRef = useRef<string | null>(null);
  const draftVersionRef = useRef(0);
  const previousDraftNodeIdRef = useRef<string | null>(null);
  const skipNextCanvasBlankClickRef = useRef(false);
  const workflowClipboardRef = useRef<WorkflowClipboard | null>(null);
  const undoStackRef = useRef<WorkflowHistoryStep[]>([]);
  const redoStackRef = useRef<WorkflowHistoryStep[]>([]);
  const pendingMoveGroupsRef = useRef<
    Record<
      string,
      {
        expected: number;
        moves: Array<{ nodeId: string; from: { x: number; y: number }; to: { x: number; y: number } }>;
      }
    >
  >({});
  const mobileInitialCanvasViewRef = useRef("");
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([]);
  const [templateSaveTitle, setTemplateSaveTitle] = useState("");
  const [templateSaveDescription, setTemplateSaveDescription] = useState("");
  const [templateSaveOpen, setTemplateSaveOpen] = useState(false);
  const [activeSidebarTab, setActiveSidebarTab] = useState<SidebarTab>("details");
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [topChromeCollapsed, setTopChromeCollapsed] = useState(false);
  const [mobileDetailsSheetOpen, setMobileDetailsSheetOpen] = useState(false);
  const [mobileCanvasMode, setMobileCanvasMode] = useState<CanvasInteractionMode>("browse");
  const [mobileCanvasControlsActive, setMobileCanvasControlsActive] = useState(() =>
    typeof window === "undefined" ? false : window.matchMedia("(max-width: 1023px)").matches,
  );
  const [pendingTemplateAutoLayoutSignature, setPendingTemplateAutoLayoutSignature] = useState<string | null>(null);
  const [initialWorkflowCanvasZoom] = useState(() =>
    normalizeWorkflowZoom(readStoredNumber("productflow.workflow.zoom", 1)),
  );
  const [snapToGrid, setSnapToGrid] = useState(() => {
    const stored = window.localStorage.getItem("productflow.workflow.snapToGrid");
    return stored === null ? true : stored === "true";
  });

  useEffect(() => {
    window.localStorage.setItem("productflow.workflow.snapToGrid", String(snapToGrid));
  }, [snapToGrid]);
  const [draft, setDraft] = useState<NodeConfigDraft>(() =>
    draftFromNode(null),
  );
  const [draftDirty, setDraftDirty] = useState(false);
  const [saveStatus, setSaveStatus] = useState<SaveStatus>("idle");
  const [notice, setNotice] = useState("");
  const [inspectorWidth, setInspectorWidth] = useState(() =>
    clamp(readStoredNumber("productflow.workflow.inspectorWidth", 360), MIN_INSPECTOR_WIDTH, MAX_INSPECTOR_WIDTH),
  );
  const [previewImage, setPreviewImage] = useState<DownloadableImage | null>(
    null,
  );
  const [pendingDeleteAction, setPendingDeleteAction] =
    useState<PendingDeleteAction | null>(null);
  const [pendingHistoryAction, setPendingHistoryAction] = useState<PendingHistoryAction | null>(null);
  const [historyActionBusy, setHistoryActionBusy] = useState(false);
  const [error, setError] = useState("");

  const productQuery = useQuery({
    queryKey: ["product", productId],
    queryFn: () => api.getProduct(productId),
    enabled: Boolean(productId),
  });

  const historyQuery = useQuery({
    queryKey: ["product-history", productId],
    queryFn: () => api.getProductHistory(productId),
    enabled: Boolean(productId),
  });

  const workflowQuery = useQuery({
    queryKey: ["product-workflow", productId],
    queryFn: () => api.getProductWorkflow(productId),
    enabled: Boolean(productId),
  });
  const workflow = workflowQuery.data ?? null;
  const canvasTemplatesQuery = useQuery({
    queryKey: ["canvas-templates"],
    queryFn: api.listCanvasTemplates,
  });
  const workflowActive = hasActiveWorkflow(workflow);
  const workflowStatusQuery = useQuery({
    queryKey: ["product-workflow-status", productId],
    queryFn: () => api.getProductWorkflowStatus(productId),
    enabled: Boolean(productId && workflowActive),
    refetchInterval: (query) => {
      const data = query.state.data as ProductWorkflowStatus | undefined;
      return isProductWorkflowStatusActive(data) ? 1200 : false;
    },
  });
  const runtimeConfigQuery = useQuery({
    queryKey: ["runtime-config"],
    queryFn: api.getRuntimeConfig,
  });
  const queueOverviewQuery = useQuery({
    queryKey: ["generation-queue"],
    queryFn: api.getGenerationQueueOverview,
    refetchInterval: (query) => ((query.state.data?.active_count ?? 0) > 0 || workflowActive ? 1500 : false),
  });
  const imageGenerationMaxDimension =
    runtimeConfigQuery.data?.image_generation_max_dimension ?? DEFAULT_IMAGE_GENERATION_MAX_DIMENSION;
  const imageToolAllowedFields = runtimeConfigQuery.data?.image_tool_allowed_fields ?? DEFAULT_IMAGE_TOOL_ALLOWED_FIELDS;
  const imageSizeOptions = useMemo(
    () => buildImageSizeOptions(imageGenerationMaxDimension),
    [imageGenerationMaxDimension],
  );

  const selectedNode =
    workflow?.nodes.find((node) => node.id === selectedNodeId) ??
    workflow?.nodes[0] ??
    null;
  const workflowStructureSignature = useMemo(
    () => (workflow ? getWorkflowStructureSignature(workflow) : null),
    [workflow],
  );

  const clearWorkflowLocalHistory = useCallback(() => {
    workflowClipboardRef.current = null;
    undoStackRef.current = [];
    redoStackRef.current = [];
    pendingMoveGroupsRef.current = {};
    setPendingHistoryAction(null);
  }, []);

  useEffect(() => {
    clearWorkflowLocalHistory();
    setPendingTemplateAutoLayoutSignature(null);
    workflowHistorySignatureRef.current = workflowStructureSignature;
  }, [clearWorkflowLocalHistory, productId, workflow?.id]);

  useEffect(() => {
    if (!workflowStructureSignature) {
      if (workflowHistorySignatureRef.current !== null) {
        clearWorkflowLocalHistory();
      }
      workflowHistorySignatureRef.current = null;
      return;
    }
    if (workflowHistorySignatureRef.current === null) {
      workflowHistorySignatureRef.current = workflowStructureSignature;
      return;
    }
    if (workflowHistorySignatureRef.current !== workflowStructureSignature) {
      clearWorkflowLocalHistory();
      workflowHistorySignatureRef.current = workflowStructureSignature;
    }
  }, [clearWorkflowLocalHistory, workflowStructureSignature]);

  useEffect(() => {
    if (!pendingTemplateAutoLayoutSignature || workflowStructureSignature !== pendingTemplateAutoLayoutSignature) {
      return;
    }
    setPendingTemplateAutoLayoutSignature(null);
    workflowCanvasRef.current?.triggerAutoLayout();
  }, [pendingTemplateAutoLayoutSignature, workflowStructureSignature]);

  useEffect(() => {
    if (!workflow?.nodes.length) {
      if (selectedNodeId) {
        setSelectedNodeId(null);
      }
      if (selectedNodeIds.length) {
        setSelectedNodeIds([]);
      }
      return;
    }
    const reconciledSelection = reconcileSelectedNodeIds(selectedNodeIds, workflow.nodes, selectedNodeId);
    if (reconciledSelection.primaryNodeId !== selectedNodeId) {
      setSelectedNodeId(reconciledSelection.primaryNodeId);
    }
    if (
      reconciledSelection.selectedNodeIds.length !== selectedNodeIds.length ||
      reconciledSelection.selectedNodeIds.some((nodeId, index) => nodeId !== selectedNodeIds[index])
    ) {
      setSelectedNodeIds(reconciledSelection.selectedNodeIds);
    }
  }, [selectedNodeId, selectedNodeIds, workflow]);

  useEffect(() => {
    const selectedId = selectedNode?.id ?? null;
    const selectedChanged = previousDraftNodeIdRef.current !== selectedId;
    previousDraftNodeIdRef.current = selectedId;
    if (draftDirty && !selectedChanged) {
      return;
    }
    setDraft(draftFromNode(selectedNode, productQuery.data));
    setDraftDirty(false);
    setSaveStatus("idle");
  }, [
    draftDirty,
    productQuery.data,
    selectedNode?.id,
    selectedNode?.last_run_at,
    selectedNode?.updated_at,
  ]);

  useEffect(() => {
    return () => {
      restoreBodyUserSelect();
    };
  }, []);

  useEffect(() => {
    if (!previewImage) {
      return;
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setPreviewImage(null);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [previewImage]);

  const disableBodyUserSelect = () => {
    if (previousBodyUserSelectRef.current === null) {
      previousBodyUserSelectRef.current = document.body.style.userSelect;
    }
    document.body.style.userSelect = "none";
  };

  const restoreBodyUserSelect = () => {
    if (previousBodyUserSelectRef.current === null) {
      return;
    }
    document.body.style.userSelect = previousBodyUserSelectRef.current;
    previousBodyUserSelectRef.current = null;
  };

  const refreshProductArtifacts = async () => {
    await queryClient.invalidateQueries({ queryKey: ["product", productId] });
    await queryClient.invalidateQueries({
      queryKey: ["product-history", productId],
    });
    await queryClient.invalidateQueries({ queryKey: ["products"] });
  };

  useEffect(() => {
    const status = workflowStatusQuery.data;
    if (!status || !productId || status.product_id !== productId) {
      return;
    }
    const currentWorkflow = queryClient.getQueryData<ProductWorkflow>(["product-workflow", productId]);
    const shouldRefetchWorkflow = shouldRefreshProductWorkflowDetailFromStatus(currentWorkflow, status);
    if (currentWorkflow) {
      const nextWorkflow = mergeProductWorkflowStatusIntoDetail(currentWorkflow, status);
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      const latestRun = nextWorkflow.runs[0];
      if (latestRun?.status === "failed" && latestRun.failure_reason) {
        setError(latestRun.failure_reason);
      }
    }
    if (shouldRefetchWorkflow) {
      void queryClient.invalidateQueries({ queryKey: ["product-workflow", productId] });
    }
  }, [productId, queryClient, workflowStatusQuery.data]);

  useEffect(() => {
    if (!wasWorkflowActiveRef.current && workflowActive) {
      wasWorkflowActiveRef.current = true;
      return;
    }
    if (wasWorkflowActiveRef.current && !workflowActive) {
      wasWorkflowActiveRef.current = false;
      void refreshProductArtifacts();
    }
  }, [workflowActive]);

  useEffect(() => {
    const mediaQuery = window.matchMedia("(max-width: 1023px)");
    const syncMobileCanvasControls = () => setMobileCanvasControlsActive(mediaQuery.matches);
    syncMobileCanvasControls();
    mediaQuery.addEventListener("change", syncMobileCanvasControls);
    return () => mediaQuery.removeEventListener("change", syncMobileCanvasControls);
  }, []);

  const handleDraftChange = (nextDraft: NodeConfigDraft) => {
    draftVersionRef.current += 1;
    setDraft(nextDraft);
    setDraftDirty(true);
    setSaveStatus("idle");
  };

  const applyPrimarySelection = (nodeId: string, nodeIds: string[]) => {
    setSelectedNodeId(nodeId);
    setSelectedNodeIds(nodeIds);
    setActiveSidebarTab("details");
  };

  const clearMultiSelection = () => {
    setSelectedNodeIds(clearSelectedNodeGroup(selectedNodeId));
  };

  const selectNodeForDetails = (nodeId: string) => {
    const applySelection = () => {
      const nextSelection = focusSelectedNodeGroup(selectedNodeIds, nodeId);
      applyPrimarySelection(nodeId, nextSelection.selectedNodeIds);
    };
    if (nodeId === selectedNode?.id || !draftDirty) {
      applySelection();
      return;
    }
    void (async () => {
      try {
        await flushSelectedDraft();
        applySelection();
      } catch {
        // Mutations already surface ApiError.detail in local error state.
      }
    })();
  };

  const toggleNodeSelectionForDetails = (nodeId: string) => {
    const applySelection = () => {
      const nextSelectedNodeIds = toggleSelectedNodeId(selectedNodeIds, nodeId);
      const nextPrimaryNodeId = nextSelectedNodeIds.includes(nodeId)
        ? nodeId
        : selectedNodeId === nodeId
          ? nextSelectedNodeIds[0] ?? workflow?.nodes[0]?.id ?? null
          : selectedNodeId;
      if (!nextPrimaryNodeId) {
        setSelectedNodeId(null);
        setSelectedNodeIds([]);
        return;
      }
      applyPrimarySelection(nextPrimaryNodeId, nextSelectedNodeIds.includes(nextPrimaryNodeId) ? nextSelectedNodeIds : [nextPrimaryNodeId]);
    };
    if (nodeId === selectedNode?.id || !draftDirty) {
      applySelection();
      return;
    }
    void (async () => {
      try {
        await flushSelectedDraft();
        applySelection();
      } catch {
        // Mutations already surface ApiError.detail in local error state.
      }
    })();
  };

  const selectNodeFromPointer = (nodeId: string, event: ReactPointerEvent | ReactMouseEvent) => {
    const mobileSelectionMode = mobileCanvasControlsActive && mobileCanvasMode === "select";
    if (event.ctrlKey || event.metaKey || event.shiftKey || mobileSelectionMode) {
      toggleNodeSelectionForDetails(nodeId);
      return;
    }
    selectNodeForDetails(nodeId);
  };

  const selectNodeForDragStart = (nodeId: string) => {
    if (selectedNodeIds.includes(nodeId)) {
      if (nodeId !== selectedNodeId) {
        applyPrimarySelection(nodeId, selectedNodeIds);
      }
      return;
    }
    selectNodeForDetails(nodeId);
  };

  const handleCanvasBlankClick = (event: ReactMouseEvent<Element>) => {
    if (skipNextCanvasBlankClickRef.current) {
      skipNextCanvasBlankClickRef.current = false;
      return;
    }
    const target = event.target;
    if (
      target instanceof HTMLElement &&
      target.closest(
        [
          "[data-workflow-node-id]",
          "[data-node-action]",
          "[data-workflow-target-node-id]",
          "[data-canvas-control]",
          "button",
          "a",
          "input",
          "textarea",
          "select",
          "label",
          "[role='button']",
        ].join(","),
      )
    ) {
      return;
    }
    clearMultiSelection();
    if (mobileCanvasControlsActive && mobileCanvasMode === "select") {
      setMobileCanvasMode("browse");
    }
  };

  const replaceSelectionFromBox = (nodeIds: string[]) => {
    skipNextCanvasBlankClickRef.current = true;
    window.setTimeout(() => {
      skipNextCanvasBlankClickRef.current = false;
    }, 0);
    const nextSelection = replaceSelectedNodeIdsFromBox(nodeIds, selectedNodeId);
    if (!nextSelection.primaryNodeId) {
      setSelectedNodeId(null);
      setSelectedNodeIds([]);
      return;
    }
    const primaryNodeId = nextSelection.primaryNodeId;
    if (primaryNodeId === selectedNode?.id || !draftDirty) {
      applyPrimarySelection(primaryNodeId, nextSelection.selectedNodeIds);
      return;
    }
    void (async () => {
      try {
        await flushSelectedDraft();
        applyPrimarySelection(primaryNodeId, nextSelection.selectedNodeIds);
      } catch {
        // Mutations already surface ApiError.detail in local error state.
      }
    })();
  };

  const getCurrentWorkflow = () =>
    queryClient.getQueryData<ProductWorkflow>(["product-workflow", productId]) ?? workflow ?? null;

  const workflowNodeIdsContainProductContext = (nodeIds: string[]) => {
    const nodeIdSet = new Set(nodeIds);
    return Boolean(
      getCurrentWorkflow()?.nodes.some((node) => nodeIdSet.has(node.id) && node.node_type === "product_context"),
    );
  };

  const setWorkflowCache = (nextWorkflow: ProductWorkflow) => {
    workflowHistorySignatureRef.current = getWorkflowStructureSignature(nextWorkflow);
    queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
  };

  const pushUndoStep = (step: WorkflowHistoryStep) => {
    if (
      (step.kind === "deleteNodes" && !step.nodeIds.length) ||
      (step.kind === "restoreNodes" && !step.nodes.length) ||
      (step.kind === "deleteEdges" && !step.edgeIds.length) ||
      (step.kind === "restoreEdges" && !step.edges.length) ||
      (step.kind === "moveNodes" && !step.moves.length)
    ) {
      return;
    }
    undoStackRef.current.push(step);
    redoStackRef.current = [];
  };

  const selectCreatedNodes = (previousWorkflow: ProductWorkflow | null, nextWorkflow: ProductWorkflow) => {
    const previousNodeIds = new Set(previousWorkflow?.nodes.map((node) => node.id) ?? []);
    const createdNodes = nextWorkflow.nodes.filter((node) => !previousNodeIds.has(node.id));
    if (!createdNodes.length) {
      return;
    }
    setSelectedNodeId(createdNodes[0].id);
    setSelectedNodeIds(createdNodes.map((node) => node.id));
    setActiveSidebarTab("details");
  };

  const executeDeleteNodesStep = async (
    step: Extract<WorkflowHistoryStep, { kind: "deleteNodes" }>,
  ): Promise<WorkflowHistoryStep> => {
    const currentWorkflow = getCurrentWorkflow();
    const nodeIdSet = new Set(step.nodeIds);
    const nodesToRestore = currentWorkflow?.nodes.filter((node) => nodeIdSet.has(node.id)) ?? [];
    const edgesToRestore = getInternalWorkflowEdges(currentWorkflow?.edges ?? [], nodeIdSet);
    let nextWorkflow = currentWorkflow;
    for (const nodeId of step.nodeIds) {
      if (!nextWorkflow?.nodes.some((node) => node.id === nodeId)) {
        continue;
      }
      nextWorkflow = await api.deleteWorkflowNode(nodeId);
      setWorkflowCache(nextWorkflow);
    }
    if (nextWorkflow) {
      const nextPrimaryNodeId = nextWorkflow.nodes[0]?.id ?? null;
      setSelectedNodeId(nextPrimaryNodeId);
      setSelectedNodeIds(clearSelectedNodeGroup(nextPrimaryNodeId));
    }
    return createRestoreNodesStep(nodesToRestore, edgesToRestore);
  };

  const executeRestoreNodesStep = async (
    step: Extract<WorkflowHistoryStep, { kind: "restoreNodes" }>,
  ): Promise<WorkflowHistoryStep> => {
    const oldToNewNodeIds = new Map<string, string>();
    let nextWorkflow = getCurrentWorkflow();
    const createdNodeIds: string[] = [];
    for (const node of step.nodes) {
      const previousNodeIds = new Set(nextWorkflow?.nodes.map((workflowNode) => workflowNode.id) ?? []);
      nextWorkflow = await api.createWorkflowNode(productId, {
        node_type: node.node_type,
        title: node.title,
        position_x: node.position_x,
        position_y: node.position_y,
        config_json: node.config_json,
      });
      setWorkflowCache(nextWorkflow);
      const createdNode = nextWorkflow.nodes.find((workflowNode) => !previousNodeIds.has(workflowNode.id));
      if (createdNode) {
        oldToNewNodeIds.set(node.oldId, createdNode.id);
        createdNodeIds.push(createdNode.id);
      }
    }
    for (const edge of step.edges) {
      const sourceNodeId = oldToNewNodeIds.get(edge.source_node_id);
      const targetNodeId = oldToNewNodeIds.get(edge.target_node_id);
      if (!sourceNodeId || !targetNodeId) {
        continue;
      }
      nextWorkflow = await api.createWorkflowEdge(productId, {
        source_node_id: sourceNodeId,
        target_node_id: targetNodeId,
        source_handle: edge.source_handle ?? undefined,
        target_handle: edge.target_handle ?? undefined,
      });
      setWorkflowCache(nextWorkflow);
    }
    if (nextWorkflow) {
      setSelectedNodeId(createdNodeIds[0] ?? null);
      setSelectedNodeIds(createdNodeIds);
      setActiveSidebarTab("details");
    }
    return { kind: "deleteNodes", nodeIds: createdNodeIds };
  };

  const executeDeleteEdgesStep = async (
    step: Extract<WorkflowHistoryStep, { kind: "deleteEdges" }>,
  ): Promise<WorkflowHistoryStep> => {
    const currentWorkflow = getCurrentWorkflow();
    const edgeIdSet = new Set(step.edgeIds);
    const edgesToRestore = currentWorkflow?.edges.filter((edge) => edgeIdSet.has(edge.id)) ?? [];
    let nextWorkflow = currentWorkflow;
    for (const edgeId of step.edgeIds) {
      if (!nextWorkflow?.edges.some((edge) => edge.id === edgeId)) {
        continue;
      }
      nextWorkflow = await api.deleteWorkflowEdge(edgeId);
      setWorkflowCache(nextWorkflow);
    }
    return createRestoreEdgesStep(edgesToRestore);
  };

  const executeRestoreEdgesStep = async (
    step: Extract<WorkflowHistoryStep, { kind: "restoreEdges" }>,
  ): Promise<WorkflowHistoryStep> => {
    let nextWorkflow = getCurrentWorkflow();
    const createdEdgeIds: string[] = [];
    for (const edge of step.edges) {
      const previousEdgeIds = new Set(nextWorkflow?.edges.map((workflowEdge) => workflowEdge.id) ?? []);
      nextWorkflow = await api.createWorkflowEdge(productId, {
        source_node_id: edge.source_node_id,
        target_node_id: edge.target_node_id,
        source_handle: edge.source_handle ?? undefined,
        target_handle: edge.target_handle ?? undefined,
      });
      setWorkflowCache(nextWorkflow);
      const createdEdge = nextWorkflow.edges.find((workflowEdge) => !previousEdgeIds.has(workflowEdge.id));
      if (createdEdge) {
        createdEdgeIds.push(createdEdge.id);
      }
    }
    return { kind: "deleteEdges", edgeIds: createdEdgeIds };
  };

  const executeMoveNodesStep = async (
    step: Extract<WorkflowHistoryStep, { kind: "moveNodes" }>,
  ): Promise<WorkflowHistoryStep> => {
    for (const move of step.moves) {
      const nextWorkflow = await api.updateWorkflowNode(move.nodeId, {
        position_x: move.to.x,
        position_y: move.to.y,
      });
      setWorkflowCache(nextWorkflow);
    }
    return {
      kind: "moveNodes",
      moves: step.moves.map((move) => ({
        nodeId: move.nodeId,
        from: move.to,
        to: move.from,
      })),
    };
  };

  const executeWorkflowHistoryStep = (step: WorkflowHistoryStep): Promise<WorkflowHistoryStep> => {
    if (step.kind === "deleteNodes") {
      return executeDeleteNodesStep(step);
    }
    if (step.kind === "restoreNodes") {
      return executeRestoreNodesStep(step);
    }
    if (step.kind === "deleteEdges") {
      return executeDeleteEdgesStep(step);
    }
    if (step.kind === "restoreEdges") {
      return executeRestoreEdgesStep(step);
    }
    return executeMoveNodesStep(step);
  };

  const executeHistoryDirection = async (direction: "undo" | "redo", expectedStep?: WorkflowHistoryStep) => {
    const sourceStack = direction === "undo" ? undoStackRef.current : redoStackRef.current;
    const targetStack = direction === "undo" ? redoStackRef.current : undoStackRef.current;
    const step = sourceStack[sourceStack.length - 1] ?? null;
    if (!step || (expectedStep && step !== expectedStep)) {
      setPendingHistoryAction(null);
      return;
    }
    sourceStack.pop();
    setHistoryActionBusy(true);
    try {
      const counterpart = await executeWorkflowHistoryStep(step);
      targetStack.push(counterpart);
      setPendingHistoryAction(null);
      setError("");
    } catch (mutationError) {
      sourceStack.push(step);
      setPendingHistoryAction(null);
      setError(mutationError instanceof ApiError ? mutationError.detail : t("detail.error.historyAction"));
    } finally {
      setHistoryActionBusy(false);
    }
  };

  const requestHistoryDirection = (direction: "undo" | "redo") => {
    const stack = direction === "undo" ? undoStackRef.current : redoStackRef.current;
    const step = stack[stack.length - 1] ?? null;
    if (!step) {
      return;
    }
    if (workflowHistoryStepRequiresConfirmation(step)) {
      setPendingHistoryAction({ direction, step });
      return;
    }
    void executeHistoryDirection(direction, step);
  };

  const runWorkflowMutation = useMutation({
    mutationFn: (startNodeId?: string) =>
      api.runProductWorkflow(
        productId,
        startNodeId ? { start_node_id: startNodeId } : {},
      ),
    onSuccess: async (nextWorkflow) => {
      setError(
        nextWorkflow.runs[0]?.status === "failed"
          ? (nextWorkflow.runs[0].failure_reason ?? "")
          : "",
      );
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      await refreshProductArtifacts();
    },
    onError: (mutationError) => {
      setPendingDeleteAction(null);
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.runWorkflow"),
      );
    },
  });

  const cancelWorkflowRunMutation = useMutation({
    mutationFn: (runId: string) => api.cancelProductWorkflowRun(productId, runId),
    onSuccess: async (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      await queryClient.invalidateQueries({ queryKey: ["product-workflow-status", productId] });
      await queryClient.invalidateQueries({ queryKey: ["generation-queue"] });
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.cancelWorkflow"),
      );
    },
  });

  const retryWorkflowRunMutation = useMutation({
    mutationFn: (runId: string) => api.retryProductWorkflowRun(productId, runId),
    onSuccess: (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.retryWorkflow"),
      );
    },
  });

  const createNodeMutation = useMutation({
    mutationFn: (type: WorkflowNodeType) => {
      const currentWorkflow = workflowQuery.data;
      if (!currentWorkflow) {
        throw new Error(t("detail.error.workflowNotLoaded"));
      }
      const siblingCount = currentWorkflow.nodes.filter(
        (node) => node.node_type === type,
      ).length;
      const nextPosition = workflowCanvasRef.current?.getViewportCenterNodePosition() ?? { x: 120, y: 120 };
      return api.createWorkflowNode(productId, {
        node_type: type,
        title: defaultTitleForType(type, siblingCount + 1),
        position_x: nextPosition.x,
        position_y: nextPosition.y,
        config_json: defaultConfigForType(type),
      });
    },
    onSuccess: (nextWorkflow) => {
      setError("");
      setWorkflowCache(nextWorkflow);
      const newest = [...nextWorkflow.nodes].sort(
        (a, b) =>
          new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
      )[0];
      if (newest) {
        pushUndoStep({ kind: "deleteNodes", nodeIds: [newest.id] });
      }
      setSelectedNodeId(newest?.id ?? null);
      setSelectedNodeIds(newest ? [newest.id] : []);
      setActiveSidebarTab("details");
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.createNode"),
      );
    },
  });

  const applyTemplateGroupMutation = useMutation({
    mutationFn: async (template: CanvasTemplateSummary) => {
      await flushSelectedDraft();
      const previousWorkflow = queryClient.getQueryData<ProductWorkflow>(["product-workflow", productId]) ?? workflow;
      const previousNodeIds = new Set(previousWorkflow?.nodes.map((node) => node.id) ?? []);
      const nextPosition = workflowCanvasRef.current?.getViewportCenterNodePosition() ?? { x: 120, y: 120 };
      const nextWorkflow = await api.applyWorkflowTemplateGroup(productId, {
        template_key: template.key,
        position_x: nextPosition.x,
        position_y: nextPosition.y,
        template_language: locale,
      });
      return { nextWorkflow, previousNodeIds };
    },
    onSuccess: async ({ nextWorkflow, previousNodeIds }) => {
      setError("");
      setWorkflowCache(nextWorkflow);
      await queryClient.invalidateQueries({ queryKey: ["product-workflow", productId] });
      const createdNodes = nextWorkflow.nodes.filter((node) => !previousNodeIds.has(node.id));
      const selectedCreatedNode =
        createdNodes.find((node) => node.node_type === "copy_generation") ??
        createdNodes.find((node) => node.node_type === "image_generation") ??
        createdNodes[0] ??
        null;
      pushUndoStep({ kind: "deleteNodes", nodeIds: createdNodes.map((node) => node.id) });
      setSelectedNodeId(selectedCreatedNode?.id ?? null);
      setSelectedNodeIds(selectedCreatedNode ? [selectedCreatedNode.id] : []);
      setActiveSidebarTab("details");
      if (createdNodes.length > 0) {
        const refreshedWorkflow =
          queryClient.getQueryData<ProductWorkflow>(["product-workflow", productId]) ?? nextWorkflow;
        setPendingTemplateAutoLayoutSignature(getWorkflowStructureSignature(refreshedWorkflow));
      }
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.applyTemplate"),
      );
    },
  });

  const duplicateNodeGroupMutation = useMutation({
    mutationFn: async ({ nodeIds, source }: { nodeIds: string[]; source: "paste" | "duplicate" }) => {
      if (!nodeIds.length) {
        throw new Error(t("detail.error.selectNodesToDuplicate"));
      }
      await flushSelectedDraft();
      const previousWorkflow = getCurrentWorkflow();
      const nodeIdSet = new Set(nodeIds);
      const hasDuplicableNodes = Boolean(
        previousWorkflow?.nodes.some((node) => nodeIdSet.has(node.id) && node.node_type !== "product_context"),
      );
      if (!hasDuplicableNodes) {
        throw new Error(t("detail.error.selectNodesToDuplicate"));
      }
      const nextWorkflow = await api.duplicateWorkflowNodeGroup(productId, {
        node_ids: nodeIds,
        offset_x: 56,
        offset_y: 56,
      });
      return { nextWorkflow, previousWorkflow, source };
    },
    onSuccess: ({ nextWorkflow, previousWorkflow, source }) => {
      const previousNodeIds = new Set(previousWorkflow?.nodes.map((node) => node.id) ?? []);
      const createdNodeIds = nextWorkflow.nodes
        .filter((node) => !previousNodeIds.has(node.id))
        .map((node) => node.id);
      setError("");
      setNotice(
        source === "paste"
          ? t("detail.notice.pastedNodes", { count: createdNodeIds.length })
          : t("detail.notice.duplicatedNodes", { count: createdNodeIds.length }),
      );
      setWorkflowCache(nextWorkflow);
      pushUndoStep({ kind: "deleteNodes", nodeIds: createdNodeIds });
      selectCreatedNodes(previousWorkflow, nextWorkflow);
    },
    onError: (mutationError) => {
      setNotice("");
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : mutationError instanceof Error
            ? mutationError.message
            : t("detail.error.duplicateNodes"),
      );
    },
  });

  const createUserTemplateGroupMutation = useMutation({
    mutationFn: async () => {
      if (selectedNodeIds.length < 2) {
        throw new Error(t("detail.error.selectNodesToSave"));
      }
      if (workflowNodeIdsContainProductContext(selectedNodeIds)) {
        throw new Error(t("detail.error.templateContainsProductContext"));
      }
      const title = templateSaveTitle.trim();
      if (!title) {
        throw new Error(t("detail.error.templateNameRequired"));
      }
      await flushSelectedDraft();
      return api.createUserTemplateGroup(productId, {
        title,
        description: templateSaveDescription.trim() || undefined,
        node_ids: selectedNodeIds,
      });
    },
    onSuccess: async () => {
      setError("");
      setTemplateSaveOpen(false);
      setTemplateSaveTitle("");
      setTemplateSaveDescription("");
      await queryClient.invalidateQueries({ queryKey: ["canvas-templates"] });
      setActiveSidebarTab("templates");
      setSidebarCollapsed(false);
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : mutationError instanceof Error
            ? mutationError.message
            : t("detail.error.saveTemplate"),
      );
    },
  });

  const updateUserTemplateGroupMutation = useMutation({
    mutationFn: ({ templateId, title }: { templateId: string; title: string }) =>
      api.updateUserTemplateGroup(templateId, { title }),
    onSuccess: async () => {
      setError("");
      await queryClient.invalidateQueries({ queryKey: ["canvas-templates"] });
    },
    onError: (mutationError) => {
      setError(mutationError instanceof ApiError ? mutationError.detail : t("detail.error.updateTemplate"));
    },
  });

  const archiveUserTemplateGroupMutation = useMutation({
    mutationFn: (templateId: string) => api.archiveUserTemplateGroup(templateId),
    onSuccess: async () => {
      setError("");
      setPendingDeleteAction(null);
      await queryClient.invalidateQueries({ queryKey: ["canvas-templates"] });
    },
    onError: (mutationError) => {
      setPendingDeleteAction(null);
      setError(mutationError instanceof ApiError ? mutationError.detail : t("detail.error.deleteTemplate"));
    },
  });

  const updateNodeConfigMutation = useMutation({
    mutationFn: (node: WorkflowNode) =>
      api.updateWorkflowNode(node.id, {
        title: draft.title,
        config_json: nodeConfigFromDraft(node, draft, imageToolAllowedFields),
    }),
    onSuccess: (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
    },
    onError: (mutationError) => {
      setSaveStatus("failed");
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.saveNode"),
      );
    },
  });

  const updateNodeCopyMutation = useMutation({
    mutationFn: (node: WorkflowNode) => {
      if (!draft.copyStructuredPayload) {
        throw new Error(t("detail.error.missingStructuredCopy"));
      }
      return api.updateWorkflowNodeCopy(node.id, {
        structured_payload: draft.copyStructuredPayload,
      });
    },
    onSuccess: async (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      setSelectedNodeIds(clearSelectedNodeGroup(selectedNodeId));
      await refreshProductArtifacts();
    },
    onError: (mutationError) => {
      setSaveStatus("failed");
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.saveCopy"),
      );
    },
  });

  const updateNodePositionMutation = useMutation({
    scope: { id: `product-workflow-node-position-${productId}` },
    mutationFn: (input: {
      node: WorkflowNode;
      position_x: number;
      position_y: number;
      mutationVersion: number;
      moveGroupId: string;
      moveGroupSize: number;
      rollbackWorkflow?: ProductWorkflow;
    }) =>
      api.updateWorkflowNode(input.node.id, {
        position_x: input.position_x,
        position_y: input.position_y,
      }),
    onMutate: async (input) => {
      await queryClient.cancelQueries({
        queryKey: ["product-workflow", productId],
      });
      const previous = queryClient.getQueryData<ProductWorkflow>([
        "product-workflow",
        productId,
      ]);
      return { previous: input.rollbackWorkflow ?? previous };
    },
    onSuccess: (nextWorkflow, input) => {
      if (!workflowCanvasRef.current?.acceptNodePositionMutation(input.node.id, input.mutationVersion)) {
        return;
      }
      setError("");
      const updatedNode = nextWorkflow.nodes.find((node) => node.id === input.node.id);
      workflowHistorySignatureRef.current = getWorkflowStructureSignature(nextWorkflow);
      queryClient.setQueryData<ProductWorkflow>(
        ["product-workflow", productId],
        (current) => {
          if (!current || !updatedNode) {
            return nextWorkflow;
          }
          return {
            ...current,
            nodes: current.nodes.map((node) => (node.id === updatedNode.id ? updatedNode : node)),
            edges: nextWorkflow.edges,
            runs: nextWorkflow.runs,
            updated_at: nextWorkflow.updated_at,
          };
        },
      );
      workflowCanvasRef.current.clearOptimisticNodePosition(input.node.id);
      const moveGroup = pendingMoveGroupsRef.current[input.moveGroupId] ?? {
        expected: input.moveGroupSize,
        moves: [],
      };
      moveGroup.moves.push({
        nodeId: input.node.id,
        from: { x: input.position_x, y: input.position_y },
        to: { x: input.node.position_x, y: input.node.position_y },
      });
      pendingMoveGroupsRef.current[input.moveGroupId] = moveGroup;
      if (moveGroup.moves.length >= moveGroup.expected) {
        pushUndoStep({ kind: "moveNodes", moves: moveGroup.moves });
        delete pendingMoveGroupsRef.current[input.moveGroupId];
      }
    },
    onError: (mutationError, _input, context) => {
      if (!workflowCanvasRef.current?.acceptNodePositionMutation(_input.node.id, _input.mutationVersion)) {
        return;
      }
      if (context?.previous) {
        setWorkflowCache(context.previous);
      }
      workflowCanvasRef.current.clearOptimisticNodePosition(_input.node.id);
      delete pendingMoveGroupsRef.current[_input.moveGroupId];
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.moveNode"),
      );
    },
  });

  const createEdgeMutation = useMutation({
    mutationFn: async (input: { sourceNodeId: string; targetNodeId: string }) => {
      const currentWorkflow = queryClient.getQueryData<ProductWorkflow>(["product-workflow", productId]) ?? workflow;
      const previousEdgeIds = new Set(currentWorkflow?.edges.map((edge) => edge.id) ?? []);
      const source = currentWorkflow?.nodes.find((node) => node.id === input.sourceNodeId);
      const target = currentWorkflow?.nodes.find((node) => node.id === input.targetNodeId);
      if (source?.node_type === "reference_image" && target?.node_type === "reference_image") {
        throw new Error(connectionDescription(source, target, t));
      }
      const nextWorkflow = await api.createWorkflowEdge(productId, {
        source_node_id: input.sourceNodeId,
        target_node_id: input.targetNodeId,
        source_handle: "output",
        target_handle: "input",
      });
      return { nextWorkflow, sourceNodeId: input.sourceNodeId, targetNodeId: input.targetNodeId, previousEdgeIds };
    },
    onSuccess: ({ nextWorkflow, sourceNodeId, targetNodeId, previousEdgeIds }) => {
      const source = nextWorkflow.nodes.find((node) => node.id === sourceNodeId);
      const target = nextWorkflow.nodes.find((node) => node.id === targetNodeId);
      const createdEdgeIds = nextWorkflow.edges
        .filter((edge) => !previousEdgeIds.has(edge.id))
        .map((edge) => edge.id);
      setError("");
      setNotice(source && target ? connectionDescription(source, target, t) : "");
      setWorkflowCache(nextWorkflow);
      pushUndoStep({ kind: "deleteEdges", edgeIds: createdEdgeIds });
      setSelectedNodeIds(clearSelectedNodeGroup(selectedNodeId));
    },
    onError: (mutationError) => {
      setNotice("");
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : mutationError instanceof Error
            ? mutationError.message
          : t("detail.error.connectNode"),
      );
    },
  });

  const deleteEdgeMutation = useMutation({
    mutationFn: async (edgeId: string) => {
      const currentWorkflow = getCurrentWorkflow();
      const edge = currentWorkflow?.edges.find((workflowEdge) => workflowEdge.id === edgeId);
      const restoreStep: WorkflowHistoryStep = edge
        ? { kind: "restoreEdges", edges: [workflowEdgeToRestorableEdge(edge)] }
        : { kind: "restoreEdges", edges: [] };
      const nextWorkflow = await api.deleteWorkflowEdge(edgeId);
      return { nextWorkflow, restoreStep };
    },
    onSuccess: ({ nextWorkflow, restoreStep }) => {
      setError("");
      setNotice("");
      setWorkflowCache(nextWorkflow);
      pushUndoStep(restoreStep);
      setSelectedNodeIds(clearSelectedNodeGroup(selectedNodeId));
    },
    onError: (mutationError) => {
      setPendingDeleteAction(null);
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.deleteEdge"),
      );
    },
  });

  const deleteNodeMutation = useMutation({
    mutationFn: async (nodeId: string) => {
      const currentWorkflow = getCurrentWorkflow();
      const node = currentWorkflow?.nodes.find((workflowNode) => workflowNode.id === nodeId);
      if (node?.node_type === "product_context") {
        throw new Error(t("detail.error.productContextProtected"));
      }
      const nodeIds = new Set(node ? [node.id] : []);
      const restoreStep = createRestoreNodesStep(
        node ? [node] : [],
        getInternalWorkflowEdges(currentWorkflow?.edges ?? [], nodeIds),
      );
      const nextWorkflow = await api.deleteWorkflowNode(nodeId);
      return { nextWorkflow, nodeId, restoreStep };
    },
    onSuccess: ({ nextWorkflow, nodeId, restoreStep }) => {
      setError("");
      setPendingDeleteAction(null);
      setWorkflowCache(nextWorkflow);
      pushUndoStep(restoreStep);
      const fallbackPrimaryNodeId = selectedNodeId === nodeId ? nextWorkflow.nodes[0]?.id ?? null : selectedNodeId;
      const nextSelection = deleteNodeFromSelection(selectedNodeIds, nodeId, fallbackPrimaryNodeId);
      setSelectedNodeId(nextSelection.primaryNodeId);
      setSelectedNodeIds(nextSelection.selectedNodeIds);
    },
    onError: (mutationError) => {
      setPendingDeleteAction(null);
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.deleteNode"),
      );
    },
  });

  const handleDeleteNode = (node: WorkflowNode) => {
    if (node.node_type === "product_context") {
      setError(t("detail.error.productContextProtected"));
      return;
    }
    setPendingDeleteAction({ kind: "node", node });
  };

  const deleteSelectedNodesMutation = useMutation({
    mutationFn: async (nodeIds: string[]) => {
      if (nodeIds.length < 2) {
        throw new Error(t("detail.error.selectNodesToDelete"));
      }
      if (workflowNodeIdsContainProductContext(nodeIds)) {
        throw new Error(t("detail.error.productContextProtected"));
      }
      const currentWorkflow = getCurrentWorkflow();
      const nodeIdSet = new Set(nodeIds);
      const restoreStep = createRestoreNodesStep(
        currentWorkflow?.nodes.filter((node) => nodeIdSet.has(node.id)) ?? [],
        getInternalWorkflowEdges(currentWorkflow?.edges ?? [], nodeIdSet),
      );
      let nextWorkflow: ProductWorkflow | null = null;
      for (const nodeId of nodeIds) {
        nextWorkflow = await api.deleteWorkflowNode(nodeId);
      }
      if (!nextWorkflow) {
        throw new Error(t("detail.error.deleteNode"));
      }
      return { nextWorkflow, restoreStep };
    },
    onSuccess: ({ nextWorkflow, restoreStep }) => {
      setError("");
      setPendingDeleteAction(null);
      setWorkflowCache(nextWorkflow);
      pushUndoStep(restoreStep);
      const nextPrimaryNodeId = nextWorkflow.nodes[0]?.id ?? null;
      setSelectedNodeId(nextPrimaryNodeId);
      setSelectedNodeIds(clearSelectedNodeGroup(nextPrimaryNodeId));
      setTemplateSaveOpen(false);
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : mutationError instanceof Error
            ? mutationError.message
            : t("detail.error.deleteSelectedNodes"),
      );
    },
  });

  const uploadNodeImageMutation = useMutation({
    mutationFn: (file: File) => {
      if (!selectedNode) {
        throw new Error(t("detail.error.selectImageNode"));
      }
      return api.uploadWorkflowNodeImage(selectedNode.id, {
        file,
        role: draft.role,
        label: draft.label,
      });
    },
    onSuccess: async (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      setSelectedNodeIds(clearSelectedNodeGroup(selectedNodeId));
      await refreshProductArtifacts();
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError ? mutationError.detail : t("detail.error.upload"),
      );
    },
  });

  const bindNodeImageMutation = useMutation({
    mutationFn: (input: { source_asset_id?: string; poster_variant_id?: string }) => {
      if (!selectedNode || selectedNode.node_type !== "reference_image") {
        throw new Error(t("detail.error.selectImageNode"));
      }
      return api.bindWorkflowNodeImage(selectedNode.id, input);
    },
    onSuccess: async (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      setSelectedNodeIds(clearSelectedNodeGroup(selectedNodeId));
      await refreshProductArtifacts();
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.fill"),
      );
    },
  });

  const selectedCopyHasOutput = Boolean(
    selectedNode?.node_type === "copy_generation" &&
      selectedNode.output_json &&
      outputText(selectedNode.output_json, "copy_set_id"),
  );

  const flushSelectedDraft = async () => {
    if (!selectedNode || !draftDirty) {
      return;
    }
    const saveVersion = draftVersionRef.current;
    setSaveStatus("saving");
    await updateNodeConfigMutation.mutateAsync(selectedNode);
    if (selectedCopyHasOutput) {
      if (draft.copyStructuredPayload?.summary.trim()) {
        await updateNodeCopyMutation.mutateAsync(selectedNode);
      }
    }
    if (draftVersionRef.current === saveVersion) {
      setDraftDirty(false);
      setSaveStatus("saved");
    } else {
      setSaveStatus("idle");
    }
  };

  useEffect(() => {
    if (!selectedNode || !draftDirty || workflowActive) {
      return;
    }
    setSaveStatus("saving");
    const timer = window.setTimeout(() => {
      void flushSelectedDraft();
    }, 700);
    return () => window.clearTimeout(timer);
  }, [draft, draftDirty, selectedNode?.id, workflowActive]);

  const handleRunWorkflow = async (startNodeId?: string) => {
    try {
      await flushSelectedDraft();
      await runWorkflowMutation.mutateAsync(startNodeId);
    } catch {
      // Mutations already surface ApiError.detail in local error state.
    }
  };

  const handleCancelWorkflowRun = (run: ProductWorkflow["runs"][number]) => {
    if (!run.is_cancelable || cancelWorkflowRunMutation.isPending) {
      return;
    }
    cancelWorkflowRunMutation.mutate(run.id);
  };

  const handleRetryWorkflowRun = (run: ProductWorkflow["runs"][number]) => {
    if (!run.is_retryable || retryWorkflowRunMutation.isPending) {
      return;
    }
    retryWorkflowRunMutation.mutate(run.id);
  };

  const openSidebarTab = (tab: SidebarTab) => {
    setActiveSidebarTab(tab);
    setSidebarCollapsed(false);
  };

  const startInspectorResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    disableBodyUserSelect();
    const startX = event.clientX;
    const startWidth = inspectorWidth;
    const onMove = (moveEvent: PointerEvent) => {
      const next = clamp(startWidth + startX - moveEvent.clientX, MIN_INSPECTOR_WIDTH, MAX_INSPECTOR_WIDTH);
      setInspectorWidth(next);
      window.localStorage.setItem("productflow.workflow.inspectorWidth", String(next));
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onUp);
      restoreBodyUserSelect();
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onUp);
  };

  const layoutMutationBusy =
    createNodeMutation.isPending ||
    applyTemplateGroupMutation.isPending ||
    duplicateNodeGroupMutation.isPending ||
    historyActionBusy ||
    updateNodeConfigMutation.isPending ||
    createEdgeMutation.isPending ||
    deleteEdgeMutation.isPending ||
    deleteNodeMutation.isPending ||
    deleteSelectedNodesMutation.isPending ||
    uploadNodeImageMutation.isPending ||
    bindNodeImageMutation.isPending ||
    updateNodeCopyMutation.isPending;
  const structureBusy = layoutMutationBusy || workflowActive;
  const runSubmissionPending = runWorkflowMutation.isPending || retryWorkflowRunMutation.isPending;
  const pendingStartNodeId = runWorkflowMutation.isPending ? (runWorkflowMutation.variables ?? null) : null;
  const fullWorkflowRunBusy = runSubmissionPending || workflowActive;
  const workflowRunActionBusyRunId =
    (cancelWorkflowRunMutation.isPending ? cancelWorkflowRunMutation.variables : null) ??
    (retryWorkflowRunMutation.isPending ? retryWorkflowRunMutation.variables : null);
  const pendingDeleteDialog = pendingDeleteAction
    ? (() => {
        if (pendingDeleteAction.kind === "node") {
          return {
            title: t("detail.confirm.deleteNodeTitle"),
            description: t("detail.confirm.deleteNode", { title: pendingDeleteAction.node.title }),
            busy: deleteNodeMutation.isPending,
          };
        }
        if (pendingDeleteAction.kind === "selectedNodes") {
          return {
            title: t("detail.confirm.deleteSelectedNodesTitle"),
            description: t("detail.confirm.deleteSelectedNodes", { count: pendingDeleteAction.count }),
            busy: deleteSelectedNodesMutation.isPending,
          };
        }
        return {
          title: t("detail.confirm.deleteTemplateTitle"),
          description: t("detail.confirm.deleteTemplate", { title: pendingDeleteAction.title }),
          busy: archiveUserTemplateGroupMutation.isPending,
        };
      })()
    : null;
  const pendingHistoryDeleteCount =
    pendingHistoryAction?.step.kind === "deleteNodes"
      ? pendingHistoryAction.step.nodeIds.length
      : pendingHistoryAction?.step.kind === "deleteEdges"
        ? pendingHistoryAction.step.edgeIds.length
        : 0;
  const pendingHistoryDialog = pendingHistoryAction
    ? {
        title: pendingHistoryAction.direction === "undo" ? t("detail.confirm.undoTitle") : t("detail.confirm.redoTitle"),
        description: t("detail.confirm.historyDeletes", { count: pendingHistoryDeleteCount }),
      }
    : null;

  const workflowActionPrimaryNode = (target: WorkflowCanvasActionTarget) => {
    const primaryNodeId = target.kind === "single" ? target.nodeId : target.primaryNodeId;
    return workflow?.nodes.find((node) => node.id === primaryNodeId) ?? null;
  };

  const workflowActionTargetNodes = (target: WorkflowCanvasActionTarget) => {
    const targetNodeIds = new Set(getWorkflowCanvasActionTargetNodeIds(target));
    return workflow?.nodes.filter((node) => targetNodeIds.has(node.id)) ?? [];
  };

  const workflowActionItems = (target: WorkflowCanvasActionTarget): WorkflowCanvasActionItem[] => {
    const primaryNode = workflowActionPrimaryNode(target);
    return buildWorkflowCanvasActionItems(target, {
      primaryNode,
      targetNodes: workflowActionTargetNodes(target),
      runActionState: primaryNode
        ? getWorkflowNodeRunActionState(primaryNode, {
            runSubmissionPending,
            pendingStartNodeId,
          })
        : null,
      structureBusy,
      duplicatePending: duplicateNodeGroupMutation.isPending,
      templatePending: createUserTemplateGroupMutation.isPending,
      deletePending: deleteNodeMutation.isPending || deleteSelectedNodesMutation.isPending,
    }).map((item) => {
      const label = item.label ?? (item.labelKey ? t(item.labelKey) : "");
      return {
        ...item,
        label,
        title: item.title ?? label,
      };
    });
  };

  const executeWorkflowCanvasAction = (actionId: WorkflowCanvasActionId, target: WorkflowCanvasActionTarget) => {
    const nodeIds = getWorkflowCanvasActionTargetNodeIds(target);
    if (actionId === "run") {
      if (target.kind === "single") {
        void handleRunWorkflow(target.nodeId);
      }
      return;
    }
    if (actionId === "duplicate") {
      duplicateNodeGroupMutation.mutate({ nodeIds, source: "duplicate" });
      return;
    }
    if (actionId === "fitSelected") {
      workflowCanvasRef.current?.fitNodeIds(nodeIds);
      return;
    }
    if (actionId === "saveTemplate") {
      if (target.kind === "group") {
        setSelectedNodeId(target.primaryNodeId);
        setSelectedNodeIds(target.nodeIds);
      }
      setTemplateSaveOpen(true);
      return;
    }
    if (nodeIds.length === 1) {
      const node = workflow?.nodes.find((workflowNode) => workflowNode.id === nodeIds[0]);
      if (node) {
        if (node.node_type === "product_context") {
          setError(t("detail.error.productContextProtected"));
          return;
        }
        setPendingDeleteAction({ kind: "node", node });
      }
      return;
    }
    if (nodeIds.length > 1) {
      setPendingDeleteAction({
        kind: "selectedNodes",
        nodeIds,
        count: nodeIds.length,
      });
    }
  };

  const getWorkflowNodeActionToolbar = (nodeId: string): WorkflowCanvasActionToolbar | null => {
    const target = getWorkflowCanvasActionTargetForNodeToolbar(nodeId, selectedNodeId, selectedNodeIds);
    if (!target) {
      return null;
    }
    const items = workflowActionItems(target);
    return items.length ? { target, items } : null;
  };

  const commitNodePosition = (input: NodePositionCommitInput) => {
    const rollbackWorkflow = queryClient.getQueryData<ProductWorkflow>([
      "product-workflow",
      productId,
    ]);
    queryClient.setQueryData<ProductWorkflow>(
      ["product-workflow", productId],
      (current) => {
        if (!current) {
          return current;
        }
        return {
          ...current,
          nodes: current.nodes.map((node) =>
            node.id === input.node.id
              ? {
                  ...node,
                  position_x: input.position_x,
                  position_y: input.position_y,
                }
              : node,
          ),
        };
      },
    );
    updateNodePositionMutation.mutate({
      ...input,
      rollbackWorkflow,
    });
  };

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const shortcut = getWorkflowKeyboardShortcut(event);
      if (!shortcut) {
        return;
      }
      if (!workflow) {
        return;
      }
      if (
        previewImage ||
        pendingDeleteAction ||
        pendingHistoryAction ||
        templateSaveOpen
      ) {
        return;
      }
      event.preventDefault();
      const shortcutNodeIds = getSelectedWorkflowShortcutNodeIds(selectedNodeIds, selectedNodeId);

      if (shortcut === "copy") {
        if (!shortcutNodeIds.length) {
          return;
        }
        workflowClipboardRef.current = { nodeIds: shortcutNodeIds };
        setNotice(t("detail.notice.copiedNodes", { count: shortcutNodeIds.length }));
        setError("");
        return;
      }

      if (structureBusy) {
        setError(t("detail.error.shortcutBusy"));
        return;
      }

      if (shortcut === "paste") {
        const clipboard = workflowClipboardRef.current;
        if (!clipboard?.nodeIds.length) {
          return;
        }
        duplicateNodeGroupMutation.mutate({ nodeIds: clipboard.nodeIds, source: "paste" });
        return;
      }

      if (shortcut === "duplicate") {
        if (!shortcutNodeIds.length) {
          return;
        }
        duplicateNodeGroupMutation.mutate({ nodeIds: shortcutNodeIds, source: "duplicate" });
        return;
      }

      if (shortcut === "delete") {
        if (!shortcutNodeIds.length) {
          return;
        }
        if (workflowNodeIdsContainProductContext(shortcutNodeIds)) {
          setError(t("detail.error.productContextProtected"));
          return;
        }
        if (shortcutNodeIds.length === 1) {
          const node = workflow.nodes.find((workflowNode) => workflowNode.id === shortcutNodeIds[0]);
          if (node) {
            setPendingDeleteAction({ kind: "node", node });
          }
          return;
        }
        setPendingDeleteAction({
          kind: "selectedNodes",
          nodeIds: shortcutNodeIds,
          count: shortcutNodeIds.length,
        });
        return;
      }

      requestHistoryDirection(shortcut);
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [
    duplicateNodeGroupMutation,
    pendingDeleteAction,
    pendingHistoryAction,
    previewImage,
    requestHistoryDirection,
    selectedNodeId,
    selectedNodeIds,
    structureBusy,
    t,
    templateSaveOpen,
    workflow,
  ]);

  useEffect(() => {
    if (!workflow?.nodes.length || !selectedNode) {
      return;
    }
    if (!mobileCanvasControlsActive) {
      return;
    }

    const layoutKey = `${productId}:${workflow.nodes.map((node) => node.id).join("|")}`;
    if (mobileInitialCanvasViewRef.current === layoutKey) {
      return;
    }
    mobileInitialCanvasViewRef.current = layoutKey;

    window.requestAnimationFrame(() => {
      workflowCanvasRef.current?.centerNode(selectedNode);
    });
  }, [mobileCanvasControlsActive, productId, selectedNode, workflow?.nodes]);

  if (productQuery.isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-white text-zinc-400 dark:bg-[#060a12] dark:text-slate-400">
        <Loader2 size={24} className="animate-spin" />
      </div>
    );
  }

  if (productQuery.isError || !productQuery.data) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-white dark:bg-[#060a12]">
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
          {t("detail.loadFailed")}
        </div>
      </div>
    );
  }

  const product = productQuery.data;
  const sourceImage = getSourceImageDownload(product, t);
  const latestRun = workflow?.runs[0] ?? null;
  const selectedNodeCancelableRun = getWorkflowNodeCancelableRun(workflow, selectedNode);
  const posters = historyQuery.data?.poster_variants ?? product.poster_variants;
  const posterSourceAssetIds = buildPosterSourceAssetMap({
    product,
    workflow,
    posters,
  });
  const referenceAssets = getVisibleReferenceAssets({
    product,
    posterSourceAssetIds,
    posters,
  });
  const artifactCount = posters.length + referenceAssets.length;
  const selectedReferenceNode =
    selectedNode?.node_type === "reference_image" ? selectedNode : null;
  const selectedGroupCount = selectedNodeIds.length;
  const workflowCanvasKeyboardShortcutsActive =
    !previewImage &&
    !pendingDeleteAction &&
    !pendingHistoryAction &&
    !templateSaveOpen;
  const fillReferenceBusy = bindNodeImageMutation.isPending;
  const queueOverview = queueOverviewQuery.data ?? null;
  const showQueueOverview = Boolean(queueOverview && queueOverview.active_count > 0);
  const canvasTemplates = canvasTemplatesQuery.data?.items ?? [];
  const userTemplateMutationBusy =
    createUserTemplateGroupMutation.isPending ||
    updateUserTemplateGroupMutation.isPending ||
    archiveUserTemplateGroupMutation.isPending;
  const autoLayoutBusy = structureBusy || !workflow || workflow.nodes.length === 0;

  const renderWorkflowToolbarButtons = () => (
    <>
      <span className="w-full text-center text-[10px] font-semibold leading-none text-slate-500">
        {t("detail.toolbar.runSection")}
      </span>
      <button
        type="button"
        onClick={() => void handleRunWorkflow(undefined)}
        disabled={fullWorkflowRunBusy || !workflow}
        className="btn-primary-spring flex w-full flex-col items-center rounded-lg px-1.5 py-2 text-xs font-semibold"
        title={fullWorkflowRunBusy ? t("detail.workflowRunning") : t("detail.runWorkflow")}
        aria-label={fullWorkflowRunBusy ? t("detail.workflowRunning") : t("detail.runWorkflow")}
      >
        {fullWorkflowRunBusy ? <Loader2 size={17} className="animate-spin" /> : <Play size={17} />}
        <span className="mt-1 leading-tight">{fullWorkflowRunBusy ? t("detail.running") : t("detail.runFullWorkflow")}</span>
      </button>
      <button
        type="button"
        onClick={() => workflowCanvasRef.current?.triggerAutoLayout()}
        disabled={autoLayoutBusy}
        className="btn-secondary-spring flex w-full flex-col items-center rounded-lg px-1.5 py-2 text-xs font-semibold"
        title={t("detail.autoLayout")}
        aria-label={t("detail.autoLayout")}
      >
        <Sparkles size={16} />
        <span className="mt-1 leading-tight">{t("detail.autoLayout")}</span>
      </button>
      <div className="my-1 h-px w-11 self-center bg-slate-200/70 dark:bg-slate-800" />
      <span className="w-full text-center text-[10px] font-semibold leading-none text-slate-500">
        {t("detail.toolbar.addSection")}
      </span>
    </>
  );

  const renderToolbarViewDivider = () => (
    <>
      <div className="my-1 h-px w-11 self-center bg-slate-200/70 dark:bg-slate-800" />
      <span className="w-full text-center text-[10px] font-semibold leading-none text-slate-500">
        {t("detail.toolbar.viewSection")}
      </span>
    </>
  );

  const renderSingleNodePanel = () => (
    <div className="space-y-3">
      {ADD_NODE_OPTIONS.map((option) => {
        const optionLabel = localizedWorkflowNodeTypeLabel(option.type, t);
        const description =
          option.type === "reference_image"
            ? t("detail.singleNode.description.referenceImage")
            : option.type === "copy_generation"
              ? t("detail.singleNode.description.copyGeneration")
              : t("detail.singleNode.description.imageGeneration");
        const creatingThisNode = createNodeMutation.isPending && createNodeMutation.variables === option.type;
        const NodeIcon = option.type === "reference_image"
          ? ImagePlus
          : option.type === "copy_generation"
            ? FileText
            : ImageIcon;
        return (
          <div
            key={option.type}
            className="config-bubble rounded-2xl p-4 shadow-sm transition-all hover:scale-[1.01]"
          >
            <div className="flex items-start">
              <span className="mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-xl bg-indigo-500/10 text-indigo-600 dark:bg-indigo-500/20 dark:text-violet-400">
                <NodeIcon size={16} />
              </span>
              <span className="ml-3 min-w-0 flex-1">
                <span className="block text-sm font-semibold text-zinc-900 dark:text-slate-100">{optionLabel}</span>
                <span className="mt-1 block text-xs leading-5 text-zinc-500 dark:text-slate-400">
                  {description}
                </span>
              </span>
            </div>
            <div className="mt-4 flex justify-end">
              <button
                type="button"
                onClick={() => createNodeMutation.mutate(option.type)}
                disabled={structureBusy || !workflow}
                className="btn-primary-spring inline-flex h-9 items-center rounded-xl px-4 text-xs font-semibold"
                title={t("detail.addNode", { label: optionLabel })}
                aria-label={t("detail.addNode", { label: optionLabel })}
              >
                {creatingThisNode ? (
                  <Loader2 size={13} className="mr-1.5 animate-spin" />
                ) : (
                  <Plus size={13} className="mr-1.5" />
                )}
                {t("detail.template.add")}
              </button>
            </div>
          </div>
        );
      })}
    </div>
  );

  const sidebarTabItems: Array<{
    key: SidebarTab;
    label: string;
    icon: ReactNode;
  }> = [
    { key: "singleNode", label: t("detail.tabSingleNode"), icon: <Plus size={16} /> },
    { key: "templates", label: t("detail.tabTemplates"), icon: <Layers3 size={16} /> },
    { key: "details", label: t("detail.tabDetails"), icon: <Settings2 size={16} /> },
    { key: "runs", label: t("detail.tabRuns"), icon: <CircleDot size={16} /> },
    { key: "images", label: t("detail.tabImages"), icon: <ImageIcon size={16} /> },
  ];

  const activeSidebarTabItem =
    sidebarTabItems.find((item) => item.key === activeSidebarTab) ??
    sidebarTabItems.find((item) => item.key === "details") ??
    sidebarTabItems[0];
  const mobileCanvasModeItems: Array<{
    key: CanvasInteractionMode;
    label: string;
    description: string;
    icon: ReactNode;
  }> = [
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

  const openMobileSidebarTab = (tab: SidebarTab) => {
    setActiveSidebarTab(tab);
    setMobileDetailsSheetOpen(true);
  };

  const renderDetailsPanelContent = () =>
    selectedNode ? (
      <InspectorPanel
        product={product}
        sourceImage={sourceImage}
        workflow={workflow}
        node={selectedNode}
        draft={draft}
        imageSizeOptions={imageSizeOptions}
        imageGenerationMaxDimension={imageGenerationMaxDimension}
        imageToolAllowedFields={imageToolAllowedFields}
        onPreviewImage={setPreviewImage}
        onDraftChange={handleDraftChange}
        onRun={() => void handleRunWorkflow(selectedNode.id)}
        onCancelRun={
          selectedNodeCancelableRun
            ? () => handleCancelWorkflowRun(selectedNodeCancelableRun)
            : null
        }
        saveStatus={saveStatus}
        onUploadImage={(file) => uploadNodeImageMutation.mutate(file)}
        onDelete={() => handleDeleteNode(selectedNode)}
        busy={structureBusy}
        cancelBusy={cancelWorkflowRunMutation.isPending}
        runActionState={getWorkflowNodeRunActionState(selectedNode, {
          runSubmissionPending,
          pendingStartNodeId,
        })}
      />
    ) : (
      <div className="glass-empty-state px-4 py-8 text-center text-xs text-zinc-500 dark:text-slate-400 flex flex-col items-center justify-center gap-2">
        <MousePointer2 size={18} className="text-indigo-500 opacity-70 dark:text-indigo-400" />
        <div>{t("detail.selectNodeHint")}</div>
      </div>
    );

  const renderSidebarPanelContent = () => (
    <>
      {activeSidebarTab === "singleNode" ? renderSingleNodePanel() : null}
      {activeSidebarTab === "details" ? renderDetailsPanelContent() : null}
      {activeSidebarTab === "runs" ? (
        <RunsPanel
          workflow={workflow}
          latestRun={latestRun}
          busyRunId={workflowRunActionBusyRunId ?? null}
          onRetryRun={handleRetryWorkflowRun}
        />
      ) : null}
      {activeSidebarTab === "images" ? (
        <ImagesPanel
          product={product}
          posters={posters}
          referenceAssets={referenceAssets}
          artifactCount={artifactCount}
          selectedReferenceNode={selectedReferenceNode}
          posterSourceAssetIds={posterSourceAssetIds}
          onPreviewImage={setPreviewImage}
          onFillFromSourceAsset={(sourceAssetId) =>
            bindNodeImageMutation.mutate({
              source_asset_id: sourceAssetId,
            })
          }
          onFillFromPoster={(posterId) =>
            bindNodeImageMutation.mutate({
              poster_variant_id: posterId,
            })
          }
          fillReferenceBusy={fillReferenceBusy}
        />
      ) : null}
      {activeSidebarTab === "templates" ? (
        <TemplateGroupsPanel
          templates={canvasTemplates}
          isLoading={canvasTemplatesQuery.isLoading}
          isError={canvasTemplatesQuery.isError}
          structureBusy={structureBusy || !workflow}
          applyBusy={applyTemplateGroupMutation.isPending}
          applyingTemplateKey={applyTemplateGroupMutation.variables?.key ?? null}
          onApplyTemplate={(template) => applyTemplateGroupMutation.mutate(template)}
          userTemplateBusy={userTemplateMutationBusy}
          onRenameUserTemplate={(template, title) => {
            if (template.user_template_id) {
              updateUserTemplateGroupMutation.mutate({
                templateId: template.user_template_id,
                title,
              });
            }
          }}
          onArchiveUserTemplate={(template) => {
            if (template.user_template_id) {
              setPendingDeleteAction({
                kind: "template",
                templateId: template.user_template_id,
                title: template.title,
              });
            }
          }}
        />
      ) : null}
    </>
  );

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-white text-sm text-zinc-900 dark:bg-[#060a12] dark:text-slate-100">
      {!topChromeCollapsed ? <TopNav onHome={() => navigate("/products")} breadcrumbs={product.name} /> : null}

      <main className="flex min-h-0 flex-1 flex-col border-t border-slate-200 bg-slate-50 dark:border-slate-800 dark:bg-[#060a12]">
        {error ? (
          <div className="z-20 border-b border-red-200 bg-red-50 px-4 py-2 text-xs text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
            <AlertCircle size={14} className="mr-2 inline" /> {error}
          </div>
        ) : null}
        {!error && notice ? (
          <div className="z-20 border-b border-blue-200 bg-blue-50 px-4 py-2 text-xs text-blue-700 dark:border-blue-400/35 dark:bg-blue-500/10 dark:text-blue-200">
            <AlertCircle size={14} className="mr-2 inline" /> {notice}
          </div>
        ) : null}
        {showQueueOverview && queueOverview ? (
          <div className="z-20 border-b border-amber-200 bg-amber-50 px-4 py-2 text-xs text-amber-800 dark:border-amber-400/35 dark:bg-amber-500/10 dark:text-amber-200">
            {t("detail.queueOverview", {
              running: queueOverview.running_count,
              queued: queueOverview.queued_count,
              active: queueOverview.active_count,
              max: queueOverview.max_concurrent_tasks,
            })}
          </div>
        ) : null}

        <div className="relative flex min-h-0 flex-1 overflow-hidden bg-slate-50 dark:bg-[#0b1220]">
          <div className="absolute inset-0 bg-gradient-to-br from-white/60 via-transparent to-indigo-50/40 dark:from-[#060a12]/78 dark:via-transparent dark:to-[#151f33]/70" />
          <section
            className="relative z-10 min-w-0 flex-1 overflow-hidden transition-[padding] duration-300 ease-out"
            style={{
              paddingRight: mobileCanvasControlsActive ? 0 : (sidebarCollapsed ? 96 : 72 + inspectorWidth + 24),
            }}
          >
            <div data-canvas-control className="pointer-events-none absolute right-3 top-3 z-30 lg:right-4 lg:top-4">
              <button
                type="button"
                onClick={() => setTopChromeCollapsed((collapsed) => !collapsed)}
                className="pointer-events-auto inline-flex h-11 w-11 items-center justify-center rounded-xl border border-zinc-200 bg-white/90 text-zinc-600 shadow-sm backdrop-blur transition-colors active:scale-[0.98] hover:bg-white hover:text-zinc-900 dark:border-slate-700/80 dark:bg-[#151f33]/92 dark:text-slate-300 dark:shadow-black/20 dark:hover:bg-[#1a2740] dark:hover:text-white lg:h-9 lg:w-9 lg:rounded-lg"
                aria-label={topChromeCollapsed ? t("detail.restoreCanvas") : t("detail.maximizeCanvas")}
                title={topChromeCollapsed ? t("detail.restoreCanvas") : t("detail.maximizeCanvas")}
              >
                {topChromeCollapsed ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
              </button>
            </div>
            <WorkflowCanvas
              ref={workflowCanvasRef}
              workflow={workflow}
              isLoading={workflowQuery.isLoading}
              selectedNodeId={selectedNodeId}
              selectedNodeIds={selectedNodeIds}
              structureBusy={structureBusy}
              mobileInteractionMode={mobileCanvasControlsActive ? mobileCanvasMode : "edit"}
              mobileCanvasControlsActive={mobileCanvasControlsActive}
              zoomStorageKey="productflow.workflow.zoom"
              initialZoom={initialWorkflowCanvasZoom}
              selectedGroupCount={selectedGroupCount}
              loadFailedLabel={t("detail.workflowLoadFailed")}
              deleteEdgeLabel={t("detail.deleteEdge")}
              inputHandleLabel={t("detail.inputHandle")}
              outputHandleLabel={t("detail.outputHandle")}
              zoomOutLabel={t("detail.zoomOut")}
              resetZoomLabel={t("detail.resetZoom")}
              zoomInLabel={t("detail.zoomIn")}
              fitViewLabel={t("detail.fitCanvas")}
              fitSelectionLabel={t("detail.fitSelection")}
              canvasControlsLabel={t("detail.canvasControls")}
              canvasMiniMapLabel={t("detail.canvasMiniMap")}
              snapToGridLabel={t("detail.snapToGrid")}
              autoLayoutLabel={t("detail.autoLayout")}
              snapToGrid={snapToGrid}
              onToggleSnapToGrid={() => setSnapToGrid((prev) => !prev)}
              onAutoLayout={() => workflowCanvasRef.current?.triggerAutoLayout()}
              onBlankClick={handleCanvasBlankClick}
              onSelectNode={selectNodeFromPointer}
              onNodeDragCompleteSelect={selectNodeForDragStart}
              getNodeDragGroup={(nodeId) => (selectedNodeIds.includes(nodeId) ? selectedNodeIds : [nodeId])}
              onSelectionBoxComplete={replaceSelectionFromBox}
              onNodePositionCommit={commitNodePosition}
              onConnectionCreate={(input) => createEdgeMutation.mutate(input)}
              onDeleteEdge={(edgeId) => deleteEdgeMutation.mutate(edgeId)}
              getNodeActionToolbar={getWorkflowNodeActionToolbar}
              onNodeAction={executeWorkflowCanvasAction}
              keyboardShortcutsActive={workflowCanvasKeyboardShortcutsActive}
              onClearSelection={clearMultiSelection}
              getNodeImage={(node) => getNodeImageDownload(node, product, t)}
            />
            {selectedGroupCount > 1 ? (
              <div data-canvas-control className="pointer-events-none absolute bottom-[calc(12.75rem+env(safe-area-inset-bottom))] left-3 right-3 z-30 lg:bottom-auto lg:left-1/2 lg:right-auto lg:top-4 lg:-translate-x-1/2">
                <div className="pointer-events-auto max-h-[calc(100dvh-16rem)] overflow-y-auto rounded-xl border border-indigo-200 bg-white/95 p-2.5 text-sm font-semibold text-indigo-700 shadow-lg shadow-indigo-950/10 backdrop-blur dark:border-violet-400/50 dark:bg-[#151f33]/95 dark:text-violet-100 dark:shadow-black/30 lg:max-h-none lg:min-w-[22rem] lg:overflow-visible">
                  <div className="flex items-center gap-2">
                    <Check size={16} strokeWidth={2.5} />
                    <span className="mr-auto">{t("detail.selectedCount", { count: selectedGroupCount })}</span>
                    <button
                      type="button"
                      onClick={clearMultiSelection}
                      className="inline-flex h-11 w-11 items-center justify-center rounded-lg border border-red-200 bg-red-50 text-red-600 shadow-sm transition-colors hover:border-red-300 hover:bg-red-100 hover:text-red-700 dark:border-red-400/40 dark:bg-red-500/10 dark:text-red-200 dark:hover:border-red-400/60 dark:hover:bg-red-500/16 dark:hover:text-red-100 lg:h-8 lg:w-8"
                      aria-label={t("detail.clearSelection")}
                      title={t("detail.clearSelection")}
                    >
                      <X size={18} strokeWidth={2.5} />
                    </button>
                  </div>
                  {templateSaveOpen ? (
                    <form
                      className="mt-2 grid gap-2 border-t border-indigo-100 pt-2 dark:border-violet-400/20"
                      onSubmit={(event) => {
                        event.preventDefault();
                        createUserTemplateGroupMutation.mutate();
                      }}
                    >
                      <input
                        value={templateSaveTitle}
                        onChange={(event) => setTemplateSaveTitle(event.target.value)}
                        className="h-11 rounded-lg border border-zinc-200 bg-white px-3 text-xs font-medium text-zinc-900 outline-none transition-colors placeholder:text-zinc-400 focus:border-indigo-300 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 lg:h-9"
                        placeholder={t("detail.templateName")}
                        maxLength={255}
                      />
                      <input
                        value={templateSaveDescription}
                        onChange={(event) => setTemplateSaveDescription(event.target.value)}
                        className="h-11 rounded-lg border border-zinc-200 bg-white px-3 text-xs font-medium text-zinc-900 outline-none transition-colors placeholder:text-zinc-400 focus:border-indigo-300 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 lg:h-9"
                        placeholder={t("detail.templateDescription")}
                        maxLength={1000}
                      />
                      <div className="flex justify-end gap-2">
                        <button
                          type="button"
                          onClick={() => setTemplateSaveOpen(false)}
                          className="h-11 rounded-lg px-3 text-xs font-semibold text-zinc-500 hover:bg-zinc-50 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white lg:h-8 lg:px-2.5"
                        >
                          {t("detail.cancel")}
                        </button>
                        <button
                          type="submit"
                          disabled={createUserTemplateGroupMutation.isPending}
                          className="inline-flex h-11 items-center rounded-lg bg-zinc-950 px-3 text-xs font-semibold text-white hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400 lg:h-8"
                        >
                          {t("detail.save")}
                        </button>
                      </div>
                    </form>
                  ) : null}
                </div>
              </div>
            ) : null}
          </section>

          {sidebarCollapsed ? (
            <div
              data-canvas-control
              className="absolute right-6 top-20 z-30 hidden w-[72px] flex-col items-center gap-2 rounded-[24px] shadow-2xl glass-inspector p-2 pb-3 lg:flex"
            >
              {renderWorkflowToolbarButtons()}
              <SidebarTabButton active={false} label={t("detail.tabSingleNode")} title={t("detail.tabSingleNode")} icon={<Plus size={17} />} onClick={() => openSidebarTab("singleNode")} />
              <SidebarTabButton active={false} label={t("detail.tabTemplates")} title={t("detail.tabTemplates")} icon={<Layers3 size={17} />} onClick={() => openSidebarTab("templates")} />
              {renderToolbarViewDivider()}
              <SidebarTabButton active={false} label={t("detail.tabDetails")} title={t("detail.tabDetails")} icon={<Eye size={17} />} onClick={() => openSidebarTab("details")} />
              <SidebarTabButton active={false} label={t("detail.tabRuns")} title={t("detail.runsTitle")} icon={<CircleDot size={17} />} onClick={() => openSidebarTab("runs")} />
              <SidebarTabButton active={false} label={t("detail.tabImages")} title={t("detail.tabImages")} icon={<ImageIcon size={17} />} onClick={() => openSidebarTab("images")} />

              <div className="mt-auto flex w-full justify-center border-t border-slate-200/40 pt-2 dark:border-white/5">
                <button
                  type="button"
                  onClick={() => setSidebarCollapsed(false)}
                  className="inline-flex h-9 w-9 items-center justify-center rounded-xl text-slate-400 transition-all hover:scale-105 hover:bg-white/40 hover:text-slate-800 dark:text-slate-400 dark:hover:bg-white/5 dark:hover:text-slate-200"
                  title={t("detail.expandSidebar")}
                  aria-label={t("detail.expandSidebar")}
                >
                  <ChevronLeft size={16} />
                </button>
              </div>
            </div>
          ) : (
          <div
            data-canvas-control
            className="absolute right-6 top-20 bottom-6 z-30 hidden rounded-[28px] shadow-[0_24px_50px_rgba(15,23,42,0.18)] dark:shadow-[0_32px_64px_rgba(0,0,0,0.45)] glass-inspector lg:flex animate-spring-slide-in"
            style={{ width: 72 + inspectorWidth }}
            >
            <div
              role="separator"
              aria-label={t("detail.resizeSidebar")}
              onPointerDown={startInspectorResize}
              className="group absolute left-0 top-0 z-30 flex h-full w-2.5 cursor-col-resize items-center justify-center"
            >
              <div className="h-12 w-[4px] rounded-full bg-slate-300 opacity-40 transition-all duration-300 group-hover:h-20 group-hover:opacity-100 dark:bg-slate-700 animate-handle-glow" />
            </div>

            <div className="flex w-[72px] shrink-0 flex-col items-center gap-2 border-r border-slate-200/40 bg-white/5 px-2 py-4 dark:border-white/5 dark:bg-black/10">
              {renderWorkflowToolbarButtons()}
              <SidebarTabButton
                active={activeSidebarTab === "singleNode"}
                label={t("detail.tabSingleNode")}
                title={t("detail.tabSingleNode")}
                icon={<Plus size={17} />}
                onClick={() => openSidebarTab("singleNode")}
              />
              <SidebarTabButton
                active={activeSidebarTab === "templates"}
                label={t("detail.tabTemplates")}
                title={t("detail.tabTemplates")}
                icon={<Layers3 size={17} />}
                onClick={() => openSidebarTab("templates")}
              />
              {renderToolbarViewDivider()}
              <SidebarTabButton
                active={activeSidebarTab === "details"}
                label={t("detail.tabDetails")}
                title={t("detail.tabDetails")}
                icon={<Eye size={17} />}
                onClick={() => openSidebarTab("details")}
              />
              <SidebarTabButton
                active={activeSidebarTab === "runs"}
                label={t("detail.tabRuns")}
                title={t("detail.runsTitle")}
                icon={<CircleDot size={17} />}
                onClick={() => openSidebarTab("runs")}
              />
              <SidebarTabButton
                active={activeSidebarTab === "images"}
                label={t("detail.tabImages")}
                title={t("detail.tabImages")}
                icon={<ImageIcon size={17} />}
                onClick={() => openSidebarTab("images")}
              />

              <div className="mt-auto flex w-full justify-center border-t border-slate-200/40 pt-2 dark:border-white/5">
                <button
                  type="button"
                  onClick={() => setSidebarCollapsed(true)}
                  className="btn-secondary-spring inline-flex h-9 w-9 items-center justify-center rounded-xl"
                  title={t("detail.collapseSidebar")}
                  aria-label={t("detail.collapseSidebar")}
                >
                  <ChevronRight size={16} />
                </button>
              </div>
            </div>

            <aside
              className="relative flex shrink-0 flex-col bg-transparent"
              style={{ width: inspectorWidth }}
            >
              <div className="flex h-12 shrink-0 items-center justify-between border-b border-slate-200/50 px-4 dark:border-slate-800">
                <div className="flex items-center">
                  <span className="mr-2 text-indigo-600 dark:text-violet-400">{activeSidebarTabItem.icon}</span>
                  <span className="text-[11px] font-bold uppercase tracking-widest text-slate-700 dark:text-slate-200">
                    {activeSidebarTabItem.label}
                  </span>
                </div>
              </div>
              <div
                key={`${activeSidebarTab}-${selectedNode?.id ?? ""}`}
                className="min-h-0 flex-1 overflow-y-auto p-4 animate-spring-slide-in"
              >
                {renderSidebarPanelContent()}
              </div>
            </aside>
          </div>
          )}
        </div>
      </main>

      <div
        className="fixed inset-x-0 z-40 px-2 lg:hidden"
        style={{ bottom: topChromeCollapsed ? "calc(0.75rem + env(safe-area-inset-bottom))" : "calc(4.1rem + env(safe-area-inset-bottom))" }}
      >
        <div
          className="mx-auto max-w-[28rem] rounded-2xl border border-slate-200 bg-white p-1.5 shadow-[0_-6px_18px_rgba(15,23,42,0.12)] dark:border-slate-700 dark:bg-slate-950 dark:shadow-[0_-12px_28px_rgba(0,0,0,0.30)]"
        >
          <div className="grid grid-cols-3 gap-1 rounded-xl bg-slate-100 p-1 dark:bg-slate-900/85">
            {mobileCanvasModeItems.map((item) => (
              <button
                key={item.key}
                type="button"
                onClick={() => setMobileCanvasMode(item.key)}
                className={`inline-flex min-h-11 min-w-0 items-center justify-center gap-2 rounded-lg px-2 text-xs font-semibold transition-colors active:scale-[0.98] focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-violet-400 ${
                  mobileCanvasMode === item.key
                    ? "bg-white text-indigo-700 shadow-sm dark:bg-violet-500/18 dark:text-violet-100 dark:ring-1 dark:ring-violet-300/35"
                    : "text-slate-500 hover:bg-white/70 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100"
                }`}
                aria-pressed={mobileCanvasMode === item.key}
                aria-label={item.description}
                title={item.description}
              >
                {item.icon}
                <span className="truncate">{item.label}</span>
              </button>
            ))}
          </div>
          <div
            role="toolbar"
            aria-label={t("detail.mobileToolbar")}
            className="mt-1.5 grid grid-cols-6 gap-1"
          >
            <button
              type="button"
              onClick={() => void handleRunWorkflow(undefined)}
              disabled={fullWorkflowRunBusy || !workflow}
              className="inline-flex min-h-14 min-w-0 flex-col items-center justify-center rounded-xl bg-indigo-600 px-1 text-[10px] font-semibold leading-[1.05] text-white shadow-lg shadow-indigo-600/20 transition-colors active:scale-[0.98] hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-gradient-to-r dark:from-indigo-500 dark:via-violet-500 dark:to-fuchsia-500 dark:shadow-violet-900/45 dark:ring-1 dark:ring-violet-300/35"
              title={fullWorkflowRunBusy ? t("detail.workflowRunning") : t("detail.runWorkflow")}
              aria-label={fullWorkflowRunBusy ? t("detail.workflowRunning") : t("detail.runWorkflow")}
            >
              {fullWorkflowRunBusy ? <Loader2 size={17} className="mb-1 shrink-0 animate-spin" /> : <Play size={17} className="mb-1 shrink-0" />}
              <span className="max-w-full text-center">{fullWorkflowRunBusy ? t("detail.running") : t("detail.runFullWorkflow")}</span>
            </button>
            {sidebarTabItems.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => openMobileSidebarTab(item.key)}
              className={`inline-flex min-h-14 min-w-0 flex-col items-center justify-center rounded-xl border px-1 text-[10px] font-semibold leading-[1.05] text-slate-600 transition-colors active:scale-[0.98] hover:border-indigo-200 hover:text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-violet-400 ${
                activeSidebarTab === item.key
                  ? "border-indigo-200 bg-indigo-50 text-indigo-700 dark:border-violet-400/55 dark:bg-violet-500/18 dark:text-violet-100"
                  : "border-slate-200 bg-white dark:border-slate-700 dark:bg-slate-950/80 dark:text-slate-300 dark:hover:border-violet-400/55 dark:hover:text-violet-100"
              }`}
              aria-label={item.label}
              title={item.label}
            >
              {item.icon}
              <span className="mt-1 max-w-full text-center">{item.label}</span>
            </button>
            ))}
          </div>
        </div>
      </div>

      <Drawer.Root
        direction="bottom"
        handleOnly
        open={mobileDetailsSheetOpen}
        onOpenChange={setMobileDetailsSheetOpen}
      >
        <Drawer.Portal>
          <Drawer.Overlay className="fixed inset-0 z-[70] bg-slate-950/42 lg:hidden" />
          <Drawer.Content
            className="fixed inset-x-0 bottom-0 z-[71] flex h-[80dvh] max-h-[80dvh] flex-col overflow-hidden rounded-t-[1.5rem] border-t border-slate-200 bg-white shadow-[0_-12px_34px_rgba(15,23,42,0.16)] outline-none dark:border-slate-700 dark:bg-[#0f1726] dark:shadow-[0_-18px_42px_rgba(0,0,0,0.34)] lg:hidden"
            onPointerDownOutside={(event) => {
              const target = event.target;
              if (target instanceof Element && target.closest("[data-template-preview-dialog]")) {
                event.preventDefault();
              }
            }}
          >
            <Drawer.Title className="sr-only">{t("detail.mobileDetailsSheet")}</Drawer.Title>
            <Drawer.Handle className="mx-auto mt-2 flex h-7 w-24 items-center justify-center rounded-full text-slate-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:text-slate-500 dark:focus-visible:ring-violet-400">
              <span className="h-1.5 w-12 rounded-full bg-slate-300 dark:bg-slate-600" />
            </Drawer.Handle>
            <div className="flex min-h-12 shrink-0 items-center justify-between gap-3 border-b border-slate-200 px-4 pb-3 dark:border-slate-800">
              <div className="flex min-w-0 items-center gap-2">
                <span className="shrink-0 text-indigo-600 dark:text-violet-200">{activeSidebarTabItem.icon}</span>
                <span className="truncate text-sm font-semibold text-slate-950 dark:text-white">{activeSidebarTabItem.label}</span>
              </div>
              <div className="ml-auto flex shrink-0 items-center gap-2">
                <button
                  type="button"
                  onClick={() => setMobileDetailsSheetOpen(false)}
                  className="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl border border-slate-200 bg-white text-slate-600 transition-colors active:scale-[0.98] hover:border-slate-300 hover:text-slate-950 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:border-slate-700 dark:bg-slate-950/80 dark:text-slate-300 dark:hover:border-violet-400/55 dark:hover:text-violet-100 dark:focus-visible:ring-violet-400"
                  aria-label={t("detail.closeMobileSheet")}
                >
                  <X size={18} />
                </button>
              </div>
            </div>
            <div
              data-vaul-no-drag
              className="min-h-0 flex-1 touch-pan-y overflow-y-auto overscroll-contain px-4 pb-[calc(env(safe-area-inset-bottom)+1rem)] pt-4 [-webkit-overflow-scrolling:touch] [&_a]:min-h-11 [&_button]:min-h-11 [&_input]:min-h-11"
            >
              {renderSidebarPanelContent()}
            </div>
          </Drawer.Content>
        </Drawer.Portal>
      </Drawer.Root>

      {previewImage ? (
        <ImagePreviewModal
          image={previewImage}
          onClose={() => setPreviewImage(null)}
        />
      ) : null}
      <ConfirmDialog
        open={Boolean(pendingDeleteDialog)}
        title={pendingDeleteDialog?.title ?? ""}
        description={pendingDeleteDialog?.description ?? ""}
        confirmLabel={t("confirm.delete.confirm")}
        cancelLabel={t("common.cancel")}
        busy={pendingDeleteDialog?.busy ?? false}
        onClose={() => setPendingDeleteAction(null)}
        onConfirm={() => {
          if (!pendingDeleteAction) {
            return;
          }
          if (pendingDeleteAction.kind === "node") {
            deleteNodeMutation.mutate(pendingDeleteAction.node.id);
            return;
          }
          if (pendingDeleteAction.kind === "selectedNodes") {
            deleteSelectedNodesMutation.mutate(pendingDeleteAction.nodeIds);
            return;
          }
          archiveUserTemplateGroupMutation.mutate(pendingDeleteAction.templateId);
        }}
      />
      <ConfirmDialog
        open={Boolean(pendingHistoryDialog)}
        title={pendingHistoryDialog?.title ?? ""}
        description={pendingHistoryDialog?.description ?? ""}
        confirmLabel={t("detail.confirm.continue")}
        cancelLabel={t("common.cancel")}
        busy={historyActionBusy}
        onClose={() => setPendingHistoryAction(null)}
        onConfirm={() => {
          if (pendingHistoryAction) {
            void executeHistoryDirection(pendingHistoryAction.direction, pendingHistoryAction.step);
          }
        }}
      />
    </div>
  );
}
