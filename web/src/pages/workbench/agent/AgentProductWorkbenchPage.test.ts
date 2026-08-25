import { describe, expect, it, vi } from "vitest";

import type {
  WorkflowRecipePreview,
  WorkflowRecipeSummary,
} from "../../../lib/types";
import {
  buildAgentWorkflowRecipeApplyInput,
  clearAgentWorkflowRecipeIdempotencyKey,
  createOrLoadEmptyWorkflowGraph,
  focusVisibleAgentComposer,
  requestAgentWorkbenchOpen,
  resolveAgentWorkbenchSidebarTool,
  startEmptyCanvasAdd,
} from "./AgentProductWorkbenchPage";

describe("Agent workbench sidebar tool", () => {
  it("keeps Agent visible until a workflow exists, but recipes stay available", () => {
    expect(resolveAgentWorkbenchSidebarTool("details", false)).toBe("agent");
    expect(resolveAgentWorkbenchSidebarTool("library", false)).toBe("agent");
    expect(resolveAgentWorkbenchSidebarTool("agent", false)).toBe("agent");
    expect(resolveAgentWorkbenchSidebarTool("recipes", false)).toBe("recipes");
    expect(resolveAgentWorkbenchSidebarTool("details", true)).toBe("details");
  });

  it("keeps canvas tools when a live graph exists, independent of conversation or Turn status", () => {
    expect(resolveAgentWorkbenchSidebarTool("add", true)).toBe("add");
    expect(resolveAgentWorkbenchSidebarTool("runs", true)).toBe("runs");
    expect(resolveAgentWorkbenchSidebarTool("library", true)).toBe("library");
    expect(resolveAgentWorkbenchSidebarTool("details", true)).toBe("details");
  });

  it("opens the Agent explicitly before asking the shell to focus its composer", () => {
    const events: string[] = [];
    const setSidebarTool = vi.fn(() => events.push("agent-tool"));
    const requestOpen = vi.fn(() => events.push("expand-request"));

    requestAgentWorkbenchOpen(setSidebarTool, requestOpen);

    expect(setSidebarTool).toHaveBeenCalledWith("agent");
    expect(requestOpen).toHaveBeenCalledOnce();
    expect(events).toEqual(["agent-tool", "expand-request"]);
  });

  it("focuses only a composer that is outside an inert ancestor", () => {
    const focus = vi.fn();
    const visibleComposer = {
      closest: vi.fn(() => null),
      focus,
    } as unknown as HTMLTextAreaElement;
    const visibleRoot = {
      querySelector: vi.fn(() => visibleComposer),
    } as unknown as ParentNode;

    expect(focusVisibleAgentComposer(visibleRoot)).toBe(true);
    expect(focus).toHaveBeenCalledOnce();

    const inertComposer = {
      closest: vi.fn(() => ({})),
      focus: vi.fn(),
    } as unknown as HTMLTextAreaElement;
    const inertRoot = {
      querySelector: vi.fn(() => inertComposer),
    } as unknown as ParentNode;

    expect(focusVisibleAgentComposer(inertRoot)).toBe(false);
    expect(inertComposer.focus).not.toHaveBeenCalled();
  });
});

describe("startEmptyCanvasAdd", () => {
  it("persists a graph before opening add when the canvas is empty", async () => {
    const calls: string[] = [];
    const result = await startEmptyCanvasAdd({
      hasGraph: false,
      createGraph: async () => {
        calls.push("create");
        return { id: "g1" } as never;
      },
      openAdd: () => {
        calls.push("add");
      },
    });
    expect(result).toBe("created");
    expect(calls).toEqual(["create", "add"]);
  });

  it("does not create again when a live graph already exists", async () => {
    const calls: string[] = [];
    const result = await startEmptyCanvasAdd({
      hasGraph: true,
      createGraph: async () => {
        calls.push("create");
        return { id: "g1" } as never;
      },
      openAdd: () => {
        calls.push("add");
      },
    });
    expect(result).toBe("opened");
    expect(calls).toEqual(["add"]);
  });
});

describe("createOrLoadEmptyWorkflowGraph", () => {
  it("loads the current graph when create reports a conflict", async () => {
    const graph = { id: "g1" } as never;
    const loaded = await createOrLoadEmptyWorkflowGraph({
      create: async () => {
        throw { status: 409, detail: "商品已有 active schema-v3 工作流" };
      },
      loadCurrent: async () => graph,
      isConflict: (error) => Boolean(
        error && typeof error === "object" && "status" in error && error.status === 409,
      ),
    });
    expect(loaded).toBe(graph);
  });

  it("does not swallow a non-conflict create failure", async () => {
    await expect(createOrLoadEmptyWorkflowGraph({
      create: async () => {
        throw new Error("unavailable");
      },
      loadCurrent: async () => ({ id: "g1" }) as never,
      isConflict: () => false,
    })).rejects.toThrow("unavailable");
  });
});

describe("Agent recipe apply contract", () => {
  it("passes preview revision and digest through apply and clears failed apply keys", () => {
    const recipe = {
      current_version: { version: 6 },
    } as WorkflowRecipeSummary;
    const preview = {
      base_graph_revision: 14,
      preview_digest: "b".repeat(64),
    } as WorkflowRecipePreview;
    const keys = new Map([["r2", "key-2"]]);

    expect(buildAgentWorkflowRecipeApplyInput(recipe, preview, "key-2")).toEqual({
      expected_recipe_version: 6,
      expected_graph_revision: 14,
      preview_digest: "b".repeat(64),
      idempotency_key: "key-2",
    });

    clearAgentWorkflowRecipeIdempotencyKey(keys, "archive", "r2");
    expect(keys.get("r2")).toBe("key-2");
    clearAgentWorkflowRecipeIdempotencyKey(keys, "apply", "r2");
    expect(keys.has("r2")).toBe(false);
  });
});
