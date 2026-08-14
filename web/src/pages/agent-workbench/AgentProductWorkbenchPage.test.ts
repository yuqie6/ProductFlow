import { describe, expect, it } from "vitest";

import type {
  ActiveProductWorkflowV2,
  ProductWorkflowV2,
  WorkflowDraft,
  WorkflowDraftRevision,
} from "../../lib/types";
import {
  preferActiveWorkflowSnapshot,
  selectAgentWorkbenchWorkflow,
  selectReviewableWorkflowRevision,
} from "./AgentProductWorkbenchPage";

function workflow(
  revision: number,
  editVersion: number,
  sourceRevisionId = `draft-revision-${revision}`,
): ProductWorkflowV2 {
  return {
    id: `workflow-${revision}`,
    product_id: "product-1",
    title: "Agent workflow",
    active: true,
    schema_version: 2,
    revision,
    edit_version: editVersion,
    source_draft_revision_id: sourceRevisionId,
    visual_system_version_id: "visual-1",
    materialization_id: `materialization-${revision}`,
    folders: [],
    nodes: [],
    edges: [],
    created_at: "2026-08-14T00:00:00Z",
    updated_at: "2026-08-14T00:00:00Z",
  };
}

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

describe("Agent workbench workflow selection", () => {
  it("uses a newly materialized revision until the active query catches up", () => {
    const persisted = workflow(1, 4);
    const materialized = workflow(2, 1);

    expect(selectAgentWorkbenchWorkflow(persisted, materialized)).toBe(materialized);
    expect(selectAgentWorkbenchWorkflow(materialized, persisted)).toBe(materialized);
  });

  it("keeps the newest edit version for the same materialized revision", () => {
    const materialized = workflow(2, 1);
    const edited = workflow(2, 3);

    expect(selectAgentWorkbenchWorkflow(edited, materialized)).toBe(edited);
    expect(selectAgentWorkbenchWorkflow(materialized, edited)).toBe(edited);
  });

  it("never replaces a newer active snapshot with an older bootstrap response", () => {
    const current: ActiveProductWorkflowV2 = {
      latest_revision: 3,
      workflow: workflow(3, 1),
    };
    const stale: ActiveProductWorkflowV2 = {
      latest_revision: 2,
      workflow: workflow(2, 9),
    };

    expect(preferActiveWorkflowSnapshot(current, stale)).toBe(current);
  });
});

describe("Agent workbench draft review", () => {
  it("reviews awaiting confirmation and a confirmed revision that still needs materialization", () => {
    const currentRevision = revision();

    expect(selectReviewableWorkflowRevision(
      draft("awaiting_confirmation", currentRevision),
      null,
    )).toBe(currentRevision);
    expect(selectReviewableWorkflowRevision(
      draft("confirmed", currentRevision),
      workflow(1, 1, "draft-revision-1"),
    )).toBe(currentRevision);
  });

  it("does not reopen review after the exact revision is materialized", () => {
    const currentRevision = revision();

    expect(selectReviewableWorkflowRevision(
      draft("confirmed", currentRevision),
      workflow(2, 1, currentRevision.id),
    )).toBeNull();
    expect(selectReviewableWorkflowRevision(
      draft("ready", currentRevision),
      workflow(2, 1, currentRevision.id),
    )).toBeNull();
  });
});
