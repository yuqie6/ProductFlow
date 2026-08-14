import { forwardRef, memo, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent, MutableRefObject } from "react";
import {
  BaseEdge,
  ConnectionLineType,
  ConnectionMode,
  EdgeToolbar,
  MiniMap,
  ReactFlow,
  SelectionMode,
  getBezierPath,
  useOnViewportChange,
  useOnSelectionChange,
  useKeyPress,
  useConnection,
} from "@xyflow/react";
import type {
  Connection,
  Edge,
  EdgeProps,
  IsValidConnection,
  Node,
  NodeProps,
  OnNodeDrag,
  ReactFlowInstance,
  Viewport,
  XYPosition,
} from "@xyflow/react";
import { CopyPlus, Focus, Loader2, Play, Save, Trash2 } from "lucide-react";

import type { DownloadableImage } from "../../lib/image-downloads";
import type { ProductWorkflow, WorkflowNode } from "../../lib/types";
import {
  WorkflowCanvasControls,
  WorkflowCanvasGrid,
  WorkflowCanvasNodePort,
  WorkflowCanvasNodeToolbar,
  WorkflowCanvasNodeToolbarButton,
  type WorkflowCanvasPortVisualState,
} from "./WorkflowCanvasChrome";
import { WorkflowNodeCard } from "./WorkflowNodeCard";
import { MAX_ZOOM, MIN_ZOOM, NODE_WIDTH } from "./constants";
import type { CanvasInteractionMode, CanvasPoint } from "./types";
import {
  WORKFLOW_CANVAS_PAN_ACTIVATION_KEY_CODE,
  WORKFLOW_CANVAS_ZOOM_ACTIVATION_KEY_CODES,
  deriveWorkflowCanvasInteractionPolicy,
} from "./workflowCanvasInteraction";
import type {
  WorkflowCanvasActionId,
  WorkflowCanvasActionIcon,
  WorkflowCanvasActionItem,
  WorkflowCanvasActionTarget,
  WorkflowCanvasActionToolbar,
} from "./workflowActions";
import {
  PRODUCTFLOW_EDGE_TYPE,
  PRODUCTFLOW_NODE_TYPE,
  PRODUCTFLOW_SOURCE_HANDLE,
  PRODUCTFLOW_TARGET_HANDLE,
  type ProductFlowEdgeData,
  type ProductFlowNodeData,
  connectionToWorkflowEdgeInput,
  getChangedWorkflowNodePositionCandidates,
  getNodePositionForViewportCenter,
  normalizeWorkflowZoom,
  reactFlowPositionToWorkflowPatch,
  shouldCommitNodeDragGroupPosition,
  workflowToReactFlowEdges,
  workflowToReactFlowNodes,
} from "./reactFlowAdapters";

export interface NodePositionCommitInput {
  node: WorkflowNode;
  position_x: number;
  position_y: number;
  mutationVersion: number;
  moveGroupId: string;
  moveGroupSize: number;
}

export interface WorkflowCanvasHandle {
  acceptNodePositionMutation: (nodeId: string, mutationVersion: number) => boolean;
  clearOptimisticNodePosition: (nodeId: string) => void;
  getViewportCenterNodePosition: () => CanvasPoint;
  centerNode: (node: WorkflowNode) => void;
  fitNodeIds: (nodeIds: string[]) => void;
  triggerAutoLayout: () => void;
}

interface WorkflowCanvasNodeData extends ProductFlowNodeData {
  image: DownloadableImage | null;
  primarySelected: boolean;
  secondarySelected: boolean;
  previewSelected: boolean;
  inputHandleLabel: string;
  outputHandleLabel: string;
  onSelectNode: (nodeId: string, event: ReactMouseEvent<HTMLElement>) => void;
  actionToolbar: WorkflowCanvasActionToolbar | null;
  onNodeAction: (actionId: WorkflowCanvasActionId, target: WorkflowCanvasActionTarget) => void;
}

interface WorkflowCanvasEdgeData extends ProductFlowEdgeData {
  deleteLabel: string;
  disabled: boolean;
  onDeleteEdge: (edgeId: string) => void;
}

type WorkflowCanvasNode = Node<WorkflowCanvasNodeData, typeof PRODUCTFLOW_NODE_TYPE>;
type WorkflowCanvasEdge = Edge<WorkflowCanvasEdgeData, typeof PRODUCTFLOW_EDGE_TYPE> & {
  data: WorkflowCanvasEdgeData;
};
type WorkflowCanvasSelectionSession = { nodeIds: string[] } | null;
type ConnectionHandleSnapshot = {
  inProgress: boolean;
  fromHandle: { id?: string | null; nodeId: string; type: "source" | "target" } | null;
};

