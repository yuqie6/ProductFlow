/**
 * Pure persisted-state and geometry helpers for the global agent dock.
 *
 * The dock window/bubble is draggable and resizable and persists its layout to
 * localStorage. All clamping, defaults, and persistence formats live here as
 * pure functions so the interactive behaviors are deterministically testable
 * without a browser DOM (the repo's test environment is SSR-style, no jsdom).
 */

export type GlobalAgentDockMode = "compact" | "wide" | "fullscreen";
export type ResizeDirection = "n" | "s" | "e" | "w" | "nw" | "ne" | "sw" | "se";

export interface DockPoint {
  x: number;
  y: number;
}

export interface DockSize {
  width: number;
  height: number;
}

export const DOCK_MODE_STORAGE_KEY = "productflow_agent_dock_mode";
export const DOCK_WIDTH_STORAGE_KEY = "productflow_agent_dock_width";
export const DOCK_HEIGHT_STORAGE_KEY = "productflow_agent_dock_height";
export const DOCK_POSITION_STORAGE_KEY = "productflow_agent_window_pos";
export const BUBBLE_POSITION_STORAGE_KEY = "productflow_agent_bubble_pos";

export const MIN_DOCK_WIDTH = 380;
export const MIN_DOCK_HEIGHT = 460;
export const DEFAULT_DOCK_SIZE: DockSize = { width: 480, height: 680 };
export const WIDE_MODE_MIN_WIDTH = 760;
export const COMPACT_MODE_MAX_WIDTH = 540;
export const WINDOW_MARGIN = 16;
export const WINDOW_EDGE_MIN = 8;
export const BUBBLE_MARGIN = 12;
export const BUBBLE_EDGE = 68;
export const BUBBLE_MIN_EDGE = 60;

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(min, value), max);
}

/** Resolve a stored dock mode, clamping any invalid value back to "compact". */
export function resolveDockMode(raw: unknown): GlobalAgentDockMode {
  return raw === "wide" || raw === "fullscreen" || raw === "compact" ? raw : "compact";
}

/** Read the persisted window size, flooring to the minimum and falling back to defaults. */
export function readDockSize(rawWidth: unknown, rawHeight: unknown): DockSize {
  const width = typeof rawWidth === "number" ? rawWidth : Number(rawWidth);
  const height = typeof rawHeight === "number" ? rawHeight : Number(rawHeight);
  return {
    width: Number.isFinite(width) && width > 0 ? Math.max(MIN_DOCK_WIDTH, width) : DEFAULT_DOCK_SIZE.width,
    height: Number.isFinite(height) && height > 0 ? Math.max(MIN_DOCK_HEIGHT, height) : DEFAULT_DOCK_SIZE.height,
  };
}

/** Read a persisted {x,y} point; invalid/absent JSON yields null (caller uses a default). */
export function readDockPoint(raw: string | null): DockPoint | null {
  if (!raw) {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (typeof parsed === "object" && parsed !== null) {
      const point = parsed as Record<string, unknown>;
      if (typeof point.x === "number" && typeof point.y === "number") {
        return { x: point.x, y: point.y };
      }
    }
  } catch {
    // malformed persisted JSON is discarded, not fatal
  }
  return null;
}

export function defaultBubblePosition(viewportWidth: number, viewportHeight: number): DockPoint {
  return {
    x: Math.max(BUBBLE_MARGIN, viewportWidth - BUBBLE_EDGE),
    y: Math.max(BUBBLE_MARGIN, viewportHeight - BUBBLE_EDGE),
  };
}

export function clampBubblePosition(pos: DockPoint, viewportWidth: number, viewportHeight: number): DockPoint {
  return {
    x: clamp(pos.x, BUBBLE_MARGIN, viewportWidth - BUBBLE_MIN_EDGE),
    y: clamp(pos.y, BUBBLE_MARGIN, viewportHeight - BUBBLE_MIN_EDGE),
  };
}

export function defaultWindowPosition(viewportWidth: number, viewportHeight: number, size: DockSize): DockPoint {
  return {
    x: Math.max(WINDOW_MARGIN, viewportWidth - size.width - 24),
    y: Math.max(WINDOW_MARGIN, viewportHeight - size.height - 24),
  };
}

export function clampWindowPosition(pos: DockPoint, size: DockSize, viewportWidth: number, viewportHeight: number): DockPoint {
  return {
    x: clamp(pos.x, WINDOW_EDGE_MIN, viewportWidth - size.width - WINDOW_EDGE_MIN),
    y: clamp(pos.y, WINDOW_EDGE_MIN, viewportHeight - size.height - WINDOW_EDGE_MIN),
  };
}

/** Apply the mode-specific width constraints used when switching dock modes. */
export function applyDockModeWidth(size: DockSize, mode: GlobalAgentDockMode): DockSize {
  if (mode === "wide") {
    return { ...size, width: Math.max(size.width, WIDE_MODE_MIN_WIDTH) };
  }
  if (mode === "compact") {
    return { ...size, width: Math.min(size.width, COMPACT_MODE_MAX_WIDTH) };
  }
  return size;
}

export interface DockResizeResult {
  size: DockSize;
  pos: DockPoint;
}

/** Compute the next size/position for an 8-direction resize drag within a viewport. */
export function resizeDockWindow(
  startSize: DockSize,
  startPos: DockPoint,
  direction: ResizeDirection,
  deltaX: number,
  deltaY: number,
  viewportWidth: number,
  viewportHeight: number,
): DockResizeResult {
  const maxW = Math.max(MIN_DOCK_WIDTH, viewportWidth - 24);
  const maxH = Math.max(MIN_DOCK_HEIGHT, viewportHeight - 24);

  let nextW = startSize.width;
  let nextH = startSize.height;
  let nextX = startPos.x;
  let nextY = startPos.y;

  if (direction.includes("e")) {
    nextW = clamp(startSize.width + deltaX, MIN_DOCK_WIDTH, maxW);
  }
  if (direction.includes("w")) {
    nextW = clamp(startSize.width - deltaX, MIN_DOCK_WIDTH, maxW);
    nextX = startPos.x + (startSize.width - nextW);
  }
  if (direction.includes("s")) {
    nextH = clamp(startSize.height + deltaY, MIN_DOCK_HEIGHT, maxH);
  }
  if (direction.includes("n")) {
    nextH = clamp(startSize.height - deltaY, MIN_DOCK_HEIGHT, maxH);
    nextY = startPos.y + (startSize.height - nextH);
  }

  nextX = clamp(nextX, WINDOW_EDGE_MIN, viewportWidth - nextW - WINDOW_EDGE_MIN);
  nextY = clamp(nextY, WINDOW_EDGE_MIN, viewportHeight - nextH - WINDOW_EDGE_MIN);

  return {
    size: { width: nextW, height: nextH },
    pos: { x: nextX, y: nextY },
  };
}
