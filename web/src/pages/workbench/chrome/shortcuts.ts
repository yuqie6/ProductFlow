export type WorkflowKeyboardShortcut = "copy" | "paste" | "duplicate" | "delete" | "undo" | "redo";

interface ShortcutTargetLike {
  tagName?: string;
  isContentEditable?: boolean;
  closest?: (selector: string) => unknown;
  getAttribute?: (name: string) => string | null;
}

interface KeyboardShortcutEventLike {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  shiftKey?: boolean;
  altKey?: boolean;
  defaultPrevented?: boolean;
  target?: EventTarget | null;
}

const SHORTCUT_BLOCKED_SELECTOR = [
  "input",
  "textarea",
  "select",
  "button",
  "a",
  "label",
  "[contenteditable]",
  "[role='textbox']",
  "[role='button']",
  "[data-workflow-shortcut-ignore]",
].join(",");

export function isWorkflowShortcutBlockedTarget(target: EventTarget | null): boolean {
  if (!target || typeof target !== "object") {
    return false;
  }
  const element = target as ShortcutTargetLike;
  if (element.isContentEditable) {
    return true;
  }
  const tagName = typeof element.tagName === "string" ? element.tagName.toLowerCase() : "";
  if (["input", "textarea", "select", "button", "a", "label"].includes(tagName)) {
    return true;
  }
  if (typeof element.getAttribute === "function") {
    const contentEditable = element.getAttribute("contenteditable");
    if (contentEditable === "" || contentEditable === "true" || contentEditable === "plaintext-only") {
      return true;
    }
  }
  return typeof element.closest === "function" && Boolean(element.closest(SHORTCUT_BLOCKED_SELECTOR));
}

export function getWorkflowKeyboardShortcut(event: KeyboardShortcutEventLike): WorkflowKeyboardShortcut | null {
  if (event.defaultPrevented || event.altKey || isWorkflowShortcutBlockedTarget(event.target ?? null)) {
    return null;
  }
  const key = event.key.toLowerCase();
  const primaryModifier = Boolean(event.ctrlKey || event.metaKey);

  if (!primaryModifier) {
    return key === "delete" || key === "backspace" ? "delete" : null;
  }
  if (key === "c" && !event.shiftKey) {
    return "copy";
  }
  if (key === "v" && !event.shiftKey) {
    return "paste";
  }
  if (key === "d" && !event.shiftKey) {
    return "duplicate";
  }
  if (key === "z") {
    return event.shiftKey ? "redo" : "undo";
  }
  if (key === "y" && !event.shiftKey) {
    return "redo";
  }
  return null;
}

export function getSelectedWorkflowShortcutNodeIds(
  selectedNodeIds: string[],
  primaryNodeId: string | null,
): string[] {
  if (selectedNodeIds.length) {
    return [...selectedNodeIds];
  }
  return primaryNodeId ? [primaryNodeId] : [];
}
