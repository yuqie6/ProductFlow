import { describe, expect, it } from "vitest";

import type { WorkflowDraft, WorkflowDraftRevision } from "../../../lib/types";
import {
  createOrLoadEmptyWorkflowGraph,
  resolveAgentWorkbenchSidebarTool,
  selectReviewableWorkflowRevision,
  startEmptyCanvasAdd,
} from "./AgentProductWorkbenchPage";

function revision(id = "draft-revision-2"): WorkflowDraftRevision {
  return { id } as WorkflowDraftRevision;
}

function draft(
  status: WorkflowDraft["status"],
  currentRevision: WorkflowDraftRevision | null = revision(),
): WorkflowDraft {
  return {
    status,
    current_revision: currentRevision,
  } as WorkflowDraft;
}

describe("Agent workbench sidebar tool", () => {
  it("keeps Agent visible until a workflow exists, but recipes stay available", () => {
    expect(resolveAgentWorkbenchSidebarTool("details", false)).toBe("agent");
    expect(resolveAgentWorkbenchSidebarTool("library", false)).toBe("agent");
    expect(resolveAgentWorkbenchSidebarTool("agent", false)).toBe("agent");
    expect(resolveAgentWorkbenchSidebarTool("recipes", false)).toBe("recipes");
    expect(resolveAgentWorkbenchSidebarTool("details", true)).toBe("details");
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

describe("Agent workbench draft review", () => {
  it("reviews awaiting confirmation and a confirmed revision that still needs persist", () => {
    const currentRevision = revision();

    expect(selectReviewableWorkflowRevision(
      draft("awaiting_confirmation", currentRevision),
      null,
    )).toBe(currentRevision);
    expect(selectReviewableWorkflowRevision(
      draft("confirmed", currentRevision),
      { source_draft_revision_id: "draft-revision-1" },
    )).toBe(currentRevision);
  });

  it("does not reopen review after the exact revision is persisted to v3", () => {
    const currentRevision = revision();

    expect(selectReviewableWorkflowRevision(
      draft("confirmed", currentRevision),
      { source_draft_revision_id: currentRevision.id },
    )).toBeNull();
    expect(selectReviewableWorkflowRevision(
      draft("ready", currentRevision),
      { source_draft_revision_id: currentRevision.id },
    )).toBeNull();
  });
});
