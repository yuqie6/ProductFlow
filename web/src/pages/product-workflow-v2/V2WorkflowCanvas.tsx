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
  useViewport,
} from "@xyflow/react";
import type {
  Connection,
  Edge,
  EdgeProps,
  IsValidConnection,
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
  CopyPlus,
  FolderOpen,
  Images,
  Link2,
  Loader2,
  Play,
  Trash2,
} from "lucide-react";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent } from "react";

import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  ProductWorkflowV2,
  WorkflowNodeStatus,
  WorkflowNodeTypeV2,
  WorkflowNodeV2,
} from "../../lib/types";
import {
  WorkflowCanvasControls,
  WorkflowCanvasGrid,
  WorkflowCanvasNodePort,
  WorkflowCanvasNodeToolbar,
  WorkflowCanvasNodeToolbarButton,
  type WorkflowCanvasPortVisualState,
} from "../product-detail/WorkflowCanvasChrome";
import { WorkflowNodePresentationCard } from "../product-detail/WorkflowNodeCard";
import type { CanvasInteractionMode } from "../product-detail/types";
import {
  WORKFLOW_CANVAS_PAN_ACTIVATION_KEY_CODE,
  WORKFLOW_CANVAS_ZOOM_ACTIVATION_KEY_CODES,
  deriveWorkflowCanvasInteractionPolicy,
  selectWorkflowCanvasNode,
  shouldToggleWorkflowCanvasNodeSelection,
} from "../product-detail/workflowCanvasInteraction";
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
  getV2ConnectionHandles,
  isV2WorkflowConnectionValid,
  isV2WorkflowLineageEdge,
  projectGlobalGraph,
  V2_NODE_HEIGHT,
  V2_NODE_WIDTH,
  type GlobalFolderNode,
} from "./graph";
import { workflowAssetThumbnailUrl, workflowNodeDownloadableImage } from "./nodeImages";

const FOLDER_CARD_WIDTH = 340;
const FOLDER_CARD_HEIGHT = 210;
const assetThumbnailUrl = workflowAssetThumbnailUrl;
const V2_SNAP_GRID: [number, number] = [24, 24];
const V2_PRO_OPTIONS = { hideAttribution: true };
const V2_CONTROL_FIT_VIEW_OPTIONS = { padding: 0.22, duration: 180, maxZoom: 1.05 };

interface WorkflowNodeData extends Record<string, unknown> {
  kind: "node";
  node: WorkflowNodeV2;
  revealActive: boolean;
  runBusy: boolean;
  structureBusy: boolean;
  canMutate: boolean;
  onRun: (node: WorkflowNodeV2) => void;
  onBindReference: (node: WorkflowNodeV2) => void;
  onDuplicate: (node: WorkflowNodeV2) => void;
  onDelete: (node: WorkflowNodeV2) => void;
  onSelectNode: (nodeId: string, event: ReactMouseEvent<HTMLElement>) => void;
  workflow: ProductWorkflowV2;
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
  protected: boolean;
  structureBusy: boolean;
  deleteLabel: string;
  protectedLabel: string;
  onDelete: (edgeId: string) => void;
  deletionEnabled: boolean;
}, "workflow-edge-v2">;

type ConnectionHandleSnapshot = {
  inProgress: boolean;
  fromHandle: { id?: string | null; nodeId: string; type: "source" | "target" } | null;
};

export interface WorkflowRevealVisibility {
  folderIds: ReadonlySet<string>;
  nodeIds: ReadonlySet<string>;
  edgeIds: ReadonlySet<string>;
}

