export type CanvasPoint = {
  x: number;
  y: number;
};

export type NodeDragState = {
  nodeId: string;
  pointerId: number;
  offsetX: number;
  offsetY: number;
  currentX: number;
  currentY: number;
};

export type ConnectionDragState = {
  sourceNodeId: string;
  pointerId: number;
  from: CanvasPoint;
  to: CanvasPoint;
};

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
  copyTitle: string;
  copySellingPoints: string;
  copyPosterHeadline: string;
  copyCta: string;
};
