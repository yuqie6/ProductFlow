import { describe, expect, it } from "vitest";

import {
  deriveWorkflowCanvasInteractionPolicy,
  selectWorkflowCanvasNode,
  shouldToggleWorkflowCanvasNodeSelection,
} from "./workflowCanvasInteraction";

describe("shared workflow canvas interaction policy", () => {
  it("preserves the desktop workbench drag, connect, and modifier-selection contract", () => {
    expect(deriveWorkflowCanvasInteractionPolicy({
      compact: false,
      mode: "edit",
      locked: false,
      connectionEditing: true,
    })).toMatchObject({
      nodesDraggable: true,
      nodesConnectable: true,
      selectionOnDrag: false,
      selectNodesOnDrag: false,
      selectionKeyCode: "Shift",
      multiSelectionKeyCode: ["Control", "Meta"],
      panOnDrag: [0],
      canSelectByBox: true,
      nodeDragThreshold: 0,
      nodeClickDistance: 3,
    });
  });

  it("keeps compact browse and select modes stable while reserving dragging for edit mode", () => {
    const browse = deriveWorkflowCanvasInteractionPolicy({
      compact: true,
      mode: "browse",
      locked: false,
      connectionEditing: true,
    });
    const edit = deriveWorkflowCanvasInteractionPolicy({
      compact: true,
      mode: "edit",
      locked: false,
      connectionEditing: true,
    });
    const select = deriveWorkflowCanvasInteractionPolicy({
      compact: true,
      mode: "select",
      locked: false,
      connectionEditing: true,
    });

    expect(browse).toMatchObject({ nodesDraggable: false, nodesConnectable: false });
    expect(edit).toMatchObject({
      nodesDraggable: true,
      nodesConnectable: true,
      nodeDragThreshold: 6,
      nodeClickDistance: 6,
    });
    expect(select).toMatchObject({
      nodesDraggable: false,
      nodesConnectable: false,
      selectionKeyCode: null,
      multiSelectionKeyCode: null,
      canSelectByBox: false,
    });
  });

  it("keeps a locked or read-only graph non-editable without changing its selection policy", () => {
    expect(deriveWorkflowCanvasInteractionPolicy({
      compact: false,
      mode: "edit",
      locked: true,
      connectionEditing: true,
    })).toMatchObject({ nodesDraggable: false, nodesConnectable: false, canSelectByBox: true });
    expect(deriveWorkflowCanvasInteractionPolicy({
      compact: false,
      mode: "edit",
      locked: false,
      connectionEditing: false,
    })).toMatchObject({ nodesDraggable: true, nodesConnectable: false, canSelectByBox: true });
  });

  it("uses the same additive selection semantics for pointer modifiers and compact select mode", () => {
    expect(shouldToggleWorkflowCanvasNodeSelection({
      compact: true,
      mode: "select",
      ctrlKey: false,
      metaKey: false,
      shiftKey: false,
    })).toBe(true);
    expect(shouldToggleWorkflowCanvasNodeSelection({
      compact: false,
      mode: "edit",
      ctrlKey: true,
      metaKey: false,
      shiftKey: false,
    })).toBe(true);
    expect(selectWorkflowCanvasNode(["a"], "b", false)).toEqual(["b"]);
    expect(selectWorkflowCanvasNode(["a"], "b", true)).toEqual(["a", "b"]);
    expect(selectWorkflowCanvasNode(["a", "b"], "a", true)).toEqual(["b"]);
  });
});
