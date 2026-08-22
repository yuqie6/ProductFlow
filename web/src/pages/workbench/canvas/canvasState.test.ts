import { describe, expect, it } from "vitest";

import {
  isWorkflowCanvasViewportCompatible,
  isWorkflowCanvasViewportScopeActive,
  parseStoredWorkflowCanvasViewport,
  workflowCanvasFitMinZoom,
  workflowCanvasViewportStorageKey,
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

  it("ignores viewport updates from a canvas scope that is no longer active", () => {
    expect(isWorkflowCanvasViewportScopeActive(null, null)).toBe(true);
    expect(isWorkflowCanvasViewportScopeActive("group-1", "group-1")).toBe(true);
    expect(isWorkflowCanvasViewportScopeActive("group-1", null)).toBe(false);
    expect(isWorkflowCanvasViewportScopeActive(null, "group-1")).toBe(false);
  });

  it("parses a stored viewport payload and rejects invalid JSON", () => {
    const viewport = {
      x: 12,
      y: 24,
      zoom: 0.8,
      surface_width: 1440,
      surface_height: 900,
    };
    expect(workflowCanvasViewportStorageKey("graph-1")).toContain("graph-1");
    expect(workflowCanvasViewportStorageKey("graph-1", "group-1")).toContain("group-1");
    expect(workflowCanvasViewportStorageKey("graph-1", "group-1")).not.toBe(
      workflowCanvasViewportStorageKey("graph-1"),
    );
    expect(parseStoredWorkflowCanvasViewport(JSON.stringify(viewport))).toEqual(viewport);
    expect(parseStoredWorkflowCanvasViewport("{not-json")).toBeNull();
    expect(parseStoredWorkflowCanvasViewport(JSON.stringify({ x: 1 }))).toBeNull();
  });
});
