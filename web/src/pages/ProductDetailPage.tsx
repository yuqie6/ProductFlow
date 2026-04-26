import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent, WheelEvent as ReactWheelEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CircleDot,
  GitBranch,
  Image as ImageIcon,
  Loader2,
  Play,
  Plus,
  Settings2,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import type {
  ProductWorkflow,
  WorkflowNode,
  WorkflowNodeType,
} from "../lib/types";
import {
  ADD_NODE_OPTIONS,
  CANVAS_MIN_HEIGHT,
  CANVAS_MIN_WIDTH,
  CANVAS_NODE_PADDING_X,
  CANVAS_NODE_PADDING_Y,
  CANVAS_VIEWPORT_PADDING,
  MAX_INSPECTOR_WIDTH,
  MAX_ZOOM,
  MIN_INSPECTOR_WIDTH,
  MIN_ZOOM,
  NODE_HANDLE_Y,
  NODE_MIN_X,
  NODE_MIN_Y,
  NODE_WIDTH,
} from "./product-detail/constants";
import {
  buildEdgePath,
  isCanvasWheelZoomBlockedTarget,
  isPanePanBlockedTarget,
} from "./product-detail/canvasUtils";
import { ImagePreviewModal } from "./product-detail/ImagePreviewModal";
import { ImagesPanel } from "./product-detail/ImagesPanel";
import { InspectorPanel } from "./product-detail/InspectorPanel";
import { RunsPanel } from "./product-detail/RunsPanel";
import { SidebarTabButton } from "./product-detail/SidebarTabButton";
import { WorkflowNodeCard } from "./product-detail/WorkflowNodeCard";
import {
  buildPosterSourceAssetMap,
  getVisibleReferenceAssets,
} from "./product-detail/galleryImages";
import { getNodeImageDownload, getSourceImageDownload } from "./product-detail/imageDownloads";
import type {
  CanvasPoint,
  ConnectionDragState,
  NodeConfigDraft,
  NodeDragState,
  SaveStatus,
  PanePanState,
} from "./product-detail/types";
import {
  clamp,
  hasActiveWorkflow,
  outputText,
  readStoredNumber,
} from "./product-detail/utils";
import {
  defaultConfigForType,
  defaultTitleForType,
  draftFromNode,
  nodeConfigFromDraft,
} from "./product-detail/workflowConfig";
import type { DownloadableImage } from "../lib/image-downloads";

type SidebarTab = "details" | "runs" | "images";

const CANVAS_WHEEL_ZOOM_SENSITIVITY = 0.001;
const CANVAS_ZOOM_PRECISION = 10_000;

interface PlannedWheelView {
  zoom: number;
  scrollLeft: number;
  scrollTop: number;
}

