import { describe, expect, it } from "vitest";

import {
  emptyWorkflowCanvasState,
  isWorkflowCanvasViewportCompatible,
  parseWorkflowCanvasState,
  reconcileWorkflowCanvasState,
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
      global_viewport: { x: 10, y: -20, zoom: 0.75 },
      folder_viewports: {
        "folder-a": { x: 4, y: 8, zoom: 1.2 },
        invalid: { x: Number.NaN, y: 0, zoom: 1 },
        huge: { x: 0, y: 0, zoom: 10 },
      },
    }));
    expect(state.global_viewport).toEqual({ x: 10, y: -20, zoom: 0.75 });
    expect(state.folder_viewports).toEqual({ "folder-a": { x: 4, y: 8, zoom: 1.2 } });
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
        global_viewport: { x: 0, y: 0, zoom: 1 },
        folder_viewports: {
          deleted: { x: 1, y: 2, zoom: 1 },
          kept: { x: 3, y: 4, zoom: 0.8 },
        },
      },
      ["kept"],
    );
    expect(reconciled.open_folder_id).toBeNull();
    expect(reconciled.folder_viewports).toEqual({ kept: { x: 3, y: 4, zoom: 0.8 } });
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
    expect(isWorkflowCanvasViewportCompatible({ x: 0, y: 0, zoom: 1 }, 1440)).toBe(true);
    expect(isWorkflowCanvasViewportCompatible({ x: 0, y: 0, zoom: 1 }, 390)).toBe(false);
  });

  it("parses optional surface dimensions without rejecting legacy viewport data", () => {
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
  });
});