interface WorkflowCanvasProps {
  workflow: ProductWorkflow | null;
  isLoading: boolean;
  selectedNodeId: string | null;
  selectedNodeIds: string[];
  structureBusy: boolean;
  mobileInteractionMode: CanvasInteractionMode;
  mobileCanvasControlsActive: boolean;
  zoomStorageKey: string;
  initialZoom: number;
  selectedGroupCount: number;
  loadFailedLabel: string;
  deleteEdgeLabel: string;
  inputHandleLabel: string;
  outputHandleLabel: string;
  zoomOutLabel: string;
  resetZoomLabel: string;
  zoomInLabel: string;
  fitViewLabel: string;
  fitSelectionLabel: string;
  canvasControlsLabel: string;
  canvasMiniMapLabel: string;
  snapToGridLabel: string;
  autoLayoutLabel: string;
  snapToGrid: boolean;
  onToggleSnapToGrid: () => void;
  onAutoLayout: () => void;
  onBlankClick: (event: ReactMouseEvent<Element>) => void;
  onSelectNode: (nodeId: string, event: ReactMouseEvent<HTMLElement>) => void;
  onNodeDragCompleteSelect: (nodeId: string) => void;
  getNodeDragGroup: (nodeId: string) => string[];
  onSelectionBoxComplete: (nodeIds: string[]) => void;
  onNodePositionCommit: (input: NodePositionCommitInput) => void;
  onConnectionCreate: (input: { sourceNodeId: string; targetNodeId: string }) => void;
  onDeleteEdge: (edgeId: string) => void;
  getNodeActionToolbar: (nodeId: string) => WorkflowCanvasActionToolbar | null;
  onNodeAction: (actionId: WorkflowCanvasActionId, target: WorkflowCanvasActionTarget) => void;
  keyboardShortcutsActive: boolean;
  onClearSelection: () => void;
  getNodeImage: (node: WorkflowNode) => DownloadableImage | null;
}

const HANDLE_STATE_SEPARATOR = ":";

function removeNodePositions(current: Record<string, CanvasPoint>, nodeIds: Iterable<string>) {
  let changed = false;
  const next = { ...current };
  for (const nodeId of nodeIds) {
    if (nodeId in next) {
      delete next[nodeId];
      changed = true;
    }
  }
  return changed ? next : current;
}

function getConnectionHandleVisualState(
  nodeId: string,
  handleType: "source" | "target",
  connection: ConnectionHandleSnapshot,
): WorkflowCanvasPortVisualState {
  if (!connection.inProgress || !connection.fromHandle) {
    return "idle";
  }
  const fromHandle = connection.fromHandle;
  if (fromHandle.nodeId === nodeId && fromHandle.type === handleType) {
    return "origin";
  }
  if (fromHandle.type === handleType) {
    return "idle";
  }

  const candidate =
    handleType === "target"
      ? {
          source: fromHandle.nodeId,
          target: nodeId,
          sourceHandle: fromHandle.id ?? null,
          targetHandle: PRODUCTFLOW_TARGET_HANDLE,
        }
      : {
          source: nodeId,
          target: fromHandle.nodeId,
          sourceHandle: PRODUCTFLOW_SOURCE_HANDLE,
          targetHandle: fromHandle.id ?? null,
        };

  return connectionToWorkflowEdgeInput(candidate) ? "valid-target" : "invalid-target";
}

function WorkflowNodeToolbarIcon({
  icon,
  pending,
}: {
  icon: WorkflowCanvasActionIcon;
  pending?: boolean;
}) {
  if (pending) {
    return <Loader2 size={16} className="animate-spin" aria-hidden="true" />;
  }
  if (icon === "run") {
    return <Play size={16} aria-hidden="true" />;
  }
  if (icon === "duplicate") {
    return <CopyPlus size={16} aria-hidden="true" />;
  }
  if (icon === "fitSelected") {
    return <Focus size={16} aria-hidden="true" />;
  }
  if (icon === "saveTemplate") {
    return <Save size={16} aria-hidden="true" />;
  }
  return <Trash2 size={16} aria-hidden="true" />;
}

function WorkflowNodeToolbarActions({
  target,
  items,
  onAction,
}: {
  target: WorkflowCanvasActionTarget;
  items: WorkflowCanvasActionItem[];
  onAction: (actionId: WorkflowCanvasActionId, target: WorkflowCanvasActionTarget) => void;
}) {
  return (
    <WorkflowCanvasNodeToolbar visible={items.length > 0}>
      {items.map((item) => {
        const label = item.title ?? item.label ?? "";
        return (
          <WorkflowCanvasNodeToolbarButton
            key={item.id}
            label={label}
            disabled={item.disabled}
            destructive={Boolean(item.destructive)}
            onClick={() => onAction(item.id, target)}
          >
            <WorkflowNodeToolbarIcon icon={item.icon} pending={item.pending} />
          </WorkflowCanvasNodeToolbarButton>
        );
      })}
    </WorkflowCanvasNodeToolbar>
  );
}

function ProductFlowCanvasNode({ data, dragging, isConnectable }: NodeProps<WorkflowCanvasNode>) {
  const node = data.workflowNode;
  const connectionHandleStateKey = useConnection<WorkflowCanvasNode, string>((connection) => {
    if (!isConnectable) {
      return `idle${HANDLE_STATE_SEPARATOR}idle`;
    }
    return [
      getConnectionHandleVisualState(node.id, "target", connection),
      getConnectionHandleVisualState(node.id, "source", connection),
    ].join(HANDLE_STATE_SEPARATOR);
  });
  const [inputHandleState, outputHandleState] = connectionHandleStateKey.split(HANDLE_STATE_SEPARATOR) as [
    WorkflowCanvasPortVisualState,
    WorkflowCanvasPortVisualState,
  ];

  return (
    <div data-workflow-node-id={node.id} className="nopan relative w-[248px]">
      {data.actionToolbar ? (
        <WorkflowNodeToolbarActions
          target={data.actionToolbar.target}
          items={data.actionToolbar.items}
          onAction={data.onNodeAction}
        />
      ) : null}
      <WorkflowCanvasNodePort
        type="target"
        id={PRODUCTFLOW_TARGET_HANDLE}
        connectable={isConnectable}
        visualState={inputHandleState}
        top="56px"
        label={data.inputHandleLabel}
      />
      <WorkflowNodeCard
        node={node}
        image={data.image}
        primarySelected={data.primarySelected}
        secondarySelected={data.secondarySelected}
        previewSelected={data.previewSelected}
        dragging={dragging}
        onSelect={(event) => {
          event.stopPropagation();
          data.onSelectNode(node.id, event);
        }}
      />
      <WorkflowCanvasNodePort
        type="source"
        id={PRODUCTFLOW_SOURCE_HANDLE}
        connectable={isConnectable}
        visualState={outputHandleState}
        top="56px"
        label={data.outputHandleLabel}
      />
    </div>
  );
}

