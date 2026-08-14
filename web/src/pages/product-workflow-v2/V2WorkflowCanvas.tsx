import {
  Background,
  BackgroundVariant,
  ControlButton,
  Controls,
  Handle,
  MiniMap,
  Position,
  ReactFlow,
  SelectionMode,
  useReactFlow,
  useNodesState,
  useViewport,
} from "@xyflow/react";
import type {
  Edge,
  Node,
  NodeMouseHandler,
  NodeProps,
  OnMoveEnd,
  OnNodeDrag,
  OnSelectionChangeFunc,
  Viewport,
} from "@xyflow/react";
import {
  Box,
  Focus,
  FolderOpen,
  Grid,
  Images,
  Link2,
  Loader2,
  Play,
  Sparkles,
} from "lucide-react";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { api } from "../../lib/api";
import { sanitizeFilenamePart, type DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  ProductWorkflowV2,
  WorkflowNodeStatus,
  WorkflowNodeTypeV2,
  WorkflowNodeV2,
} from "../../lib/types";
import { WorkflowNodePresentationCard } from "../product-detail/WorkflowNodeCard";
import {
  isWorkflowCanvasViewportCompatible,
  workflowCanvasFitMinZoom,
  type WorkflowCanvasViewport,
} from "./canvasState";
import {
  buildLocalFolderGraph,
  buildAutoLayoutNodePositions,
  deriveFolderSummary,
  folderSyntheticNodeId,
  projectGlobalGraph,
  V2_NODE_HEIGHT,
  V2_NODE_WIDTH,
  type GlobalFolderNode,
} from "./graph";

const FOLDER_CARD_WIDTH = 340;
const FOLDER_CARD_HEIGHT = 210;
const V2_SNAP_GRID: [number, number] = [24, 24];
const V2_MULTI_SELECTION_KEYS = ["Control", "Meta", "Shift"];
const V2_PAN_BUTTONS = [0, 1];
const V2_PRO_OPTIONS = { hideAttribution: true };
const V2_CONTROL_FIT_VIEW_OPTIONS = { padding: 0.22, duration: 180, maxZoom: 1.05 };

interface WorkflowNodeData extends Record<string, unknown> {
  kind: "node";
  node: WorkflowNodeV2;
  revealActive: boolean;
  runBusy: boolean;
  structureBusy: boolean;
  onRun: (node: WorkflowNodeV2) => void;
  onBindReference: (node: WorkflowNodeV2) => void;
  inputHandleIds: string[];
  outputHandleIds: string[];
}

interface WorkflowFolderData extends Record<string, unknown> {
  kind: "folder";
  projection: GlobalFolderNode;
  revealActive: boolean;
  structureBusy: boolean;
  onOpen: (folderId: string) => void;
}

type WorkflowCanvasNode =
  | Node<WorkflowNodeData, "workflow-node-v2">
  | Node<WorkflowFolderData, "workflow-folder-v2">;

type WorkflowCanvasEdge = Edge<{
  projected: boolean;
  originalEdgeIds: string[];
  count: number;
}>;

export interface WorkflowRevealVisibility {
  folderIds: ReadonlySet<string>;
  nodeIds: ReadonlySet<string>;
  edgeIds: ReadonlySet<string>;
}

interface V2WorkflowCanvasProps {
  workflow: ProductWorkflowV2;
  revealVisibility?: WorkflowRevealVisibility;
  openFolderId: string | null;
  viewport: WorkflowCanvasViewport | null;
  structureBusy: boolean;
  runningNodeId: string | null;
  onOpenFolder: (folderId: string) => void;
  onRunNode: (node: WorkflowNodeV2) => void;
  onBindReference: (node: WorkflowNodeV2) => void;
  onSelectionChange: (nodeIds: string[]) => void;
  onLayoutCommit: (positions: Array<{ node_id: string; position_x: number; position_y: number }>) => void;
  onFolderTranslate: (folderId: string, deltaX: number, deltaY: number) => void;
  onViewportChange: (viewport: WorkflowCanvasViewport) => void;
}

