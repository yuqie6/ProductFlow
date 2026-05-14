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
  buildCanvasRect,
  buildEdgePath,
  isCanvasWheelZoomBlockedTarget,
  isPanePanBlockedTarget,
} from "./canvasUtils";
import { getIntersectingNodeIds } from "./selection";
import type {
  CanvasInteractionMode,
  CanvasPoint,
  CanvasRect,
  ConnectionDragState,
  NodeDragState,
  PanePanState,
  SelectionBoxState,
} from "./types";
import { clamp, readStoredNumber } from "./utils";

const CANVAS_WHEEL_ZOOM_SENSITIVITY = 0.001;
const CANVAS_ZOOM_PRECISION = 10_000;
const NODE_DRAG_START_THRESHOLD_PX = 6;

interface PlannedWheelView {
  zoom: number;
  scrollLeft: number;
  scrollTop: number;
}

interface TouchPointerState {
  pointerId: number;
  clientX: number;
  clientY: number;
}

interface PinchZoomState {
  pointerIds: [number, number];
  startDistance: number;
  startCenter: { x: number; y: number };
  startZoom: number;
  startScrollLeft: number;
  startScrollTop: number;
  anchorPoint: CanvasPoint;
}

interface PendingNodeDragState {
  node: WorkflowNode;
  pointerId: number;
  startClientX: number;
  startClientY: number;
  offsetX: number;
  offsetY: number;
  renderedPosition: CanvasPoint;
  originPositions: Record<string, CanvasPoint>;
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
  onNodeDragStartSelect: (nodeId: string) => void;
  getNodeDragGroup: (nodeId: string) => string[];
  onSelectionBoxComplete: (nodeIds: string[]) => void;
  onNodePositionCommit: (input: NodePositionCommitInput) => void;
  onConnectionCreate: (input: { sourceNodeId: string; targetNodeId: string }) => void;
  mobileInteractionMode?: CanvasInteractionMode;
}

export function normalizeWorkflowZoom(nextZoom: number): number {
  return clamp(Math.round(nextZoom * CANVAS_ZOOM_PRECISION) / CANVAS_ZOOM_PRECISION, MIN_ZOOM, MAX_ZOOM);
}

export function getWheelZoom(previousZoom: number, wheelDelta: number): number {
  return normalizeWorkflowZoom(previousZoom * Math.exp(-wheelDelta * CANVAS_WHEEL_ZOOM_SENSITIVITY));
}

export function getPinchDistance(
  first: Pick<TouchPointerState, "clientX" | "clientY">,
  second: Pick<TouchPointerState, "clientX" | "clientY">,
): number {
  return Math.hypot(second.clientX - first.clientX, second.clientY - first.clientY);
}

export function getPinchCenter(
  first: Pick<TouchPointerState, "clientX" | "clientY">,
  second: Pick<TouchPointerState, "clientX" | "clientY">,
): { x: number; y: number } {
  return {
    x: (first.clientX + second.clientX) / 2,
    y: (first.clientY + second.clientY) / 2,
  };
}

export function getPinchZoom(startZoom: number, startDistance: number, currentDistance: number): number {
  if (startDistance <= 0 || currentDistance <= 0) {
    return normalizeWorkflowZoom(startZoom);
  }
  return normalizeWorkflowZoom(startZoom * (currentDistance / startDistance));
}

export function getAnchoredZoomScroll(input: {
  anchorPoint: CanvasPoint;
  startZoom: number;
  nextZoom: number;
  startScrollLeft: number;
  startScrollTop: number;
  startCenter: { x: number; y: number };
  currentCenter: { x: number; y: number };
}): { scrollLeft: number; scrollTop: number } {
  return {
    scrollLeft:
      input.startScrollLeft +
      input.anchorPoint.x * (input.nextZoom - input.startZoom) +
      input.startCenter.x -
      input.currentCenter.x,
    scrollTop:
      input.startScrollTop +
      input.anchorPoint.y * (input.nextZoom - input.startZoom) +
      input.startCenter.y -
      input.currentCenter.y,
  };
}

export function exceedsNodeDragStartThreshold(input: {
  startClientX: number;
  startClientY: number;
  clientX: number;
  clientY: number;
  threshold?: number;
}): boolean {
  const threshold = input.threshold ?? NODE_DRAG_START_THRESHOLD_PX;
  return Math.hypot(input.clientX - input.startClientX, input.clientY - input.startClientY) >= threshold;
}

