import { describe, expect, it, vi } from "vitest";

import type {
  WorkflowDraft,
  WorkflowDraftRevision,
  WorkflowMaterializationResult,
} from "../../lib/types";

import {
  confirmAndMaterializeWorkflow,
  workflowMaterializationIdempotencyKey,
  workflowMaterializationInput,
} from "./useWorkflowMaterialization";

describe("workflow materialization request", () => {
  it("binds the reviewed draft version and bootstrap workflow revision to one stable key", () => {
    expect(workflowMaterializationIdempotencyKey("draft-1", 7)).toBe(
      "agent-workspace:draft-1:v7",
    );
    expect(workflowMaterializationInput("draft-1", 7, 3)).toEqual({
      expected_draft_version: 7,
      expected_workflow_revision: 3,
      idempotency_key: "agent-workspace:draft-1:v7",
    });
    expect(workflowMaterializationInput("draft-1", 7, 3)).toEqual(
      workflowMaterializationInput("draft-1", 7, 3),
    );
  });

  it("confirms the exact displayed revision before materializing with the same version", async () => {
    const revision = {
      id: "revision-7",
      draft_id: "draft-1",
      version: 7,
    } as WorkflowDraftRevision;
    const confirmedDraft = {
      id: "draft-1",
      current_revision: { ...revision, confirmed_at: "2026-08-14T00:00:00Z" },
    } as WorkflowDraft;
    const materialization = { id: "materialization-1", created: true } as WorkflowMaterializationResult;
    const callOrder: string[] = [];
    const confirmWorkflowDraft = vi.fn(async () => {
      callOrder.push("confirm");
      return confirmedDraft;
    });
    const materializeWorkflowDraft = vi.fn(async () => {
      callOrder.push("materialize");
      return materialization;
    });
    const onDraftConfirmed = vi.fn(() => callOrder.push("confirmed-callback"));

    const result = await confirmAndMaterializeWorkflow({
      productId: "product-1",
      draftId: "draft-1",
      revision,
      expectedWorkflowRevision: 3,
      onDraftConfirmed,
    }, { confirmWorkflowDraft, materializeWorkflowDraft });

    expect(callOrder).toEqual(["confirm", "confirmed-callback", "materialize"]);
    expect(confirmWorkflowDraft).toHaveBeenCalledWith("product-1", "draft-1", 7);
    expect(materializeWorkflowDraft).toHaveBeenCalledWith("product-1", "draft-1", {
      expected_draft_version: 7,
      expected_workflow_revision: 3,
      idempotency_key: "agent-workspace:draft-1:v7",
    });
    expect(result).toEqual({ confirmedDraft, materialization });
  });
});