const statusClasses: Record<WorkflowNodeStatus, string> = {
  idle: "bg-slate-300 dark:bg-slate-600",
  queued: "bg-amber-400",
  running: "bg-blue-500",
  succeeded: "bg-emerald-500",
  failed: "bg-red-500",
  cancelled: "bg-slate-400",
};

function nodeTypeLabel(type: WorkflowNodeTypeV2, t: ReturnType<typeof useI18n>["t"]): string {
  const keys = {
    product_context: "workflowV2.node.productContext",
    reference_image: "workflowV2.node.referenceImage",
    prompt_generation: "workflowV2.node.promptGeneration",
    image_generation: "workflowV2.node.imageGeneration",
  } as const;
  return t(keys[type]);
}

function nodeStatusLabel(status: WorkflowNodeStatus, t: ReturnType<typeof useI18n>["t"]): string {
  const keys = {
    idle: "detail.nodeStatus.idle",
    queued: "detail.nodeStatus.queued",
    running: "detail.nodeStatus.running",
    succeeded: "detail.nodeStatus.succeeded",
    failed: "detail.nodeStatus.failed",
    cancelled: "detail.nodeStatus.cancelled",
  } as const;
  return t(keys[status]);
}

function nodeAssetId(node: WorkflowNodeV2): string | null {
  if (node.bound_image_asset_id) {
    return node.bound_image_asset_id;
  }
  const resultAssetId = node.output_json?.result_asset_id;
  return typeof resultAssetId === "string" && resultAssetId ? resultAssetId : null;
}

function assetThumbnailUrl(assetId: string): string {
  return api.toApiUrl(`/api/v2/product-image-assets/${assetId}/download?variant=thumbnail`);
}

function nodeImage(node: WorkflowNodeV2): DownloadableImage | null {
  const assetId = nodeAssetId(node);
  if (!assetId) {
    return null;
  }
  return {
    previewUrl: assetThumbnailUrl(assetId),
    downloadUrl: api.toApiUrl(`/api/v2/product-image-assets/${assetId}/download`),
    filename: `${sanitizeFilenamePart(node.title, node.key || "workflow-image")}.png`,
    alt: node.title,
  };
}

const WorkflowNodeCard = memo(function WorkflowNodeCard({ data, selected, dragging }: NodeProps<Node<WorkflowNodeData>>) {
  const { t } = useI18n();
  const { node } = data;
  const runnable = node.node_type === "prompt_generation" || node.node_type === "image_generation";
  const active = node.status === "queued" || node.status === "running";

  return (
    <div className="relative w-[248px]">
      {data.inputHandleIds.map((handleId, index) => (
        <Handle
          key={`target:${handleId}`}
          id={handleId}
          type="target"
          position={Position.Left}
          style={{ top: `${((index + 1) / (data.inputHandleIds.length + 1)) * 100}%` }}
          className="!h-2.5 !w-2.5 !border-2 !border-white !bg-slate-400 dark:!border-slate-900"
        />
      ))}
      {data.outputHandleIds.map((handleId, index) => (
        <Handle
          key={`source:${handleId}`}
          id={handleId}
          type="source"
          position={Position.Right}
          style={{ top: `${((index + 1) / (data.outputHandleIds.length + 1)) * 100}%` }}
          className="!h-2.5 !w-2.5 !border-2 !border-white !bg-indigo-500 dark:!border-slate-900"
        />
      ))}
      <WorkflowNodePresentationCard
        id={node.id}
        kind={node.node_type}
        title={node.title}
        label={nodeTypeLabel(node.node_type, t)}
        status={node.status}
        statusLabel={nodeStatusLabel(node.status, t)}
        image={nodeImage(node)}
        imageWaiting={node.node_type === "image_generation" && active}
        waitingLabel={nodeStatusLabel(node.status, t)}
        activityText={active ? nodeStatusLabel(node.status, t) : null}
        failureReason={node.failure_reason}
        primarySelected={selected}
        dragging={dragging}
        revealActive={data.revealActive}
        onSelect={() => undefined}
        actions={(
          <>
            {node.node_type === "reference_image" ? (
              <button
                type="button"
                className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-600 transition-colors hover:border-indigo-300 hover:text-indigo-700 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:border-violet-400 dark:hover:text-violet-200"
                onClick={(event) => {
                  event.stopPropagation();
                  data.onBindReference(node);
                }}
                disabled={data.structureBusy}
                aria-label={t("workflowV2.node.bindReference")}
                title={t("workflowV2.node.bindReference")}
              >
                <Link2 size={14} />
              </button>
            ) : null}
            {runnable ? (
              <button
                type="button"
                className="inline-flex h-8 w-8 items-center justify-center rounded-lg bg-slate-950 text-white transition-colors hover:bg-indigo-700 disabled:opacity-40 dark:bg-violet-500 dark:hover:bg-violet-400"
                onClick={(event) => {
                  event.stopPropagation();
                  data.onRun(node);
                }}
                disabled={data.runBusy || active || data.structureBusy}
                aria-label={t("workflowV2.node.run")}
                title={t("workflowV2.node.run")}
              >
                {data.runBusy || active ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : (
                  <Play size={14} fill="currentColor" />
                )}
              </button>
            ) : null}
          </>
        )}
      />
    </div>
  );
});

