import { describe, expect, it, vi } from "vitest";

import type { ImageSessionStatus } from "../../lib/types";
import { subscribeImageSessionEvents } from "./sessionEvents";

class FakeEventSource {
  static last: FakeEventSource | null = null;
  url: string;
  withCredentials: boolean;
  listeners = new Map<string, EventListener>();
  closed = false;

  constructor(url: string, init?: EventSourceInit) {
    this.url = url;
    this.withCredentials = Boolean(init?.withCredentials);
    FakeEventSource.last = this;
  }

  addEventListener(type: string, listener: EventListener) {
    this.listeners.set(type, listener);
  }

  removeEventListener(type: string) {
    this.listeners.delete(type);
  }

  emit(type: string, event: Event) {
    this.listeners.get(type)?.(event);
  }

  close() {
    this.closed = true;
    this.listeners.clear();
  }
}

function status(overrides: Partial<ImageSessionStatus> = {}): ImageSessionStatus {
  return {
    id: "session-1",
    title: "会话",
    rounds_count: 0,
    latest_round_id: null,
    latest_generation_group_id: null,
    has_active_generation_task: true,
    generation_tasks: [],
    created_at: "2026-08-31T00:00:00.000Z",
    updated_at: "2026-08-31T00:00:00.000Z",
    ...overrides,
  };
}

describe("subscribeImageSessionEvents", () => {
  it("opens an EventSource with credentials", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const stop = subscribeImageSessionEvents("/api/image-sessions/s1/events", () => undefined);
    expect(FakeEventSource.last?.url).toBe("/api/image-sessions/s1/events");
    expect(FakeEventSource.last?.withCredentials).toBe(true);
    stop();
    vi.unstubAllGlobals();
  });

  it("delivers session.status frames", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const onStatus = vi.fn();
    const onError = vi.fn();
    const stop = subscribeImageSessionEvents("/events", onStatus, { onError });
    FakeEventSource.last?.emit("session.status", {
      data: JSON.stringify(status({ rounds_count: 1 })),
    } as MessageEvent<string> as Event);
    expect(onStatus).toHaveBeenCalledOnce();
    expect(onStatus.mock.calls[0][0].rounds_count).toBe(1);
    expect(onError).not.toHaveBeenCalled();
    expect(FakeEventSource.last?.closed).toBe(false);
    stop();
    vi.unstubAllGlobals();
  });

  it("reports transport and schema errors without creating a fallback poller", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const onError = vi.fn();
    const stop = subscribeImageSessionEvents("/events", () => undefined, { onError });
    FakeEventSource.last?.emit("error", new Event("error"));
    FakeEventSource.last?.emit("session.status", { data: "not-json" } as MessageEvent<string> as Event);
    expect(onError).toHaveBeenCalledTimes(2);
    stop();
    vi.unstubAllGlobals();
  });

  it("closes cleanly after idle status without reporting a disconnect", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const onStatus = vi.fn();
    const onError = vi.fn();
    const stop = subscribeImageSessionEvents("/events", onStatus, { onError });
    const source = FakeEventSource.last;
    source?.emit("session.status", {
      data: JSON.stringify(status({
        has_active_generation_task: false,
        rounds_count: 1,
        latest_round_id: "round-1",
      })),
    } as MessageEvent<string> as Event);
    source?.emit("error", new Event("error"));
    expect(onStatus).toHaveBeenCalledOnce();
    expect(source?.closed).toBe(true);
    expect(onError).not.toHaveBeenCalled();
    stop();
    vi.unstubAllGlobals();
  });
});
