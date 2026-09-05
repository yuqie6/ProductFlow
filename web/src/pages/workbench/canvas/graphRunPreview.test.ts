import { describe, expect, it, vi } from "vitest";

import {
  dominantPlannedAction,
  failedNodesRunInput,
  plannedActionClassName,
  plannedActionsFromPreview,
  runPreviewPointerHandlers,
} from "./graphRunPreview";

describe("plannedActionsFromPreview", () => {
  it("maps node ids to planned actions", () => {
    expect(plannedActionsFromPreview({
      scope: "node",
      requested_node_id: "image",
      requested_node_ids: [],
      force: false,
      document_action: "complete",
      document_section: "",
      nodes: [
        { node_id: "prompt", title: "提示词", planned_action: "frozen", reason: "已手写" },
        { node_id: "image", title: "主图", planned_action: "generate", reason: "将生成" },
      ],
    })).toEqual({ prompt: "frozen", image: "generate" });
  });
});

describe("dominantPlannedAction", () => {
  it("prefers blocked over generate over frozen over reuse", () => {
    expect(dominantPlannedAction(["reuse", "generate", "frozen"])).toBe("generate");
    expect(dominantPlannedAction(["frozen", "blocked", "generate"])).toBe("blocked");
    expect(dominantPlannedAction([null, undefined])).toBeNull();
  });
});

describe("plannedActionClassName", () => {
  it("returns outline classes for each action", () => {
    expect(plannedActionClassName("generate")).toContain("outline-accent");
    expect(plannedActionClassName("frozen")).toContain("outline-state-frozen");
    expect(plannedActionClassName("blocked")).toContain("outline-state-error");
    expect(plannedActionClassName(null)).toBe("");
  });
});

describe("runPreviewPointerHandlers", () => {
  it("no-ops when the request or show callback is missing", () => {
    expect(runPreviewPointerHandlers({ scope: "graph" }, undefined, () => undefined)).toEqual({});
    expect(runPreviewPointerHandlers(null, () => undefined, () => undefined)).toEqual({});
  });

  it("forwards the same submit input on hover and focus", () => {
    const show = vi.fn();
    const hide = vi.fn();
    const input = { scope: "node" as const, node_id: "image" };
    const handlers = runPreviewPointerHandlers(input, show, hide);
    handlers.onMouseEnter?.();
    handlers.onFocus?.();
    handlers.onMouseLeave?.();
    handlers.onBlur?.();
    expect(show).toHaveBeenCalledTimes(2);
    expect(show).toHaveBeenCalledWith(input);
    expect(hide).toHaveBeenCalledTimes(2);
  });
});

describe("failedNodesRunInput", () => {
  it("builds a selection run from failed node ids", () => {
    expect(failedNodesRunInput({
      node_runs: [
        { status: "failed", node_id: "image-1" },
        { status: "succeeded", node_id: "image-2" },
        { status: "unknown", node_id: "image-unknown" },
        { status: "cancelled", node_id: "image-cancelled" },
        { status: "skipped", node_id: "image-skipped" },
        { status: "running", node_id: "image-running" },
        { status: "queued", node_id: "image-queued" },
        { status: "failed", node_id: null },
      ],
    })).toEqual({ scope: "selection", node_ids: ["image-1"] });
    expect(failedNodesRunInput({ node_runs: [{ status: "succeeded", node_id: "image-1" }] })).toBeNull();
  });
});
