import { describe, expect, it } from "vitest";

import type { AgentTurn, AgentTurnEvent } from "../../lib/types";
import {
  agentEventReducer,
  createAgentTurnEventState,
  currentAgentAttempt,
  parseAgentTurnEvent,
  selectAgentAssistantText,
  selectAgentToolSteps,
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
    task_id: null,
    harness_turn_id: "harness-turn-1",
    idempotency_key: "turn-key",
    input_text: "hello",
    input_asset_ids: [],
    status: "running",
    resume_required: false,
    output_text: null,
    error_text: null,
    question: null,
    question_answer: null,
    continuation_turn_id: null,
    artifact_name: null,
    artifact_step_id: null,
    workflow_draft_revision_id: null,
    library_organization_draft_revision_id: null,
    workflow_run_request_id: null,
    page_context_snapshot_id: null,
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

  it("retains state for an empty turn key and resets before a different nonempty turn", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "tool.step", {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "读取商品上下文",
        status: "succeeded",
      }),
    });

    const retained = agentEventReducer(state, { type: "reset", turn_key: "" });
    expect(retained).toBe(state);

    const reset = agentEventReducer(retained, { type: "reset", turn_key: "projection-2" });
    expect(reset).toEqual(createAgentTurnEventState("projection-2"));
    expect(reset).not.toBe(retained);
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
    expect(parseAgentTurnEvent(JSON.stringify(event(1, "turn.started")), "turn.started", { run_id: null, turn_id: "harness-turn-1" }).run_id)
      .toBe("run-1");
  });

  it("strictly validates bounded tool.step payloads", () => {
    const scope = { run_id: "run-1", turn_id: "harness-turn-1" };
    const validPayload = {
      step_id: "step-1",
      kind: "inspect_image",
      summary: "检查商品正面图",
      status: "running",
    };

    expect(parseAgentTurnEvent(JSON.stringify(event(1, "tool.step", validPayload)), "tool.step", scope).payload)
      .toEqual(validPayload);
    const detailedPayload = {
      ...validPayload,
      kind: "propose_draft",
      tool_name: "propose_workflow_draft",
      details: {
        phase: "tool_result",
        error_code: "workflow_draft_validation_failed",
        retryable: true,
        validation_issues: [{ path: "image_types.0.images.0.delivery_spec.crop_anchor", message: "contain 不能指定 crop_anchor" }],
      },
    };
    let detailedState = createAgentTurnEventState("projection-1");
    detailedState = agentEventReducer(detailedState, {
      type: "event",
      event: event(2, "tool.step", detailedPayload),
    });
    expect(detailedState.tool_steps["step-1"].step).toMatchObject({
      tool_name: "propose_workflow_draft",
      details: detailedPayload.details,
    });
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "tool.step", { ...validPayload, raw: { secret: true } })),
        "tool.step",
        scope,
      ),
    ).toThrow("tool.step payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "tool.step", { ...validPayload, details: { raw: "secret" } })),
        "tool.step",
        scope,
      ),
    ).toThrow("tool.step details");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "tool.step", { ...validPayload, kind: "generate_image" })),
        "tool.step",
        scope,
      ),
    ).toThrow("tool.step payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "tool.step", { ...validPayload, status: "canceled" })),
        "tool.step",
        scope,
      ),
    ).toThrow("tool.step payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "tool.step", { ...validPayload, step_id: "界".repeat(67) })),
        "tool.step",
        scope,
      ),
    ).toThrow("tool.step payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "tool.step", { ...validPayload, summary: "界".repeat(54) })),
        "tool.step",
        scope,
      ),
    ).toThrow("tool.step payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "tool.step", { ...validPayload, summary: "line 1\nline 2" })),
        "tool.step",
        scope,
      ),
    ).toThrow("tool.step payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "tool.step", { ...validPayload, step_id: "   " })),
        "tool.step",
        scope,
      ),
    ).toThrow("tool.step payload");
  });

  it("merges newer tool step updates by ID without duplication", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "tool.step", {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "读取商品上下文",
        status: "running",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "tool.step", {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "已读取商品上下文",
        status: "succeeded",
      }),
    });

    expect(state.tool_step_order).toEqual(["step-1"]);
    expect(state.tool_steps["step-1"]).toEqual({
      sequence: 2,
      step: {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "已读取商品上下文",
        status: "succeeded",
      },
    });
  });

  it("merges snapshot steps first and appends new live steps for the matching Turn", () => {
    const snapshotTurn = turn({
      tool_steps: [
        {
          step_id: "step-1",
          kind: "inspect_image",
          summary: "检查商品图片",
          status: "running",
        },
      ],
    });
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "tool.step", {
        step_id: "step-1",
        kind: "inspect_image",
        summary: "检查商品图片",
        status: "succeeded",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "tool.step", {
        step_id: "step-2",
        kind: "organize_assets",
        summary: "整理商品图片",
        status: "running",
      }),
    });

    expect(selectAgentToolSteps(snapshotTurn, state).map(({ step_id, status }) => ({ step_id, status })))
      .toEqual([
        { step_id: "step-1", status: "succeeded" },
        { step_id: "step-2", status: "running" },
      ]);
    expect(selectAgentToolSteps(snapshotTurn, createAgentTurnEventState("other"))).toEqual(
      snapshotTurn.tool_steps,
    );
    expect(selectAgentToolSteps(turn(), null)).toEqual([]);
    expect(selectAgentToolSteps(undefined, state)).toEqual([]);
  });

  it("keeps the first snapshot value and position for duplicate step IDs, then overlays live values", () => {
    const snapshotTurn = turn({
      tool_steps: [
        {
          step_id: "step-1",
          kind: "inspect_context",
          summary: "first snapshot value",
          status: "running",
        },
        {
          step_id: "step-2",
          kind: "inspect_image",
          summary: "second position",
          status: "running",
        },
        {
          step_id: "step-1",
          kind: "read_history",
          summary: "duplicate snapshot value",
          status: "failed",
        },
      ],
    });

    expect(selectAgentToolSteps(snapshotTurn, null)).toEqual([
      {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "first snapshot value",
        status: "running",
      },
      {
        step_id: "step-2",
        kind: "inspect_image",
        summary: "second position",
        status: "running",
      },
    ]);

    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "tool.step", {
        step_id: "step-3",
        kind: "organize_assets",
        summary: "first live-only step",
        status: "running",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "tool.step", {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "live overlay",
        status: "succeeded",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(3, "tool.step", {
        step_id: "step-4",
        kind: "propose_draft",
        summary: "second live-only step",
        status: "running",
      }),
    });

    expect(selectAgentToolSteps(snapshotTurn, state).map(({ step_id, summary }) => ({ step_id, summary })))
      .toEqual([
        { step_id: "step-1", summary: "live overlay" },
        { step_id: "step-2", summary: "second position" },
        { step_id: "step-3", summary: "first live-only step" },
        { step_id: "step-4", summary: "second live-only step" },
      ]);
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
