import { useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  ChevronLeft,
  ChevronRight,
  CircleDot,
  Check,
  Eye,
  Save,
  Image as ImageIcon,
  Layers3,
  Loader2,
  Maximize2,
  Minimize2,
  Play,
  Plus,
  Settings2,
  Trash2,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

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
import { TemplateGroupsPanel } from "./product-detail/TemplateGroupsPanel";
import { WorkflowNodeCard } from "./product-detail/WorkflowNodeCard";
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
import { connectionDescription, localizedWorkflowNodeTypeLabel } from "./product-detail/nodeDisplay";
import type { NodeConfigDraft, SaveStatus } from "./product-detail/types";
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
import {
  buildConnectionDragPath,
  useWorkflowCanvas,
} from "./product-detail/useWorkflowCanvas";
import type { NodePositionCommitInput } from "./product-detail/useWorkflowCanvas";

type SidebarTab = "singleNode" | "templates" | "details" | "runs" | "images";

type WorkflowCanvasMutationBridge = {
  acceptNodePositionMutation: (nodeId: string, mutationVersion: number) => boolean;
  clearOptimisticNodePosition: (nodeId: string) => void;
};

export function ProductDetailPage() {
  const { t } = useI18n();
  const { productId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const previousBodyUserSelectRef = useRef<string | null>(null);
  const workflowCanvasRef = useRef<WorkflowCanvasMutationBridge | null>(null);
  const wasWorkflowActiveRef = useRef(false);
  const draftVersionRef = useRef(0);
  const previousDraftNodeIdRef = useRef<string | null>(null);
  const skipNextCanvasBlankClickRef = useRef(false);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([]);
  const [templateSaveTitle, setTemplateSaveTitle] = useState("");
  const [templateSaveDescription, setTemplateSaveDescription] = useState("");
  const [templateSaveOpen, setTemplateSaveOpen] = useState(false);
  const [activeSidebarTab, setActiveSidebarTab] = useState<SidebarTab>("details");
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [topChromeCollapsed, setTopChromeCollapsed] = useState(false);
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
    if (event.ctrlKey || event.metaKey || event.shiftKey) {
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

  const handleCanvasBlankClick = (event: ReactMouseEvent<HTMLDivElement>) => {
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
  };

  const replaceSelectionFromBox = (nodeIds: string[]) => {
    skipNextCanvasBlankClickRef.current = true;
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
      const nextPosition = workflowCanvas.getViewportCenterNodePosition();
      const nextWorkflow = await api.applyWorkflowTemplateGroup(productId, {
        template_key: template.key,
        position_x: nextPosition.x,
        position_y: nextPosition.y,
      });
      return { nextWorkflow, previousNodeIds };
    },
    onSuccess: async ({ nextWorkflow, previousNodeIds }) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      await queryClient.invalidateQueries({ queryKey: ["product-workflow", productId] });
      const createdNodes = nextWorkflow.nodes.filter((node) => !previousNodeIds.has(node.id));
      const selectedCreatedNode =
        createdNodes.find((node) => node.node_type === "copy_generation") ??
        createdNodes.find((node) => node.node_type === "image_generation") ??
        createdNodes[0] ??
        null;
      setSelectedNodeId(selectedCreatedNode?.id ?? null);
      setSelectedNodeIds(selectedCreatedNode ? [selectedCreatedNode.id] : []);
      setActiveSidebarTab("details");
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.applyTemplate"),
      );
    },
  });

  const createUserTemplateGroupMutation = useMutation({
    mutationFn: async () => {
      if (selectedNodeIds.length < 2) {
        throw new Error(t("detail.error.selectNodesToSave"));
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
      await queryClient.invalidateQueries({ queryKey: ["canvas-templates"] });
    },
    onError: (mutationError) => {
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
          : t("detail.error.moveNode"),
      );
    },
  });

  const createEdgeMutation = useMutation({
    mutationFn: async (input: { sourceNodeId: string; targetNodeId: string }) => {
      const currentWorkflow = queryClient.getQueryData<ProductWorkflow>(["product-workflow", productId]) ?? workflow;
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
      return { nextWorkflow, sourceNodeId: input.sourceNodeId, targetNodeId: input.targetNodeId };
    },
    onSuccess: ({ nextWorkflow, sourceNodeId, targetNodeId }) => {
      const source = nextWorkflow.nodes.find((node) => node.id === sourceNodeId);
      const target = nextWorkflow.nodes.find((node) => node.id === targetNodeId);
      setError("");
      setNotice(source && target ? connectionDescription(source, target, t) : "");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
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
    mutationFn: (edgeId: string) => api.deleteWorkflowEdge(edgeId),
    onSuccess: (nextWorkflow) => {
      setError("");
      setNotice("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      setSelectedNodeIds(clearSelectedNodeGroup(selectedNodeId));
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.deleteEdge"),
      );
    },
  });

  const deleteNodeMutation = useMutation({
    mutationFn: (nodeId: string) => api.deleteWorkflowNode(nodeId),
    onSuccess: (nextWorkflow, nodeId) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      const fallbackPrimaryNodeId = selectedNodeId === nodeId ? nextWorkflow.nodes[0]?.id ?? null : selectedNodeId;
      const nextSelection = deleteNodeFromSelection(selectedNodeIds, nodeId, fallbackPrimaryNodeId);
      setSelectedNodeId(nextSelection.primaryNodeId);
      setSelectedNodeIds(nextSelection.selectedNodeIds);
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : t("detail.error.deleteNode"),
      );
    },
  });

  const handleDeleteNode = (node: WorkflowNode) => {
    if (!window.confirm(t("detail.confirm.deleteNode", { title: node.title }))) {
      return;
    }
    deleteNodeMutation.mutate(node.id);
  };

  const deleteSelectedNodesMutation = useMutation({
    mutationFn: async (nodeIds: string[]) => {
      if (nodeIds.length < 2) {
        throw new Error(t("detail.error.selectNodesToDelete"));
      }
      let nextWorkflow: ProductWorkflow | null = null;
      for (const nodeId of nodeIds) {
        nextWorkflow = await api.deleteWorkflowNode(nodeId);
      }
      if (!nextWorkflow) {
        throw new Error(t("detail.error.deleteNode"));
      }
      return nextWorkflow;
    },
    onSuccess: (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
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

  const handleDeleteSelectedNodes = () => {
    if (selectedNodeIds.length < 2) {
      return;
    }
    if (!window.confirm(t("detail.confirm.deleteSelectedNodes", { count: selectedNodeIds.length }))) {
      return;
    }
    deleteSelectedNodesMutation.mutate([...selectedNodeIds]);
  };

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
  const workflowCanvas = useWorkflowCanvas({
    workflow,
    zoomStorageKey: "productflow.workflow.zoom",
    structureBusy,
    onSelectNode: selectNodeForDetails,
    onNodeDragStartSelect: selectNodeForDragStart,
    getNodeDragGroup: (nodeId) => (selectedNodeIds.includes(nodeId) ? selectedNodeIds : [nodeId]),
    onSelectionBoxComplete: replaceSelectionFromBox,
    onNodePositionCommit: commitNodePosition,
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
    selectionBoxRect,
    previewSelectedNodeIds,
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
  const selectedNodeIdSet = new Set(selectedNodeIds);
  const selectedGroupCount = selectedNodeIds.length;
  const fillReferenceBusy = bindNodeImageMutation.isPending;
  const queueOverview = queueOverviewQuery.data ?? null;
  const showQueueOverview = Boolean(queueOverview && queueOverview.active_count > 0);
  const canvasTemplates = canvasTemplatesQuery.data?.items ?? [];
  const userTemplateMutationBusy =
    createUserTemplateGroupMutation.isPending ||
    updateUserTemplateGroupMutation.isPending ||
    archiveUserTemplateGroupMutation.isPending;

  const renderWorkflowToolbarButtons = () => (
    <>
      <span className="w-full text-center text-[10px] font-semibold leading-none text-slate-500">
        {t("detail.toolbar.runSection")}
      </span>
      <button
        type="button"
        onClick={() => void handleRunWorkflow(undefined)}
        disabled={fullWorkflowRunBusy || !workflow}
        className="flex w-full flex-col items-center rounded-lg bg-indigo-600 px-1.5 py-2 text-xs font-semibold text-white transition-colors hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
        title={fullWorkflowRunBusy ? t("detail.workflowRunning") : t("detail.runWorkflow")}
        aria-label={fullWorkflowRunBusy ? t("detail.workflowRunning") : t("detail.runWorkflow")}
      >
        {fullWorkflowRunBusy ? <Loader2 size={17} className="animate-spin" /> : <Play size={17} />}
        <span className="mt-1 leading-tight">{fullWorkflowRunBusy ? t("detail.running") : t("detail.run")}</span>
      </button>
      <div className="my-1 h-px w-11 self-center bg-slate-700/80" />
      <span className="w-full text-center text-[10px] font-semibold leading-none text-slate-500">
        {t("detail.toolbar.addSection")}
      </span>
    </>
  );

  const renderToolbarViewDivider = () => (
    <>
      <div className="my-1 h-px w-11 self-center bg-slate-700/80" />
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
        return (
          <div
            key={option.type}
            className="rounded-lg border border-zinc-200 bg-white px-3 py-3 dark:border-slate-700/80 dark:bg-slate-900/70"
          >
            <div className="flex items-start">
              <span className="mt-0.5 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600 dark:bg-violet-500/14 dark:text-violet-200">
                <Plus size={16} />
              </span>
              <span className="ml-3 min-w-0 flex-1">
                <span className="block text-sm font-semibold text-zinc-900 dark:text-slate-100">{optionLabel}</span>
                <span className="mt-1 block text-xs leading-5 text-zinc-500 dark:text-slate-400">
                  {description}
                </span>
              </span>
            </div>
            <div className="mt-3 flex justify-end">
              <button
                type="button"
                onClick={() => createNodeMutation.mutate(option.type)}
                disabled={structureBusy || !workflow}
                className="inline-flex h-8 items-center rounded-md bg-zinc-950 px-2.5 text-xs font-medium text-white transition-colors hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400"
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
          <div className="absolute inset-0 bg-[radial-gradient(#cbd5e1_1px,transparent_1px)] [background-size:18px_18px] dark:bg-[radial-gradient(rgba(148,163,184,0.2)_1px,transparent_1px)]" />
          <div className="absolute inset-0 bg-gradient-to-br from-white/60 via-transparent to-indigo-50/40 dark:from-[#060a12]/78 dark:via-transparent dark:to-[#151f33]/70" />
          <section className="relative z-10 min-w-0 flex-1 overflow-hidden">
            <div data-canvas-control className="pointer-events-none absolute right-4 top-4 z-30">
              <button
                type="button"
                onClick={() => setTopChromeCollapsed((collapsed) => !collapsed)}
                className="pointer-events-auto inline-flex h-9 w-9 items-center justify-center rounded-lg border border-zinc-200 bg-white/90 text-zinc-600 shadow-sm backdrop-blur transition-colors hover:bg-white hover:text-zinc-900 dark:border-slate-700/80 dark:bg-[#151f33]/92 dark:text-slate-300 dark:shadow-black/20 dark:hover:bg-[#1a2740] dark:hover:text-white"
                aria-label={topChromeCollapsed ? t("detail.restoreCanvas") : t("detail.maximizeCanvas")}
                title={topChromeCollapsed ? t("detail.restoreCanvas") : t("detail.maximizeCanvas")}
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
              onClick={handleCanvasBlankClick}
              onWheel={handleCanvasWheel}
            >
              {workflowQuery.isLoading ? (
                <div className="flex h-full items-center justify-center text-zinc-400 dark:text-slate-500">
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
                        stroke="#94a3b8"
                        strokeWidth="1.7"
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
                      className="absolute z-20 flex h-5 w-5 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border border-zinc-300 bg-white text-[12px] leading-none text-zinc-500 shadow-sm hover:border-red-300 hover:bg-red-50 hover:text-red-600 disabled:opacity-50 dark:border-slate-600 dark:bg-[#0f1726] dark:text-slate-300 dark:hover:border-red-400/60 dark:hover:bg-red-500/10 dark:hover:text-red-200"
                      style={{
                        left: (start.x + end.x) / 2,
                        top: (start.y + end.y) / 2,
                      }}
                      title={t("detail.deleteEdge")}
                      aria-label={t("detail.deleteEdge")}
                    >
                      ×
                    </button>
                  );
                })}
                    {selectionBoxRect ? (
                      <div
                        className="pointer-events-none absolute z-30 rounded-md border border-indigo-400 bg-indigo-500/10 shadow-[0_0_0_1px_rgba(99,102,241,0.12)]"
                        style={{
                          left: selectionBoxRect.x,
                          top: selectionBoxRect.y,
                          width: selectionBoxRect.width,
                          height: selectionBoxRect.height,
                        }}
                      />
                    ) : null}
                    {workflow.nodes.map((node) => (
                      <WorkflowNodeCard
                        key={node.id}
                        node={node}
                        nodeRef={(element) => setNodeElementRef(node.id, element)}
                        position={getRenderedNodePosition(node)}
                        image={getNodeImageDownload(node, product, t)}
                        primarySelected={node.id === selectedNode?.id}
                        secondarySelected={selectedNodeIdSet.has(node.id) && node.id !== selectedNode?.id}
                        previewSelected={previewSelectedNodeIds.includes(node.id)}
                        dragging={nodeDrag?.nodeIds.includes(node.id) ?? false}
                        onSelect={(event) => selectNodeFromPointer(node.id, event)}
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
                        runActionState={getWorkflowNodeRunActionState(node, {
                          runSubmissionPending,
                          pendingStartNodeId,
                        })}
                      />
                    ))}
                  </div>
                </div>
              ) : (
                <div className="flex h-full items-center justify-center text-xs text-zinc-500 dark:text-slate-400">
                  {t("detail.workflowLoadFailed")}
                </div>
              )}
            </div>

            <div data-canvas-control className="pointer-events-none absolute bottom-4 left-4 z-30">
              <div className="pointer-events-auto flex items-center gap-1 rounded-lg border border-zinc-200 bg-white/90 p-1 shadow-sm backdrop-blur dark:border-slate-700/80 dark:bg-[#151f33]/92 dark:shadow-black/20">
              <button
                type="button"
                onClick={() => updateZoom(zoom - 0.1)}
                className="inline-flex items-center rounded px-2 py-1 text-xs text-zinc-600 hover:bg-zinc-50 dark:text-slate-300 dark:hover:bg-violet-500/15 dark:hover:text-white"
                aria-label={t("detail.zoomOut")}
              >
                <ZoomOut size={13} />
              </button>
              <button
                type="button"
                onClick={() => updateZoom(1)}
                className="rounded px-2 py-1 text-xs tabular-nums text-zinc-600 hover:bg-zinc-50 dark:text-slate-300 dark:hover:bg-violet-500/15 dark:hover:text-white"
                aria-label={t("detail.resetZoom")}
              >
                {Math.round(zoom * 100)}%
              </button>
              <button
                type="button"
                onClick={() => updateZoom(zoom + 0.1)}
                className="inline-flex items-center rounded px-2 py-1 text-xs text-zinc-600 hover:bg-zinc-50 dark:text-slate-300 dark:hover:bg-violet-500/15 dark:hover:text-white"
                aria-label={t("detail.zoomIn")}
              >
                <ZoomIn size={13} />
              </button>
              </div>
            </div>
            {selectedGroupCount > 1 ? (
              <div data-canvas-control className="pointer-events-none absolute left-1/2 top-4 z-30 -translate-x-1/2">
                <div className="pointer-events-auto min-w-[22rem] rounded-xl border border-indigo-200 bg-white/95 p-2.5 text-sm font-semibold text-indigo-700 shadow-lg shadow-indigo-950/10 backdrop-blur dark:border-violet-400/50 dark:bg-[#151f33]/95 dark:text-violet-100 dark:shadow-black/30">
                  <div className="flex items-center gap-2">
                    <Check size={16} strokeWidth={2.5} />
                    <span className="mr-auto">{t("detail.selectedCount", { count: selectedGroupCount })}</span>
                    <button
                      type="button"
                      onClick={() => setTemplateSaveOpen((open) => !open)}
                      disabled={createUserTemplateGroupMutation.isPending}
                      className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-indigo-200 bg-indigo-50 px-2.5 text-xs font-semibold text-indigo-700 shadow-sm transition-colors hover:border-indigo-300 hover:bg-indigo-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-violet-400/45 dark:bg-violet-500/16 dark:text-violet-100 dark:hover:border-violet-300/70 dark:hover:bg-violet-500/25"
                    >
                      {createUserTemplateGroupMutation.isPending ? (
                        <Loader2 size={14} className="animate-spin" />
                      ) : (
                        <Save size={14} />
                      )}
                      {t("detail.saveTemplate")}
                    </button>
                    <button
                      type="button"
                      onClick={handleDeleteSelectedNodes}
                      disabled={deleteSelectedNodesMutation.isPending || structureBusy}
                      className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-red-200 bg-red-50 px-2.5 text-xs font-semibold text-red-600 shadow-sm transition-colors hover:border-red-300 hover:bg-red-100 hover:text-red-700 disabled:cursor-not-allowed disabled:opacity-50 dark:border-red-400/40 dark:bg-red-500/10 dark:text-red-200 dark:hover:border-red-400/60 dark:hover:bg-red-500/16 dark:hover:text-red-100"
                    >
                      {deleteSelectedNodesMutation.isPending ? (
                        <Loader2 size={14} className="animate-spin" />
                      ) : (
                        <Trash2 size={14} />
                      )}
                      {t("detail.delete")}
                    </button>
                    <button
                      type="button"
                      onClick={clearMultiSelection}
                      className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-red-200 bg-red-50 text-red-600 shadow-sm transition-colors hover:border-red-300 hover:bg-red-100 hover:text-red-700 dark:border-red-400/40 dark:bg-red-500/10 dark:text-red-200 dark:hover:border-red-400/60 dark:hover:bg-red-500/16 dark:hover:text-red-100"
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
                        className="h-9 rounded-lg border border-zinc-200 bg-white px-3 text-xs font-medium text-zinc-900 outline-none transition-colors placeholder:text-zinc-400 focus:border-indigo-300 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400"
                        placeholder={t("detail.templateName")}
                        maxLength={255}
                      />
                      <input
                        value={templateSaveDescription}
                        onChange={(event) => setTemplateSaveDescription(event.target.value)}
                        className="h-9 rounded-lg border border-zinc-200 bg-white px-3 text-xs font-medium text-zinc-900 outline-none transition-colors placeholder:text-zinc-400 focus:border-indigo-300 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400"
                        placeholder={t("detail.templateDescription")}
                        maxLength={1000}
                      />
                      <div className="flex justify-end gap-2">
                        <button
                          type="button"
                          onClick={() => setTemplateSaveOpen(false)}
                          className="h-8 rounded-lg px-2.5 text-xs font-semibold text-zinc-500 hover:bg-zinc-50 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white"
                        >
                          {t("detail.cancel")}
                        </button>
                        <button
                          type="submit"
                          disabled={createUserTemplateGroupMutation.isPending}
                          className="inline-flex h-8 items-center rounded-lg bg-zinc-950 px-3 text-xs font-semibold text-white hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400"
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
            <>
            <div data-canvas-control className="group/sidebar-expand absolute right-0 top-0 z-30 flex h-full w-8 items-center justify-center">
              <button
                type="button"
                onClick={() => setSidebarCollapsed(false)}
                className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-zinc-200 bg-white/95 text-zinc-500 opacity-0 shadow-sm transition-opacity hover:text-zinc-900 focus:opacity-100 focus:outline-none group-hover/sidebar-expand:opacity-100 dark:border-slate-700/80 dark:bg-[#151f33]/95 dark:text-slate-300 dark:hover:text-white"
                aria-label={t("detail.expandSidebar")}
                title={t("detail.expandSidebar")}
              >
                <ChevronLeft size={14} />
              </button>
            </div>
            <div
              data-canvas-control
              className="absolute right-4 top-16 z-30 flex w-[72px] flex-col items-center gap-2 rounded-2xl border border-slate-700/80 bg-[#0f1726]/95 p-2 shadow-xl shadow-slate-950/25 backdrop-blur"
            >
              {renderWorkflowToolbarButtons()}
              <SidebarTabButton active={false} label={t("detail.tabSingleNode")} title={t("detail.tabSingleNode")} icon={<Plus size={17} />} onClick={() => openSidebarTab("singleNode")} />
              <SidebarTabButton active={false} label={t("detail.tabTemplates")} title={t("detail.tabTemplates")} icon={<Layers3 size={17} />} onClick={() => openSidebarTab("templates")} />
              {renderToolbarViewDivider()}
              <SidebarTabButton active={false} label={t("detail.tabDetails")} title={t("detail.tabDetails")} icon={<Eye size={17} />} onClick={() => openSidebarTab("details")} />
              <SidebarTabButton active={false} label={t("detail.tabRuns")} title={t("detail.runsTitle")} icon={<CircleDot size={17} />} onClick={() => openSidebarTab("runs")} />
              <SidebarTabButton active={false} label={t("detail.tabImages")} title={t("detail.tabImages")} icon={<ImageIcon size={17} />} onClick={() => openSidebarTab("images")} />
            </div>
            </>
          ) : (
          <div className="relative z-20 flex shrink-0 border-l border-slate-200 bg-white/95 shadow-[-8px_0_24px_-20px_rgba(15,23,42,0.35)] backdrop-blur dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[-16px_0_42px_rgba(0,0,0,0.28)]">
            <div data-canvas-control className="group/sidebar-collapse absolute left-[-28px] top-0 z-30 flex h-full w-7 items-center justify-center">
              <button
                type="button"
                onClick={() => setSidebarCollapsed(true)}
                className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-zinc-200 bg-white/95 text-zinc-500 opacity-0 shadow-sm transition-opacity hover:text-zinc-900 focus:opacity-100 focus:outline-none group-hover/sidebar-collapse:opacity-100 dark:border-slate-700/80 dark:bg-[#151f33]/95 dark:text-slate-300 dark:hover:text-white"
                aria-label={t("detail.collapseSidebar")}
                title={t("detail.collapseSidebar")}
              >
                <ChevronRight size={14} />
              </button>
            </div>
            <div data-canvas-control className="flex w-[72px] shrink-0 flex-col items-center gap-2 border-r border-slate-800 bg-slate-950 px-2 py-3 dark:border-slate-700/80 dark:bg-[#0b1220]">
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
            </div>
            <aside
              className="relative flex shrink-0 flex-col bg-white/95 dark:bg-[#111a2b]"
              style={{ width: inspectorWidth }}
            >
              <div
                role="separator"
                aria-label={t("detail.resizeSidebar")}
                onPointerDown={startInspectorResize}
                className="absolute left-[-4px] top-0 h-full w-2 cursor-col-resize hover:bg-zinc-300/50 dark:hover:bg-violet-400/20"
              />
              <div className="flex h-12 shrink-0 items-center justify-between border-b border-zinc-200 px-4 dark:border-slate-700/80">
                <div className="flex items-center">
                {activeSidebarTab === "singleNode" ? <Plus size={14} className="mr-2 text-zinc-400 dark:text-slate-400" /> : null}
                {activeSidebarTab === "templates" ? <Layers3 size={14} className="mr-2 text-zinc-400 dark:text-slate-400" /> : null}
                {activeSidebarTab === "details" ? <Settings2 size={14} className="mr-2 text-zinc-400 dark:text-slate-400" /> : null}
                {activeSidebarTab === "runs" ? <CircleDot size={14} className="mr-2 text-zinc-400 dark:text-slate-400" /> : null}
                {activeSidebarTab === "images" ? <ImageIcon size={14} className="mr-2 text-zinc-400 dark:text-slate-400" /> : null}
                <span className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500 dark:text-slate-300">
                  {activeSidebarTab === "singleNode"
                    ? t("detail.tabSingleNode")
                    : activeSidebarTab === "templates"
                      ? t("detail.tabTemplates")
                      : activeSidebarTab === "details"
                        ? t("detail.tabDetails")
                        : activeSidebarTab === "runs"
                          ? t("detail.tabRuns")
                          : t("detail.tabImages")}
                </span>
                </div>
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto p-4">
                {activeSidebarTab === "singleNode" ? renderSingleNodePanel() : null}
                {activeSidebarTab === "details" ? (
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
                    <div className="text-xs text-zinc-500 dark:text-slate-400">
                      {t("detail.selectNodeHint")}
                    </div>
                  )
                ) : null}
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
                        if (!window.confirm(t("detail.confirm.deleteTemplate", { title: template.title }))) {
                          return;
                        }
                        archiveUserTemplateGroupMutation.mutate(template.user_template_id);
                      }
                    }}
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
