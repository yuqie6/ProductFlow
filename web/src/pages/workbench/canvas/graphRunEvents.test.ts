import { describe, expect, it, vi } from "vitest";

import type { GraphRun, GraphRunListResponse } from "../../../lib/types";
import {
  applyGraphRunEvent,
  applyGraphRunEventToRun,
  subscribeGraphRunEvents,
  type GraphRunEvent,
} from "./graphRunEvents";

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

const run: GraphRun = {
  id: "r1",
  graph_id: "g1",
  status: "running",
  scope: "graph",
  requested_node_id: null,
  graph_revision: 3,
  failure_reason: null,
  is_retryable: true,
  started_at: "2026-08-31T00:00:00.000Z",
  finished_at: null,
  node_runs: [{
    id: "nr1",
    node_id: "n1",
    status: "running",
    sort_order: 0,
    compiled_context: null,
    output: null,
    failure_reason: null,
    attempt_count: 1,
    progress_phase: "provider_call",
    started_at: "2026-08-31T00:00:00.000Z",
    finished_at: null,
  }],
};

function event(overrides: Partial<GraphRunEvent> = {}): GraphRunEvent {
  return {
    schema_version: 1,
    run_id: "r1",
    sequence: 1,
    kind: "node.progress",
    node_run_id: "nr1",
    payload: { status: "running", phase: "provider_result_received" },
    created_at: "2026-08-31T00:00:01.000Z",
    ...overrides,
  };
}

describe("subscribeGraphRunEvents", () => {
  it("opens a cursor EventSource with credentials", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const stop = subscribeGraphRunEvents("/api/v3/runs/r1/events?view=canvas", () => undefined, { after: 7 });
    expect(FakeEventSource.last?.url).toBe("/api/v3/runs/r1/events?view=canvas&after=7");
    expect(FakeEventSource.last?.withCredentials).toBe(true);
    stop();
    vi.unstubAllGlobals();
  });

  it("delivers valid run.event frames once and ignores old sequences", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const onEvent = vi.fn();
    const onError = vi.fn();
    const stop = subscribeGraphRunEvents("/events", onEvent, { after: 2, onError });
    FakeEventSource.last?.emit("run.event", { data: JSON.stringify(event({ sequence: 3 })) } as MessageEvent<string> as Event);
    FakeEventSource.last?.emit("run.event", { data: JSON.stringify(event({ sequence: 3 })) } as MessageEvent<string> as Event);
    FakeEventSource.last?.emit("run.event", { data: JSON.stringify(event({ sequence: 2 })) } as MessageEvent<string> as Event);
    expect(onEvent).toHaveBeenCalledOnce();
    expect(onError).not.toHaveBeenCalled();
    stop();
    vi.unstubAllGlobals();
  });

  it("reports transport and schema errors without creating a fallback poller", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const onError = vi.fn();
    const stop = subscribeGraphRunEvents("/events", () => undefined, { onError });
    FakeEventSource.last?.emit("error", new Event("error"));
    FakeEventSource.last?.emit("run.event", { data: "not-json" } as MessageEvent<string> as Event);
    expect(onError).toHaveBeenCalledTimes(2);
    stop();
    vi.unstubAllGlobals();
  });

  it("closes cleanly after a terminal event without reporting a disconnect", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const onEvent = vi.fn();
    const onError = vi.fn();
    const stop = subscribeGraphRunEvents("/events", onEvent, { onError });
    const source = FakeEventSource.last;
    source?.emit("run.event", {
      data: JSON.stringify(event({ kind: "run.completed", node_run_id: null, payload: { status: "succeeded" } })),
    } as MessageEvent<string> as Event);
    source?.emit("error", new Event("error"));
    expect(onEvent).toHaveBeenCalledOnce();
    expect(source?.closed).toBe(true);
    expect(onError).not.toHaveBeenCalled();
    stop();
    vi.unstubAllGlobals();
  });
});

describe("applyGraphRunEvent", () => {
  it("keeps the list and unrelated runs structurally shared", () => {
    const previous: GraphRunListResponse = { items: [run, { ...run, id: "r2" }] };
    const next = applyGraphRunEvent(previous, event());
    expect(next).not.toBe(previous);
    expect(next?.items[1]).toBe(previous.items[1]);
    expect(next?.items[0]).not.toBe(previous.items[0]);
    expect(next?.items[0].node_runs[0].progress_phase).toBe("provider_result_received");
  });

  it("projects terminal node and run events incrementally", () => {
    const nodeEvent = event({
      sequence: 2,
      kind: "node.succeeded",
      payload: { status: "succeeded", output: { artifact_id: "a1" } },
      created_at: "2026-08-31T00:00:02.000Z",
    });
    const detailedNodeDone = applyGraphRunEventToRun(run, nodeEvent);
    expect(detailedNodeDone.node_runs[0].output).toEqual({ artifact_id: "a1" });

    const nodeDone = applyGraphRunEvent({ items: [run] }, nodeEvent);
    const runDone = applyGraphRunEvent(nodeDone, event({
      sequence: 3,
      kind: "run.completed",
      node_run_id: null,
      payload: { status: "succeeded" },
      created_at: "2026-08-31T00:00:03.000Z",
    }));
    expect(runDone?.items[0].node_runs[0].status).toBe("succeeded");
    expect(runDone?.items[0].node_runs[0].progress_phase).toBeNull();
    expect(runDone?.items[0].status).toBe("succeeded");
    expect(runDone?.items[0].finished_at).toBe("2026-08-31T00:00:03.000Z");
  });

  it("projects the persisted attempt count from claim events", () => {
    const next = applyGraphRunEvent({ items: [run] }, event({
      kind: "node.claimed",
      payload: { status: "running", attempt_count: 2 },
    }));
    expect(next?.items[0].node_runs[0].attempt_count).toBe(2);
  });

  it.each(["succeeded", "skipped", "failed", "cancelled", "unknown"] as const)(
    "clears live progress when a node becomes %s",
    (status) => {
      const next = applyGraphRunEvent({ items: [run] }, event({
        kind: `node.${status}`,
        payload: { status },
      }));
      expect(next?.items[0].node_runs[0].status).toBe(status);
      expect(next?.items[0].node_runs[0].progress_phase).toBeNull();
      expect(next?.items[0].node_runs[0].finished_at).toBe("2026-08-31T00:00:01.000Z");
    },
  );

  it("keeps progress while the node remains live", () => {
    const next = applyGraphRunEvent({ items: [run] }, event({
      payload: { status: "running", phase: "provider_result_received" },
    }));
    expect(next?.items[0].node_runs[0].progress_phase).toBe("provider_result_received");
    expect(next?.items[0].node_runs[0].finished_at).toBeNull();
  });
});