export function canStartTouchCanvasEdit(input: {
  pointerType: string;
  interactionMode: CanvasInteractionMode;
}): boolean {
  return input.pointerType !== "touch" && input.pointerType !== "pen" ? true : input.interactionMode === "edit";
}

export function shouldDelayNodeDragStart(pointerType: string): boolean {
  return pointerType === "touch" || pointerType === "pen";
}

export function getFinalNodeDragPosition(point: CanvasPoint, drag: Pick<NodeDragState, "offsetX" | "offsetY">): CanvasPoint {
  return {
    x: Math.max(NODE_MIN_X, Math.round(point.x - drag.offsetX)),
    y: Math.max(NODE_MIN_Y, Math.round(point.y - drag.offsetY)),
  };
}

export function getNodeDragPositions(
  point: CanvasPoint,
  drag: Pick<NodeDragState, "nodeId" | "offsetX" | "offsetY" | "originPositions">,
  options: { round?: boolean } = {},
): Record<string, CanvasPoint> {
  const draggedOrigin = drag.originPositions[drag.nodeId];
  if (!draggedOrigin) {
    return {};
  }
  const rawDeltaX = point.x - drag.offsetX - draggedOrigin.x;
  const rawDeltaY = point.y - drag.offsetY - draggedOrigin.y;
  const minOriginX = Math.min(...Object.values(drag.originPositions).map((position) => position.x));
  const minOriginY = Math.min(...Object.values(drag.originPositions).map((position) => position.y));
  const deltaX = Math.max(NODE_MIN_X - minOriginX, rawDeltaX);
  const deltaY = Math.max(NODE_MIN_Y - minOriginY, rawDeltaY);
  const normalizeCoordinate = options.round === false ? (value: number) => value : Math.round;

  return Object.fromEntries(
    Object.entries(drag.originPositions).map(([nodeId, origin]) => [
      nodeId,
      {
        x: Math.max(NODE_MIN_X, normalizeCoordinate(origin.x + deltaX)),
        y: Math.max(NODE_MIN_Y, normalizeCoordinate(origin.y + deltaY)),
      },
    ]),
  );
}

export function getNodePositionForViewportCenter(center: CanvasPoint): CanvasPoint {
  return {
    x: Math.max(NODE_MIN_X, Math.round(center.x - NODE_WIDTH / 2)),
    y: Math.max(NODE_MIN_Y, Math.round(center.y - 80)),
  };
}

export function buildConnectionDragPath(connectionDrag: ConnectionDragState): string {
  const mid = Math.max(80, Math.abs(connectionDrag.to.x - connectionDrag.from.x) / 2);
  return `M ${connectionDrag.from.x} ${connectionDrag.from.y} C ${connectionDrag.from.x + mid} ${connectionDrag.from.y}, ${connectionDrag.to.x - mid} ${connectionDrag.to.y}, ${connectionDrag.to.x} ${connectionDrag.to.y}`;
}

export function getSelectionBoxRect(selectionBox: SelectionBoxState): CanvasRect {
  return buildCanvasRect(selectionBox.origin, selectionBox.current);
}

