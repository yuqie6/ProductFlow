import type { WorkflowNode, WorkflowNodeType } from "../../lib/types";

export const NODE_WIDTH = 248;
export const NODE_HANDLE_Y = 56;
export const NODE_MIN_X = 24;
export const NODE_MIN_Y = 24;
export const CANVAS_MIN_WIDTH = 1800;
export const CANVAS_MIN_HEIGHT = 1080;
export const CANVAS_NODE_PADDING_X = 720;
export const CANVAS_NODE_PADDING_Y = 480;
export const CANVAS_VIEWPORT_PADDING = 520;

export const NODE_LABELS: Record<WorkflowNodeType, string> = {
  product_context: "商品",
  reference_image: "参考图",
  copy_generation: "文案",
  image_generation: "生图",
};

export const NODE_STATUS_LABELS: Record<WorkflowNode["status"], string> = {
  idle: "未运行",
  queued: "排队中",
  running: "运行中",
  succeeded: "成功",
  failed: "失败",
};

export const ADD_NODE_OPTIONS: Array<{ type: WorkflowNodeType; label: string }> = [
  { type: "reference_image", label: "参考图" },
  { type: "copy_generation", label: "文案" },
  { type: "image_generation", label: "生图" },
];

export const MIN_INSPECTOR_WIDTH = 280;
export const MAX_INSPECTOR_WIDTH = 560;
export const MIN_ZOOM = 0.5;
export const MAX_ZOOM = 1.6;
export const IMAGE_PREVIEW_SURFACE_CLASS_NAME =
  "bg-[linear-gradient(135deg,#fafafa_25%,#f4f4f5_25%,#f4f4f5_50%,#fafafa_50%,#fafafa_75%,#f4f4f5_75%,#f4f4f5_100%)] bg-[length:16px_16px]";
