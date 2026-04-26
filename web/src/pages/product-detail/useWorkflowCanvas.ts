import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent, WheelEvent as ReactWheelEvent } from "react";

import type { ProductWorkflow, WorkflowNode } from "../../lib/types";
import {
  CANVAS_NODE_PADDING_X,
  CANVAS_NODE_PADDING_Y,
  CANVAS_VIEWPORT_PADDING,
  MAX_ZOOM,
  MIN_ZOOM,
  NODE_HANDLE_Y,
  NODE_MIN_X,
  NODE_MIN_Y,
  NODE_WIDTH,
} from "./constants";
import {
  buildEdgePath,
  isCanvasWheelZoomBlockedTarget,
  isPanePanBlockedTarget,
} from "./canvasUtils";
import type {
  CanvasPoint,
  ConnectionDragState,
  NodeDragState,
  PanePanState,
} from "./types";
import { clamp, readStoredNumber } from "./utils";

const CANVAS_WHEEL_ZOOM_SENSITIVITY = 0.001;
const CANVAS_ZOOM_PRECISION = 10_000;

interface PlannedWheelView {
  zoom: number;
  scrollLeft: number;
  scrollTop: number;
}

export interface NodePositionCommitInput {
  node: WorkflowNode;
  position_x: number;
  position_y: number;
  mutationVersion: number;
}

interface UseWorkflowCanvasOptions {
  workflow: ProductWorkflow | null;
  zoomStorageKey: string;
  structureBusy: boolean;
  onSelectNode: (nodeId: string) => void;
  onNodePositionCommit: (input: NodePositionCommitInput) => void;
  onConnectionCreate: (input: { sourceNodeId: string; targetNodeId: string }) => void;
}

export function normalizeWorkflowZoom(nextZoom: number): number {
  return clamp(Math.round(nextZoom * CANVAS_ZOOM_PRECISION) / CANVAS_ZOOM_PRECISION, MIN_ZOOM, MAX_ZOOM);
}

export function getWheelZoom(previousZoom: number, wheelDelta: number): number {
  return normalizeWorkflowZoom(previousZoom * Math.exp(-wheelDelta * CANVAS_WHEEL_ZOOM_SENSITIVITY));
}

export function getFinalNodeDragPosition(point: CanvasPoint, drag: Pick<NodeDragState, "offsetX" | "offsetY">): CanvasPoint {
  return {
    x: Math.max(NODE_MIN_X, Math.round(point.x - drag.offsetX)),
    y: Math.max(NODE_MIN_Y, Math.round(point.y - drag.offsetY)),
  };
}

export function buildConnectionDragPath(connectionDrag: ConnectionDragState): string {
  const mid = Math.max(80, Math.abs(connectionDrag.to.x - connectionDrag.from.x) / 2);
  return `M ${connectionDrag.from.x} ${connectionDrag.from.y} C ${connectionDrag.from.x + mid} ${connectionDrag.from.y}, ${connectionDrag.to.x - mid} ${connectionDrag.to.y}, ${connectionDrag.to.x} ${connectionDrag.to.y}`;
}

