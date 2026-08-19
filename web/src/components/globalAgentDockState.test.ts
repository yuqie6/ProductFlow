import { describe, expect, it } from "vitest";

import {
  applyDockModeWidth,
  clampBubblePosition,
  clampWindowPosition,
  COMPACT_MODE_MAX_WIDTH,
  defaultBubblePosition,
  defaultWindowPosition,
  MIN_DOCK_HEIGHT,
  MIN_DOCK_WIDTH,
  readDockPoint,
  readDockSize,
  resizeDockWindow,
  resolveDockMode,
  WIDE_MODE_MIN_WIDTH,
} from "./globalAgentDockState";

describe("globalAgentDockState", () => {
  it("resolves dock mode with clamping to compact", () => {
    expect(resolveDockMode("compact")).toBe("compact");
    expect(resolveDockMode("wide")).toBe("wide");
    expect(resolveDockMode("fullscreen")).toBe("fullscreen");
    expect(resolveDockMode(null)).toBe("compact");
    expect(resolveDockMode("huge")).toBe("compact");
    expect(resolveDockMode(123)).toBe("compact");
  });

  it("reads persisted window size with minimum floor and defaults", () => {
    expect(readDockSize(null, null)).toEqual({ width: 480, height: 680 });
    expect(readDockSize("abc", "")).toEqual({ width: 480, height: 680 });
    expect(readDockSize(100, 200)).toEqual({ width: MIN_DOCK_WIDTH, height: MIN_DOCK_HEIGHT });
    expect(readDockSize("900", "700")).toEqual({ width: 900, height: 700 });
  });

  it("reads persisted points and discards malformed JSON", () => {
    expect(readDockPoint(null)).toBeNull();
    expect(readDockPoint("not json")).toBeNull();
    expect(readDockPoint('{"x":"bad","y":1}')).toBeNull();
    expect(readDockPoint('{"x":10,"y":20}')).toEqual({ x: 10, y: 20 });
  });

  it("computes default and clamped bubble positions within the viewport", () => {
    expect(defaultBubblePosition(1280, 720)).toEqual({ x: 1280 - 68, y: 720 - 68 });
    const clamped = clampBubblePosition({ x: 5, y: 5 }, 1280, 720);
    expect(clamped.x).toBeGreaterThanOrEqual(12);
    expect(clamped.y).toBeGreaterThanOrEqual(12);
    const maxClamped = clampBubblePosition({ x: 99999, y: 99999 }, 1280, 720);
    expect(maxClamped.x).toBeLessThanOrEqual(1280 - 60);
    expect(maxClamped.y).toBeLessThanOrEqual(720 - 60);
  });

  it("computes default and clamped window positions within the viewport", () => {
    const size = { width: 480, height: 680 };
    expect(defaultWindowPosition(1280, 900, size)).toEqual({ x: 1280 - 480 - 24, y: 900 - 680 - 24 });
    const clamped = clampWindowPosition({ x: -50, y: -50 }, size, 1280, 900);
    expect(clamped).toEqual({ x: 8, y: 8 });
    const maxY = clampWindowPosition({ x: 10, y: 99999 }, size, 1280, 900);
    expect(maxY.y).toBeLessThanOrEqual(900 - 680 - 8);
  });

  it("applies mode-specific width constraints", () => {
    expect(applyDockModeWidth({ width: 480, height: 680 }, "compact").width).toBeLessThanOrEqual(COMPACT_MODE_MAX_WIDTH);
    expect(applyDockModeWidth({ width: 500, height: 680 }, "wide").width).toBe(WIDE_MODE_MIN_WIDTH);
    expect(applyDockModeWidth({ width: 900, height: 680 }, "wide")).toEqual({ width: 900, height: 680 });
    expect(applyDockModeWidth({ width: 480, height: 680 }, "fullscreen")).toEqual({ width: 480, height: 680 });
  });

  it("resizes east/south directions and clamps to viewport max", () => {
    const start = { size: { width: 480, height: 680 }, pos: { x: 100, y: 100 } };
    const grown = resizeDockWindow(start.size, start.pos, "se", 120, 100, 1280, 900);
    expect(grown.size).toEqual({ width: 600, height: 780 });
    expect(grown.pos).toEqual({ x: 100, y: 100 });

    const capped = resizeDockWindow(start.size, start.pos, "se", 5000, 5000, 1280, 900);
    expect(capped.size.width).toBeLessThanOrEqual(1280 - 24);
    expect(capped.size.width).toBeLessThanOrEqual(1280 - capped.pos.x - 8);
    expect(capped.size.height).toBeLessThanOrEqual(900 - 24);
  });

  it("resizes north/west directions adjusting the anchor position", () => {
    const start = { size: { width: 600, height: 700 }, pos: { x: 200, y: 150 } };
    const result = resizeDockWindow(start.size, start.pos, "nw", 100, 80, 1280, 900);
    // west drag right (+x) shrinks width, moving x right; north drag down (+y) shrinks height, moving y down
    expect(result.size.width).toBe(500);
    expect(result.size.width).toBeLessThanOrEqual(start.size.width);
    expect(result.pos.x).toBeGreaterThanOrEqual(start.pos.x);
    expect(result.pos.y).toBeGreaterThanOrEqual(start.pos.y);
  });

  it("never shrinks below the minimum window size", () => {
    const start = { size: { width: 480, height: 680 }, pos: { x: 100, y: 100 } };
    const result = resizeDockWindow(start.size, start.pos, "se", -5000, -5000, 1280, 900);
    expect(result.size.width).toBe(MIN_DOCK_WIDTH);
    expect(result.size.height).toBe(MIN_DOCK_HEIGHT);
  });
});
