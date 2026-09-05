import { describe, expect, it, vi } from "vitest";

import type { AgentTurnEvent } from "../../../../lib/types";
import {
  ConversationRuntime,
  subscribeToConversationEvents,
  type ConversationEventPage,
  type ConversationEventSourceLike,
} from "./runtime";

class FakeEventSource implements ConversationEventSourceLike {
  private readonly listeners = new Map<string, EventListener[]>();
  closed = false;

  addEventListener(type: string, listener: EventListener): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  close(): void {
    this.closed = true;
  }

  emit(type: string, data?: string): void {
    const event = { type, ...(data === undefined ? {} : { data }) } as unknown as Event;
    this.listeners.get(type)?.forEach((listener) => listener(event));
  }
}

function event(
  sequence: number,
  kind: AgentTurnEvent["kind"] = "turn.started",
  payload: Record<string, unknown> = kind === "turn.completed"
    ? { reason: "completed" }
    : { status: "running" },
): string {
  return JSON.stringify({
    schema_version: 1,
    run_id: "run-1",
    turn_id: "turn-1",
    sequence,
    created_at: "2026-08-31T00:00:00Z",
    kind,
    payload,
  });
}

async function flushAsyncWork(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

describe("ConversationRuntime", () => {

  it("keeps shared repair alive until the last consumer releases it", () => {
    const sources: FakeEventSource[] = [];
    const signals: AbortSignal[] = [];
    const runtime = new ConversationRuntime({
      key: "shared-repair",
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => {
        const source = new FakeEventSource();
        sources.push(source);
        return source;
      },
      fetchEventPage: (_url, signal) => {
        if (signal) signals.push(signal);
        return new Promise(() => undefined);
      },
    });
    try {
      const releaseFirst = runtime.acquire({});
      const releaseSecond = runtime.acquire({});
      sources[0]?.emit("turn.started", event(2));
      expect(signals).toHaveLength(1);
      releaseFirst();
      expect(signals[0]?.aborted).toBe(false);
      expect(sources[0]?.closed).toBe(false);
      releaseSecond();
      expect(signals[0]?.aborted).toBe(true);
      const releaseNext = runtime.acquire({});
      sources[1]?.emit("turn.started", event(2));
      expect(signals).toHaveLength(2);
      expect(signals[1]?.aborted).toBe(false);
      releaseNext();
      expect(signals[1]?.aborted).toBe(true);
    } finally {
      runtime.dispose();
    }
  });

  it("aborts an in-flight browser repair when its subscription closes", async () => {
    const source = new FakeEventSource();
    let requestSignal: AbortSignal | undefined;
    let aborts = 0;
    const protocolErrors = vi.fn();
    const received = vi.fn();
    const fetchMock = vi.fn((_url: unknown, init?: RequestInit) => {
      requestSignal = init?.signal ?? undefined;
      return new Promise<Response>((_resolve, reject) => {
        requestSignal?.addEventListener("abort", () => {
          aborts += 1;
          reject(new DOMException("Aborted", "AbortError"));
        }, { once: true });
      });
    });
    vi.stubGlobal("fetch", fetchMock);
    const close = subscribeToConversationEvents({
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent: received,
      onProtocolError: protocolErrors,
    });
    try {
      source.emit("turn.started", event(2));
      expect(fetchMock).toHaveBeenCalledTimes(1);
      close();
      close();
      await flushAsyncWork();
      expect(requestSignal?.aborted).toBe(true);
      expect(aborts).toBe(1);
      expect(source.closed).toBe(true);
      expect(received).not.toHaveBeenCalled();
      expect(protocolErrors).not.toHaveBeenCalled();
    } finally {
      close();
      vi.unstubAllGlobals();
    }
  });

  it("repairs a sequence gap from the durable event page before delivering buffered events", async () => {
    const source = new FakeEventSource();
    const received: number[] = [];
    const fetchEventPage = vi.fn(async () => ({
      items: [JSON.parse(event(2)) as AgentTurnEvent],
      next_after: 2,
      has_more: false,
      stream_state: "live" as const,
    }));
    const close = subscribeToConversationEvents({
      url: "/api/v2/agent-conversations/c/turns/t/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      fetchEventPage,
      onEvent: (value) => received.push(value.sequence),
    });

    source.emit("turn.started", event(1));
    source.emit("turn.started", event(3));
    await flushAsyncWork();

    expect(fetchEventPage).toHaveBeenCalledWith(
      "/api/v2/agent-conversations/c/turns/t/events/page?after=1&limit=250",
      expect.any(AbortSignal),
    );
    expect(received).toEqual([1, 2, 3]);
    expect(source.closed).toBe(false);
    close();
  });

  it("applies duplicate seq1 then seq2 on the same generation without a stream error", async () => {
    const source = new FakeEventSource();
    const received: number[] = [];
    const streamErrors: Array<string | null> = [];
    const states: string[] = [];
    const close = subscribeToConversationEvents({
      url: "/api/v2/agent-conversations/c/turns/t/events?after=0",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      fetchEventPage: async () => {
        throw new Error("duplicate seq1 must not open a gap repair");
      },
      onEvent: (value) => received.push(value.sequence),
      onConnectionState: (state) => states.push(state),
      onStreamError: (message) => streamErrors.push(message),
      onProtocolError: (error) => {
        throw error;
      },
    });

    source.emit("open");
    source.emit("turn.started", event(1));
    source.emit("turn.started", event(1));
    source.emit("turn.started", event(2));
    await flushAsyncWork();

    expect(received).toEqual([1, 2]);
    expect(streamErrors.filter((message) => message)).toEqual([]);
    expect(states).toContain("open");
    expect(states).not.toContain("closed");
    expect(states).not.toContain("reconnecting");
    expect(source.closed).toBe(false);
    close();
  });

  it("follows has_more across multiple repair pages using the previous cursor", async () => {
    const source = new FakeEventSource();
    const received: number[] = [];
    const firstPage = Array.from(
      { length: 250 },
      (_, index) => JSON.parse(event(index + 1)) as AgentTurnEvent,
    );
    const fetchEventPage = vi.fn(async (url: string): Promise<ConversationEventPage> => {
      if (url.includes("after=0")) {
        return {
          items: firstPage,
          next_after: 250,
          has_more: true,
          stream_state: "live",
        };
      }
      return {
        items: [JSON.parse(event(251)) as AgentTurnEvent],
        next_after: 251,
        has_more: false,
        stream_state: "live",
      };
    });
    const close = subscribeToConversationEvents({
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      fetchEventPage,
      onEvent: (value) => received.push(value.sequence),
    });

    source.emit("turn.started", event(252));
    await flushAsyncWork();

    expect(fetchEventPage).toHaveBeenNthCalledWith(1, "/events/page?after=0&limit=250", expect.any(AbortSignal));
    expect(fetchEventPage).toHaveBeenNthCalledWith(2, "/events/page?after=250&limit=250", expect.any(AbortSignal));
    expect(received).toEqual(Array.from({ length: 252 }, (_, index) => index + 1));
    expect(source.closed).toBe(false);
    close();
  });

  it("closes with a protocol error for a required journal kind unknown to the UI", () => {
    const source = new FakeEventSource();
    const protocolErrors: Error[] = [];
    const close = subscribeToConversationEvents({
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent: vi.fn(),
      onProtocolError: (error) => protocolErrors.push(error),
    });

    source.emit("agent.invalid", JSON.stringify({
      schema_version: 1,
      run_id: "run-1",
      turn_id: "turn-1",
      sequence: 1,
      created_at: "2026-08-31T00:00:00Z",
      kind: "agent.invalid",
      payload: { raw_kind: "future/required" },
    }));

    expect(protocolErrors).toHaveLength(1);
    expect(protocolErrors[0]?.message).toContain("kind 不受支持");
    expect(source.closed).toBe(true);
    close();
  });

  it("repairs a finite stream gap before closing on stream.complete", async () => {
    const source = new FakeEventSource();
    const received: number[] = [];
    const close = subscribeToConversationEvents({
      url: "/events",
      finite: true,
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      fetchEventPage: async () => ({
        items: [JSON.parse(event(2)) as AgentTurnEvent],
        next_after: 2,
        has_more: false,
        stream_state: "terminal",
      }),
      onEvent: (value) => received.push(value.sequence),
    });

    source.emit("turn.started", event(1));
    source.emit("turn.started", event(3));
    source.emit("stream.complete");
    await flushAsyncWork();

    expect(received).toEqual([1, 2, 3]);
    expect(source.closed).toBe(true);
    close();
  });

  it("reports a protocol error when stream.complete repair skips the next sequence", async () => {
    const source = new FakeEventSource();
    const protocolErrors: Error[] = [];
    const close = subscribeToConversationEvents({
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      fetchEventPage: async () => ({
        items: [JSON.parse(event(3)) as AgentTurnEvent],
        next_after: 3,
        has_more: false,
        stream_state: "terminal",
      }),
      onEvent: vi.fn(),
      onProtocolError: (error) => protocolErrors.push(error),
    });

    source.emit("turn.started", event(1));
    source.emit("turn.started", event(3));
    source.emit("stream.complete");
    await flushAsyncWork();

    expect(protocolErrors).toHaveLength(1);
    expect(protocolErrors[0]?.message).toContain("未返回 sequence 2");
    expect(source.closed).toBe(true);
    close();
  });

  it("closes after three repair generations fail", async () => {
    vi.useFakeTimers();
    const sources: FakeEventSource[] = [];
    const protocolErrors: Error[] = [];
    const close = subscribeToConversationEvents({
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => {
        const source = new FakeEventSource();
        sources.push(source);
        return source;
      },
      fetchEventPage: async () => { throw new Error("page unavailable"); },
      onEvent: vi.fn(),
      onProtocolError: (error) => protocolErrors.push(error),
    });

    for (let attempt = 0; attempt < 3; attempt += 1) {
      sources[attempt].emit("turn.started", event(2));
      await flushAsyncWork();
      if (attempt < 2) await vi.advanceTimersByTimeAsync(250 * 2 ** attempt);
    }

    expect(protocolErrors).toHaveLength(1);
    expect(protocolErrors[0]?.message).toContain("page unavailable");
    expect(sources.at(-1)?.closed).toBe(true);
    close();
    vi.useRealTimers();
  });

  it("rejects a gap buffer larger than 512 events", () => {
    const source = new FakeEventSource();
    const protocolErrors: Error[] = [];
    const close = subscribeToConversationEvents({
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      fetchEventPage: () => new Promise(() => undefined),
      onEvent: vi.fn(),
      onProtocolError: (error) => protocolErrors.push(error),
    });

    for (let sequence = 2; sequence <= 514; sequence += 1) {
      source.emit("turn.started", event(sequence));
    }

    expect(protocolErrors).toHaveLength(1);
    expect(protocolErrors[0]?.message).toContain("缓存超过限制");
    expect(source.closed).toBe(true);
    close();
  });
  it("shares one EventSource and one assembled snapshot across consumers", () => {
    const source = new FakeEventSource();
    const onFirstEvent = vi.fn<(value: AgentTurnEvent) => void>();
    const onSecondEvent = vi.fn<(value: AgentTurnEvent) => void>();
    const runtime = new ConversationRuntime({
      key: "turn-1:/events",
      url: "/events",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
    });

    const releaseFirst = runtime.acquire({ onEvent: onFirstEvent });
    const releaseSecond = runtime.acquire({ onEvent: onSecondEvent });
    expect(runtime.hasConsumers).toBe(true);

    source.emit("turn.started", event(1));

    expect(onFirstEvent).toHaveBeenCalledOnce();
    expect(onSecondEvent).toHaveBeenCalledOnce();
    expect(runtime.getSnapshot().last_sequence).toBe(1);

    releaseFirst();
    expect(source.closed).toBe(false);
    releaseSecond();
    expect(source.closed).toBe(true);
    runtime.dispose();
  });

  it("reconciles a finite stream from its current cursor before closing", async () => {
    const sources: FakeEventSource[] = [];
    const received: number[] = [];
    const states: string[] = [];
    let resolvePage!: (page: ConversationEventPage) => void;
    const pageResult = new Promise<ConversationEventPage>((resolve) => {
      resolvePage = resolve;
    });
    const fetchEventPage = vi.fn(() => pageResult);
    const runtime = new ConversationRuntime({
      key: "turn-1:/events-finite",
      url: "/events-finite",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      finite: true,
      createEventSource: () => {
        const source = new FakeEventSource();
        sources.push(source);
        return source;
      },
      fetchEventPage,
    });

    runtime.acquire({
      onEvent: (value) => received.push(value.sequence),
      onConnectionState: (state) => states.push(state),
    });
    sources[0].emit("turn.started", event(1));
    sources[0].emit("error");

    expect(fetchEventPage).toHaveBeenCalledWith("/events-finite/page?after=1&limit=250", expect.any(AbortSignal));
    expect(sources).toHaveLength(1);
    expect(sources[0].closed).toBe(true);
    expect(states).toEqual(["connecting", "reconnecting"]);

    resolvePage({
      items: [JSON.parse(event(2, "turn.completed")) as AgentTurnEvent],
      next_after: 2,
      has_more: false,
      stream_state: "terminal",
    });
    await flushAsyncWork();

    expect(received).toEqual([1, 2]);
    expect(runtime.getSnapshot().last_sequence).toBe(2);
    expect(states).toEqual(["connecting", "reconnecting", "closed"]);
    runtime.dispose();
  });

  it("closes on stream.complete without reconnecting", () => {
    vi.useFakeTimers();
    const sources: FakeEventSource[] = [];
    const close = subscribeToConversationEvents({
      url: "/events-complete",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => {
        const source = new FakeEventSource();
        sources.push(source);
        return source;
      },
      onEvent: vi.fn(),
    });

    sources[0].emit("stream.complete");
    vi.advanceTimersByTime(10_000);
    expect(sources).toHaveLength(1);
    expect(sources[0].closed).toBe(true);
    close();
    vi.useRealTimers();
  });

  it("does not close on turn.awaiting_confirmation so approval.resolved can arrive", () => {
    const source = new FakeEventSource();
    const close = subscribeToConversationEvents({
      url: "/events-parked",
      scope: { run_id: "run-1", turn_id: "turn-1" },
      createEventSource: () => source,
      onEvent: vi.fn(),
    });
    source.emit(
      "turn.awaiting_confirmation",
      JSON.stringify({
        schema_version: 1,
        run_id: "run-1",
        turn_id: "turn-1",
        sequence: 1,
        created_at: "2026-08-31T00:00:00Z",
        kind: "turn.awaiting_confirmation",
        payload: { reason: "awaiting_confirmation" },
      }),
    );
    expect(source.closed).toBe(false);
    close();
  });
});
