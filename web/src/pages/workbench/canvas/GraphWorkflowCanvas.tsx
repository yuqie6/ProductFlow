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
  FitViewOptions,
  IsValidConnection,
  Node,
  NodeProps,
  NodeMouseHandler,
  OnConnectEnd,
  OnReconnect,
  OnMoveEnd,
  OnNodeDrag,
  OnSelectionChangeFunc,
  ReactFlowInstance,
  Viewport,
} from "@xyflow/react";
import { BookmarkPlus, ChevronsRight, CopyPlus, Folder, FolderOpen, FolderPlus, Focus, Hand, Link2, Loader2, Lock, MousePointer2, Pencil, Pin, Play, Trash2, Ungroup } from "lucide-react";
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { DragEvent, MouseEvent as ReactMouseEvent } from "react";

import { IconButton } from "../../../components/ui/icon-button";
import { api } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { sanitizeFilenamePart } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type { GraphGroup, GraphNode, GraphNodeCatalog, GraphPlannedAction, GraphProjection, GraphRunSubmitInput, WorkflowNodeDisplayStatus, WorkflowNodeStatus } from "../../../lib/types";
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
  graphConnectionInvalidReason,
  graphDataTypeLabelKey,
  graphEdgeRoleLabelKey,
  graphInputPorts,
  graphNodePresentationKind,
  graphPortDataTypeClass,
  graphPortVisualState,
  isGraphConnectionValid,
  isProcessingNode,
  missingRequiredRunNodes,
  missingRequiredRunRoles,
  missingRunNodesSummary,
} from "./graphCatalog";
import { graphProgressPhaseLabelKey, type GraphNodeRunPresentation } from "./graphRunDisplay";
import { plannedActionClassName, runPreviewPointerHandlers } from "./graphRunPreview";
import { shotRunRequest } from "./shotChangeSet";
import {
  GRAPH_NODE_WIDTH,
  GRAPH_SNAP,
  computeGraphGroupBounds,
  graphCanvasView,
  graphNodeHasPinnableOutput,
} from "./graphLayout";
import { graphEdgeDeleteClassName, graphEdgeEmphasis, graphPortVisualScale } from "./graphCanvasVisual";

const SNAP_GRID: [number, number] = [GRAPH_SNAP, GRAPH_SNAP];
const PRO_OPTIONS = { hideAttribution: true };
const CONTROL_FIT_VIEW_OPTIONS = { padding: 0.22, duration: 180, maxZoom: 1.05 };

export function graphCanvasFitPadding(basePadding: number, bottomInset: number): FitViewOptions["padding"] {
  if (bottomInset <= 0) return basePadding;
  return {
    top: basePadding,
    right: basePadding,
    bottom: `${bottomInset + 24}px`,
    left: basePadding,
  };
}

export function rejectedGraphConnectionNotice(
  graph: GraphProjection,
  state: {
    isValid: boolean | null;
    fromNodeId: string | null;
    toNodeId: string | null;
    toHandleId?: string | null;
  },
  catalog: GraphNodeCatalog | null | undefined,
): ReturnType<typeof graphConnectionInvalidReason> {
  if (state.isValid) return null;
  if (!state.fromNodeId || !state.toNodeId) return null;
  if (state.toHandleId === null) return null;
  return graphConnectionInvalidReason(graph, state.fromNodeId, state.toNodeId, catalog, state.toHandleId);
}

type ConnectionHandleSnapshot = {
  inProgress: boolean;
  fromHandle: { nodeId: string; type: "source" | "target" } | null;
};

interface GraphNodeData extends Record<string, unknown> {
  kind: "node";
  node: GraphNode;
  status: WorkflowNodeDisplayStatus;
  failureReason: string | null;
  lastRunAt: string | null;
  retryable: boolean;
  runBusy: boolean;
  runDisabled: boolean;
  structureBusy: boolean;
  onRun: (node: GraphNode) => void;
  onRunToNode: (node: GraphNode) => void;
  onRunShot?: (groupId: string) => void;
  onBind: (node: GraphNode) => void;
  onPin?: (node: GraphNode) => void;
  onDuplicate: (node: GraphNode) => void;
  onSaveRecipe: (node: GraphNode) => void;
  onDelete: (node: GraphNode) => void;
  onDuplicateSelection?: () => void;
  onGroupSelection?: () => void;
  onSaveSelection?: () => void;
  onDeleteSelection?: () => void;
  selectedCount: number;
  selectionPrimary: boolean;
  missingRunLabels: string[];
  plannedAction?: GraphPlannedAction | null;
  latestPlannedAction?: GraphPlannedAction | null;
  progressPhase?: string | null;
  elapsedLabel?: string | null;
  attemptCount?: number;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
  onSelectNode: (nodeId: string, event: ReactMouseEvent<HTMLElement>) => void;
  graph: GraphProjection;
  catalog: GraphNodeCatalog | null;
  proposalState?: GraphProposalState | null;
}