function ProductFlowCanvasEdge({
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

  const [isHovered, setIsHovered] = useState(false);

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        style={{
          stroke: selected ? "#4f46e5" : isHovered ? "#64748b" : "#94a3b8",
          strokeWidth: selected ? 2.2 : 1.7,
          transition: "stroke 0.15s ease, stroke-width 0.15s ease",
        }}
      />
      <path
        d={edgePath}
        fill="none"
        stroke="transparent"
        strokeWidth={15}
        className="cursor-pointer"
        onMouseEnter={() => setIsHovered(true)}
        onMouseLeave={() => setIsHovered(false)}
      />
      <EdgeToolbar
        edgeId={id}
        x={labelX}
        y={labelY}
        isVisible
        className={`nodrag nowheel nopan transition-all duration-200 ${
          isHovered || selected
            ? "scale-100 opacity-100 pointer-events-auto"
            : "scale-75 opacity-0 pointer-events-none"
        }`}
        onMouseEnter={() => setIsHovered(true)}
        onMouseLeave={() => setIsHovered(false)}
      >
        <button
          type="button"
          data-node-action
          className="nodrag nowheel nopan flex h-6 w-6 items-center justify-center rounded-full border border-slate-200 bg-white/95 text-slate-500 shadow-sm transition-colors hover:border-red-200 hover:bg-red-50 hover:text-red-600 disabled:opacity-50 dark:border-slate-800 dark:bg-[#0f1726]/95 dark:text-slate-400 dark:hover:border-red-900/50 dark:hover:bg-red-950/30 dark:hover:text-red-200"
          onClick={(event) => {
            event.stopPropagation();
            data?.onDeleteEdge(id);
          }}
          disabled={data?.disabled ?? false}
          title={data?.deleteLabel}
          aria-label={data?.deleteLabel}
        >
          <Trash2 size={12} strokeWidth={2.2} />
        </button>
      </EdgeToolbar>
    </>
  );
}

const nodeTypes = {
  [PRODUCTFLOW_NODE_TYPE]: memo(ProductFlowCanvasNode),
};

const edgeTypes = {
  [PRODUCTFLOW_EDGE_TYPE]: memo(ProductFlowCanvasEdge),
};

const FIT_VIEW_DURATION_MS = 180;
const FIT_VIEW_PADDING = 0.2;
const FIT_VIEW_MAX_ZOOM = 1.2;
const WORKFLOW_CLEAR_SELECTION_KEY_CODE = "Escape";
const MINI_MAP_NODE_COLORS: Record<WorkflowNode["node_type"], string> = {
  product_context: "#64748b",
  reference_image: "#0ea5e9",
  copy_generation: "#8b5cf6",
  image_generation: "#22c55e",
};
const MINI_MAP_STATUS_STROKE_COLORS: Record<WorkflowNode["status"], string> = {
  idle: "#94a3b8",
  queued: "#f59e0b",
  running: "#2563eb",
  succeeded: "#16a34a",
  failed: "#dc2626",
  cancelled: "#71717a",
};

interface WorkflowCanvasViewportBridgeProps {
  onViewportChange: (viewport: Viewport) => void;
  onViewportChangeEnd: (viewport: Viewport) => void;
}

function WorkflowCanvasViewportBridge({
  onViewportChange,
  onViewportChangeEnd,
}: WorkflowCanvasViewportBridgeProps) {
  useOnViewportChange({
    onChange: onViewportChange,
    onEnd: onViewportChangeEnd,
  });
  return null;
}

interface WorkflowCanvasSelectionBridgeProps {
  activeSessionRef: MutableRefObject<WorkflowCanvasSelectionSession>;
}

function WorkflowCanvasSelectionBridge({ activeSessionRef }: WorkflowCanvasSelectionBridgeProps) {
  const handleSelectionChange = useCallback(
    ({ nodes: selectedNodes }: { nodes: WorkflowCanvasNode[] }) => {
      const activeSession = activeSessionRef.current;
      if (activeSession) {
        activeSession.nodeIds = selectedNodes.map((node) => node.id);
      }
    },
    [activeSessionRef],
  );

  useOnSelectionChange<WorkflowCanvasNode, WorkflowCanvasEdge>({
    onChange: handleSelectionChange,
  });
  return null;
}

interface WorkflowCanvasKeyboardBridgeProps {
  enabled: boolean;
  hasMultiSelection: boolean;
  onClearSelection: () => void;
}

