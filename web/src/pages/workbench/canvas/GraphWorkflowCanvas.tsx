import {
  BaseEdge,
  ConnectionLineType,
  ConnectionMode,
  EdgeToolbar,
  MiniMap,
  ReactFlow,
  SelectionMode,
  getBezierPath,
  useConnection,
  useNodesState,
  useReactFlow,
  useUpdateNodeInternals,
  useViewport,
} from "@xyflow/react";
import type {
  Connection,
  Edge,
  EdgeProps,
  IsValidConnection,
  Node,
  NodeProps,
  NodeMouseHandler,
  OnMoveEnd,
  OnNodeDrag,
  OnSelectionChangeFunc,
  ReactFlowInstance,
  Viewport,
} from "@xyflow/react";
import { BookmarkPlus, ChevronsRight, CopyPlus, Folder, FolderOpen, Focus, Hand, Link2, Loader2, MousePointer2, Pencil, Play, Trash2, Ungroup } from "lucide-react";
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { DragEvent, MouseEvent as ReactMouseEvent } from "react";

import { api } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { sanitizeFilenamePart } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { GraphGroup, GraphNode, GraphNodeCatalog, GraphProjection, WorkflowNodeStatus } from "../../../lib/types";
import {
  graphProposalEdgeStates,
  graphProposalNodeStates,
  overlayGraphProposal,
  type GraphProposalState,
} from "./graphProposalOverlay";
import { IMAGE_EXPLORER_DRAG_MIME, decodeAssetDragPayload } from "../chrome/image-explorer/explorerState";
import type { GraphAssetDropInput } from "./graphAssetDrop";
import {
  WorkflowCanvasControls,
  WorkflowCanvasGrid,
  WorkflowCanvasMobileModeTabs,
  WorkflowCanvasNodePort,
  WorkflowCanvasNodeToolbar,
  WorkflowCanvasNodeToolbarButton,
} from "../chrome/WorkflowCanvasChrome";
import { WorkflowNodePresentationCard } from "../chrome/WorkflowNodeCard";
import {
  WORKFLOW_CANVAS_PAN_ACTIVATION_KEY_CODE,
  WORKFLOW_CANVAS_ZOOM_ACTIVATION_KEY_CODES,
  deriveWorkflowCanvasInteractionPolicy,
  selectWorkflowCanvasNode,
  shouldToggleWorkflowCanvasNodeSelection,
  type CanvasInteractionMode,
} from "../chrome/workflowCanvasInteraction";
import {
  isWorkflowCanvasViewportCompatible,
  workflowCanvasFitMinZoom,
  type WorkflowCanvasViewport,
} from "./canvasState";
import {
  graphEdgeRoleLabelKey,
  graphNodeHasInput,
  graphNodePresentationKind,
  graphPortVisualState,
  isGraphConnectionValid,
  isProcessingNode,
} from "./graphCatalog";
import type { GraphNodeRunPresentation } from "./graphRunDisplay";
import {
  GRAPH_NODE_WIDTH,
  GRAPH_SNAP,
  computeGraphGroupBounds,
  graphCanvasView,
} from "./graphLayout";
import { graphEdgeEmphasis, graphPortVisualScale } from "./graphCanvasVisual";

const SNAP_GRID: [number, number] = [GRAPH_SNAP, GRAPH_SNAP];
const PRO_OPTIONS = { hideAttribution: true };
const CONTROL_FIT_VIEW_OPTIONS = { padding: 0.22, duration: 180, maxZoom: 1.05 };

type ConnectionHandleSnapshot = {
  inProgress: boolean;
  fromHandle: { nodeId: string; type: "source" | "target" } | null;
};

interface GraphNodeData extends Record<string, unknown> {
  kind: "node";
  node: GraphNode;
  status: WorkflowNodeStatus;
  failureReason: string | null;
  lastRunAt: string | null;
  retryable: boolean;
  runBusy: boolean;
  structureBusy: boolean;
  onRun: (node: GraphNode) => void;
  onRunToNode: (node: GraphNode) => void;
  onRunShot?: (groupId: string) => void;
  onBind: (node: GraphNode) => void;
  onDuplicate: (node: GraphNode) => void;
  onSaveRecipe: (node: GraphNode) => void;
  onDelete: (node: GraphNode) => void;
  onSelectNode: (nodeId: string, event: ReactMouseEvent<HTMLElement>) => void;
  graph: GraphProjection;
  catalog: GraphNodeCatalog | null;
  proposalState?: GraphProposalState | null;
}

interface GraphGroupData extends Record<string, unknown> {
  kind: "group";
  group: GraphGroup;
  bounds: { x: number; y: number; width: number; height: number };
  structureBusy: boolean;
  onEnter: (groupId: string) => void;
  onRename: (groupId: string, title: string) => void;
  onDissolve: (groupId: string) => void;
  onRunShot?: (groupId: string) => void;
}

type GraphCanvasNode =
  | Node<GraphNodeData, "graph-node">
  | Node<GraphGroupData, "graph-group">;