interface GraphGroupData extends Record<string, unknown> {
  kind: "group";
  group: GraphGroup;
  bounds: { x: number; y: number; width: number; height: number };
  runDisabled: boolean;
  runBlocked: boolean;
  runBlockedReason?: string;
  structureBusy: boolean;
  onEnter: (groupId: string) => void;
  onRename: (groupId: string, title: string) => void;
  onDissolve: (groupId: string) => void;
  onRunShot?: (groupId: string) => void;
  shotRunInput?: GraphRunSubmitInput | null;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
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

function nodeStatusLabel(status: WorkflowNodeDisplayStatus, t: ReturnType<typeof useI18n>["t"]): string {
  const keys = {
    idle: "detail.nodeStatus.idle",
    queued: "detail.nodeStatus.queued",
    running: "detail.nodeStatus.running",
    succeeded: "detail.nodeStatus.succeeded",
    failed: "detail.nodeStatus.failed",
    cancelled: "detail.nodeStatus.cancelled",
    skipped: "detail.nodeStatus.skipped",
    unknown: "detail.nodeStatus.unknown",
  } as const;
  return t(keys[status]);
}

function nodeImage(node: GraphNode): DownloadableImage | null {
  const assetId = node.preview_asset_id ?? node.bound_asset_id;
  if (!assetId) return null;
  return {
    previewUrl: api.getProductImageAssetMediaUrl(assetId, "thumbnail"),
    downloadUrl: api.getProductImageAssetMediaUrl(assetId),
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
  const inputPorts = graphInputPorts(data.catalog, node.node_type);
  const hasInput = inputPorts.length > 0;
  const running = data.status === "queued" || data.status === "running";
  const runBlocked = data.missingRunLabels.length > 0;
  const connectionSnapshot = {
    inProgress: connection.inProgress,
    fromNodeId: connection.fromHandle?.nodeId ?? null,
    fromType: connection.fromHandle?.type ?? null,
  };

  useLayoutEffect(() => {
    updateNodeInternals(id);
  }, [id, hasInput, inputPorts.length, node.preview_asset_id, node.title, running, updateNodeInternals]);

  const sourceState = graphPortVisualState(
    data.graph,
    node.id,
    "source",
    connectionSnapshot,
    data.catalog,
  );

  const proposalState = data.proposalState ?? null;
  const multi = data.selectedCount >= 2 && data.selectionPrimary;
  const plannedClass = plannedActionClassName(data.plannedAction);
  const displayPlannedAction = data.plannedAction ?? data.latestPlannedAction ?? null;
  return (
    <div
      className={`relative w-[248px] overflow-visible ${proposalState === "deleted"
        ? "opacity-40"
        : proposalState === "added"
          ? "opacity-90 outline-dashed outline-2 outline-accent/80"
          : proposalState === "changed"
            ? "outline outline-2 outline-state-warning/80"
            : plannedClass
        }`}
      data-graph-proposal-state={proposalState ?? undefined}
      data-graph-planned-action={data.plannedAction ?? undefined}
    >
      {multi ? (
        <div
          data-graph-selection-actions
          data-node-action
          className="nodrag nopan nowheel absolute -top-12 left-1/2 z-dropdown flex -translate-x-1/2 items-center gap-1 rounded-panel border border-border-l1 bg-surface-raised/98 p-1 shadow-elev-2"
        >
          <WorkflowCanvasNodeToolbarButton
            label={t("detail.duplicate")}
            disabled={data.structureBusy || running}
            onClick={() => data.onDuplicateSelection?.()}
          >
            <CopyPlus size={16} aria-hidden="true" />
          </WorkflowCanvasNodeToolbarButton>
          <WorkflowCanvasNodeToolbarButton
            label={t("graph.palette.group")}
            disabled={data.structureBusy || running}
            onClick={() => data.onGroupSelection?.()}
          >
            <FolderPlus size={16} aria-hidden="true" />
          </WorkflowCanvasNodeToolbarButton>
          <WorkflowCanvasNodeToolbarButton
            label={t("graph.canvas.saveRecipe")}
            disabled={data.structureBusy || running}
            onClick={() => data.onSaveSelection?.()}
          >
            <BookmarkPlus size={16} aria-hidden="true" />
          </WorkflowCanvasNodeToolbarButton>
          <WorkflowCanvasNodeToolbarButton
            label={t("graph.canvas.delete")}
            disabled={data.structureBusy || running}
            destructive
            onClick={() => data.onDeleteSelection?.()}
          >
            <Trash2 size={16} aria-hidden="true" />
          </WorkflowCanvasNodeToolbarButton>
        </div>
      ) : (
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
          {graphNodeHasPinnableOutput(node) && data.onPin ? (
            <WorkflowCanvasNodeToolbarButton
              label={t("graph.canvas.pinAsset")}
              disabled={data.structureBusy}
              onClick={() => data.onPin?.(node)}
            >
              <Pin size={16} aria-hidden="true" />
            </WorkflowCanvasNodeToolbarButton>
          ) : null}
          {isProcessingNode(node, data.catalog) && data.proposalState !== "added" ? (
            <>
              <WorkflowCanvasNodeToolbarButton
                label={runBlocked ? data.missingRunLabels.join(" · ") : t("graph.canvas.runNode")}
                disabled={data.runBusy || data.runDisabled || running || data.structureBusy || runBlocked}
                onClick={() => data.onRun(node)}
                {...runPreviewPointerHandlers(
                  { scope: "node", node_id: node.id },
                  data.onPreviewRun,
                  data.onHideRunPreview,
                )}
              >
                {data.runBusy || running ? <Loader2 size={16} className="animate-spin motion-reduce:animate-none" aria-hidden="true" /> : <Play size={16} aria-hidden="true" />}
              </WorkflowCanvasNodeToolbarButton>
              <WorkflowCanvasNodeToolbarButton
                label={runBlocked ? data.missingRunLabels.join(" · ") : t("graph.runs.scope.toNode")}
                disabled={data.runBusy || data.runDisabled || running || data.structureBusy || runBlocked}
                onClick={() => data.onRunToNode(node)}
                {...runPreviewPointerHandlers(
                  { scope: "to_node", node_id: node.id },
                  data.onPreviewRun,
                  data.onHideRunPreview,
                )}
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
      )}
      {inputPorts.map((port, index) => {
        const occupancy = node.incoming.filter((edge) => edge.role === port.role).length;
        const maxLabel = port.max_count == null ? t("graph.port.unbounded") : String(port.max_count);
        const roleKey = graphEdgeRoleLabelKey(port.role);
        const typeKey = graphDataTypeLabelKey(port.data_type);
        const titleParts = [
          roleKey ? t(roleKey) : t("graph.edgeRole.unknown"),
          typeKey ? t("graph.port.accepts", { type: t(typeKey) }) : null,
          t("graph.port.occupancy", { count: occupancy, max: maxLabel }),
          port.required_to_run ? t("graph.port.required") : null,
        ].filter(Boolean);
        const top = inputPorts.length === 1 ? "50%" : `${((index + 1) / (inputPorts.length + 1)) * 100}%`;
        return (
          <WorkflowCanvasNodePort
            key={port.role}
            id={port.role}
            type="target"
            top={top}
            label={titleParts.join(" · ")}
            connectable={Boolean(isConnectable)}
            visualState={graphPortVisualState(
              data.graph,
              node.id,
              "target",
              connectionSnapshot,
              data.catalog,
              port.role,
            )}
            visualScale={portVisualScale}
            colorClass={graphPortDataTypeClass(port.data_type)}
          />
        );
      })}
      <WorkflowCanvasNodePort
        id="output"
        type="source"
        top="50%"
        label={t("detail.outputHandle")}
        connectable={Boolean(isConnectable)}
        visualState={sourceState}
        visualScale={portVisualScale}
        colorClass={graphPortDataTypeClass(
          data.catalog?.nodes.find((item) => item.node_type === node.node_type)?.output_data_type ?? "output",
        )}
      />
      <WorkflowNodePresentationCard
        id={node.id}
        kind={graphNodePresentationKind(node.node_type)}
        title={node.title}
        label={nodeTypeLabel(node.node_type, t)}
        status={
          displayPlannedAction === "frozen" && (data.status === "idle" || data.status === "skipped")
            ? "frozen"
            : data.status
        }
        statusLabel={
          node.node_type === "image_asset" && !node.bound_asset_id
            ? t("graph.inspector.unbound")
            : data.status === "skipped"
              ? (displayPlannedAction === "frozen" ? t("graph.node.skippedFrozen") : t("graph.node.skippedReuse"))
              : data.status !== "idle"
                ? nodeStatusLabel(data.status, t)
                : node.unused
                  ? t("graph.inspector.unused")
                  : nodeStatusLabel(data.status, t)}
        image={nodeImage(node)}
        imageWaiting={node.node_type === "image_generation" && running}
        waitingLabel={graphProgressPhaseLabelKey(data.progressPhase)
          ? t(graphProgressPhaseLabelKey(data.progressPhase)!)
          : nodeStatusLabel(data.status, t)}
        activityText={running
          ? [
            graphProgressPhaseLabelKey(data.progressPhase)
              ? t(graphProgressPhaseLabelKey(data.progressPhase)!)
              : nodeStatusLabel(data.status, t),
            data.elapsedLabel,
          ].filter(Boolean).join(" · ")
          : data.elapsedLabel && data.status !== "idle"
            ? data.elapsedLabel
          : null}
        failureReason={data.failureReason}
        blockedReason={runBlocked ? data.missingRunLabels.join(" · ") : null}
        lastRunAt={data.lastRunAt}
        retryable={data.retryable}
        attemptCount={data.attemptCount ?? 0}
        primarySelected={selected}
        dragging={dragging}
        onSelect={(event) => {
          event.stopPropagation();
          data.onSelectNode(node.id, event);
        }}
      />
      {displayPlannedAction === "frozen" ? (
        <span className="pointer-events-none absolute left-2 top-2 z-30 rounded-full bg-state-frozen p-1 text-surface-raised" aria-hidden="true">
          <Lock size={10} />
        </span>
      ) : null}
      {data.missingRunLabels.length ? (
        <div
          data-graph-missing-run-input
          className="absolute -right-1 -top-1 z-30 max-w-[11rem] rounded-full bg-state-error px-1.5 py-0.5 text-[9px] font-semibold leading-4 text-white shadow-elev-1"
        >
          {data.missingRunLabels.join(" · ")}
        </div>
      ) : null}
    </div>
  );
});

export const GraphGroupCard = memo(function GraphGroupCard({
  data,
  selected,
}: NodeProps<Node<GraphGroupData>>) {
  const { t } = useI18n();
  const { group, bounds, runDisabled, runBlocked, runBlockedReason, structureBusy, onEnter, onRename, onDissolve, onRunShot, shotRunInput, onPreviewRun, onHideRunPreview } = data;
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
      className={`group relative rounded-surface border-2 border-dashed transition-[border-color,background-color,box-shadow] duration-fast pointer-events-none ${selected
        ? "border-border-l3 bg-surface-subtle/40"
        : "border-border-l1 bg-surface-subtle/30"
        }`}
      data-graph-group-id={group.id}
    >
      <div
        className="pointer-events-auto flex items-center gap-2 rounded-t-surface border-b border-dashed border-border-l1 bg-surface-raised/70 px-3.5 py-2 backdrop-blur-sm"
        onDoubleClick={(event) => {
          if (structureBusy) return;
          if ((event.target as HTMLElement).closest("input, button")) return;
          event.stopPropagation();
          onEnter(group.id);
        }}
      >
        <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-control bg-surface-subtle text-text-secondary">
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
            className="nodrag nowheel nopan min-w-0 flex-1 rounded-control border border-border-l1 bg-surface-raised px-2 py-1 text-xs font-bold text-text-primary outline-none focus:border-accent focus:ring-2 focus:ring-focus-ring"
          />
        ) : (
          <span className="min-w-0 flex-1 truncate text-xs font-bold text-text-primary">{group.title}</span>
        )}
        <span className="rounded-full bg-surface-subtle px-2 py-0.5 text-[10px] font-semibold text-text-secondary">
          {t("workbench.folder.memberCount", { count: group.member_ids.length })}
        </span>
        {onRunShot ? (
          <IconButton
            label={runBlocked ? (runBlockedReason ?? t("graph.canvas.runShot")) : t("graph.canvas.runShot")}
            size="toolbar"
            className="nodrag nowheel nopan"
            disabled={structureBusy || runDisabled || runBlocked}
            data-run-shot=""
            {...runPreviewPointerHandlers(shotRunInput, onPreviewRun, onHideRunPreview)}
            onClick={(event) => {
              event.stopPropagation();
              onRunShot(group.id);
            }}
          >
            <Play size={12} aria-hidden="true" />
          </IconButton>
        ) : null}
        <IconButton
          label={t("graph.canvas.enterGroup")}
          size="toolbar"
          className="nodrag nowheel nopan"
          disabled={structureBusy}
          data-enter-group=""
          onClick={(event) => {
            event.stopPropagation();
            onEnter(group.id);
          }}
        >
          <FolderOpen size={12} aria-hidden="true" />
        </IconButton>
        <IconButton
          label={t("graph.canvas.renameGroup")}
          size="toolbar"
          className="nodrag nowheel nopan"
          disabled={structureBusy}
          onClick={(event) => {
            event.stopPropagation();
            setEditing(true);
          }}
        >
          <Pencil size={12} aria-hidden="true" />
        </IconButton>
        <IconButton
          label={t("graph.palette.dissolve")}
          size="toolbar"
          variant="danger"
          className="nodrag nowheel nopan"
          disabled={structureBusy}
          onClick={(event) => {
            event.stopPropagation();
            onDissolve(group.id);
          }}
        >
          <Ungroup size={12} aria-hidden="true" />
        </IconButton>
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
  const connectionInProgress = useConnection((connection) => connection.inProgress);
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
            ? "var(--color-edge-proposal)"
            : data?.proposalState === "deleted"
              ? "var(--color-edge-receded)"
              : emphasis === "active" ? (selected ? "var(--color-edge-selected)" : "var(--color-edge-active)") : "var(--color-edge-receded)",
          strokeWidth: emphasis === "active" ? (selected ? 2.2 : 1.6) : 1.1,
          strokeDasharray: data?.proposalState === "added" || data?.proposalState === "deleted" ? "6 4" : undefined,
          opacity: data?.proposalState === "deleted" ? 0.45 : 1,
        }}
        data-graph-proposal-state={data?.proposalState ?? undefined}
        label={(hovered || selected) ? data?.roleLabel ?? undefined : undefined}
        labelStyle={{ fontSize: 10, fill: "var(--color-text-muted)" }}
      />
      <path
        d={edgePath}
        fill="none"
        stroke="transparent"
        strokeWidth={15}
        data-edge-emphasis={emphasis}
        className={selected ? "pointer-events-none" : "cursor-pointer"}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      />
      {!connectionInProgress ? (
        <EdgeToolbar
          edgeId={id}
          x={labelX}
          y={labelY}
          isVisible
          className={`nodrag nowheel nopan transition-[opacity,visibility,transform] duration-fast ${graphEdgeDeleteClassName(Boolean(selected))}`}
          onMouseEnter={() => setHovered(true)}
          onMouseLeave={() => setHovered(false)}
        >
          <IconButton
            label={[data?.roleLabel, data?.deleteLabel].filter(Boolean).join(" · ")}
            size="toolbar"
            variant="danger"
            className="nodrag nowheel nopan"
            disabled={data?.structureBusy}
            onClick={(event) => {
              event.stopPropagation();
              if (!data?.structureBusy) data?.onDelete(id);
            }}
          >
            <Trash2 size={13} strokeWidth={2.2} aria-hidden="true" />
          </IconButton>
        </EdgeToolbar>
      ) : null}
    </>
  );
});

