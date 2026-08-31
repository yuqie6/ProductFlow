import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { GraphProjection } from "../../../lib/types";
import {
  existingWorkbenchGroupId,
  existingWorkbenchNodeIds,
  keepAgentWorkbenchPlaceholder,
  parseWorkbenchSidebarTool,
  parseWorkbenchUiState,
  patchWorkbenchUiState,
  readWorkbenchUiState,
  sameWorkbenchIds,
  workbenchUiStorageKey,
} from "./workbenchUiState";

const productId = "product-1";

describe("parseWorkbenchUiState", () => {
  it("reads sidebar, selection, and chrome flags", () => {
    expect(parseWorkbenchUiState(JSON.stringify({
      sidebarTool: "runs",
      selectedNodeIds: ["node-a", "node-b"],
      chromeCollapsed: true,
      inspectorCollapsed: false,
      enteredGroupId: "group-1",
      filmstripVisible: false,
    }))).toEqual({
      sidebarTool: "runs",
      selectedNodeIds: ["node-a", "node-b"],
      chromeCollapsed: true,
      inspectorCollapsed: false,
      enteredGroupId: "group-1",
      filmstripVisible: false,
    });
  });

  it("drops unknown tools and malformed payloads", () => {
    expect(parseWorkbenchSidebarTool("digest")).toBeNull();
    expect(parseWorkbenchUiState("{")).toEqual({});
    expect(parseWorkbenchUiState(JSON.stringify({ sidebarTool: "payloadHash" }))).toEqual({});
  });
});

describe("workbench ui storage", () => {
  const storage = new Map<string, string>();
  beforeEach(() => {
    storage.clear();
    vi.stubGlobal("window", {
      localStorage: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => {
          storage.set(key, value);
        },
        removeItem: (key: string) => {
          storage.delete(key);
        },
      },
    });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("patches without wiping neighboring fields", () => {
    patchWorkbenchUiState(productId, { sidebarTool: "details", selectedNodeIds: ["n1"] });
    patchWorkbenchUiState(productId, { chromeCollapsed: true });
    expect(readWorkbenchUiState(productId)).toEqual({
      sidebarTool: "details",
      selectedNodeIds: ["n1"],
      chromeCollapsed: true,
    });
    expect(workbenchUiStorageKey(productId)).toContain(productId);
  });
});

describe("existingWorkbenchNodeIds", () => {
  it("keeps only nodes that still exist on the graph", () => {
    expect(existingWorkbenchNodeIds(
      { nodes: [{ id: "keep" }, { id: "other" }] } as Pick<GraphProjection, "nodes">,
      ["keep", "gone"],
    )).toEqual(["keep"]);
  });
});

describe("existingWorkbenchGroupId", () => {
  it("keeps a group that still exists and drops missing ones", () => {
    expect(existingWorkbenchGroupId(
      { groups: [{ id: "group-1" }] } as Pick<GraphProjection, "groups">,
      "group-1",
    )).toBe("group-1");
    expect(existingWorkbenchGroupId(
      { groups: [{ id: "group-1" }] } as Pick<GraphProjection, "groups">,
      "gone",
    )).toBeNull();
    expect(existingWorkbenchGroupId(null, "group-1")).toBeNull();
  });
});

describe("sameWorkbenchIds", () => {
  it("compares id lists in order", () => {
    expect(sameWorkbenchIds(["a", "b"], ["a", "b"])).toBe(true);
    expect(sameWorkbenchIds(["a", "b"], ["b", "a"])).toBe(false);
  });
});

describe("keepAgentWorkbenchPlaceholder", () => {
  it("keeps the previous bootstrap when only the session query key changes", () => {
    const previous = { conversation: { id: "c1" } };
    expect(keepAgentWorkbenchPlaceholder(
      previous,
      { queryKey: ["agent-workbench", productId, "session-old", null] },
      productId,
    )).toBe(previous);
  });

  it("does not keep another product's workbench", () => {
    expect(keepAgentWorkbenchPlaceholder(
      { conversation: { id: "c1" } },
      { queryKey: ["agent-workbench", "other-product", null, null] },
      productId,
    )).toBeUndefined();
  });
});
