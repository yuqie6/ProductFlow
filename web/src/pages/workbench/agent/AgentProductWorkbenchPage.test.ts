import { describe, expect, it } from "vitest";

import type { WorkflowDraft, WorkflowDraftRevision } from "../../../lib/types";
import {
  resolveAgentWorkbenchSidebarTool,
  selectReviewableWorkflowRevision,
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