const nodeTypes = { "graph-node": GraphNodeCard, "graph-group": GraphGroupCard };
const edgeTypes = { "graph-edge": GraphCanvasEdgeCard };

function isRealNode(node: GraphCanvasNode): node is Node<GraphNodeData, "graph-node"> {
  return node.data.kind === "node";
}

export interface GraphCanvasFocusRequest {
  nodeIds: string[];
  version: number;
  padding?: number;
  duration?: number;
}

export function visibleGraphFocusNodeIds(
  requestedNodeIds: readonly string[],
  visibleNodeIds: readonly string[],
): string[] {
  const visible = new Set(visibleNodeIds);
  return [...new Set(requestedNodeIds)].filter((nodeId) => visible.has(nodeId));
}

export function GraphWorkflowCanvas({
  graph,
  catalog,
  selectedNodeIds,
  busy,
  runDisabled = false,
  nodeStatuses,
  nodePresentations = {},
  plannedActions = {},
  runningNodeId,
  viewport,
  onViewportChange,
  mobileInteractionMode = "edit",
  onMobileInteractionModeChange,
  compact = false,
  enteredGroupId = null,
  canvasSyncVersion = 0,
  focusRequest = null,
  bottomInset = 0,
  onSelect,
  onConnect,
  onReconnect,
  onMove,
  onTranslateGroup,
  onDeleteNode,
  onDeleteEdge,
  onRunNode,
  onRunToNode,
  onRunShot,
  onPreviewRun,
  onHideRunPreview,
  onBindNode,
  onPinNode,
  onDuplicateNode,
  onSaveRecipeNode,
  onGroupSelected,
  onDeleteSelected,
  onSaveSelection,
  onConnectionRejected,
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
  runDisabled?: boolean;
  nodeStatuses: Record<string, WorkflowNodeStatus>;
  nodePresentations?: Record<string, GraphNodeRunPresentation>;
  plannedActions?: Record<string, GraphPlannedAction>;
  runningNodeId: string | null;
  viewport: WorkflowCanvasViewport | null;
  onViewportChange: (viewport: WorkflowCanvasViewport, groupId: string | null) => void;
  mobileInteractionMode?: CanvasInteractionMode;
  onMobileInteractionModeChange?: (mode: CanvasInteractionMode) => void;
  compact?: boolean;
  enteredGroupId?: string | null;
  canvasSyncVersion?: number;
  focusRequest?: GraphCanvasFocusRequest | null;
  bottomInset?: number;
  onSelect: (nodeIds: string[]) => void | Promise<void>;
  onConnect: (source: string, target: string) => void;
  onReconnect?: (edgeId: string, source: string, target: string, order: number) => void;
  onMove: (positions: Array<{ node_id: string; position_x: number; position_y: number }>) => void;
  onTranslateGroup: (groupId: string, deltaX: number, deltaY: number) => void;
  onDeleteNode: (nodeId: string) => void;
  onDeleteEdge: (edgeId: string) => void;
  onRunNode: (nodeId: string) => void;
  onRunToNode?: (nodeId: string) => void;
  onRunShot?: (groupId: string) => void;
  onPreviewRun?: (input: GraphRunSubmitInput) => void;
  onHideRunPreview?: () => void;
  onBindNode: (nodeId: string) => void;
  onPinNode?: (nodeId: string) => void;
  onDuplicateNode: (nodeIds: string[]) => void;
  onSaveRecipeNode?: (nodeId: string) => void;
  onGroupSelected?: () => void;
  onDeleteSelected?: () => void;
  onSaveSelection?: () => void;
  onConnectionRejected?: (reasonKey: ReturnType<typeof graphConnectionInvalidReason>) => void;
  onAutoLayout: () => void;
  onAssetDrop?: (input: GraphAssetDropInput) => void;
  onRenameGroup: (groupId: string, title: string) => void;
  onDissolveGroup: (groupId: string) => void;
  onEnterGroup: (groupId: string) => void;
}) {
  const { t } = useI18n();
  const surfaceRef = useRef<HTMLDivElement | null>(null);
  const flowRef = useRef<ReactFlowInstance<GraphCanvasNode, GraphCanvasEdge> | null>(null);
  const handledFocusVersionRef = useRef<number | null>(null);
  const viewportInitTimerRef = useRef<number | null>(null);
  const [surfaceSize, setSurfaceSize] = useState(() => ({
    width: typeof window === "undefined" ? 1440 : window.innerWidth,
    height: typeof window === "undefined" ? 900 : window.innerHeight,
  }));
  const [snapToGrid, setSnapToGrid] = useState(false);
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);

  useEffect(() => {
    if (!focusRequest || handledFocusVersionRef.current === focusRequest.version) return;
    const frame = window.requestAnimationFrame(() => {
      const flow = flowRef.current;
      if (!flow) return;
      const nodes = visibleGraphFocusNodeIds(
        focusRequest.nodeIds,
        flow.getNodes().map((node) => node.id),
      ).map((id) => ({ id }));
      if (!nodes.length) return;
      handledFocusVersionRef.current = focusRequest.version;
      void flow.fitView({
        nodes,
        padding: graphCanvasFitPadding(focusRequest.padding ?? 0.28, bottomInset),
        duration: focusRequest.duration ?? 220,
        maxZoom: 1.05,
      });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [bottomInset, focusRequest]);
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
      const missing = missingRequiredRunNodes(displayGraph, catalog, new Set(group.member_ids));
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
          runDisabled,
          runBlocked: missing.length > 0,
          runBlockedReason: missing.length
            ? missingRunNodesSummary(missing, (role) => {
              const key = graphEdgeRoleLabelKey(role);
              return t("graph.missingRunInput", { role: key ? t(key) : t("graph.edgeRole.unknown") });
            })
            : undefined,
          structureBusy: busy,
          onEnter: onEnterGroup,
          onRename: onRenameGroup,
          onDissolve: onDissolveGroup,
          onRunShot,
          shotRunInput: shotRunRequest(displayGraph, group.id),
          onPreviewRun,
          onHideRunPreview,
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
        runDisabled,
        structureBusy: busy,
        onRun: (item: GraphNode) => onRunNode(item.id),
        onRunToNode: (item: GraphNode) => onRunToNode?.(item.id),
        onBind: (item: GraphNode) => onBindNode(item.id),
        onPin: onPinNode ? (item: GraphNode) => onPinNode(item.id) : undefined,
        onDuplicate: (item: GraphNode) => onDuplicateNode([item.id]),
        onSaveRecipe: (item: GraphNode) => onSaveRecipeNode?.(item.id),
        onDelete: (item: GraphNode) => onDeleteNode(item.id),
        onDuplicateSelection: () => onDuplicateNode(selectedNodeIds),
        onGroupSelection: onGroupSelected,
        onSaveSelection,
        onDeleteSelection: onDeleteSelected,
        selectedCount: selectedNodeIds.length,
        selectionPrimary: selectedNodeIds[0] === node.id,
        missingRunLabels: missingRequiredRunRoles(node, catalog).map((role) => {
          const key = graphEdgeRoleLabelKey(role);
          return t("graph.missingRunInput", { role: key ? t(key) : t("graph.edgeRole.unknown") });
        }),
        plannedAction: plannedActions[node.id] ?? null,
        latestPlannedAction: nodePresentations[node.id]?.lastPlannedAction ?? null,
        progressPhase: nodePresentations[node.id]?.progressPhase ?? null,
        elapsedLabel: nodePresentations[node.id]?.elapsedLabel ?? null,
        attemptCount: nodePresentations[node.id]?.attemptCount ?? 0,
        onPreviewRun,
        onHideRunPreview,
        onSelectNode: selectNodeFromPointer,
        graph: displayGraph,
        catalog,
        proposalState: proposalNodeStates[node.id] ?? null,
      },
    }));
    return [...groups, ...nodes];
  }, [busy, catalog, displayGraph, nodePresentations, nodeStatuses, onBindNode, onDeleteNode, onDeleteSelected, onDissolveGroup, onDuplicateNode, onEnterGroup, onGroupSelected, onHideRunPreview, onPinNode, onPreviewRun, onRenameGroup, onRunNode, onRunShot, onRunToNode, onSaveRecipeNode, onSaveSelection, plannedActions, proposalNodeStates, runDisabled, runningNodeId, selectNodeFromPointer, selectedNodeIds, t, viewGraph]);
  const selectedNodeIdSet = useMemo(() => new Set(selectedNodeIds), [selectedNodeIds]);
  const graphEdges = useMemo<GraphCanvasEdge[]>(
    () => viewGraph.edges.map((edge) => ({
      id: edge.id,
      source: edge.source_node_id,
      target: edge.target_node_id,
      sourceHandle: "output",
      targetHandle: edge.role,
      type: "graph-edge" as const,
      selected: selectedEdgeId === edge.id,
      data: {
        role: edge.role,
        roleLabel: (() => {
          const key = graphEdgeRoleLabelKey(edge.role);
          return key ? t(key) : null;
        })(),
        structureBusy: busy,
        deleteLabel: t("detail.deleteEdge"),
        emphasis: graphEdgeEmphasis({
          edgeSelected: selectedEdgeId === edge.id,
          sourceSelected: selectedNodeIdSet.has(edge.source_node_id),
          targetSelected: selectedNodeIdSet.has(edge.target_node_id),
        }),
        onDelete: onDeleteEdge,
        proposalState: proposalEdgeStates[edge.id] ?? null,
      },
    })),
    [busy, onDeleteEdge, proposalEdgeStates, selectedEdgeId, selectedNodeIdSet, t, viewGraph.edges],
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

  useEffect(() => {
    if (selectedEdgeId && !viewGraph.edges.some((edge) => edge.id === selectedEdgeId)) {
      setSelectedEdgeId(null);
    }
  }, [selectedEdgeId, viewGraph.edges]);

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
    padding: graphCanvasFitPadding(CONTROL_FIT_VIEW_OPTIONS.padding, bottomInset),
    minZoom: fitMinZoom,
  }), [bottomInset, fitMinZoom]);
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
    setSelectedEdgeId(null);
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
    if (
      connection.source
      && connection.target
      && isGraphConnectionValid(graph, connection.source, connection.target, catalog, connection.targetHandle)
    ) {
      onConnect(connection.source, connection.target);
    }
  }, [catalog, graph, onConnect]);
  const handleReconnect = useCallback<OnReconnect<GraphCanvasEdge>>((oldEdge, connection) => {
    if (
      !onReconnect
      || !connection.source
      || !connection.target
      || !isGraphConnectionValid(graph, connection.source, connection.target, catalog, connection.targetHandle, oldEdge.id)
    ) {
      return;
    }
    if (oldEdge.source === connection.source && oldEdge.target === connection.target && oldEdge.targetHandle === connection.targetHandle) {
      return;
    }
    const order = graph.edges.find((edge) => edge.id === oldEdge.id)?.order ?? 0;
    onReconnect(oldEdge.id, connection.source, connection.target, order);
  }, [catalog, graph, onReconnect]);
  const handleConnectEnd = useCallback<OnConnectEnd>((_event, state) => {
    const reason = rejectedGraphConnectionNotice(graph, {
      isValid: state.isValid,
      fromNodeId: state.fromNode?.id ?? null,
      toNodeId: state.toNode?.id ?? null,
      toHandleId: state.toHandle?.id ?? null,
    }, catalog);
    if (reason && reason !== "graph.connect.incompatible") onConnectionRejected?.(reason);
  }, [catalog, graph, onConnectionRejected]);
  const isValidConnection = useCallback<IsValidConnection<GraphCanvasEdge>>(
    (connection) => Boolean(
      connection.source
      && connection.target
      && isGraphConnectionValid(graph, connection.source, connection.target, catalog, connection.targetHandle),
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
        onEdgeClick={(event, edge) => {
          event.stopPropagation();
          publishSelection([]);
          setSelectedEdgeId(edge.id);
        }}
        onSelectionChange={handleSelectionChange}
        onSelectionStart={() => {
          selectionBoxNodeIdsRef.current = interactionPolicy.canSelectByBox ? [] : null;
        }}
        onSelectionEnd={() => {
          const nodeIds = selectionBoxNodeIdsRef.current;
          selectionBoxNodeIdsRef.current = null;
          if (nodeIds !== null) publishSelection(nodeIds);
        }}
        onPaneClick={() => {
          setSelectedEdgeId(null);
          publishSelection([]);
        }}
        onMoveEnd={((_event, nextViewport) => persistViewport(nextViewport)) as OnMoveEnd}
        defaultViewport={restoredViewport ?? undefined}
        fitView={!restoredViewport}
        fitViewOptions={fitViewOptions}
        minZoom={0.12}
        maxZoom={2}
        nodesDraggable={interactionPolicy.nodesDraggable}
        nodesConnectable={interactionPolicy.nodesConnectable}
        edgesReconnectable
        elevateEdgesOnSelect
        onReconnect={handleReconnect}
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
        connectionLineStyle={{ stroke: "var(--color-edge-default)", strokeWidth: 2, strokeDasharray: "6 4" }}
        autoPanOnConnect
        onConnect={handleConnect}
        onConnectEnd={handleConnectEnd}
        isValidConnection={isValidConnection}
        className="bg-transparent"
        proOptions={PRO_OPTIONS}
      >
        <WorkflowCanvasGrid gap={24} />
        <WorkflowCanvasControls
          labels={{
            resetZoom: t("detail.resetZoom"),
            zoomIn: t("detail.zoomIn"),
            zoomOut: t("detail.zoomOut"),
            fitView: t("detail.fitCanvas"),
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
          topInset={compact ? 68 : undefined}
        />
        <MiniMap
          position="bottom-right"
          aria-label={t("detail.canvasMiniMap")}
          className="workflow-canvas-minimap nopan nodrag nowheel hidden lg:block"
          nodeColor={(node) => node.data?.kind === "group" ? "var(--color-canvas-minimap-group)" : "var(--color-canvas-minimap-node)"}
          nodeBorderRadius={8}
          nodeStrokeWidth={3}
          pannable
          zoomable
          offsetScale={8}
          style={bottomInset > 0 ? { bottom: bottomInset + 16 } : undefined}
        />
      </ReactFlow>
    </div>
  );
}