type GraphCanvasEdge = Edge<{
  role: string;
  roleLabel: string | null;
  structureBusy: boolean;
  deleteLabel: string;
  emphasis: "active" | "receded";
  onDelete: (edgeId: string) => void;
  proposalState?: GraphProposalState | null;
}, "graph-edge">;

function nodeTypeLabel(type: GraphNode["node_type"], t: ReturnType<typeof useI18n>["t"]): string {
  const keys = {
    product_source: "graph.node.productSource",
    image_asset: "graph.node.imageAsset",
    creative_brief: "graph.node.creativeBrief",
    visual_system: "graph.node.visualSystem",
    prompt_generation: "graph.node.promptGeneration",
    image_generation: "graph.node.imageGeneration",
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
    unknown: "detail.nodeStatus.unknown",
  } as const;
  return t(keys[status]);
}

function nodeImage(node: GraphNode): DownloadableImage | null {
  if (!node.preview_asset_id) return null;
  return {
    previewUrl: api.getProductImageAssetMediaUrl(node.preview_asset_id, "thumbnail"),
    downloadUrl: api.getProductImageAssetMediaUrl(node.preview_asset_id),
    filename: `${sanitizeFilenamePart(node.title, "workflow-image")}.png`,
    alt: node.title,
  };
}

export const GraphNodeCard = memo(function GraphNodeCard({
  id,
  data,
  selected,
  dragging,
  isConnectable,
}: NodeProps<Node<GraphNodeData>>) {
  const { t } = useI18n();
  const reactFlow = useReactFlow();
  const { node } = data;
  const { zoom } = useViewport();
  const portVisualScale = graphPortVisualScale(zoom);
  const connection = useConnection<GraphCanvasNode, ConnectionHandleSnapshot>((snapshot) => ({
    inProgress: snapshot.inProgress,
    fromHandle: snapshot.fromHandle
      ? { nodeId: snapshot.fromHandle.nodeId, type: snapshot.fromHandle.type }
      : null,
  }));
  const updateNodeInternals = useUpdateNodeInternals();
  const hasInput = graphNodeHasInput(node.node_type, data.catalog)
    || node.node_type === "prompt_generation"
    || node.node_type === "image_generation";
  const running = data.status === "queued" || data.status === "running";

  useLayoutEffect(() => {
    updateNodeInternals(id);
  }, [id, hasInput, node.preview_asset_id, node.title, running, updateNodeInternals]);

  const portState = graphPortVisualState(
    data.graph,
    node.id,
    "target",
    {
      inProgress: connection.inProgress,
      fromNodeId: connection.fromHandle?.nodeId ?? null,
      fromType: connection.fromHandle?.type ?? null,
    },
    data.catalog,
  );
  const sourceState = graphPortVisualState(
    data.graph,
    node.id,
    "source",
    {
      inProgress: connection.inProgress,
      fromNodeId: connection.fromHandle?.nodeId ?? null,
      fromType: connection.fromHandle?.type ?? null,
    },
    data.catalog,
  );

  const proposalState = data.proposalState ?? null;
  return (
    <div
      className={`relative w-[248px] overflow-visible ${
        proposalState === "deleted"
          ? "opacity-40"
          : proposalState === "added"
            ? "opacity-90 outline-dashed outline-2 outline-indigo-400/80"
            : proposalState === "changed"
              ? "outline outline-2 outline-amber-400/80"
              : ""
      }`}
      data-graph-proposal-state={proposalState ?? undefined}
    >
      <WorkflowCanvasNodeToolbar visible={selected}>
        {node.node_type === "image_asset" ? (
          <WorkflowCanvasNodeToolbarButton
            label={t("graph.inspector.bind")}
            disabled={data.structureBusy}
            onClick={() => data.onBind(node)}
          >
            <Link2 size={16} aria-hidden="true" />
          </WorkflowCanvasNodeToolbarButton>
        ) : null}
        {isProcessingNode(node, data.catalog) && data.proposalState !== "added" ? (
          <>
            <WorkflowCanvasNodeToolbarButton
              label={t("graph.canvas.runNode")}
              disabled={data.runBusy || running || data.structureBusy}
              onClick={() => data.onRun(node)}
            >
              {data.runBusy || running ? <Loader2 size={16} className="animate-spin motion-reduce:animate-none" aria-hidden="true" /> : <Play size={16} aria-hidden="true" />}
            </WorkflowCanvasNodeToolbarButton>
            <WorkflowCanvasNodeToolbarButton
              label={t("graph.runs.scope.toNode")}
              disabled={data.runBusy || running || data.structureBusy}
              onClick={() => data.onRunToNode(node)}
            >
              <ChevronsRight size={16} aria-hidden="true" />
            </WorkflowCanvasNodeToolbarButton>
          </>
        ) : null}
        <WorkflowCanvasNodeToolbarButton
          label={t("detail.duplicate")}
          disabled={data.structureBusy || running}
          onClick={() => data.onDuplicate(node)}
        >
          <CopyPlus size={16} aria-hidden="true" />
        </WorkflowCanvasNodeToolbarButton>
        <WorkflowCanvasNodeToolbarButton
          label={t("graph.canvas.saveRecipe")}
          disabled={data.structureBusy || running}
          onClick={() => data.onSaveRecipe(node)}
        >
          <BookmarkPlus size={16} aria-hidden="true" />
        </WorkflowCanvasNodeToolbarButton>
        <WorkflowCanvasNodeToolbarButton
          label={t("detail.fitSelection")}
          onClick={() => {
            void reactFlow.fitView({ padding: 0.22, duration: 180, maxZoom: 1.05, nodes: [{ id }] });
          }}
        >
          <Focus size={16} aria-hidden="true" />
        </WorkflowCanvasNodeToolbarButton>
        <WorkflowCanvasNodeToolbarButton
          label={t("graph.canvas.delete")}
          disabled={data.structureBusy || running}
          destructive
          onClick={() => data.onDelete(node)}
        >
          <Trash2 size={16} aria-hidden="true" />
        </WorkflowCanvasNodeToolbarButton>
      </WorkflowCanvasNodeToolbar>
      {hasInput ? (
        <WorkflowCanvasNodePort
          id="input"
          type="target"
          top="50%"
          label={t("detail.inputHandle")}
          connectable={Boolean(isConnectable)}
          visualState={portState}
          visualScale={portVisualScale}
        />
      ) : null}
      <WorkflowCanvasNodePort
        id="output"
        type="source"
        top="50%"
        label={t("detail.outputHandle")}
        connectable={Boolean(isConnectable)}
        visualState={sourceState}
        visualScale={portVisualScale}
      />
      <WorkflowNodePresentationCard
        id={node.id}
        kind={graphNodePresentationKind(node.node_type)}
        title={node.title}
        label={nodeTypeLabel(node.node_type, t)}
        status={data.status}
        statusLabel={data.status !== "idle"
          ? nodeStatusLabel(data.status, t)
          : node.unused
            ? t("graph.inspector.unused")
            : nodeStatusLabel(data.status, t)}
        image={nodeImage(node)}
        imageWaiting={node.node_type === "image_generation" && running}
        waitingLabel={nodeStatusLabel(data.status, t)}
        activityText={running ? nodeStatusLabel(data.status, t) : null}
        failureReason={data.failureReason}
        lastRunAt={data.lastRunAt}
        retryable={data.retryable}
        primarySelected={selected}
        dragging={dragging}
        onSelect={(event) => {
          event.stopPropagation();
          data.onSelectNode(node.id, event);
        }}
      />
    </div>
  );
});

