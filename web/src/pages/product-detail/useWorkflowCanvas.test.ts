import { describe, expect, it } from "vitest";

import {
  buildConnectionDragPath,
  canStartTouchCanvasEdit,
  exceedsNodeDragStartThreshold,
  getAnchoredZoomScroll,
  getFinalNodeDragPosition,
  getNodeDragPositions,
  getNodePositionForViewportCenter,
  getPinchCenter,
  getPinchDistance,
  getPinchZoom,
  getWheelZoom,
  normalizeWorkflowZoom,
  shouldDelayNodeDragStart,
} from "./useWorkflowCanvas";

const baseConnectionDrag = {
  sourceNodeId: "source",
  pointerId: 1,
  from: { x: 10, y: 20 },
  to: { x: 250, y: 80 },
};

describe("workflow canvas pure helpers", () => {
  it("normalizes zoom precision and clamps to configured bounds", () => {
    expect(normalizeWorkflowZoom(0.1)).toBe(0.5);
    expect(normalizeWorkflowZoom(2)).toBe(1.6);
    expect(normalizeWorkflowZoom(1.234567)).toBe(1.2346);
  });

  it("calculates anchored wheel zoom direction without exceeding bounds", () => {
    expect(getWheelZoom(1, 120)).toBeLessThan(1);
    expect(getWheelZoom(1, -120)).toBeGreaterThan(1);
    expect(getWheelZoom(0.5, 10_000)).toBe(0.5);
    expect(getWheelZoom(1.6, -10_000)).toBe(1.6);
  });

  it("calculates pinch distance, center, zoom, and anchored scroll", () => {
    const first = { clientX: 100, clientY: 120 };
    const second = { clientX: 220, clientY: 280 };
    expect(getPinchDistance(first, second)).toBe(200);
    expect(getPinchCenter(first, second)).toEqual({ x: 160, y: 200 });
    expect(getPinchZoom(1, 200, 260)).toBe(1.3);
    expect(getPinchZoom(1, 200, 40)).toBe(0.5);
    expect(getPinchZoom(1.4, 200, 400)).toBe(1.6);
    expect(getPinchZoom(1.2, 0, 200)).toBe(1.2);
    expect(
      getAnchoredZoomScroll({
        anchorPoint: { x: 300, y: 180 },
        startZoom: 1,
        nextZoom: 1.25,
        startScrollLeft: 40,
        startScrollTop: 70,
        startCenter: { x: 160, y: 200 },
        currentCenter: { x: 150, y: 215 },
      }),
    ).toEqual({ scrollLeft: 125, scrollTop: 100 });
  });

  it("gates touch and pen canvas edits behind explicit edit mode", () => {
    expect(canStartTouchCanvasEdit({ pointerType: "mouse", interactionMode: "browse" })).toBe(true);
    expect(canStartTouchCanvasEdit({ pointerType: "touch", interactionMode: "browse" })).toBe(false);
    expect(canStartTouchCanvasEdit({ pointerType: "pen", interactionMode: "select" })).toBe(false);
    expect(canStartTouchCanvasEdit({ pointerType: "touch", interactionMode: "edit" })).toBe(true);
  });

  it("keeps desktop mouse node dragging immediate while delaying touch-like drags", () => {
    expect(shouldDelayNodeDragStart("mouse")).toBe(false);
    expect(shouldDelayNodeDragStart("")).toBe(false);
    expect(shouldDelayNodeDragStart("touch")).toBe(true);
    expect(shouldDelayNodeDragStart("pen")).toBe(true);
  });

  it("uses a drag threshold so taps do not become node drags", () => {
    expect(
      exceedsNodeDragStartThreshold({
        startClientX: 10,
        startClientY: 10,
        clientX: 13,
        clientY: 14,
      }),
    ).toBe(false);
    expect(
      exceedsNodeDragStartThreshold({
        startClientX: 10,
        startClientY: 10,
        clientX: 16,
        clientY: 10,
      }),
    ).toBe(true);
  });

  it("rounds final drag positions and keeps nodes inside the canvas minimum", () => {
    expect(getFinalNodeDragPosition({ x: 54.6, y: 75.4 }, { offsetX: 10.2, offsetY: 20.8 })).toEqual({
      x: 44,
      y: 55,
    });
    expect(getFinalNodeDragPosition({ x: 12, y: 12 }, { offsetX: 30, offsetY: 40 })).toEqual({
      x: 24,
      y: 24,
    });
  });

  it("moves selected node groups by a shared rounded delta", () => {
    expect(
      getNodeDragPositions(
        { x: 177.4, y: 230.8 },
        {
          nodeId: "a",
          offsetX: 7.4,
          offsetY: 10.8,
          originPositions: {
            a: { x: 100, y: 120 },
            b: { x: 360, y: 240 },
          },
        },
      ),
    ).toEqual({
      a: { x: 170, y: 220 },
      b: { x: 430, y: 340 },
    });
  });

  it("clamps selected node groups without changing their relative spacing", () => {
    expect(
      getNodeDragPositions(
        { x: -100, y: -80 },
        {
          nodeId: "a",
          offsetX: 10,
          offsetY: 10,
          originPositions: {
            a: { x: 100, y: 120 },
            b: { x: 40, y: 70 },
          },
        },
      ),
    ).toEqual({
      a: { x: 84, y: 74 },
      b: { x: 24, y: 24 },
    });
  });

  it("positions new nodes around the current viewport center", () => {
    expect(getNodePositionForViewportCenter({ x: 640, y: 360 })).toEqual({ x: 516, y: 280 });
    expect(getNodePositionForViewportCenter({ x: 60, y: 70 })).toEqual({ x: 24, y: 24 });
  });

  it("builds the temporary connection drag path with the wider midpoint rule", () => {
    expect(buildConnectionDragPath(baseConnectionDrag)).toBe("M 10 20 C 130 20, 130 80, 250 80");
    expect(buildConnectionDragPath({ ...baseConnectionDrag, to: { x: 50, y: 90 } })).toBe(
      "M 10 20 C 90 20, -30 90, 50 90",
    );
  });
});
