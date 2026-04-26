import type { CanvasPoint } from "./types";

const PANE_PAN_BLOCKED_SELECTORS = [
  "[data-workflow-node-id]",
  "[data-node-action]",
  "[data-workflow-target-node-id]",
  "[data-canvas-control]",
  "button",
  "a",
  "input",
  "textarea",
  "select",
  "label",
  "[role='button']",
].join(",");

const CANVAS_WHEEL_ZOOM_BLOCKED_SELECTORS = [
  "[data-node-action]",
  "[data-workflow-target-node-id]",
  "[data-canvas-control]",
  "button",
  "a",
  "input",
  "textarea",
  "select",
  "label",
  "[role='button']",
].join(",");

export function buildEdgePath(start: CanvasPoint, end: CanvasPoint): string {
  const mid = Math.max(50, Math.abs(end.x - start.x) / 2);
  return `M ${start.x} ${start.y} C ${start.x + mid} ${start.y}, ${end.x - mid} ${end.y}, ${end.x} ${end.y}`;
}

export function isPanePanBlockedTarget(target: EventTarget | null): boolean {
  if (typeof HTMLElement === "undefined" || !(target instanceof HTMLElement)) {
    return false;
  }
  return Boolean(target.closest(PANE_PAN_BLOCKED_SELECTORS));
}

export function isCanvasWheelZoomBlockedTarget(target: EventTarget | null): boolean {
  if (typeof HTMLElement === "undefined" || !(target instanceof HTMLElement)) {
    return false;
  }
  return Boolean(target.closest(CANVAS_WHEEL_ZOOM_BLOCKED_SELECTORS));
}