export const GraphGroupCard = memo(function GraphGroupCard({
  data,
  selected,
}: NodeProps<Node<GraphGroupData>>) {
  const { t } = useI18n();
  const { group, bounds, structureBusy, onEnter, onRename, onDissolve, onRunShot } = data;
  const [draft, setDraft] = useState(group.title);
  const [editing, setEditing] = useState(false);
  useEffect(() => {
    setDraft(group.title);
    setEditing(false);
  }, [group.id, group.title]);
  const commitTitle = () => {
    const title = draft.trim();
    setEditing(false);
    if (!title) {
      setDraft(group.title);
      return;
    }
    if (title !== group.title) onRename(group.id, title);
  };
  return (
    <div
      style={{ width: bounds.width, height: bounds.height }}
      className={`group relative rounded-2xl border-2 border-dashed transition-all pointer-events-none ${selected
        ? "border-slate-400 bg-slate-50/40 dark:border-slate-500 dark:bg-slate-800/20"
        : "border-slate-300/80 bg-slate-50/30 dark:border-slate-700/80 dark:bg-[#0c1322]/25"
        }`}
      data-graph-group-id={group.id}
    >
      <div
        className="pointer-events-auto flex items-center gap-2 border-b border-dashed border-slate-200/80 bg-white/70 px-3.5 py-2 backdrop-blur-sm dark:border-slate-800/80 dark:bg-[#0f172a]/60 rounded-t-2xl"
        onDoubleClick={(event) => {
          if (structureBusy) return;
          if ((event.target as HTMLElement).closest("input, button")) return;
          event.stopPropagation();
          onEnter(group.id);
        }}
      >
        <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300">
          <Folder size={13} aria-hidden="true" />
        </span>
        {editing ? (
          <input
            value={draft}
            maxLength={255}
            autoFocus
            disabled={structureBusy}
            onChange={(event) => setDraft(event.target.value)}
            onBlur={commitTitle}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                commitTitle();
              }
              if (event.key === "Escape") {
                setDraft(group.title);
                setEditing(false);
              }
            }}
            className="nodrag nowheel nopan min-w-0 flex-1 rounded-md border border-slate-200 bg-white px-2 py-1 text-xs font-bold text-slate-800 outline-none dark:border-slate-600 dark:bg-[#0f1726] dark:text-slate-100"
          />
        ) : (
          <span className="min-w-0 flex-1 truncate text-xs font-bold text-slate-800 dark:text-slate-200">{group.title}</span>
        )}
        <span className="rounded-full bg-slate-200/70 px-2 py-0.5 text-[10px] font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">
          {t("workbench.folder.memberCount", { count: group.member_ids.length })}
        </span>
        {onRunShot ? (
          <button
            type="button"
            className="nodrag nowheel nopan flex h-7 w-7 items-center justify-center rounded-md text-slate-500 hover:bg-white hover:text-slate-800 disabled:opacity-40 dark:hover:bg-slate-900 dark:hover:text-slate-100"
            disabled={structureBusy}
            title={t("graph.canvas.runShot")}
            aria-label={t("graph.canvas.runShot")}
            data-run-shot=""
            onClick={(event) => {
              event.stopPropagation();
              onRunShot(group.id);
            }}
          >
            <Play size={12} aria-hidden="true" />
          </button>
        ) : null}
        <button
          type="button"
          className="nodrag nowheel nopan flex h-7 w-7 items-center justify-center rounded-md text-slate-500 hover:bg-white hover:text-slate-800 disabled:opacity-40 dark:hover:bg-slate-900 dark:hover:text-slate-100"
          disabled={structureBusy}
          title={t("graph.canvas.enterGroup")}
          aria-label={t("graph.canvas.enterGroup")}
          data-enter-group=""
          onClick={(event) => {
            event.stopPropagation();
            onEnter(group.id);
          }}
        >
          <FolderOpen size={12} aria-hidden="true" />
        </button>
        <button
          type="button"
          className="nodrag nowheel nopan flex h-7 w-7 items-center justify-center rounded-md text-slate-500 hover:bg-white hover:text-slate-800 disabled:opacity-40 dark:hover:bg-slate-900 dark:hover:text-slate-100"
          disabled={structureBusy}
          title={t("graph.canvas.renameGroup")}
          aria-label={t("graph.canvas.renameGroup")}
          onClick={(event) => {
            event.stopPropagation();
            setEditing(true);
          }}
        >
          <Pencil size={12} aria-hidden="true" />
        </button>
        <button
          type="button"
          className="nodrag nowheel nopan flex h-7 w-7 items-center justify-center rounded-md text-slate-500 hover:bg-red-50 hover:text-red-600 disabled:opacity-40 dark:hover:bg-red-500/10 dark:hover:text-red-300"
          disabled={structureBusy}
          title={t("graph.palette.dissolve")}
          aria-label={t("graph.palette.dissolve")}
          onClick={(event) => {
            event.stopPropagation();
            onDissolve(group.id);
          }}
        >
          <Ungroup size={12} aria-hidden="true" />
        </button>
      </div>
    </div>
  );
});