export function useWorkflowCanvas({
  workflow,
  zoomStorageKey,
  structureBusy,
  onSelectNode,
  onNodePositionCommit,
  onConnectionCreate,
}: UseWorkflowCanvasOptions) {
  const canvasScrollRef = useRef<HTMLDivElement | null>(null);
  const canvasRef = useRef<HTMLDivElement | null>(null);
  const plannedWheelViewRef = useRef<PlannedWheelView | null>(null);
  const nodeDragRef = useRef<NodeDragState | null>(null);
  const nodeElementRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const edgePathRefs = useRef<Record<string, SVGPathElement | null>>({});
  const edgeDeleteButtonRefs = useRef<Record<string, HTMLButtonElement | null>>({});
  const nodePositionMutationVersionsRef = useRef<Record<string, number>>({});
  const previousBodyUserSelectRef = useRef<string | null>(null);
  const [nodeDrag, setNodeDrag] = useState<NodeDragState | null>(null);
  const [optimisticNodePositions, setOptimisticNodePositions] = useState<Record<string, CanvasPoint>>({});
  const [connectionDrag, setConnectionDrag] = useState<ConnectionDragState | null>(null);
  const [panePan, setPanePan] = useState<PanePanState | null>(null);
  const [zoom, setZoom] = useState(() => normalizeWorkflowZoom(readStoredNumber(zoomStorageKey, 1)));
  const zoomRef = useRef(zoom);
  const [wheelViewRevision, setWheelViewRevision] = useState(0);

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

  useEffect(() => {
    return () => {
      restoreBodyUserSelect();
    };
  }, []);

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
    const scrollLeft = scrollLeftOverride ?? (plannedViewMatchesZoom ? plannedView.scrollLeft : scrollElement.scrollLeft);
    const scrollTop = scrollTopOverride ?? (plannedViewMatchesZoom ? plannedView.scrollTop : scrollElement.scrollTop);
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

  const updateZoom = (nextZoom: number) => {
    const normalized = normalizeWorkflowZoom(nextZoom);
    zoomRef.current = normalized;
    setZoom(normalized);
    window.localStorage.setItem(zoomStorageKey, String(normalized));
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

  const setEdgeDeleteButtonRef = (edgeId: string, element: HTMLButtonElement | null) => {
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
      const source = workflow.nodes.find((node) => node.id === edge.source_node_id);
      const target = workflow.nodes.find((node) => node.id === edge.target_node_id);
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
    const anchorPoint = getCanvasPoint(event.clientX, event.clientY, previousZoom, previousScrollLeft, previousScrollTop);
    const nextZoom = getWheelZoom(previousZoom, wheelDelta);
    if (nextZoom === previousZoom) {
      return;
    }

    updateZoom(nextZoom);
    plannedWheelViewRef.current = {
      zoom: nextZoom,
      scrollLeft: previousScrollLeft + anchorPoint.x * (nextZoom - previousZoom),
      scrollTop: previousScrollTop + anchorPoint.y * (nextZoom - previousZoom),
    };
    setWheelViewRevision((current) => current + 1);
  };

  const startNodeDrag = (node: WorkflowNode, event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return;
    }
    const actionTarget = event.target instanceof HTMLElement ? event.target.closest("[data-node-action]") : null;
    if (actionTarget) {
      return;
    }
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    disableBodyUserSelect();
    const point = getCanvasPoint(event.clientX, event.clientY);
    onSelectNode(node.id);
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
    applyNodeElementPosition(nextDrag.nodeId, { x: nextDrag.currentX, y: nextDrag.currentY });
    applyConnectedEdgePositions(nextDrag.nodeId);
  };

  const cancelNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    const activeDrag = nodeDragRef.current;
    if (!activeDrag || activeDrag.pointerId !== event.pointerId) {
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
    const finalPosition = getFinalNodeDragPosition(point, activeDrag);
    const dragged = workflow?.nodes.find((node) => node.id === activeDrag.nodeId);
    if (dragged && (dragged.position_x !== finalPosition.x || dragged.position_y !== finalPosition.y)) {
      const mutationVersion = (nodePositionMutationVersionsRef.current[dragged.id] ?? 0) + 1;
      nodePositionMutationVersionsRef.current[dragged.id] = mutationVersion;
      nodeDragRef.current = {
        ...activeDrag,
        currentX: finalPosition.x,
        currentY: finalPosition.y,
      };
      applyNodeElementPosition(dragged.id, finalPosition);
      applyConnectedEdgePositions(dragged.id);
      setOptimisticNodePositions((current) => ({ ...current, [dragged.id]: finalPosition }));
      onNodePositionCommit({
        node: dragged,
        position_x: finalPosition.x,
        position_y: finalPosition.y,
        mutationVersion,
      });
    }
    nodeDragRef.current = null;
    setNodeDrag(null);
  };

  const startConnectionDrag = (node: WorkflowNode, event: ReactPointerEvent<HTMLButtonElement>) => {
    if (structureBusy || event.button !== 0) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    const from = getOutputHandlePoint(node);
    onSelectNode(node.id);
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
    setConnectionDrag((current) => (current && current.pointerId === event.pointerId ? { ...current, to } : current));
  };

  const endConnectionDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (!connectionDrag || connectionDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    const element = document.elementFromPoint(event.clientX, event.clientY);
    const targetElement =
      element instanceof HTMLElement ? element.closest<HTMLElement>("[data-workflow-target-node-id]") : null;
    const nodeElement = element instanceof HTMLElement ? element.closest<HTMLElement>("[data-workflow-node-id]") : null;
    const targetNodeId = targetElement?.dataset.workflowTargetNodeId ?? nodeElement?.dataset.workflowNodeId ?? null;
    if (targetNodeId && targetNodeId !== connectionDrag.sourceNodeId) {
      onConnectionCreate({
        sourceNodeId: connectionDrag.sourceNodeId,
        targetNodeId,
      });
    }
    setConnectionDrag(null);
  };

  const acceptNodePositionMutation = (nodeId: string, mutationVersion: number) =>
    nodePositionMutationVersionsRef.current[nodeId] === mutationVersion;

  const clearOptimisticNodePosition = (nodeId: string) => {
    setOptimisticNodePositions((current) => {
      const next = { ...current };
      delete next[nodeId];
      return next;
    });
  };

  const getCanvasSize = (options: { minWidth: number; minHeight: number }) => {
    const canvasViewportWidth = canvasScrollRef.current?.clientWidth ?? 0;
    const canvasViewportHeight = canvasScrollRef.current?.clientHeight ?? 0;
    return {
      width: Math.max(
        options.minWidth,
        canvasViewportWidth / zoom + CANVAS_VIEWPORT_PADDING,
        ...(workflow?.nodes.map((node) => getRenderedNodePosition(node).x + CANVAS_NODE_PADDING_X) ?? [options.minWidth]),
        connectionDrag ? connectionDrag.to.x + CANVAS_NODE_PADDING_X : 0,
      ),
      height: Math.max(
        options.minHeight,
        canvasViewportHeight / zoom + CANVAS_VIEWPORT_PADDING,
        ...(workflow?.nodes.map((node) => getRenderedNodePosition(node).y + CANVAS_NODE_PADDING_Y) ?? [options.minHeight]),
        connectionDrag ? connectionDrag.to.y + CANVAS_NODE_PADDING_Y : 0,
      ),
    };
  };

  return {
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
    acceptNodePositionMutation,
    clearOptimisticNodePosition,
    restoreBodyUserSelect,
  };
}
