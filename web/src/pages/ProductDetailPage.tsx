import { useEffect, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  ChevronLeft,
  ChevronRight,
  CircleDot,
  Image as ImageIcon,
  Loader2,
  Maximize2,
  Minimize2,
  Play,
  Plus,
  Settings2,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { DEFAULT_IMAGE_SIZE_OPTIONS } from "../lib/imageSizes";
import type {
  ProductWorkflow,
  WorkflowNode,
  WorkflowNodeType,
} from "../lib/types";
import {
  ADD_NODE_OPTIONS,
  CANVAS_MIN_HEIGHT,
  CANVAS_MIN_WIDTH,
  MAX_INSPECTOR_WIDTH,
  MIN_INSPECTOR_WIDTH,
} from "./product-detail/constants";
import { buildEdgePath } from "./product-detail/canvasUtils";
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
import type { NodeConfigDraft, SaveStatus } from "./product-detail/types";
import { clamp, hasActiveWorkflow, outputText, readStoredNumber } from "./product-detail/utils";
import {
  defaultConfigForType,
  defaultTitleForType,
  draftFromNode,
  nodeConfigFromDraft,
} from "./product-detail/workflowConfig";
import type { DownloadableImage } from "../lib/image-downloads";
import {
  buildConnectionDragPath,
  useWorkflowCanvas,
} from "./product-detail/useWorkflowCanvas";
import type { NodePositionCommitInput } from "./product-detail/useWorkflowCanvas";

type SidebarTab = "details" | "runs" | "images";

type WorkflowCanvasMutationBridge = {
  acceptNodePositionMutation: (nodeId: string, mutationVersion: number) => boolean;
  clearOptimisticNodePosition: (nodeId: string) => void;
};

export function ProductDetailPage() {
  const { productId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const previousBodyUserSelectRef = useRef<string | null>(null);
  const workflowCanvasRef = useRef<WorkflowCanvasMutationBridge | null>(null);
  const wasWorkflowActiveRef = useRef(false);
  const draftVersionRef = useRef(0);
  const previousDraftNodeIdRef = useRef<string | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [activeSidebarTab, setActiveSidebarTab] = useState<SidebarTab>("details");
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [topChromeCollapsed, setTopChromeCollapsed] = useState(false);
  const [draft, setDraft] = useState<NodeConfigDraft>(() =>
    draftFromNode(null),
  );
  const [draftDirty, setDraftDirty] = useState(false);
  const [saveStatus, setSaveStatus] = useState<SaveStatus>("idle");
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
  const imageSizeOptions = DEFAULT_IMAGE_SIZE_OPTIONS;

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
      const nextPosition = workflowCanvas.getViewportCenterNodePosition();
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
      if (!workflowCanvasRef.current?.acceptNodePositionMutation(input.node.id, input.mutationVersion)) {
        return;
      }
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      workflowCanvasRef.current.clearOptimisticNodePosition(input.node.id);
    },
    onError: (mutationError, _input, context) => {
      if (!workflowCanvasRef.current?.acceptNodePositionMutation(_input.node.id, _input.mutationVersion)) {
        return;
      }
      if (context?.previous) {
        queryClient.setQueryData(
          ["product-workflow", productId],
          context.previous,
        );
      }
      workflowCanvasRef.current.clearOptimisticNodePosition(_input.node.id);
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
  const workflowCanvas = useWorkflowCanvas({
    workflow,
    zoomStorageKey: "productflow.workflow.zoom",
    structureBusy,
    onSelectNode: selectNodeForDetails,
    onNodePositionCommit: (input: NodePositionCommitInput) => {
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
    },
    onConnectionCreate: (input) => createEdgeMutation.mutate(input),
  });
  workflowCanvasRef.current = workflowCanvas;
  const {
    canvasScrollRef,
    canvasRef,
    zoom,
    nodeDrag,
    connectionDrag,
    panePan,
    updateZoom,
    getRenderedNodePosition,
    getOutputHandlePoint,
    getInputHandlePoint,
    getCanvasSize,
    setNodeElementRef,
    setEdgePathRef,
    setEdgeDeleteButtonRef,
    startPanePan,
    movePanePan,
    endPanePan,
    cancelPanePan,
    handleCanvasWheel,
    startNodeDrag,
    moveNodeDrag,
    endNodeDrag,
    cancelNodeDrag,
    startConnectionDrag,
    moveConnectionDrag,
    endConnectionDrag,
  } = workflowCanvas;

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
  const canvasSize = getCanvasSize({
    minWidth: CANVAS_MIN_WIDTH,
    minHeight: CANVAS_MIN_HEIGHT,
  });
  const canvasWidth = canvasSize.width;
  const canvasHeight = canvasSize.height;
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

  const renderWorkflowActions = (variant: "panel" | "rail") => {
    const isRail = variant === "rail";
    return (
      <div
        data-canvas-control
        className={
          isRail
            ? "rounded-xl border border-zinc-200 bg-white/90 p-2 shadow-sm backdrop-blur"
            : "mb-4 rounded-2xl border border-zinc-200 bg-white p-4 shadow-sm"
        }
      >
        {!isRail ? (
          <div className="mb-3 flex items-center justify-between">
            <div>
              <div className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">工作流操作</div>
              <div className="mt-1 text-[11px] text-zinc-400">添加节点或运行当前画布。</div>
            </div>
          </div>
        ) : null}
        <button
          type="button"
          onClick={() => void handleRunWorkflow(undefined)}
          disabled={runBusy || !workflow}
          className={
            isRail
              ? "inline-flex w-full items-center justify-center rounded-lg bg-zinc-900 px-2 py-2 text-[10px] font-medium text-white transition-colors hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-50"
              : "inline-flex w-full items-center justify-center rounded-lg bg-zinc-900 px-3 py-2 text-xs font-medium text-white transition-colors hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-50"
          }
          title={runBusy ? "工作流运行中" : "运行整个工作流"}
          aria-label={runBusy ? "工作流运行中" : "运行整个工作流"}
        >
          {runBusy ? (
            <Loader2 size={13} className={isRail ? "animate-spin" : "mr-1.5 animate-spin"} />
          ) : (
            <Play size={13} className={isRail ? "" : "mr-1.5"} />
          )}
          {isRail ? null : runBusy ? "运行中" : "运行工作流"}
        </button>
        <div className={isRail ? "mt-2 flex flex-col gap-1.5" : "mt-3 grid grid-cols-2 gap-2"}>
          {ADD_NODE_OPTIONS.map((option) => (
            <button
              key={option.type}
              type="button"
              onClick={() => createNodeMutation.mutate(option.type)}
              disabled={structureBusy || !workflow}
              className={
                isRail
                  ? "inline-flex items-center justify-center rounded-lg border border-zinc-200 bg-white px-2 py-1.5 text-[10px] font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:bg-zinc-50 disabled:cursor-not-allowed disabled:opacity-50"
                  : "inline-flex items-center justify-center rounded-lg border border-zinc-200 bg-white px-2.5 py-2 text-xs font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:bg-zinc-50 disabled:cursor-not-allowed disabled:opacity-50"
              }
              title={`添加${option.label}节点`}
              aria-label={`添加${option.label}节点`}
            >
              <Plus size={13} className="mr-1" />
              {option.label}
            </button>
          ))}
        </div>
      </div>
    );
  };

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-white text-sm text-zinc-900">
      {!topChromeCollapsed ? <TopNav onHome={() => navigate("/products")} breadcrumbs={product.name} /> : null}

      <main className="flex min-h-0 flex-1 flex-col border-t border-zinc-200 bg-[#f7f7f8]">
        {error ? (
          <div className="z-20 border-b border-red-200 bg-red-50 px-4 py-2 text-xs text-red-700">
            <AlertCircle size={14} className="mr-2 inline" /> {error}
          </div>
        ) : null}

        <div className="relative flex min-h-0 flex-1 overflow-hidden">
          <div className="absolute inset-0 bg-[radial-gradient(#d4d4d8_1px,transparent_1px)] [background-size:18px_18px]" />
          <section className="relative z-10 min-w-0 flex-1 overflow-hidden">
            <div data-canvas-control className="pointer-events-none absolute right-4 top-4 z-30">
              <button
                type="button"
                onClick={() => setTopChromeCollapsed((collapsed) => !collapsed)}
                className="pointer-events-auto inline-flex h-9 w-9 items-center justify-center rounded-lg border border-zinc-200 bg-white/90 text-zinc-600 shadow-sm backdrop-blur transition-colors hover:bg-white hover:text-zinc-900"
                aria-label={topChromeCollapsed ? "还原画布布局" : "最大化画布"}
                title={topChromeCollapsed ? "还原画布布局" : "最大化画布"}
              >
                {topChromeCollapsed ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
              </button>
            </div>
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
                      d={buildConnectionDragPath(connectionDrag)}
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

          {sidebarCollapsed ? (
            <>
            <div data-canvas-control className="group/sidebar-expand absolute right-0 top-0 z-30 flex h-full w-8 items-center justify-center">
              <button
                type="button"
                onClick={() => setSidebarCollapsed(false)}
                className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-zinc-200 bg-white/95 text-zinc-500 opacity-0 shadow-sm transition-opacity hover:text-zinc-900 focus:opacity-100 focus:outline-none group-hover/sidebar-expand:opacity-100"
                aria-label="展开右侧栏"
                title="展开右侧栏"
              >
                <ChevronLeft size={14} />
              </button>
            </div>
            <div className="absolute right-4 top-16 z-30 flex flex-col gap-2">
              {renderWorkflowActions("rail")}
              <div data-canvas-control className="flex flex-col gap-2 rounded-xl border border-zinc-200 bg-white/90 p-2 shadow-sm backdrop-blur">
                <SidebarTabButton active={false} label="详情" title="Details" icon={<Settings2 size={15} />} onClick={() => { setActiveSidebarTab("details"); setSidebarCollapsed(false); }} />
                <SidebarTabButton active={false} label="运行" title="Runs" icon={<CircleDot size={15} />} onClick={() => { setActiveSidebarTab("runs"); setSidebarCollapsed(false); }} />
                <SidebarTabButton active={false} label="图片" title="Images" icon={<ImageIcon size={15} />} onClick={() => { setActiveSidebarTab("images"); setSidebarCollapsed(false); }} />
              </div>
            </div>
            </>
          ) : (
          <div className="relative z-20 flex shrink-0 border-l border-zinc-200 bg-white/95 shadow-[-8px_0_24px_-20px_rgba(0,0,0,0.35)] backdrop-blur">
            <div data-canvas-control className="group/sidebar-collapse absolute left-[-28px] top-0 z-30 flex h-full w-7 items-center justify-center">
              <button
                type="button"
                onClick={() => setSidebarCollapsed(true)}
                className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-zinc-200 bg-white/95 text-zinc-500 opacity-0 shadow-sm transition-opacity hover:text-zinc-900 focus:opacity-100 focus:outline-none group-hover/sidebar-collapse:opacity-100"
                aria-label="折叠右侧栏"
                title="折叠右侧栏"
              >
                <ChevronRight size={14} />
              </button>
            </div>
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
              <div className="flex h-12 shrink-0 items-center justify-between border-b border-zinc-200 px-4">
                <div className="flex items-center">
                {activeSidebarTab === "details" ? <Settings2 size={14} className="mr-2 text-zinc-400" /> : null}
                {activeSidebarTab === "runs" ? <CircleDot size={14} className="mr-2 text-zinc-400" /> : null}
                {activeSidebarTab === "images" ? <ImageIcon size={14} className="mr-2 text-zinc-400" /> : null}
                <span className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                  {activeSidebarTab === "details" ? "详情" : activeSidebarTab === "runs" ? "运行记录" : "图片"}
                </span>
                </div>
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto p-4">
                {renderWorkflowActions("panel")}
                {activeSidebarTab === "details" ? (
                  selectedNode ? (
                    <InspectorPanel
                      product={product}
                      sourceImage={sourceImage}
                      workflow={workflow}
                      node={selectedNode}
                      draft={draft}
                      imageSizeOptions={imageSizeOptions}
                      onDraftChange={handleDraftChange}
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
          )}
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