const GraphCanvasEdgeCard = memo(function GraphCanvasEdgeCard({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  selected,
  data,
}: EdgeProps<GraphCanvasEdge>) {
  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });
  const [hovered, setHovered] = useState(false);
  const emphasis = hovered || selected ? "active" : data?.emphasis ?? "receded";
  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        style={{
          stroke: data?.proposalState === "added"
            ? "#6366f1"
            : data?.proposalState === "deleted"
              ? "rgba(148,163,184,0.45)"
              : emphasis === "active" ? (selected ? "#334155" : "#64748b") : "rgba(148,163,184,0.28)",
          strokeWidth: emphasis === "active" ? (selected ? 2.2 : 1.6) : 1.1,
          strokeDasharray: data?.proposalState === "added" || data?.proposalState === "deleted" ? "6 4" : undefined,
          opacity: data?.proposalState === "deleted" ? 0.45 : 1,
        }}
        data-graph-proposal-state={data?.proposalState ?? undefined}
        label={(hovered || selected) ? data?.roleLabel ?? undefined : undefined}
        labelStyle={{ fontSize: 10, fill: "#64748b" }}
      />
      <path
        d={edgePath}
        fill="none"
        stroke="transparent"
        strokeWidth={15}
        data-edge-emphasis={emphasis}
        className="cursor-pointer"
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      />
      <EdgeToolbar
        edgeId={id}
        x={labelX}
        y={labelY}
        isVisible
        className={`nodrag nowheel nopan transition-all duration-200 ${hovered || selected ? "pointer-events-auto scale-100 opacity-100" : "pointer-events-none scale-75 opacity-0"
          }`}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        <button
          type="button"
          className="nodrag nowheel nopan flex h-7 w-7 items-center justify-center rounded-full border border-slate-200 bg-white/95 text-slate-500 shadow-sm hover:border-red-200 hover:bg-red-50 hover:text-red-600 disabled:opacity-45 dark:border-slate-800 dark:bg-[#0f1726]/95"
          onClick={(event) => {
            event.stopPropagation();
            if (!data?.structureBusy) data?.onDelete(id);
          }}
          disabled={data?.structureBusy}
          title={[data?.roleLabel, data?.deleteLabel].filter(Boolean).join(" · ")}
          aria-label={[data?.roleLabel, data?.deleteLabel].filter(Boolean).join(" · ")}
        >
          <Trash2 size={13} strokeWidth={2.2} aria-hidden="true" />
        </button>
      </EdgeToolbar>
    </>
  );
});

const nodeTypes = { "graph-node": GraphNodeCard, "graph-group": GraphGroupCard };
const edgeTypes = { "graph-edge": GraphCanvasEdgeCard };

function isRealNode(node: GraphCanvasNode): node is Node<GraphNodeData, "graph-node"> {
  return node.data.kind === "node";
}

