import { describe, expect, it } from "vitest";

import type { AgentTurn, AgentTurnEvent } from "../../lib/types";
import {
  agentEventReducer,
  createAgentTurnEventState,
  currentAgentAttempt,
  parseAgentTurnEvent,
  selectAgentAssistantText,
} from "./agentEventReducer";

function event(
  sequence: number,
  kind: string,
  payload: Record<string, unknown> = {},
): AgentTurnEvent {
  return {
    schema_version: 1,
    run_id: "run-1",
    turn_id: "harness-turn-1",
    sequence,
    created_at: "2026-08-14T00:00:00Z",
    kind,
    payload,
  };
}

function turn(overrides: Partial<AgentTurn> = {}): AgentTurn {
  return {
    id: "projection-1",
    conversation_id: "conversation-1",
    harness_turn_id: "harness-turn-1",
    idempotency_key: "turn-key",
    input_text: "hello",
    input_asset_ids: [],
    status: "running",
    resume_required: false,
    output_text: null,
    error_text: null,
    question: null,
    artifact_name: null,
    artifact_step_id: null,
    workflow_draft_revision_id: null,
    sync_error: null,
    finished_at: null,
    created_at: "2026-08-14T00:00:00Z",
    updated_at: "2026-08-14T00:00:00Z",
    ...overrides,
  };
}

describe("agentEventReducer", () => {
  it("deduplicates replayed sequences and displays only the latest attempt buffer", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "text.delta", { delta: "old", step_id: "step-1", attempt_id: "attempt-1" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "text.delta", { delta: " duplicate", step_id: "step-1", attempt_id: "attempt-1" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(5, "text.delta", { delta: "new", step_id: "step-2", attempt_id: "attempt-2" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(6, "text.delta", { delta: " answer", step_id: "step-2", attempt_id: "attempt-2" }),
    });

    expect(state.attempts["attempt-1"].text).toBe("old");
    expect(state.attempt_order).toEqual(["attempt-1", "attempt-2"]);
    expect(currentAgentAttempt(state)?.text).toBe("new answer");
    expect(selectAgentAssistantText(turn(), state)).toBe("new answer");
  });

  it("tracks Question, answer/resume, artifact, cancel, and terminal control events", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "question.required", {
        id: "question-1",
        header: "价格",
        question: "商品价格是多少？",
        options: [{ label: "稍后提供", description: "先保留价格占位" }],
      }),
    });
    state = agentEventReducer(state, { type: "event", event: event(2, "question.answered") });
    state = agentEventReducer(state, { type: "event", event: event(3, "turn.resume_requested") });
    state = agentEventReducer(state, { type: "event", event: event(4, "artifact.proposed") });
    state = agentEventReducer(state, { type: "event", event: event(5, "turn.cancel_requested") });
    state = agentEventReducer(state, { type: "event", event: event(6, "turn.awaiting_confirmation") });

    expect(state.question?.id).toBe("question-1");
    expect(state.question_answered).toBe(true);
    expect(state.resume_requested).toBe(true);
    expect(state.artifact_sequence).toBe(4);
    expect(state.cancel_requested).toBe(true);
    expect(state.terminal_kind).toBe("turn.awaiting_confirmation");
  });

  it("uses the canonical terminal projection instead of replayed deltas", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "text.delta", { delta: "partial", step_id: "step-1", attempt_id: "attempt-1" }),
    });

    expect(
      selectAgentAssistantText(turn({ status: "succeeded", output_text: "canonical final" }), state),
    ).toBe("canonical final");
  });

  it("rejects malformed, mismatched-scope, and mismatched-kind SSE data", () => {
    const scope = { run_id: "run-1", turn_id: "harness-turn-1" };
    expect(() => parseAgentTurnEvent("not-json", "text.delta", scope)).toThrow("有效 JSON");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify({ ...event(1, "text.delta"), run_id: "other" }),
        "text.delta",
        scope,
      ),
    ).toThrow("作用域");
    expect(() =>
      parseAgentTurnEvent(JSON.stringify(event(1, "turn.started")), "turn.succeeded", scope),
    ).toThrow("kind");
    expect(() =>
      parseAgentTurnEvent(JSON.stringify(event(1, "text.delta", { delta: "x" })), "text.delta", scope),
    ).toThrow("text.delta payload");
  });

  it("marks an attempt that changes step identity as a protocol error", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "text.delta", { delta: "a", step_id: "step-1", attempt_id: "attempt-1" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "text.delta", { delta: "b", step_id: "step-2", attempt_id: "attempt-1" }),
    });

    expect(state.protocol_error).toContain("step_id");
    expect(state.attempts["attempt-1"].text).toBe("a");
    expect(state.last_sequence).toBe(2);
  });
});
