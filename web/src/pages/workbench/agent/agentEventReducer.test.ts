import { describe, expect, it } from "vitest";

import type { AgentTurn, AgentTurnEvent } from "../../../lib/types";
import {
  agentEventReducer,
  agentTurnNeedsEventStream,
  createAgentTurnEventState,
  currentAgentAttempt,
  parseAgentTurnEvent,
  isAgentTurnSettled,
  selectAgentAssistantText,
  selectAgentToolSteps,
  selectAgentTurnBlocks,
} from "./agentEventReducer";

function event(
  sequence: number,
  kind: string,
  payload: Record<string, unknown> = {},
): AgentTurnEvent {
  const stablePayload = normalizeStablePayload(kind, payload);
  return {
    schema_version: 1,
    run_id: "run-1",
    turn_id: "harness-turn-1",
    sequence,
    created_at: "2026-08-14T00:00:00Z",
    kind,
    payload: stablePayload,
  };
}

function normalizeStablePayload(kind: string, payload: Record<string, unknown>): Record<string, unknown> {
  if (kind === "item.delta" && typeof payload.attempt_id === "string") {
    return {
      item_id: payload.attempt_id,
      item_kind: "truncated" in payload ? "thinking" : "assistant_text",
      ...payload,
    };
  }
  if ((kind === "item.started" || kind === "item.completed") && typeof payload.step_id === "string" && "status" in payload) {
    return { item_id: payload.step_id, item_kind: "tool_call", ...payload };
  }
  if (kind === "item.completed" && typeof payload.attempt_id === "string") {
    return { item_id: payload.attempt_id, item_kind: "assistant_text", ...payload };
  }
  if (kind === "approval.requested" && typeof payload.id === "string") {
    return { approval_id: payload.id, approval_kind: "question", item_kind: "question", ...payload };
  }
  return payload;
}