export function ProductDetailPage() {
  const { productId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canvasScrollRef = useRef<HTMLDivElement | null>(null);
  const canvasRef = useRef<HTMLDivElement | null>(null);
  const plannedWheelViewRef = useRef<PlannedWheelView | null>(null);
  const nodeDragRef = useRef<NodeDragState | null>(null);
  const nodeElementRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const edgePathRefs = useRef<Record<string, SVGPathElement | null>>({});
  const edgeDeleteButtonRefs = useRef<Record<string, HTMLButtonElement | null>>(
    {},
  );
  const nodePositionMutationVersionsRef = useRef<Record<string, number>>({});
  const previousBodyUserSelectRef = useRef<string | null>(null);
  const wasWorkflowActiveRef = useRef(false);
  const draftVersionRef = useRef(0);
  const previousDraftNodeIdRef = useRef<string | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [activeSidebarTab, setActiveSidebarTab] = useState<SidebarTab>("details");
  const [nodeDrag, setNodeDrag] = useState<NodeDragState | null>(null);
  const [optimisticNodePositions, setOptimisticNodePositions] = useState<
    Record<string, CanvasPoint>
  >({});
  const [connectionDrag, setConnectionDrag] =
    useState<ConnectionDragState | null>(null);
  const [panePan, setPanePan] = useState<PanePanState | null>(null);
  const [draft, setDraft] = useState<NodeConfigDraft>(() =>
    draftFromNode(null),
  );
  const [draftDirty, setDraftDirty] = useState(false);
  const [saveStatus, setSaveStatus] = useState<SaveStatus>("idle");
  const [zoom, setZoom] = useState(() => clamp(readStoredNumber("productflow.workflow.zoom", 1), MIN_ZOOM, MAX_ZOOM));
  const zoomRef = useRef(zoom);
  const [wheelViewRevision, setWheelViewRevision] = useState(0);
  const [inspectorWidth, setInspectorWidth] = useState(() =>
    clamp(readStoredNumber("productflow.workflow.inspectorWidth", 360), MIN_INSPECTOR_WIDTH, MAX_INSPECTOR_WIDTH),
  );
  const [previewImage, setPreviewImage] = useState<DownloadableImage | null>(
    null,
  );
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
    refetchInterval: (query) =>
      hasActiveWorkflow(query.state.data as ProductWorkflow | undefined) ? 1200 : false,
  });

  const workflow = workflowQuery.data ?? null;
  const workflowActive = hasActiveWorkflow(workflow);
  const selectedNode =
    workflow?.nodes.find((node) => node.id === selectedNodeId) ??
    workflow?.nodes[0] ??
    null;

  useEffect(() => {
    if (!workflow?.nodes.length) {
      return;
    }
    const stillExists = workflow.nodes.some(
      (node) => node.id === selectedNodeId,
    );
    if (!selectedNodeId || !stillExists) {
      setSelectedNodeId(workflow.nodes[0].id);
    }
  }, [selectedNodeId, workflow]);

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
    if (!wasWorkflowActiveRef.current && workflowActive) {
      wasWorkflowActiveRef.current = true;
      return;
    }
    if (wasWorkflowActiveRef.current && !workflowActive) {
      wasWorkflowActiveRef.current = false;
      void refreshProductArtifacts();
    }
  }, [workflowActive]);

  useLayoutEffect(() => {
    const plannedView = plannedWheelViewRef.current;
    const scrollElement = canvasScrollRef.current;
    if (!plannedView || !scrollElement) {
      return;
    }
    if (plannedView.zoom !== zoom) {
      plannedWheelViewRef.current = null;
      return;
    }
    plannedWheelViewRef.current = null;
    scrollElement.scrollLeft = plannedView.scrollLeft;
    scrollElement.scrollTop = plannedView.scrollTop;
  }, [zoom, wheelViewRevision]);

  const getCanvasPoint = (
    clientX: number,
    clientY: number,
    currentZoom = zoomRef.current,
    scrollLeftOverride?: number,
    scrollTopOverride?: number,
  ): CanvasPoint => {
    const scrollElement = canvasScrollRef.current;
    const canvasElement = canvasRef.current;
    if (!scrollElement || !canvasElement) {
      return { x: clientX, y: clientY };
    }
    const scrollRect = scrollElement.getBoundingClientRect();
    const canvasRect = canvasElement.getBoundingClientRect();
    const canvasOffsetLeft = canvasRect.left - scrollRect.left + scrollElement.scrollLeft;
    const canvasOffsetTop = canvasRect.top - scrollRect.top + scrollElement.scrollTop;
    const plannedView = plannedWheelViewRef.current;
    const plannedViewMatchesZoom = plannedView?.zoom === currentZoom;
    const scrollLeft =
      scrollLeftOverride ??
      (plannedViewMatchesZoom ? plannedView.scrollLeft : scrollElement.scrollLeft);
    const scrollTop =
      scrollTopOverride ??
      (plannedViewMatchesZoom ? plannedView.scrollTop : scrollElement.scrollTop);
    return {
      x: (scrollLeft + clientX - scrollRect.left - canvasOffsetLeft) / currentZoom,
      y: (scrollTop + clientY - scrollRect.top - canvasOffsetTop) / currentZoom,
    };
  };

  const getRenderedNodePosition = (node: WorkflowNode): CanvasPoint => {
    const activeDrag = nodeDragRef.current;
    if (activeDrag?.nodeId === node.id) {
      return { x: activeDrag.currentX, y: activeDrag.currentY };
    }
    const optimisticPosition = optimisticNodePositions[node.id];
    if (optimisticPosition) {
      return optimisticPosition;
    }
    return { x: node.position_x, y: node.position_y };
  };

  const getOutputHandlePoint = (node: WorkflowNode): CanvasPoint => {
    const position = getRenderedNodePosition(node);
    return { x: position.x + NODE_WIDTH, y: position.y + NODE_HANDLE_Y };
  };

  const getInputHandlePoint = (node: WorkflowNode): CanvasPoint => {
    const position = getRenderedNodePosition(node);
    return { x: position.x, y: position.y + NODE_HANDLE_Y };
  };

  const handleDraftChange = (nextDraft: NodeConfigDraft) => {
    draftVersionRef.current += 1;
    setDraft(nextDraft);
    setDraftDirty(true);
    setSaveStatus("idle");
  };

  const selectNodeForDetails = (nodeId: string) => {
    setSelectedNodeId(nodeId);
    setActiveSidebarTab("details");
  };

  const updateZoom = (nextZoom: number) => {
    const normalized = clamp(
      Math.round(nextZoom * CANVAS_ZOOM_PRECISION) / CANVAS_ZOOM_PRECISION,
      MIN_ZOOM,
      MAX_ZOOM,
    );
    zoomRef.current = normalized;
    setZoom(normalized);
    window.localStorage.setItem("productflow.workflow.zoom", String(normalized));
    return normalized;
  };

  const setNodeElementRef = (nodeId: string, element: HTMLDivElement | null) => {
    if (element) {
      nodeElementRefs.current[nodeId] = element;
      return;
    }
    delete nodeElementRefs.current[nodeId];
  };

  const applyNodeElementPosition = (nodeId: string, position: CanvasPoint) => {
    const element = nodeElementRefs.current[nodeId];
    if (!element) {
      return;
    }
    element.style.transform = `translate3d(${position.x}px, ${position.y}px, 0)`;
  };

  const setEdgePathRef = (edgeId: string, element: SVGPathElement | null) => {
    if (element) {
      edgePathRefs.current[edgeId] = element;
      return;
    }
    delete edgePathRefs.current[edgeId];
  };

  const setEdgeDeleteButtonRef = (
    edgeId: string,
    element: HTMLButtonElement | null,
  ) => {
    if (element) {
      edgeDeleteButtonRefs.current[edgeId] = element;
      return;
    }
    delete edgeDeleteButtonRefs.current[edgeId];
  };

  const applyConnectedEdgePositions = (nodeId: string) => {
    if (!workflow) {
      return;
    }
    for (const edge of workflow.edges) {
      if (edge.source_node_id !== nodeId && edge.target_node_id !== nodeId) {
        continue;
      }
      const source = workflow.nodes.find(
        (node) => node.id === edge.source_node_id,
      );
      const target = workflow.nodes.find(
        (node) => node.id === edge.target_node_id,
      );
      if (!source || !target) {
        continue;
      }
      const start = getOutputHandlePoint(source);
      const end = getInputHandlePoint(target);
      edgePathRefs.current[edge.id]?.setAttribute("d", buildEdgePath(start, end));
      const deleteButton = edgeDeleteButtonRefs.current[edge.id];
      if (deleteButton) {
        deleteButton.style.left = `${(start.x + end.x) / 2}px`;
        deleteButton.style.top = `${(start.y + end.y) / 2}px`;
      }
    }
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
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : "工作流运行失败",
      );
    },
  });

  const createNodeMutation = useMutation({
    mutationFn: (type: WorkflowNodeType) => {
      const currentWorkflow = workflowQuery.data;
      if (!currentWorkflow) {
        throw new Error("工作流尚未加载");
      }
      const siblingCount = currentWorkflow.nodes.filter(
        (node) => node.node_type === type,
      ).length;
      return api.createWorkflowNode(productId, {
        node_type: type,
        title: defaultTitleForType(type, siblingCount + 1),
        position_x: 120 + (currentWorkflow.nodes.length % 4) * 260,
        position_y: 120 + Math.floor(currentWorkflow.nodes.length / 4) * 170,
        config_json: defaultConfigForType(type),
      });
    },
    onSuccess: (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      const newest = [...nextWorkflow.nodes].sort(
        (a, b) =>
          new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
      )[0];
      setSelectedNodeId(newest?.id ?? null);
      setActiveSidebarTab("details");
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : "新增节点失败",
      );
    },
  });

  const updateNodeConfigMutation = useMutation({
    mutationFn: (node: WorkflowNode) =>
      api.updateWorkflowNode(node.id, {
        title: draft.title,
        config_json: nodeConfigFromDraft(node, draft),
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
          : "保存节点失败",
      );
    },
  });

  const updateNodeCopyMutation = useMutation({
    mutationFn: (node: WorkflowNode) =>
      api.updateWorkflowNodeCopy(node.id, {
        title: draft.copyTitle,
        selling_points: draft.copySellingPoints
          .split("\n")
          .map((item) => item.trim())
          .filter(Boolean),
        poster_headline: draft.copyPosterHeadline,
        cta: draft.copyCta,
    }),
    onSuccess: async (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      await refreshProductArtifacts();
    },
    onError: (mutationError) => {
      setSaveStatus("failed");
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : "保存文案失败",
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
      if (nodePositionMutationVersionsRef.current[input.node.id] !== input.mutationVersion) {
        return;
      }
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      setOptimisticNodePositions((current) => {
        const next = { ...current };
        delete next[input.node.id];
        return next;
      });
    },
    onError: (mutationError, _input, context) => {
      if (nodePositionMutationVersionsRef.current[_input.node.id] !== _input.mutationVersion) {
        return;
      }
      if (context?.previous) {
        queryClient.setQueryData(
          ["product-workflow", productId],
          context.previous,
        );
      }
      setOptimisticNodePositions((current) => {
        const next = { ...current };
        delete next[_input.node.id];
        return next;
      });
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : "移动节点失败",
      );
    },
  });

  const createEdgeMutation = useMutation({
    mutationFn: (input: { sourceNodeId: string; targetNodeId: string }) =>
      api.createWorkflowEdge(productId, {
        source_node_id: input.sourceNodeId,
        target_node_id: input.targetNodeId,
        source_handle: "output",
        target_handle: "input",
      }),
    onSuccess: (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : "连接节点失败",
      );
    },
  });

  const deleteEdgeMutation = useMutation({
    mutationFn: (edgeId: string) => api.deleteWorkflowEdge(edgeId),
    onSuccess: (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : "删除连线失败",
      );
    },
  });

  const deleteNodeMutation = useMutation({
    mutationFn: (nodeId: string) => api.deleteWorkflowNode(nodeId),
    onSuccess: (nextWorkflow, nodeId) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      if (selectedNodeId === nodeId) {
        setSelectedNodeId(nextWorkflow.nodes[0]?.id ?? null);
      }
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : "删除节点失败",
      );
    },
  });

  const handleDeleteNode = (node: WorkflowNode) => {
    if (!window.confirm(`确定删除节点「${node.title}」吗？关联连线也会删除。`)) {
      return;
    }
    deleteNodeMutation.mutate(node.id);
  };

  const uploadNodeImageMutation = useMutation({
    mutationFn: (file: File) => {
      if (!selectedNode) {
        throw new Error("请选择参考图");
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
      await refreshProductArtifacts();
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError ? mutationError.detail : "上传失败",
      );
    },
  });

  const bindNodeImageMutation = useMutation({
    mutationFn: (input: { source_asset_id?: string; poster_variant_id?: string }) => {
      if (!selectedNode || selectedNode.node_type !== "reference_image") {
        throw new Error("请选择参考图节点");
      }
      return api.bindWorkflowNodeImage(selectedNode.id, input);
    },
    onSuccess: async (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      await refreshProductArtifacts();
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : "填充参考图失败",
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
      const sellingPoints = draft.copySellingPoints
        .split("\n")
        .map((item) => item.trim())
        .filter(Boolean);
      if (draft.copyTitle.trim() && draft.copyPosterHeadline.trim() && draft.copyCta.trim() && sellingPoints.length) {
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
      if (!startNodeId || startNodeId === selectedNode?.id) {
        await flushSelectedDraft();
      }
      await runWorkflowMutation.mutateAsync(startNodeId);
    } catch {
      // Mutations already surface ApiError.detail in local error state.
    }
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
    updateNodeConfigMutation.isPending ||
    createEdgeMutation.isPending ||
    deleteEdgeMutation.isPending ||
    deleteNodeMutation.isPending ||
    uploadNodeImageMutation.isPending ||
    bindNodeImageMutation.isPending ||
    updateNodeCopyMutation.isPending;
  const structureBusy = layoutMutationBusy || workflowActive;
  const runBusy = runWorkflowMutation.isPending || workflowActive;

  const startPanePan = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (
      event.button !== 0 ||
      event.defaultPrevented ||
      nodeDragRef.current ||
      connectionDrag ||
      isPanePanBlockedTarget(event.target)
    ) {
      return;
    }
    const scrollElement = canvasScrollRef.current;
    if (!scrollElement) {
      return;
    }
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    disableBodyUserSelect();
    setPanePan({
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      startScrollLeft: scrollElement.scrollLeft,
      startScrollTop: scrollElement.scrollTop,
    });
  };

  const movePanePan = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!panePan || panePan.pointerId !== event.pointerId) {
      return;
    }
    const scrollElement = canvasScrollRef.current;
    if (!scrollElement) {
      return;
    }
    event.preventDefault();
    scrollElement.scrollLeft = panePan.startScrollLeft - (event.clientX - panePan.startX);
    scrollElement.scrollTop = panePan.startScrollTop - (event.clientY - panePan.startY);
  };

  const handleCanvasWheel = (event: ReactWheelEvent<HTMLDivElement>) => {
    if (
      event.defaultPrevented ||
      nodeDragRef.current ||
      connectionDrag ||
      isCanvasWheelZoomBlockedTarget(event.target)
    ) {
      return;
    }
    const scrollElement = canvasScrollRef.current;
    if (!scrollElement || !canvasRef.current) {
      return;
    }
    const rawDelta = event.deltaY !== 0 ? event.deltaY : event.deltaX;
    if (rawDelta === 0) {
      return;
    }
    const wheelDelta =
      event.deltaMode === 1
        ? rawDelta * 16
        : event.deltaMode === 2
          ? rawDelta * scrollElement.clientHeight
          : rawDelta;
    event.preventDefault();

    const plannedView = plannedWheelViewRef.current;
    const previousZoom = plannedView?.zoom ?? zoomRef.current;
    const previousScrollLeft = plannedView?.scrollLeft ?? scrollElement.scrollLeft;
    const previousScrollTop = plannedView?.scrollTop ?? scrollElement.scrollTop;
    const anchorPoint = getCanvasPoint(
      event.clientX,
      event.clientY,
      previousZoom,
      previousScrollLeft,
      previousScrollTop,
    );
    const nextZoom = updateZoom(
      previousZoom * Math.exp(-wheelDelta * CANVAS_WHEEL_ZOOM_SENSITIVITY),
    );
    if (nextZoom === previousZoom) {
      return;
    }

    plannedWheelViewRef.current = {
      zoom: nextZoom,
      scrollLeft: previousScrollLeft + anchorPoint.x * (nextZoom - previousZoom),
      scrollTop: previousScrollTop + anchorPoint.y * (nextZoom - previousZoom),
    };
    setWheelViewRevision((current) => current + 1);
  };

  const endPanePan = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!panePan || panePan.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    restoreBodyUserSelect();
    setPanePan(null);
  };

  const cancelPanePan = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (panePan && panePan.pointerId !== event.pointerId) {
      return;
    }
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    restoreBodyUserSelect();
    setPanePan(null);
  };

  const startNodeDrag = (
    node: WorkflowNode,
    event: ReactPointerEvent<HTMLDivElement>,
  ) => {
    if (event.button !== 0) {
      return;
    }
    const actionTarget =
      event.target instanceof HTMLElement
        ? event.target.closest("[data-node-action]")
        : null;
    if (actionTarget) {
      return;
    }
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    disableBodyUserSelect();
    const point = getCanvasPoint(event.clientX, event.clientY);
    setSelectedNodeId(node.id);
    setActiveSidebarTab("details");
    const renderedPosition = getRenderedNodePosition(node);
    const nextDrag = {
      nodeId: node.id,
      pointerId: event.pointerId,
      offsetX: point.x - renderedPosition.x,
      offsetY: point.y - renderedPosition.y,
      currentX: renderedPosition.x,
      currentY: renderedPosition.y,
    };
    nodeDragRef.current = nextDrag;
    setNodeDrag(nextDrag);
  };

  const moveNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    const activeDrag = nodeDragRef.current;
    if (!activeDrag || activeDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    const point = getCanvasPoint(event.clientX, event.clientY);
    const nextDrag = {
      ...activeDrag,
      currentX: Math.max(NODE_MIN_X, point.x - activeDrag.offsetX),
      currentY: Math.max(NODE_MIN_Y, point.y - activeDrag.offsetY),
    };
    nodeDragRef.current = nextDrag;
    applyNodeElementPosition(nextDrag.nodeId, {
      x: nextDrag.currentX,
      y: nextDrag.currentY,
    });
    applyConnectedEdgePositions(nextDrag.nodeId);
  };

  const cancelNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    const activeDrag = nodeDragRef.current;
    if (!activeDrag) {
      return;
    }
    if (activeDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    restoreBodyUserSelect();
    nodeDragRef.current = null;
    setNodeDrag(null);
  };

  const endNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    const activeDrag = nodeDragRef.current;
    if (!activeDrag || activeDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    restoreBodyUserSelect();
    const point = getCanvasPoint(event.clientX, event.clientY);
    const finalX = Math.max(NODE_MIN_X, Math.round(point.x - activeDrag.offsetX));
    const finalY = Math.max(NODE_MIN_Y, Math.round(point.y - activeDrag.offsetY));
    const dragged = workflow?.nodes.find((node) => node.id === activeDrag.nodeId);
    if (
      dragged &&
      (dragged.position_x !== finalX || dragged.position_y !== finalY)
    ) {
      const rollbackWorkflow = queryClient.getQueryData<ProductWorkflow>([
        "product-workflow",
        productId,
      ]);
      const mutationVersion = (nodePositionMutationVersionsRef.current[dragged.id] ?? 0) + 1;
      nodePositionMutationVersionsRef.current[dragged.id] = mutationVersion;
      nodeDragRef.current = {
        ...activeDrag,
        currentX: finalX,
        currentY: finalY,
      };
      applyNodeElementPosition(dragged.id, { x: finalX, y: finalY });
      applyConnectedEdgePositions(dragged.id);
      setOptimisticNodePositions((current) => ({
        ...current,
        [dragged.id]: { x: finalX, y: finalY },
      }));
      queryClient.setQueryData<ProductWorkflow>(
        ["product-workflow", productId],
        (current) => {
          if (!current) {
            return current;
          }
          return {
            ...current,
            nodes: current.nodes.map((node) =>
              node.id === dragged.id
                ? { ...node, position_x: finalX, position_y: finalY }
                : node,
            ),
          };
        },
      );
      updateNodePositionMutation.mutate({
        node: dragged,
        position_x: finalX,
        position_y: finalY,
        mutationVersion,
        rollbackWorkflow,
      });
    }
    nodeDragRef.current = null;
    setNodeDrag(null);
  };

  const startConnectionDrag = (
    node: WorkflowNode,
    event: ReactPointerEvent<HTMLButtonElement>,
  ) => {
    if (structureBusy || event.button !== 0) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    const from = getOutputHandlePoint(node);
    setSelectedNodeId(node.id);
    setActiveSidebarTab("details");
    setConnectionDrag({
      sourceNodeId: node.id,
      pointerId: event.pointerId,
      from,
      to: getCanvasPoint(event.clientX, event.clientY),
    });
  };

  const moveConnectionDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (!connectionDrag || connectionDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    const to = getCanvasPoint(event.clientX, event.clientY);
    setConnectionDrag((current) =>
      current && current.pointerId === event.pointerId
        ? { ...current, to }
        : current,
    );
  };

  const endConnectionDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (!connectionDrag || connectionDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    const element = document.elementFromPoint(event.clientX, event.clientY);
    const targetElement =
      element instanceof HTMLElement
        ? element.closest<HTMLElement>("[data-workflow-target-node-id]")
        : null;
    const nodeElement =
      element instanceof HTMLElement
        ? element.closest<HTMLElement>("[data-workflow-node-id]")
        : null;
    const targetNodeId =
      targetElement?.dataset.workflowTargetNodeId ??
      nodeElement?.dataset.workflowNodeId ??
      null;
    if (targetNodeId && targetNodeId !== connectionDrag.sourceNodeId) {
      createEdgeMutation.mutate({
        sourceNodeId: connectionDrag.sourceNodeId,
        targetNodeId,
      });
    }
    setConnectionDrag(null);
  };

  if (productQuery.isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-white text-zinc-400">
        <Loader2 size={24} className="animate-spin" />
      </div>
    );
  }

  if (productQuery.isError || !productQuery.data) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-white">
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          商品详情加载失败，请返回列表重试。
        </div>
      </div>
    );
  }

  const product = productQuery.data;
  const sourceImage = getSourceImageDownload(product);
  const latestRun = workflow?.runs[0] ?? null;
  const canvasViewportWidth = canvasScrollRef.current?.clientWidth ?? 0;
  const canvasViewportHeight = canvasScrollRef.current?.clientHeight ?? 0;
  const canvasWidth = Math.max(
    CANVAS_MIN_WIDTH,
    canvasViewportWidth / zoom + CANVAS_VIEWPORT_PADDING,
    ...(workflow?.nodes.map(
      (node) => getRenderedNodePosition(node).x + CANVAS_NODE_PADDING_X,
    ) ?? [CANVAS_MIN_WIDTH]),
    connectionDrag ? connectionDrag.to.x + CANVAS_NODE_PADDING_X : 0,
  );
  const canvasHeight = Math.max(
    CANVAS_MIN_HEIGHT,
    canvasViewportHeight / zoom + CANVAS_VIEWPORT_PADDING,
    ...(workflow?.nodes.map(
      (node) => getRenderedNodePosition(node).y + CANVAS_NODE_PADDING_Y,
    ) ?? [CANVAS_MIN_HEIGHT]),
    connectionDrag ? connectionDrag.to.y + CANVAS_NODE_PADDING_Y : 0,
  );
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
  const fillReferenceBusy = bindNodeImageMutation.isPending;

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-white text-sm text-zinc-900">
      <TopNav onHome={() => navigate("/products")} breadcrumbs={product.name} />

      <main className="flex min-h-0 flex-1 flex-col border-t border-zinc-200 bg-[#f7f7f8]">
        <div className="z-20 flex h-12 shrink-0 items-center justify-between border-b border-zinc-200 bg-white/85 px-4 backdrop-blur">
          <div className="flex min-w-0 items-center gap-3">
            <GitBranch size={16} className="text-zinc-500" />
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold">
                {product.name}
              </div>
              <div className="text-[11px] text-zinc-500">
                节点 · {workflow?.nodes.length ?? 0} · 缩放 {Math.round(zoom * 100)}%
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            {ADD_NODE_OPTIONS.map((option) => (
              <button
                key={option.type}
                type="button"
                onClick={() => createNodeMutation.mutate(option.type)}
                disabled={structureBusy || !workflow}
                className="inline-flex items-center rounded-md border border-zinc-200 bg-white px-2.5 py-1.5 text-xs font-medium text-zinc-700 hover:border-zinc-300 hover:bg-zinc-50 disabled:opacity-50"
              >
                <Plus size={13} className="mr-1" /> {option.label}
              </button>
            ))}
            <button
              type="button"
              onClick={() => void handleRunWorkflow(undefined)}
              disabled={runBusy || !workflow}
              className="inline-flex items-center rounded-md bg-zinc-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-zinc-800 disabled:opacity-50"
            >
              {runBusy ? (
                <Loader2 size={13} className="mr-1 animate-spin" />
              ) : (
                <Play size={13} className="mr-1" />
              )}
              运行
            </button>
          </div>
        </div>

        {error ? (
          <div className="z-20 border-b border-red-200 bg-red-50 px-4 py-2 text-xs text-red-700">
            <AlertCircle size={14} className="mr-2 inline" /> {error}
          </div>
        ) : null}

        <div className="relative flex min-h-0 flex-1 overflow-hidden">
          <div className="absolute inset-0 bg-[radial-gradient(#d4d4d8_1px,transparent_1px)] [background-size:18px_18px]" />
          <section className="relative z-10 min-w-0 flex-1 overflow-hidden">
            <div
              ref={canvasScrollRef}
              className={`h-full overflow-auto p-6 ${panePan ? "cursor-grabbing" : "cursor-grab"}`}
              onPointerDown={startPanePan}
              onPointerMove={movePanePan}
              onPointerUp={endPanePan}
              onPointerCancel={cancelPanePan}
              onLostPointerCapture={cancelPanePan}
              onWheel={handleCanvasWheel}
            >
              {workflowQuery.isLoading ? (
                <div className="flex h-full items-center justify-center text-zinc-400">
                  <Loader2 size={24} className="animate-spin" />
                </div>
              ) : workflow ? (
                <div className="relative" style={{ width: canvasWidth * zoom, height: canvasHeight * zoom }}>
                  <div
                    ref={canvasRef}
                    className="relative origin-top-left"
                    style={{ width: canvasWidth, height: canvasHeight, transform: `scale(${zoom})` }}
                  >
                    <svg className="pointer-events-none absolute inset-0 h-full w-full">
                  {workflow.edges.map((edge) => {
                    const source = workflow.nodes.find(
                      (node) => node.id === edge.source_node_id,
                    );
                    const target = workflow.nodes.find(
                      (node) => node.id === edge.target_node_id,
                    );
                    if (!source || !target) {
                      return null;
                    }
                    const start = getOutputHandlePoint(source);
                    const end = getInputHandlePoint(target);
                    return (
                      <path
                        key={edge.id}
                        ref={(element) => setEdgePathRef(edge.id, element)}
                        d={buildEdgePath(start, end)}
                        fill="none"
                        stroke="#71717a"
                        strokeWidth="1.6"
                      />
                    );
                  })}
                  {connectionDrag ? (
                    <path
                      d={`M ${connectionDrag.from.x} ${connectionDrag.from.y} C ${connectionDrag.from.x + Math.max(80, Math.abs(connectionDrag.to.x - connectionDrag.from.x) / 2)} ${connectionDrag.from.y}, ${connectionDrag.to.x - Math.max(80, Math.abs(connectionDrag.to.x - connectionDrag.from.x) / 2)} ${connectionDrag.to.y}, ${connectionDrag.to.x} ${connectionDrag.to.y}`}
                      fill="none"
                      stroke="#2563eb"
                      strokeDasharray="6 4"
                      strokeWidth="2"
                    />
                  ) : null}
                    </svg>
                    {workflow.edges.map((edge) => {
                  const source = workflow.nodes.find(
                    (node) => node.id === edge.source_node_id,
                  );
                  const target = workflow.nodes.find(
                    (node) => node.id === edge.target_node_id,
                  );
                  if (!source || !target) {
                    return null;
                  }
                  const start = getOutputHandlePoint(source);
                  const end = getInputHandlePoint(target);
                  return (
                    <button
                      key={`${edge.id}-delete`}
                      ref={(element) => setEdgeDeleteButtonRef(edge.id, element)}
                      type="button"
                      onClick={() => deleteEdgeMutation.mutate(edge.id)}
                      disabled={structureBusy}
                      className="absolute z-20 flex h-5 w-5 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border border-zinc-300 bg-white text-[12px] leading-none text-zinc-500 shadow-sm hover:border-red-300 hover:bg-red-50 hover:text-red-600 disabled:opacity-50"
                      style={{
                        left: (start.x + end.x) / 2,
                        top: (start.y + end.y) / 2,
                      }}
                      title="删除连线"
                      aria-label="删除连线"
                    >
                      ×
                    </button>
                  );
                })}
                    {workflow.nodes.map((node) => (
                      <WorkflowNodeCard
                        key={node.id}
                        node={node}
                        nodeRef={(element) => setNodeElementRef(node.id, element)}
                        position={getRenderedNodePosition(node)}
                        image={getNodeImageDownload(node, product)}
                        selected={node.id === selectedNode?.id}
                        dragging={nodeDrag?.nodeId === node.id}
                        onSelect={() => selectNodeForDetails(node.id)}
                        onStartDrag={(event) => startNodeDrag(node, event)}
                        onMoveDrag={moveNodeDrag}
                        onEndDrag={endNodeDrag}
                        onCancelDrag={cancelNodeDrag}
                        onStartConnection={(event) =>
                          startConnectionDrag(node, event)
                        }
                        onMoveConnection={moveConnectionDrag}
                        onEndConnection={endConnectionDrag}
                        onRun={() => void handleRunWorkflow(node.id)}
                        onDelete={() => handleDeleteNode(node)}
                        busy={structureBusy}
                        runBusy={runBusy}
                      />
                    ))}
                  </div>
                </div>
              ) : (
                <div className="flex h-full items-center justify-center text-xs text-zinc-500">
                  工作流加载失败
                </div>
              )}
            </div>

            <div data-canvas-control className="pointer-events-none absolute bottom-4 left-4 z-30">
              <div className="pointer-events-auto flex items-center gap-1 rounded-lg border border-zinc-200 bg-white/90 p-1 shadow-sm backdrop-blur">
              <button
                type="button"
                onClick={() => updateZoom(zoom - 0.1)}
                className="inline-flex items-center rounded px-2 py-1 text-xs text-zinc-600 hover:bg-zinc-50"
                aria-label="缩小画布"
              >
                <ZoomOut size={13} />
              </button>
              <button
                type="button"
                onClick={() => updateZoom(1)}
                className="rounded px-2 py-1 text-xs tabular-nums text-zinc-600 hover:bg-zinc-50"
                aria-label="重置画布缩放"
              >
                {Math.round(zoom * 100)}%
              </button>
              <button
                type="button"
                onClick={() => updateZoom(zoom + 0.1)}
                className="inline-flex items-center rounded px-2 py-1 text-xs text-zinc-600 hover:bg-zinc-50"
                aria-label="放大画布"
              >
                <ZoomIn size={13} />
              </button>
              </div>
            </div>
          </section>

          <div className="relative z-20 flex shrink-0 border-l border-zinc-200 bg-white/95 shadow-[-8px_0_24px_-20px_rgba(0,0,0,0.35)] backdrop-blur">
            <div data-canvas-control className="flex w-14 shrink-0 flex-col items-center gap-2 border-r border-zinc-200 bg-zinc-50/80 px-1.5 py-3">
              <SidebarTabButton
                active={activeSidebarTab === "details"}
                label="详情"
                title="Details"
                icon={<Settings2 size={15} />}
                onClick={() => setActiveSidebarTab("details")}
              />
              <SidebarTabButton
                active={activeSidebarTab === "runs"}
                label="运行"
                title="Runs"
                icon={<CircleDot size={15} />}
                onClick={() => setActiveSidebarTab("runs")}
              />
              <SidebarTabButton
                active={activeSidebarTab === "images"}
                label="图片"
                title="Images"
                icon={<ImageIcon size={15} />}
                onClick={() => setActiveSidebarTab("images")}
              />
            </div>
            <aside
              className="relative flex shrink-0 flex-col bg-white/95"
              style={{ width: inspectorWidth }}
            >
              <div
                role="separator"
                aria-label="调整右侧栏宽度"
                onPointerDown={startInspectorResize}
                className="absolute left-[-4px] top-0 h-full w-2 cursor-col-resize hover:bg-zinc-300/50"
              />
              <div className="flex h-12 shrink-0 items-center border-b border-zinc-200 px-4">
                {activeSidebarTab === "details" ? <Settings2 size={14} className="mr-2 text-zinc-400" /> : null}
                {activeSidebarTab === "runs" ? <CircleDot size={14} className="mr-2 text-zinc-400" /> : null}
                {activeSidebarTab === "images" ? <ImageIcon size={14} className="mr-2 text-zinc-400" /> : null}
                <span className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                  {activeSidebarTab === "details" ? "详情" : activeSidebarTab === "runs" ? "运行记录" : "图片"}
                </span>
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto p-4">
                {activeSidebarTab === "details" ? (
                  selectedNode ? (
                    <InspectorPanel
                      product={product}
                      sourceImage={sourceImage}
                      workflow={workflow}
                      node={selectedNode}
                      draft={draft}
                      onDraftChange={handleDraftChange}
                      onSave={() => void flushSelectedDraft()}
                      onSaveCopy={() => void flushSelectedDraft()}
                      onRun={() => void handleRunWorkflow(selectedNode.id)}
                      saveStatus={saveStatus}
                      onUploadImage={(file) => uploadNodeImageMutation.mutate(file)}
                      onDelete={() => handleDeleteNode(selectedNode)}
                      busy={structureBusy}
                      runBusy={runBusy}
                    />
                  ) : (
                    <div className="text-xs text-zinc-500">
                      选择一个画布节点后编辑配置。
                    </div>
                  )
                ) : null}
                {activeSidebarTab === "runs" ? <RunsPanel workflow={workflow} latestRun={latestRun} /> : null}
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
              </div>
            </aside>
          </div>
        </div>
      </main>
      {previewImage ? (
        <ImagePreviewModal
          image={previewImage}
          onClose={() => setPreviewImage(null)}
        />
      ) : null}
    </div>
  );
}