function WorkflowCanvasKeyboardBridge({
  enabled,
  hasMultiSelection,
  onClearSelection,
}: WorkflowCanvasKeyboardBridgeProps) {
  const clearSelectionPressed = useKeyPress(WORKFLOW_CLEAR_SELECTION_KEY_CODE, {
    preventDefault: false,
    actInsideInputWithModifier: false,
  });

  useEffect(() => {
    if (enabled && hasMultiSelection && clearSelectionPressed) {
      onClearSelection();
    }
  }, [clearSelectionPressed, enabled, hasMultiSelection, onClearSelection]);

  return null;
}

function workflowMiniMapNodeColor(node: WorkflowCanvasNode) {
  return MINI_MAP_NODE_COLORS[node.data.workflowNode.node_type];
}

function workflowMiniMapNodeStrokeColor(node: WorkflowCanvasNode) {
  if (node.data.primarySelected) {
    return "#2563eb";
  }
  if (node.data.secondarySelected) {
    return "#7c3aed";
  }
  return MINI_MAP_STATUS_STROKE_COLORS[node.data.workflowNode.status];
}

export const WorkflowCanvas = forwardRef<WorkflowCanvasHandle, WorkflowCanvasProps>(function WorkflowCanvas(
  {
    workflow,
    isLoading,
    selectedNodeId,
    selectedNodeIds,
    structureBusy,
    mobileInteractionMode,
    mobileCanvasControlsActive,
    zoomStorageKey,
    initialZoom,
    selectedGroupCount,
    loadFailedLabel,
    deleteEdgeLabel,
    inputHandleLabel,
    outputHandleLabel,
    zoomOutLabel,
    resetZoomLabel,
    zoomInLabel,
    fitViewLabel,
    fitSelectionLabel,
    canvasControlsLabel,
    canvasMiniMapLabel,
    snapToGridLabel,
    autoLayoutLabel,
    snapToGrid,
    onToggleSnapToGrid,
    onAutoLayout,
    onBlankClick,
    onSelectNode,
    onNodeDragCompleteSelect,
    getNodeDragGroup,
    onSelectionBoxComplete,
    onNodePositionCommit,
    onConnectionCreate,
    onDeleteEdge,
    getNodeActionToolbar,
    onNodeAction,
    keyboardShortcutsActive,
    onClearSelection,
    getNodeImage,
  },
  ref,
) {
  const flowWrapperRef = useRef<HTMLDivElement | null>(null);
  const flowInstanceRef = useRef<ReactFlowInstance<WorkflowCanvasNode, WorkflowCanvasEdge> | null>(null);
  const viewportRef = useRef<Viewport>({ x: 0, y: 0, zoom: initialZoom });
  const selectionBoxSessionRef = useRef<WorkflowCanvasSelectionSession>(null);
  const draggingNodeIdsRef = useRef<string[]>([]);
  const pendingNodeDragSelectionRef = useRef<string | null>(null);
  const nodeDragStartPositionsRef = useRef<Record<string, CanvasPoint>>({});
  const nodePositionMutationVersionsRef = useRef<Record<string, number>>({});
  const nodeDragCommitGroupCounterRef = useRef(0);
  const [flowReady, setFlowReady] = useState(false);
  const [optimisticNodePositions, setOptimisticNodePositions] = useState<Record<string, CanvasPoint>>({});

  const recordViewport = useCallback((nextViewport: Viewport) => {
    const normalizedViewport = {
      ...nextViewport,
      zoom: normalizeWorkflowZoom(nextViewport.zoom),
    };
    viewportRef.current = normalizedViewport;
    return normalizedViewport;
  }, []);

  const persistViewport = useCallback(
    (nextViewport: Viewport) => {
      const normalizedViewport = recordViewport(nextViewport);
      window.localStorage.setItem(zoomStorageKey, String(normalizedViewport.zoom));
      return normalizedViewport;
    },
    [recordViewport, zoomStorageKey],
  );

  const fitNodeIds = useCallback(
    (nodeIds: string[]) => {
      const instance = flowInstanceRef.current;
      if (!instance) {
        return;
      }
      const fitNodes = nodeIds.filter((nodeId) => instance.getNode(nodeId)).map((nodeId) => ({ id: nodeId }));
      if (!fitNodes.length) {
        return;
      }
      void instance
        .fitView({
          nodes: fitNodes,
          padding: FIT_VIEW_PADDING,
          duration: FIT_VIEW_DURATION_MS,
          maxZoom: FIT_VIEW_MAX_ZOOM,
        })
        .then(() => persistViewport(instance.getViewport()));
    },
    [persistViewport],
  );

  useImperativeHandle(
    ref,
    () => ({
      acceptNodePositionMutation: (nodeId, mutationVersion) =>
        nodePositionMutationVersionsRef.current[nodeId] === mutationVersion,
      clearOptimisticNodePosition: (nodeId) => {
        setOptimisticNodePositions((current) => removeNodePositions(current, [nodeId]));
      },
      getViewportCenterNodePosition: () => {
        const instance = flowInstanceRef.current;
        const wrapper = flowWrapperRef.current;
        if (!instance || !wrapper) {
          return { x: 120, y: 120 };
        }
        const rect = wrapper.getBoundingClientRect();
        const center = instance.screenToFlowPosition({
          x: rect.left + rect.width / 2,
          y: rect.top + rect.height / 2,
        });
        return getNodePositionForViewportCenter(center);
      },
      centerNode: (node) => {
        void flowInstanceRef.current?.setCenter(node.position_x + NODE_WIDTH / 2, node.position_y + 120, {
          zoom: viewportRef.current.zoom,
        });
      },
      fitNodeIds,
      triggerAutoLayout: () => {
        if (!workflow || !workflow.nodes.length) {
          return;
        }
        const inDegree: Record<string, number> = {};
        const adj: Record<string, string[]> = {};
        const nodes = workflow.nodes;
        const edges = workflow.edges;

        for (const node of nodes) {
          inDegree[node.id] = 0;
          adj[node.id] = [];
        }
        for (const edge of edges) {
          if (adj[edge.source_node_id]) {
            adj[edge.source_node_id].push(edge.target_node_id);
          }
          inDegree[edge.target_node_id] = (inDegree[edge.target_node_id] || 0) + 1;
        }

        const depth: Record<string, number> = {};
        const queue: string[] = [];

        for (const node of nodes) {
          if (inDegree[node.id] === 0) {
            depth[node.id] = 0;
            queue.push(node.id);
          } else {
            depth[node.id] = 0;
          }
        }

        while (queue.length > 0) {
          const u = queue.shift()!;
          const uDepth = depth[u];
          for (const v of adj[u]) {
            depth[v] = Math.max(depth[v] || 0, uDepth + 1);
            queue.push(v);
          }
        }

        const layers: Record<number, typeof nodes> = {};
        for (const node of nodes) {
          const d = depth[node.id] || 0;
          if (!layers[d]) {
            layers[d] = [];
          }
          layers[d].push(node);
        }

        for (const d of Object.keys(layers)) {
          layers[Number(d)].sort((a, b) => a.position_y - b.position_y);
        }

        const getNodeHeight = (node: typeof nodes[number]): number => {
          const element = document.querySelector(`[data-workflow-node-id="${node.id}"]`);
          if (element) {
            const rect = element.getBoundingClientRect();
            if (rect.height > 0) {
              return rect.height;
            }
          }
          let estimatedHeight = 116;
          const isImageNode = node.node_type === "reference_image" || node.node_type === "image_generation";
          const hasImage = isImageNode && (node.status === "succeeded" || node.status === "failed");
          const imageWaiting = isImageNode && node.status === "queued";
          if (hasImage || imageWaiting) {
            estimatedHeight += 120;
          }
          if (node.status === "queued") {
            estimatedHeight += 44;
          }
          if (node.failure_reason) {
            estimatedHeight += 60;
          }
          return estimatedHeight;
        };

        const startX = 72;
        const centerY = 216;
        const gapX = 360;
        const itemSpacingY = 72;

        const committed: Array<{ nodeId: string; position: CanvasPoint }> = [];
        const moveGroupId = `auto-layout-${Date.now()}`;

        const calculatedPositions: Record<string, { x: number; y: number }> = {};

        for (const dStr of Object.keys(layers)) {
          const d = Number(dStr);
          const layerNodes = layers[d];
          if (!layerNodes.length) {
            continue;
          }

          const heights = layerNodes.map(node => getNodeHeight(node));
          const totalHeights = heights.reduce((sum, h) => sum + h, 0);
          const totalSpacing = (layerNodes.length - 1) * itemSpacingY;
          const totalLayerHeight = totalHeights + totalSpacing;

          let currentY = centerY - totalLayerHeight / 2;

          for (let i = 0; i < layerNodes.length; i++) {
            const node = layerNodes[i];
            const nodeHeight = heights[i];
            const newX = Math.round((startX + d * gapX) / 36) * 36;
            const newY = Math.round(currentY / 36) * 36;

            calculatedPositions[node.id] = { x: newX, y: newY };
            currentY = newY + nodeHeight + itemSpacingY;
          }
        }

        const changedCandidates = nodes.map(node => {
          const pos = calculatedPositions[node.id];
          if (!pos) {
            return null;
          }
          if (node.position_x !== pos.x || node.position_y !== pos.y) {
            return { node, newX: pos.x, newY: pos.y };
          }
          return null;
        }).filter((item): item is { node: typeof nodes[number]; newX: number; newY: number } => item !== null);

        if (changedCandidates.length === 0) {
          return;
        }

        changedCandidates.forEach(({ node, newX, newY }) => {
          const mutationVersion = (nodePositionMutationVersionsRef.current[node.id] ?? 0) + 1;
          nodePositionMutationVersionsRef.current[node.id] = mutationVersion;

          onNodePositionCommit({
            node,
            position_x: newX,
            position_y: newY,
            mutationVersion,
            moveGroupId,
            moveGroupSize: changedCandidates.length,
          });

          committed.push({
            nodeId: node.id,
            position: { x: newX, y: newY },
          });
        });

        if (committed.length) {
          setOptimisticNodePositions((current) => ({
            ...current,
            ...Object.fromEntries(committed.map((entry) => [entry.nodeId, entry.position])),
          }));
        }
      },
    }),
    [fitNodeIds, onNodePositionCommit, workflow],
  );

  const interactionPolicy = deriveWorkflowCanvasInteractionPolicy({
    compact: mobileCanvasControlsActive,
    mode: mobileInteractionMode,
    locked: structureBusy,
    connectionEditing: true,
  });
  const canDragNodes = interactionPolicy.nodesDraggable;
  const canConnectNodes = interactionPolicy.nodesConnectable;
  const nodeClickCommitDistance = interactionPolicy.nodeClickDistance;

  const buildNodes = useCallback((previousNodes: WorkflowCanvasNode[] = []): WorkflowCanvasNode[] => {
    if (!workflow) {
      return [];
    }
    const previewSelectedNodeIds = new Set<string>();
    const secondarySelectedNodeIds = new Set(selectedNodeIds);

    return workflowToReactFlowNodes(workflow, {
      selectedNodeIds,
      positionOverrides: optimisticNodePositions,
      previousNodes,
      preservePreviousPositionsForNodeIds: draggingNodeIdsRef.current,
    }).map((node) => {
      const workflowNode = node.data.workflowNode;
      return {
        ...node,
        draggable: canDragNodes,
        connectable: canConnectNodes,
        zIndex: secondarySelectedNodeIds.has(node.id) ? 20 : 0,
        data: {
          ...node.data,
          image: getNodeImage(workflowNode),
          primarySelected: node.id === selectedNodeId,
          secondarySelected: secondarySelectedNodeIds.has(node.id) && node.id !== selectedNodeId,
          previewSelected: previewSelectedNodeIds.has(node.id),
          inputHandleLabel,
          outputHandleLabel,
          onSelectNode,
          actionToolbar: getNodeActionToolbar(node.id),
          onNodeAction,
        },
      };
    });
  }, [
    canConnectNodes,
    canDragNodes,
    getNodeActionToolbar,
    getNodeImage,
    inputHandleLabel,
    onNodeAction,
    onSelectNode,
    optimisticNodePositions,
    selectedNodeId,
    selectedNodeIds,
    structureBusy,
    outputHandleLabel,
    workflow,
  ]);

  const nodes = useMemo<WorkflowCanvasNode[]>(() => buildNodes(), [buildNodes]);

  const edges = useMemo<WorkflowCanvasEdge[]>(() => {
    if (!workflow) {
      return [];
    }
    return workflowToReactFlowEdges(workflow).map((edge): WorkflowCanvasEdge => ({
      ...edge,
      data: {
        workflowEdge: edge.data!.workflowEdge,
        ...edge.data,
        deleteLabel: deleteEdgeLabel,
        disabled: structureBusy,
        onDeleteEdge,
      },
    }));
  }, [deleteEdgeLabel, onDeleteEdge, structureBusy, workflow]);

  useEffect(() => {
    const instance = flowInstanceRef.current;
    if (!flowReady || !instance) {
      return;
    }
    instance.setNodes((currentNodes) => buildNodes(currentNodes));
  }, [buildNodes, flowReady]);

  useEffect(() => {
    const instance = flowInstanceRef.current;
    if (!flowReady || !instance) {
      return;
    }
    instance.setEdges(edges);
  }, [edges, flowReady]);

  const captureNodeDragStartPositions = useCallback((nodeIds: string[], activeNode: WorkflowCanvasNode) => {
    const instance = flowInstanceRef.current;
    const startPositions: Record<string, CanvasPoint> = {};
    for (const nodeId of nodeIds) {
      const flowNode = instance?.getNode(nodeId) ?? (activeNode.id === nodeId ? activeNode : null);
      if (flowNode) {
        startPositions[nodeId] = { x: flowNode.position.x, y: flowNode.position.y };
      }
    }
    return startPositions;
  }, []);

  const restoreNodeDragStartPositions = useCallback((startPositions: Record<string, CanvasPoint>) => {
    const instance = flowInstanceRef.current;
    if (!instance) {
      return;
    }
    instance.setNodes((currentNodes) =>
      currentNodes.map((currentNode) => {
        const startPosition = startPositions[currentNode.id];
        return startPosition ? { ...currentNode, position: { ...startPosition } } : currentNode;
      }),
    );
  }, []);

  const clearActiveNodeDragSession = useCallback(() => {
    draggingNodeIdsRef.current = [];
    pendingNodeDragSelectionRef.current = null;
    nodeDragStartPositionsRef.current = {};
  }, []);

  const completeActiveNodeDragSession = useCallback(() => {
    const pendingSelectionNodeId = pendingNodeDragSelectionRef.current;
    clearActiveNodeDragSession();
    if (pendingSelectionNodeId) {
      onNodeDragCompleteSelect(pendingSelectionNodeId);
    }
  }, [clearActiveNodeDragSession, onNodeDragCompleteSelect]);

  const handleNodeDragStart = useCallback<OnNodeDrag<WorkflowCanvasNode>>(
    (event, node, draggedNodes) => {
      const reactFlowDragNodeIds = draggedNodes.length ? draggedNodes.map((draggedNode) => draggedNode.id) : [node.id];
      if ("ctrlKey" in event && (event.ctrlKey || event.metaKey || event.shiftKey)) {
        draggingNodeIdsRef.current = reactFlowDragNodeIds;
        pendingNodeDragSelectionRef.current = null;
        nodeDragStartPositionsRef.current = captureNodeDragStartPositions(reactFlowDragNodeIds, node);
        return;
      }
      const selectedGroup = getNodeDragGroup(node.id);
      const dragGroup = selectedGroup.includes(node.id) ? selectedGroup : [node.id];
      draggingNodeIdsRef.current = dragGroup;
      // Keep parent selection/sidebar updates out of the drag-start frame; they can trigger heavy ProductDetail renders.
      pendingNodeDragSelectionRef.current = node.id;
      nodeDragStartPositionsRef.current = captureNodeDragStartPositions(dragGroup, node);
    },
    [captureNodeDragStartPositions, getNodeDragGroup],
  );

  const commitNodePosition = useCallback(
    (node: WorkflowNode, position: XYPosition, moveGroupId: string, moveGroupSize: number) => {
      const patch = reactFlowPositionToWorkflowPatch(position);
      if (node.position_x === patch.position_x && node.position_y === patch.position_y) {
        return null;
      }
      const mutationVersion = (nodePositionMutationVersionsRef.current[node.id] ?? 0) + 1;
      nodePositionMutationVersionsRef.current[node.id] = mutationVersion;
      onNodePositionCommit({
        node,
        ...patch,
        mutationVersion,
        moveGroupId,
        moveGroupSize,
      });
      return {
        nodeId: node.id,
        position: { x: patch.position_x, y: patch.position_y },
      };
    },
    [onNodePositionCommit],
  );

  const handleNodeDragStop = useCallback<OnNodeDrag<WorkflowCanvasNode>>(
    (_event, node, draggedNodes) => {
      if (!workflow) {
        clearActiveNodeDragSession();
        return;
      }
      const draggedNodeMap = new Map<string, WorkflowCanvasNode>();
      for (const draggedNode of draggedNodes) {
        draggedNodeMap.set(draggedNode.id, draggedNode);
      }
      draggedNodeMap.set(node.id, node);
      const candidateNodeIds = new Set([...draggingNodeIdsRef.current, ...draggedNodeMap.keys()]);
      const workflowNodeMap = new Map(workflow.nodes.map((workflowNode) => [workflowNode.id, workflowNode]));
      const commitCandidates = [...candidateNodeIds]
        .map((nodeId) => {
          const workflowNode = workflowNodeMap.get(nodeId);
          const flowNode = draggedNodeMap.get(nodeId) ?? flowInstanceRef.current?.getNode(nodeId);
          if (workflowNode && flowNode) {
            let position = flowNode.position;
            if (snapToGrid) {
              position = {
                x: Math.round(position.x / 36) * 36,
                y: Math.round(position.y / 36) * 36,
              };
            }
            return { workflowNode, position };
          }
          return null;
        })
        .filter((candidate): candidate is { workflowNode: WorkflowNode; position: XYPosition } => Boolean(candidate));

      if (!commitCandidates.length) {
        completeActiveNodeDragSession();
        return;
      }
      const dragStartPositions = nodeDragStartPositionsRef.current;
      const hasDragStartPositions = Object.keys(dragStartPositions).length > 0;
      const shouldCommitPosition = hasDragStartPositions
        ? shouldCommitNodeDragGroupPosition(
            commitCandidates.map(({ workflowNode, position }) => ({ nodeId: workflowNode.id, position })),
            dragStartPositions,
            viewportRef.current.zoom,
            nodeClickCommitDistance,
          )
        : true;
      if (!shouldCommitPosition) {
        restoreNodeDragStartPositions(dragStartPositions);
        completeActiveNodeDragSession();
        return;
      }
      const changedCommitCandidates = getChangedWorkflowNodePositionCandidates(commitCandidates);
      if (!changedCommitCandidates.length) {
        completeActiveNodeDragSession();
        return;
      }
      nodeDragCommitGroupCounterRef.current += 1;
      const moveGroupId = String(nodeDragCommitGroupCounterRef.current);
      const committed = changedCommitCandidates
        .map(({ workflowNode, position }) =>
          commitNodePosition(workflowNode, position, moveGroupId, changedCommitCandidates.length),
        )
        .filter((entry): entry is { nodeId: string; position: CanvasPoint } => Boolean(entry));

      if (committed.length) {
        setOptimisticNodePositions((current) => ({
          ...current,
          ...Object.fromEntries(committed.map((entry) => [entry.nodeId, entry.position])),
        }));
      }
      completeActiveNodeDragSession();
    },
    [
      clearActiveNodeDragSession,
      commitNodePosition,
      completeActiveNodeDragSession,
      nodeClickCommitDistance,
      restoreNodeDragStartPositions,
      snapToGrid,
      workflow,
    ],
  );

  const handleConnect = useCallback(
    (connection: Connection) => {
      const input = connectionToWorkflowEdgeInput(connection);
      if (!input) {
        return;
      }
      onConnectionCreate({
        sourceNodeId: input.source_node_id,
        targetNodeId: input.target_node_id,
      });
    },
    [onConnectionCreate],
  );

  const handleSelectionEnd = useCallback(() => {
    const selectionSession = selectionBoxSessionRef.current;
    if (!selectionSession) {
      return;
    }
    selectionBoxSessionRef.current = null;
    onSelectionBoxComplete(selectionSession.nodeIds);
  }, [onSelectionBoxComplete]);

  const isValidConnection = useCallback<IsValidConnection<WorkflowCanvasEdge>>(
    (connection) =>
      Boolean(
        connectionToWorkflowEdgeInput({
          source: connection.source,
          target: connection.target,
          sourceHandle: connection.sourceHandle ?? null,
          targetHandle: connection.targetHandle ?? null,
        }),
      ),
    [],
  );

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center text-zinc-400 dark:text-slate-500">
        <span className="h-6 w-6 animate-spin rounded-full border-2 border-zinc-300 border-t-transparent dark:border-slate-600 dark:border-t-transparent" />
      </div>
    );
  }

  if (!workflow) {
    return (
      <div className="flex h-full items-center justify-center text-xs text-zinc-500 dark:text-slate-400">
        {loadFailedLabel}
      </div>
    );
  }

  return (
    <div
      ref={flowWrapperRef}
      className={`h-full touch-none ${
        selectedGroupCount > 1
          ? "pb-[calc(22rem+env(safe-area-inset-bottom))] lg:pb-0"
          : "pb-[calc(13rem+env(safe-area-inset-bottom))] lg:pb-0"
      }`}
    >
      <ReactFlow<WorkflowCanvasNode, WorkflowCanvasEdge>
        defaultNodes={nodes}
        defaultEdges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        defaultViewport={viewportRef.current}
        minZoom={MIN_ZOOM}
        maxZoom={MAX_ZOOM}
        nodesDraggable={canDragNodes}
        nodesConnectable={canConnectNodes}
        nodesFocusable={false}
        edgesFocusable={false}
        edgesReconnectable={false}
        elementsSelectable
        selectNodesOnDrag={interactionPolicy.selectNodesOnDrag}
        panOnDrag={interactionPolicy.panOnDrag}
        panOnScroll={false}
        zoomOnScroll
        zoomOnPinch
        preventScrolling
        noWheelClassName="nowheel"
        selectionOnDrag={interactionPolicy.selectionOnDrag}
        snapToGrid={false}
        snapGrid={[36, 36]}
        selectionKeyCode={interactionPolicy.selectionKeyCode}
        selectionMode={SelectionMode.Partial}
        multiSelectionKeyCode={interactionPolicy.multiSelectionKeyCode}
        panActivationKeyCode={WORKFLOW_CANVAS_PAN_ACTIVATION_KEY_CODE}
        zoomActivationKeyCode={WORKFLOW_CANVAS_ZOOM_ACTIVATION_KEY_CODES}
        deleteKeyCode={null}
        connectOnClick={false}
        connectionMode={ConnectionMode.Strict}
        connectionLineType={ConnectionLineType.Bezier}
        connectionLineStyle={{ stroke: "#2563eb", strokeWidth: 2, strokeDasharray: "6 4" }}
        nodeDragThreshold={interactionPolicy.nodeDragThreshold}
        nodeClickDistance={interactionPolicy.nodeClickDistance}
        noDragClassName="nodrag"
        noPanClassName="nopan"
        autoPanOnConnect
        autoPanOnNodeDrag
        onInit={(instance) => {
          flowInstanceRef.current = instance;
          setFlowReady(true);
        }}
        onNodeDragStart={handleNodeDragStart}
        onNodeDragStop={handleNodeDragStop}
        onConnect={handleConnect}
        isValidConnection={isValidConnection}
        onSelectionStart={() => {
          selectionBoxSessionRef.current = interactionPolicy.canSelectByBox ? { nodeIds: [] } : null;
        }}
        onSelectionEnd={handleSelectionEnd}
        onPaneClick={onBlankClick}
        onBeforeDelete={async () => false}
        ariaLabelConfig={{
          "controls.ariaLabel": canvasControlsLabel,
          "controls.zoomIn.ariaLabel": zoomInLabel,
          "controls.zoomOut.ariaLabel": zoomOutLabel,
          "controls.fitView.ariaLabel": fitViewLabel,
          "minimap.ariaLabel": canvasMiniMapLabel,
        }}
        className="bg-transparent"
      >
        <WorkflowCanvasGrid />
        <WorkflowCanvasSelectionBridge activeSessionRef={selectionBoxSessionRef} />
        <WorkflowCanvasKeyboardBridge
          enabled={keyboardShortcutsActive}
          hasMultiSelection={selectedNodeIds.length > 1}
          onClearSelection={onClearSelection}
        />
        <WorkflowCanvasViewportBridge onViewportChange={recordViewport} onViewportChangeEnd={persistViewport} />
        <WorkflowCanvasControls
          labels={{
            resetZoom: resetZoomLabel,
            fitSelection: fitSelectionLabel,
            controls: canvasControlsLabel,
            snapToGrid: snapToGridLabel,
            autoLayout: autoLayoutLabel,
          }}
          selectedNodeIds={selectedNodeIds}
          onViewportCommit={persistViewport}
          snapToGrid={snapToGrid}
          onToggleSnapToGrid={onToggleSnapToGrid}
          onAutoLayout={onAutoLayout}
          fitViewOptions={{
            padding: FIT_VIEW_PADDING,
            duration: FIT_VIEW_DURATION_MS,
            maxZoom: FIT_VIEW_MAX_ZOOM,
          }}
          normalizeZoom={normalizeWorkflowZoom}
        />
        <MiniMap<WorkflowCanvasNode>
          position="bottom-right"
          className="workflow-canvas-minimap nopan nodrag nowheel hidden lg:block"
          nodeColor={workflowMiniMapNodeColor}
          nodeStrokeColor={workflowMiniMapNodeStrokeColor}
          nodeBorderRadius={8}
          nodeStrokeWidth={3}
          pannable
          zoomable
          ariaLabel={canvasMiniMapLabel}
          offsetScale={8}
        />
      </ReactFlow>
    </div>
  );
});
