import { describe, expect, it } from "vitest";

import {
  emptyWorkflowCanvasState,
  isWorkflowCanvasViewportCompatible,
  parseWorkflowCanvasState,
  reconcileWorkflowCanvasState,
  workflowCanvasFitMinZoom,
  workflowCanvasStateStorageKey,
} from "./canvasState";

describe("schema-v2 workflow canvas preference", () => {
  it("uses workflow-scoped keys and accepts only finite bounded viewports", () => {
    expect(workflowCanvasStateStorageKey("workflow-a")).not.toBe(
      workflowCanvasStateStorageKey("workflow-b"),
    );
    const state = parseWorkflowCanvasState(JSON.stringify({
      schema_version: 1,
      open_folder_id: "folder-a",
      global_viewport: { x: 10, y: -20, zoom: 0.75, surface_width: 1440, surface_height: 900 },
      folder_viewports: {
        "folder-a": { x: 4, y: 8, zoom: 1.2, surface_width: 1440, surface_height: 900 },
        invalid: { x: Number.NaN, y: 0, zoom: 1, surface_width: 1440, surface_height: 900 },
        huge: { x: 0, y: 0, zoom: 10, surface_width: 1440, surface_height: 900 },
      },
    }));
    expect(state.global_viewport).toEqual({
      x: 10,
      y: -20,
      zoom: 0.75,
      surface_width: 1440,
      surface_height: 900,
    });
    expect(state.folder_viewports).toEqual({
      "folder-a": { x: 4, y: 8, zoom: 1.2, surface_width: 1440, surface_height: 900 },
    });
  });

  it("falls back for malformed or stale state and removes deleted folders", () => {
    expect(parseWorkflowCanvasState("not-json")).toEqual(emptyWorkflowCanvasState());
    expect(parseWorkflowCanvasState(JSON.stringify({ schema_version: 2 }))).toEqual(
      emptyWorkflowCanvasState(),
    );
    const reconciled = reconcileWorkflowCanvasState(
      {
        schema_version: 1,
        open_folder_id: "deleted",
        global_viewport: { x: 0, y: 0, zoom: 1, surface_width: 1440, surface_height: 900 },
        folder_viewports: {
          deleted: { x: 1, y: 2, zoom: 1, surface_width: 1440, surface_height: 900 },
          kept: { x: 3, y: 4, zoom: 0.8, surface_width: 1440, surface_height: 900 },
        },
      },
      ["kept"],
    );
    expect(reconciled.open_folder_id).toBeNull();
    expect(reconciled.folder_viewports).toEqual({
      kept: { x: 3, y: 4, zoom: 0.8, surface_width: 1440, surface_height: 900 },
    });
  });

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

  it("requires surface dimensions when restoring viewport data", () => {
    const state = parseWorkflowCanvasState(JSON.stringify({
      schema_version: 1,
      open_folder_id: null,
      global_viewport: {
        x: 1,
        y: 2,
        zoom: 0.8,
        surface_width: 1440,
        surface_height: 900,
      },
      folder_viewports: {},
    }));
    expect(state.global_viewport).toEqual({
      x: 1,
      y: 2,
      zoom: 0.8,
      surface_width: 1440,
      surface_height: 900,
    });
    expect(parseWorkflowCanvasState(JSON.stringify({
      schema_version: 1,
      open_folder_id: null,
      global_viewport: { x: 1, y: 2, zoom: 0.8 },
      folder_viewports: {},
    })).global_viewport).toBeNull();
  });

  it("allows compact canvases to fit the complete graph without changing desktop readability", () => {
    expect(workflowCanvasFitMinZoom(390)).toBe(0.24);
    expect(workflowCanvasFitMinZoom(600)).toBe(0.32);
    expect(workflowCanvasFitMinZoom(889)).toBe(0.55);
  });
});
