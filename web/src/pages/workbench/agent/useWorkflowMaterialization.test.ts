import { describe, expect, it, vi } from "vitest";

import type {
  GraphProjection,
  WorkflowDraft,
  WorkflowDraftRevision,
} from "../../../lib/types";

import {
  confirmAndMaterializeWorkflow,
  workflowMaterializationIdempotencyKey,
} from "./useWorkflowMaterialization";

describe("workflow materialization request", () => {
  it("binds the reviewed draft version to one stable key", () => {
    expect(workflowMaterializationIdempotencyKey("draft-1", 7)).toBe(
      "agent-workspace:draft-1:v7",
    );
  });

  it("confirms the exact displayed revision before persisting a v3 graph", async () => {
    const revision = {
      id: "revision-7",
      draft_id: "draft-1",
      version: 7,
    } as WorkflowDraftRevision;
    const confirmedDraft = {
      id: "draft-1",
      current_revision: { ...revision, confirmed_at: "2026-08-14T00:00:00Z" },
    } as WorkflowDraft;
    const graph = { id: "graph-1", schema_version: 3, revision: 1 } as GraphProjection;
    const callOrder: string[] = [];
    const confirmWorkflowDraft = vi.fn(async () => {
      callOrder.push("confirm");
      return confirmedDraft;
    });
    const persistConfirmedDraftGraph = vi.fn(async () => {
      callOrder.push("persist");
      return { created: true, graph };
    });
    const onDraftConfirmed = vi.fn(() => callOrder.push("confirmed-callback"));

    const result = await confirmAndMaterializeWorkflow({
      productId: "product-1",
      draftId: "draft-1",
      revision,
      expectedWorkflowRevision: 3,
      onDraftConfirmed,
    }, { confirmWorkflowDraft, persistConfirmedDraftGraph });

    expect(callOrder).toEqual(["confirm", "confirmed-callback", "persist"]);
    expect(confirmWorkflowDraft).toHaveBeenCalledWith("product-1", "draft-1", 7);
    expect(persistConfirmedDraftGraph).toHaveBeenCalledWith("product-1", "draft-1", 7);
    expect(result).toEqual({ confirmedDraft, graph });
  });
});