export function GraphWorkflowCanvas({
  graph,
  catalog,
  selectedNodeIds,
  busy,
  nodeStatuses,
  nodePresentations = {},
  runningNodeId,
  viewport,
  onViewportChange,
  mobileInteractionMode = "edit",
  onMobileInteractionModeChange,
  compact = false,
  enteredGroupId = null,
  canvasSyncVersion = 0,
  onSelect,
  onConnect,
  onMove,
  onTranslateGroup,
  onDeleteNode,
  onDeleteEdge,
  onRunNode,
  onRunToNode,
  onRunShot,
  onBindNode,
  onDuplicateNode,
  onSaveRecipeNode,
  onAutoLayout,
  onAssetDrop,
  onRenameGroup,
  onDissolveGroup,
  onEnterGroup,
}: {
  graph: GraphProjection;
  catalog: GraphNodeCatalog | null;
  selectedNodeIds: string[];
  busy: boolean;
  nodeStatuses: Record<string, WorkflowNodeStatus>;
  nodePresentations?: Record<string, GraphNodeRunPresentation>;
  runningNodeId: string | null;
  viewport: WorkflowCanvasViewport | null;
  onViewportChange: (viewport: WorkflowCanvasViewport, groupId: string | null) => void;
  mobileInteractionMode?: CanvasInteractionMode;
  onMobileInteractionModeChange?: (mode: CanvasInteractionMode) => void;
  compact?: boolean;
  enteredGroupId?: string | null;
  canvasSyncVersion?: number;
  onSelect: (nodeIds: string[]) => void | Promise<void>;
  onConnect: (source: string, target: string) => void;
  onMove: (positions: Array<{ node_id: string; position_x: number; position_y: number }>) => void;
  onTranslateGroup: (groupId: string, deltaX: number, deltaY: number) => void;
  onDeleteNode: (nodeId: string) => void;
  onDeleteEdge: (edgeId: string) => void;
  onRunNode: (nodeId: string) => void;
  onRunToNode?: (nodeId: string) => void;
  onRunShot?: (groupId: string) => void;
  onBindNode: (nodeId: string) => void;
  onDuplicateNode: (nodeIds: string[]) => void;
  onSaveRecipeNode?: (nodeId: string) => void;
  onAutoLayout: () => void;
  onAssetDrop?: (input: GraphAssetDropInput) => void;
  onRenameGroup: (groupId: string, title: string) => void;
  onDissolveGroup: (groupId: string) => void;
  onEnterGroup: (groupId: string) => void;
}) {
  const { t } = useI18n();
  const surfaceRef = useRef<HTMLDivElement | null>(null);
  const flowRef = useRef<ReactFlowInstance<GraphCanvasNode, GraphCanvasEdge> | null>(null);
  const viewportInitTimerRef = useRef<number | null>(null);
  const [surfaceSize, setSurfaceSize] = useState(() => ({
    width: typeof window === "undefined" ? 1440 : window.innerWidth,
    height: typeof window === "undefined" ? 900 : window.innerHeight,
  }));
  const [snapToGrid, setSnapToGrid] = useState(false);
  const publishSelection = useCallback((nodeIds: string[]) => {
    if (
      selectedNodeIds.length !== nodeIds.length
      || selectedNodeIds.some((nodeId, index) => nodeId !== nodeIds[index])
    ) {
      onSelect(nodeIds);
    }
  }, [onSelect, selectedNodeIds]);
  const selectNodeFromPointer = useCallback((nodeId: string, event: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }) => {
    const toggle = shouldToggleWorkflowCanvasNodeSelection({
      compact,
      mode: mobileInteractionMode,
      ctrlKey: event.ctrlKey,
      metaKey: event.metaKey,
      shiftKey: event.shiftKey,
    });
    publishSelection(selectWorkflowCanvasNode(selectedNodeIds, nodeId, toggle));
  }, [compact, mobileInteractionMode, publishSelection, selectedNodeIds]);

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

  useEffect(() => () => {
    if (viewportInitTimerRef.current !== null) {
      window.clearTimeout(viewportInitTimerRef.current);
      viewportInitTimerRef.current = null;
    }
    flowRef.current = null;
  }, []);

  const interactionPolicy = deriveWorkflowCanvasInteractionPolicy({
    compact,
    mode: mobileInteractionMode,
    locked: busy,
    connectionEditing: true,
  });
  const displayGraph = useMemo(() => overlayGraphProposal(graph), [graph]);
  const proposalNodeStates = useMemo(() => graphProposalNodeStates(graph.pending_proposal), [graph.pending_proposal]);
  const proposalEdgeStates = useMemo(() => graphProposalEdgeStates(graph.pending_proposal), [graph.pending_proposal]);
  const graphIdentity = `${graph.id}:${graph.revision}:${enteredGroupId ?? "graph"}:${canvasSyncVersion}:${graph.pending_proposal?.id ?? "none"}`;
  const viewGraph = useMemo(() => graphCanvasView(displayGraph, enteredGroupId), [displayGraph, enteredGroupId]);
  const graphNodes = useMemo<GraphCanvasNode[]>(() => {
    const groups = viewGraph.groups.flatMap((group) => {
      const bounds = computeGraphGroupBounds(viewGraph, group);
      if (!bounds) return [];
      return [{
        id: `group:${group.id}`,
        type: "graph-group" as const,
        position: { x: bounds.x, y: bounds.y },
        width: bounds.width,
        height: bounds.height,
        zIndex: -1,
        selectable: true,
        connectable: false,
        data: {
          kind: "group" as const,
          group,
          bounds,
          structureBusy: busy,
          onEnter: onEnterGroup,
          onRename: onRenameGroup,
          onDissolve: onDissolveGroup,
          onRunShot,
        },
      }];
    });
    const nodes = viewGraph.nodes.map((node) => ({
      id: node.id,
      type: "graph-node" as const,
      position: { x: node.position_x, y: node.position_y },
      width: GRAPH_NODE_WIDTH,
      className: "!h-auto",
      style: { width: GRAPH_NODE_WIDTH, height: "auto" },
      selected: selectedNodeIds.includes(node.id),
      data: {
        kind: "node" as const,
        node,
        status: nodePresentations[node.id]?.status ?? nodeStatuses[node.id] ?? "idle",
        failureReason: nodePresentations[node.id]?.failureReason ?? null,
        lastRunAt: nodePresentations[node.id]?.lastRunAt ?? null,
        retryable: nodePresentations[node.id]?.retryable ?? false,
        runBusy: runningNodeId === node.id,
        structureBusy: busy,
        onRun: (item: GraphNode) => onRunNode(item.id),
        onRunToNode: (item: GraphNode) => onRunToNode?.(item.id),
        onBind: (item: GraphNode) => onBindNode(item.id),
        onDuplicate: (item: GraphNode) => onDuplicateNode([item.id]),
        onSaveRecipe: (item: GraphNode) => onSaveRecipeNode?.(item.id),
        onDelete: (item: GraphNode) => onDeleteNode(item.id),
        onSelectNode: selectNodeFromPointer,
        graph: displayGraph,
        catalog,
        proposalState: proposalNodeStates[node.id] ?? null,
      },
    }));
    return [...groups, ...nodes];
  }, [busy, catalog, displayGraph, nodePresentations, nodeStatuses, onBindNode, onDeleteNode, onDissolveGroup, onDuplicateNode, onEnterGroup, onRenameGroup, onRunNode, onRunToNode, onSaveRecipeNode, proposalNodeStates, runningNodeId, selectNodeFromPointer, selectedNodeIds, viewGraph]);
  const selectedNodeIdSet = useMemo(() => new Set(selectedNodeIds), [selectedNodeIds]);
  const graphEdges = useMemo<GraphCanvasEdge[]>(
    () => viewGraph.edges.map((edge) => ({
      id: edge.id,
      source: edge.source_node_id,
      target: edge.target_node_id,
      sourceHandle: "output",
      targetHandle: "input",
      type: "graph-edge" as const,
      data: {
        role: edge.role,
        roleLabel: (() => {
          const key = graphEdgeRoleLabelKey(edge.role);
          return key ? t(key) : null;
        })(),
        structureBusy: busy,
        deleteLabel: t("detail.deleteEdge"),
        emphasis: graphEdgeEmphasis({
          edgeSelected: false,
          sourceSelected: selectedNodeIdSet.has(edge.source_node_id),
          targetSelected: selectedNodeIdSet.has(edge.target_node_id),
        }),
        onDelete: onDeleteEdge,
        proposalState: proposalEdgeStates[edge.id] ?? null,
      },
    })),
    [busy, onDeleteEdge, selectedNodeIdSet, t, viewGraph.edges],
  );
  const [nodes, setNodes, onNodesChange] = useNodesState<GraphCanvasNode>(graphNodes);
  const previousIdentityRef = useRef(graphIdentity);
  const selectionBoxNodeIdsRef = useRef<string[] | null>(null);
  const dragStartRef = useRef(new Map<string, { x: number; y: number }>());

  useEffect(() => {
    const identityChanged = previousIdentityRef.current !== graphIdentity;
    previousIdentityRef.current = graphIdentity;
    setNodes((current) => {
      if (identityChanged) return graphNodes;
      const currentById = new Map(current.map((node) => [node.id, node]));
      return graphNodes.map((node) => {
        const previous = currentById.get(node.id);
        return previous ? { ...node, position: previous.position } : node;
      });
    });
  }, [graphIdentity, graphNodes, setNodes]);

  const handleNodeDragStart = useCallback<OnNodeDrag<GraphCanvasNode>>((_event, activeNode, selectedNodes) => {
    const dragged = selectedNodes.length ? selectedNodes : [activeNode];
    dragStartRef.current = new Map(dragged.map((node) => [node.id, { ...node.position }]));
  }, []);

  const handleNodeDragStop = useCallback<OnNodeDrag<GraphCanvasNode>>((_event, activeNode, selectedNodes) => {
    const start = dragStartRef.current;
    dragStartRef.current = new Map();
    if (activeNode.data.kind === "group") {
      const origin = start.get(activeNode.id);
      if (!origin) return;
      const deltaX = Math.round(activeNode.position.x - origin.x);
      const deltaY = Math.round(activeNode.position.y - origin.y);
      if (deltaX || deltaY) onTranslateGroup(activeNode.data.group.id, deltaX, deltaY);
      return;
    }
    const dragged = (selectedNodes.length ? selectedNodes : [activeNode]).filter(isRealNode);
    const positions = dragged
      .map((node) => ({
        node_id: node.data.node.id,
        position_x: Math.round(node.position.x),
        position_y: Math.round(node.position.y),
      }))
      .filter((position) => {
        const source = graph.nodes.find((node) => node.id === position.node_id);
        return source && (source.position_x !== position.position_x || source.position_y !== position.position_y);
      });
    if (positions.length) onMove(positions);
  }, [graph.nodes, onMove, onTranslateGroup]);

  const restoredViewport = isWorkflowCanvasViewportCompatible(viewport, surfaceSize.width) ? viewport : null;
  const fitMinZoom = workflowCanvasFitMinZoom(surfaceSize.width);
  const fitViewOptions = useMemo(() => ({
    ...CONTROL_FIT_VIEW_OPTIONS,
    minZoom: fitMinZoom,
  }), [fitMinZoom]);
  const persistViewport = useCallback((nextViewport: Viewport, groupId = enteredGroupId) => {
    onViewportChange({
      ...nextViewport,
      surface_width: surfaceSize.width,
      surface_height: surfaceSize.height,
    }, groupId);
  }, [enteredGroupId, onViewportChange, surfaceSize.height, surfaceSize.width]);
  const handleSelectionChange = useCallback<OnSelectionChangeFunc<GraphCanvasNode, GraphCanvasEdge>>(
    ({ nodes: selectedNodes }) => {
      if (selectionBoxNodeIdsRef.current !== null) {
        selectionBoxNodeIdsRef.current = selectedNodes.filter(isRealNode).map((node) => node.data.node.id);
      }
    },
    [],
  );
  const handleNodeClick = useCallback<NodeMouseHandler<GraphCanvasNode>>((event, node) => {
    if (!isRealNode(node)) return;
    selectNodeFromPointer(node.data.node.id, event);
  }, [selectNodeFromPointer]);
  const handleNodeDoubleClick = useCallback<NodeMouseHandler<GraphCanvasNode>>((event, node) => {
    if (busy || node.data.kind !== "group") return;
    const target = event.target as HTMLElement | null;
    if (target?.closest("input, button")) return;
    event.preventDefault();
    onEnterGroup(node.data.group.id);
  }, [busy, onEnterGroup]);
  const handleConnect = useCallback((connection: Connection) => {
    if (connection.source && connection.target && isGraphConnectionValid(graph, connection.source, connection.target, catalog)) {
      onConnect(connection.source, connection.target);
    }
  }, [catalog, graph, onConnect]);
  const isValidConnection = useCallback<IsValidConnection<GraphCanvasEdge>>(
    (connection) => Boolean(
      connection.source
      && connection.target
      && isGraphConnectionValid(graph, connection.source, connection.target, catalog),
    ),
    [catalog, graph],
  );

  const handleAssetDragOver = useCallback((event: DragEvent<HTMLDivElement>) => {
    if (busy) {
      event.dataTransfer.dropEffect = "none";
      return;
    }
    if (![...event.dataTransfer.types].includes(IMAGE_EXPLORER_DRAG_MIME)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  }, [busy]);
  const handleAssetDrop = useCallback((event: DragEvent<HTMLDivElement>) => {
    if (busy || !onAssetDrop || !flowRef.current) return;
    const assetIds = decodeAssetDragPayload(event.dataTransfer.getData(IMAGE_EXPLORER_DRAG_MIME));
    if (!assetIds.length) return;
    event.preventDefault();
    const position = flowRef.current.screenToFlowPosition({ x: event.clientX, y: event.clientY });
    const hitId = document.elementFromPoint(event.clientX, event.clientY)?.closest(".react-flow__node")?.getAttribute("data-id");
    const nodeId = hitId && viewGraph.nodes.some((node) => node.id === hitId) ? hitId : null;
    onAssetDrop({ assetIds, position, nodeId, groupId: enteredGroupId });
  }, [busy, enteredGroupId, onAssetDrop, viewGraph.nodes]);

  return (
    <div
      ref={surfaceRef}
      className="absolute inset-0 min-h-0 w-full overflow-hidden"
      aria-label={t("graph.canvas.ariaLabel")}
      data-graph-entered-group={enteredGroupId ?? undefined}
      onDragOver={handleAssetDragOver}
      onDrop={handleAssetDrop}
    >
      {compact && onMobileInteractionModeChange ? (
        <div className="absolute left-3 right-3 top-3 z-20 lg:hidden">
          <WorkflowCanvasMobileModeTabs
            value={mobileInteractionMode}
            items={[
              { key: "browse", label: t("detail.mobileCanvasBrowse"), description: t("detail.mobileCanvasBrowseHint"), icon: <Hand size={14} aria-hidden="true" /> },
              { key: "edit", label: t("detail.mobileCanvasEdit"), description: t("detail.mobileCanvasEditHint"), icon: <Pencil size={14} aria-hidden="true" /> },
              { key: "select", label: t("detail.mobileCanvasSelect"), description: t("detail.mobileCanvasSelectHint"), icon: <MousePointer2 size={14} aria-hidden="true" /> },
            ]}
            onChange={onMobileInteractionModeChange}
          />
        </div>
      ) : null}
      <ReactFlow<GraphCanvasNode, GraphCanvasEdge>
        key={`${graph.id}:${enteredGroupId ?? "graph"}:${restoredViewport ? "restore" : "fit"}`}
        onInit={(instance) => {
          flowRef.current = instance;
          if (viewportInitTimerRef.current !== null) {
            window.clearTimeout(viewportInitTimerRef.current);
          }
          const initialGroupId = enteredGroupId;
          viewportInitTimerRef.current = window.setTimeout(() => {
            viewportInitTimerRef.current = null;
            if (flowRef.current !== instance) return;
            persistViewport(instance.getViewport(), initialGroupId);
          }, restoredViewport ? 0 : 220);
        }}
        nodes={nodes}
        edges={graphEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onNodeDragStart={handleNodeDragStart}
        onNodeDragStop={handleNodeDragStop}
        onNodeClick={handleNodeClick}
        onNodeDoubleClick={handleNodeDoubleClick}
        onSelectionChange={handleSelectionChange}
        onSelectionStart={() => {
          selectionBoxNodeIdsRef.current = interactionPolicy.canSelectByBox ? [] : null;
        }}
        onSelectionEnd={() => {
          const nodeIds = selectionBoxNodeIdsRef.current;
          selectionBoxNodeIdsRef.current = null;
          if (nodeIds !== null) publishSelection(nodeIds);
        }}
        onPaneClick={() => publishSelection([])}
        onMoveEnd={((_event, nextViewport) => persistViewport(nextViewport)) as OnMoveEnd}
        defaultViewport={restoredViewport ?? undefined}
        fitView={!restoredViewport}
        fitViewOptions={fitViewOptions}
        minZoom={0.12}
        maxZoom={2}
        nodesDraggable={interactionPolicy.nodesDraggable}
        nodesConnectable={interactionPolicy.nodesConnectable}
        edgesReconnectable={false}
        elementsSelectable
        selectNodesOnDrag={interactionPolicy.selectNodesOnDrag}
        snapToGrid={snapToGrid}
        snapGrid={SNAP_GRID}
        selectionMode={SelectionMode.Partial}
        selectionOnDrag={interactionPolicy.selectionOnDrag}
        selectionKeyCode={interactionPolicy.selectionKeyCode}
        multiSelectionKeyCode={interactionPolicy.multiSelectionKeyCode}
        panOnDrag={interactionPolicy.panOnDrag}
        panActivationKeyCode={WORKFLOW_CANVAS_PAN_ACTIVATION_KEY_CODE}
        zoomActivationKeyCode={WORKFLOW_CANVAS_ZOOM_ACTIVATION_KEY_CODES}
        nodeDragThreshold={interactionPolicy.nodeDragThreshold}
        nodeClickDistance={interactionPolicy.nodeClickDistance}
        noWheelClassName="nowheel"
        noDragClassName="nodrag"
        noPanClassName="nopan"
        deleteKeyCode={null}
        connectOnClick={false}
        connectionMode={ConnectionMode.Strict}
        connectionLineType={ConnectionLineType.Bezier}
        connectionLineStyle={{ stroke: "#64748b", strokeWidth: 2, strokeDasharray: "6 4" }}
        autoPanOnConnect
        onConnect={handleConnect}
        isValidConnection={isValidConnection}
        className="bg-transparent"
        proOptions={PRO_OPTIONS}
      >
        <WorkflowCanvasGrid gap={24} />
        <WorkflowCanvasControls
          labels={{
            resetZoom: t("detail.resetZoom"),
            fitSelection: t("detail.fitSelection"),
            controls: t("detail.canvasControls"),
            snapToGrid: t("detail.snapToGrid"),
            autoLayout: t("detail.autoLayout"),
          }}
          selectedNodeIds={selectedNodeIds}
          snapToGrid={snapToGrid}
          autoLayoutBusy={busy}
          fitViewOptions={fitViewOptions}
          onToggleSnapToGrid={() => setSnapToGrid((current) => !current)}
          onAutoLayout={onAutoLayout}
          onViewportCommit={persistViewport}
          normalizeZoom={(zoom) => Math.min(2, Math.max(0.12, zoom))}
        />
        <MiniMap
          position="bottom-right"
          aria-label={t("detail.canvasMiniMap")}
          className="workflow-canvas-minimap nopan nodrag nowheel hidden lg:block"
          nodeColor={(node) => node.data?.kind === "group" ? "#94a3b8" : "#a5b4fc"}
          nodeBorderRadius={8}
          nodeStrokeWidth={3}
          pannable
          zoomable
          offsetScale={8}
        />
      </ReactFlow>
    </div>
  );
}