interface V2WorkflowCanvasProps {
  workflow: ProductWorkflowV2;
  resetVersion?: number;
  revealVisibility?: WorkflowRevealVisibility;
  openFolderId: string | null;
  viewport: WorkflowCanvasViewport | null;
  structureBusy: boolean;
  runningNodeId: string | null;
  selectedNodeIds: string[];
  onOpenFolder: (folderId: string) => void;
  onRunNode: (node: WorkflowNodeV2) => void;
  onBindReference: (node: WorkflowNodeV2) => void;
  onDuplicateNode?: (node: WorkflowNodeV2) => void;
  onDeleteNode?: (node: WorkflowNodeV2) => void;
  onConnectionCreate?: (sourceNodeId: string, targetNodeId: string) => void;
  onEdgeDelete?: (edgeId: string) => void;
  onSelectionChange: (nodeIds: string[]) => void;
  onLayoutCommit: (positions: Array<{ node_id: string; position_x: number; position_y: number }>) => void;
  onFolderTranslate: (folderId: string, deltaX: number, deltaY: number) => void;
  onViewportChange: (viewport: WorkflowCanvasViewport) => void;
  mobileInteractionMode?: CanvasInteractionMode;
  mobileCanvasControlsActive?: boolean;
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

function nodeImage(node: WorkflowNodeV2): DownloadableImage | null {
  return workflowNodeDownloadableImage(node, "thumbnail");
}

function nodeHandleIds(nodeType: WorkflowNodeTypeV2): { input: string[]; output: string[] } {
  if (nodeType === "product_context") return { input: [], output: ["facts"] };
  if (nodeType === "reference_image") return { input: [], output: ["asset"] };
  if (nodeType === "prompt_generation") return { input: ["facts", "reference"], output: ["prompt"] };
  return { input: ["facts", "reference", "prompt"], output: ["image"] };
}

function getConnectionHandleVisualState(
  workflow: ProductWorkflowV2,
  node: WorkflowNodeV2,
  handleId: string,
  handleType: "source" | "target",
  connection: ConnectionHandleSnapshot,
): WorkflowCanvasPortVisualState {
  if (!connection.inProgress || !connection.fromHandle) return "idle";
  const from = connection.fromHandle;
  if (from.nodeId === node.id && from.type === handleType && from.id === handleId) return "origin";
  if (from.type === handleType) return "idle";

  const sourceNodeId = handleType === "target" ? from.nodeId : node.id;
  const targetNodeId = handleType === "target" ? node.id : from.nodeId;
  const source = workflow.nodes.find((candidate) => candidate.id === sourceNodeId);
  const target = workflow.nodes.find((candidate) => candidate.id === targetNodeId);
  if (!source || !target) return "invalid-target";
  const handles = getV2ConnectionHandles(source.node_type, target.node_type);
  const candidateMatches = handleType === "target"
    ? handles?.target === handleId && (!from.id || handles.source === from.id)
    : handles?.source === handleId && (!from.id || handles.target === from.id);
  return candidateMatches && isV2WorkflowConnectionValid(workflow, sourceNodeId, targetNodeId)
    ? "valid-target"
    : "invalid-target";
}

const WorkflowNodeCard = memo(function WorkflowNodeCard({
  data,
  selected,
  dragging,
  isConnectable,
}: NodeProps<Node<WorkflowNodeData>>) {
  const { t } = useI18n();
  const { node } = data;
  const runnable = node.node_type === "prompt_generation" || node.node_type === "image_generation";
  const active = node.status === "queued" || node.status === "running";
  const { zoom } = useViewport();
  const portVisualScale = Math.min(4.25, Math.max(1, 1 / zoom));
  const connection = useConnection<WorkflowCanvasNode, ConnectionHandleSnapshot>((snapshot) => ({
    inProgress: snapshot.inProgress,
    fromHandle: snapshot.fromHandle
      ? {
          id: snapshot.fromHandle.id,
          nodeId: snapshot.fromHandle.nodeId,
          type: snapshot.fromHandle.type,
        }
      : null,
  }));

  return (
    <div className="relative w-[248px]">
      <WorkflowCanvasNodeToolbar visible={selected}>
        {node.node_type === "reference_image" ? (
          <WorkflowCanvasNodeToolbarButton
            label={t("workflowV2.node.bindReference")}
            disabled={data.structureBusy}
            onClick={() => data.onBindReference(node)}
          >
            <Link2 size={16} aria-hidden="true" />
          </WorkflowCanvasNodeToolbarButton>
        ) : null}
        {runnable ? (
          <WorkflowCanvasNodeToolbarButton
            label={t("workflowV2.node.run")}
            disabled={data.runBusy || active || data.structureBusy}
            onClick={() => data.onRun(node)}
          >
            {data.runBusy || active ? (
              <Loader2 size={16} className="animate-spin" aria-hidden="true" />
            ) : (
              <Play size={16} aria-hidden="true" />
            )}
          </WorkflowCanvasNodeToolbarButton>
        ) : null}
        {data.canMutate && node.node_type !== "product_context" ? (
          <WorkflowCanvasNodeToolbarButton
            label={t("detail.duplicate")}
            disabled={data.structureBusy || active}
            onClick={() => data.onDuplicate(node)}
          >
            <CopyPlus size={16} aria-hidden="true" />
          </WorkflowCanvasNodeToolbarButton>
        ) : null}
        {data.canMutate && node.node_type !== "product_context" ? (
          <WorkflowCanvasNodeToolbarButton
            label={t("detail.delete")}
            disabled={data.structureBusy || active}
            destructive
            onClick={() => data.onDelete(node)}
          >
            <Trash2 size={16} aria-hidden="true" />
          </WorkflowCanvasNodeToolbarButton>
        ) : null}
      </WorkflowCanvasNodeToolbar>
      {data.inputHandleIds.map((handleId, index) => (
        <WorkflowCanvasNodePort
          key={`target:${handleId}`}
          id={handleId}
          type="target"
          top={((index + 1) / (data.inputHandleIds.length + 1)) * 100}
          label={`${t("detail.inputHandle")}: ${handleId}`}
          connectable={isConnectable}
          visualScale={portVisualScale}
          visualState={getConnectionHandleVisualState(
            data.workflow,
            node,
            handleId,
            "target",
            connection,
          )}
        />
      ))}
      {data.outputHandleIds.map((handleId, index) => (
        <WorkflowCanvasNodePort
          key={`source:${handleId}`}
          id={handleId}
          type="source"
          top={((index + 1) / (data.outputHandleIds.length + 1)) * 100}
          label={`${t("detail.outputHandle")}: ${handleId}`}
          connectable={isConnectable}
          visualScale={portVisualScale}
          visualState={getConnectionHandleVisualState(
            data.workflow,
            node,
            handleId,
            "source",
            connection,
          )}
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
        onSelect={(event) => {
          event.stopPropagation();
          data.onSelectNode(node.id, event);
        }}
      />
    </div>
  );
});

export const WorkflowFolderCard = memo(function WorkflowFolderCard({
  data,
  selected,
}: NodeProps<Node<WorkflowFolderData>>) {
  const { t } = useI18n();
  const { projection } = data;
  const { folder, summary } = projection;

  return (
    <div
      className={`relative h-[210px] w-[340px] rounded-lg border bg-white shadow-md transition-[border-color,box-shadow] dark:!bg-[#10151c] ${data.revealActive ? "animate-spring-pop-in" : ""} ${
        selected
          ? "border-indigo-500 shadow-[0_0_0_3px_rgba(99,102,241,0.16)] dark:border-violet-400"
          : "border-slate-300 dark:border-slate-700"
      }`}
      data-workflow-folder-id={folder.id}
    >
      {summary.inbound_edge_count > 0 ? (
        <WorkflowCanvasNodePort
          type="target"
          top="50%"
          label={t("workflowV2.folder.inbound", { count: summary.inbound_edge_count })}
          connectable={false}
          visualScale={0.8}
        />
      ) : null}
      {summary.outbound_edge_count > 0 ? (
        <WorkflowCanvasNodePort
          type="source"
          top="50%"
          label={t("workflowV2.folder.outbound", { count: summary.outbound_edge_count })}
          connectable={false}
          visualScale={0.8}
        />
      ) : null}
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

const WorkflowCanvasEdgeCard = memo(function WorkflowCanvasEdgeCard({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  selected,
  data,
}: EdgeProps<WorkflowCanvasEdge>) {
  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });
  const [hovered, setHovered] = useState(false);
  const projected = data?.projected ?? false;
  const protectedEdge = data?.protected ?? false;
  const deleteDisabled = protectedEdge || Boolean(data?.structureBusy);
  const label = protectedEdge ? data?.protectedLabel : data?.deleteLabel;

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        style={{
          stroke: projected ? "#6366f1" : selected ? "#4f46e5" : hovered ? "#64748b" : "#94a3b8",
          strokeWidth: projected ? 2.2 : selected ? 2.2 : 1.8,
          transition: "stroke 0.15s ease, stroke-width 0.15s ease",
        }}
      />
      <path
        d={edgePath}
        fill="none"
        stroke="transparent"
        strokeWidth={15}
        className={projected ? "pointer-events-none" : "cursor-pointer"}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      />
      {!projected && data?.deletionEnabled ? (
        <EdgeToolbar
          edgeId={id}
          x={labelX}
          y={labelY}
          isVisible
          className={`nodrag nowheel nopan transition-all duration-200 ${
            hovered || selected
              ? "pointer-events-auto scale-100 opacity-100"
              : "pointer-events-none scale-75 opacity-0"
          }`}
          onMouseEnter={() => setHovered(true)}
          onMouseLeave={() => setHovered(false)}
        >
          <button
            type="button"
            className="nodrag nowheel nopan flex h-7 w-7 items-center justify-center rounded-full border border-slate-200 bg-white/95 text-slate-500 shadow-sm transition-colors hover:border-red-200 hover:bg-red-50 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-45 dark:border-slate-800 dark:bg-[#0f1726]/95 dark:text-slate-400 dark:hover:border-red-900/50 dark:hover:bg-red-950/30 dark:hover:text-red-200"
            onClick={(event) => {
              event.stopPropagation();
              if (!deleteDisabled) data?.onDelete(id);
            }}
            disabled={deleteDisabled}
            title={label}
            aria-label={label}
          >
            <Trash2 size={13} strokeWidth={2.2} />
          </button>
        </EdgeToolbar>
      ) : data && data.count > 1 ? (
        <EdgeToolbar edgeId={id} x={labelX} y={labelY} isVisible className="nodrag nopan nowheel">
          <span className="rounded-full border border-indigo-200 bg-white px-2 py-0.5 text-[10px] font-bold text-indigo-700 shadow-sm dark:border-violet-400/40 dark:bg-slate-900 dark:text-violet-200">
            {data.count}
          </span>
        </EdgeToolbar>
      ) : null}
    </>
  );
});

const nodeTypes = {
  "workflow-node-v2": WorkflowNodeCard,
  "workflow-folder-v2": WorkflowFolderCard,
};

const edgeTypes = {
  "workflow-edge-v2": WorkflowCanvasEdgeCard,
};

function toCanvasNodes(
  workflow: ProductWorkflowV2,
  openFolderId: string | null,
  options: Pick<
    V2WorkflowCanvasProps,
    | "structureBusy"
    | "runningNodeId"
    | "onOpenFolder"
    | "onRunNode"
    | "onBindReference"
    | "onDuplicateNode"
    | "onDeleteNode"
    | "revealVisibility"
  > & {
    selectedNodeIds: string[];
    onSelectNode: (nodeId: string, event: ReactMouseEvent<HTMLElement>) => void;
  },
): WorkflowCanvasNode[] {
  const revealVisibility = options.revealVisibility;
  const toRealCanvasNode = (
    node: WorkflowNodeV2,
    position: { x: number; y: number },
  ): Node<WorkflowNodeData, "workflow-node-v2"> => {
    const handles = nodeHandleIds(node.node_type);
    return {
      id: node.id,
      type: "workflow-node-v2",
      position,
      width: V2_NODE_WIDTH,
      height: V2_NODE_HEIGHT,
      selected: options.selectedNodeIds.includes(node.id),
      hidden: Boolean(revealVisibility && !revealVisibility.nodeIds.has(node.id)),
      data: {
        kind: "node",
        node,
        revealActive: Boolean(revealVisibility),
        runBusy: options.runningNodeId === node.id,
        structureBusy: options.structureBusy,
        canMutate: Boolean(options.onDuplicateNode && options.onDeleteNode),
        onRun: options.onRunNode,
        onBindReference: options.onBindReference,
        onDuplicate: options.onDuplicateNode ?? (() => undefined),
        onDelete: options.onDeleteNode ?? (() => undefined),
        onSelectNode: options.onSelectNode,
        workflow,
        inputHandleIds: handles.input,
        outputHandleIds: handles.output,
      },
    };
  };
  if (openFolderId) {
    return buildLocalFolderGraph(workflow, openFolderId).nodes.map(
      (node) => toRealCanvasNode(node, { x: node.position_x, y: node.position_y }),
    );
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
  return fullProjection.nodes.map((item): WorkflowCanvasNode => {
    if (item.kind === "node") {
      return toRealCanvasNode(item.node, item.position);
    }
    return {
      id: item.id,
      type: "workflow-folder-v2",
      position: item.position,
      width: FOLDER_CARD_WIDTH,
      height: FOLDER_CARD_HEIGHT,
      selected: false,
      hidden: Boolean(revealVisibility && !revealVisibility.folderIds.has(item.folder.id)),
      connectable: false,
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
    };
  });
}

function toCanvasEdges(
  workflow: ProductWorkflowV2,
  openFolderId: string | null,
  revealVisibility?: WorkflowRevealVisibility,
  options?: {
    structureBusy: boolean;
    deleteLabel: string;
    protectedLabel: string;
    onDelete: (edgeId: string) => void;
    deletionEnabled: boolean;
  },
): WorkflowCanvasEdge[] {
  if (openFolderId) {
    return buildLocalFolderGraph(workflow, openFolderId).edges.map((edge) => ({
      id: edge.id,
      source: edge.source_node_id,
      target: edge.target_node_id,
      sourceHandle: edge.source_handle,
      targetHandle: edge.target_handle,
      type: "workflow-edge-v2",
      hidden: Boolean(revealVisibility && !revealVisibility.edgeIds.has(edge.id)),
      style: { stroke: "#94a3b8", strokeWidth: 1.8 },
      data: {
        projected: false,
        originalEdgeIds: [edge.id],
        count: 1,
        protected: isV2WorkflowLineageEdge(workflow, edge),
        structureBusy: options?.structureBusy ?? false,
        deleteLabel: options?.deleteLabel ?? "",
        protectedLabel: options?.protectedLabel ?? "",
        onDelete: options?.onDelete ?? (() => undefined),
        deletionEnabled: options?.deletionEnabled ?? false,
      },
    }));
  }
  return projectGlobalGraph(workflow).edges.map((edge) => {
    const revealedEdgeIds = revealVisibility
      ? edge.original_edge_ids.filter((edgeId) => revealVisibility.edgeIds.has(edgeId))
      : edge.original_edge_ids;
    const count = revealedEdgeIds.length;
    const originalEdge = edge.projected
      ? null
      : workflow.edges.find((candidate) => candidate.id === edge.original_edge_ids[0]) ?? null;
    return {
      id: edge.id,
      source: edge.source,
      target: edge.target,
      sourceHandle: edge.source_handle,
      targetHandle: edge.target_handle,
      type: "workflow-edge-v2",
      hidden: Boolean(revealVisibility && count === 0),
      animated: edge.projected && count > 1,
      style: {
        stroke: edge.projected ? "#6366f1" : "#94a3b8",
        strokeWidth: edge.projected ? 2.2 : 1.8,
      },
      data: {
        projected: edge.projected,
        originalEdgeIds: revealedEdgeIds,
        count,
        protected: Boolean(originalEdge && isV2WorkflowLineageEdge(workflow, originalEdge)),
        structureBusy: options?.structureBusy ?? false,
        deleteLabel: options?.deleteLabel ?? "",
        protectedLabel: options?.protectedLabel ?? "",
        onDelete: options?.onDelete ?? (() => undefined),
        deletionEnabled: options?.deletionEnabled ?? false,
      },
    };
  });
}

function isRealNode(node: WorkflowCanvasNode): node is Node<WorkflowNodeData, "workflow-node-v2"> {
  return node.data.kind === "node";
}

export function V2WorkflowCanvas({
  workflow,
  resetVersion = 0,
  revealVisibility,
  openFolderId,
  viewport,
  structureBusy,
  runningNodeId,
  selectedNodeIds,
  onOpenFolder,
  onRunNode,
  onBindReference,
  onDuplicateNode,
  onDeleteNode,
  onConnectionCreate,
  onEdgeDelete,
  onSelectionChange,
  onLayoutCommit,
  onFolderTranslate,
  onViewportChange,
  mobileInteractionMode = "edit",
  mobileCanvasControlsActive = false,
}: V2WorkflowCanvasProps) {
  const { t } = useI18n();
  const surfaceRef = useRef<HTMLDivElement | null>(null);
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
      onSelectionChange(nodeIds);
    }
  }, [onSelectionChange, selectedNodeIds]);
  const selectNodeFromPointer = useCallback((nodeId: string, event: ReactMouseEvent<HTMLElement>) => {
    const toggle = shouldToggleWorkflowCanvasNodeSelection({
      compact: mobileCanvasControlsActive,
      mode: mobileInteractionMode,
      ctrlKey: event.ctrlKey,
      metaKey: event.metaKey,
      shiftKey: event.shiftKey,
    });
    publishSelection(selectWorkflowCanvasNode(selectedNodeIds, nodeId, toggle));
  }, [mobileCanvasControlsActive, mobileInteractionMode, publishSelection, selectedNodeIds]);
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
  const interactionPolicy = deriveWorkflowCanvasInteractionPolicy({
    compact: mobileCanvasControlsActive,
    mode: mobileInteractionMode,
    locked: structureBusy,
    connectionEditing: Boolean(onConnectionCreate),
  });
  const graphIdentity = `${workflow.id}:${openFolderId ?? "global"}:${workflow.edit_version}:${resetVersion}`;
  const selectionScope = `${workflow.id}:${openFolderId ?? "global"}`;
  const graphNodes = useMemo(
    () => toCanvasNodes(workflow, openFolderId, {
      structureBusy,
      runningNodeId,
      onOpenFolder,
      onRunNode,
      onBindReference,
      onDuplicateNode,
      onDeleteNode,
      revealVisibility,
      selectedNodeIds,
      onSelectNode: selectNodeFromPointer,
    }),
    [
      onBindReference,
      onDeleteNode,
      onDuplicateNode,
      onOpenFolder,
      onRunNode,
      openFolderId,
      revealVisibility,
      runningNodeId,
      selectNodeFromPointer,
      selectedNodeIds,
      structureBusy,
      workflow,
    ],
  );
  const graphEdges = useMemo(
    () => toCanvasEdges(workflow, openFolderId, revealVisibility, {
      structureBusy,
      deleteLabel: t("detail.deleteEdge"),
      protectedLabel: t("workflowV2.edge.protected"),
      onDelete: onEdgeDelete ?? (() => undefined),
      deletionEnabled: Boolean(onEdgeDelete),
    }),
    [onEdgeDelete, openFolderId, revealVisibility, structureBusy, t, workflow],
  );
  const [nodes, setNodes, onNodesChange] = useNodesState<WorkflowCanvasNode>(graphNodes);
  const previousIdentityRef = useRef(graphIdentity);
  const previousSelectionScopeRef = useRef(selectionScope);
  const selectionBoxNodeIdsRef = useRef<string[] | null>(null);
  const dragStartRef = useRef(new Map<string, { x: number; y: number }>());

  useEffect(() => {
    if (previousSelectionScopeRef.current === selectionScope) {
      return;
    }
    previousSelectionScopeRef.current = selectionScope;
    publishSelection([]);
  }, [publishSelection, selectionScope]);

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
          ? { ...node, position: previous.position }
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
      if (selectionBoxNodeIdsRef.current !== null) {
        selectionBoxNodeIdsRef.current = selectedNodes.filter(isRealNode).map((node) => node.data.node.id);
      }
    },
    [],
  );
  const handleSelectionEnd = useCallback(() => {
    const nodeIds = selectionBoxNodeIdsRef.current;
    selectionBoxNodeIdsRef.current = null;
    if (nodeIds !== null) {
      publishSelection(nodeIds);
    }
  }, [publishSelection]);
  const handlePaneClick = useCallback(() => {
    publishSelection([]);
  }, [publishSelection]);
  const handleMoveEnd = useCallback<OnMoveEnd>((_event, nextViewport) => {
    persistViewport(nextViewport);
  }, [persistViewport]);
  const toggleSnapToGrid = useCallback(() => {
    setSnapToGrid((current) => !current);
  }, []);
  const handleConnect = useCallback((connection: Connection) => {
    if (
      onConnectionCreate
      && connection.source
      && connection.target
      && isV2WorkflowConnectionValid(
        workflow,
        connection.source,
        connection.target,
        connection.sourceHandle,
        connection.targetHandle,
      )
    ) {
      onConnectionCreate(connection.source, connection.target);
    }
  }, [onConnectionCreate, workflow]);
  const isValidConnection = useCallback<IsValidConnection<WorkflowCanvasEdge>>(
    (connection) => Boolean(
      connection.source
      && connection.target
      && isV2WorkflowConnectionValid(
        workflow,
        connection.source,
        connection.target,
        connection.sourceHandle,
        connection.targetHandle,
      ),
    ),
    [workflow],
  );

