import { useEffect, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CheckCircle2,
  CircleDot,
  Clock3,
  FileText,
  GitBranch,
  Image as ImageIcon,
  ImagePlus,
  Layers3,
  Loader2,
  Play,
  Plus,
  Save,
  Settings2,
  Trash2,
  ZoomIn,
  ZoomOut,
  Upload,
  XCircle,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { ImageDropZone } from "../components/ImageDropZone";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { formatDateTime, formatPrice } from "../lib/format";
import type {
  ProductDetail,
  ProductWorkflow,
  WorkflowNode,
  WorkflowRunStatus,
  WorkflowNodeType,
} from "../lib/types";
import {
  ADD_NODE_OPTIONS,
  MAX_BOTTOM_PANEL_HEIGHT,
  MAX_INSPECTOR_WIDTH,
  MAX_ZOOM,
  MIN_BOTTOM_PANEL_HEIGHT,
  MIN_INSPECTOR_WIDTH,
  MIN_ZOOM,
  NODE_HANDLE_Y,
  NODE_LABELS,
  NODE_MIN_X,
  NODE_MIN_Y,
  NODE_STATUS_LABELS,
  NODE_WIDTH,
} from "./product-detail/constants";
import { DownloadLink, PosterThumb, SourceAssetThumb } from "./product-detail/ImageDownloadComponents";
import { NodeOutputPreview } from "./product-detail/NodeOutputPreview";
import { getNodeImageDownload, getSourceImageDownload } from "./product-detail/imageDownloads";
import type { CanvasPoint, ConnectionDragState, NodeConfigDraft, NodeDragState } from "./product-detail/types";
import {
  clamp,
  hasActiveWorkflow,
  nodeIdleSummary,
  outputText,
  readStoredNumber,
  statusClass,
} from "./product-detail/utils";
import {
  defaultConfigForType,
  defaultTitleForType,
  draftFromNode,
  nodeConfigFromDraft,
} from "./product-detail/workflowConfig";
import type { DownloadableImage } from "../lib/image-downloads";

type SaveStatus = "idle" | "saving" | "saved" | "failed";

const RUN_STATUS_LABELS: Record<WorkflowRunStatus, string> = {
  running: "运行中",
  succeeded: "成功",
  failed: "失败",
};

const RUN_STATUS_CLASS_NAMES: Record<WorkflowRunStatus, string> = {
  running: "border-blue-200 bg-blue-50 text-blue-700",
  succeeded: "border-emerald-200 bg-emerald-50 text-emerald-700",
  failed: "border-red-200 bg-red-50 text-red-700",
};

const RUN_STATUS_DOT_CLASS_NAMES: Record<WorkflowRunStatus, string> = {
  running: "bg-blue-500 shadow-blue-500/30",
  succeeded: "bg-emerald-500 shadow-emerald-500/30",
  failed: "bg-red-500 shadow-red-500/30",
};

const SAVE_STATUS_LABELS: Record<SaveStatus, string> = {
  idle: "自动保存",
  saving: "保存中",
  saved: "已保存",
  failed: "保存失败",
};

const SAVE_STATUS_CLASS_NAMES: Record<SaveStatus, string> = {
  idle: "border-zinc-200 bg-zinc-50 text-zinc-500",
  saving: "border-blue-200 bg-blue-50 text-blue-700",
  saved: "border-emerald-200 bg-emerald-50 text-emerald-700",
  failed: "border-red-200 bg-red-50 text-red-700",
};

const IMAGE_PREVIEW_SURFACE_CLASS_NAME =
  "bg-[linear-gradient(135deg,#fafafa_25%,#f4f4f5_25%,#f4f4f5_50%,#fafafa_50%,#fafafa_75%,#f4f4f5_75%,#f4f4f5_100%)] bg-[length:16px_16px]";
const BOTTOM_IMAGE_RATIO_STORAGE_KEY = "productflow.workflow.bottomPanelImageRatio";
const MIN_BOTTOM_IMAGE_RATIO = 0.28;
const MAX_BOTTOM_IMAGE_RATIO = 0.55;

export function ProductDetailPage() {
  const { productId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canvasRef = useRef<HTMLDivElement | null>(null);
  const nodeDragRafRef = useRef<number | null>(null);
  const pendingNodeDragRef = useRef<CanvasPoint | null>(null);
  const previousBodyUserSelectRef = useRef<string | null>(null);
  const wasWorkflowActiveRef = useRef(false);
  const draftVersionRef = useRef(0);
  const previousDraftNodeIdRef = useRef<string | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [nodeDrag, setNodeDrag] = useState<NodeDragState | null>(null);
  const [optimisticNodePositions, setOptimisticNodePositions] = useState<
    Record<string, CanvasPoint>
  >({});
  const [connectionDrag, setConnectionDrag] =
    useState<ConnectionDragState | null>(null);
  const [draft, setDraft] = useState<NodeConfigDraft>(() =>
    draftFromNode(null),
  );
  const [draftDirty, setDraftDirty] = useState(false);
  const [saveStatus, setSaveStatus] = useState<SaveStatus>("idle");
  const [zoom, setZoom] = useState(() => clamp(readStoredNumber("productflow.workflow.zoom", 1), MIN_ZOOM, MAX_ZOOM));
  const [inspectorWidth, setInspectorWidth] = useState(() =>
    clamp(readStoredNumber("productflow.workflow.inspectorWidth", 360), MIN_INSPECTOR_WIDTH, MAX_INSPECTOR_WIDTH),
  );
  const [bottomPanelHeight, setBottomPanelHeight] = useState(() =>
    clamp(
      readStoredNumber("productflow.workflow.bottomPanelHeight", 224),
      MIN_BOTTOM_PANEL_HEIGHT,
      MAX_BOTTOM_PANEL_HEIGHT,
    ),
  );
  const [bottomImageRatio, setBottomImageRatio] = useState(() =>
    clamp(
      readStoredNumber(BOTTOM_IMAGE_RATIO_STORAGE_KEY, 0.38),
      MIN_BOTTOM_IMAGE_RATIO,
      MAX_BOTTOM_IMAGE_RATIO,
    ),
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
      if (nodeDragRafRef.current !== null) {
        window.cancelAnimationFrame(nodeDragRafRef.current);
      }
      restoreBodyUserSelect();
    };
  }, []);

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

  const getCanvasPoint = (clientX: number, clientY: number): CanvasPoint => {
    const rect = canvasRef.current?.getBoundingClientRect();
    if (!rect) {
      return { x: clientX, y: clientY };
    }
    return {
      x: (clientX - rect.left) / zoom,
      y: (clientY - rect.top) / zoom,
    };
  };

  const getRenderedNodePosition = (node: WorkflowNode): CanvasPoint => {
    if (nodeDrag?.nodeId === node.id) {
      return { x: nodeDrag.currentX, y: nodeDrag.currentY };
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

  const updateZoom = (nextZoom: number) => {
    const normalized = clamp(Math.round(nextZoom * 100) / 100, MIN_ZOOM, MAX_ZOOM);
    setZoom(normalized);
    window.localStorage.setItem("productflow.workflow.zoom", String(normalized));
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
    mutationFn: (input: {
      node: WorkflowNode;
      position_x: number;
      position_y: number;
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
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      setOptimisticNodePositions((current) => {
        const next = { ...current };
        delete next[input.node.id];
        return next;
      });
    },
    onError: (mutationError, _input, context) => {
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

  const startBottomResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    disableBodyUserSelect();
    const startY = event.clientY;
    const startHeight = bottomPanelHeight;
    const onMove = (moveEvent: PointerEvent) => {
      const next = clamp(startHeight + startY - moveEvent.clientY, MIN_BOTTOM_PANEL_HEIGHT, MAX_BOTTOM_PANEL_HEIGHT);
      setBottomPanelHeight(next);
      window.localStorage.setItem("productflow.workflow.bottomPanelHeight", String(next));
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

  const startBottomSplitResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    disableBodyUserSelect();
    const panelWidth = event.currentTarget.parentElement?.getBoundingClientRect().width ?? 0;
    if (!panelWidth) {
      restoreBodyUserSelect();
      return;
    }
    const startX = event.clientX;
    const startRatio = bottomImageRatio;
    const onMove = (moveEvent: PointerEvent) => {
      const next = clamp(
        startRatio - (moveEvent.clientX - startX) / panelWidth,
        MIN_BOTTOM_IMAGE_RATIO,
        MAX_BOTTOM_IMAGE_RATIO,
      );
      setBottomImageRatio(next);
      window.localStorage.setItem(BOTTOM_IMAGE_RATIO_STORAGE_KEY, String(next));
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
    updateNodeCopyMutation.isPending;
  const structureBusy = layoutMutationBusy || workflowActive;
  const runBusy = runWorkflowMutation.isPending || workflowActive;

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
    const renderedPosition = getRenderedNodePosition(node);
    setNodeDrag({
      nodeId: node.id,
      pointerId: event.pointerId,
      offsetX: point.x - renderedPosition.x,
      offsetY: point.y - renderedPosition.y,
      currentX: renderedPosition.x,
      currentY: renderedPosition.y,
    });
  };

  const moveNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!nodeDrag || nodeDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    const point = getCanvasPoint(event.clientX, event.clientY);
    pendingNodeDragRef.current = {
      x: Math.max(NODE_MIN_X, Math.round(point.x - nodeDrag.offsetX)),
      y: Math.max(NODE_MIN_Y, Math.round(point.y - nodeDrag.offsetY)),
    };
    if (nodeDragRafRef.current !== null) {
      return;
    }
    nodeDragRafRef.current = window.requestAnimationFrame(() => {
      nodeDragRafRef.current = null;
      const pending = pendingNodeDragRef.current;
      if (!pending) {
        return;
      }
      setNodeDrag((current) =>
        current && current.pointerId === event.pointerId
          ? { ...current, currentX: pending.x, currentY: pending.y }
          : current,
      );
    });
  };

  const cancelNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (nodeDrag && nodeDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    restoreBodyUserSelect();
    pendingNodeDragRef.current = null;
    if (nodeDragRafRef.current !== null) {
      window.cancelAnimationFrame(nodeDragRafRef.current);
      nodeDragRafRef.current = null;
    }
    setNodeDrag(null);
  };

  const endNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!nodeDrag || nodeDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    restoreBodyUserSelect();
    pendingNodeDragRef.current = null;
    if (nodeDragRafRef.current !== null) {
      window.cancelAnimationFrame(nodeDragRafRef.current);
      nodeDragRafRef.current = null;
    }
    const point = getCanvasPoint(event.clientX, event.clientY);
    const finalX = Math.max(NODE_MIN_X, Math.round(point.x - nodeDrag.offsetX));
    const finalY = Math.max(NODE_MIN_Y, Math.round(point.y - nodeDrag.offsetY));
    const dragged = workflow?.nodes.find((node) => node.id === nodeDrag.nodeId);
    if (
      dragged &&
      (dragged.position_x !== finalX || dragged.position_y !== finalY)
    ) {
      const rollbackWorkflow = queryClient.getQueryData<ProductWorkflow>([
        "product-workflow",
        productId,
      ]);
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
        rollbackWorkflow,
      });
    }
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
  const canvasWidth = Math.max(
    1480,
    ...(workflow?.nodes.map(
      (node) => getRenderedNodePosition(node).x + 360,
    ) ?? [1480]),
    connectionDrag ? connectionDrag.to.x + 120 : 0,
  );
  const canvasHeight = Math.max(
    820,
    ...(workflow?.nodes.map(
      (node) => getRenderedNodePosition(node).y + 220,
    ) ?? [820]),
    connectionDrag ? connectionDrag.to.y + 120 : 0,
  );
  const posters = historyQuery.data?.poster_variants ?? product.poster_variants;
  const referenceAssets = [...product.source_assets]
    .filter((asset) => asset.kind === "reference_image")
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
  const artifactCount = posters.length + referenceAssets.length;

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
            <div className="h-full overflow-auto p-6">
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
                    const { x: x1, y: y1 } = getOutputHandlePoint(source);
                    const { x: x2, y: y2 } = getInputHandlePoint(target);
                    const mid = Math.max(50, Math.abs(x2 - x1) / 2);
                    return (
                      <path
                        key={edge.id}
                        d={`M ${x1} ${y1} C ${x1 + mid} ${y1}, ${x2 - mid} ${y2}, ${x2} ${y2}`}
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
                        position={getRenderedNodePosition(node)}
                        image={getNodeImageDownload(node, product, posters)}
                        selected={node.id === selectedNode?.id}
                        dragging={nodeDrag?.nodeId === node.id}
                        onSelect={() => setSelectedNodeId(node.id)}
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

            <div className="pointer-events-none absolute bottom-4 left-4 z-30">
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

          <aside
            className="relative z-20 flex shrink-0 flex-col border-l border-zinc-200 bg-white/95 shadow-[-8px_0_24px_-20px_rgba(0,0,0,0.35)] backdrop-blur"
            style={{ width: inspectorWidth }}
          >
            <div
              role="separator"
              aria-label="调整节点栏宽度"
              onPointerDown={startInspectorResize}
              className="absolute left-[-4px] top-0 h-full w-2 cursor-col-resize hover:bg-zinc-300/50"
            />
            <div className="flex h-12 shrink-0 items-center border-b border-zinc-200 px-4">
              <Settings2 size={14} className="mr-2 text-zinc-400" />
              <span className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                节点
              </span>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-4">
              {selectedNode ? (
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
              )}
            </div>
          </aside>
        </div>

        <div
          className="relative z-20 grid shrink-0 border-t border-zinc-200 bg-white/95 shadow-[0_-10px_30px_-26px_rgba(0,0,0,0.45)] backdrop-blur"
          style={{
            height: bottomPanelHeight,
            gridTemplateColumns: `minmax(320px, ${1 - bottomImageRatio}fr) 10px minmax(300px, ${bottomImageRatio}fr)`,
          }}
        >
          <div
            role="separator"
            aria-label="调整底部面板高度"
            onPointerDown={startBottomResize}
            className="absolute top-[-4px] left-0 z-30 h-2 w-full cursor-row-resize hover:bg-zinc-300/50"
          />
          <section className="min-w-0 overflow-y-auto p-4">
            <div className="mb-3 flex items-center justify-between">
              <div>
                <div className="flex items-center text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                  <CircleDot size={13} className="mr-2 text-zinc-400" /> 运行记录
                </div>
                <div className="mt-1 text-xs text-zinc-500">
                  {workflow?.runs.length ? `共 ${workflow.runs.length} 次运行` : "暂无运行历史"}
                </div>
              </div>
              {latestRun ? (
                <div className="rounded-full border border-zinc-200 bg-zinc-50 px-2.5 py-1 text-[11px] text-zinc-500">
                  最近 {formatDateTime(latestRun.started_at)}
                </div>
              ) : null}
            </div>
            {workflow?.runs.length ? (
              <div className="space-y-2">
                {workflow.runs.map((run) => (
                  <div
                    key={run.id}
                    className="rounded-xl border border-zinc-200 bg-white px-3 py-2.5 text-xs shadow-sm"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex min-w-0 items-start gap-3">
                        <span
                          className={`mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full shadow-md ${RUN_STATUS_DOT_CLASS_NAMES[run.status]}`}
                        />
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <span
                              className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${RUN_STATUS_CLASS_NAMES[run.status]}`}
                            >
                              {RUN_STATUS_LABELS[run.status]}
                            </span>
                            <span className="inline-flex items-center text-[11px] text-zinc-500">
                              <Layers3 size={12} className="mr-1 text-zinc-400" />
                              节点记录 {run.node_runs.length}
                            </span>
                          </div>
                          {run.failure_reason ? (
                            <div className="mt-2 line-clamp-2 rounded-lg border border-red-100 bg-red-50 px-2.5 py-1.5 text-red-700">
                              {run.failure_reason}
                            </div>
                          ) : null}
                        </div>
                      </div>
                      <div className="shrink-0 text-right text-[10px] leading-relaxed text-zinc-400">
                        <div>{formatDateTime(run.started_at)}</div>
                        {run.finished_at ? <div>完成 {formatDateTime(run.finished_at)}</div> : null}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="flex h-[calc(100%-44px)] min-h-[96px] items-center justify-center rounded-xl border border-dashed border-zinc-200 bg-zinc-50/60 px-4 py-6 text-center text-xs text-zinc-500">
                暂无运行记录
              </div>
            )}
          </section>

          <div
            role="separator"
            aria-label="调整运行记录和图片区宽度"
            onPointerDown={startBottomSplitResize}
            className="relative cursor-col-resize border-x border-zinc-200 bg-zinc-100/70 hover:bg-zinc-200"
          >
            <div className="absolute left-1/2 top-1/2 h-9 w-1 -translate-x-1/2 -translate-y-1/2 rounded-full bg-zinc-300" />
          </div>

          <section className="overflow-y-auto bg-zinc-50/40 p-4">
            <div className="mb-3 flex items-center justify-between">
              <div>
                <div className="flex items-center text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                  <ImageIcon size={13} className="mr-2 text-zinc-400" /> 图片
                </div>
                <div className="mt-1 text-xs text-zinc-500">
                  {artifactCount ? `可下载 ${artifactCount} 张` : "等待生成素材"}
                </div>
              </div>
            </div>
            {artifactCount ? (
              <div className="grid grid-cols-3 gap-2">
                {posters.map((poster) => (
                  <PosterThumb
                    key={poster.id}
                    poster={poster}
                    productName={product.name}
                  />
                ))}
                {referenceAssets.map((asset) => (
                  <SourceAssetThumb key={asset.id} asset={asset} product={product} />
                ))}
              </div>
            ) : (
              <div className="flex h-[calc(100%-44px)] min-h-[96px] items-center justify-center rounded-xl border border-dashed border-zinc-200 bg-white px-3 py-6 text-center text-xs leading-relaxed text-zinc-500">
                暂无图片
              </div>
            )}
          </section>
        </div>
      </main>
    </div>
  );
}

function WorkflowNodeCard({
  node,
  position,
  image,
  selected,
  dragging,
  onSelect,
  onStartDrag,
  onMoveDrag,
  onEndDrag,
  onCancelDrag,
  onStartConnection,
  onMoveConnection,
  onEndConnection,
  onRun,
  onDelete,
  busy,
  runBusy,
}: {
  node: WorkflowNode;
  position: CanvasPoint;
  image: DownloadableImage | null;
  selected: boolean;
  dragging: boolean;
  onSelect: () => void;
  onStartDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onMoveDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onEndDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onCancelDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onStartConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onMoveConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onEndConnection: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onRun: () => void;
  onDelete: () => void;
  busy: boolean;
  runBusy: boolean;
}) {
  const icon = {
    product_context: FileText,
    reference_image: ImagePlus,
    copy_generation: FileText,
    image_generation: ImageIcon,
  }[node.node_type];
  const Icon = icon;
  const summary = node.failure_reason
    ? node.failure_reason
    : typeof node.output_json?.summary === "string"
      ? node.output_json.summary
      : nodeIdleSummary(node);

  return (
    <div
      data-workflow-node-id={node.id}
      className={`absolute w-[248px] touch-none select-none rounded-2xl border bg-white p-3 text-left shadow-sm ${
        dragging ? "cursor-grabbing" : "transition-[border-color,box-shadow] hover:shadow-md"
      } ${
        selected ? "border-zinc-900 shadow-lg shadow-zinc-900/5 ring-2 ring-zinc-900/10" : "border-zinc-200"
      }`}
      style={{
        left: 0,
        top: 0,
        transform: `translate3d(${position.x}px, ${position.y}px, 0)`,
      }}
      onPointerDown={onStartDrag}
      onPointerMove={onMoveDrag}
      onPointerUp={onEndDrag}
      onPointerCancel={onCancelDrag}
      onLostPointerCapture={onCancelDrag}
    >
      <button
        type="button"
        data-node-action
        data-workflow-target-node-id={node.id}
        onClick={onSelect}
        className="absolute left-[-9px] top-[47px] z-20 h-[18px] w-[18px] rounded-full border border-zinc-300 bg-white shadow-sm hover:border-blue-400 hover:ring-4 hover:ring-blue-100"
        title="输入 handle"
        aria-label={`${node.title} 输入 handle`}
      />
      <button
        type="button"
        data-node-action
        onPointerDown={onStartConnection}
        onPointerMove={onMoveConnection}
        onPointerUp={onEndConnection}
        onPointerCancel={onEndConnection}
        className="absolute right-[-10px] top-[46px] z-20 h-5 w-5 rounded-full border-2 border-blue-500 bg-white shadow-sm hover:bg-blue-50 hover:ring-4 hover:ring-blue-100"
        title="拖拽连接输出"
        aria-label={`${node.title} 输出 handle`}
      />
      <div onClick={onSelect} className="cursor-grab active:cursor-grabbing">
        <div className="mb-3 flex items-start justify-between gap-2">
          <div className="flex min-w-0 gap-2">
            <span className="mt-0.5 rounded-lg border border-zinc-200 bg-zinc-50 p-1.5 text-zinc-500">
              <Icon size={14} />
            </span>
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold text-zinc-900">
                {node.title}
              </div>
              <div className="mt-0.5 text-[10px] uppercase tracking-wider text-zinc-400">
                {NODE_LABELS[node.node_type]}
              </div>
            </div>
          </div>
          <span
            className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(node.status)}`}
          >
            {NODE_STATUS_LABELS[node.status]}
          </span>
        </div>
        {image ? (
          <div
            className={`relative mb-2 flex h-28 items-center justify-center overflow-hidden rounded-xl border border-zinc-100 p-2 ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
          >
            <img
              src={image.previewUrl}
              alt={image.alt}
              className="h-full w-full object-contain"
            />
            <DownloadLink image={image} variant="overlay" />
          </div>
        ) : null}
        <div className="line-clamp-2 min-h-[32px] text-xs leading-relaxed text-zinc-500">
          {summary}
        </div>
      </div>
      <div className="mt-3 flex items-center justify-between text-[10px] text-zinc-400">
        <span>{node.last_run_at ? `最近 ${formatDateTime(node.last_run_at)}` : NODE_LABELS[node.node_type]}</span>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            data-node-action
            onClick={onDelete}
            disabled={busy}
            className="inline-flex items-center rounded border border-zinc-200 px-2 py-1 text-[11px] font-medium text-red-500 hover:border-red-300 hover:bg-red-50 disabled:opacity-50"
          >
            <Trash2 size={11} className="mr-1" /> 删除
          </button>
          <button
            type="button"
            data-node-action
            onClick={onRun}
            disabled={runBusy}
            className="inline-flex items-center rounded border border-zinc-200 px-2 py-1 text-[11px] font-medium text-zinc-600 hover:border-zinc-900 hover:text-zinc-900 disabled:opacity-50"
          >
            {runBusy ? (
              <Loader2 size={11} className="mr-1 animate-spin" />
            ) : (
              <Play size={11} className="mr-1" />
            )}
            运行
          </button>
        </div>
      </div>
    </div>
  );
}

function InspectorPanel({
  product,
  sourceImage,
  workflow,
  node,
  draft,
  onDraftChange,
  onSave,
  onSaveCopy,
  onRun,
  onUploadImage,
  onDelete,
  busy,
  runBusy,
  saveStatus,
}: {
  product: ProductDetail;
  sourceImage: DownloadableImage | null;
  workflow: ProductWorkflow | null;
  node: WorkflowNode;
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
  onSave: () => void;
  onSaveCopy: () => void;
  onRun: () => void;
  onUploadImage: (file: File) => void;
  onDelete: () => void;
  busy: boolean;
  runBusy: boolean;
  saveStatus: SaveStatus;
}) {
  const icon = {
    product_context: FileText,
    reference_image: ImagePlus,
    copy_generation: FileText,
    image_generation: ImageIcon,
  }[node.node_type];
  const InspectorIcon = icon;
  const incomingEdges =
    workflow?.edges.filter((edge) => edge.target_node_id === node.id) ?? [];
  const downstreamReferenceCount =
    node.node_type === "image_generation"
      ? new Set(
          workflow?.edges
            .filter((edge) => {
              if (edge.source_node_id !== node.id) {
                return false;
              }
              const target = workflow.nodes.find(
                (item) => item.id === edge.target_node_id,
              );
              return target?.node_type === "reference_image";
            })
            .map((edge) => edge.target_node_id) ?? [],
        ).size
      : 0;
  const hasReferenceImage = Boolean(
    node.node_type === "reference_image" &&
      Array.isArray(node.output_json?.source_asset_ids) &&
      node.output_json.source_asset_ids.length,
  );

  return (
    <div className="space-y-3">
      <section className="rounded-2xl border border-zinc-200 bg-white p-4 shadow-sm">
        <div className="flex items-start gap-3">
          <span className="rounded-xl border border-zinc-200 bg-zinc-50 p-2 text-zinc-500">
            <InspectorIcon size={16} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="truncate text-base font-semibold text-zinc-950">
              {draft.title || node.title}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              <span className="rounded-full border border-zinc-200 bg-zinc-50 px-2 py-0.5 text-[10px] font-medium text-zinc-600">
                {NODE_LABELS[node.node_type]}
              </span>
              <span
                className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(node.status)}`}
              >
                {node.status === "running" || node.status === "queued" ? (
                  <Loader2 size={11} className="mr-1 animate-spin" />
                ) : node.status === "failed" ? (
                  <XCircle size={11} className="mr-1" />
                ) : node.status === "succeeded" ? (
                  <CheckCircle2 size={11} className="mr-1" />
                ) : (
                  <Clock3 size={11} className="mr-1" />
                )}
                {NODE_STATUS_LABELS[node.status]}
              </span>
              <span
                className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium ${SAVE_STATUS_CLASS_NAMES[saveStatus]}`}
              >
                {saveStatus === "saving" ? (
                  <Loader2 size={11} className="mr-1 animate-spin" />
                ) : saveStatus === "saved" ? (
                  <CheckCircle2 size={11} className="mr-1" />
                ) : saveStatus === "failed" ? (
                  <XCircle size={11} className="mr-1" />
                ) : null}
                {SAVE_STATUS_LABELS[saveStatus]}
              </span>
            </div>
            {node.last_run_at ? (
              <div className="mt-2 text-[11px] text-zinc-400">
                最近 {formatDateTime(node.last_run_at)}
              </div>
            ) : null}
          </div>
        </div>

        <div className="mt-4 grid grid-cols-3 gap-2">
          <button
            type="button"
            onClick={onSave}
            disabled={busy}
            className="inline-flex items-center justify-center rounded-lg border border-zinc-300 bg-white px-3 py-2 text-xs font-medium text-zinc-700 hover:bg-zinc-50 disabled:opacity-50"
          >
            <Save size={13} className="mr-1.5" /> 保存
          </button>
          <button
            type="button"
            onClick={onRun}
            disabled={runBusy}
            className="inline-flex items-center justify-center rounded-lg bg-zinc-900 px-3 py-2 text-xs font-medium text-white hover:bg-zinc-800 disabled:opacity-50"
          >
            {runBusy ? (
              <Loader2 size={13} className="mr-1.5 animate-spin" />
            ) : (
              <Play size={13} className="mr-1.5" />
            )}
            运行
          </button>
          <button
            type="button"
            onClick={onDelete}
            disabled={busy}
            className="inline-flex items-center justify-center rounded-lg border border-red-200 bg-white px-3 py-2 text-xs font-medium text-red-600 hover:bg-red-50 disabled:opacity-50"
          >
            <Trash2 size={13} className="mr-1.5" /> 删除
          </button>
        </div>
      </section>

      <section className="rounded-2xl border border-zinc-200 bg-white p-4 shadow-sm">
        <div className="mb-3 text-[10px] font-semibold uppercase tracking-widest text-zinc-500">
          配置
        </div>
        <label className="mb-3 block">
          <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
            节点名称
          </span>
          <input
            value={draft.title}
            onChange={(event) =>
              onDraftChange({ ...draft, title: event.target.value })
            }
            className="w-full rounded-lg border border-zinc-200 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
          />
        </label>

        {node.node_type === "product_context" ? (
          <ProductContextInspector
            product={product}
            sourceImage={sourceImage}
            draft={draft}
            onDraftChange={onDraftChange}
          />
        ) : null}
        {node.node_type === "reference_image" ? (
          <ReferenceImageInspector
            draft={draft}
            onDraftChange={onDraftChange}
            onUploadImage={onUploadImage}
            busy={busy}
            hasImage={hasReferenceImage}
          />
        ) : null}
        {node.node_type === "copy_generation" ? (
          <CopyNodeInspector
            node={node}
            draft={draft}
            onDraftChange={onDraftChange}
            onSaveCopy={onSaveCopy}
            busy={busy}
          />
        ) : null}
        {node.node_type === "image_generation" ? (
          <ImageGenerationInspector
            draft={draft}
            onDraftChange={onDraftChange}
            incomingCount={incomingEdges.length}
            downstreamReferenceCount={downstreamReferenceCount}
          />
        ) : null}
      </section>

      <section className="rounded-2xl border border-zinc-200 bg-white p-4 shadow-sm">
        <div className="mb-3 text-[10px] font-semibold uppercase tracking-widest text-zinc-500">
          输出
        </div>
        {node.failure_reason ? (
          <div className="rounded-xl border border-red-200 bg-red-50 px-3 py-2 text-xs leading-relaxed text-red-700">
            <AlertCircle size={13} className="mr-1.5 inline" />
            {node.failure_reason}
          </div>
        ) : node.output_json ? (
          <NodeOutputPreview output={node.output_json} />
        ) : (
          <div className="rounded-xl border border-dashed border-zinc-200 bg-zinc-50/80 px-4 py-4 text-center text-xs text-zinc-500">
            暂无输出
          </div>
        )}
      </section>
    </div>
  );
}

function ProductContextInspector({
  product,
  sourceImage,
  draft,
  onDraftChange,
}: {
  product: ProductDetail;
  sourceImage: DownloadableImage | null;
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
}) {
  return (
    <div className="space-y-3">
      <div
        className={`relative flex h-40 items-center justify-center overflow-hidden rounded-xl border border-zinc-200 p-2 ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
      >
        {sourceImage ? (
          <>
            <img
              src={sourceImage.previewUrl}
              alt={sourceImage.alt}
              className="h-full w-full object-contain"
            />
            <DownloadLink image={sourceImage} variant="overlay" />
          </>
        ) : (
          <div className="text-xs text-zinc-400">暂无商品源图</div>
        )}
      </div>
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          商品名称
        </span>
        <input
          value={draft.productName}
          onChange={(event) =>
            onDraftChange({ ...draft, productName: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </label>
      <div className="grid grid-cols-2 gap-2">
        <label className="block">
          <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
            类目
          </span>
          <input
            value={draft.category}
            onChange={(event) =>
              onDraftChange({ ...draft, category: event.target.value })
            }
            className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
          />
        </label>
        <label className="block">
          <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
            价格
          </span>
          <input
            value={draft.price}
            onChange={(event) =>
              onDraftChange({ ...draft, price: event.target.value })
            }
            className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
          />
        </label>
      </div>
      <TextArea
        label="商品描述"
        value={draft.sourceNote}
        onChange={(value) => onDraftChange({ ...draft, sourceNote: value })}
      />
      <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-xs text-zinc-500">
        原始商品：{product.name}
        {product.category ? ` · ${product.category}` : ""}
        {product.price ? ` · ${formatPrice(product.price)}` : ""}
      </div>
    </div>
  );
}

function ReferenceImageInspector({
  draft,
  onDraftChange,
  onUploadImage,
  busy,
  hasImage,
}: {
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
  onUploadImage: (file: File) => void;
  busy: boolean;
  hasImage: boolean;
}) {
  return (
    <div className="space-y-3">
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          标签
        </span>
        <input
          value={draft.label}
          onChange={(event) =>
            onDraftChange({ ...draft, label: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </label>
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          角色
        </span>
        <select
          value={draft.role}
          onChange={(event) =>
            onDraftChange({ ...draft, role: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        >
          <option value="reference">参考图</option>
          <option value="style">风格图</option>
          <option value="product_angle">商品角度</option>
        </select>
      </label>
      <ImageDropZone
        ariaLabel={hasImage ? "替换参考图" : "上传参考图"}
        disabled={busy}
        className="flex cursor-pointer items-center justify-center rounded-md border border-dashed border-zinc-300 px-3 py-6 text-xs font-medium text-zinc-600 transition-colors hover:bg-zinc-50"
        onFiles={(files) => {
          const file = files[0];
          if (file) {
            onUploadImage(file);
          }
        }}
      >
        {({ isDragging }) => (
          <>
            <Upload size={14} className="mr-2" />
            {isDragging ? "松开以上传图片" : hasImage ? "拖拽或点击替换图片" : "拖拽或点击上传图片"}
          </>
        )}
      </ImageDropZone>
    </div>
  );
}

function CopyNodeInspector({
  node,
  draft,
  onDraftChange,
  onSaveCopy,
  busy,
}: {
  node: WorkflowNode;
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
  onSaveCopy: () => void;
  busy: boolean;
}) {
  const hasCopy = Boolean(
    node.output_json && outputText(node.output_json, "copy_set_id"),
  );
  return (
    <div className="space-y-3">
      <TextArea
        label="文案指令"
        value={draft.instruction}
        onChange={(value) => onDraftChange({ ...draft, instruction: value })}
      />
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          语气
        </span>
        <input
          value={draft.tone}
          onChange={(event) =>
            onDraftChange({ ...draft, tone: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </label>
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          渠道
        </span>
        <input
          value={draft.channel}
          onChange={(event) =>
            onDraftChange({ ...draft, channel: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </label>
      {hasCopy ? (
        <div className="space-y-3 rounded-md border border-zinc-200 bg-zinc-50 p-3">
          <div className="text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
            编辑文案
          </div>
          <label className="block">
            <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
              标题
            </span>
            <input
              value={draft.copyTitle}
              onChange={(event) =>
                onDraftChange({ ...draft, copyTitle: event.target.value })
              }
              className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
            />
          </label>
          <TextArea
            label="卖点"
            value={draft.copySellingPoints}
            onChange={(value) =>
              onDraftChange({ ...draft, copySellingPoints: value })
            }
          />
          <label className="block">
            <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
              海报标题
            </span>
            <input
              value={draft.copyPosterHeadline}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  copyPosterHeadline: event.target.value,
                })
              }
              className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
            />
          </label>
          <label className="block">
            <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
              CTA
            </span>
            <input
              value={draft.copyCta}
              onChange={(event) =>
                onDraftChange({ ...draft, copyCta: event.target.value })
              }
              className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
            />
          </label>
          <button
            type="button"
            onClick={onSaveCopy}
            disabled={busy}
            className="inline-flex w-full items-center justify-center rounded-md bg-zinc-900 px-3 py-2 text-xs font-medium text-white hover:bg-zinc-800 disabled:opacity-50"
          >
            <Save size={13} className="mr-1.5" /> 保存文案
          </button>
        </div>
      ) : null}
    </div>
  );
}

function ImageGenerationInspector({
  draft,
  onDraftChange,
  incomingCount,
  downstreamReferenceCount,
}: {
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
  incomingCount: number;
  downstreamReferenceCount: number;
}) {
  return (
    <div className="space-y-3">
      <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-xs text-zinc-600">
        上游 {incomingCount} · 下游参考图 {downstreamReferenceCount || "可选"}
      </div>
      <TextArea
        label="生图"
        value={draft.instruction}
        onChange={(value) => onDraftChange({ ...draft, instruction: value })}
      />
      <label className="block">
        <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          尺寸
        </span>
        <input
          value={draft.size}
          onChange={(event) =>
            onDraftChange({ ...draft, size: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 px-3 py-2 text-xs outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </label>
    </div>
  );
}

function TextArea({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
        {label}
      </span>
      <textarea
        value={value}
        onChange={(event) => onChange(event.target.value)}
        rows={5}
        className="w-full resize-none rounded-md border border-zinc-200 px-3 py-2 text-xs leading-relaxed outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
      />
    </label>
  );
}
