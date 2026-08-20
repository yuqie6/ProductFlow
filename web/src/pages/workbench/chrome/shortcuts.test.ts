import { describe, expect, it } from "vitest";

import {
  getSelectedWorkflowShortcutNodeIds,
  getWorkflowKeyboardShortcut,
  isWorkflowShortcutBlockedTarget,
} from "./shortcuts";

function keyboardEvent(overrides: Partial<KeyboardEvent> & Pick<KeyboardEvent, "key">): KeyboardEvent {
  return {
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    altKey: false,
    defaultPrevented: false,
    target: null,
    ...overrides,
  } as KeyboardEvent;
}

function shortcutTarget(target: object): EventTarget {
  return target as unknown as EventTarget;
}

describe("workflow keyboard shortcut helpers", () => {
  it("ignores shortcuts from input-like and editable targets", () => {
    expect(isWorkflowShortcutBlockedTarget(shortcutTarget({ tagName: "INPUT" }))).toBe(true);
    expect(isWorkflowShortcutBlockedTarget(shortcutTarget({ tagName: "textarea" }))).toBe(true);
    expect(isWorkflowShortcutBlockedTarget(shortcutTarget({ tagName: "SELECT" }))).toBe(true);
    expect(isWorkflowShortcutBlockedTarget(shortcutTarget({ tagName: "button" }))).toBe(true);
    expect(isWorkflowShortcutBlockedTarget(shortcutTarget({ tagName: "A" }))).toBe(true);
    expect(isWorkflowShortcutBlockedTarget(shortcutTarget({ tagName: "LABEL" }))).toBe(true);
    expect(isWorkflowShortcutBlockedTarget(shortcutTarget({ isContentEditable: true }))).toBe(true);
    expect(
      isWorkflowShortcutBlockedTarget(shortcutTarget({
        closest: (selector: string) => (selector.includes("input") ? {} : null),
      })),
    ).toBe(true);
  });

  it("ignores shortcuts from nested anchors and role buttons", () => {
    expect(
      isWorkflowShortcutBlockedTarget(shortcutTarget({
        closest: (selector: string) => (selector.split(",").includes("a") ? {} : null),
      })),
    ).toBe(true);
    expect(
      isWorkflowShortcutBlockedTarget(shortcutTarget({
        closest: (selector: string) => (selector.includes("[role='button']") ? {} : null),
      })),
    ).toBe(true);
    expect(
      getWorkflowKeyboardShortcut(
        keyboardEvent({
          key: "c",
          ctrlKey: true,
          target: shortcutTarget({ tagName: "A" }),
        }),
      ),
    ).toBeNull();
  });

  it("maps canvas shortcuts while ignoring ordinary keys", () => {
    expect(getWorkflowKeyboardShortcut(keyboardEvent({ key: "c", ctrlKey: true }))).toBe("copy");
    expect(getWorkflowKeyboardShortcut(keyboardEvent({ key: "v", metaKey: true }))).toBe("paste");
    expect(getWorkflowKeyboardShortcut(keyboardEvent({ key: "d", ctrlKey: true }))).toBe("duplicate");
    expect(getWorkflowKeyboardShortcut(keyboardEvent({ key: "Delete" }))).toBe("delete");
    expect(getWorkflowKeyboardShortcut(keyboardEvent({ key: "Backspace" }))).toBe("delete");
    expect(getWorkflowKeyboardShortcut(keyboardEvent({ key: "z", metaKey: true }))).toBe("undo");
    expect(getWorkflowKeyboardShortcut(keyboardEvent({ key: "z", metaKey: true, shiftKey: true }))).toBe("redo");
    expect(getWorkflowKeyboardShortcut(keyboardEvent({ key: "y", ctrlKey: true }))).toBe("redo");
    expect(getWorkflowKeyboardShortcut(keyboardEvent({ key: "x", ctrlKey: true }))).toBeNull();
  });

  it("uses the selected group and falls back to the primary node for shortcut targets", () => {
    expect(getSelectedWorkflowShortcutNodeIds(["a", "b"], "a")).toEqual(["a", "b"]);
    expect(getSelectedWorkflowShortcutNodeIds([], "a")).toEqual(["a"]);
    expect(getSelectedWorkflowShortcutNodeIds([], null)).toEqual([]);
  });
});