function turn(overrides: Partial<AgentTurn> = {}): AgentTurn {
  return {
    id: "projection-1",
    conversation_id: "conversation-1",
    task_id: null,
    harness_run_id: "run-1",
    harness_turn_id: "harness-turn-1",
    idempotency_key: "turn-key",
    input_text: "hello",
    input_asset_ids: [],
    status: "running",
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
      event: event(2, "item.delta", { delta: "old", step_id: "step-1", attempt_id: "attempt-1" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.delta", { delta: " duplicate", step_id: "step-1", attempt_id: "attempt-1" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(5, "item.delta", { delta: "new", step_id: "step-2", attempt_id: "attempt-2" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(6, "item.delta", { delta: " answer", step_id: "step-2", attempt_id: "attempt-2" }),
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
      event: event(1, "item.completed", {
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

  it("settles streaming text on item.completed without treating it as output", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "item.delta", { delta: "终答", step_id: "step-1", attempt_id: "attempt-1" }),
    });
    expect(state.text_settled).toBe(false);
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.completed", { reason: "stop", attempt_id: "attempt-1" }),
    });
    expect(state.text_settled).toBe(true);
    expect(currentAgentAttempt(state)?.text).toBe("终答");
  });

  it("starts streaming again after a previous assistant item completes", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "item.delta", { delta: "第一段", step_id: "step-1", attempt_id: "attempt-1" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.completed", { reason: "stop", attempt_id: "attempt-1" }),
    });
    expect(state.text_settled).toBe(true);

    state = agentEventReducer(state, {
      type: "event",
      event: event(3, "item.completed", {
        step_id: "tool-1",
        kind: "inspect_context",
        summary: "读取商品上下文",
        status: "succeeded",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(4, "item.delta", { delta: "第二段", step_id: "step-1", attempt_id: "attempt-1" }),
    });

    expect(state.text_settled).toBe(false);
    expect(state.blocks.map((block) => block.type)).toEqual(["text", "tool", "text"]);
    expect(state.blocks[0]).toMatchObject({ type: "text", text: "第一段" });
    expect(state.blocks[2]).toMatchObject({ type: "text", text: "第二段" });
  });

  it("assembles interleaved blocks from Go-projected journal events instead of snapshot fold", () => {
    const scope = { run_id: "run-1", turn_id: "harness-turn-1" };
    const frames: Array<[string, Record<string, unknown>]> = [
      ["turn.started", { status: "running" }],
      ["item.delta", {
        delta: "先想约束",
        step_id: "pi_turn",
        attempt_id: "attempt-1",
        content_index: 0,
        item_id: "attempt-1",
        item_kind: "thinking",
      }],
      ["item.started", {
        step_id: "call-1",
        kind: "inspect_context",
        summary: "读取商品上下文",
        status: "running",
        tool_name: "get_product_workflow_context_v1",
        item_id: "call-1",
        item_kind: "tool_call",
      }],
      ["item.completed", {
        step_id: "call-1",
        kind: "inspect_context",
        summary: "读取商品上下文",
        status: "succeeded",
        tool_name: "get_product_workflow_context_v1",
        details: { phase: "tool_result", output_summary: "已读取有界 ProductFlow 上下文。" },
        item_id: "call-1",
        item_kind: "tool_call",
      }],
      ["item.delta", {
        delta: "交错终答",
        step_id: "pi_turn",
        attempt_id: "attempt-1",
        content_index: 0,
        item_id: "attempt-1",
        item_kind: "assistant_text",
      }],
      ["item.completed", {
        reason: "stop",
        attempt_id: "attempt-1",
        text: "交错终答",
        thinking: "先想约束",
        interrupted: false,
        item_id: "attempt-1",
        item_kind: "assistant_text",
      }],
      ["approval.requested", {
        id: "question-1",
        header: "价格",
        question: "商品价格是多少？",
        options: [{ label: "稍后提供" }],
        approval_id: "question-1",
        approval_kind: "question",
        item_kind: "question",
      }],
      ["turn.completed", { reason: "completed" }],
    ];

    let state = createAgentTurnEventState("projection-1");
    for (const [index, [kind, payload]] of frames.entries()) {
      const parsed = parseAgentTurnEvent(
        JSON.stringify(event(index + 1, kind, payload)),
        kind,
        scope,
      );
      state = agentEventReducer(state, { type: "event", event: parsed });
    }

    expect(state.blocks.map((block) => block.type)).toEqual(["thinking", "tool", "text"]);
    expect(state.question?.id).toBe("question-1");
    expect(
      selectAgentTurnBlocks(
        turn({
          status: "succeeded",
          thinking_text: "快照思考",
          output_text: "快照终答",
          tool_steps: [{
            step_id: "snapshot-tool",
            kind: "inspect_context",
            summary: "快照工具",
            status: "succeeded",
          }],
        }),
        state,
      ).map((block) => block.type),
    ).toEqual(["thinking", "tool", "text"]);
    expect(selectAgentAssistantText(turn({ status: "succeeded", output_text: "快照终答" }), state)).toBe("快照终答");
  });

  it("renders assistant text carried by a completed stable item even without deltas", () => {
    const state = agentEventReducer(createAgentTurnEventState("projection-1"), {
      type: "event",
      event: event(1, "item.completed", {
        item_id: "assistant-1",
        item_kind: "assistant_text",
        attempt_id: "attempt-1",
        text: "完整恢复的回答",
      }),
    });

    expect(state.text_settled).toBe(true);
    expect(state.blocks).toEqual([{
      type: "text",
      key: "text:assistant-1",
      attempt_id: "attempt-1",
      content_index: 0,
      text: "完整恢复的回答",
    }]);
  });

  it("accepts and ignores a stable step-start marker without inventing a tool row", () => {
    const parsed = parseAgentTurnEvent(
      JSON.stringify(event(1, "item.started", { item_id: "step-1", item_kind: "step", step_id: "step-1" })),
      "item.started",
      { run_id: "run-1", turn_id: "harness-turn-1" },
    );
    const state = agentEventReducer(createAgentTurnEventState("projection-1"), { type: "event", event: parsed });

    expect(state.last_sequence).toBe(1);
    expect(state.blocks).toEqual([]);
  });

  it("tracks question and graph approval lifecycle events", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "approval.requested", {
        id: "question-1",
        header: "价格",
        question: "商品价格是多少？",
        options: [{ label: "稍后提供", description: "先保留价格占位" }],
      }),
    });
    state = agentEventReducer(state, { type: "event", event: event(2, "approval.resolved") });
    state = agentEventReducer(state, {
      type: "event",
      event: event(3, "approval.requested", {
        approval_id: "proposal-1",
        approval_kind: "graph_proposal",
        proposal_id: "proposal-1",
      }),
    });
    state = agentEventReducer(state, { type: "event", event: event(4, "turn.awaiting_confirmation") });

    expect(state.question).toBeNull();
    expect(state.question_answered).toBe(true);
    expect(state.approval).toMatchObject({ approval_kind: "graph_proposal", proposal_id: "proposal-1" });
    expect(state.approval_resolved).toBe(false);
    expect(state.terminal_kind).toBe("turn.awaiting_confirmation");
  });

  it("keeps journal text blocks separate from the aggregated terminal snapshot", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "item.delta", { delta: "partial", step_id: "step-1", attempt_id: "attempt-1" }),
    });

    expect(selectAgentTurnBlocks(turn({ status: "succeeded", output_text: "partial plus later answer" }), state))
      .toMatchObject([{ type: "text", text: "partial" }]);
    expect(selectAgentAssistantText(turn({ status: "succeeded", output_text: "canonical final" }), state)).toBe("canonical final");
  });

  it("rejects malformed, mismatched-scope, and mismatched-kind SSE data", () => {
    const scope = { run_id: "run-1", turn_id: "harness-turn-1" };
    expect(() => parseAgentTurnEvent("not-json", "item.delta", scope)).toThrow("有效 JSON");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify({ ...event(1, "item.delta"), run_id: "other" }),
        "item.delta",
        scope,
      ),
    ).toThrow("作用域");
    expect(() =>
      parseAgentTurnEvent(JSON.stringify(event(1, "turn.started")), "turn.completed", scope),
    ).toThrow("kind");
    expect(() =>
      parseAgentTurnEvent(JSON.stringify(event(1, "item.delta", { delta: "x" })), "item.delta", scope),
    ).toThrow("item.delta payload");
    expect(parseAgentTurnEvent(JSON.stringify(event(1, "turn.started")), "turn.started", { run_id: null, turn_id: "harness-turn-1" }).run_id)
      .toBe("run-1");
  });

  it("strictly validates bounded tool_call item payloads", () => {
    const scope = { run_id: "run-1", turn_id: "harness-turn-1" };
    const validPayload = {
      step_id: "step-1",
      kind: "inspect_image",
      summary: "检查商品正面图",
      status: "running",
    };

    expect(parseAgentTurnEvent(JSON.stringify(event(1, "item.completed", validPayload)), "item.completed", scope).payload)
      .toMatchObject(validPayload);
    expect(
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.completed", {
          step_id: "apply-1",
          kind: "apply_graph",
          summary: "立即写入 live graph ChangeSet",
          status: "succeeded",
        })),
        "item.completed",
        scope,
      ).payload.kind,
    ).toBe("apply_graph");
    const detailedPayload = {
      ...validPayload,
      kind: "propose_draft",
      tool_name: "propose_global_draft",
      details: {
        phase: "tool_result",
        error_code: "library_organization_draft_validation_failed",
        retryable: true,
        validation_issues: [{ path: "image_types.0.images.0.delivery_spec.crop_anchor", message: "contain 不能指定 crop_anchor" }],
      },
    };
    let detailedState = createAgentTurnEventState("projection-1");
    detailedState = agentEventReducer(detailedState, {
      type: "event",
      event: event(2, "item.completed", detailedPayload),
    });
    expect(detailedState.tool_steps["step-1"].step).toMatchObject({
      tool_name: "propose_global_draft",
      details: detailedPayload.details,
    });
    const resultMetaPayload = {
      step_id: "apply-meta-1",
      kind: "apply_graph",
      summary: "改名",
      status: "succeeded",
      tool_name: "apply_graph_change_set_v1",
      details: {
        phase: "tool_result",
        truncated: true,
        pending_confirmation: false,
        reconciled: true,
        response_format: "concise",
        operation_summaries: ["rename_node", "rename_node"],
        affected_node_ids: ["n1"],
        node_count: 19,
        workflow_title: "工作流",
      },
    };
    expect(
      parseAgentTurnEvent(JSON.stringify(event(1, "item.completed", resultMetaPayload)), "item.completed", scope).payload,
    ).toMatchObject(resultMetaPayload);
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.completed", { ...validPayload, raw: { secret: true } })),
        "item.completed",
        scope,
      ),
    ).toThrow("tool_call item payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.completed", { ...validPayload, details: { raw: "secret" } })),
        "item.completed",
        scope,
      ),
    ).toThrow("tool_call item details");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.completed", { ...validPayload, kind: "generate_image" })),
        "item.completed",
        scope,
      ),
    ).toThrow("tool_call item payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.completed", { ...validPayload, status: "canceled" })),
        "item.completed",
        scope,
      ),
    ).toThrow("tool_call item payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.completed", { ...validPayload, step_id: "界".repeat(67) })),
        "item.completed",
        scope,
      ),
    ).toThrow("tool_call item payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.completed", { ...validPayload, summary: "界".repeat(54) })),
        "item.completed",
        scope,
      ),
    ).toThrow("tool_call item payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.completed", { ...validPayload, summary: "line 1\nline 2" })),
        "item.completed",
        scope,
      ),
    ).toThrow("tool_call item payload");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.completed", { ...validPayload, step_id: "   " })),
        "item.completed",
        scope,
      ),
    ).toThrow("tool_call item payload");
  });

  it("merges newer tool step updates by ID without duplication", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "item.completed", {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "读取商品上下文",
        status: "running",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.completed", {
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
      event: event(1, "item.completed", {
        step_id: "step-1",
        kind: "inspect_image",
        summary: "检查商品图片",
        status: "succeeded",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.completed", {
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
      event: event(1, "item.completed", {
        step_id: "step-3",
        kind: "organize_assets",
        summary: "first live-only step",
        status: "running",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.completed", {
        step_id: "step-1",
        kind: "inspect_context",
        summary: "live overlay",
        status: "succeeded",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(3, "item.completed", {
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
      event: event(1, "item.delta", { delta: "a", step_id: "step-1", attempt_id: "attempt-1" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.delta", { delta: "b", step_id: "step-2", attempt_id: "attempt-1" }),
    });

    expect(state.protocol_error).toContain("step_id");
    expect(state.attempts["attempt-1"].text).toBe("a");
    expect(state.last_sequence).toBe(2);
  });

  it("interleaves thinking, text, and tools by sequence and keeps thinking out of the final answer", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "item.delta", {
        item_kind: "thinking",
        delta: "先看约束",
        step_id: "step-1",
        attempt_id: "attempt-1",
        content_index: 0,
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.delta", {
        delta: "中间说明",
        step_id: "step-1",
        attempt_id: "attempt-1",
        content_index: 0,
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(3, "item.completed", {
        step_id: "tool-1",
        kind: "inspect_context",
        summary: "读取商品上下文",
        status: "succeeded",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(4, "item.delta", {
        item_kind: "thinking",
        delta: "再给结论",
        step_id: "step-1",
        attempt_id: "attempt-1",
        content_index: 0,
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(5, "item.delta", {
        delta: "终答",
        step_id: "step-1",
        attempt_id: "attempt-1",
        content_index: 1,
      }),
    });

    const blocks = selectAgentTurnBlocks(turn(), state);
    expect(blocks.map((block) => block.type)).toEqual(["thinking", "text", "tool", "thinking", "text"]);
    expect(blocks[0]).toMatchObject({ type: "thinking", text: "先看约束" });
    expect(blocks[3]).toMatchObject({ type: "thinking", text: "再给结论" });
    expect(selectAgentAssistantText(turn(), state)).toBe("中间说明终答");

    const settledBlocks = selectAgentTurnBlocks(turn({ status: "succeeded", output_text: "终答" }), state);
    expect(settledBlocks.map((block) => block.type)).toEqual(["thinking", "text", "tool", "thinking", "text"]);
    expect(settledBlocks[1]).toMatchObject({ type: "text", text: "中间说明" });
    expect(settledBlocks[4]).toMatchObject({ type: "text", text: "终答" });
  });

  it("projects historical thinking_text before tools and output when events are gone", () => {
    const blocks = selectAgentTurnBlocks(
      turn({
        status: "succeeded",
        thinking_text: "内部推理",
        output_text: "终答",
        tool_steps: [
          {
            step_id: "step-1",
            kind: "inspect_context",
            summary: "读取商品上下文",
            status: "succeeded",
          },
        ],
      }),
      null,
    );
    expect(blocks.map((block) => block.type)).toEqual(["thinking", "tool", "text"]);
    expect(blocks[0]).toMatchObject({ type: "thinking", text: "内部推理" });
    expect(selectAgentAssistantText(turn({ status: "succeeded", thinking_text: "内部推理", output_text: "终答" }), null)).toBe(
      "终答",
    );
  });

  it("subscribes to live and terminal turns that have a harness id", () => {
    expect(agentTurnNeedsEventStream(turn({ status: "running" }))).toBe(true);
    expect(agentTurnNeedsEventStream(turn({ status: "succeeded" }))).toBe(true);
    expect(agentTurnNeedsEventStream(turn({ status: "failed", harness_turn_id: "harness-turn-1" }))).toBe(true);
    expect(agentTurnNeedsEventStream(turn({ harness_turn_id: null }))).toBe(false);
    expect(agentTurnNeedsEventStream(null)).toBe(false);
  });

  it("keeps journal block order for a settled turn and falls back when the log is empty", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "item.delta", {
        item_kind: "thinking",
        delta: "先看约束",
        step_id: "step-1",
        attempt_id: "attempt-1",
        content_index: 0,
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.completed", {
        step_id: "tool-1",
        kind: "inspect_context",
        summary: "读取商品上下文",
        status: "succeeded",
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(3, "item.delta", {
        delta: "终答",
        step_id: "step-1",
        attempt_id: "attempt-1",
        content_index: 1,
      }),
    });
    const settled = turn({
      status: "succeeded",
      thinking_text: "内部推理",
      output_text: "终答",
      tool_steps: [
        {
          step_id: "tool-1",
          kind: "inspect_context",
          summary: "读取商品上下文",
          status: "succeeded",
        },
      ],
    });
    expect(selectAgentTurnBlocks(settled, state).map((block) => block.type)).toEqual(["thinking", "tool", "text"]);
    expect(selectAgentTurnBlocks(settled, state)[0]).toMatchObject({ type: "thinking", text: "先看约束" });
    expect(selectAgentTurnBlocks(settled, createAgentTurnEventState("projection-1")).map((block) => block.type)).toEqual([
      "thinking",
      "tool",
      "text",
    ]);
    expect(selectAgentTurnBlocks(settled, createAgentTurnEventState("projection-1"))[0]).toMatchObject({
      type: "thinking",
      text: "内部推理",
    });
  });

  it("rejects malformed thinking item.delta payloads", () => {
    const scope = { run_id: "run-1", turn_id: "harness-turn-1" };
    expect(
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.delta", { item_kind: "thinking", delta: "", step_id: "step-1", attempt_id: "attempt-1" })),
        "item.delta",
        scope,
      ).payload.delta,
    ).toBe("");
    expect(() =>
      parseAgentTurnEvent(
        JSON.stringify(event(1, "item.delta", { item_kind: "thinking", delta: "x", attempt_id: "attempt-1" })),
        "item.delta",
        scope,
      ),
    ).toThrow("item.delta thinking payload");
  });

  it("accepts tool/result meta on Go-projected item.completed payloads", () => {
    const parsed = parseAgentTurnEvent(
      JSON.stringify(event(1, "item.completed", {
        item_id: "step-1",
        item_kind: "tool_call",
        step_id: "step-1",
        kind: "propose_graph",
        summary: "提出图修改",
        status: "succeeded",
        meta: {
          schema_version: 1,
          kind: "propose_graph",
          pending_confirmation: true,
          proposal_id: "proposal-1",
          summary: "加一个镜头",
          operation_summaries: ["add_node"],
          affected_node_ids: ["node-1"],
        },
      })),
      "item.completed",
      { run_id: "run-1", turn_id: "harness-turn-1" },
    );
    expect(parsed.payload.meta).toMatchObject({
      pending_confirmation: true,
      proposal_id: "proposal-1",
      summary: "加一个镜头",
    });
  });

  it("keeps awaiting_confirmation unsettled so confirmation can stream", () => {
    expect(isAgentTurnSettled("awaiting_confirmation")).toBe(false);
    expect(isAgentTurnSettled("succeeded")).toBe(true);
  });
});
