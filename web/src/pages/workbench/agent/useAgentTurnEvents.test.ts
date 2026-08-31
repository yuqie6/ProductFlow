import { describe, expect, it, vi } from "vitest";

import type { AgentTurn, AgentTurnEvent } from "../../../lib/types";
import {
  listAgentTurnEventStreams,
  subscribeToAgentTurnEvents,
  type AgentEventConnectionState,
  type AgentEventSourceLike,
} from "./useAgentTurnEvents";

class FakeEventSource implements AgentEventSourceLike {
  listeners = new Map<string, EventListener[]>();
  closed = false;

  constructor(readonly url = "") { }

  addEventListener(type: string, listener: EventListener): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  close(): void {
    this.closed = true;
  }

  emit(type: string, data?: string): void {
    const event = { type, ...(data === undefined ? {} : { data }) } as Event;
    this.listeners.get(type)?.forEach((listener) => listener(event));
  }
}

function event(sequence: number, kind: string, payload: Record<string, unknown> = {}): string {
  const stablePayload = normalizeStablePayload(kind, payload);
  return JSON.stringify({
    schema_version: 1,
    run_id: "run-1",
    turn_id: "turn-1",
    sequence,
    created_at: "2026-08-14T00:00:00Z",
    kind,
    payload: stablePayload,
  });
}

function normalizeStablePayload(kind: string, payload: Record<string, unknown>): Record<string, unknown> {
  if (kind === "item.delta" && typeof payload.attempt_id === "string") {
    return { item_id: payload.attempt_id, item_kind: "truncated" in payload ? "thinking" : "assistant_text", ...payload };
  }
  if ((kind === "item.started" || kind === "item.completed") && typeof payload.step_id === "string" && "status" in payload) {
    return { item_id: payload.step_id, item_kind: "tool_call", ...payload };
  }
  return payload;
}

