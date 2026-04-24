import { useEffect, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CircleDot,
  Download,
  FileText,
  GitBranch,
  Image as ImageIcon,
  ImagePlus,
  Loader2,
  Play,
  Plus,
  Save,
  Settings2,
  Trash2,
  Upload,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { formatDateTime, formatPrice } from "../lib/format";
import type {
  PosterVariant,
  ProductDetail,
  ProductWorkflow,
  SourceAsset,
  WorkflowNode,
  WorkflowNodeType,
} from "../lib/types";

const NODE_WIDTH = 248;
const NODE_HANDLE_Y = 56;
const NODE_MIN_X = 24;
const NODE_MIN_Y = 24;

type CanvasPoint = {
  x: number;
  y: number;
};

type NodeDragState = {
  nodeId: string;
  pointerId: number;
  offsetX: number;
  offsetY: number;
  currentX: number;
  currentY: number;
};

type ConnectionDragState = {
  sourceNodeId: string;
  pointerId: number;
  from: CanvasPoint;
  to: CanvasPoint;
};

type NodeConfigDraft = {
  title: string;
  productName: string;
  category: string;
  price: string;
  sourceNote: string;
  instruction: string;
  role: string;
  label: string;
  tone: string;
  channel: string;
  size: string;
  copyTitle: string;
  copySellingPoints: string;
  copyPosterHeadline: string;
  copyCta: string;
};

const NODE_LABELS: Record<WorkflowNodeType, string> = {
  product_context: "商品",
  reference_image: "参考图",
  copy_generation: "文案",
  image_generation: "生图",
};

const NODE_STATUS_LABELS: Record<WorkflowNode["status"], string> = {
  idle: "未运行",
  queued: "排队中",
  running: "运行中",
  succeeded: "成功",
  failed: "失败",
};

const ADD_NODE_OPTIONS: Array<{ type: WorkflowNodeType; label: string }> = [
  { type: "product_context", label: "商品" },
  { type: "reference_image", label: "参考图" },
  { type: "copy_generation", label: "文案" },
  { type: "image_generation", label: "生图" },
];

type DownloadableImage = {
  previewUrl: string;
  downloadUrl: string;
  filename: string;
  alt: string;
};

function getSourceImageAsset(product: ProductDetail): SourceAsset | null {
  return (
    product.source_assets.find((asset) => asset.kind === "original_image") ??
    null
  );
}

function sanitizeFilenamePart(
  value: string | null | undefined,
  fallback: string,
): string {
  const cleaned = (value ?? fallback)
    .trim()
    .replace(/[\u0000-\u001f\u007f]+/g, "")
    .replace(/[\\/:*?"<>|]+/g, "-")
    .replace(/\s+/g, "-")
    .replace(/\.+/g, ".")
    .replace(/^-+|-+$/g, "");
  return (cleaned && cleaned !== "." && cleaned !== ".." ? cleaned : fallback)
    .slice(0, 80)
    .replace(/\.+$/g, "");
}

function toImageUrl(...paths: Array<string | null | undefined>): string {
  const path = paths.find((item) => typeof item === "string" && item.trim());
  return api.toApiUrl(path ?? "/");
}

function getExtensionFromMime(mimeType: string | null | undefined): string {
  if (mimeType === "image/jpeg") {
    return ".jpg";
  }
  if (mimeType === "image/webp") {
    return ".webp";
  }
  return ".png";
}

function getExtensionFromFilename(
  filename: string | null | undefined,
  mimeType?: string | null,
): string {
  const match = filename?.match(/\.[a-z0-9]+$/i);
  return match ? match[0].toLowerCase() : getExtensionFromMime(mimeType);
}

function compactDateTime(value: string): string {
  return value.replace(/[^0-9]/g, "").slice(0, 14) || "image";
}

function buildSourceImageDownload(
  product: ProductDetail,
  asset: SourceAsset,
  label: string,
  previewUrl?: string,
): DownloadableImage {
  const productName = sanitizeFilenamePart(product.name, "商品");
  const imageLabel = sanitizeFilenamePart(label, "图片");
  const extension = getExtensionFromFilename(
    asset.original_filename,
    asset.mime_type,
  );
  return {
    previewUrl: toImageUrl(previewUrl, asset.preview_url, asset.download_url),
    downloadUrl: toImageUrl(asset.download_url, asset.preview_url),
    filename: `${productName}-${imageLabel}-${compactDateTime(asset.created_at)}${extension}`,
    alt: `${product.name} ${label}`,
  };
}

function buildPosterDownload(
  productName: string,
  poster: PosterVariant,
  previewUrl?: string,
): DownloadableImage {
  const productLabel = sanitizeFilenamePart(productName, "商品");
  const posterLabel = poster.kind === "main_image" ? "主图" : "海报";
  const extension = getExtensionFromMime(poster.mime_type);
  return {
    previewUrl: toImageUrl(previewUrl, poster.preview_url, poster.download_url),
    downloadUrl: toImageUrl(poster.download_url, poster.preview_url),
    filename: `${productLabel}-${posterLabel}-${compactDateTime(poster.created_at)}${extension}`,
    alt: `${productName} ${posterLabel}`,
  };
}

function getSourceImageDownload(
  product: ProductDetail,
): DownloadableImage | null {
  const sourceAsset = getSourceImageAsset(product);
  return sourceAsset
    ? buildSourceImageDownload(product, sourceAsset, "主图")
    : null;
}

function outputStringArray(node: WorkflowNode, key: string): string[] {
  const value = node.output_json?.[key] ?? node.config_json[key];
  if (typeof value === "string") {
    return [value];
  }
  if (Array.isArray(value)) {
    return value.filter((item): item is string => typeof item === "string");
  }
  return [];
}

function getNodeImageDownload(
  node: WorkflowNode,
  product: ProductDetail,
  posters: PosterVariant[],
): DownloadableImage | null {
  if (node.node_type === "product_context") {
    return getSourceImageDownload(product);
  }
  if (node.node_type === "reference_image") {
    const ids = outputStringArray(node, "source_asset_ids");
    const asset = ids
      .map((id) =>
        product.source_assets.find((item: SourceAsset) => item.id === id),
      )
      .find((item): item is SourceAsset => Boolean(item));
    return asset
      ? buildSourceImageDownload(
          product,
          asset,
          node.title || "参考图",
          asset.thumbnail_url,
        )
      : null;
  }
  if (node.node_type === "image_generation") {
    const ids = outputStringArray(node, "poster_variant_ids");
    const poster = ids
      .map((id) => posters.find((item) => item.id === id))
      .find((item): item is PosterVariant => Boolean(item));
    return poster
      ? buildPosterDownload(product.name, poster, poster.thumbnail_url)
      : null;
  }
  return null;
}

function configString(
  node: WorkflowNode | null,
  key: string,
  fallback = "",
): string {
  const value = node?.config_json[key];
  return typeof value === "string" ? value : fallback;
}

function draftFromNode(
  node: WorkflowNode | null,
  product?: ProductDetail | null,
): NodeConfigDraft {
  const copySetId = node?.output_json
    ? outputText(node.output_json, "copy_set_id")
    : null;
  const copySet = copySetId
    ? product?.copy_sets.find((item) => item.id === copySetId)
    : null;
  const outputSellingPoints = node
    ? outputStringArray(node, "selling_points")
    : [];
  return {
    title: node?.title ?? "",
    productName: configString(node, "name", product?.name ?? ""),
    category: configString(node, "category", product?.category ?? ""),
    price: configString(node, "price", product?.price ?? ""),
    sourceNote: configString(node, "source_note", product?.source_note ?? ""),
    instruction: configString(node, "instruction"),
    role: configString(node, "role", "reference"),
    label: configString(node, "label"),
    tone: configString(node, "tone", "转化清晰"),
    channel: configString(node, "channel", "商品主图"),
    size: configString(node, "size", "1024x1024"),
    copyTitle:
      copySet?.title ??
      (node?.output_json ? (outputText(node.output_json, "title") ?? "") : ""),
    copySellingPoints: (copySet?.selling_points ?? outputSellingPoints).join(
      "\n",
    ),
    copyPosterHeadline:
      copySet?.poster_headline ??
      (node?.output_json
        ? (outputText(node.output_json, "poster_headline") ?? "")
        : ""),
    copyCta:
      copySet?.cta ??
      (node?.output_json ? (outputText(node.output_json, "cta") ?? "") : ""),
  };
}

function nodeConfigFromDraft(
  node: WorkflowNode,
  draft: NodeConfigDraft,
): Record<string, unknown> {
  const base = { ...node.config_json };
  if (node.node_type === "product_context") {
    return {
      ...base,
      name: draft.productName,
      category: draft.category,
      price: draft.price,
      source_note: draft.sourceNote,
    };
  }
  if (node.node_type === "reference_image") {
    return { ...base, role: draft.role, label: draft.label };
  }
  if (node.node_type === "copy_generation") {
    return {
      ...base,
      instruction: draft.instruction,
      tone: draft.tone,
      channel: draft.channel,
    };
  }
  if (node.node_type === "image_generation") {
    return {
      ...base,
      instruction: draft.instruction,
      size: draft.size,
    };
  }
  return base;
}

function defaultConfigForType(type: WorkflowNodeType): Record<string, unknown> {
  if (type === "reference_image") {
    return { role: "reference", label: "参考图" };
  }
  if (type === "copy_generation") {
    return { instruction: "生成商品文案", tone: "清晰可信", channel: "商品图" };
  }
  if (type === "image_generation") {
    return {
      instruction: "生成商品图",
      size: "1024x1024",
    };
  }
  return {};
}

function defaultTitleForType(type: WorkflowNodeType, index: number): string {
  return {
    product_context: `商品 ${index}`,
    reference_image: `参考图 ${index}`,
    copy_generation: `文案 ${index}`,
    image_generation: `生图 ${index}`,
  }[type];
}

function statusClass(status: WorkflowNode["status"]): string {
  return {
    idle: "border-zinc-200 bg-white text-zinc-500",
    queued: "border-amber-200 bg-amber-50 text-amber-700",
    running: "border-blue-200 bg-blue-50 text-blue-700",
    succeeded: "border-emerald-200 bg-emerald-50 text-emerald-700",
    failed: "border-red-200 bg-red-50 text-red-700",
  }[status];
}

function nodeIdleSummary(node: WorkflowNode): string {
  if (node.node_type === "product_context") {
    return "商品资料";
  }
  if (node.node_type === "reference_image") {
    return "参考图";
  }
  if (node.node_type === "copy_generation") {
    return "文案";
  }
  return "连接参考图";
}

function hasActiveWorkflow(workflow: ProductWorkflow | undefined | null): boolean {
  if (!workflow) {
    return false;
  }
  return (
    workflow.runs.some((run) => run.status === "running") ||
    workflow.nodes.some((node) => node.status === "queued" || node.status === "running")
  );
}

export function ProductDetailPage() {
  const { productId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canvasRef = useRef<HTMLDivElement | null>(null);
  const nodeDragRafRef = useRef<number | null>(null);
  const pendingNodeDragRef = useRef<CanvasPoint | null>(null);
  const wasWorkflowActiveRef = useRef(false);
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
    setDraft(draftFromNode(selectedNode, productQuery.data));
  }, [
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
    };
  }, []);

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
      x: clientX - rect.left,
      y: clientY - rect.top,
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
      setOptimisticNodePositions((current) => ({
        ...current,
        [input.node.id]: { x: input.position_x, y: input.position_y },
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
      return { previous };
    },
    onSuccess: (nextWorkflow) => {
      setError("");
      queryClient.setQueryData(["product-workflow", productId], nextWorkflow);
      setOptimisticNodePositions((current) => {
        const next = { ...current };
        for (const node of nextWorkflow.nodes) {
          delete next[node.id];
        }
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

  const layoutMutationBusy =
    createNodeMutation.isPending ||
    updateNodeConfigMutation.isPending ||
    updateNodePositionMutation.isPending ||
    createEdgeMutation.isPending ||
    deleteEdgeMutation.isPending ||
    deleteNodeMutation.isPending ||
    uploadNodeImageMutation.isPending ||
    updateNodeCopyMutation.isPending;
  const structureBusy = layoutMutationBusy || workflowActive;
  const runBusy = runWorkflowMutation.isPending || workflowActive;
  const dragBusy = layoutMutationBusy;

  const startNodeDrag = (
    node: WorkflowNode,
    event: ReactPointerEvent<HTMLDivElement>,
  ) => {
    if (dragBusy || event.button !== 0) {
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
    const point = getCanvasPoint(event.clientX, event.clientY);
    setSelectedNodeId(node.id);
    setNodeDrag({
      nodeId: node.id,
      pointerId: event.pointerId,
      offsetX: point.x - node.position_x,
      offsetY: point.y - node.position_y,
      currentX: node.position_x,
      currentY: node.position_y,
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

  const endNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!nodeDrag || nodeDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
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
                节点 · {workflow?.nodes.length ?? 0}
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
              onClick={() => runWorkflowMutation.mutate(undefined)}
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
          <section className="relative z-10 min-w-0 flex-1 overflow-auto p-6">
            {workflowQuery.isLoading ? (
              <div className="flex h-full items-center justify-center text-zinc-400">
                <Loader2 size={24} className="animate-spin" />
              </div>
            ) : workflow ? (
              <div
                ref={canvasRef}
                className="relative"
                style={{ width: canvasWidth, height: canvasHeight }}
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
                    onSelect={() => setSelectedNodeId(node.id)}
                    onStartDrag={(event) => startNodeDrag(node, event)}
                    onMoveDrag={moveNodeDrag}
                    onEndDrag={endNodeDrag}
                    onStartConnection={(event) =>
                      startConnectionDrag(node, event)
                    }
                    onMoveConnection={moveConnectionDrag}
                    onEndConnection={endConnectionDrag}
                    onRun={() => runWorkflowMutation.mutate(node.id)}
                    onDelete={() => handleDeleteNode(node)}
                    busy={structureBusy}
                    runBusy={runBusy}
                  />
                ))}
              </div>
            ) : (
              <div className="flex h-full items-center justify-center text-xs text-zinc-500">
                工作流加载失败
              </div>
            )}
          </section>

          <aside className="relative z-20 flex w-[360px] shrink-0 flex-col border-l border-zinc-200 bg-white/95 shadow-[-8px_0_24px_-20px_rgba(0,0,0,0.35)] backdrop-blur">
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
                  onDraftChange={setDraft}
                  onSave={() => updateNodeConfigMutation.mutate(selectedNode)}
                  onSaveCopy={() => updateNodeCopyMutation.mutate(selectedNode)}
                  onRun={() => runWorkflowMutation.mutate(selectedNode.id)}
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

        <div className="z-20 grid max-h-56 shrink-0 grid-cols-[1fr_420px] border-t border-zinc-200 bg-white">
          <section className="min-w-0 overflow-y-auto p-4">
            <div className="mb-3 flex items-center justify-between">
              <div className="flex items-center text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
                <CircleDot size={13} className="mr-2" /> 运行记录
              </div>
              {latestRun ? (
                <div className="text-[11px] text-zinc-400">
                  最近：{formatDateTime(latestRun.started_at)}
                </div>
              ) : null}
            </div>
            {workflow?.runs.length ? (
              <div className="grid grid-cols-2 gap-2">
                {workflow.runs.map((run) => (
                  <div
                    key={run.id}
                    className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-xs"
                  >
                    <div className="flex items-center justify-between">
                      <span className="font-medium text-zinc-700">
                        {run.status === "succeeded"
                          ? "成功"
                          : run.status === "failed"
                            ? "失败"
                            : "运行中"}
                      </span>
                      <span className="text-[10px] text-zinc-400">
                        {formatDateTime(run.started_at)}
                      </span>
                    </div>
                    <div className="mt-1 text-zinc-500">
                      节点记录 {run.node_runs.length} 条
                    </div>
                    {run.failure_reason ? (
                      <div className="mt-1 text-red-600">
                        {run.failure_reason}
                      </div>
                    ) : null}
                  </div>
                ))}
              </div>
            ) : (
              <div className="rounded-md border border-dashed border-zinc-200 px-4 py-6 text-center text-xs text-zinc-500">
                暂无记录
              </div>
            )}
          </section>

          <section className="overflow-y-auto border-l border-zinc-200 p-4">
            <div className="mb-3 flex items-center text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
              <ImageIcon size={13} className="mr-2" /> 图片
            </div>
            {posters.length ? (
              <div className="grid grid-cols-3 gap-2">
                {posters.slice(0, 9).map((poster) => (
                  <PosterThumb
                    key={poster.id}
                    poster={poster}
                    productName={product.name}
                  />
                ))}
              </div>
            ) : (
              <div className="rounded-md border border-dashed border-zinc-200 px-3 py-6 text-center text-xs text-zinc-500">
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
  onSelect,
  onStartDrag,
  onMoveDrag,
  onEndDrag,
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
  onSelect: () => void;
  onStartDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onMoveDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onEndDrag: (event: ReactPointerEvent<HTMLDivElement>) => void;
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
      className={`absolute w-[248px] touch-none rounded-xl border bg-white p-3 text-left shadow-sm transition-shadow hover:shadow-md ${
        selected ? "border-zinc-900 ring-2 ring-zinc-900/10" : "border-zinc-200"
      }`}
      style={{
        left: 0,
        top: 0,
        transform: `translate3d(${position.x}px, ${position.y}px, 0)`,
      }}
      onPointerDown={onStartDrag}
      onPointerMove={onMoveDrag}
      onPointerUp={onEndDrag}
      onPointerCancel={onEndDrag}
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
            <span className="mt-0.5 rounded-md border border-zinc-200 bg-zinc-50 p-1.5 text-zinc-500">
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
          <div className="relative mb-2 h-24 overflow-hidden rounded-md border border-zinc-100 bg-zinc-100">
            <img
              src={image.previewUrl}
              alt={image.alt}
              className="h-full w-full object-cover"
            />
            <DownloadLink image={image} variant="overlay" />
          </div>
        ) : null}
        <div className="line-clamp-2 min-h-[32px] text-xs leading-relaxed text-zinc-500">
          {summary}
        </div>
      </div>
      <div className="mt-3 flex items-center justify-between text-[10px] text-zinc-400">
        <span className="font-mono">
          {position.x}, {position.y}
        </span>
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
}) {
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

  return (
    <div className="space-y-5">
      <div>
        <div className="mb-1 text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          节点类型
        </div>
        <div className="text-sm font-semibold text-zinc-900">
          {NODE_LABELS[node.node_type]}
        </div>
        <div className="mt-1 text-xs text-zinc-500">
          状态：{NODE_STATUS_LABELS[node.status]}
        </div>
      </div>

      <div>
        <label className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          节点名称
        </label>
        <input
          value={draft.title}
          onChange={(event) =>
            onDraftChange({ ...draft, title: event.target.value })
          }
          className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
        />
      </div>

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
      <div className="rounded-md border border-zinc-200 bg-zinc-50 p-3">
        <div className="mb-2 text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
          连接
        </div>
        <div className="text-xs leading-relaxed text-zinc-500">
          上游 {incomingEdges.length} · 拖拽连接
        </div>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <button
          type="button"
          onClick={onSave}
          disabled={busy}
          className="inline-flex items-center justify-center rounded-md border border-zinc-300 bg-white px-3 py-2 text-xs font-medium text-zinc-700 hover:bg-zinc-50 disabled:opacity-50"
        >
          <Save size={13} className="mr-1.5" /> 保存
        </button>
        <button
          type="button"
          onClick={onRun}
          disabled={runBusy}
          className="inline-flex items-center justify-center rounded-md bg-zinc-900 px-3 py-2 text-xs font-medium text-white hover:bg-zinc-800 disabled:opacity-50"
        >
          {runBusy ? (
            <Loader2 size={13} className="mr-1.5 animate-spin" />
          ) : (
            <Play size={13} className="mr-1.5" />
          )}
          运行
        </button>
      </div>
      <button
        type="button"
        onClick={onDelete}
        disabled={busy}
        className="inline-flex w-full items-center justify-center rounded-md border border-red-200 bg-white px-3 py-2 text-xs font-medium text-red-600 hover:bg-red-50 disabled:opacity-50"
      >
        <Trash2 size={13} className="mr-1.5" /> 删除节点
      </button>

      {node.failure_reason ? (
        <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
          {node.failure_reason}
        </div>
      ) : null}
      {node.output_json ? (
        <NodeOutputPreview output={node.output_json} />
      ) : null}
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
      <div className="relative aspect-video overflow-hidden rounded-md border border-zinc-200 bg-zinc-100">
        {sourceImage ? (
          <>
            <img
              src={sourceImage.previewUrl}
              alt={sourceImage.alt}
              className="h-full w-full object-cover"
            />
            <DownloadLink image={sourceImage} variant="overlay" />
          </>
        ) : null}
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
}: {
  draft: NodeConfigDraft;
  onDraftChange: (draft: NodeConfigDraft) => void;
  onUploadImage: (file: File) => void;
  busy: boolean;
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
      <label className="flex cursor-pointer items-center justify-center rounded-md border border-dashed border-zinc-300 px-3 py-6 text-xs font-medium text-zinc-600 hover:bg-zinc-50">
        <Upload size={14} className="mr-2" /> 上传
        <input
          type="file"
          accept="image/png,image/jpeg,image/webp"
          className="hidden"
          disabled={busy}
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) {
              onUploadImage(file);
            }
            event.currentTarget.value = "";
          }}
        />
      </label>
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
        上游 {incomingCount} · 参考图 {downstreamReferenceCount || "未连接"}
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

function outputCount(output: Record<string, unknown>, key: string): number {
  const value = output[key];
  if (Array.isArray(value)) {
    return value.filter((item) => typeof item === "string" && item.length > 0)
      .length;
  }
  return typeof value === "string" && value.length > 0 ? 1 : 0;
}

function outputText(
  output: Record<string, unknown>,
  key: string,
): string | null {
  const value = output[key];
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function NodeOutputPreview({ output }: { output: Record<string, unknown> }) {
  const copyReady = Boolean(outputText(output, "copy_set_id"));
  const posterCount = outputCount(output, "poster_variant_ids");
  const filledCount = outputCount(output, "filled_source_asset_ids");
  const imageCount = Math.max(
    outputCount(output, "source_asset_ids"),
    outputCount(output, "image_asset_ids"),
  );
  const targetCount =
    typeof output.target_count === "number" ? output.target_count : null;
  const size = outputText(output, "size");
  const facts = [
    copyReady ? "文案 已生成" : null,
    posterCount ? `图片 ${posterCount}` : null,
    filledCount
      ? `参考图 ${filledCount}`
      : imageCount
        ? `参考图 ${imageCount}`
        : null,
    targetCount ? `槽位 ${targetCount}` : null,
    size,
  ].filter((item): item is string => Boolean(item));

  return (
    <div className="rounded-md border border-zinc-200 bg-zinc-50 p-3">
      <div className="mb-2 text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
        输出
      </div>
      {typeof output.summary === "string" ? (
        <div className="mb-2 text-xs text-zinc-700">{output.summary}</div>
      ) : null}
      {facts.length ? (
        <div className="flex flex-wrap gap-1.5">
          {facts.map((item) => (
            <span
              key={item}
              className="rounded-full border border-zinc-200 bg-white px-2 py-0.5 text-[10px] text-zinc-500"
            >
              {item}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function DownloadLink({
  image,
  variant = "button",
}: {
  image: DownloadableImage;
  variant?: "button" | "overlay";
}) {
  const className =
    variant === "overlay"
      ? "absolute bottom-2 right-2 inline-flex items-center rounded bg-white/95 px-2 py-1 text-[10px] font-medium text-zinc-700 shadow-sm ring-1 ring-zinc-200 hover:bg-white"
      : "inline-flex items-center rounded border border-zinc-200 bg-white px-2 py-1 text-[10px] font-medium text-zinc-600 hover:border-zinc-300 hover:bg-zinc-50";
  return (
    <a
      data-node-action
      href={image.downloadUrl}
      download={image.filename}
      onClick={(event) => event.stopPropagation()}
      target="_blank"
      rel="noreferrer"
      className={className}
      title={`下载 ${image.filename}`}
      aria-label={`下载 ${image.filename}`}
    >
      <Download size={11} className="mr-1" /> 下载
    </a>
  );
}

function PosterThumb({
  poster,
  productName,
}: {
  poster: PosterVariant;
  productName: string;
}) {
  const image = buildPosterDownload(productName, poster, poster.thumbnail_url);
  return (
    <div className="group overflow-hidden rounded-md border border-zinc-200 bg-white">
      <a
        href={api.toApiUrl(poster.preview_url)}
        target="_blank"
        rel="noreferrer"
        className="block"
      >
        <div className="aspect-square bg-zinc-100">
          <img
            src={image.previewUrl}
            alt={image.alt}
            className="h-full w-full object-cover transition-transform group-hover:scale-[1.02]"
          />
        </div>
      </a>
      <div className="flex items-center justify-between gap-2 border-t border-zinc-100 px-2 py-1 text-[10px] text-zinc-500">
        <span className="min-w-0 truncate">
          {poster.kind === "main_image" ? "主图" : "促销"} ·{" "}
          {formatDateTime(poster.created_at)}
        </span>
        <DownloadLink image={image} />
      </div>
    </div>
  );
}
