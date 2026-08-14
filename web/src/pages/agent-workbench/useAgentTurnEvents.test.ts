import { describe, expect, it, vi } from "vitest";

import type { AgentTurnEvent } from "../../lib/types";
import {
  subscribeToAgentTurnEvents,
  type AgentEventConnectionState,
  type AgentEventSourceLike,
} from "./useAgentTurnEvents";

class FakeEventSource implements AgentEventSourceLike {
  listeners = new Map<string, EventListener[]>();
  closed = false;

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
  it("keeps native reconnect active and closes immediately after a terminal event", () => {
    const source = new FakeEventSource();
    const states: AgentEventConnectionState[] = [];
    const events: AgentTurnEvent[] = [];
    subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent: (value) => events.push(value),
      onConnectionState: (state) => states.push(state),
    });

    source.emit("open");
    source.emit("error");
    expect(source.closed).toBe(false);
    source.emit("text.delta", event(2, "text.delta", {
      delta: "hello",
      step_id: "step-1",
      attempt_id: "attempt-1",
    }));
    source.emit("turn.succeeded", event(5, "turn.succeeded"));

    expect(events.map((item) => item.sequence)).toEqual([2, 5]);
    expect(states).toEqual(["connecting", "open", "reconnecting", "closed"]);
    expect(source.closed).toBe(true);
  });

  it("rejects a mismatched scope without exposing the event", () => {
    const source = new FakeEventSource();
    const onEvent = vi.fn();
    const onProtocolError = vi.fn();
    subscribeToAgentTurnEvents({
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
  });

  it("reports structured stream errors separately from connection reconnects", () => {
    const source = new FakeEventSource();
    const onStreamError = vi.fn();
    subscribeToAgentTurnEvents({
      url: "/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent: vi.fn(),
      onStreamError,
    });

    source.emit("error", JSON.stringify({ error: { message: "upstream unavailable" } }));

    expect(onStreamError).toHaveBeenCalledWith("upstream unavailable");
    expect(source.closed).toBe(false);
  });
});