export function useWorkflowCanvas({
  workflow,
  zoomStorageKey,
  structureBusy,
  onSelectNode,
  onNodeDragStartSelect,
  getNodeDragGroup,
  onSelectionBoxComplete,
  onNodePositionCommit,
  onConnectionCreate,
  mobileInteractionMode = "browse",
}: UseWorkflowCanvasOptions) {
  const canvasScrollRef = useRef<HTMLDivElement | null>(null);
  const canvasRef = useRef<HTMLDivElement | null>(null);
  const plannedWheelViewRef = useRef<PlannedWheelView | null>(null);
  const nodeDragRef = useRef<NodeDragState | null>(null);
  const pendingNodeDragRef = useRef<PendingNodeDragState | null>(null);
  const activeTouchPointersRef = useRef<Record<number, TouchPointerState>>({});
  const pinchZoomRef = useRef<PinchZoomState | null>(null);
  const nodeElementRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const edgePathRefs = useRef<Record<string, SVGPathElement | null>>({});
  const edgeDeleteButtonRefs = useRef<Record<string, HTMLButtonElement | null>>({});
  const nodePositionMutationVersionsRef = useRef<Record<string, number>>({});
  const previousBodyUserSelectRef = useRef<string | null>(null);
  const [nodeDrag, setNodeDrag] = useState<NodeDragState | null>(null);
  const [optimisticNodePositions, setOptimisticNodePositions] = useState<Record<string, CanvasPoint>>({});
  const [connectionDrag, setConnectionDrag] = useState<ConnectionDragState | null>(null);
  const [panePan, setPanePan] = useState<PanePanState | null>(null);
  const [selectionBox, setSelectionBox] = useState<SelectionBoxState | null>(null);
  const [zoom, setZoom] = useState(() => normalizeWorkflowZoom(readStoredNumber(zoomStorageKey, 1)));
  const zoomRef = useRef(zoom);
  const [pinchZooming, setPinchZooming] = useState(false);
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
    const activeDragPosition = activeDrag?.originPositions[node.id];
    if (activeDragPosition) {
      const draggedOrigin = activeDrag.originPositions[activeDrag.nodeId] ?? activeDragPosition;
      return {
        x: activeDragPosition.x + activeDrag.currentX - draggedOrigin.x,
        y: activeDragPosition.y + activeDrag.currentY - draggedOrigin.y,
      };
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

  const canStartDirectCanvasEdit = (event: ReactPointerEvent<HTMLElement>) =>
    canStartTouchCanvasEdit({
      pointerType: event.pointerType,
      interactionMode: mobileInteractionMode,
    });

  const trackTouchPointer = (event: ReactPointerEvent<HTMLElement>) => {
    if (event.pointerType !== "touch") {
      return;
    }
    activeTouchPointersRef.current[event.pointerId] = {
      pointerId: event.pointerId,
      clientX: event.clientX,
      clientY: event.clientY,
    };
  };

  const removeTouchPointer = (pointerId: number) => {
    delete activeTouchPointersRef.current[pointerId];
  };

  const getTrackedTouchPair = (pointerIds?: [number, number]) => {
    if (pointerIds) {
      const first = activeTouchPointersRef.current[pointerIds[0]];
      const second = activeTouchPointersRef.current[pointerIds[1]];
      return first && second ? ([first, second] as const) : null;
    }
    const pointers = Object.values(activeTouchPointersRef.current).sort(
      (first, second) => first.pointerId - second.pointerId,
    );
    return pointers.length >= 2 ? ([pointers[0], pointers[1]] as const) : null;
  };

  const clearPinchZoom = () => {
    pinchZoomRef.current = null;
    setPinchZooming(false);
  };

  const startPinchZoom = (scrollElement: HTMLDivElement) => {
    const touchPair = getTrackedTouchPair();
    if (!touchPair) {
      return false;
    }
    const [first, second] = touchPair;
    const startDistance = getPinchDistance(first, second);
    if (startDistance <= 0) {
      return false;
    }
    const startCenter = getPinchCenter(first, second);
    const startZoom = zoomRef.current;
    pendingNodeDragRef.current = null;
    nodeDragRef.current = null;
    setNodeDrag(null);
    setConnectionDrag(null);
    setPanePan(null);
    setSelectionBox(null);
    pinchZoomRef.current = {
      pointerIds: [first.pointerId, second.pointerId],
      startDistance,
      startCenter,
      startZoom,
      startScrollLeft: scrollElement.scrollLeft,
      startScrollTop: scrollElement.scrollTop,
      anchorPoint: getCanvasPoint(
        startCenter.x,
        startCenter.y,
        startZoom,
        scrollElement.scrollLeft,
        scrollElement.scrollTop,
      ),
    };
    setPinchZooming(true);
    return true;
  };

  const buildNodeDragFromPending = (pendingDrag: PendingNodeDragState): NodeDragState => ({
    nodeId: pendingDrag.node.id,
    nodeIds: Object.keys(pendingDrag.originPositions),
    pointerId: pendingDrag.pointerId,
    offsetX: pendingDrag.offsetX,
    offsetY: pendingDrag.offsetY,
    currentX: pendingDrag.renderedPosition.x,
    currentY: pendingDrag.renderedPosition.y,
    originPositions: pendingDrag.originPositions,
  });

  const setNodeElementRef = (nodeId: string, element: HTMLDivElement | null) => {
    if (element) {
      nodeElementRefs.current[nodeId] = element;
      return;
    }
    delete nodeElementRefs.current[nodeId];
  };

  const getNodeMeasuredRect = (nodeId: string) => {
    const element = nodeElementRefs.current[nodeId];
    if (!element) {
      return null;
    }
    const rect = element.getBoundingClientRect();
    return {
      width: rect.width / zoomRef.current,
      height: rect.height / zoomRef.current,
    };
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
    trackTouchPointer(event);
    const scrollElement = canvasScrollRef.current;
    if (event.pointerType === "touch" && scrollElement && getTrackedTouchPair()) {
      event.preventDefault();
      event.currentTarget.setPointerCapture(event.pointerId);
      disableBodyUserSelect();
      startPinchZoom(scrollElement);
      return;
    }
    if (
      event.button !== 0 ||
      event.defaultPrevented ||
      nodeDragRef.current ||
      connectionDrag ||
      isPanePanBlockedTarget(event.target)
    ) {
      return;
    }
    if (!scrollElement) {
      return;
    }
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    disableBodyUserSelect();
    if (event.shiftKey) {
      const origin = getCanvasPoint(event.clientX, event.clientY);
      setSelectionBox({
        pointerId: event.pointerId,
        origin,
        current: origin,
      });
      return;
    }
    setPanePan({
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      startScrollLeft: scrollElement.scrollLeft,
      startScrollTop: scrollElement.scrollTop,
    });
  };

  const movePanePan = (event: ReactPointerEvent<HTMLDivElement>) => {
    trackTouchPointer(event);
    const activePinch = pinchZoomRef.current;
    if (activePinch?.pointerIds.includes(event.pointerId)) {
      const scrollElement = canvasScrollRef.current;
      const touchPair = getTrackedTouchPair(activePinch.pointerIds);
      if (!scrollElement || !touchPair) {
        return;
      }
      event.preventDefault();
      const [first, second] = touchPair;
      const currentDistance = getPinchDistance(first, second);
      const currentCenter = getPinchCenter(first, second);
      const nextZoom = getPinchZoom(activePinch.startZoom, activePinch.startDistance, currentDistance);
      if (nextZoom !== zoomRef.current) {
        updateZoom(nextZoom);
      }
      plannedWheelViewRef.current = {
        zoom: nextZoom,
        ...getAnchoredZoomScroll({
          anchorPoint: activePinch.anchorPoint,
          startZoom: activePinch.startZoom,
          nextZoom,
          startScrollLeft: activePinch.startScrollLeft,
          startScrollTop: activePinch.startScrollTop,
          startCenter: activePinch.startCenter,
          currentCenter,
        }),
      };
      setWheelViewRevision((current) => current + 1);
      return;
    }
    if (selectionBox?.pointerId === event.pointerId) {
      event.preventDefault();
      setSelectionBox((current) =>
        current && current.pointerId === event.pointerId
          ? {
              ...current,
              current: getCanvasPoint(event.clientX, event.clientY),
            }
          : current,
      );
      return;
    }
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
    const activePinch = pinchZoomRef.current;
    const endedPinchPointer = activePinch?.pointerIds.includes(event.pointerId) ?? false;
    if (event.pointerType === "touch") {
      removeTouchPointer(event.pointerId);
    }
    if (endedPinchPointer) {
      event.preventDefault();
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId);
      }
      clearPinchZoom();
      restoreBodyUserSelect();
      setPanePan(null);
      setSelectionBox(null);
      return;
    }
    if (selectionBox?.pointerId === event.pointerId) {
      event.preventDefault();
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId);
      }
      restoreBodyUserSelect();
      const finalSelectionBox = {
        ...selectionBox,
        current: getCanvasPoint(event.clientX, event.clientY),
      };
      onSelectionBoxComplete(
        workflow
          ? getIntersectingNodeIds(
              workflow.nodes,
              getSelectionBoxRect(finalSelectionBox),
              getRenderedNodePosition,
              getNodeMeasuredRect,
            )
          : [],
      );
      setSelectionBox(null);
      return;
    }
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
    const activePinch = pinchZoomRef.current;
    const cancelledPinchPointer = activePinch?.pointerIds.includes(event.pointerId) ?? false;
    if (event.pointerType === "touch") {
      removeTouchPointer(event.pointerId);
    }
    if (cancelledPinchPointer) {
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId);
      }
      clearPinchZoom();
      restoreBodyUserSelect();
      setPanePan(null);
      setSelectionBox(null);
      return;
    }
    if (selectionBox && selectionBox.pointerId === event.pointerId) {
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId);
      }
      restoreBodyUserSelect();
      setSelectionBox(null);
      return;
    }
    if (panePan && panePan.pointerId !== event.pointerId) {
      return;
    }
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    restoreBodyUserSelect();
    setPanePan(null);
  };

  const leavePanePan = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.pointerType === "touch") {
      removeTouchPointer(event.pointerId);
    }
    const activePinch = pinchZoomRef.current;
    if (activePinch?.pointerIds.includes(event.pointerId)) {
      clearPinchZoom();
      restoreBodyUserSelect();
      setPanePan(null);
      setSelectionBox(null);
      return;
    }
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      return;
    }
    if (selectionBox?.pointerId === event.pointerId) {
      restoreBodyUserSelect();
      setSelectionBox(null);
      return;
    }
    if (panePan?.pointerId === event.pointerId) {
      restoreBodyUserSelect();
      setPanePan(null);
    }
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
    if (!canStartDirectCanvasEdit(event)) {
      return;
    }
    if (event.ctrlKey || event.metaKey || event.shiftKey) {
      return;
    }
    const actionTarget = event.target instanceof HTMLElement ? event.target.closest("[data-node-action]") : null;
    if (actionTarget) {
      return;
    }
    event.currentTarget.setPointerCapture(event.pointerId);
    disableBodyUserSelect();
    const point = getCanvasPoint(event.clientX, event.clientY);
    const renderedPosition = getRenderedNodePosition(node);
    const selectedGroup = getNodeDragGroup(node.id);
    const groupNodeIds = selectedGroup.includes(node.id) ? selectedGroup : [node.id];
    const groupNodes =
      workflow?.nodes.filter((workflowNode) => groupNodeIds.includes(workflowNode.id)) ?? [node];
    const originPositions = Object.fromEntries(
      groupNodes.map((workflowNode) => [
        workflowNode.id,
        getRenderedNodePosition(workflowNode),
      ]),
    );
    const pendingDrag = {
      node,
      pointerId: event.pointerId,
      startClientX: event.clientX,
      startClientY: event.clientY,
      offsetX: point.x - renderedPosition.x,
      offsetY: point.y - renderedPosition.y,
      renderedPosition,
      originPositions,
    };
    if (shouldDelayNodeDragStart(event.pointerType)) {
      pendingNodeDragRef.current = pendingDrag;
      return;
    }
    onNodeDragStartSelect(node.id);
    const nextDrag = buildNodeDragFromPending(pendingDrag);
    nodeDragRef.current = nextDrag;
    setNodeDrag(nextDrag);
  };

  const moveNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (pinchZoomRef.current) {
      return;
    }
    const pendingDrag = pendingNodeDragRef.current;
    if (pendingDrag?.pointerId === event.pointerId) {
      if (
        !exceedsNodeDragStartThreshold({
          startClientX: pendingDrag.startClientX,
          startClientY: pendingDrag.startClientY,
          clientX: event.clientX,
          clientY: event.clientY,
        })
      ) {
        return;
      }
      event.preventDefault();
      onNodeDragStartSelect(pendingDrag.node.id);
      const nextDrag = buildNodeDragFromPending(pendingDrag);
      pendingNodeDragRef.current = null;
      nodeDragRef.current = nextDrag;
      setNodeDrag(nextDrag);
    }
    const activeDrag = nodeDragRef.current;
    if (!activeDrag || activeDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    const point = getCanvasPoint(event.clientX, event.clientY);
    const nextPositions = getNodeDragPositions(point, activeDrag, { round: false });
    const draggedPosition = nextPositions[activeDrag.nodeId] ?? {
      x: activeDrag.currentX,
      y: activeDrag.currentY,
    };
    const nextDrag = {
      ...activeDrag,
      currentX: draggedPosition.x,
      currentY: draggedPosition.y,
    };
    nodeDragRef.current = nextDrag;
    for (const [nodeId, position] of Object.entries(nextPositions)) {
      applyNodeElementPosition(nodeId, position);
    }
    for (const nodeId of activeDrag.nodeIds) {
      applyConnectedEdgePositions(nodeId);
    }
  };

  const cancelNodeDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    const pendingDrag = pendingNodeDragRef.current;
    if (pendingDrag?.pointerId === event.pointerId) {
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId);
      }
      restoreBodyUserSelect();
      pendingNodeDragRef.current = null;
      return;
    }
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
    const pendingDrag = pendingNodeDragRef.current;
    if (pendingDrag?.pointerId === event.pointerId) {
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId);
      }
      restoreBodyUserSelect();
      pendingNodeDragRef.current = null;
      return;
    }
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
    const finalPositions = getNodeDragPositions(point, activeDrag);
    const draggedPosition = finalPositions[activeDrag.nodeId];
    if (draggedPosition) {
      nodeDragRef.current = {
        ...activeDrag,
        currentX: draggedPosition.x,
        currentY: draggedPosition.y,
      };
    }
    const movedEntries =
      workflow?.nodes
        .map((workflowNode) => ({ node: workflowNode, position: finalPositions[workflowNode.id] }))
        .filter(
          (entry): entry is { node: WorkflowNode; position: CanvasPoint } =>
            Boolean(entry.position) &&
            (entry.node.position_x !== entry.position.x || entry.node.position_y !== entry.position.y),
        ) ?? [];
    if (movedEntries.length) {
      const nextOptimisticPositions: Record<string, CanvasPoint> = {};
      for (const { node: movedNode, position } of movedEntries) {
        const mutationVersion = (nodePositionMutationVersionsRef.current[movedNode.id] ?? 0) + 1;
        nodePositionMutationVersionsRef.current[movedNode.id] = mutationVersion;
        applyNodeElementPosition(movedNode.id, position);
        nextOptimisticPositions[movedNode.id] = position;
        onNodePositionCommit({
          node: movedNode,
          position_x: position.x,
          position_y: position.y,
          mutationVersion,
        });
      }
      for (const { node: movedNode } of movedEntries) {
        applyConnectedEdgePositions(movedNode.id);
      }
      setOptimisticNodePositions((current) => ({ ...current, ...nextOptimisticPositions }));
    }
    nodeDragRef.current = null;
    setNodeDrag(null);
  };

  const startConnectionDrag = (node: WorkflowNode, event: ReactPointerEvent<HTMLButtonElement>) => {
    if (structureBusy || event.button !== 0) {
      return;
    }
    if (!canStartDirectCanvasEdit(event)) {
      return;
    }
    trackTouchPointer(event);
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
    trackTouchPointer(event);
    event.preventDefault();
    const to = getCanvasPoint(event.clientX, event.clientY);
    setConnectionDrag((current) => (current && current.pointerId === event.pointerId ? { ...current, to } : current));
  };

  const endConnectionDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (event.pointerType === "touch") {
      removeTouchPointer(event.pointerId);
    }
    if (!connectionDrag || connectionDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
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

  const cancelConnectionDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (event.pointerType === "touch") {
      removeTouchPointer(event.pointerId);
    }
    if (!connectionDrag || connectionDrag.pointerId !== event.pointerId) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    setConnectionDrag(null);
  };

  const getViewportCenterNodePosition = (): CanvasPoint => {
    const scrollElement = canvasScrollRef.current;
    const canvasElement = canvasRef.current;
    if (!scrollElement || !canvasElement) {
      return { x: 120, y: 120 };
    }
    const scrollRect = scrollElement.getBoundingClientRect();
    const center = getCanvasPoint(scrollRect.left + scrollRect.width / 2, scrollRect.top + scrollRect.height / 2);
    return getNodePositionForViewportCenter(center);
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

  const selectionBoxRect = selectionBox ? getSelectionBoxRect(selectionBox) : null;
  const previewSelectedNodeIds =
    workflow && selectionBoxRect
      ? getIntersectingNodeIds(workflow.nodes, selectionBoxRect, getRenderedNodePosition, getNodeMeasuredRect)
      : [];

  return {
    canvasScrollRef,
    canvasRef,
    zoom,
    nodeDrag,
    connectionDrag,
    panePan,
    pinchZooming,
    selectionBoxRect,
    previewSelectedNodeIds,
    updateZoom,
    getRenderedNodePosition,
    getOutputHandlePoint,
    getInputHandlePoint,
    getCanvasSize,
    getViewportCenterNodePosition,
    setNodeElementRef,
    setEdgePathRef,
    setEdgeDeleteButtonRef,
    startPanePan,
    movePanePan,
    endPanePan,
    cancelPanePan,
    leavePanePan,
    handleCanvasWheel,
    startNodeDrag,
    moveNodeDrag,
    endNodeDrag,
    cancelNodeDrag,
    startConnectionDrag,
    moveConnectionDrag,
    endConnectionDrag,
    cancelConnectionDrag,
    acceptNodePositionMutation,
    clearOptimisticNodePosition,
    restoreBodyUserSelect,
  };
}
