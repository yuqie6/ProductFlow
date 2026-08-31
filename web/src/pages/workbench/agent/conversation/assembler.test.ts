import { describe, expect, it } from "vitest";

import type { AgentTurn, AgentTurnEvent } from "../../../../lib/types";
import { createAgentTurnEventState, agentEventReducer } from "../agentEventReducer";
import { ConversationAssembler, type FrameScheduler } from "./assembler";
import { assembleTurnNodes, echoMatchesTurn, isStreamPublicationKind } from "./types";

function event(sequence: number, kind: string, payload: Record<string, unknown> = {}): AgentTurnEvent {
  const stablePayload = normalizeStablePayload(kind, payload);
  return {
    schema_version: 1,
    run_id: "run-1",
    turn_id: "harness-turn-1",
    sequence,
    created_at: "2026-08-30T00:00:00Z",
    kind,
    payload: stablePayload,
  };
}

function normalizeStablePayload(kind: string, payload: Record<string, unknown>): Record<string, unknown> {
  if (kind === "item.delta" && typeof payload.attempt_id === "string") {
    return { item_id: payload.attempt_id, item_kind: "truncated" in payload ? "thinking" : "assistant_text", ...payload };
  }
  if ((kind === "item.started" || kind === "item.completed") && typeof payload.step_id === "string" && "status" in payload) {
    return { item_id: payload.step_id, item_kind: "tool_call", ...payload };
  }
  if (kind === "item.completed" && typeof payload.attempt_id === "string") {
    return { item_id: payload.attempt_id, item_kind: "assistant_text", ...payload };
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
    idempotency_key: "key-1",
    input_text: "请整理商品信息",
    input_asset_ids: ["asset-1"],
    status: "running",
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
    created_at: "2026-08-30T00:00:00Z",
    updated_at: "2026-08-30T00:00:00Z",
    ...overrides,
  };
}

class QueueScheduler implements FrameScheduler {
  readonly queued: Array<() => void> = [];
  schedule(callback: () => void): number {
    this.queued.push(callback);
    return this.queued.length;
  }
  cancel(): void {
    this.queued.length = 0;
  }
  flush(): void {
    const jobs = this.queued.splice(0);
    for (const job of jobs) job();
  }
}

describe("ConversationAssembler", () => {
  it("holds text deltas until the next animation frame and publishes structure immediately", () => {
    const scheduler = new QueueScheduler();
    const assembler = new ConversationAssembler("projection-1", scheduler);
    const snapshots: number[] = [];
    assembler.subscribe(() => snapshots.push(assembler.getSnapshot().last_sequence));

    assembler.apply(event(1, "item.delta", { delta: "你", step_id: "s", attempt_id: "a" }));
    expect(assembler.getSnapshot().last_sequence).toBe(0);
    expect(scheduler.queued).toHaveLength(1);

    assembler.apply(event(2, "item.delta", { delta: "好", step_id: "s", attempt_id: "a" }));
    expect(scheduler.queued).toHaveLength(1);
    scheduler.flush();
    expect(assembler.getSnapshot().last_sequence).toBe(2);
    expect(assembler.getSnapshot().blocks).toEqual([
      expect.objectContaining({ type: "text", text: "你好" }),
    ]);

    assembler.apply(event(3, "item.completed", {
      step_id: "tool-1",
      kind: "inspect_context",
      summary: "读取上下文",
      status: "running",
    }));
    expect(assembler.getSnapshot().last_sequence).toBe(3);
    expect(assembler.getSnapshot().tool_step_order).toEqual(["tool-1"]);
    expect(snapshots.at(-1)).toBe(3);
  });

  it("settles assistant text on item.completed", () => {
    const scheduler = new QueueScheduler();
    const assembler = new ConversationAssembler("projection-1", scheduler);
    assembler.apply(event(1, "item.delta", { delta: "终答", step_id: "s", attempt_id: "a" }));
    scheduler.flush();
    assembler.apply(event(2, "item.completed", { reason: "stop", attempt_id: "a" }));
    expect(assembler.getSnapshot().text_settled).toBe(true);
  });
});

describe("assembleTurnNodes", () => {
  it("keys user/thinking/assistant/tool/question/turn-tail from live blocks plus snapshot question", () => {
    let state = createAgentTurnEventState("projection-1");
    state = agentEventReducer(state, {
      type: "event",
      event: event(1, "item.delta", {
        item_kind: "thinking",
        delta: "先看约束",
        step_id: "s",
        attempt_id: "a",
        content_index: 0,
      }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(2, "item.delta", { delta: "建议", step_id: "s", attempt_id: "a" }),
    });
    state = agentEventReducer(state, {
      type: "event",
      event: event(3, "item.completed", {
        step_id: "tool-1",
        kind: "ask_question",
        summary: "提问",
        status: "running",
      }),
    });
    const current = turn({
      status: "requires_input",
      question: {
        id: "q1",
        header: "确认",
        question: "商品叫什么？",
        options: [{ label: "A" }],
      },
    });
    const nodes = assembleTurnNodes({
      turn: current,
      eventState: state,
      blocks: state.blocks,
      live: true,
    });
    expect(nodes.map((node) => node.type)).toEqual([
      "thinking",
      "assistant",
      "tool",
      "question",
      "turn-tail",
    ]);
    expect(nodes.map((node) => node.key)).toEqual([
      "thinking:a:0",
      "text:a:1",
      "tool:tool-1",
      "question:q1",
      "tail:projection-1",
    ]);
  });

  it("rebuilds settled history from the Turn snapshot without token rows", () => {
    const settled = turn({
      status: "succeeded",
      thinking_text: "内部推理",
      output_text: "最终回答",
      tool_steps: [{
        step_id: "tool-1",
        kind: "inspect_image",
        summary: "查看图片",
        status: "succeeded",
      }],
    });
    const nodes = assembleTurnNodes({
      turn: settled,
      eventState: createAgentTurnEventState(settled.id),
      blocks: [
        {
          type: "thinking",
          key: "thinking:snapshot",
          attempt_id: "snapshot",
          content_index: 0,
          text: "内部推理",
          truncated: false,
        },
        { type: "tool", key: "tool:tool-1", step_id: "tool-1" },
        {
          type: "text",
          key: "text:snapshot",
          attempt_id: "snapshot",
          content_index: 0,
          text: "最终回答",
        },
      ],
      live: false,
    });
    expect(nodes.filter((node) => node.type === "assistant")).toEqual([
      expect.objectContaining({ type: "assistant", text: "最终回答", streaming: false }),
    ]);
  });

  it("matches composer echo to the persisted user turn", () => {
    expect(echoMatchesTurn(
      { text: "请整理商品信息", assetIds: ["asset-1"], createdAt: "2026-08-30T00:00:00Z" },
      turn(),
    )).toBe(true);
    expect(echoMatchesTurn(
      { text: "请整理商品信息", assetIds: [], createdAt: "2026-08-30T00:00:00Z" },
      turn(),
    )).toBe(false);
    expect(isStreamPublicationKind("item.delta")).toBe(true);
    expect(isStreamPublicationKind("item.completed")).toBe(false);
  });
});
