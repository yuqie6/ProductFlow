import { describe, expect, it } from "vitest";

import {
  isWorkflowCanvasViewportCompatible,
  workflowCanvasFitMinZoom,
} from "./canvasState";

describe("workflow canvas viewport", () => {
  it("restores a viewport only for a compatible responsive layout", () => {
    const desktopViewport = {
      x: 10,
      y: 20,
      zoom: 0.5,
      surface_width: 1440,
      surface_height: 1000,
    };
    expect(isWorkflowCanvasViewportCompatible(desktopViewport, 1366)).toBe(true);
    expect(isWorkflowCanvasViewportCompatible(desktopViewport, 390)).toBe(false);
    expect(isWorkflowCanvasViewportCompatible({
      x: 0,
      y: 0,
      zoom: 1,
      surface_width: 1440,
      surface_height: 900,
    }, 1440)).toBe(true);
    expect(isWorkflowCanvasViewportCompatible({
      x: 0,
      y: 0,
      zoom: 1,
      surface_width: 1440,
      surface_height: 900,
    }, 390)).toBe(false);
  });

  it("allows compact canvases to fit the complete graph without changing desktop readability", () => {
    expect(workflowCanvasFitMinZoom(390)).toBe(0.24);
    expect(workflowCanvasFitMinZoom(600)).toBe(0.32);
    expect(workflowCanvasFitMinZoom(889)).toBe(0.55);
  });
});
