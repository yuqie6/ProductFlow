import type { CopyPayloadV2, ImageToolOptions } from "../../lib/types";

export type CanvasPoint = {
  x: number;
  y: number;
};

export type CanvasRect = {
  x: number;
  y: number;
  width: number;
  height: number;
};

export type CanvasInteractionMode = "browse" | "edit" | "select";

export type NodeDragState = {
  nodeId: string;
  nodeIds: string[];
  pointerId: number;
  offsetX: number;
  offsetY: number;
  currentX: number;
  currentY: number;
  originPositions: Record<string, CanvasPoint>;
};

export type ConnectionDragState = {
  sourceNodeId: string;
  pointerId: number;
  from: CanvasPoint;
  to: CanvasPoint;
};

export type PanePanState = {
  pointerId: number;
  startX: number;
  startY: number;
  startScrollLeft: number;
  startScrollTop: number;
};

export type SelectionBoxState = {
  pointerId: number;
  origin: CanvasPoint;
  current: CanvasPoint;
};

export type SaveStatus = "idle" | "saving" | "saved" | "failed";

export type NodeConfigDraft = {
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
  toolOptions: ImageToolOptions;
  copyStructuredPayload: CopyPayloadV2 | null;
};