const WorkflowFolderCard = memo(function WorkflowFolderCard({ data, selected }: NodeProps<Node<WorkflowFolderData>>) {
  const { t } = useI18n();
  const { projection } = data;
  const { folder, summary } = projection;

  return (
    <div
      className={`relative h-[210px] w-[340px] overflow-hidden rounded-lg border bg-white shadow-md transition-[border-color,box-shadow] dark:!bg-[#10151c] ${data.revealActive ? "animate-spring-pop-in" : ""} ${
        selected
          ? "border-indigo-500 shadow-[0_0_0_3px_rgba(99,102,241,0.16)] dark:border-violet-400"
          : "border-slate-300 dark:border-slate-700"
      }`}
      data-workflow-folder-id={folder.id}
    >
      <Handle type="target" position={Position.Left} className="!h-3 !w-3 !border-2 !border-white !bg-slate-500 dark:!border-slate-900" />
      <Handle type="source" position={Position.Right} className="!h-3 !w-3 !border-2 !border-white !bg-indigo-500 dark:!border-slate-900" />

      <div className="flex h-14 items-center gap-3 border-b border-slate-100 px-4 dark:border-slate-800">
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-indigo-50 text-indigo-700 dark:bg-violet-500/15 dark:text-violet-200">
          <Box size={17} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-slate-950 dark:text-slate-100" title={folder.title}>{folder.title}</div>
          <div className="mt-0.5 flex items-center gap-2 text-[11px] text-slate-500 dark:text-slate-400">
            <span>{t("workflowV2.folder.memberCount", { count: summary.member_count })}</span>
            <span className={`h-1.5 w-1.5 rounded-full ${statusClasses[summary.status]}`} />
            <span>{nodeStatusLabel(summary.status, t)}</span>
          </div>
        </div>
        <button
          type="button"
          className="nodrag nopan inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-slate-200 text-slate-600 hover:border-indigo-300 hover:text-indigo-700 disabled:opacity-40 dark:border-slate-700 dark:text-slate-300 dark:hover:border-violet-400 dark:hover:text-violet-200"
          onClick={(event) => {
            event.stopPropagation();
            data.onOpen(folder.id);
          }}
          disabled={data.structureBusy}
          aria-label={t("workflowV2.folder.open")}
          title={t("workflowV2.folder.open")}
        >
          <FolderOpen size={15} />
        </button>
      </div>

      <div className="grid h-[154px] grid-cols-[1fr_126px] gap-3 p-4">
        <div className="min-w-0">
          <div className="flex flex-wrap gap-1.5">
            {summary.node_types.map((type) => (
              <span key={type} className="rounded bg-slate-100 px-2 py-1 text-[10px] font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                {nodeTypeLabel(type, t)}
              </span>
            ))}
          </div>
          <div className="mt-4 grid grid-cols-2 gap-2 text-[10px] text-slate-500 dark:text-slate-400">
            <span className="flex items-center gap-1"><Link2 size={11} /> {t("workflowV2.folder.inbound", { count: summary.inbound_edge_count })}</span>
            <span className="flex items-center gap-1"><Link2 size={11} /> {t("workflowV2.folder.outbound", { count: summary.outbound_edge_count })}</span>
          </div>
        </div>
        <div className="grid h-[92px] grid-cols-3 gap-1 overflow-hidden rounded-md bg-slate-100 p-1 dark:bg-slate-950">
          {summary.preview_asset_ids.length ? summary.preview_asset_ids.map((assetId) => (
            <img
              key={assetId}
              src={assetThumbnailUrl(assetId)}
              alt=""
              className="h-full min-w-0 object-cover"
              loading="lazy"
              draggable={false}
            />
          )) : (
            <div className="col-span-3 flex items-center justify-center text-slate-400 dark:text-slate-600">
              <Images size={20} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
});

const nodeTypes = {
  "workflow-node-v2": WorkflowNodeCard,
  "workflow-folder-v2": WorkflowFolderCard,
};

function toCanvasNodes(
  workflow: ProductWorkflowV2,
  openFolderId: string | null,
  options: Pick<V2WorkflowCanvasProps, "structureBusy" | "runningNodeId" | "onOpenFolder" | "onRunNode" | "onBindReference" | "revealVisibility">,
): WorkflowCanvasNode[] {
  const revealVisibility = options.revealVisibility;
  const handleIds = (nodeId: string, direction: "input" | "output"): string[] => {
    const edgeHandleIds = workflow.edges.flatMap((edge) => {
      if (direction === "input" && edge.target_node_id === nodeId && edge.target_handle) return [edge.target_handle];
      if (direction === "output" && edge.source_node_id === nodeId && edge.source_handle) return [edge.source_handle];
      return [];
    });
    return Array.from(new Set([direction, ...edgeHandleIds])).sort();
  };
  if (openFolderId) {
    return buildLocalFolderGraph(workflow, openFolderId).nodes.map((node) => ({
      id: node.id,
      type: "workflow-node-v2",
      position: { x: node.position_x, y: node.position_y },
      width: V2_NODE_WIDTH,
      height: V2_NODE_HEIGHT,
      hidden: Boolean(revealVisibility && !revealVisibility.nodeIds.has(node.id)),
      data: {
        kind: "node",
        node,
        revealActive: Boolean(revealVisibility),
        runBusy: options.runningNodeId === node.id,
        structureBusy: options.structureBusy,
        onRun: options.onRunNode,
        onBindReference: options.onBindReference,
        inputHandleIds: handleIds(node.id, "input"),
        outputHandleIds: handleIds(node.id, "output"),
      },
    }));
  }
  const fullProjection = projectGlobalGraph(workflow);
  const visibleWorkflow = revealVisibility
    ? {
        ...workflow,
        nodes: workflow.nodes.filter((node) => revealVisibility.nodeIds.has(node.id)),
        edges: workflow.edges.filter(
          (edge) =>
            revealVisibility.edgeIds.has(edge.id) &&
            revealVisibility.nodeIds.has(edge.source_node_id) &&
            revealVisibility.nodeIds.has(edge.target_node_id),
        ),
      }
    : workflow;
  const visibleFolderSummary = new Map(
    workflow.folders.map((folder) => {
      const memberCount = visibleWorkflow.nodes.filter((node) => node.folder_id === folder.id).length;
      return [
        folder.id,
        memberCount
          ? deriveFolderSummary(visibleWorkflow, folder.id)
          : {
              member_count: 0,
              node_types: [],
              status: "idle" as const,
              preview_asset_ids: [],
              inbound_edge_count: 0,
              outbound_edge_count: 0,
            },
      ] as const;
    }),
  );
  return fullProjection.nodes.map((item): WorkflowCanvasNode => item.kind === "folder" ? {
    id: item.id,
    type: "workflow-folder-v2",
    position: item.position,
    width: FOLDER_CARD_WIDTH,
    height: FOLDER_CARD_HEIGHT,
    hidden: Boolean(revealVisibility && !revealVisibility.folderIds.has(item.folder.id)),
    data: {
      kind: "folder",
      projection: revealVisibility
        ? {
            ...item,
            member_ids: item.member_ids.filter((nodeId) => revealVisibility.nodeIds.has(nodeId)),
            summary: visibleFolderSummary.get(item.folder.id) ?? item.summary,
          }
        : item,
      revealActive: Boolean(revealVisibility),
      structureBusy: options.structureBusy,
      onOpen: options.onOpenFolder,
    },
  } : {
    id: item.id,
    type: "workflow-node-v2",
    position: item.position,
    width: V2_NODE_WIDTH,
    height: V2_NODE_HEIGHT,
    hidden: Boolean(revealVisibility && !revealVisibility.nodeIds.has(item.node.id)),
    data: {
      kind: "node",
      node: item.node,
      revealActive: Boolean(revealVisibility),
      runBusy: options.runningNodeId === item.node.id,
      structureBusy: options.structureBusy,
      onRun: options.onRunNode,
      onBindReference: options.onBindReference,
      inputHandleIds: handleIds(item.node.id, "input"),
      outputHandleIds: handleIds(item.node.id, "output"),
    },
  });
}

function toCanvasEdges(
  workflow: ProductWorkflowV2,
  openFolderId: string | null,
  revealVisibility?: WorkflowRevealVisibility,
): WorkflowCanvasEdge[] {
  if (openFolderId) {
    return buildLocalFolderGraph(workflow, openFolderId).edges.map((edge) => ({
      id: edge.id,
      source: edge.source_node_id,
      target: edge.target_node_id,
      sourceHandle: edge.source_handle,
      targetHandle: edge.target_handle,
      type: "smoothstep",
      hidden: Boolean(revealVisibility && !revealVisibility.edgeIds.has(edge.id)),
      style: { stroke: "#94a3b8", strokeWidth: 1.8 },
      data: { projected: false, originalEdgeIds: [edge.id], count: 1 },
    }));
  }
  return projectGlobalGraph(workflow).edges.map((edge) => {
    const revealedEdgeIds = revealVisibility
      ? edge.original_edge_ids.filter((edgeId) => revealVisibility.edgeIds.has(edgeId))
      : edge.original_edge_ids;
    const count = revealedEdgeIds.length;
    return {
      id: edge.id,
      source: edge.source,
      target: edge.target,
      sourceHandle: edge.source_handle,
      targetHandle: edge.target_handle,
      type: "smoothstep",
      hidden: Boolean(revealVisibility && count === 0),
      label: count > 1 ? String(count) : undefined,
      animated: edge.projected && count > 1,
      style: {
        stroke: edge.projected ? "#6366f1" : "#94a3b8",
        strokeWidth: edge.projected ? 2.2 : 1.8,
      },
      labelStyle: { fill: "#475569", fontSize: 11, fontWeight: 700 },
      labelBgStyle: { fill: "#ffffff", fillOpacity: 0.92 },
      data: {
        projected: edge.projected,
        originalEdgeIds: revealedEdgeIds,
        count,
      },
    };
  });
}

function isRealNode(node: WorkflowCanvasNode): node is Node<WorkflowNodeData, "workflow-node-v2"> {
  return node.data.kind === "node";
}

export function V2WorkflowCanvas({
  workflow,
  revealVisibility,
  openFolderId,
  viewport,
  structureBusy,
  runningNodeId,
  onOpenFolder,
  onRunNode,
  onBindReference,
  onSelectionChange,
  onLayoutCommit,
  onFolderTranslate,
  onViewportChange,
}: V2WorkflowCanvasProps) {
  const { t } = useI18n();
  const surfaceRef = useRef<HTMLDivElement | null>(null);
  const [surfaceSize, setSurfaceSize] = useState(() => ({
    width: typeof window === "undefined" ? 1440 : window.innerWidth,
    height: typeof window === "undefined" ? 900 : window.innerHeight,
  }));
  const [snapToGrid, setSnapToGrid] = useState(false);
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([]);
  useEffect(() => {
    const surface = surfaceRef.current;
    if (!surface || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) => {
      if (!entry) return;
      setSurfaceSize({
        width: Math.max(1, Math.round(entry.contentRect.width)),
        height: Math.max(1, Math.round(entry.contentRect.height)),
      });
    });
    observer.observe(surface);
    return () => observer.disconnect();
  }, []);
  const restoredViewport = isWorkflowCanvasViewportCompatible(viewport, surfaceSize.width) ? viewport : null;
  const layoutMode = surfaceSize.width >= 1024 ? "wide" : "compact";
  const graphIdentity = `${workflow.id}:${openFolderId ?? "global"}:${workflow.edit_version}`;
  const graphNodes = useMemo(
    () => toCanvasNodes(workflow, openFolderId, {
      structureBusy,
      runningNodeId,
      onOpenFolder,
      onRunNode,
      onBindReference,
      revealVisibility,
    }),
    [onBindReference, onOpenFolder, onRunNode, openFolderId, revealVisibility, runningNodeId, structureBusy, workflow],
  );
  const graphEdges = useMemo(
    () => toCanvasEdges(workflow, openFolderId, revealVisibility),
    [openFolderId, revealVisibility, workflow],
  );
  const [nodes, setNodes, onNodesChange] = useNodesState<WorkflowCanvasNode>(graphNodes);
  const previousIdentityRef = useRef(graphIdentity);
  const dragStartRef = useRef(new Map<string, { x: number; y: number }>());

  useEffect(() => {
    const identityChanged = previousIdentityRef.current !== graphIdentity;
    previousIdentityRef.current = graphIdentity;
    setNodes((current) => {
      if (identityChanged) {
        return graphNodes;
      }
      const currentById = new Map(current.map((node) => [node.id, node]));
      return graphNodes.map((node) => {
        const previous = currentById.get(node.id);
        return previous
          ? { ...node, position: previous.position, selected: previous.selected }
          : node;
      });
    });
  }, [graphIdentity, graphNodes, setNodes]);

  const handleNodeDragStart = useCallback<OnNodeDrag<WorkflowCanvasNode>>((_event, activeNode, selectedNodes) => {
    const draggedNodes = selectedNodes.length ? selectedNodes : [activeNode];
    dragStartRef.current = new Map(draggedNodes.map((node) => [node.id, { ...node.position }]));
  }, []);

  const handleNodeDragStop = useCallback<OnNodeDrag<WorkflowCanvasNode>>((_event, activeNode, selectedNodes) => {
    const start = dragStartRef.current;
    dragStartRef.current = new Map();
    if (activeNode.data.kind === "folder") {
      const origin = start.get(activeNode.id);
      if (!origin) {
        return;
      }
      const deltaX = Math.round(activeNode.position.x - origin.x);
      const deltaY = Math.round(activeNode.position.y - origin.y);
      if (deltaX || deltaY) {
        onFolderTranslate(activeNode.data.projection.folder.id, deltaX, deltaY);
      }
      return;
    }
    const draggedNodes = selectedNodes.length ? selectedNodes : [activeNode];
    const positions = draggedNodes
      .filter(isRealNode)
      .map((node) => ({
        node_id: node.data.node.id,
        position_x: Math.round(node.position.x),
        position_y: Math.round(node.position.y),
      }))
      .filter((position) => {
        const source = workflow.nodes.find((node) => node.id === position.node_id);
        return source && (source.position_x !== position.position_x || source.position_y !== position.position_y);
      });
    if (positions.length) {
      onLayoutCommit(positions);
    }
  }, [onFolderTranslate, onLayoutCommit, workflow.nodes]);

  const defaultViewport: Viewport | undefined = restoredViewport ?? undefined;
  const includeHiddenNodes = Boolean(revealVisibility);
  const fitMinZoom = workflowCanvasFitMinZoom(surfaceSize.width);
  const fitViewOptions = useMemo(() => ({
    ...V2_CONTROL_FIT_VIEW_OPTIONS,
    includeHiddenNodes,
    minZoom: fitMinZoom,
  }), [fitMinZoom, includeHiddenNodes]);
  const commitAutoLayout = useCallback(() => {
    const positions = buildAutoLayoutNodePositions(workflow, openFolderId);
    if (positions.length) onLayoutCommit(positions);
  }, [onLayoutCommit, openFolderId, workflow]);
  const persistViewport = useCallback((nextViewport: Viewport) => {
    onViewportChange({
      ...nextViewport,
      surface_width: surfaceSize.width,
      surface_height: surfaceSize.height,
    });
  }, [onViewportChange, surfaceSize.height, surfaceSize.width]);
  const handleNodeDoubleClick = useCallback<NodeMouseHandler<WorkflowCanvasNode>>((_event, node) => {
    if (node.data.kind === "folder") {
      onOpenFolder(node.data.projection.folder.id);
    }
  }, [onOpenFolder]);
  const handleSelectionChange = useCallback<OnSelectionChangeFunc<WorkflowCanvasNode, WorkflowCanvasEdge>>(
    ({ nodes: selectedNodes }) => {
      const nodeIds = selectedNodes.filter(isRealNode).map((node) => node.data.node.id);
      setSelectedNodeIds((current) => (
        current.length === nodeIds.length && current.every((nodeId, index) => nodeId === nodeIds[index])
          ? current
          : nodeIds
      ));
      onSelectionChange(nodeIds);
    },
    [onSelectionChange],
  );
  const handlePaneClick = useCallback(() => {
    setSelectedNodeIds((current) => current.length ? [] : current);
    onSelectionChange([]);
  }, [onSelectionChange]);
  const handleMoveEnd = useCallback<OnMoveEnd>((_event, nextViewport) => {
    persistViewport(nextViewport);
  }, [persistViewport]);
  const toggleSnapToGrid = useCallback(() => {
    setSnapToGrid((current) => !current);
  }, []);

  return (
    <div ref={surfaceRef} className="h-full min-h-0 w-full overflow-hidden">
      <ReactFlow<WorkflowCanvasNode, WorkflowCanvasEdge>
      key={`${workflow.id}:${openFolderId ?? "global"}:${layoutMode}:${restoredViewport ? "restore" : "fit"}`}
      nodes={nodes}
      edges={graphEdges}
      nodeTypes={nodeTypes}
      onNodesChange={onNodesChange}
      onNodeDragStart={handleNodeDragStart}
      onNodeDragStop={handleNodeDragStop}
      onNodeDoubleClick={handleNodeDoubleClick}
      onSelectionChange={handleSelectionChange}
      onPaneClick={handlePaneClick}
      onMoveEnd={handleMoveEnd}
      defaultViewport={defaultViewport}
      fitView={!restoredViewport}
      fitViewOptions={fitViewOptions}
      minZoom={0.12}
      maxZoom={2}
      nodesDraggable={!structureBusy}
      nodesConnectable={false}
      edgesReconnectable={false}
      snapToGrid={snapToGrid}
      snapGrid={V2_SNAP_GRID}
      selectionMode={SelectionMode.Partial}
      selectionOnDrag
      multiSelectionKeyCode={V2_MULTI_SELECTION_KEYS}
      panOnDrag={V2_PAN_BUTTONS}
      deleteKeyCode={null}
      className="bg-transparent"
      proOptions={V2_PRO_OPTIONS}
    >
      <Background variant={BackgroundVariant.Dots} gap={28} size={1.4} color="#94a3b8" />
      <V2CanvasControls
        selectedNodeIds={selectedNodeIds}
        snapToGrid={snapToGrid}
        structureBusy={structureBusy}
        fitMinZoom={fitMinZoom}
        onToggleSnapToGrid={toggleSnapToGrid}
        onAutoLayout={commitAutoLayout}
        onViewportCommit={persistViewport}
      />
      <MiniMap
        position="bottom-right"
        aria-label={t("detail.canvasMiniMap")}
        className="hidden !m-4 overflow-hidden rounded-xl border border-slate-200 bg-white/90 shadow-lg dark:border-slate-700 dark:!bg-slate-950/90 sm:block"
        nodeColor={(node) => node.data?.kind === "folder" ? "#6366f1" : "#94a3b8"}
        nodeBorderRadius={6}
        pannable
        zoomable
      />
      </ReactFlow>
    </div>
  );
}

function V2CanvasControls({
  selectedNodeIds,
  snapToGrid,
  structureBusy,
  fitMinZoom,
  onToggleSnapToGrid,
  onAutoLayout,
  onViewportCommit,
}: {
  selectedNodeIds: string[];
  snapToGrid: boolean;
  structureBusy: boolean;
  fitMinZoom: number;
  onToggleSnapToGrid: () => void;
  onAutoLayout: () => void;
  onViewportCommit: (viewport: Viewport) => void;
}) {
  const { t } = useI18n();
  const { zoom } = useViewport();
  const reactFlow = useReactFlow<WorkflowCanvasNode, WorkflowCanvasEdge>();
  const commitAfterAction = () => window.setTimeout(() => onViewportCommit(reactFlow.getViewport()), 240);
  const fitSelection = () => {
    const selectedNodes = selectedNodeIds
      .filter((nodeId) => reactFlow.getNode(nodeId))
      .map((nodeId) => ({ id: nodeId }));
    if (!selectedNodes.length) return;
    void reactFlow.fitView({ nodes: selectedNodes, padding: 0.22, duration: 180, maxZoom: 1.05 })
      .then(() => onViewportCommit(reactFlow.getViewport()));
  };
  return (
    <Controls
      position="top-left"
      orientation="horizontal"
      showInteractive={false}
      fitViewOptions={{ ...V2_CONTROL_FIT_VIEW_OPTIONS, minZoom: fitMinZoom }}
      onZoomIn={commitAfterAction}
      onZoomOut={commitAfterAction}
      onFitView={commitAfterAction}
      aria-label={t("detail.canvasControls")}
      className="workflow-canvas-controls nopan nodrag nowheel z-30 !m-0 translate-x-3 translate-y-3 lg:translate-x-4 lg:translate-y-4"
    >
      <ControlButton
        onClick={() => void reactFlow.zoomTo(1).then(() => onViewportCommit(reactFlow.getViewport()))}
        aria-label={t("detail.resetZoom")}
        title={t("detail.resetZoom")}
      >
        <span className="text-[11px] tabular-nums">{Math.round(zoom * 100)}%</span>
      </ControlButton>
      <ControlButton
        onClick={fitSelection}
        disabled={!selectedNodeIds.length}
        aria-label={t("detail.fitSelection")}
        title={t("detail.fitSelection")}
      >
        <Focus size={13} />
      </ControlButton>
      <ControlButton
        onClick={onToggleSnapToGrid}
        aria-label={t("detail.snapToGrid")}
        title={t("detail.snapToGrid")}
        className={snapToGrid ? "!bg-indigo-50 dark:!bg-violet-500/20" : ""}
      >
        <Grid size={13} className={snapToGrid ? "text-indigo-600 dark:text-violet-300" : ""} />
      </ControlButton>
      <ControlButton
        onClick={onAutoLayout}
        disabled={structureBusy}
        aria-label={t("detail.autoLayout")}
        title={t("detail.autoLayout")}
      >
        {structureBusy ? <Loader2 size={13} className="animate-spin" /> : <Sparkles size={13} />}
      </ControlButton>
    </Controls>
  );
}

export { assetThumbnailUrl, folderSyntheticNodeId };
