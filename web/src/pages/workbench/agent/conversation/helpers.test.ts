import { describe, expect, it } from "vitest";

import type { AgentTurn, AgentWorkflowRunRequest } from "../../../../lib/types";
import { createAgentTurnEventState } from "../agentEventReducer";
import {
  mergeWorkflowRunRequest,
  workflowRequestFromTurn,
} from "./helpers";

function turn(overrides: Partial<AgentTurn> = {}): AgentTurn {
  return {
    id: "projection-1",
    conversation_id: "conversation-1",
    task_id: null,
    harness_run_id: "run-1",
    harness_turn_id: "harness-turn-1",
    idempotency_key: "key-1",
    input_text: "执行工作流",
    input_asset_ids: [],
    status: "awaiting_confirmation",
    resume_required: false,
    output_text: null,
    thinking_text: null,
    error_text: null,
    question: null,
    question_answer: null,
    continuation_turn_id: null,
    artifact_name: null,
    artifact_step_id: null,
    library_organization_draft_revision_id: null,
    workflow_run_request_id: null,
    page_context_snapshot_id: null,
    sync_error: null,
    finished_at: null,
    created_at: "2026-08-31T00:00:00Z",
    updated_at: "2026-08-31T00:00:00Z",
    tool_steps: [
      {
        step_id: "step-run",
        kind: "request_workflow_run",
        summary: "请求执行工作流",
        status: "succeeded",
        meta: {
          pending_confirmation: true,
          workflow_id: "workflow-1",
          workflow_title: "春季主图",
          request_id: "request-journal",
          product_id: "product-1",
        },
      },
    ],
    ...overrides,
  };
}

describe("workflowRequestFromTurn", () => {
  it("builds a confirmable request from journal meta without inventing revision 0", () => {
    const hint = workflowRequestFromTurn(turn(), undefined);
    expect(hint).toMatchObject({
      id: "request-journal",
      workflow_id: "workflow-1",
      expected_workflow_revision: null,
      status: "awaiting_confirmation",
    });
  });

  it("copies a real revision from journal meta when present", () => {
    const seeded = turn();
    const step = seeded.tool_steps?.[0];
    if (!step?.meta) throw new Error("seed");
    step.meta.expected_workflow_revision = 7;
    expect(workflowRequestFromTurn(seeded, undefined)?.expected_workflow_revision).toBe(7);
  });

  it("lets the fetched request overlay the journal hint", () => {
    const journal = workflowRequestFromTurn(turn(), undefined);
    const fetched = {
      id: "request-live",
      conversation_id: "conversation-1",
      task_id: null,
      product_id: "product-1",
      product_name: "春季新品",
      workflow_id: "workflow-1",
      workflow_title: "春季主图",
      expected_workflow_revision: 4,
      status: "awaiting_confirmation",
      workflow_run_id: null,
      workflow_run_status: null,
      source_step_id: "step-run",
      failure_reason: null,
      confirmed_at: null,
      finished_at: null,
      created_at: "2026-08-31T00:00:00Z",
      updated_at: "2026-08-31T00:00:00Z",
    } satisfies AgentWorkflowRunRequest;
    expect(mergeWorkflowRunRequest(fetched, journal)?.id).toBe("request-live");
    expect(mergeWorkflowRunRequest(null, journal)?.id).toBe("request-journal");
    expect(mergeWorkflowRunRequest(undefined, null)).toBeNull();
  });

  it("does not require a live event snapshot when the turn already has tool meta", () => {
    const empty = createAgentTurnEventState("projection-1");
    expect(workflowRequestFromTurn(turn(), { "projection-1": empty })?.id).toBe("request-journal");
  });
});
