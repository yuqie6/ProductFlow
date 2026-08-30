import { describe, expect, it, vi } from "vitest";

import type { AgentTurnEvent } from "../../../lib/types";
import {
  subscribeToAgentTurnEvents,
  type AgentEventConnectionState,
  type AgentEventSourceLike,
} from "./useAgentTurnEvents";

class FakeEventSource implements AgentEventSourceLike {
  listeners = new Map<string, EventListener[]>();
  closed = false;

  constructor(readonly url = "") {}

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
  return JSON.stringify({
    schema_version: 1,
    run_id: "run-1",
    turn_id: "turn-1",
    sequence,
    created_at: "2026-08-14T00:00:00Z",
    kind,
    payload,
  });
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
    sources[0].emit("text.delta", event(1, "text.delta", {
      delta: "hello",
      step_id: "step-1",
      attempt_id: "attempt-1",
    }));
    sources[0].emit("error");
    expect(sources[0].closed).toBe(true);
    vi.advanceTimersByTime(250);
    expect(sources[1].url).toBe("/events?after=1");
    sources[1].emit("open");
    sources[1].emit("turn.succeeded", event(2, "turn.succeeded"));

    expect(events.map((item) => item.sequence)).toEqual([1, 2]);
    expect(states).toEqual(["connecting", "open", "reconnecting", "connecting", "open", "closed"]);
    expect(sources[1].closed).toBe(true);
    close();
    vi.useRealTimers();
  });

  it("parses and dispatches scoped tool.step events and reports invalid payloads", () => {
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

    source.emit("tool.step", event(1, "tool.step", {
      step_id: "step-1",
      kind: "inspect_image",
      summary: "检查商品图片",
      status: "running",
    }));
    source.emit(
      "tool.step",
      JSON.stringify({ ...JSON.parse(event(2, "tool.step", {
        step_id: "step-2",
        kind: "inspect_context",
        summary: "读取商品上下文",
        status: "running",
      })), turn_id: "other-turn" }),
    );
    source.emit("tool.step", event(2, "tool.step", {
      step_id: "step-3",
      kind: "inspect_image",
      summary: "invalid",
      status: "canceled",
    }));

    expect(onEvent).toHaveBeenCalledOnce();
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({
      kind: "tool.step",
      payload: expect.objectContaining({ step_id: "step-1", status: "running" }),
    }));
    expect(onProtocolError).toHaveBeenCalledTimes(2);
    close();
  });

  it("accepts a forward sequence jump so durable control events can skip live-only rows", () => {
    const source = new FakeEventSource();
    const events: AgentTurnEvent[] = [];
    const protocolErrors: Error[] = [];
    const close = subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent: (value) => events.push(value),
      onProtocolError: (error) => protocolErrors.push(error),
    });

    source.emit("text.delta", event(1, "text.delta", {
      delta: "a",
      step_id: "step-1",
      attempt_id: "attempt-1",
    }));
    source.emit("tool.step", event(4, "tool.step", {
      step_id: "step-2",
      kind: "inspect_image",
      summary: "检查商品图片",
      status: "succeeded",
    }));
    source.emit("turn.succeeded", event(6, "turn.succeeded"));

    expect(protocolErrors).toHaveLength(0);
    expect(events.map((item) => item.sequence)).toEqual([1, 4, 6]);
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
});
