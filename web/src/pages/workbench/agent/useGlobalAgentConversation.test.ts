import { describe, expect, it } from "vitest";

import type { AgentTurn } from "../../../lib/types";
import {
  initialGlobalTaskTurnInput,
  selectGlobalAgentInteractionTurns,
  selectLatestGlobalAgentTurn,
} from "./useGlobalAgentConversation";

function turn(id: string, taskId: string | null, status: AgentTurn["status"]): AgentTurn {
  return {
    id,
    conversation_id: "global-conversation",
    task_id: taskId,
    harness_run_id: taskId ? `task-run-${taskId}` : "global-run",
    harness_turn_id: `harness-${id}`,
    idempotency_key: `key-${id}`,
    input_text: id,
    input_asset_ids: [],
    status,
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
    created_at: `2026-08-19T00:00:0${id.slice(-1)}Z`,
    updated_at: `2026-08-19T00:00:0${id.slice(-1)}Z`,
  };
}

describe("global Agent Task bootstrap input", () => {
  it("uses the task goal and a task-scoped idempotency key", () => {
    const input = initialGlobalTaskTurnInput(
      "global-conversation",
      "task-1",
      "整理最近生成的场景图",
      {
        route: "/media-library",
        page_type: "media_library",
        product_id: null,
        workflow_id: null,
        selected_asset_ids: ["asset-1"],
        visible_asset_ids: ["asset-1", "asset-2"],
        filters: { tag: "scene" },
        workflow_revision: null,
        library_revision: null,
        captured_at: "2026-08-18T00:00:00.000Z",
      },
    );

    expect(input.input_text).toBe("整理最近生成的场景图");
    expect(input.asset_ids).toEqual([]);
    expect(input.task_id).toBe("task-1");
    expect(input.idempotency_key).toBe("initial:global-conversation:task-1");
    expect(input.page_context).toEqual(expect.objectContaining({
      route: "/media-library",
      selected_asset_ids: ["asset-1"],
    }));
  });

  it("keeps a background Task Turn out of the direct global conversation controls", () => {
    const directTurn = turn("direct-1", null, "succeeded");
    const backgroundTurn = turn("task-1", "task-1", "running");

    expect(selectLatestGlobalAgentTurn([directTurn, backgroundTurn], null)).toBe(directTurn);
    expect(selectLatestGlobalAgentTurn([directTurn, backgroundTurn], "task-1")).toBe(backgroundTurn);
  });

  it("returns only direct Turns for a direct global conversation", () => {
    const directTurn = turn("direct-1", null, "succeeded");
    const nextDirectTurn = turn("direct-2", null, "running");
    const backgroundTurn = turn("task-1", "task-1", "running");

    expect(
      selectGlobalAgentInteractionTurns([directTurn, backgroundTurn, nextDirectTurn], null),
    ).toEqual([directTurn, nextDirectTurn]);
    expect(
      selectGlobalAgentInteractionTurns([directTurn, backgroundTurn, nextDirectTurn], "task-1"),
    ).toEqual([backgroundTurn]);
  });
});
