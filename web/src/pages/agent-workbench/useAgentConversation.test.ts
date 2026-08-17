import type { InfiniteData } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import type { AgentTurn, AgentTurnPage } from "../../lib/types";
import {
  INITIAL_AGENT_TURN_TEXT,
  flattenAgentTurnPages,
  initialAgentTurnInput,
  selectNewestAgentTurnProjection,
  upsertAgentTurnPageData,
} from "./useAgentConversation";

function turn(id: string, status: AgentTurn["status"] = "succeeded"): AgentTurn {
  return {
    id,
    conversation_id: "conversation-1",
    task_id: null,
    harness_turn_id: `harness-${id}`,
    idempotency_key: `key-${id}`,
    input_text: id,
    input_asset_ids: [],
    status,
    resume_required: false,
    output_text: status === "succeeded" ? `output-${id}` : null,
    error_text: null,
    question: null,
    artifact_name: null,
    artifact_step_id: null,
    workflow_draft_revision_id: null,
    library_organization_draft_revision_id: null,
    workflow_run_request_id: null,
    page_context_snapshot_id: null,
    sync_error: null,
    finished_at: status === "succeeded" ? "2026-08-14T00:00:01Z" : null,
    created_at: `2026-08-14T00:00:0${id.slice(-1)}Z`,
    updated_at: `2026-08-14T00:00:0${id.slice(-1)}Z`,
  };
}

describe("Agent conversation model", () => {
  it("uses one deterministic first-Turn request across refreshes and browser tabs", () => {
    const first = initialAgentTurnInput("conversation-1", ["asset-1", "asset-2"]);
    const second = initialAgentTurnInput("conversation-1", ["asset-1", "asset-2"]);

    expect(first).toEqual(second);
    expect(first).toEqual({
      input_text: INITIAL_AGENT_TURN_TEXT,
      asset_ids: ["asset-1", "asset-2"],
      idempotency_key: "initial:conversation-1",
    });
  });

  it("scopes the first-Turn idempotency key to an explicit parallel Task", () => {
    const firstTask = initialAgentTurnInput("conversation-1", [], "task-1");
    const secondTask = initialAgentTurnInput("conversation-1", [], "task-2");

    expect(firstTask.idempotency_key).toBe("initial:conversation-1:task-1");
    expect(secondTask.idempotency_key).toBe("initial:conversation-1:task-2");
    expect(firstTask.idempotency_key).not.toBe(secondTask.idempotency_key);
  });

  it("prepends reverse-keyset pages into one chronological transcript and removes overlap", () => {
    const pages: AgentTurnPage[] = [
      { items: [turn("turn-3"), turn("turn-4")], next_cursor: "older" },
      { items: [turn("turn-1"), turn("turn-2"), turn("turn-3", "failed")], next_cursor: null },
    ];

    const result = flattenAgentTurnPages(pages);

    expect(result.map((item) => item.id)).toEqual(["turn-1", "turn-2", "turn-3", "turn-4"]);
    expect(result[2].status).toBe("succeeded");
  });

  it("updates an existing projection and appends a newly reserved Turn to the newest page", () => {
    const current: InfiniteData<AgentTurnPage, string | null> = {
      pages: [{ items: [turn("turn-1", "running")], next_cursor: null }],
      pageParams: [null],
    };
    const updated = upsertAgentTurnPageData(current, turn("turn-1", "succeeded"));
    const appended = upsertAgentTurnPageData(updated, turn("turn-2", "queued"));

    expect(updated.pages[0].items[0].status).toBe("succeeded");
    expect(appended.pages[0].items.map((item) => item.id)).toEqual(["turn-1", "turn-2"]);
    expect(current.pages[0].items[0].status).toBe("running");
  });

  it("never lets a stale active detail cache replace a terminal list projection", () => {
    const staleDetail = turn("turn-1", "running");
    const terminalPage = turn("turn-1", "succeeded");
    terminalPage.updated_at = "2026-08-14T00:00:05Z";

    expect(selectNewestAgentTurnProjection(terminalPage, staleDetail)?.status).toBe("succeeded");
    expect(selectNewestAgentTurnProjection(staleDetail, terminalPage)?.status).toBe("succeeded");
  });
});