describe("subscribeToAgentTurnEvents", () => {
  it("reconnects from the last contiguous cursor and closes after a terminal event", () => {
    vi.useFakeTimers();
    const sources: FakeEventSource[] = [];
    const states: AgentEventConnectionState[] = [];
    const events: AgentTurnEvent[] = [];
    const close = subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: (url) => {
        const source = new FakeEventSource(url);
        sources.push(source);
        return source;
      },
      onEvent: (value) => events.push(value),
      onConnectionState: (state) => states.push(state),
    });

    sources[0].emit("open");
    sources[0].emit("item.delta", event(1, "item.delta", {
      delta: "hello",
      step_id: "step-1",
      attempt_id: "attempt-1",
    }));
    sources[0].emit("error");
    expect(sources[0].closed).toBe(true);
    vi.advanceTimersByTime(250);
    expect(sources[1].url).toBe("/events?after=1");
    sources[1].emit("open");
    sources[1].emit("turn.completed", event(2, "turn.completed"));

    expect(events.map((item) => item.sequence)).toEqual([1, 2]);
    expect(states).toEqual(["connecting", "open", "reconnecting", "connecting", "open", "closed"]);
    expect(sources[1].closed).toBe(true);
    close();
    vi.useRealTimers();
  });

  it("keeps the stream open through turn.awaiting_confirmation", () => {
    const source = new FakeEventSource();
    const close = subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent: vi.fn(),
    });
    source.emit("open");
    source.emit("turn.awaiting_confirmation", event(1, "turn.awaiting_confirmation"));
    expect(source.closed).toBe(false);
    close();
  });

  it("parses and dispatches scoped tool_call item events and reports invalid payloads", () => {
    const source = new FakeEventSource();
    const onEvent = vi.fn();
    const onProtocolError = vi.fn();
    const close = subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent,
      onProtocolError,
    });

    source.emit("item.completed", event(1, "item.completed", {
      step_id: "step-1",
      kind: "inspect_image",
      summary: "检查商品图片",
      status: "running",
    }));
    source.emit(
      "item.completed",
      JSON.stringify({
        ...JSON.parse(event(2, "item.completed", {
          step_id: "step-2",
          kind: "inspect_context",
          summary: "读取商品上下文",
          status: "running",
        })), turn_id: "other-turn"
      }),
    );
    source.emit("item.completed", event(2, "item.completed", {
      step_id: "step-3",
      kind: "inspect_image",
      summary: "invalid",
      status: "canceled",
    }));

    expect(onEvent).toHaveBeenCalledOnce();
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({
      kind: "item.completed",
      payload: expect.objectContaining({ step_id: "step-1", status: "running" }),
    }));
    expect(onProtocolError).toHaveBeenCalledOnce();
    expect(source.closed).toBe(true);
    close();
  });

  it("repairs a sequence gap so the journal cursor cannot skip events", async () => {
    const source = new FakeEventSource();
    const events: AgentTurnEvent[] = [];
    const protocolErrors: Error[] = [];
    const close = subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      fetchEventPage: async () => ({
        items: [JSON.parse(event(2, "item.delta", {
          delta: "b",
          step_id: "step-1",
          attempt_id: "attempt-1",
        })) as AgentTurnEvent],
        next_after: 2,
        has_more: false,
        stream_state: "live",
      }),
      onEvent: (value) => events.push(value),
      onProtocolError: (error) => protocolErrors.push(error),
    });

    source.emit("item.delta", event(1, "item.delta", {
      delta: "a",
      step_id: "step-1",
      attempt_id: "attempt-1",
    }));
    source.emit("item.completed", event(3, "item.completed", {
      step_id: "step-2",
      kind: "inspect_image",
      summary: "检查商品图片",
      status: "succeeded",
    }));
    await Promise.resolve();
    await Promise.resolve();

    expect(protocolErrors).toHaveLength(0);
    expect(events.map((item) => item.sequence)).toEqual([1, 2, 3]);
    expect(source.closed).toBe(false);
    close();
  });

  it("rejects a mismatched scope without exposing the event", () => {
    const source = new FakeEventSource();
    const onEvent = vi.fn();
    const onProtocolError = vi.fn();
    const close = subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent,
      onProtocolError,
    });

    source.emit(
      "turn.started",
      JSON.stringify({ ...JSON.parse(event(1, "turn.started")), turn_id: "other-turn" }),
    );

    expect(onEvent).not.toHaveBeenCalled();
    expect(onProtocolError).toHaveBeenCalledOnce();
    close();
  });

  it("reports structured stream errors separately from connection reconnects", () => {
    vi.useFakeTimers();
    const source = new FakeEventSource();
    const onStreamError = vi.fn();
    const close = subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent: vi.fn(),
      onStreamError,
    });

    source.emit("error", JSON.stringify({ error: { message: "upstream unavailable" } }));

    expect(onStreamError).toHaveBeenCalledWith("upstream unavailable");
    expect(source.closed).toBe(true);
    close();
    vi.useRealTimers();
  });

  it("does not reconnect a finite dump when EventSource closes after flushing", () => {
    vi.useFakeTimers();
    const sources: FakeEventSource[] = [];
    const events: AgentTurnEvent[] = [];
    const close = subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      finite: true,
      createEventSource: (url) => {
        const source = new FakeEventSource(url);
        sources.push(source);
        return source;
      },
      onEvent: (value) => events.push(value),
    });

    sources[0].emit("open");
    sources[0].emit("error");
    vi.advanceTimersByTime(10_000);

    expect(sources).toHaveLength(1);
    expect(sources[0].closed).toBe(true);
    expect(events).toEqual([]);
    close();
    vi.useRealTimers();
  });

  it("keeps live reconnects when a non-finite stream drops without a terminal event", () => {
    vi.useFakeTimers();
    const sources: FakeEventSource[] = [];
    const close = subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: (url) => {
        const source = new FakeEventSource(url);
        sources.push(source);
        return source;
      },
      onEvent: vi.fn(),
    });

    sources[0].emit("error");
    vi.advanceTimersByTime(250);
    expect(sources).toHaveLength(2);
    close();
    vi.useRealTimers();
  });
});

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

describe("listAgentTurnEventStreams", () => {
  it("includes terminal turns with a harness id and marks them finite", () => {
    const specs = listAgentTurnEventStreams(
      [
        turn({ id: "live", status: "running" }),
        turn({ id: "settled", status: "succeeded" }),
        turn({ id: "no-harness", harness_turn_id: null, status: "succeeded" }),
      ],
      (turnId) => `/events/${turnId}`,
    );

    expect(specs).toEqual([
      expect.objectContaining({ turnId: "live", key: "live", finite: false, url: "/events/live" }),
      expect.objectContaining({ turnId: "settled", key: "settled", finite: true, url: "/events/settled" }),
    ]);
  });

  it("uses the turn projection id as the shared runtime identity", () => {
    const product = listAgentTurnEventStreams(
      [turn({ id: "shared-turn" })],
      (turnId) => `/api/v2/products/p1/conversations/c1/turns/${turnId}/events`,
    );
    const global = listAgentTurnEventStreams(
      [turn({ id: "shared-turn" })],
      (turnId) => `/api/v2/agent-conversations/c1/turns/${turnId}/events`,
    );

    expect(product[0]?.url).not.toBe(global[0]?.url);
    expect(product[0]?.key).toBe("shared-turn");
    expect(global[0]?.key).toBe("shared-turn");
  });

  it("keeps awaiting_confirmation streams open for approval.resolved", () => {
    const specs = listAgentTurnEventStreams(
      [turn({ id: "parked", status: "awaiting_confirmation" })],
      (turnId) => `/events/${turnId}`,
    );
    expect(specs).toEqual([
      expect.objectContaining({ turnId: "parked", finite: false }),
    ]);
  });
});
