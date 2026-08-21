import { describe, expect, it } from "vitest";

import { graphHistoryShortcutAction } from "./GraphCanvasPanel";

describe("graphHistoryShortcutAction", () => {
  it("maps undo and redo to dedicated actions", () => {
    expect(graphHistoryShortcutAction("undo")).toBe("undo");
    expect(graphHistoryShortcutAction("redo")).toBe("redo");
    expect(graphHistoryShortcutAction("delete")).toBeNull();
  });
});
