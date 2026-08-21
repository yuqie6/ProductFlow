import { describe, expect, it } from "vitest";

import { graphHistoryShortcutAction } from "./GraphCanvasPanel";

describe("graphHistoryShortcutAction", () => {
  it("maps undo only; redo does not replay undo", () => {
    expect(graphHistoryShortcutAction("undo")).toBe("undo");
    expect(graphHistoryShortcutAction("redo")).toBeNull();
    expect(graphHistoryShortcutAction("delete")).toBeNull();
  });
});