  return (
    <div ref={surfaceRef} className="h-full min-h-0 w-full overflow-hidden">
      <ReactFlow<WorkflowCanvasNode, WorkflowCanvasEdge>
        key={`${workflow.id}:${openFolderId ?? "global"}:${layoutMode}:${restoredViewport ? "restore" : "fit"}`}
        nodes={nodes}
        edges={graphEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onNodeDragStart={handleNodeDragStart}
        onNodeDragStop={handleNodeDragStop}
        onNodeDoubleClick={handleNodeDoubleClick}
        onSelectionChange={handleSelectionChange}
        onSelectionStart={() => {
          selectionBoxNodeIdsRef.current = interactionPolicy.canSelectByBox ? [] : null;
        }}
        onSelectionEnd={handleSelectionEnd}
        onPaneClick={handlePaneClick}
        onMoveEnd={handleMoveEnd}
        defaultViewport={defaultViewport}
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
        snapGrid={V2_SNAP_GRID}
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
        connectionLineStyle={{ stroke: "#2563eb", strokeWidth: 2, strokeDasharray: "6 4" }}
        autoPanOnConnect
        onConnect={handleConnect}
        isValidConnection={isValidConnection}
        className="bg-transparent"
        proOptions={V2_PRO_OPTIONS}
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
          autoLayoutBusy={structureBusy}
          fitViewOptions={fitViewOptions}
          onToggleSnapToGrid={toggleSnapToGrid}
          onAutoLayout={commitAutoLayout}
          onViewportCommit={persistViewport}
          normalizeZoom={(zoom) => Math.min(2, Math.max(0.12, zoom))}
        />
        <MiniMap
          position="bottom-right"
          aria-label={t("detail.canvasMiniMap")}
          className="workflow-canvas-minimap nopan nodrag nowheel hidden lg:block"
          nodeColor={(node) => node.data?.kind === "folder" ? "#6366f1" : "#94a3b8"}
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

export { assetThumbnailUrl, folderSyntheticNodeId };
