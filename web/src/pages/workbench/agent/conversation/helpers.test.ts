import { describe, expect, it } from "vitest";

import type { AgentTurn, AgentWorkflowRunRequest } from "../../../../lib/types";
import { createAgentTurnEventState } from "../agentEventReducer";
import { visibleAgentTurns } from "../agentTurnRetry";
import {
  latestAgentCanvasFocus,
  mergeWorkflowRunRequest,
  workflowRequestFromTurn,
  workflowRunRequestForTurn,
  workflowRunRequestRefetchInterval,
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
      run_scope: "graph",
      target_node_id: null,
      target_node_ids: [],
      force: false,
      document_action: null,
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

describe("workflowRunRequestForTurn", () => {
  it("pins a fetched request to the turn that created it, including after it finishes", () => {
    const attached = turn({
      status: "succeeded",
      workflow_run_request_id: "request-live",
      tool_steps: [
        {
          step_id: "step-run",
          kind: "request_workflow_run",
          summary: "请求执行工作流",
          status: "succeeded",
          meta: {
            pending_confirmation: false,
            workflow_id: "workflow-1",
            request_id: "request-live",
          },
        },
      ],
    });
    const fetched = {
      id: "request-live",
      conversation_id: "conversation-1",
      task_id: null,
      product_id: "product-1",
      product_name: "春季新品",
      workflow_id: "workflow-1",
      workflow_title: "春季主图",
      expected_workflow_revision: 4,
      run_scope: "graph" as const,
      target_node_id: null,
      target_node_ids: [],
      force: false,
      document_action: null,
      status: "succeeded" as const,
      workflow_run_id: "run-1",
      workflow_run_status: "succeeded" as const,
      source_step_id: "step-run",
      failure_reason: null,
      confirmed_at: "2026-08-31T00:01:00Z",
      finished_at: "2026-08-31T00:10:00Z",
      created_at: "2026-08-31T00:00:00Z",
      updated_at: "2026-08-31T00:10:00Z",
    };
    expect(workflowRunRequestForTurn(attached, fetched)?.id).toBe("request-live");
    expect(workflowRunRequestForTurn(turn({ id: "projection-other" }), fetched)).toBeNull();
  });

  it("falls back to journal meta while the request is still waiting on this turn", () => {
    expect(workflowRunRequestForTurn(turn(), null)?.id).toBe("request-journal");
  });
});

describe("workflowRunRequestRefetchInterval", () => {
  it("polls only while the request is still waiting or running", () => {
    const hint = workflowRequestFromTurn(turn(), undefined);
    expect(workflowRunRequestRefetchInterval(hint)).toBe(2000);
    expect(workflowRunRequestRefetchInterval({
      ...hint!,
      status: "confirmed",
      workflow_run_status: "running",
    })).toBe(2000);
    expect(workflowRunRequestRefetchInterval({
      ...hint!,
      status: "succeeded",
      workflow_run_status: "succeeded",
    })).toBe(false);
    expect(workflowRunRequestRefetchInterval(null)).toBe(false);
  });
});

describe("latestAgentCanvasFocus", () => {
  it("uses a live focus_canvas tool result before the turn snapshot records canvas_focus", () => {
    const live = turn({
      canvas_focus: null,
      tool_steps: [
        {
          step_id: "step-focus",
          kind: "focus_canvas",
          summary: "聚焦画布",
          status: "succeeded",
          meta: { affected_node_ids: ["node-detail-1"] },
        },
      ],
    });
    expect(latestAgentCanvasFocus([live], undefined, null)).toEqual({
      requestId: "projection-1:step-focus",
      nodeIds: ["node-detail-1"],
      waitingForGraph: false,
    });
  });

  it("ignores canvas focus from later Turns after an earlier Turn is retried", () => {
    const first = turn({
      id: "projection-1",
      created_at: "2026-08-31T00:00:01Z",
      canvas_focus: null,
      tool_steps: [],
    });
    const later = turn({
      id: "projection-2",
      created_at: "2026-08-31T00:00:02Z",
      canvas_focus: null,
      tool_steps: [
        {
          step_id: "step-later",
          kind: "focus_canvas",
          summary: "聚焦画布",
          status: "succeeded",
          meta: { affected_node_ids: ["node-later"] },
        },
      ],
    });
    const retry = turn({
      id: "projection-3",
      created_at: "2026-08-31T00:00:03Z",
      idempotency_key: "retry:projection-1:attempt-2",
      canvas_focus: null,
      tool_steps: [
        {
          step_id: "step-retry",
          kind: "focus_canvas",
          summary: "聚焦画布",
          status: "succeeded",
          meta: { affected_node_ids: ["node-retry"] },
        },
      ],
    });
    expect(latestAgentCanvasFocus(visibleAgentTurns([first, later, retry]), undefined, null)).toEqual({
      requestId: "projection-3:step-retry",
      nodeIds: ["node-retry"],
      waitingForGraph: false,
    });
  });
});
